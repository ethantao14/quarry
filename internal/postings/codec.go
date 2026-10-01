package postings

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Encode appends gap-encoded postings to dst. Doc IDs must strictly increase and TFs must be positive.
func Encode(dst []byte, list []Posting) []byte {
	var previous uint32
	for _, posting := range list {
		dst = binary.AppendUvarint(dst, uint64(posting.DocID-previous))
		dst = binary.AppendUvarint(dst, uint64(posting.TF))
		previous = posting.DocID
	}
	return dst
}

// Decode reads exactly count postings and rejects invalid or noncanonical encodings.
func Decode(data []byte, count int) ([]Posting, error) {
	if count < 0 {
		return nil, fmt.Errorf("negative posting count: %d", count)
	}
	list := make([]Posting, 0, min(count, len(data)/2))
	var previous uint64
	for i := 0; i < count; i++ {
		gap, n, err := readValue(data, "doc ID gap")
		if err != nil {
			return nil, fmt.Errorf("posting %d: %w", i, err)
		}
		data = data[n:]
		if i > 0 && gap == 0 {
			return nil, fmt.Errorf("posting %d: zero doc ID gap", i)
		}
		if previous+gap > math.MaxUint32 {
			return nil, fmt.Errorf("posting %d: doc ID overflows uint32", i)
		}
		tf, n, err := readValue(data, "term frequency")
		if err != nil {
			return nil, fmt.Errorf("posting %d: %w", i, err)
		}
		data = data[n:]
		if tf == 0 {
			return nil, fmt.Errorf("posting %d: zero term frequency", i)
		}
		previous += gap
		list = append(list, Posting{DocID: uint32(previous), TF: uint32(tf)})
	}
	if len(data) != 0 {
		return nil, fmt.Errorf("posting %d: trailing bytes", count)
	}
	return list, nil
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
