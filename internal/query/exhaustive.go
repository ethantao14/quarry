package query

import (
	"fmt"
	"sort"

	"github.com/ethantao14/quarry/internal/scoring"
)

// Index is what query processing needs from an index, in memory or on disk.
type Index interface {
	Cursor(term string) (Cursor, error)
	DocCount() int
	DocLen(docID uint32) uint32
	AvgDocLen() float64
}

// cursor is one query term's state. doc caches Cursor.DocID(), so the loop over
// all terms reads a field instead of calling through the interface.
type cursor struct {
	Cursor
	term  string
	count int
	idf   float64
	doc   uint32
}

// Exhaustive scores every matching document and returns the best k results.
func Exhaustive(ix Index, bm25 scoring.BM25, queryTerms []string, k int) ([]Result, error) {
	top := NewTopK(k)
	if k <= 0 {
		return top.Results(), nil
	}
	counts := make(map[string]int)
	for _, term := range queryTerms {
		counts[term]++
	}
	terms := make([]string, 0, len(counts))
	for term := range counts {
		terms = append(terms, term)
	}
	// Sorted terms make floating point summation deterministic.
	sort.Strings(terms)

	var cursors []cursor
	for _, term := range terms {
		termCursor, err := ix.Cursor(term)
		if err != nil {
			return nil, fmt.Errorf("cursor for %q: %w", term, err)
		}
		if termCursor == nil {
			continue
		}
		cursors = append(cursors, cursor{
			Cursor: termCursor,
			term:   term,
			count:  counts[term],
			idf:    scoring.IDF(ix.DocCount(), termCursor.DocFreq()),
			doc:    termCursor.DocID(),
		})
	}

	for {
		docID, found := nextDocID(cursors)
		if !found {
			break
		}

		var score float64
		for i := range cursors {
			c := &cursors[i]
			if c.doc != docID {
				continue
			}
			score += float64(c.count) * bm25.TermScore(c.idf, c.TF(), ix.DocLen(docID), ix.AvgDocLen())
			if err := c.Next(); err != nil {
				return nil, fmt.Errorf("cursor for %q: %w", c.term, err)
			}
			c.doc = c.DocID()
		}
		top.Offer(Result{DocID: docID, Score: score})
	}
	return top.Results(), nil
}

// nextDocID returns the smallest doc ID any cursor is on, or false if all are exhausted.
func nextDocID(cursors []cursor) (uint32, bool) {
	var smallest uint32
	found := false
	for i := range cursors {
		current := cursors[i].doc
		if current == NoMoreDocs {
			continue
		}
		if !found || current < smallest {
			smallest = current
			found = true
		}
	}
	return smallest, found
}
