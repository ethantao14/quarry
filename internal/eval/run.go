// Package eval scores ranked results against relevance judgments, matching trec_eval.
package eval

import "sort"

// RunEntry is one retrieved document for a query, as written in a TREC run file.
type RunEntry struct {
	DocID string
	Score float64
}

// Run maps each query ID to its retrieved documents.
type Run map[string][]RunEntry

// Qrels maps each query ID to its judged documents and their relevance grades.
type Qrels map[string]map[string]int

// SortLikeTrecEval orders entries the way trec_eval does before scoring:
// higher score first, ties broken by doc ID in descending string order.
// trec_eval ignores the rank column, so this order is what counts.
func SortLikeTrecEval(entries []RunEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].DocID > entries[j].DocID
	})
}
