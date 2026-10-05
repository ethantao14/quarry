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
// not produced by the writer. Corpus: x="b a b", yz="a", w="c" x300. Max scores
// are BM25 (k1 0.9, b 0.4, N 3, average length 304/3) computed separately in
// Python and rounded up to float32.
func TestWriteGoldenFormat(t *testing.T) {
	ix := New()
	ix.Add("x", []string{"b", "a", "b"})
	ix.Add("yz", []string{"a"})
	ix.Add("w", strings.Fields(strings.Repeat("c ", 300)))

	dir := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(dir); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	version := []byte{2, 0, 0, 0}
	maxA := []byte{0xBC, 0xE6, 0x9B, 0x3E} // 0.30449474 (doc 1, tf 1, length 1)
	maxB := []byte{0x64, 0xE2, 0x44, 0x3F} // 0.76907945 (doc 0, tf 2, length 3)
	maxC := []byte{0x65, 0xC1, 0x79, 0x3F} // 0.9756072 (doc 2, tf 300, length 300)
	wantDict := concat(
		[]byte("QDCT"), version,
		[]byte{3, 0, 0, 0}, // term count
		// termOffset, termLen, docFreq, postingsOffset (8 bytes), postingsLen, skipOffset (8 bytes), maxScore
		[]byte{0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0}, maxA, // "a"
		[]byte{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 12, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 20, 0, 0, 0, 0, 0, 0, 0}, maxB, // "b"
		[]byte{2, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 14, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 32, 0, 0, 0, 0, 0, 0, 0}, maxC, // "c"
		[]byte("abc"),
	)
	// One block per term: lastDocID, offset within the term's postings, blockMax.
	wantSkip := concat(
		[]byte("QSKP"), version,
		[]byte{1, 0, 0, 0, 0, 0, 0, 0}, maxA, // "a": last doc 1
		[]byte{0, 0, 0, 0, 0, 0, 0, 0}, maxB, // "b": last doc 0
		[]byte{2, 0, 0, 0, 0, 0, 0, 0}, maxC, // "c": last doc 2
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
		"seg0.skip": wantSkip,
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
  "format_version": 2,
  "doc_count": 3,
  "total_terms": 304,
  "term_count": 3,
  "bm25_k1": 0.9,
  "bm25_b": 0.4,
  "files": {
    "seg0.dict": {
      "size": 123,
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
    },
    "seg0.skip": {
      "size": 44,
      "crc32c": %d
    }
  }
}
`, crc("seg0.dict"), crc("seg0.ids"), crc("seg0.lens"), crc("seg0.post"), crc("seg0.skip"))

	gotManifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if string(gotManifest) != wantManifest {
		t.Errorf("manifest.json =\n%s\nwant\n%s", gotManifest, wantManifest)
	}
}

// TestWriteGoldenBlocks checks a term with two blocks: 130 documents "d", each one
// word long, so every posting scores idf(130, 130) * 1 / (1 + 0.9) and the second
// block starts after 128 two-byte postings.
func TestWriteGoldenBlocks(t *testing.T) {
	ix := New()
	for i := range 130 {
		ix.Add(fmt.Sprint(i), []string{"d"})
	}
	dir := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(dir); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	version := []byte{2, 0, 0, 0}
	maxD := []byte{0x35, 0xE7, 0x03, 0x3B} // 0.0020126824
	want := map[string][]byte{
		"seg0.dict": concat(
			[]byte("QDCT"), version,
			[]byte{1, 0, 0, 0},
			// termOffset 0, termLen 1, docFreq 130, postingsOffset 8, postingsLen 260, skipOffset 8, maxScore
			[]byte{0, 0, 0, 0, 1, 0, 0, 0, 130, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 4, 1, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0}, maxD,
			[]byte("d"),
		),
		// Doc 0 with tf 1, then 129 gaps of 1 with tf 1: one stream across both blocks.
		"seg0.post": concat([]byte("QPST"), version, []byte{0, 1}, bytes.Repeat([]byte{1, 1}, 129)),
		"seg0.skip": concat(
			[]byte("QSKP"), version,
			[]byte{127, 0, 0, 0, 0, 0, 0, 0}, maxD, // block 0: docs 0..127 at offset 0
			[]byte{129, 0, 0, 0, 0, 1, 0, 0}, maxD, // block 1: docs 128..129 at offset 256
		),
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
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}
