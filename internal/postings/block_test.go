package postings

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestDecodeBlock(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		count       int
		previous    uint32
		hasPrevious bool
		want        []Posting
		consumed    int
	}{
		{"empty", nil, 0, 0, false, nil, 0},
		{"zero count with data", []byte{0, 1}, 0, 0, false, nil, 0},
		{"first doc zero", []byte{0, 2}, 1, 0, false, []Posting{{0, 2}}, 2},
		{"ignore previous", []byte{3, 2}, 1, 100, false, []Posting{{3, 2}}, 2},
		{"previous zero", []byte{3, 2}, 1, 0, true, []Posting{{3, 2}}, 2},
		{"previous nonzero", []byte{3, 2, 2, 1, 99}, 2, 100, true, []Posting{{103, 2}, {105, 1}}, 4},
		{"multibyte", []byte{0x80, 1, 0x80, 1, 1, 1}, 1, 10, true, []Posting{{138, 128}}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]Posting, 3, 8)
			got, n, err := DecodeBlock(dst, tt.data, tt.count, tt.previous, tt.hasPrevious)
			if err != nil || n != tt.consumed || !slices.Equal(got, tt.want) {
				t.Fatalf("DecodeBlock() = %v, %d, %v; want %v, %d", got, n, err, tt.want, tt.consumed)
			}
			if len(got) > 0 && &got[0] != &dst[0] {
				t.Fatal("DecodeBlock did not reuse dst")
			}
		})
	}
}

func TestDecodeBlockMalformed(t *testing.T) {
	for _, tt := range malformedCases() {
		if strings.Contains(tt.want, "trailing bytes") {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeBlock(nil, tt.data, tt.count, 0, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("DecodeBlock() error = %v, want %q", err, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		name     string
		gap      byte
		previous uint32
		want     string
	}{
		{"zero first gap", 0, 0, "zero doc ID gap"},
		{"previous overflow", 1, math.MaxUint32, "doc ID overflows uint32"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeBlock(nil, []byte{tt.gap, 1}, 1, tt.previous, true)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("DecodeBlock() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func checkBlockRoundTrip(t *testing.T, data []byte, count int, previous uint32, hasPrevious bool) {
	t.Helper()
	list, n, err := DecodeBlock(make([]Posting, 0, BlockSize), data, count, previous, hasPrevious)
	if err != nil {
		return
	}
	var encoded []byte
	if !hasPrevious {
		previous = 0
	}
	for _, posting := range list {
		encoded = binary.AppendUvarint(encoded, uint64(posting.DocID-previous))
		encoded = binary.AppendUvarint(encoded, uint64(posting.TF))
		previous = posting.DocID
	}
	if n < 0 || n > len(data) || !bytes.Equal(encoded, data[:n]) || len(list) != count {
		t.Fatalf("successful DecodeBlock did not round trip: %x", data)
	}
}

func FuzzDecodeBlock(f *testing.F) {
	for _, tt := range roundTripCases() {
		f.Add(Encode(nil, tt.list), len(tt.list), uint32(0), false)
	}
	for _, tt := range malformedCases() {
		f.Add(tt.data, tt.count, uint32(1), true)
	}
	f.Add([]byte{1, 1}, 1, uint32(math.MaxUint32), true)
	f.Fuzz(func(t *testing.T, data []byte, count int, previous uint32, hasPrevious bool) {
		checkBlockRoundTrip(t, data, count, previous, hasPrevious)
	})
}
