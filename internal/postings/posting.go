// Package postings represents and encodes sorted term postings.
package postings

// BlockSize is the number of postings in a full on-disk block.
const BlockSize = 128

// Posting records that a document contains a term, and how many times.
type Posting struct {
	DocID uint32
	TF    uint32
}
