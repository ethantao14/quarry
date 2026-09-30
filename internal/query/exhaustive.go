package query

import (
	"sort"

	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/scoring"
)

type cursor struct {
	postings []index.Posting
	position int
	count    int
	idf      float64
}

// Exhaustive scores every matching document and returns the best k results.
func Exhaustive(ix *index.Index, bm25 scoring.BM25, queryTerms []string, k int) []Result {
	top := NewTopK(k)
	if k <= 0 {
		return top.Results()
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
		postings := ix.Postings(term)
		if len(postings) == 0 {
			continue
		}
		cursors = append(cursors, cursor{
			postings: postings,
			count:    counts[term],
			idf:      scoring.IDF(ix.DocCount(), len(postings)),
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
	return top.Results()
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
