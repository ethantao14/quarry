package query

import (
	"slices"
	"testing"
)

func TestTopK(t *testing.T) {
	tests := []struct {
		name    string
		k       int
		offered []Result
		want    []Result
	}{
		{
			name:    "keeps best k",
			k:       2,
			offered: []Result{{DocID: 0, Score: 2}, {DocID: 1, Score: 4}, {DocID: 2, Score: 3}, {DocID: 3, Score: 1}},
			want:    []Result{{DocID: 1, Score: 4}, {DocID: 2, Score: 3}},
		},
		{
			name:    "ties prefer lower doc ID",
			k:       2,
			offered: []Result{{DocID: 3, Score: 1}, {DocID: 2, Score: 1}, {DocID: 0, Score: 1}, {DocID: 1, Score: 1}},
			want:    []Result{{DocID: 0, Score: 1}, {DocID: 1, Score: 1}},
		},
		{
			name:    "k larger than offers and sorted results",
			k:       10,
			offered: []Result{{DocID: 2, Score: 1}, {DocID: 1, Score: 3}, {DocID: 0, Score: 1}},
			want:    []Result{{DocID: 1, Score: 3}, {DocID: 0, Score: 1}, {DocID: 2, Score: 1}},
		},
		{name: "zero k", k: 0, offered: []Result{{DocID: 0, Score: 1}}},
		{name: "negative k", k: -1, offered: []Result{{DocID: 0, Score: 1}}},
		{name: "no offers", k: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			top := NewTopK(tt.k)
			for _, result := range tt.offered {
				top.Offer(result)
			}
			if got := top.Results(); !slices.Equal(got, tt.want) {
				t.Errorf("Results() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTopKOfferOrder(t *testing.T) {
	orders := []struct {
		name    string
		offered []Result
	}{
		{"forward", []Result{{DocID: 0, Score: 1}, {DocID: 1, Score: 2}, {DocID: 2, Score: 2}, {DocID: 3, Score: 3}}},
		{"reverse", []Result{{DocID: 3, Score: 3}, {DocID: 2, Score: 2}, {DocID: 1, Score: 2}, {DocID: 0, Score: 1}}},
		{"mixed", []Result{{DocID: 2, Score: 2}, {DocID: 0, Score: 1}, {DocID: 3, Score: 3}, {DocID: 1, Score: 2}}},
	}
	want := []Result{{DocID: 3, Score: 3}, {DocID: 1, Score: 2}}
	for _, tt := range orders {
		t.Run(tt.name, func(t *testing.T) {
			top := NewTopK(2)
			for _, result := range tt.offered {
				top.Offer(result)
			}
			if got := top.Results(); !slices.Equal(got, want) {
				t.Errorf("Results() = %v, want %v", got, want)
			}
		})
	}
}

func TestTopKResultsCopy(t *testing.T) {
	top := NewTopK(2)
	top.Offer(Result{DocID: 0, Score: 1})
	top.Offer(Result{DocID: 1, Score: 3})
	before := slices.Clone(top.results)
	results := top.Results()
	if !slices.Equal(top.results, before) {
		t.Fatalf("Results() heap = %v, want %v", top.results, before)
	}
	results[0] = Result{DocID: 9, Score: 99}
	top.Offer(Result{DocID: 2, Score: 2})
	want := []Result{{DocID: 1, Score: 3}, {DocID: 2, Score: 2}}
	if got := top.Results(); !slices.Equal(got, want) {
		t.Errorf("Results() = %v, want %v", got, want)
	}
}

func TestTopKThreshold(t *testing.T) {
	for _, tt := range []struct {
		name   string
		k      int
		scores []float64
		want   float64
		full   bool
	}{
		{"empty", 2, nil, 0, false},
		{"partial", 2, []float64{3}, 0, false},
		{"full", 2, []float64{3, 1}, 1, true},
		{"replacement", 2, []float64{3, 1, 2}, 2, true},
		{"rejected", 2, []float64{3, 2, 1}, 2, true},
		{"ties", 2, []float64{2, 2, 2}, 2, true},
		{"zero score", 1, []float64{0}, 0, true},
		{"zero k", 0, []float64{1}, 0, false},
		{"negative k", -1, []float64{1}, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			top := NewTopK(tt.k)
			for doc, score := range tt.scores {
				top.Offer(Result{DocID: uint32(doc), Score: score})
			}
			if got, full := top.Threshold(); got != tt.want || full != tt.full {
				t.Errorf("Threshold() = %g, %t, want %g, %t", got, full, tt.want, tt.full)
			}
		})
	}
}
