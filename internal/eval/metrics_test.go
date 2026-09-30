package eval

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestMetricsEdges(t *testing.T) {
	tests := []struct {
		name      string
		ranked    []string
		judgments map[string]int
		k         int
		want      [3]float64
	}{
		{name: "zero k", ranked: []string{"a"}, judgments: map[string]int{"a": 1}, k: 0},
		{name: "negative k", ranked: []string{"a"}, judgments: map[string]int{"a": 1}, k: -1},
		{name: "no judgments", ranked: []string{"a"}, k: 10},
		{name: "no relevant judgments", ranked: []string{"a", "b", "x"}, judgments: map[string]int{"a": 0, "b": -1}, k: 10},
		{name: "no results", judgments: map[string]int{"a": 1}, k: 10},
		{name: "only unjudged results", ranked: []string{"x"}, judgments: map[string]int{"a": 1}, k: 10},
		{
			name:      "ideal graded ranking",
			ranked:    []string{"a", "b", "c", "x"},
			judgments: map[string]int{"a": 3, "b": 2, "c": 1, "z": 0},
			k:         10,
			want:      [3]float64{1, 1, 1},
		},
		{
			name:      "ideal truncated ranking",
			ranked:    []string{"a", "b", "c"},
			judgments: map[string]int{"a": 3, "b": 2, "c": 1},
			k:         2,
			want:      [3]float64{1, 2.0 / 3.0, 1},
		},
		{
			name:      "negative grade contributes no gain",
			ranked:    []string{"z", "a"},
			judgments: map[string]int{"z": -2, "a": 1},
			k:         2,
			want:      [3]float64{1 / math.Log2(3), 1, 0.5},
		},
	}
	metrics := []struct {
		name string
		fn   func([]string, map[string]int, int) float64
	}{
		{name: "NDCG", fn: NDCG},
		{name: "Recall", fn: Recall},
		{name: "ReciprocalRank", fn: ReciprocalRank},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, metric := range metrics {
				t.Run(metric.name, func(t *testing.T) {
					got := metric.fn(tt.ranked, tt.judgments, tt.k)
					if math.IsNaN(got) || math.Abs(got-tt.want[i]) > 1e-9 {
						t.Errorf("%s(%v, %v, %d) = %.10f, want %.10f", metric.name, tt.ranked, tt.judgments, tt.k, got, tt.want[i])
					}
				})
			}
		})
	}
}

func TestEvaluateEdges(t *testing.T) {
	longRun := make([]RunEntry, 1001)
	for i := range longRun {
		longRun[i] = RunEntry{DocID: fmt.Sprint(i + 1), Score: float64(1001 - i)}
	}
	tests := []struct {
		name  string
		run   Run
		qrels Qrels
		want  Summary
	}{
		{name: "empty qrels", run: Run{"q1": {{DocID: "a", Score: 1}}}, qrels: Qrels{}},
		{name: "nil inputs"},
		{name: "empty run", qrels: Qrels{"q1": {"a": 1}}, want: Summary{Queries: 1}},
		{name: "no relevant judgments", run: Run{"q1": {{DocID: "a", Score: 1}}}, qrels: Qrels{"q1": {"a": 0}}, want: Summary{Queries: 1}},
		{
			name:  "sorts a copy",
			run:   Run{"q1": {{DocID: "c", Score: 1}, {DocID: "a", Score: 2}, {DocID: "b", Score: 2}}},
			qrels: Qrels{"q1": {"b": 1}},
			want:  Summary{Queries: 1, NDCG10: 1, Recall100: 1, Recall1000: 1, MRR10: 1},
		},
		{
			name:  "metric cutoffs",
			run:   Run{"q1": longRun},
			qrels: Qrels{"q1": {"11": 1, "101": 1, "1001": 1}},
			want:  Summary{Queries: 1, Recall100: 1.0 / 3.0, Recall1000: 2.0 / 3.0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var before Run
			if tt.run != nil {
				before = make(Run)
				for queryID, entries := range tt.run {
					before[queryID] = slices.Clone(entries)
				}
			}
			if got := Evaluate(tt.run, tt.qrels); got != tt.want {
				t.Errorf("Evaluate() = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.run, before) {
				t.Errorf("Evaluate() run = %v, want %v", tt.run, before)
			}
		})
	}
}
