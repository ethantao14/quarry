package index

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Expected bytes below were worked out by hand from the format description,
// not produced by the writer. Corpus: x="b a b", yz="a", w="c" x300.
func TestWriteGoldenFormat(t *testing.T) {
	ix := New()
	ix.Add("x", []string{"b", "a", "b"})
	ix.Add("yz", []string{"a"})
	ix.Add("w", strings.Fields(strings.Repeat("c ", 300)))

	dir := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(dir); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	version := []byte{1, 0, 0, 0}
	wantDict := concat(
		[]byte("QDCT"), version,
		[]byte{3, 0, 0, 0}, // term count
		// termOffset, termLen, docFreq, postingsOffset (8 bytes), postingsLen
		[]byte{0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0},  // "a"
		[]byte{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 12, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0}, // "b"
		[]byte{2, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 14, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0}, // "c"
		[]byte("abc"),
	)
	wantPost := concat(
		[]byte("QPST"), version,
		[]byte{0, 1, 1, 1},    // "a": doc 0 tf 1, gap 1 tf 1
		[]byte{0, 2},          // "b": doc 0 tf 2
		[]byte{2, 0xAC, 0x02}, // "c": doc 2, tf 300 as a two-byte uvarint
	)
	wantLens := concat(
		[]byte("QLEN"), version,
		[]byte{3, 0, 0, 0},
		[]byte{3, 0, 0, 0, 1, 0, 0, 0, 0x2C, 0x01, 0, 0}, // 3, 1, 300
	)
	wantIDs := concat(
		[]byte("QIDS"), version,
		[]byte{3, 0, 0, 0},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0}, // offsets 0, 1, 3, 4
		[]byte{1, 0, 0, 0, 0, 0, 0, 0},
		[]byte{3, 0, 0, 0, 0, 0, 0, 0},
		[]byte{4, 0, 0, 0, 0, 0, 0, 0},
		[]byte("xyzw"),
	)

	want := map[string][]byte{
		"seg0.dict": wantDict,
		"seg0.post": wantPost,
		"seg0.lens": wantLens,
		"seg0.ids":  wantIDs,
	}
	for name, wantBytes := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, wantBytes) {
			t.Errorf("%s =\n% x\nwant\n% x", name, got, wantBytes)
		}
	}

	castagnoli := crc32.MakeTable(crc32.Castagnoli)
	crc := func(name string) uint32 { return crc32.Checksum(want[name], castagnoli) }
	wantManifest := fmt.Sprintf(`{
  "format_version": 1,
  "doc_count": 3,
  "total_terms": 304,
  "term_count": 3,
  "files": {
    "seg0.dict": {
      "size": 87,
      "crc32c": %d
    },
    "seg0.ids": {
      "size": 48,
      "crc32c": %d
    },
    "seg0.lens": {
      "size": 24,
      "crc32c": %d
    },
    "seg0.post": {
      "size": 17,
      "crc32c": %d
    }
  }
}
`, crc("seg0.dict"), crc("seg0.ids"), crc("seg0.lens"), crc("seg0.post"))

	gotManifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if string(gotManifest) != wantManifest {
		t.Errorf("manifest.json =\n%s\nwant\n%s", gotManifest, wantManifest)
	}
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}
