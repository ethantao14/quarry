// Package query ranks documents matching query terms.
package query

import (
	"container/heap"
	"sort"
)

// Result holds a document's internal ID and relevance score.
type Result struct {
	DocID uint32
	Score float64
}

// better reports whether a ranks above b: higher score first, then lower doc ID.
func better(a, b Result) bool {
	if a.Score == b.Score {
		return a.DocID < b.DocID
	}
	return a.Score > b.Score
}

type resultHeap []Result

// Len returns the number of results in the heap.
func (h resultHeap) Len() int {
	return len(h)
}

// Less orders the worst result first.
func (h resultHeap) Less(i, j int) bool {
	return better(h[j], h[i])
}

// Swap exchanges two results in the heap.
func (h resultHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Push appends a result for container/heap.
func (h *resultHeap) Push(value any) {
	*h = append(*h, value.(Result))
}

// Pop removes the last result for container/heap.
func (h *resultHeap) Pop() any {
	last := len(*h) - 1
	result := (*h)[last]
	*h = (*h)[:last]
	return result
}

// TopK keeps the best k results, with the worst kept result at the heap root.
type TopK struct {
	k       int
	results resultHeap
}

// NewTopK returns a collector that keeps at most k results.
func NewTopK(k int) *TopK {
	return &TopK{k: k}
}

// Offer keeps r if it belongs among the best k results seen so far.
func (t *TopK) Offer(r Result) {
	if t.k <= 0 {
		return
	}
	if len(t.results) < t.k {
		heap.Push(&t.results, r)
	} else if better(r, t.results[0]) {
		t.results[0] = r
		heap.Fix(&t.results, 0)
	}
}

// Results returns a new slice sorted by descending score, then ascending DocID.
func (t *TopK) Results() []Result {
	results := make([]Result, len(t.results))
	copy(results, t.results)
	sort.Slice(results, func(i, j int) bool {
		return better(results[i], results[j])
	})
	return results
}
