package index

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
)

// writeBenchIndex saves a synthetic index of 100,000 documents whose term
// frequencies follow a Zipf distribution, like natural text.
func writeBenchIndex(b *testing.B) string {
	b.Helper()
	random := rand.New(rand.NewPCG(1, 2))
	zipf := rand.NewZipf(random, 1.1, 1, 49999)
	ix := New()
	terms := make([]string, 0, 60)
	for doc := 0; doc < 100000; doc++ {
		terms = terms[:0]
		for i, length := 0, 20+random.IntN(40); i < length; i++ {
			terms = append(terms, fmt.Sprintf("t%d", zipf.Uint64()))
		}
		ix.Add(fmt.Sprintf("doc-%d", doc), terms)
	}
	dir := filepath.Join(b.TempDir(), "idx")
	if err := ix.Write(dir); err != nil {
		b.Fatal(err)
	}
	return dir
}

// BenchmarkOpen measures opening and validating a saved index.
// B/op is the heap memory each open allocates.
func BenchmarkOpen(b *testing.B) {
	dir := writeBenchIndex(b)
	b.ReportAllocs()
	for b.Loop() {
		disk, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		if disk.DocCount() != 100000 {
			b.Fatalf("DocCount() = %d", disk.DocCount())
		}
	}
}
