package index

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func smallIndex() *Index {
	ix := New()
	ix.Add("first", []string{"a", "a", "b"})
	ix.Add("", nil)
	ix.Add("三", []string{"b", "猫"})
	return ix
}

func TestDiskRoundTrip(t *testing.T) {
	type roundTripCase struct {
		name string
		ix   *Index
	}
	tests := []roundTripCase{
		{name: "empty", ix: New()},
		{name: "small", ix: smallIndex()},
	}
	for seed := uint64(0); seed < 30; seed++ {
		random := rand.New(rand.NewPCG(seed, 2))
		ix := New()
		vocabulary := []string{"", "a", "b", "c", "猫", "café"}
		for doc, count := 0, random.IntN(41); doc < count; doc++ {
			terms := make([]string, random.IntN(31))
			for i := range terms {
				terms[i] = vocabulary[random.IntN(len(vocabulary))]
			}
			ix.Add(fmt.Sprint(doc), terms)
		}
		tests = append(tests, roundTripCase{name: fmt.Sprintf("seed %d", seed), ix: ix})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "idx")
			if err := tt.ix.Write(dir); err != nil {
				t.Fatal(err)
			}
			disk, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			cleanupDisk(t, disk)
			if disk.DocCount() != tt.ix.DocCount() || disk.AvgDocLen() != tt.ix.AvgDocLen() {
				t.Fatalf("index statistics differ: %d, %v", disk.DocCount(), disk.AvgDocLen())
			}
			for i := 0; i < tt.ix.DocCount(); i++ {
				docID := uint32(i)
				if disk.DocLen(docID) != tt.ix.DocLen(docID) || disk.ExternalID(docID) != tt.ix.ExternalID(docID) {
					t.Fatalf("document %d differs", i)
				}
			}
			terms := []string{"absent"}
			for term := range tt.ix.postings {
				terms = append(terms, term)
			}
			for _, term := range terms {
				got, err := disk.Postings(term)
				if err != nil {
					t.Fatal(err)
				}
				want, err := tt.ix.Postings(term)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Postings(%q) = %v, want %v", term, got, want)
				}
			}
		})
	}
}

func cleanupDisk(t *testing.T, disk *Disk) {
	t.Helper()
	t.Cleanup(func() {
		if err := disk.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

func readTestFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeTestFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func rewriteManifest(t *testing.T, dir string, change func(*manifest)) {
	t.Helper()
	var m manifest
	if err := json.Unmarshal(readTestFile(t, dir, manifestName), &m); err != nil {
		t.Fatal(err)
	}
	change(&m)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, dir, manifestName, append(data, '\n'))
}

func rewriteFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	writeTestFile(t, dir, name, data)
	rewriteManifest(t, dir, func(m *manifest) {
		m.Files[name] = fileInfo{Size: uint64(len(data)), CRC32C: crc32.Checksum(data, checksumTable)}
	})
}

func copyTestIndex(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenFileCorruption(t *testing.T) {
	source := filepath.Join(t.TempDir(), "idx")
	if err := smallIndex().Write(source); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{dictName, postName, skipName, lensName, idsName} {
		t.Run(name, func(t *testing.T) {
			tests := []struct {
				name        string
				change      func([]byte) []byte
				fixChecksum bool
				want        string
			}{
				{"flip byte", func(b []byte) []byte { b[len(b)-1] ^= 1; return b }, false, "checksum mismatch"},
				{"truncate", func(b []byte) []byte { return b[:len(b)-1] }, false, "size mismatch"},
				{"empty", func(b []byte) []byte { return b[:0] }, false, "size mismatch"},
				{"short header", func(b []byte) []byte { return b[:7] }, true, "truncated header"},
				{"bad magic", func(b []byte) []byte { b[0] = '!'; return b }, true, "bad magic"},
				{"bad version", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:8], 1); return b }, true, "index is format 1, this build reads format 2; rebuild it with quarry-index"},
				{"delete", nil, false, name},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					dir := copyTestIndex(t, source)
					if tt.change == nil {
						if err := os.Remove(filepath.Join(dir, name)); err != nil {
							t.Fatal(err)
						}
					} else {
						data := tt.change(readTestFile(t, dir, name))
						if tt.fixChecksum {
							rewriteFile(t, dir, name, data)
						} else {
							writeTestFile(t, dir, name, data)
						}
					}
					disk, err := Open(dir)
					if err == nil {
						cleanupDisk(t, disk)
					}
					if err == nil || !strings.Contains(err.Error(), tt.want) || (tt.name != "bad version" && !strings.Contains(err.Error(), name)) {
						t.Fatalf("Open() error = %v, want %q and %q", err, tt.want, name)
					}
					if tt.change == nil && !errors.Is(err, fs.ErrNotExist) {
						t.Errorf("Open() error = %v, want file not found", err)
					}
				})
			}
		})
	}
}

