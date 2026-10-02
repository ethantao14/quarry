package index

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// Builder indexes documents in memory-budgeted chunks and merges them on Finish.
// The budget estimates chunk memory; a single document and merging can exceed it.
type Builder struct {
	dir       string
	memBudget int64
	memory    int64
	chunk     *Index
	docCount  uint32
	segments  []uint32
	finished  bool
	failure   error
}

// NewBuilder creates a new index directory with a positive memory budget in bytes.
func NewBuilder(dir string, memBudget int64) (*Builder, error) {
	if memBudget <= 0 {
		return nil, fmt.Errorf("memory budget must be positive")
	}
	dir = filepath.Clean(dir)
	if err := createIndexDir(dir); err != nil {
		return nil, err
	}
	return &Builder{dir: dir, memBudget: memBudget, chunk: New()}, nil
}

// Add indexes a document in input order, flushing the chunk when it reaches the budget.
func (b *Builder) Add(externalID string, terms []string) error {
	if b.finished {
		return fmt.Errorf("builder is finished")
	}
	if b.failure != nil {
		return b.failure
	}
	if b.docCount == math.MaxUint32 {
		return fmt.Errorf("index counts exceed format limits")
	}
	b.memory += b.estimateDocument(externalID, terms)
	b.chunk.Add(externalID, terms)
	b.docCount++
	if b.memory >= b.memBudget {
		b.failure = b.flush()
		return b.failure
	}
	return nil
}

// estimateDocument counts 64 bytes of map/string/slice overhead per new term,
// 8 bytes per posting, and 4 bytes of length plus a 16-byte ID header per document.
func (b *Builder) estimateDocument(externalID string, terms []string) int64 {
	size := int64(4+16) + int64(len(externalID))
	seen := make(map[string]bool)
	for _, term := range terms {
		if seen[term] {
			continue
		}
		seen[term] = true
		size += 8
		if _, exists := b.chunk.postings[term]; !exists {
			size += int64(len(term)) + 64
		}
	}
	return size
}

func (b *Builder) segmentDir(n int) string {
	return filepath.Join(b.dir, fmt.Sprintf("tmp-seg%d", n))
}

func (b *Builder) flush() error {
	dir := b.segmentDir(len(b.segments))
	if err := createIndexDir(dir); err != nil {
		return err
	}
	if err := b.chunk.writeSegment(dir, temporary); err != nil {
		return err
	}
	b.segments = append(b.segments, uint32(b.chunk.DocCount()))
	b.chunk = New()
	b.memory = 0
	return nil
}

// Finish writes the final index and removes temporary segments after publication.
// A Finish attempt ends the build, including when it returns an error.
func (b *Builder) Finish() error {
	if b.finished {
		return fmt.Errorf("builder is finished")
	}
	b.finished = true
	if b.failure != nil {
		return b.failure
	}
	if len(b.segments) == 0 {
		return b.chunk.writeSegment(b.dir, durable)
	}
	if b.chunk.DocCount() != 0 {
		if err := b.flush(); err != nil {
			return err
		}
	}
	dirs := make([]string, len(b.segments))
	for i := range dirs {
		dirs[i] = b.segmentDir(i)
	}
	if err := mergeSegments(b.dir, dirs, b.segments); err != nil {
		return err
	}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove temporary segment: %w", err)
		}
	}
	return nil
}

// Segments returns the number of chunks flushed to temporary segment directories.
func (b *Builder) Segments() int {
	return len(b.segments)
}
