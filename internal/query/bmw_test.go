package query

import (
	"slices"
	"testing"
)

func TestReorder(t *testing.T) {
	for _, tt := range []struct {
		name string
		docs []uint32 // Cursor i has rank i
		want []int    // Ranks after reorder
	}{
		{"sorted", []uint32{1, 2, 3}, []int{0, 1, 2}},
		{"reversed", []uint32{3, 2, 1}, []int{2, 1, 0}},
		{"smallest last", []uint32{2, 3, 4, 1}, []int{3, 0, 1, 2}},
		{"ties by rank", []uint32{5, 5, 1, 5}, []int{2, 0, 1, 3}},
		{"drops exhausted", []uint32{NoMoreDocs, 4, NoMoreDocs, 2}, []int{3, 1}},
		{"all exhausted", []uint32{NoMoreDocs, NoMoreDocs}, []int{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cursors := make([]*wandCursor, len(tt.docs))
			for i, doc := range tt.docs {
				cursors[i] = &wandCursor{cursor: cursor{doc: doc}, rank: i}
			}
			got := []int{}
			for _, c := range reorder(cursors) {
				got = append(got, c.rank)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("reorder ranks = %v, want %v", got, tt.want)
			}
		})
	}
}

type blockBoundCursor struct {
	Cursor
	last   uint32
	score  float32
	target uint32
}

func (c *blockBoundCursor) ShallowAdvance(target uint32) { c.target = target }

func (c *blockBoundCursor) BlockMax() (uint32, float32) { return c.last, c.score }

func TestBlockBoundBoundary(t *testing.T) {
	for _, tt := range []struct {
		name    string
		last    uint32
		score   float32
		nextDoc uint32
		want    uint32
		bound   float64
	}{
		{"block end", 127, 2, NoMoreDocs, 128, 6},
		{"next cursor", 127, 2, 100, 100, 6},
		{"last possible doc", NoMoreDocs - 1, 2, NoMoreDocs, NoMoreDocs, 6},
		{"past end", NoMoreDocs, 0, NoMoreDocs, NoMoreDocs, 0},
		{"past end with next cursor", NoMoreDocs, 0, 100, 100, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &blockBoundCursor{last: tt.last, score: tt.score}
			cursors := []*wandCursor{{cursor: cursor{Cursor: c, doc: 10, count: 3}}}
			if tt.nextDoc != NoMoreDocs {
				cursors = append(cursors, &wandCursor{cursor: cursor{doc: tt.nextDoc}})
			}
			bound, next := blockBound(cursors, 0)
			if bound != tt.bound || next != tt.want || c.target != 10 {
				t.Fatalf("blockBound() = (%g, %d), target = %d; want (%g, %d), target 10", bound, next, c.target, tt.bound, tt.want)
			}
		})
	}
}
