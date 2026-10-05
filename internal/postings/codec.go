package postings

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Encode appends gap-encoded postings to dst. Doc IDs must strictly increase and TFs must be positive.
func Encode(dst []byte, list []Posting) []byte {
	return EncodeBlock(dst, list, 0)
}

// EncodeBlock appends postings whose first gap is relative to previous, the doc ID
// before the block (0 for a list's first block). It is the inverse of DecodeBlock.
func EncodeBlock(dst []byte, list []Posting, previous uint32) []byte {
	for _, posting := range list {
		dst = binary.AppendUvarint(dst, uint64(posting.DocID-previous))
		dst = binary.AppendUvarint(dst, uint64(posting.TF))
		previous = posting.DocID
	}
	return dst
}

// Decode reads exactly count postings and rejects invalid or noncanonical encodings.
func Decode(data []byte, count int) ([]Posting, error) {
	list, consumed, err := DecodeBlock(nil, data, count, 0, false)
	if err != nil {
		return nil, err
	}
	if consumed != len(data) {
		return nil, fmt.Errorf("posting %d: trailing bytes", count)
	}
	return list, nil
}

// DecodeBlock reads count postings into dst[:0] and returns the bytes consumed.
// With hasPrevious, the first gap is relative to previous and must be positive.
func DecodeBlock(dst []Posting, data []byte, count int, previous uint32, hasPrevious bool) ([]Posting, int, error) {
	if count < 0 {
		return nil, 0, fmt.Errorf("negative posting count: %d", count)
	}
	list := dst[:0]
	if cap(list) < min(count, len(data)/2) {
		list = make([]Posting, 0, min(count, len(data)/2))
	}
	var docID uint64
	if hasPrevious {
		docID = uint64(previous)
	}
	consumed := 0
	for i := 0; i < count; i++ {
		gap, n, err := readValue(data[consumed:], "doc ID gap")
		if err != nil {
			return nil, consumed, fmt.Errorf("posting %d: %w", i, err)
		}
		consumed += n
		if (hasPrevious || i > 0) && gap == 0 {
			return nil, consumed, fmt.Errorf("posting %d: zero doc ID gap", i)
		}
		if docID+gap > math.MaxUint32 {
			return nil, consumed, fmt.Errorf("posting %d: doc ID overflows uint32", i)
		}
		tf, n, err := readValue(data[consumed:], "term frequency")
		if err != nil {
			return nil, consumed, fmt.Errorf("posting %d: %w", i, err)
		}
		consumed += n
		if tf == 0 {
			return nil, consumed, fmt.Errorf("posting %d: zero term frequency", i)
		}
		docID += gap
		list = append(list, Posting{DocID: uint32(docID), TF: uint32(tf)})
	}
	return list, consumed, nil
}

func readValue(data []byte, name string) (uint64, int, error) {
	value, n := binary.Uvarint(data)
	if n == 0 {
		return 0, 0, fmt.Errorf("truncated %s", name)
	}
	if n < 0 {
		return 0, 0, fmt.Errorf("malformed or overflowing %s varint", name)
	}
	if value > math.MaxUint32 {
		return 0, 0, fmt.Errorf("%s exceeds uint32", name)
	}
	if n > 1 && data[n-1] == 0 {
		return 0, 0, fmt.Errorf("noncanonical %s varint", name)
	}
	return value, n, nil
}
