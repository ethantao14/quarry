package query_test

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

func randomPrunedCorpus(seed uint64, count int) (*index.Index, [][]string) {
	random := rand.New(rand.NewPCG(seed, 83))
	vocabulary := make([]string, 20+random.IntN(41))
	for i := range vocabulary {
		vocabulary[i] = fmt.Sprintf("term%02d", i)
	}
	zipf := rand.NewZipf(random, 1.3, 1, uint64(len(vocabulary)-1))
	memory := index.New()
	var previous []string
	for doc := range count {
		terms := previous
		if doc == 0 || random.IntN(5) != 0 {
			terms = make([]string, 1+random.IntN(30))
			for i := range terms {
				terms[i] = vocabulary[zipf.Uint64()]
			}
		}
		memory.Add(fmt.Sprint(doc), terms)
		previous = terms
	}
	queries := make([][]string, 12)
	for i := range queries {
		terms := make([]string, 1+random.IntN(6))
		for j := range terms {
			if random.IntN(8) == 0 {
				terms[j] = "missing"
			} else {
				terms[j] = vocabulary[zipf.Uint64()]
			}
		}
		queries[i] = terms
	}
	queries = append(queries, []string{"term01", "term00", "missing", "term00"})
	return memory, queries
}

func savePrunedIndex(t testing.TB, memory *index.Index) *index.Disk {
	t.Helper()
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
	return disk
}

func assertPruned(t *testing.T, ix query.Index, terms []string, k int) []query.Result {
	t.Helper()
	bm25 := scoring.DefaultBM25()
	want, err := query.Exhaustive(ix, bm25, terms, k)
	if err != nil {
		t.Fatal(err)
	}
	for _, algo := range []string{"wand", "bmw"} {
		got, err := query.Search(algo, ix, bm25, terms, k)
		if err != nil {
			t.Fatal(err)
		}
		assertResults(t, algo, got, want)
	}
	return want
}

func assertResults(t *testing.T, algo string, got, want []query.Result) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, Exhaustive = %v", algo, got, want)
	}
	for i := range got {
		if math.Float64bits(got[i].Score) != math.Float64bits(want[i].Score) {
			t.Fatalf("%s score bits differ at result %d", algo, i)
		}
	}
}

func TestPrunedRandomized(t *testing.T) {
	for seed := uint64(0); seed < 300; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			random := rand.New(rand.NewPCG(seed, 17))
			// Seeds 0 and 1 pin the edge sizes: an empty corpus and the largest one.
			count := random.IntN(401)
			switch seed {
			case 0:
				count = 0
			case 1:
				count = 400
			}
			memory, queries := randomPrunedCorpus(seed, count)
			for _, ix := range []query.Index{memory, savePrunedIndex(t, memory)} {
				for _, terms := range queries {
					for _, k := range []int{1, 2, 5, 10, 100, 1000} {
						assertPruned(t, ix, terms, k)
					}
				}
			}
		})
	}
}

func TestPrunedTies(t *testing.T) {
	memory := index.New()
	for doc := range 400 {
		memory.Add(fmt.Sprint(doc), []string{"red", "red", "blue", "green"})
	}
	for _, ix := range []query.Index{memory, savePrunedIndex(t, memory)} {
		for _, k := range []int{1, 2, 5, 10, 100} {
			got := assertPruned(t, ix, []string{"red", "blue", "red", "green"}, k)
			for i, result := range got {
				if result.DocID != uint32(i) {
					t.Fatalf("tie result %d has doc ID %d", i, result.DocID)
				}
			}
		}
	}
}

func TestPrunedEmpty(t *testing.T) {
	memory := index.New()
	memory.Add("doc", []string{"term"})
	for _, tt := range []struct {
		name  string
		terms []string
		k     int
	}{
		{"zero k", []string{"term"}, 0},
		{"negative k", []string{"term"}, -1},
		{"empty query", nil, 10},
		{"absent term", []string{"missing"}, 10},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, ix := range []query.Index{memory, savePrunedIndex(t, memory)} {
				if got := assertPruned(t, ix, tt.terms, tt.k); len(got) != 0 {
					t.Fatalf("WAND() = %v, want empty", got)
				}
			}
		})
	}
}

func TestPrunedBM25Mismatch(t *testing.T) {
	memory := index.New()
	memory.Add("doc", []string{"term"})
	for _, tt := range []struct {
		name string
		bm25 scoring.BM25
	}{
		{"k1", scoring.BM25{K1: 1.2, B: 0.4}},
		{"b", scoring.BM25{K1: 0.9, B: 0.75}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, ix := range []query.Index{memory, savePrunedIndex(t, memory)} {
				for _, algo := range []string{"wand", "bmw"} {
					_, err := query.Search(algo, ix, tt.bm25, []string{"term"}, 1)
					want := "score bounds were computed with k1=0.9 b=0.4; pruning needs the same parameters"
					if err == nil || err.Error() != want {
						t.Fatalf("%s error = %v, want %q", algo, err, want)
					}
				}
				got, err := query.Exhaustive(ix, tt.bm25, []string{"term"}, 1)
				if err != nil || len(got) != 1 {
					t.Fatalf("Exhaustive() = %v, %v", got, err)
				}
			}
		})
	}
}

