package query_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

func TestDiskExhaustiveAcrossBlocks(t *testing.T) {
	vocabulary := []string{"a", "b", "common", "rare", "猫"}
	for seed := uint64(0); seed < 10; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			random := rand.New(rand.NewPCG(seed, 42))
			memory := index.New()
			for doc, count := 0, 300+random.IntN(200); doc < count; doc++ {
				terms := []string{"common"}
				for range random.IntN(50) {
					terms = append(terms, vocabulary[random.IntN(len(vocabulary))])
				}
				memory.Add(fmt.Sprint(doc), terms)
			}
			dir := filepath.Join(t.TempDir(), "idx")
			if err := memory.Write(dir); err != nil {
				t.Fatal(err)
			}
			disk, err := index.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := disk.Close(); err != nil {
					t.Error(err)
				}
			})
			for range 30 {
				terms := []string{"common", "common", "missing"}
				for range random.IntN(15) {
					terms = append(terms, vocabulary[random.IntN(len(vocabulary))])
				}
				for _, bm25 := range []scoring.BM25{scoring.DefaultBM25(), {K1: 1.2, B: 0.75}} {
					for _, k := range []int{1, 10, 1000} {
						want := exhaustiveLists(t, memory, bm25, terms, k)
						for _, ix := range []query.Index{memory, disk} {
							got, err := query.Exhaustive(ix, bm25, terms, k)
							if err != nil {
								t.Fatal(err)
							}
							if !slices.Equal(got, want) {
								t.Fatalf("Exhaustive(%v, %d) differs from full-list scoring", terms, k)
							}
						}
					}
				}
			}
		})
	}
}

// exhaustiveLists preserves the full-list scorer's sorted-term summation order.
func exhaustiveLists(t *testing.T, ix *index.Index, bm25 scoring.BM25, terms []string, k int) []query.Result {
	t.Helper()
	counts := make(map[string]int)
	for _, term := range terms {
		counts[term]++
	}
	ordered := make([]string, 0, len(counts))
	for term := range counts {
		ordered = append(ordered, term)
	}
	sort.Strings(ordered)
	scores := make(map[uint32]float64)
	for _, term := range ordered {
		list, err := ix.Postings(term)
		if err != nil {
			t.Fatal(err)
		}
		idf := scoring.IDF(ix.DocCount(), len(list))
		for _, p := range list {
			scores[p.DocID] += float64(counts[term]) * bm25.TermScore(idf, p.TF, ix.DocLen(p.DocID), ix.AvgDocLen())
		}
	}
	var results []query.Result
	for doc, score := range scores {
		results = append(results, query.Result{DocID: doc, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].DocID < results[j].DocID
		}
		return results[i].Score > results[j].Score
	})
	return results[:min(k, len(results))]
}

type failingIndex struct {
	query.Index
	cursor query.Cursor
	err    error
}

func (ix failingIndex) Cursor(string) (query.Cursor, error) { return ix.cursor, ix.err }

type failingCursor struct {
	query.Cursor
	err error
}

func (c failingCursor) Next() error { return c.err }

func TestExhaustiveCursorErrors(t *testing.T) {
	memory := index.New()
	memory.Add("doc", []string{"term"})
	c, err := memory.Cursor("term")
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("broken cursor")
	for _, tt := range []struct {
		name string
		ix   query.Index
	}{
		{"open", failingIndex{Index: memory, err: failure}},
		{"next", failingIndex{Index: memory, cursor: failingCursor{Cursor: c, err: failure}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := query.Exhaustive(tt.ix, scoring.DefaultBM25(), []string{"term"}, 1)
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), `"term"`) {
				t.Fatalf("Exhaustive() error = %v", err)
			}
		})
	}
}
