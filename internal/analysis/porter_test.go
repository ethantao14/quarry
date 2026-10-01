package analysis

import (
	"bufio"
	"compress/gzip"
	"os"
	"strings"
	"testing"
)

// TestPorterVocabulary checks every word in Martin Porter's published test
// vocabulary (tartarus.org/martin/PorterStemmer: voc.txt and output.txt).
func TestPorterVocabulary(t *testing.T) {
	file, err := os.Open("testdata/porter_vocabulary.tsv.gz")
	if err != nil {
		t.Fatalf("open vocabulary: %v", err)
	}
	defer func() { _ = file.Close() }()
	decompressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("decompress vocabulary: %v", err)
	}

	words := 0
	scanner := bufio.NewScanner(decompressed)
	for scanner.Scan() {
		word, want, found := strings.Cut(scanner.Text(), "\t")
		if !found {
			t.Fatalf("line %d has no tab: %q", words+1, scanner.Text())
		}
		if got := porterStem(word); got != want {
			t.Errorf("porterStem(%q) = %q, want %q", word, got, want)
		}
		words++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read vocabulary: %v", err)
	}
	if words != 23531 {
		t.Errorf("checked %d words, want 23531", words)
	}
}
