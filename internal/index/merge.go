package index

import (
	"bytes"
	"container/heap"
	"sort"

	"github.com/ethantao14/quarry/internal/postings"
)

type segmentCursor struct {
	segment int
	entry   uint32
}

type termHeap struct {
	disks   []*Disk
	cursors []segmentCursor
}

// Len returns the number of active segment cursors.
func (h termHeap) Len() int { return len(h.cursors) }

// Less orders cursors by term bytes, then by segment position.
func (h termHeap) Less(i, j int) bool {
	a, b := h.cursors[i], h.cursors[j]
	comparison := bytes.Compare(h.term(a), h.term(b))
	if comparison == 0 {
		return a.segment < b.segment
	}
	return comparison < 0
}

// Swap exchanges two cursors.
func (h termHeap) Swap(i, j int) { h.cursors[i], h.cursors[j] = h.cursors[j], h.cursors[i] }

// Push appends a cursor for container/heap.
func (h *termHeap) Push(value any) { h.cursors = append(h.cursors, value.(segmentCursor)) }

// Pop removes the last cursor for container/heap.
func (h *termHeap) Pop() any {
	last := len(h.cursors) - 1
	cursor := h.cursors[last]
	h.cursors = h.cursors[:last]
	return cursor
}

func (h termHeap) term(cursor segmentCursor) []byte {
	disk := h.disks[cursor.segment]
	return disk.term(disk.entry(cursor.entry))
}

func mergeSegments(dir string, dirs []string, counts []uint32) (err error) {
	disks := make([]*Disk, 0, len(dirs))
	defer func() {
		for _, disk := range disks {
			if closeErr := disk.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
	}()
	bases := make([]uint32, len(dirs))
	var docCount uint32
	var totalTerms uint64
	for i, path := range dirs {
		disk, err := Open(path)
		if err != nil {
			return err
		}
		disks = append(disks, disk)
		bases[i] = docCount
		docCount += counts[i]
		totalTerms += disk.totalTerms
	}
	segmentFor := func(docID uint32) int {
		return sort.Search(len(bases), func(i int) bool { return bases[i] > docID }) - 1
	}
	docLen := func(docID uint32) uint32 {
		i := segmentFor(docID)
		return disks[i].DocLen(docID - bases[i])
	}
	var avgDocLen float64
	if docCount > 0 {
		avgDocLen = float64(totalTerms) / float64(docCount)
	}
	writer, err := newSegmentWriter(dir, durable, scoringStats{docCount, avgDocLen, docLen})
	if err != nil {
		return err
	}
	defer writer.abort()
	terms := &termHeap{disks: disks}
	for i, disk := range disks {
		if disk.termCount > 0 {
			terms.cursors = append(terms.cursors, segmentCursor{segment: i})
		}
	}
	heap.Init(terms)
	for terms.Len() > 0 {
		term := string(terms.term(terms.cursors[0]))
		var combined []postings.Posting
		for terms.Len() > 0 && string(terms.term(terms.cursors[0])) == term {
			cursor := heap.Pop(terms).(segmentCursor)
			disk := disks[cursor.segment]
			list, err := disk.postingsEntry(disk.entry(cursor.entry))
			if err != nil {
				return err
			}
			for _, posting := range list {
				posting.DocID += bases[cursor.segment]
				combined = append(combined, posting)
			}
			cursor.entry++
			if cursor.entry < disk.termCount {
				heap.Push(terms, cursor)
			}
		}
		if err := writer.addTerm(term, combined); err != nil {
			return err
		}
	}
	return writer.finish(docCount, docLen, func(docID uint32) string {
		i := segmentFor(docID)
		return disks[i].ExternalID(docID - bases[i])
	})
}
