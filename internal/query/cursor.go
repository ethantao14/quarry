package query

import "math"

// NoMoreDocs marks an exhausted cursor.
const NoMoreDocs = math.MaxUint32

// Cursor walks one term's postings in doc ID order.
// Its block pointer starts at zero, never moves backward, and stays at or ahead
// of the posting's block. Memory and disk use postings.BlockSize (128) boundaries.
type Cursor interface {
	DocID() uint32 // NoMoreDocs after the last posting
	TF() uint32
	Next() error                 // Move to the next posting
	Advance(target uint32) error // Move to the first posting at or beyond target
	DocFreq() int
	MaxScore() float32 // Upper bound of this term's BM25 contribution (count 1)
	// ShallowAdvance finds the first block ending at or beyond target, or the end.
	// Targets at or before the current block's last doc are a no-op.
	// It never decodes postings or changes DocID or TF; Next/Advance keep it caught up.
	ShallowAdvance(target uint32)
	// BlockMax returns the block pointer's last doc and cached, upward-rounded bound.
	// The bound equals the disk skip entry's blockMax; past the end it returns (NoMoreDocs, 0).
	BlockMax() (lastDoc uint32, score float32)
}
