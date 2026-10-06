package query

import (
	"cmp"
	"fmt"
	"slices"
	"sort"

	"github.com/ethantao14/quarry/internal/scoring"
)

type wandCursor struct {
	cursor
	rank  int
	upper float64
}

// WAND prunes by term score bounds and returns the same best k as Exhaustive.
func WAND(ix Index, bm25 scoring.BM25, queryTerms []string, k int) ([]Result, error) {
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
		if err := wandStep(ix, bm25, cursors, pivotDoc, top); err != nil {
			return nil, err
		}
	}
	return top.Results(), nil
}

func checkBM25(ix Index, bm25 scoring.BM25) error {
	if bounds := ix.BM25(); bounds != bm25 {
		return fmt.Errorf("score bounds were computed with k1=%g b=%g; pruning needs the same parameters", bounds.K1, bounds.B)
	}
	return nil
}

func newWANDCursors(ix Index, queryTerms []string) ([]wandCursor, error) {
	counts := make(map[string]int)
	for _, term := range queryTerms {
		counts[term]++
	}
	terms := make([]string, 0, len(counts))
	for term := range counts {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	cursors := make([]wandCursor, 0, len(terms))
	for rank, term := range terms {
		c, err := ix.Cursor(term)
		if err != nil {
			return nil, fmt.Errorf("cursor for %q: %w", term, err)
		}
		if c == nil {
			continue
		}
		cursors = append(cursors, wandCursor{
			cursor: cursor{
				Cursor: c,
				term:   term,
				count:  counts[term],
				idf:    scoring.IDF(ix.DocCount(), c.DocFreq()),
				doc:    c.DocID(),
			},
			rank:  rank,
			upper: float64(counts[term]) * float64(c.MaxScore()),
		})
	}
	return cursors, nil
}

func reorder(cursors []wandCursor) []wandCursor {
	slices.SortFunc(cursors, func(a, b wandCursor) int {
		if a.doc == b.doc {
			return cmp.Compare(a.rank, b.rank)
		}
		return cmp.Compare(a.doc, b.doc)
	})
	for len(cursors) > 0 && cursors[len(cursors)-1].doc == NoMoreDocs {
		cursors = cursors[:len(cursors)-1]
	}
	return cursors
}

func findPivot(cursors []wandCursor, top *TopK) int {
	threshold, full := top.Threshold()
	var bound float64
	for i := range cursors {
		bound += cursors[i].upper
		// Documents arrive in increasing ID order; ties favor lower IDs, so later scores must be higher.
		// The 1e-9 margin covers summing bounds in a different order than scores.
		// It can only cause extra scoring, never a skip.
		if !full || bound*(1+1e-9) > threshold {
			return i
		}
	}
	return -1
}

func scoreDoc(ix Index, bm25 scoring.BM25, cursors []wandCursor, doc uint32) float64 {
	var score float64
	// Equal doc IDs are ordered by rank, matching Exhaustive's summation order.
	for i := range cursors {
		c := &cursors[i]
		if c.doc != doc {
			break
		}
		score += float64(c.count) * bm25.TermScore(c.idf, c.TF(), ix.DocLen(doc), ix.AvgDocLen())
	}
	return score
}

func advanceCandidate(cursors []wandCursor, pivotDoc uint32) *wandCursor {
	best := &cursors[0]
	for i := range cursors {
		c := &cursors[i]
		if c.doc == pivotDoc {
			break
		}
		if c.upper > best.upper || (c.upper == best.upper && c.rank < best.rank) {
			best = c
		}
	}
	return best
}

func wandStep(ix Index, bm25 scoring.BM25, cursors []wandCursor, pivotDoc uint32, top *TopK) error {
	if cursors[0].doc == pivotDoc {
		top.Offer(Result{DocID: pivotDoc, Score: scoreDoc(ix, bm25, cursors, pivotDoc)})
		for i := range cursors {
			c := &cursors[i]
			if c.doc != pivotDoc {
				break
			}
			if err := c.Next(); err != nil {
				return fmt.Errorf("cursor for %q: %w", c.term, err)
			}
			c.doc = c.DocID()
		}
	} else {
		c := advanceCandidate(cursors, pivotDoc)
		if err := c.Advance(pivotDoc); err != nil {
			return fmt.Errorf("cursor for %q: %w", c.term, err)
		}
		c.doc = c.DocID()
	}
	return nil
}