func TestOpenManifestCorruption(t *testing.T) {
	source := filepath.Join(t.TempDir(), "idx")
	if err := smallIndex().Write(source); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*testing.T, string)
		want   string
	}{
		{"delete", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
				t.Fatal(err)
			}
		}, "manifest"},
		{"version", func(t *testing.T, dir string) { rewriteManifest(t, dir, func(m *manifest) { m.FormatVersion = 1 }) }, "index is format 1, this build reads format 2; rebuild it with quarry-index"},
		{"unknown field", func(t *testing.T, dir string) {
			data := readTestFile(t, dir, manifestName)
			data = append([]byte(`{"unknown":1,`), data[1:]...)
			writeTestFile(t, dir, manifestName, data)
		}, "unknown field"},
		{"trailing JSON", func(t *testing.T, dir string) {
			writeTestFile(t, dir, manifestName, append(readTestFile(t, dir, manifestName), []byte("{}")...))
		}, "trailing JSON"},
		{"malformed JSON", func(t *testing.T, dir string) { writeTestFile(t, dir, manifestName, []byte("{")) }, "manifest"},
		{"missing file entry", func(t *testing.T, dir string) {
			rewriteManifest(t, dir, func(m *manifest) { delete(m.Files, postName) })
		}, "manifest"},
		{"wrong file entry", func(t *testing.T, dir string) {
			rewriteManifest(t, dir, func(m *manifest) { m.Files["unknown"] = m.Files[postName]; delete(m.Files, postName) })
		}, postName},
		{"doc count", func(t *testing.T, dir string) { rewriteManifest(t, dir, func(m *manifest) { m.DocCount++ }) }, lensName},
		{"term count", func(t *testing.T, dir string) { rewriteManifest(t, dir, func(m *manifest) { m.TermCount++ }) }, dictName},
		{"total terms", func(t *testing.T, dir string) { rewriteManifest(t, dir, func(m *manifest) { m.TotalTerms++ }) }, lensName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestIndex(t, source)
			tt.change(t, dir)
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

func TestOpenTableCorruption(t *testing.T) {
	source := filepath.Join(t.TempDir(), "idx")
	if err := smallIndex().Write(source); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		file   string
		change func([]byte) []byte
		want   string
		lookup bool
	}{
		{"short dict count", dictName, func(b []byte) []byte { return b[:8] }, "term count", false},
		{"short dict entries", dictName, func(b []byte) []byte { return b[:13] }, "entries table", false},
		{"term offset", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[12:16], math.MaxUint32); return b }, "term range", false},
		{"term length", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:20], math.MaxUint32); return b }, "term range", false},
		{"unsorted terms", dictName, func(b []byte) []byte { b[12+3*dictEntrySize] = 'z'; return b }, "strictly ascending", false},
		{"duplicate terms", dictName, func(b []byte) []byte { b[12+3*dictEntrySize+1] = 'a'; return b }, "strictly ascending", false},
		{"post offset overflow", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[24:32], math.MaxUint64); return b }, "postings range", false},
		{"post length", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[32:36], math.MaxUint32); return b }, "postings range", false},
		{"post inside header", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[24:32], 7); return b }, "postings range", false},
		{"zero doc freq", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[20:24], 0); return b }, "document frequency", false},
		{"large doc freq", dictName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[20:24], 4); return b }, "document frequency", false},
		{"lens count", lensName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:12], 4); return b }, "document count", false},
		{"lens truncated", lensName, func(b []byte) []byte { return b[:len(b)-1] }, "table size", false},
		{"lens sum", lensName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[12:16], 20); return b }, "total terms", false},
		{"ids count", idsName, func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:12], 4); return b }, "document count", false},
		{"ids truncated offsets", idsName, func(b []byte) []byte { return b[:13] }, "offsets table", false},
		{"ids first offset", idsName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[12:20], 1); return b }, "invalid offset", false},
		{"ids decreasing offsets", idsName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[28:36], 1); return b }, "invalid offset", false},
		{"ids offset overflow", idsName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[20:28], math.MaxUint64); return b }, "invalid offset", false},
		{"ids final offset", idsName, func(b []byte) []byte { binary.LittleEndian.PutUint64(b[36:44], 7); return b }, "final offset", false},
		{"posting doc out of range", postName, func(b []byte) []byte { b[8] = 3; return b }, "doc ID out of bounds", true},
		{"posting zero tf", postName, func(b []byte) []byte { b[9] = 0; return b }, "zero term frequency", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestIndex(t, source)
			rewriteFile(t, dir, tt.file, tt.change(readTestFile(t, dir, tt.file)))
			disk, err := Open(dir)
			if err == nil {
				cleanupDisk(t, disk)
			}
			if tt.lookup {
				if err != nil {
					t.Fatalf("Open() error = %v", err)
				}
				_, err = disk.Postings("a")
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), tt.file) {
				t.Fatalf("error = %v, want %q and %q", err, tt.want, tt.file)
			}
		})
	}
}

