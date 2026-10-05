package query

import "math"

// NoMoreDocs marks an exhausted cursor.
const NoMoreDocs = math.MaxUint32

// Cursor walks one term's postings in doc ID order.
type Cursor interface {
	DocID() uint32 // NoMoreDocs after the last posting
	TF() uint32
	Next() error                 // Move to the next posting
	Advance(target uint32) error // Move to the first posting at or beyond target
	DocFreq() int
	MaxScore() float32 // Upper bound of this term's BM25 contribution (count 1)
}
