package eval

import (
	"math"
	"slices"
	"testing"
)

// Expected values below were worked out by hand and cross-checked with a
// separate script, not with this package.

func TestSortLikeTrecEval(t *testing.T) {
	entries := []RunEntry{
		{DocID: "d1", Score: 1.5},
		{DocID: "d10", Score: 1.5},
		{DocID: "d3", Score: 2.0},
		{DocID: "d2", Score: 1.5},
	}
	SortLikeTrecEval(entries)

	// Ties sort by string, descending: "d2" > "d10" > "d1".
	want := []RunEntry{
		{DocID: "d3", Score: 2.0},
		{DocID: "d2", Score: 1.5},
		{DocID: "d10", Score: 1.5},
		{DocID: "d1", Score: 1.5},
	}
	if !slices.Equal(entries, want) {
		t.Errorf("SortLikeTrecEval() = %v, want %v", entries, want)
	}
}

func TestMetricsGolden(t *testing.T) {
	judgments := map[string]int{"a": 2, "b": 1, "c": 1, "z": 0}
	ranked := []string{"x", "a", "b"}

	// nDCG@3: DCG = 2/log2(3) + 1/log2(4); ideal = 2/log2(2) + 1/log2(3) + 1/log2(4).
	// nDCG@2: DCG = 2/log2(3); ideal = 2/log2(2) + 1/log2(3).
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{name: "nDCG@3", got: NDCG(ranked, judgments, 3), want: 0.5627272554},
		{name: "nDCG@2", got: NDCG(ranked, judgments, 2), want: 0.4796249331},
		{name: "nDCG@10 past end of ranking", got: NDCG(ranked, judgments, 10), want: 0.5627272554},
		{name: "nDCG@1 no relevant in top 1", got: NDCG(ranked, judgments, 1), want: 0},
		{name: "Recall@3 finds 2 of 3 relevant", got: Recall(ranked, judgments, 3), want: 2.0 / 3.0},
		{name: "Recall@1", got: Recall(ranked, judgments, 1), want: 0},
		{name: "reciprocal rank@3, first relevant at rank 2", got: ReciprocalRank(ranked, judgments, 3), want: 0.5},
		{name: "reciprocal rank@1", got: ReciprocalRank(ranked, judgments, 1), want: 0},
		{name: "grade 0 is not relevant", got: Recall([]string{"z"}, judgments, 1), want: 0},
		{name: "empty ranking nDCG", got: NDCG(nil, judgments, 10), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if math.Abs(tt.got-tt.want) > 1e-9 {
				t.Errorf("got %.10f, want %.10f", tt.got, tt.want)
			}
		})
	}
}

func TestEvaluateAveragesOverAllJudgedQueries(t *testing.T) {
	qrels := Qrels{
		"q1": {"a": 1},
		"q2": {"b": 1},
	}
	// q2 has no results, so it counts as 0, like trec_eval -c.
	// q3 and q4 are not judged, so they are ignored.
	run := Run{
		"q1": {{DocID: "a", Score: 3}},
		"q3": {{DocID: "a", Score: 3}},
		"q4": {{DocID: "a", Score: 3}},
	}
	got := Evaluate(run, qrels)

	if got.Queries != 2 {
		t.Errorf("Queries = %d, want 2", got.Queries)
	}
	for name, value := range map[string]float64{
		"NDCG10": got.NDCG10, "Recall100": got.Recall100, "Recall1000": got.Recall1000, "MRR10": got.MRR10,
	} {
		if math.Abs(value-0.5) > 1e-9 {
			t.Errorf("%s = %.10f, want 0.5", name, value)
		}
	}
}

func TestEvaluateUsesTrecEvalOrder(t *testing.T) {
	// Tied scores: trec_eval ranks "b" above "a", so the relevant doc "a" is at rank 2.
	qrels := Qrels{"q1": {"a": 1}}
	run := Run{"q1": {{DocID: "a", Score: 1}, {DocID: "b", Score: 1}}}

	got := Evaluate(run, qrels)
	if math.Abs(got.MRR10-0.5) > 1e-9 {
		t.Errorf("MRR10 = %.10f, want 0.5", got.MRR10)
	}
}
