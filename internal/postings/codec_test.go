package postings

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func roundTripCases() []struct {
	name string
	list []Posting
} {
	many := make([]Posting, 1000)
	for i := range many {
		many[i] = Posting{DocID: uint32(i * 17), TF: uint32(i + 1)}
	}
	return []struct {
		name string
		list []Posting
	}{
		{name: "empty"},
		{name: "doc zero", list: []Posting{{DocID: 0, TF: 1}}},
		{name: "max doc", list: []Posting{{DocID: math.MaxUint32, TF: 1}}},
		{name: "max frequency", list: []Posting{{DocID: 0, TF: math.MaxUint32}}},
		{name: "large gaps", list: []Posting{{DocID: 128, TF: 128}, {DocID: 1 << 30, TF: 1}, {DocID: math.MaxUint32, TF: 7}}},
		{name: "many", list: many},
	}
}

func TestCodecRoundTrip(t *testing.T) {
	for _, tt := range roundTripCases() {
		t.Run(tt.name, func(t *testing.T) {
			prefix := []byte{7, 9, 3}
			data := Encode(slices.Clone(prefix), tt.list)
			if !bytes.Equal(data[:len(prefix)], prefix) {
				t.Fatal("Encode changed prefix")
			}
			got, err := Decode(data[len(prefix):], len(tt.list))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.list) {
				t.Fatalf("Decode() = %v, want %v", got, tt.list)
			}
		})
	}
}

func TestCodecRandomRoundTrip(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 1000; trial++ {
		list := make([]Posting, random.IntN(1000))
		var docID uint32
		for i := range list {
			docID += uint32(random.IntN(100000) + 1)
			list[i] = Posting{DocID: docID, TF: uint32(random.Uint64N(math.MaxUint32) + 1)}
		}
		got, err := Decode(Encode(nil, list), len(list))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, list) {
			t.Fatalf("trial %d: Decode() = %v, want %v", trial, got, list)
		}
	}
}

type malformedCase struct {
	name  string
	data  []byte
	count int
	want  string
}

func malformedCases() []malformedCase {
	tooLarge := binary.AppendUvarint(nil, uint64(math.MaxUint32)+1)
	// Nine continuation bytes then a final byte above 1 exceed 64 bits.
	overflow := append(bytes.Repeat([]byte{0xff}, 9), 0x7f)
	maxDoc := Encode(nil, []Posting{{DocID: math.MaxUint32, TF: 1}})
	return []malformedCase{
		{"negative count", nil, -1, "negative posting count"},
		{"huge count", nil, math.MaxInt, "posting 0: truncated"},
		{"missing gap", nil, 1, "posting 0: truncated doc ID gap"},
		{"truncated gap", []byte{0x80}, 1, "posting 0: truncated doc ID gap"},
		{"missing tf", []byte{0}, 1, "posting 0: truncated term frequency"},
		{"truncated tf", []byte{0, 0x80}, 1, "posting 0: truncated term frequency"},
		{"overflow gap varint", overflow, 1, "posting 0: malformed or overflowing doc ID gap"},
		{"overflow tf varint", append([]byte{0}, overflow...), 1, "posting 0: malformed or overflowing term frequency"},
		{"gap exceeds uint32", tooLarge, 1, "posting 0: doc ID gap exceeds uint32"},
		{"tf exceeds uint32", append([]byte{0}, tooLarge...), 1, "posting 0: term frequency exceeds uint32"},
		{"doc overflow", append(maxDoc, 1, 1), 2, "posting 1: doc ID overflows uint32"},
		{"zero gap", []byte{0, 1, 0, 1}, 2, "posting 1: zero doc ID gap"},
		{"zero tf", []byte{0, 0}, 1, "posting 0: zero term frequency"},
		{"trailing bytes", []byte{0, 1, 7}, 1, "posting 1: trailing bytes"},
		{"zero count with data", []byte{0}, 0, "posting 0: trailing bytes"},
		{"noncanonical gap", []byte{0x80, 0, 1}, 1, "posting 0: noncanonical doc ID gap"},
		{"noncanonical tf", []byte{0, 0x81, 0}, 1, "posting 0: noncanonical term frequency"},
	}
}

func TestDecodeMalformed(t *testing.T) {
	for _, tt := range malformedCases() {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.data, tt.count)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Decode() error = %v, want %q", err, tt.want)
			}
		})
	}
}

// FuzzDecode checks that arbitrary input cannot panic and successful decoding preserves bytes.
func FuzzDecode(f *testing.F) {
	for _, tt := range roundTripCases() {
		f.Add(Encode(nil, tt.list), len(tt.list))
	}
	for _, tt := range malformedCases() {
		f.Add(tt.data, tt.count)
	}
	f.Fuzz(func(t *testing.T, data []byte, count int) {
		checkBlockRoundTrip(t, data, count, 0, false)
		checkBlockRoundTrip(t, data, count, 17, true)
		list, err := Decode(data, count)
		if err == nil && !bytes.Equal(Encode(nil, list), data) {
			t.Fatalf("successful Decode did not round trip: %x", data)
		}
	})
}

// BenchmarkDecode measures decoding a list of 100,000 postings.
func BenchmarkDecode(b *testing.B) {
	list := make([]Posting, 100000)
	for i := range list {
		list[i] = Posting{DocID: uint32(i * 3), TF: uint32(i%100 + 1)}
	}
	data := Encode(nil, list)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Decode(data, len(list)); err != nil {
			b.Fatal(err)
		}
	}
}
