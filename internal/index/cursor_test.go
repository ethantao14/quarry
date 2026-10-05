package index

import (
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/ethantao14/quarry/internal/postings"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

func cursorIndex(count int, seed uint64) *Index {
	random := rand.New(rand.NewPCG(seed, 31))
	ix := New()
	for range count {
		for range random.IntN(5) + 1 {
			ix.Add("gap", []string{"other"})
		}
		terms := make([]string, random.IntN(300)+1)
		for i := range terms {
			terms[i] = "term"
		}
		for range random.IntN(20) {
			terms = append(terms, "other")
		}
		ix.Add("match", terms)
	}
	ix.Add("last", nil)
	return ix
}

func TestCursors(t *testing.T) {
	lengths := []int{1, 127, 128, 129, 256, 300}
	random := rand.New(rand.NewPCG(7, 11))
	for range 10 {
		lengths = append(lengths, random.IntN(1000)+1)
	}
	for trial, length := range lengths {
		t.Run(fmt.Sprintf("%d/%d", trial, length), func(t *testing.T) {
			memory := cursorIndex(length, uint64(trial))
			dir := filepath.Join(t.TempDir(), "idx")
			if err := memory.Write(dir); err != nil {
				t.Fatal(err)
			}
			disk, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			cleanupDisk(t, disk)
			if disk.BM25() != scoring.DefaultBM25() {
				t.Fatalf("BM25() = %v", disk.BM25())
			}
			list := memory.postings["term"]
			for _, source := range []struct {
				name  string
				index query.Index
			}{
				{"memory", memory}, {"disk", disk},
			} {
				t.Run(source.name, func(t *testing.T) {
					absent, err := source.index.Cursor("absent")
					if err != nil || absent != nil {
						t.Fatalf("absent cursor = %v, %v", absent, err)
					}
					newCursor := func() query.Cursor {
						t.Helper()
						c, err := source.index.Cursor("term")
						if err != nil {
							t.Fatal(err)
						}
						return c
					}
					c := newCursor()
					if c.DocFreq() != len(list) {
						t.Fatalf("DocFreq() = %d, want %d", c.DocFreq(), len(list))
					}
					var maximum float64
					bm25 := scoring.DefaultBM25()
					idf := scoring.IDF(memory.DocCount(), len(list))
					for _, posting := range list {
						score := bm25.TermScore(idf, posting.TF, memory.DocLen(posting.DocID), memory.AvgDocLen())
						maximum = max(maximum, score)
						if float64(c.MaxScore()) < score {
							t.Fatalf("MaxScore() = %g < %g", c.MaxScore(), score)
						}
					}
					if c.MaxScore() != roundScoreUp(maximum) {
						t.Fatalf("MaxScore() = %g, want %g", c.MaxScore(), roundScoreUp(maximum))
					}
					var got []postings.Posting
					for c.DocID() != query.NoMoreDocs {
						got = append(got, postings.Posting{DocID: c.DocID(), TF: c.TF()})
						if err := c.Next(); err != nil {
							t.Fatal(err)
						}
					}
					if !slices.Equal(got, list) {
						t.Fatal("Next walk differs from list")
					}
					if err := c.Next(); err != nil || c.DocID() != query.NoMoreDocs || c.TF() != 0 {
						t.Fatalf("Next after exhaustion: %v", err)
					}
					targets := []uint32{0, list[0].DocID, list[len(list)-1].DocID, list[len(list)-1].DocID + 1, query.NoMoreDocs}
					for _, p := range list {
						targets = append(targets, p.DocID-1, p.DocID, p.DocID+1)
					}
					for range 100 {
						targets = append(targets, uint32(random.IntN(memory.DocCount()+10)))
					}
					for _, target := range targets {
						c := newCursor()
						position := 0
						// Repeated and decreasing targets must leave the cursor in place.
						for _, next := range []uint32{target, target, 0, target, math.MaxUint32, 0} {
							for position < len(list) && list[position].DocID < next {
								position++
							}
							if err := c.Advance(next); err != nil {
								t.Fatal(err)
							}
							checkCursorPosition(t, c, list, position)
						}
					}
					c = newCursor()
					position := 0
					for target := uint32(0); target < uint32(memory.DocCount()+30); target += uint32(random.IntN(17) + 1) {
						for position < len(list) && list[position].DocID < target {
							position++
						}
						if err := c.Advance(target); err != nil {
							t.Fatal(err)
						}
						checkCursorPosition(t, c, list, position)
						if err := c.Next(); err != nil {
							t.Fatal(err)
						}
						if position < len(list) {
							position++
						}
						checkCursorPosition(t, c, list, position)
					}
				})
			}
			// Check each stored block bound and offset against the original list.
			c, err := disk.Cursor("term")
			if err != nil {
				t.Fatal(err)
			}
			dc := c.(*diskCursor)
			for block := 0; block < len(dc.skip)/skipEntrySize; block++ {
				start := block * postings.BlockSize
				end := min(start+postings.BlockSize, len(list))
				var maximum float64
				for _, p := range list[start:end] {
					maximum = max(maximum, disk.BM25().TermScore(scoring.IDF(disk.DocCount(), len(list)), p.TF, disk.DocLen(p.DocID), disk.AvgDocLen()))
				}
				skip := dc.skipEntry(block)
				if skip.blockMax != roundScoreUp(maximum) || skip.lastDocID != list[end-1].DocID || int(skip.offset) != len(postings.Encode(nil, list[:start])) {
					t.Fatalf("block %d: incorrect skip entry %+v", block, skip)
				}
			}
		})
	}
}

func checkCursorPosition(t *testing.T, c query.Cursor, list []postings.Posting, position int) {
	t.Helper()
	want := postings.Posting{DocID: query.NoMoreDocs}
	if position < len(list) {
		want = list[position]
	}
	if c.DocID() != want.DocID || c.TF() != want.TF {
		t.Fatalf("cursor = (%d, %d), want %v", c.DocID(), c.TF(), want)
	}
}

func TestEmptyCursors(t *testing.T) {
	memory := New()
	dir := filepath.Join(t.TempDir(), "idx")
	if err := memory.Write(dir); err != nil {
		t.Fatal(err)
	}
	disk, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDisk(t, disk)
	for _, ix := range []query.Index{memory, disk} {
		c, err := ix.Cursor("missing")
		if err != nil || c != nil {
			t.Fatalf("empty cursor = %v, %v", c, err)
		}
	}
}

func benchmarkCursorIndex(b *testing.B) *Disk {
	b.Helper()
	memory := New()
	for range 100000 {
		memory.Add("doc", []string{"term"})
	}
	dir := filepath.Join(b.TempDir(), "idx")
	if err := memory.Write(dir); err != nil {
		b.Fatal(err)
	}
	disk, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := disk.Close(); err != nil {
			b.Error(err)
		}
	})
	return disk
}