type advanceFailCursor struct {
	query.Cursor
	err error
}

func (c advanceFailCursor) Advance(uint32) error { return c.err }

type prunedErrorIndex struct {
	query.Index
	cursors map[string]query.Cursor
}

func (ix prunedErrorIndex) Cursor(term string) (query.Cursor, error) {
	return ix.cursors[term], nil
}

func TestPrunedCursorErrors(t *testing.T) {
	memory := index.New()
	memory.Add("a", []string{"high", "high", "low"})
	for doc := range 20 {
		memory.Add(fmt.Sprint(doc), []string{"low"})
	}
	memory.Add("b", []string{"high"})
	failure := errors.New("broken cursor")
	for _, algo := range []string{"wand", "bmw"} {
		for _, step := range []string{"open", "next", "advance"} {
			t.Run(algo+"/"+step, func(t *testing.T) {
				low, err := memory.Cursor("low")
				if err != nil {
					t.Fatal(err)
				}
				high, err := memory.Cursor("high")
				if err != nil {
					t.Fatal(err)
				}
				var ix query.Index
				switch step {
				case "open":
					ix = failingIndex{Index: memory, err: failure}
				case "next":
					ix = failingIndex{Index: memory, cursor: failingCursor{Cursor: low, err: failure}}
				case "advance":
					ix = prunedErrorIndex{Index: memory, cursors: map[string]query.Cursor{
						"high": advanceFailCursor{Cursor: high, err: failure},
						"low":  advanceFailCursor{Cursor: low, err: failure},
					}}
				}
				_, err = query.Search(algo, ix, scoring.DefaultBM25(), []string{"low", "high"}, 1)
				if !errors.Is(err, failure) || !strings.Contains(err.Error(), "cursor for ") {
					t.Fatalf("%s error = %v", algo, err)
				}
				if step == "advance" {
					term := "low"
					if algo == "bmw" {
						term = "high"
					}
					if !strings.Contains(err.Error(), fmt.Sprintf("%q", term)) {
						t.Fatalf("%s error = %v, want %s term", algo, err, term)
					}
				}
			})
		}
	}
}

func BenchmarkBMW(b *testing.B) {
	benchmarkSearch(b, query.BMW)
}

func BenchmarkWAND(b *testing.B) {
	benchmarkSearch(b, query.WAND)
}

func BenchmarkExhaustive(b *testing.B) {
	benchmarkSearch(b, query.Exhaustive)
}

func benchmarkSearch(b *testing.B, search func(query.Index, scoring.BM25, []string, int) ([]query.Result, error)) {
	memory, queries := randomPrunedCorpus(42, 10000)
	disk := savePrunedIndex(b, memory)
	bm25 := scoring.DefaultBM25()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := search(disk, bm25, queries[i%len(queries)], 10); err != nil {
			b.Fatal(err)
		}
	}
}

type countingIndex struct {
	query.Index
	commonNext int
}

func (ix *countingIndex) Cursor(term string) (query.Cursor, error) {
	c, err := ix.Index.Cursor(term)
	if err != nil || c == nil || term != "common" {
		return c, err
	}
	return &countingCursor{Cursor: c, calls: &ix.commonNext}, nil
}

type countingCursor struct {
	query.Cursor
	calls *int
}

func (c *countingCursor) Next() error {
	*c.calls += 1
	return c.Cursor.Next()
}

func TestBMWSkipsBlocks(t *testing.T) {
	memory := index.New()
	memory.Add("first", strings.Fields(strings.Repeat("common ", 20)))
	for doc := range 4096 {
		memory.Add(fmt.Sprint(doc), strings.Fields("common "+strings.Repeat("filler ", 20)))
	}
	memory.Add("winner", []string{"rare", "common"})
	terms := []string{"rare", "common"}
	for _, ix := range []query.Index{memory, savePrunedIndex(t, memory)} {
		want, err := query.Exhaustive(ix, scoring.DefaultBM25(), terms, 1)
		if err != nil {
			t.Fatal(err)
		}
		counts := make(map[string]int)
		for _, algo := range []string{"wand", "bmw"} {
			counted := &countingIndex{Index: ix}
			got, err := query.Search(algo, counted, scoring.DefaultBM25(), terms, 1)
			if err != nil {
				t.Fatal(err)
			}
			assertResults(t, algo, got, want)
			counts[algo] = counted.commonNext
		}
		if counts["bmw"] >= counts["wand"] {
			t.Fatalf("%T common Next calls: BMW = %d, WAND = %d", ix, counts["bmw"], counts["wand"])
		}
		t.Logf("%T common Next calls: BMW = %d, WAND = %d", ix, counts["bmw"], counts["wand"])
	}
}
