// Package postings represents and encodes sorted term postings.
package postings

// Posting records that a document contains a term, and how many times.
type Posting struct {
	DocID uint32
	TF    uint32
}
