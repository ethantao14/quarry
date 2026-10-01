package query

import (
	"fmt"
	"sort"

	"github.com/ethantao14/quarry/internal/postings"
	"github.com/ethantao14/quarry/internal/scoring"
)

// Index is what query processing needs from an index, in memory or on disk.
type Index interface {
	Postings(term string) ([]postings.Posting, error)
	DocCount() int
	DocLen(docID uint32) uint32
	AvgDocLen() float64
}

type cursor struct {
	postings []postings.Posting
	position int
	count    int
	idf      float64
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
		list, err := ix.Postings(term)
		if err != nil {
			return nil, fmt.Errorf("postings for %q: %w", term, err)
		}
		if len(list) == 0 {
			continue
		}
		cursors = append(cursors, cursor{
			postings: list,
			count:    counts[term],
			idf:      scoring.IDF(ix.DocCount(), len(list)),
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
			if c.position == len(c.postings) || c.postings[c.position].DocID != docID {
				continue
			}
			posting := c.postings[c.position]
			score += float64(c.count) * bm25.TermScore(c.idf, posting.TF, ix.DocLen(docID), ix.AvgDocLen())
			c.position++
		}
		top.Offer(Result{DocID: docID, Score: score})
	}
	return top.Results(), nil
}

// nextDocID returns the smallest doc ID any cursor is on, or false if all are exhausted.
func nextDocID(cursors []cursor) (uint32, bool) {
	var smallest uint32
	found := false
	for _, c := range cursors {
		if c.position == len(c.postings) {
			continue
		}
		current := c.postings[c.position].DocID
		if !found || current < smallest {
			smallest = current
			found = true
		}
	}
	return smallest, found
}
