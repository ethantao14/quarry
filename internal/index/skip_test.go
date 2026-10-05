package index

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

func TestOpenSkipCorruption(t *testing.T) {
	ix := New()
	for range 300 {
		ix.Add("doc", []string{"a", "b"})
	}
	source := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(source); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		file   string
		change func([]byte) []byte
		want   string
	}{
		{"short skip", skipName, func(b []byte) []byte { return b[:len(b)-1] }, "table size"},
		{"extra skip", skipName, func(b []byte) []byte { return append(b, make([]byte, skipEntrySize)...) }, "table size"},
		{"first skip offset", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[tableStart+24:], 9); return b }, "skip offset"},
		{"later skip offset", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[tableStart+dictEntrySize+24:], 8); return b }, "skip offset"},
		{"overflow skip offset", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[tableStart+24:], math.MaxUint64); return b }, "skip offset"},
		{"first block offset", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+4:], 1); return b }, "block offset"},
		{"equal block offset", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+skipEntrySize+4:], 0); return b }, "block offset"},
		{"decreasing block offset", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+2*skipEntrySize+4:], 1); return b }, "block offset"},
		{"block offset at end", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+skipEntrySize+4:], 600); return b }, "block offset"},
		{"block offset past end", skipName, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[headerSize+skipEntrySize+4:], math.MaxUint32)
			return b
		}, "block offset"},
		{"equal last doc", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+skipEntrySize:], 127); return b }, "last doc ID"},
		{"decreasing last doc", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+skipEntrySize:], 126); return b }, "last doc ID"},
		{"last doc out of bounds", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize:], 300); return b }, "last doc ID"},
		// Term "a" has blocks of 128, 128 and 44 postings; only the bounds check catches the last one.
		{"last block doc out of bounds", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+2*skipEntrySize:], 300); return b }, "last doc ID"},
		{"last doc overflow", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize:], math.MaxUint32); return b }, "last doc ID"},
		{"negative block max", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+8:], math.Float32bits(-1)); return b }, "block maximum"},
		{"nan block max", skipName, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[headerSize+8:], math.Float32bits(float32(math.NaN())))
			return b
		}, "block maximum"},
		{"positive inf block max", skipName, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[headerSize+8:], math.Float32bits(float32(math.Inf(1))))
			return b
		}, "block maximum"},
		{"negative inf block max", skipName, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[headerSize+8:], math.Float32bits(float32(math.Inf(-1))))
			return b
		}, "block maximum"},
		{"term max too low", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[tableStart+32:], 0); return b }, "maximum score"},
		{"term max too high", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[tableStart+32:], math.Float32bits(1)); return b }, "maximum score"},
		{"term max nan", dictName, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[tableStart+32:], math.Float32bits(float32(math.NaN())))
			return b
		}, "maximum score"},
		{"term max inf", dictName, func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[tableStart+32:], math.Float32bits(float32(math.Inf(1))))
			return b
		}, "maximum score"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestIndex(t, source)
			rewriteFile(t, dir, tt.file, tt.change(readTestFile(t, dir, tt.file)))
			disk, err := Open(dir)
			if err == nil {
				cleanupDisk(t, disk)
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Open() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestOpenBM25(t *testing.T) {
	source := filepath.Join(t.TempDir(), "idx")
	if err := smallIndex().Write(source); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"bm25_k1", "bm25_b"} {
		for _, value := range []string{"missing", "null", "1e999", "-1e999", `"NaN"`, `"Infinity"`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				dir := copyTestIndex(t, source)
				var m map[string]json.RawMessage
				if err := json.Unmarshal(readTestFile(t, dir, manifestName), &m); err != nil {
					t.Fatal(err)
				}
				if value == "missing" {
					delete(m, field)
				} else {
					m[field] = json.RawMessage(value)
				}
				data, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, dir, manifestName, data)
				disk, err := Open(dir)
				if err == nil {
					cleanupDisk(t, disk)
				}
				if err == nil || !strings.Contains(err.Error(), manifestName) {
					t.Fatalf("Open() error = %v", err)
				}
			})
		}
	}
	// Zero values are present and finite, and must not be mistaken for absence.
	dir := copyTestIndex(t, source)
	rewriteManifest(t, dir, func(m *manifest) { *m.BM25K1 = 0; *m.BM25B = 0 })
	disk, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDisk(t, disk)
	if disk.BM25() != (scoring.BM25{}) {
		t.Fatalf("BM25() = %v", disk.BM25())
	}
}