// TestDiskClose checks idempotent cleanup and ownership of returned data.
func TestDiskClose(t *testing.T) {
	memory := smallIndex()
	dir := filepath.Join(t.TempDir(), "idx")
	if err := memory.Write(dir); err != nil {
		t.Fatal(err)
	}
	disk, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDisk(t, disk)
	list, err := disk.Postings("a")
	if err != nil {
		t.Fatal(err)
	}
	id := disk.ExternalID(0)
	if err := disk.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if disk.dict != nil || disk.post != nil || disk.skip != nil || disk.lens != nil || disk.ids != nil {
		t.Error("Close() did not clear all mappings")
	}
	if err := disk.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if !reflect.DeepEqual(list, memory.postings["a"]) {
		t.Errorf("saved postings = %v, want %v", list, memory.postings["a"])
	}
	if id != memory.ExternalID(0) {
		t.Errorf("saved external ID = %q, want %q", id, memory.ExternalID(0))
	}
}

// TestDiskConcurrentReads compares concurrent lookups with an in-memory index.
func TestDiskConcurrentReads(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	vocabulary := []string{"", "a", "b", "c", "猫", "café"}
	memory := New()
	for doc := 0; doc < 30; doc++ {
		terms := make([]string, random.IntN(31))
		for i := range terms {
			terms[i] = vocabulary[random.IntN(len(vocabulary))]
		}
		memory.Add(fmt.Sprintf("doc-%d", doc), terms)
	}
	dir := filepath.Join(t.TempDir(), "idx")
	if err := memory.Write(dir); err != nil {
		t.Fatal(err)
	}
	disk, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDisk(t, disk)
	vocabulary = append(vocabulary, "absent")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				for _, term := range vocabulary {
					got, err := disk.Postings(term)
					if err != nil {
						t.Errorf("Postings(%q) error = %v", term, err)
						return
					}
					want, err := memory.Postings(term)
					if err != nil {
						t.Errorf("memory.Postings(%q) error = %v", term, err)
						return
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("Postings(%q) = %v, want %v", term, got, want)
						return
					}
				}
				for doc := 0; doc < memory.DocCount(); doc++ {
					docID := uint32(doc)
					if disk.DocLen(docID) != memory.DocLen(docID) || disk.ExternalID(docID) != memory.ExternalID(docID) {
						t.Errorf("document %d differs", doc)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

func TestDiskPostingCount(t *testing.T) {
	docs := []builderDocument{
		{id: "a", terms: []string{"red", "fish", "fish"}},
		{id: "b", terms: []string{"blue", "fish"}},
		{id: "c"},
	}
	for _, mode := range []string{"empty", "small", "merged"} {
		t.Run(mode, func(t *testing.T) {
			memory := New()
			if mode != "empty" {
				for _, doc := range docs {
					memory.Add(doc.id, doc.terms)
				}
			}
			dir := filepath.Join(t.TempDir(), "idx")
			if mode == "merged" {
				builder, err := NewBuilder(dir, 1)
				if err != nil {
					t.Fatal(err)
				}
				for _, doc := range docs {
					if err := builder.Add(doc.id, doc.terms); err != nil {
						t.Fatal(err)
					}
				}
				if err := builder.Finish(); err != nil {
					t.Fatal(err)
				}
				if builder.Segments() < 2 {
					t.Fatalf("Segments() = %d, want multiple segments", builder.Segments())
				}
			} else if err := memory.Write(dir); err != nil {
				t.Fatal(err)
			}
			disk, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			cleanupDisk(t, disk)
			var want, decoded uint64
			for term, list := range memory.postings {
				want += uint64(len(list))
				got, err := disk.Postings(term)
				if err != nil {
					t.Fatal(err)
				}
				decoded += uint64(len(got))
			}
			if disk.PostingCount() != want || decoded != want {
				t.Fatalf("PostingCount() = %d, decoded = %d, want %d", disk.PostingCount(), decoded, want)
			}
		})
	}
}
