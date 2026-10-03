package index

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ethantao14/quarry/internal/postings"
)

// Disk reads an immutable index from memory-mapped binary files.
// Its methods return owned data: Postings decodes into new memory and ExternalID copies into a string.
type Disk struct {
	dict         []byte
	post         []byte
	lens         []byte
	ids          []byte
	docCount     uint32
	termCount    uint32
	totalTerms   uint64
	postingCount uint64
}

// Open memory-maps and validates an index directory without decoding its tables.
// Data returned by Disk methods does not point into the mappings. Call Close when done.
func Open(dir string) (*Disk, error) {
	// Resolve through the OS first; filepath.Join resolves ".." by text otherwise.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestName, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestName, err)
	}
	var m manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", manifestName, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", manifestName, err)
		}
		return nil, fmt.Errorf("%s: trailing JSON value", manifestName)
	}
	if m.FormatVersion != FormatVersion {
		return nil, formatError(manifestName, m.FormatVersion)
	}
	if len(m.Files) != 4 {
		return nil, fmt.Errorf("%s: expected exactly four binary files", manifestName)
	}
	d := &Disk{docCount: m.DocCount, termCount: m.TermCount, totalTerms: m.TotalTerms}
	opened := false
	defer func() {
		if !opened {
			// Unmapping read-only files cannot lose data.
			_ = d.Close()
		}
	}()
	files := []struct {
		name  string
		magic string
		dst   *[]byte
	}{
		{dictName, dictMagic, &d.dict},
		{postName, postMagic, &d.post},
		{lensName, lensMagic, &d.lens},
		{idsName, idsMagic, &d.ids},
	}
	for _, file := range files {
		info, ok := m.Files[file.name]
		if !ok {
			return nil, fmt.Errorf("%s: missing %s", manifestName, file.name)
		}
		data, err := mapFile(filepath.Join(dir, file.name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file.name, err)
		}
		*file.dst = data
		if uint64(len(data)) != info.Size {
			return nil, fmt.Errorf("%s: size mismatch", file.name)
		}
		if crc32.Checksum(data, checksumTable) != info.CRC32C {
			return nil, fmt.Errorf("%s: checksum mismatch", file.name)
		}
		if len(data) < headerSize {
			return nil, fmt.Errorf("%s: truncated header", file.name)
		}
		if string(data[:4]) != file.magic {
			return nil, fmt.Errorf("%s: bad magic", file.name)
		}
		if version := binary.LittleEndian.Uint32(data[4:8]); version != FormatVersion {
			return nil, formatError(file.name, version)
		}
	}
	if err := d.validateDict(); err != nil {
		return nil, err
	}
	if err := d.validateLens(); err != nil {
		return nil, err
	}
	if err := d.validateIDs(); err != nil {
		return nil, err
	}
	opened = true
	return d, nil
}

// Close unmaps the index files. After Close, the Disk must not be used.
// Calling Close again returns nil.
func (d *Disk) Close() error {
	var firstErr error
	for _, data := range []*[]byte{&d.dict, &d.post, &d.lens, &d.ids} {
		if err := unmapFile(*data); err != nil && firstErr == nil {
			firstErr = err
		}
		*data = nil
	}
	return firstErr
}

func formatError(name string, version uint32) error {
	return fmt.Errorf("%s: index is format %d, this build reads format %d", name, version, FormatVersion)
}

// readCount returns the uint32 count stored right after a file's header.
func readCount(data []byte) (uint32, bool) {
	if len(data) < tableStart {
		return 0, false
	}
	return binary.LittleEndian.Uint32(data[headerSize:tableStart]), true
}

func (d *Disk) validateDict() error {
	if count, ok := readCount(d.dict); !ok || count != d.termCount {
		return fmt.Errorf("%s: term count mismatch or missing", dictName)
	}
	if d.termBlobStart() > uint64(len(d.dict)) {
		return fmt.Errorf("%s: truncated entries table", dictName)
	}
	blobLen := uint64(len(d.dict)) - d.termBlobStart()
	postLen := uint64(len(d.post))
	var previous []byte
	d.postingCount = 0
	for i := uint32(0); i < d.termCount; i++ {
		entry := d.entry(i)
		if uint64(entry.termOffset)+uint64(entry.termLen) > blobLen {
			return fmt.Errorf("%s: entry %d term range out of bounds", dictName, i)
		}
		term := d.term(entry)
		if i > 0 && bytes.Compare(previous, term) >= 0 {
			return fmt.Errorf("%s: entry %d terms are not strictly ascending", dictName, i)
		}
		previous = term
		start := entry.postingsOffset
		if start < headerSize || start > postLen || uint64(entry.postingsLen) > postLen-start {
			return fmt.Errorf("%s: entry %d postings range out of bounds in %s", dictName, i, postName)
		}
		if entry.docFreq == 0 || entry.docFreq > d.docCount {
			return fmt.Errorf("%s: entry %d invalid document frequency", dictName, i)
		}
		d.postingCount += uint64(entry.docFreq)
	}
	return nil
}

