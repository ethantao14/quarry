// Package index stores which documents contain which terms.
package index

import (
	"github.com/ethantao14/quarry/internal/postings"
	"github.com/ethantao14/quarry/internal/scoring"
)

// Index is an in-memory inverted index. Internal doc IDs are assigned in
// the order documents are added, so every postings list is sorted by DocID.
type Index struct {
	postings    map[string][]postings.Posting
	docLens     []uint32
	externalIDs []string
	totalLen    uint64
}

// New returns an empty index.
func New() *Index {
	return &Index{postings: make(map[string][]postings.Posting)}
}

// BM25 returns the parameters used to compute cursor score bounds.
func (ix *Index) BM25() scoring.BM25 {
	return scoring.DefaultBM25()
}

// Add indexes a document's terms and returns its internal doc ID.
func (ix *Index) Add(externalID string, terms []string) uint32 {
	docID := uint32(len(ix.docLens))

	termCounts := make(map[string]uint32)
	for _, term := range terms {
		termCounts[term]++
	}
	for term, tf := range termCounts {
		ix.postings[term] = append(ix.postings[term], postings.Posting{DocID: docID, TF: tf})
	}

	ix.docLens = append(ix.docLens, uint32(len(terms)))
	ix.externalIDs = append(ix.externalIDs, externalID)
	ix.totalLen += uint64(len(terms))
	return docID
}

// Postings returns the postings list for term, or nil if no document has it.
func (ix *Index) Postings(term string) ([]postings.Posting, error) {
	return ix.postings[term], nil
}

// DocCount returns the number of documents in the index.
func (ix *Index) DocCount() int {
	return len(ix.docLens)
}

// DocLen returns the number of terms in a document.
func (ix *Index) DocLen(docID uint32) uint32 {
	return ix.docLens[docID]
}

// AvgDocLen returns the mean document length, or 0 for an empty index.
func (ix *Index) AvgDocLen() float64 {
	if len(ix.docLens) == 0 {
		return 0
	}
	return float64(ix.totalLen) / float64(len(ix.docLens))
}

// ExternalID returns the ID a document had in the original corpus.
func (ix *Index) ExternalID(docID uint32) string {
	return ix.externalIDs[docID]
}

// TermCount returns the number of distinct terms in the index.
func (ix *Index) TermCount() int {
	return len(ix.postings)
}
