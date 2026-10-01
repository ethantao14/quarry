package query

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/scoring"
)

// Searching a saved index must give exactly the in-memory results: same doc IDs, same scores.
func TestDiskMatchesMemory(t *testing.T) {
	vocabulary := []string{"red", "blue", "green", "fish", "bird", "car", "a", "ab", "abc",
		"zebra", "café", "猫", "naïve", "x1", "y"}
	queryVocabulary := append(slices.Clone(vocabulary), "missing")
	bm25 := scoring.DefaultBM25()

	for seed := uint64(0); seed < 200; seed++ {
		random := rand.New(rand.NewPCG(seed, 7))
		memory := index.New()
		for doc, count := 0, random.IntN(41); doc < count; doc++ {
			terms := make([]string, random.IntN(31))
			for i := range terms {
				terms[i] = vocabulary[random.IntN(len(vocabulary))]
			}
			memory.Add(fmt.Sprintf("doc-%d", doc), terms)
		}
		dir := filepath.Join(t.TempDir(), "idx")
		if err := memory.Write(dir); err != nil {
			t.Fatalf("seed %d: Write() error = %v", seed, err)
		}
		disk, err := index.Open(dir)
		if err != nil {
			t.Fatalf("seed %d: Open() error = %v", seed, err)
		}
		t.Cleanup(func() {
			if err := disk.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})

		for queryNumber := 0; queryNumber < 20; queryNumber++ {
			terms := make([]string, random.IntN(5)+1)
			for i := range terms {
				terms[i] = queryVocabulary[random.IntN(len(queryVocabulary))]
			}
			for _, k := range []int{1, 3, 10, 1000} {
				want, err := Exhaustive(memory, bm25, terms, k)
				if err != nil {
					t.Fatal(err)
				}
				got, err := Exhaustive(disk, bm25, terms, k)
				if err != nil {
					t.Fatalf("seed %d: Exhaustive(disk) error = %v", seed, err)
				}
				// Result holds a float64, so slices.Equal compares scores exactly.
				if !slices.Equal(got, want) {
					t.Fatalf("seed %d: Exhaustive(%q, %d) on disk = %v, in memory = %v", seed, terms, k, got, want)
				}
			}
		}
	}
}