func (d *Disk) validateLens() error {
	if count, ok := readCount(d.lens); !ok || count != d.docCount {
		return fmt.Errorf("%s: document count mismatch or missing", lensName)
	}
	if uint64(len(d.lens)) != tableStart+4*uint64(d.docCount) {
		return fmt.Errorf("%s: invalid lengths table size", lensName)
	}
	var total uint64
	for docID := uint32(0); docID < d.docCount; docID++ {
		total += uint64(d.DocLen(docID))
	}
	if total != d.totalTerms {
		return fmt.Errorf("%s: total terms mismatch", lensName)
	}
	return nil
}

func (d *Disk) validateIDs() error {
	if count, ok := readCount(d.ids); !ok || count != d.docCount {
		return fmt.Errorf("%s: document count mismatch or missing", idsName)
	}
	if d.idBlobStart() > uint64(len(d.ids)) {
		return fmt.Errorf("%s: truncated offsets table", idsName)
	}
	blobLen := uint64(len(d.ids)) - d.idBlobStart()
	var previous uint64
	for i := uint32(0); i <= d.docCount; i++ {
		offset := d.idOffset(i)
		if (i == 0 && offset != 0) || offset < previous || offset > blobLen {
			return fmt.Errorf("%s: invalid offset %d", idsName, i)
		}
		previous = offset
	}
	if previous != blobLen {
		return fmt.Errorf("%s: final offset does not match blob length", idsName)
	}
	return nil
}

func (d *Disk) entry(i uint32) dictEntry {
	start := tableStart + uint64(i)*dictEntrySize
	return parseDictEntry(d.dict[start : start+dictEntrySize])
}

func (d *Disk) termBlobStart() uint64 {
	return tableStart + uint64(d.termCount)*dictEntrySize
}

func (d *Disk) term(entry dictEntry) []byte {
	start := d.termBlobStart() + uint64(entry.termOffset)
	return d.dict[start : start+uint64(entry.termLen)]
}

func (d *Disk) idBlobStart() uint64 {
	return tableStart + 8*(uint64(d.docCount)+1)
}

func (d *Disk) idOffset(i uint32) uint64 {
	start := tableStart + 8*uint64(i)
	return binary.LittleEndian.Uint64(d.ids[start : start+8])
}

// Postings decodes the postings for term, or returns nil if the term is absent.
func (d *Disk) Postings(term string) ([]postings.Posting, error) {
	// Go does not copy for a string(bytes) conversion used only in a comparison.
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
	return d.postingsEntry(entry)
}

func (d *Disk) postingsEntry(entry dictEntry) ([]postings.Posting, error) {
	start := entry.postingsOffset
	data := d.post[start : start+uint64(entry.postingsLen)]
	list, err := postings.Decode(data, int(entry.docFreq))
	if err != nil {
		return nil, fmt.Errorf("%s: term %q: %w", postName, d.term(entry), err)
	}
	for i, posting := range list {
		if posting.DocID >= d.docCount {
			return nil, fmt.Errorf("%s: term %q posting %d: doc ID out of bounds", postName, d.term(entry), i)
		}
	}
	return list, nil
}

// DocCount returns the number of documents in the index.
func (d *Disk) DocCount() int {
	return int(d.docCount)
}

// DocLen returns the number of terms in a document.
func (d *Disk) DocLen(docID uint32) uint32 {
	start := tableStart + 4*uint64(docID)
	return binary.LittleEndian.Uint32(d.lens[start : start+4])
}

// AvgDocLen returns the mean document length, or 0 for an empty index.
func (d *Disk) AvgDocLen() float64 {
	if d.docCount == 0 {
		return 0
	}
	return float64(d.totalTerms) / float64(d.docCount)
}

// ExternalID returns the ID a document had in the original corpus.
func (d *Disk) ExternalID(docID uint32) string {
	if docID >= d.docCount {
		panic("document ID out of range")
	}
	start := d.idBlobStart()
	return string(d.ids[start+d.idOffset(docID) : start+d.idOffset(docID+1)])
}

// TermCount returns the number of distinct terms in the index.
func (d *Disk) TermCount() int {
	return int(d.termCount)
}

// PostingCount returns the number of term-document postings in the index.
func (d *Disk) PostingCount() uint64 {
	return d.postingCount
}