func BenchmarkCursorAdvance(b *testing.B) {
	disk := benchmarkCursorIndex(b)
	random := rand.New(rand.NewPCG(17, 3))
	b.ReportAllocs()
	for b.Loop() {
		c, err := disk.Cursor("term")
		if err != nil {
			b.Fatal(err)
		}
		for c.DocID() != query.NoMoreDocs {
			if err := c.Advance(c.DocID() + uint32(random.IntN(1024)+1)); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkCursorNext(b *testing.B) {
	disk := benchmarkCursorIndex(b)
	b.ReportAllocs()
	for b.Loop() {
		c, err := disk.Cursor("term")
		if err != nil {
			b.Fatal(err)
		}
		for c.DocID() != query.NoMoreDocs {
			if err := c.Next(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestConcurrentCursors(t *testing.T) {
	memory := cursorIndex(300, 99)
	dir := filepath.Join(t.TempDir(), "idx")
	if err := memory.Write(dir); err != nil {
		t.Fatal(err)
	}
	disk, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDisk(t, disk)
	var wg sync.WaitGroup
	for _, ix := range []query.Index{memory, disk} {
		for range 8 {
			wg.Go(func() {
				c, err := ix.Cursor("term")
				if err != nil {
					t.Error(err)
					return
				}
				maximum := c.MaxScore()
				for _, p := range memory.postings["term"] {
					if c.DocID() != p.DocID || c.TF() != p.TF || c.MaxScore() != maximum {
						t.Error("concurrent cursor differs from list")
						return
					}
					if err := c.Next(); err != nil {
						t.Error(err)
						return
					}
				}
				if c.DocID() != query.NoMoreDocs {
					t.Error("cursor not exhausted")
				}
			})
		}
	}
	wg.Wait()
}
