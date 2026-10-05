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

func randomWANDCorpus(seed uint64, count int) (*index.Index, [][]string) {
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

func saveWANDIndex(t testing.TB, memory *index.Index) *index.Disk {
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

func assertWAND(t *testing.T, ix query.Index, terms []string, k int) []query.Result {
	t.Helper()
	bm25 := scoring.DefaultBM25()
	want, err := query.Exhaustive(ix, bm25, terms, k)
	if err != nil {
		t.Fatal(err)
	}
	got, err := query.WAND(ix, bm25, terms, k)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%T WAND(%q, %d) = %v, Exhaustive = %v", ix, terms, k, got, want)
	}
	for i := range got {
		if math.Float64bits(got[i].Score) != math.Float64bits(want[i].Score) {
			t.Fatalf("score bits differ at result %d", i)
		}
	}
	return got
}

func TestWANDRandomized(t *testing.T) {
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
			memory, queries := randomWANDCorpus(seed, count)
			for _, ix := range []query.Index{memory, saveWANDIndex(t, memory)} {
				for _, terms := range queries {
					for _, k := range []int{1, 2, 5, 10, 100, 1000} {
						assertWAND(t, ix, terms, k)
					}
				}
			}
		})
	}
}

func TestWANDTies(t *testing.T) {
	memory := index.New()
	for doc := range 400 {
		memory.Add(fmt.Sprint(doc), []string{"red", "red", "blue", "green"})
	}
	for _, ix := range []query.Index{memory, saveWANDIndex(t, memory)} {
		for _, k := range []int{1, 2, 5, 10, 100} {
			got := assertWAND(t, ix, []string{"red", "blue", "red", "green"}, k)
			for i, result := range got {
				if result.DocID != uint32(i) {
					t.Fatalf("tie result %d has doc ID %d", i, result.DocID)
				}
			}
		}
	}
}

func TestWANDEmpty(t *testing.T) {
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
			for _, ix := range []query.Index{memory, saveWANDIndex(t, memory)} {
				if got := assertWAND(t, ix, tt.terms, tt.k); len(got) != 0 {
					t.Fatalf("WAND() = %v, want empty", got)
				}
			}
		})
	}
}

func TestWANDBM25Mismatch(t *testing.T) {
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
			for _, ix := range []query.Index{memory, saveWANDIndex(t, memory)} {
				_, err := query.WAND(ix, tt.bm25, []string{"term"}, 1)
				want := "score bounds were computed with k1=0.9 b=0.4; WAND needs the same parameters"
				if err == nil || err.Error() != want {
					t.Fatalf("WAND() error = %v, want %q", err, want)
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

type wandErrorIndex struct {
	query.Index
	cursors map[string]query.Cursor
}

func (ix wandErrorIndex) Cursor(term string) (query.Cursor, error) {
	return ix.cursors[term], nil
}

func TestWANDCursorErrors(t *testing.T) {
	memory := index.New()
	memory.Add("a", []string{"high", "high", "low"})
	for doc := range 20 {
		memory.Add(fmt.Sprint(doc), []string{"low"})
	}
	memory.Add("b", []string{"high"})
	failure := errors.New("broken cursor")
	for _, step := range []string{"open", "next", "advance"} {
		t.Run(step, func(t *testing.T) {
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
				ix = wandErrorIndex{Index: memory, cursors: map[string]query.Cursor{
					"high": high, "low": advanceFailCursor{Cursor: low, err: failure},
				}}
			}
			_, err = query.WAND(ix, scoring.DefaultBM25(), []string{"low", "high"}, 1)
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), "cursor for ") {
				t.Fatalf("WAND() error = %v", err)
			}
			if step == "advance" && !strings.Contains(err.Error(), `"low"`) {
				t.Fatalf("WAND() error = %v, want low term", err)
			}
		})
	}
}

func BenchmarkWAND(b *testing.B) {
	benchmarkSearch(b, query.WAND)
}

func BenchmarkExhaustive(b *testing.B) {
	benchmarkSearch(b, query.Exhaustive)
}

func benchmarkSearch(b *testing.B, search func(query.Index, scoring.BM25, []string, int) ([]query.Result, error)) {
	memory, queries := randomWANDCorpus(42, 10000)
	disk := saveWANDIndex(b, memory)
	bm25 := scoring.DefaultBM25()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := search(disk, bm25, queries[i%len(queries)], 10); err != nil {
			b.Fatal(err)
		}
	}
}
