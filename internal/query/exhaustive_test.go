package query

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/scoring"
)

func TestExhaustive(t *testing.T) {
	ix := index.New()
	for i, text := range []string{"red red blue", "blue green", "red red blue", "green", ""} {
		ix.Add(fmt.Sprint(i), strings.Fields(text))
	}
	tests := []struct {
		name  string
		terms []string
		k     int
		want  []uint32
	}{
		{name: "single term", terms: []string{"green"}, k: 10, want: []uint32{3, 1}},
		{name: "multiple terms", terms: []string{"red", "blue"}, k: 10, want: []uint32{0, 2, 1}},
		{name: "missing term", terms: []string{"missing"}, k: 10},
		{name: "empty query", k: 10},
		{name: "k smaller than matches", terms: []string{"blue"}, k: 1, want: []uint32{1}},
		{name: "identical documents", terms: []string{"red"}, k: 10, want: []uint32{0, 2}},
		{name: "known and missing terms", terms: []string{"red", "missing"}, k: 10, want: []uint32{0, 2}},
		{name: "zero k", terms: []string{"red"}, k: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Exhaustive(ix, scoring.DefaultBM25(), tt.terms, tt.k)
			if err != nil {
				t.Fatal(err)
			}
			var got []uint32
			for _, result := range results {
				got = append(got, result.DocID)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Exhaustive(%q, %d) doc IDs = %v, want %v", tt.terms, tt.k, got, tt.want)
			}
		})
	}
}

func TestExhaustiveRepeatedTerm(t *testing.T) {
	ix := index.New()
	ix.Add("a", []string{"red", "red", "blue"})
	ix.Add("b", []string{"red"})
	single, err := Exhaustive(ix, scoring.DefaultBM25(), []string{"red"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := Exhaustive(ix, scoring.DefaultBM25(), []string{"red", "red"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated) != len(single) || len(single) != 2 {
		t.Fatalf("Exhaustive() lengths = %d, %d, want 2, 2", len(repeated), len(single))
	}
	for i, result := range repeated {
		want := Result{DocID: single[i].DocID, Score: 2 * single[i].Score}
		if result != want {
			t.Errorf("Exhaustive(repeated)[%d] = %v, want %v", i, result, want)
		}
	}
}

func TestExhaustiveBruteForce(t *testing.T) {
	random := rand.New(rand.NewPCG(14, 29))
	vocabulary := []string{"red", "blue", "green", "fish", "bird", "car"}
	docs := make([][]string, 20)
	ix := index.New()
	for i := range docs {
		length := random.IntN(15)
		for j := 0; j < length; j++ {
			docs[i] = append(docs[i], vocabulary[random.IntN(len(vocabulary))])
		}
		ix.Add(fmt.Sprint(i), docs[i])
	}
	queryVocabulary := append(slices.Clone(vocabulary), "missing")
	bm25 := scoring.DefaultBM25()
	for queryNumber := 0; queryNumber < 100; queryNumber++ {
		terms := make([]string, random.IntN(9))
		for i := range terms {
			terms[i] = queryVocabulary[random.IntN(len(queryVocabulary))]
		}
		for _, k := range []int{-1, 0, 1, 3, 10, 25} {
			got, err := Exhaustive(ix, bm25, terms, k)
			if err != nil {
				t.Fatal(err)
			}
			want := bruteForce(docs, bm25, terms, k)
			if len(got) != len(want) {
				t.Fatalf("Exhaustive(%q, %d) length = %d, want %d", terms, k, len(got), len(want))
			}
			for i := range got {
				if got[i].DocID != want[i].DocID || math.Abs(got[i].Score-want[i].Score) > 1e-9 {
					t.Errorf("Exhaustive(%q, %d)[%d] = %v, want %v", terms, k, i, got[i], want[i])
				}
			}
			reversed := slices.Clone(terms)
			slices.Reverse(reversed)
			reordered, err := Exhaustive(ix, bm25, reversed, k)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(reordered, got) {
				t.Errorf("Exhaustive(%q, %d) = %v, want %v", reversed, k, reordered, got)
			}
		}
	}
}

func bruteForce(docs [][]string, bm25 scoring.BM25, terms []string, k int) []Result {
	if k <= 0 {
		return nil
	}
	totalLen := 0
	for _, doc := range docs {
		totalLen += len(doc)
	}
	avgDocLen := float64(totalLen) / float64(len(docs))
	var results []Result
	for docID, doc := range docs {
		var score float64
		matched := false
		for _, term := range terms {
			var tf uint32
			for _, word := range doc {
				if word == term {
					tf++
				}
			}
			if tf == 0 {
				continue
			}
			matched = true
			docFreq := 0
			for _, other := range docs {
				if slices.Contains(other, term) {
					docFreq++
				}
			}
			score += bm25.TermScore(scoring.IDF(len(docs), docFreq), tf, uint32(len(doc)), avgDocLen)
		}
		if matched {
			results = append(results, Result{DocID: uint32(docID), Score: score})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].DocID < results[j].DocID
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > k {
		results = results[:k]
	}
	return results
}
