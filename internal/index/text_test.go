package index

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type textDoc struct {
	id    string
	text  string
	terms []string
}

func randomTextDocs(seed uint64) []textDoc {
	random := rand.New(rand.NewPCG(seed, 9))
	vocabulary := []string{"a", "b", "c", "猫", "café"}
	docs := make([]textDoc, random.IntN(30))
	for i := range docs {
		terms := make([]string, random.IntN(8))
		for j := range terms {
			terms[j] = vocabulary[random.IntN(len(vocabulary))]
		}
		// Empty text, and text unrelated to the terms, must both round trip.
		docs[i] = textDoc{id: fmt.Sprint(i), text: strings.Repeat("x\n猫 ", random.IntN(4)), terms: terms}
	}
	return docs
}

func buildTextIndex(t testing.TB, dir string, docs []textDoc, budget int64, storeText bool) *Builder {
	t.Helper()
	builder, err := NewBuilder(dir, budget, storeText)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		if err := builder.Add(doc.id, doc.text, doc.terms); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.Finish(); err != nil {
		t.Fatal(err)
	}
	return builder
}

func TestBuilderStoresText(t *testing.T) {
	for seed := uint64(0); seed < 20; seed++ {
		docs := randomTextDocs(seed)
		// Budget 1 flushes every document, so the text must survive a merge.
		for _, budget := range []int64{1, 1 << 20} {
			t.Run(fmt.Sprintf("seed %d budget %d", seed, budget), func(t *testing.T) {
				withText := filepath.Join(t.TempDir(), "text")
				without := filepath.Join(t.TempDir(), "plain")
				buildTextIndex(t, withText, docs, budget, true)
				buildTextIndex(t, without, docs, budget, false)
				disk, err := Open(withText)
				if err != nil {
					t.Fatal(err)
				}
				cleanupDisk(t, disk)
				if !disk.HasText() {
					t.Fatal("HasText() = false, want true")
				}
				for i, doc := range docs {
					if got := disk.Text(uint32(i)); got != doc.text {
						t.Fatalf("Text(%d) = %q, want %q", i, got, doc.text)
					}
				}
				// Storing text must not change the rest of the index.
				for _, name := range []string{dictName, postName, skipName, lensName, idsName} {
					if !bytes.Equal(readTestFile(t, withText, name), readTestFile(t, without, name)) {
						t.Errorf("%s differs when text is stored", name)
					}
				}
			})
		}
	}
}

func TestIndexWithoutText(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "idx")
	buildTextIndex(t, dir, []textDoc{{id: "a", text: "ignored", terms: []string{"a"}}}, 1<<20, false)
	disk, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDisk(t, disk)
	if disk.HasText() || disk.Text(0) != "" {
		t.Errorf("HasText() = %t, Text(0) = %q, want false and empty", disk.HasText(), disk.Text(0))
	}
	if _, err := os.Stat(filepath.Join(dir, textName)); !os.IsNotExist(err) {
		t.Errorf("%s exists without --store-text: %v", textName, err)
	}
}

func TestOpenTextCorruption(t *testing.T) {
	source := filepath.Join(t.TempDir(), "idx")
	docs := []textDoc{{id: "a", text: "red fish", terms: []string{"red", "fish"}}, {id: "b", text: "car", terms: []string{"car"}}}
	buildTextIndex(t, source, docs, 1<<20, true)
	offsets := func(values ...uint64) []byte {
		data := binary.LittleEndian.AppendUint32([]byte(textOffsetMagic), FormatVersion)
		for _, value := range values {
			data = binary.LittleEndian.AppendUint64(data, value)
		}
		return data
	}
	tests := []struct {
		name    string
		corrupt func(t *testing.T, dir string)
		wantErr string
	}{
		{"only one text file", func(t *testing.T, dir string) {
			rewriteManifest(t, dir, func(m *manifest) { delete(m.Files, textOffsetsName) })
		}, "expected five binary files, or seven with stored text"},
		{"text file listed under another name", func(t *testing.T, dir string) {
			rewriteManifest(t, dir, func(m *manifest) {
				m.Files["seg0.other"] = m.Files[textName]
				delete(m.Files, textName)
			})
		}, "expected five binary files, or seven with stored text"},
		{"offsets for too few documents", func(t *testing.T, dir string) {
			rewriteFile(t, dir, textOffsetsName, offsets(0, 8))
		}, "size does not match document count"},
		{"offsets for too many documents", func(t *testing.T, dir string) {
			rewriteFile(t, dir, textOffsetsName, offsets(0, 8, 11, 11))
		}, "size does not match document count"},
		{"first offset not zero", func(t *testing.T, dir string) {
			rewriteFile(t, dir, textOffsetsName, offsets(1, 8, 11))
		}, "invalid offset 0"},
		{"offsets decrease", func(t *testing.T, dir string) {
			rewriteFile(t, dir, textOffsetsName, offsets(0, 9, 8))
		}, "invalid offset 2"},
		{"offset past the text", func(t *testing.T, dir string) {
			rewriteFile(t, dir, textOffsetsName, offsets(0, 12, 12))
		}, "invalid offset 1"},
		{"text longer than the offsets say", func(t *testing.T, dir string) {
			rewriteFile(t, dir, textOffsetsName, offsets(0, 8, 10))
		}, "final offset does not match text length"},
		{"bad text magic", func(t *testing.T, dir string) {
			data := readTestFile(t, dir, textName)
			data[0] = 'X'
			rewriteFile(t, dir, textName, data)
		}, "seg0.text: bad magic"},
		{"text checksum", func(t *testing.T, dir string) {
			data := readTestFile(t, dir, textName)
			data[len(data)-1] ^= 1
			writeTestFile(t, dir, textName, data)
		}, "seg0.text: checksum mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestIndex(t, source)
			tt.corrupt(t, dir)
			disk, err := Open(dir)
			if err == nil {
				_ = disk.Close()
				t.Fatal("Open() error = nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Open() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestAbortRemovesText(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "idx")
	builder, err := NewBuilder(dir, 1<<20, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Add("a", "text", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := builder.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("index directory still exists after Abort: %v", err)
	}
}
