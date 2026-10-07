package query

import (
	"fmt"

	"github.com/ethantao14/quarry/internal/scoring"
)

// BMW prunes by block score bounds and returns the same best k as Exhaustive.
func BMW(ix Index, bm25 scoring.BM25, queryTerms []string, k int) ([]Result, error) {
	if err := checkBM25(ix, bm25); err != nil {
		return nil, err
	}
	top := NewTopK(k)
	if k <= 0 {
		return top.Results(), nil
	}
	cursors, err := newWANDCursors(ix, queryTerms)
	if err != nil {
		return nil, err
	}
	for {
		cursors = reorder(cursors)
		pivot := findPivot(cursors, top)
		if pivot < 0 {
			break
		}
		pivotDoc := cursors[pivot].doc
		for pivot+1 < len(cursors) && cursors[pivot+1].doc == pivotDoc {
			pivot++
		}
		if threshold, full := top.Threshold(); full {
			bound, next := blockBound(cursors, pivot)
			if !(bound*(1+1e-9) > threshold) {
				// WAND excludes docs before pivotDoc. In [pivotDoc, next), only terms
				// through pivot can contribute, each within its current block.
				// Their summed bounds cannot beat the threshold, so skipping is exact.
				c := advanceCandidate(cursors[:pivot+1], NoMoreDocs)
				if err := c.Advance(next); err != nil {
					return nil, fmt.Errorf("cursor for %q: %w", c.term, err)
				}
				c.doc = c.DocID()
				continue
			}
		}
		if err := wandStep(ix, bm25, cursors, pivotDoc, top); err != nil {
			return nil, err
		}
	}
	return top.Results(), nil
}

func blockBound(cursors []*wandCursor, pivot int) (float64, uint32) {
	var bound float64
	next := uint64(NoMoreDocs)
	for i := 0; i <= pivot; i++ {
		c := cursors[i]
		c.ShallowAdvance(cursors[pivot].doc)
		lastDoc, score := c.BlockMax()
		bound += float64(c.count) * float64(score)
		next = min(next, uint64(lastDoc)+1)
	}
	if pivot+1 < len(cursors) {
		next = min(next, uint64(cursors[pivot+1].doc))
	}
	return bound, uint32(next)
}