func TestVersionRebuildMessage(t *testing.T) {
	source := filepath.Join(t.TempDir(), "idx")
	if err := smallIndex().Write(source); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{manifestName, dictName, postName, skipName, lensName, idsName} {
		t.Run(name, func(t *testing.T) {
			dir := copyTestIndex(t, source)
			if name == manifestName {
				rewriteManifest(t, dir, func(m *manifest) { m.FormatVersion = 1 })
			} else {
				data := readTestFile(t, dir, name)
				binary.LittleEndian.PutUint32(data[4:8], 1)
				rewriteFile(t, dir, name, data)
			}
			disk, err := Open(dir)
			if err == nil {
				cleanupDisk(t, disk)
			}
			want := "index is format 1, this build reads format 2; rebuild it with quarry-index"
			if err == nil || err.Error() != want {
				t.Fatalf("Open() error = %v, want %q", err, want)
			}
		})
	}
}

func TestCursorCorruption(t *testing.T) {
	ix := New()
	for range 300 {
		ix.Add("doc", []string{"term"})
	}
	source := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(source); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		file   string
		change func([]byte) []byte
		want   string
	}{
		{"first gap", postName, func(b []byte) []byte { b[headerSize] = 1; return b }, "last doc ID"},
		{"second block gap", postName, func(b []byte) []byte { b[headerSize+256] = 2; return b }, "last doc ID"},
		{"second block zero tf", postName, func(b []byte) []byte { b[headerSize+257] = 0; return b }, "zero term frequency"},
		{"short block", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+skipEntrySize+4:], 255); return b }, "truncated"},
		{"long block", skipName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[headerSize+skipEntrySize+4:], 257); return b }, "decoded bytes"},
		{"last block trailing bytes", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[tableStart+8:], 299); return b }, "decoded bytes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestIndex(t, source)
			rewriteFile(t, dir, tt.file, tt.change(readTestFile(t, dir, tt.file)))
			disk, err := Open(dir)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			cleanupDisk(t, disk)
			for _, walk := range []string{"next", "advance"} {
				c, err := disk.Cursor("term")
				for err == nil && c.DocID() != query.NoMoreDocs {
					if walk == "next" {
						err = c.Next()
					} else {
						err = c.Advance(c.DocID() + 128)
					}
				}
				if err == nil || !strings.Contains(err.Error(), postName) || !strings.Contains(err.Error(), `term "term"`) || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("%s error = %v, want %q with file and term", walk, err, tt.want)
				}
			}
		})
	}
}

func TestPostingsStreamUnchanged(t *testing.T) {
	ix := cursorIndex(300, 42)
	dir := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(dir); err != nil {
		t.Fatal(err)
	}
	disk, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDisk(t, disk)
	for i := uint32(0); i < disk.termCount; i++ {
		e := disk.entry(i)
		term := string(disk.term(e))
		want := postingsBytes(ix, term)
		got := disk.post[e.postingsOffset : e.postingsOffset+uint64(e.postingsLen)]
		if !bytes.Equal(got, want) {
			t.Fatalf("postings bytes changed for %q", term)
		}
	}
}

func postingsBytes(ix *Index, term string) []byte {
	var data []byte
	var previous uint32
	for _, p := range ix.postings[term] {
		data = binary.AppendUvarint(data, uint64(p.DocID-previous))
		data = binary.AppendUvarint(data, uint64(p.TF))
		previous = p.DocID
	}
	return data
}
