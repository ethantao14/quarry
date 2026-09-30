package eval

import (
	"math"
	"slices"
	"sort"
)

// NDCG returns normalized discounted cumulative gain at k, using linear grades.
// Ranked document IDs are assumed to be unique.
func NDCG(ranked []string, judgments map[string]int, k int) float64 {
	if k <= 0 {
		return 0
	}
	var dcg float64
	for i, docID := range ranked[:min(k, len(ranked))] {
		if grade := judgments[docID]; grade >= 1 {
			dcg += float64(grade) / math.Log2(float64(i+2))
		}
	}
	var grades []int
	for _, grade := range judgments {
		if grade >= 1 {
			grades = append(grades, grade)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(grades)))
	var ideal float64
	for i, grade := range grades[:min(k, len(grades))] {
		ideal += float64(grade) / math.Log2(float64(i+2))
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

// Recall returns the fraction of relevant documents found in the first k results.
// Ranked document IDs are assumed to be unique.
func Recall(ranked []string, judgments map[string]int, k int) float64 {
	if k <= 0 {
		return 0
	}
	total := 0
	for _, grade := range judgments {
		if grade >= 1 {
			total++
		}
	}
	if total == 0 {
		return 0
	}
	found := 0
	for _, docID := range ranked[:min(k, len(ranked))] {
		if judgments[docID] >= 1 {
			found++
		}
	}
	return float64(found) / float64(total)
}

// ReciprocalRank returns the reciprocal of the first relevant rank within k.
func ReciprocalRank(ranked []string, judgments map[string]int, k int) float64 {
	if k <= 0 {
		return 0
	}
	for i, docID := range ranked[:min(k, len(ranked))] {
		if judgments[docID] >= 1 {
			return 1 / float64(i+1)
		}
	}
	return 0
}

// Summary holds retrieval metrics averaged over all judged queries.
type Summary struct {
	// Queries is the number of judged queries, including those with no results.
	Queries int
	// NDCG10 is mean normalized discounted cumulative gain at 10.
	NDCG10 float64
	// Recall100 is mean recall at 100.
	Recall100 float64
	// Recall1000 is mean recall at 1000.
	Recall1000 float64
	// MRR10 is mean reciprocal rank at 10.
	MRR10 float64
}

// Evaluate scores each judged query in trec_eval order without modifying run.
func Evaluate(run Run, qrels Qrels) Summary {
	summary := Summary{Queries: len(qrels)}
	if summary.Queries == 0 {
		return summary
	}
	queryIDs := make([]string, 0, len(qrels))
	for queryID := range qrels {
		queryIDs = append(queryIDs, queryID)
	}
	sort.Strings(queryIDs)
	for _, queryID := range queryIDs {
		entries := slices.Clone(run[queryID])
		SortLikeTrecEval(entries)
		ranked := make([]string, len(entries))
		for i, entry := range entries {
			ranked[i] = entry.DocID
		}
		judgments := qrels[queryID]
		summary.NDCG10 += NDCG(ranked, judgments, 10)
		summary.Recall100 += Recall(ranked, judgments, 100)
		summary.Recall1000 += Recall(ranked, judgments, 1000)
		summary.MRR10 += ReciprocalRank(ranked, judgments, 10)
	}
	count := float64(summary.Queries)
	summary.NDCG10 /= count
	summary.Recall100 /= count
	summary.Recall1000 /= count
	summary.MRR10 /= count
	return summary
}
