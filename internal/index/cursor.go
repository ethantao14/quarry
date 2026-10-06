package index

import (
	"fmt"
	"sort"

	"github.com/ethantao14/quarry/internal/postings"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

// Cursor returns a cursor on the first posting, or nil if term is absent.
// The cursor borrows the mappings and must not be used after Close.
func (d *Disk) Cursor(term string) (query.Cursor, error) {
	i := sort.Search(int(d.termCount), func(i int) bool {
		return string(d.term(d.entry(uint32(i)))) >= term
	})
	if i == int(d.termCount) {
		return nil, nil
	}
	entry := d.entry(uint32(i))
	if string(d.term(entry)) != term {
		return nil, nil
	}
	count := (uint64(entry.docFreq) + postings.BlockSize - 1) / postings.BlockSize
	c := &diskCursor{
		disk:  d,
		entry: entry,
		skip:  d.skip[entry.skipOffset : entry.skipOffset+count*skipEntrySize],
	}
	if err := c.decodeBlock(0); err != nil {
		return nil, err
	}
	return c, nil
}

type diskCursor struct {
	disk     *Disk
	entry    dictEntry
	skip     []byte
	buffer   [postings.BlockSize]postings.Posting
	list     []postings.Posting
	block    int
	shallow  int
	position int
}

func (c *diskCursor) skipEntry(block int) skipEntry {
	start := block * skipEntrySize
	return parseSkipEntry(c.skip[start : start+skipEntrySize])
}

func (c *diskCursor) decodeBlock(block int) error {
	skip := c.skipEntry(block)
	end := c.entry.postingsLen
	if block+1 < len(c.skip)/skipEntrySize {
		end = c.skipEntry(block + 1).offset
	}
	var previous uint32
	if block > 0 {
		previous = c.skipEntry(block - 1).lastDocID
	}
	start := c.entry.postingsOffset
	data := c.disk.post[start+uint64(skip.offset) : start+uint64(end)]
	count := min(postings.BlockSize, int(c.entry.docFreq)-block*postings.BlockSize)
	list, consumed, err := postings.DecodeBlock(c.buffer[:0], data, count, previous, block > 0)
	if err == nil && consumed != len(data) {
		err = fmt.Errorf("block %d: decoded bytes disagree with skip offset", block)
	}
	if err == nil && list[len(list)-1].DocID != skip.lastDocID {
		err = fmt.Errorf("block %d: last doc ID disagrees with skip entry", block)
	}
	if err == nil {
		for _, posting := range list {
			if posting.DocID >= c.disk.docCount {
				err = fmt.Errorf("block %d: doc ID out of bounds", block)
				break
			}
		}
	}
	if err != nil {
		c.list = nil
		c.position = 0
		return fmt.Errorf("%s: term %q: %w", postName, c.disk.term(c.entry), err)
	}
	c.list = list
	c.block = block
	c.shallow = max(c.shallow, block)
	c.position = 0
	return nil
}

func (c *diskCursor) DocID() uint32 {
	if c.position == len(c.list) {
		return query.NoMoreDocs
	}
	return c.list[c.position].DocID
}

func (c *diskCursor) TF() uint32 {
	if c.position == len(c.list) {
		return 0
	}
	return c.list[c.position].TF
}

func (c *diskCursor) Next() error {
	if c.DocID() == query.NoMoreDocs {
		return nil
	}
	c.position++
	if c.position == len(c.list) && c.block+1 < len(c.skip)/skipEntrySize {
		return c.decodeBlock(c.block + 1)
	}
	if c.position == len(c.list) {
		c.shallow = len(c.skip) / skipEntrySize
	}
	return nil
}

func (c *diskCursor) Advance(target uint32) error {
	if target <= c.DocID() {
		return nil
	}
	block := sort.Search(len(c.skip)/skipEntrySize, func(i int) bool {
		return c.skipEntry(i).lastDocID >= target
	})
	if block == len(c.skip)/skipEntrySize {
		c.position = len(c.list)
		c.shallow = block
		return nil
	}
	if block != c.block {
		if err := c.decodeBlock(block); err != nil {
			return err
		}
	}
	for c.list[c.position].DocID < target {
		c.position++
	}
	return nil
}

func (c *diskCursor) DocFreq() int { return int(c.entry.docFreq) }

func (c *diskCursor) MaxScore() float32 { return c.entry.maxScore }

func (c *diskCursor) ShallowAdvance(target uint32) {
	c.shallow += sort.Search(len(c.skip)/skipEntrySize-c.shallow, func(i int) bool {
		return c.skipEntry(c.shallow+i).lastDocID >= target
	})
}

func (c *diskCursor) BlockMax() (uint32, float32) {
	if c.shallow == len(c.skip)/skipEntrySize {
		return query.NoMoreDocs, 0
	}
	skip := c.skipEntry(c.shallow)
	return skip.lastDocID, skip.blockMax
}

// Cursor returns a cursor on the first posting, or nil if term is absent.
// Finish adding documents before creating cursors.
func (ix *Index) Cursor(term string) (query.Cursor, error) {
	list := ix.postings[term]
	if len(list) == 0 {
		return nil, nil
	}
	return &memoryCursor{index: ix, list: list}, nil
}

type memoryCursor struct {
	index    *Index
	list     []postings.Posting
	position int
	shallow  int
	blocks   []float32
	maximum  float32
	scored   bool
}

func (c *memoryCursor) DocID() uint32 {
	if c.position == len(c.list) {
		return query.NoMoreDocs
	}
	return c.list[c.position].DocID
}

func (c *memoryCursor) TF() uint32 {
	if c.position == len(c.list) {
		return 0
	}
	return c.list[c.position].TF
}

func (c *memoryCursor) Next() error {
	if c.position < len(c.list) {
		c.position++
		c.syncBlock()
	}
	return nil
}

func (c *memoryCursor) Advance(target uint32) error {
	if target <= c.DocID() {
		return nil
	}
	c.position += sort.Search(len(c.list)-c.position, func(i int) bool {
		return c.list[c.position+i].DocID >= target
	})
	c.syncBlock()
	return nil
}

func (c *memoryCursor) DocFreq() int { return len(c.list) }

func (c *memoryCursor) MaxScore() float32 {
	if !c.scored {
		for block := 0; block < c.blockCount(); block++ {
			c.maximum = max(c.maximum, c.blockScore(block))
		}
		c.scored = true
	}
	return c.maximum
}

func (c *memoryCursor) blockCount() int {
	return (len(c.list) + postings.BlockSize - 1) / postings.BlockSize
}

func (c *memoryCursor) syncBlock() {
	if c.position == len(c.list) {
		c.shallow = c.blockCount()
	} else {
		c.shallow = max(c.shallow, c.position/postings.BlockSize)
	}
}

func (c *memoryCursor) blockLast(block int) uint32 {
	end := min((block+1)*postings.BlockSize, len(c.list))
	return c.list[end-1].DocID
}

func (c *memoryCursor) ShallowAdvance(target uint32) {
	c.shallow += sort.Search(c.blockCount()-c.shallow, func(i int) bool {
		return c.blockLast(c.shallow+i) >= target
	})
}

func (c *memoryCursor) BlockMax() (uint32, float32) {
	if c.shallow == c.blockCount() {
		return query.NoMoreDocs, 0
	}
	return c.blockLast(c.shallow), c.blockScore(c.shallow)
}

func (c *memoryCursor) blockScore(block int) float32 {
	if c.blocks == nil {
		c.blocks = make([]float32, c.blockCount())
	}
	// Nonempty blocks have positive scores with DefaultBM25, so zero means uncached.
	if c.blocks[block] == 0 {
		bm25 := scoring.DefaultBM25()
		idf := scoring.IDF(c.index.DocCount(), len(c.list))
		start := block * postings.BlockSize
		end := min(start+postings.BlockSize, len(c.list))
		var maximum float64
		for _, posting := range c.list[start:end] {
			score := bm25.TermScore(idf, posting.TF, c.index.DocLen(posting.DocID), c.index.AvgDocLen())
			maximum = max(maximum, score)
		}
		c.blocks[block] = roundScoreUp(maximum)
	}
	return c.blocks[block]
}
