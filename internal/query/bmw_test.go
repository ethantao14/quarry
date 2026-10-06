package query

import "testing"

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
			cursors := []wandCursor{{cursor: cursor{Cursor: c, doc: 10, count: 3}}}
			if tt.nextDoc != NoMoreDocs {
				cursors = append(cursors, wandCursor{cursor: cursor{doc: tt.nextDoc}})
			}
			bound, next := blockBound(cursors, 0)
			if bound != tt.bound || next != tt.want || c.target != 10 {
				t.Fatalf("blockBound() = (%g, %d), target = %d; want (%g, %d), target 10", bound, next, c.target, tt.bound, tt.want)
			}
		})
	}
}
