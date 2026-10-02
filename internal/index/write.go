package index

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/ethantao14/quarry/internal/postings"
)

// Write saves the index in a new directory, publishing its manifest last.
func (ix *Index) Write(dir string) error {
	if uint64(ix.DocCount()) > math.MaxUint32 || uint64(ix.TermCount()) > math.MaxUint32 {
		return fmt.Errorf("index counts exceed format limits")
	}
	// Clean drops a trailing slash, so Dir returns the parent, not dir itself.
	dir = filepath.Clean(dir)
	if err := createIndexDir(dir); err != nil {
		return err
	}
	return ix.writeSegment(dir, durable)
}

// Whether files are synced to disk before closing. Temporary merge inputs skip
// it: they are deleted afterwards, and a crash leaves no manifest to open.
const (
	durable   = true
	temporary = false
)

func createIndexDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("create index parent: %w", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("index directory %s already exists: %w", dir, err)
		}
		return fmt.Errorf("create index directory: %w", err)
	}
	return nil
}

func (ix *Index) writeSegment(dir string, sync bool) error {
	writer, err := newSegmentWriter(dir, sync)
	if err != nil {
		return err
	}
	defer writer.abort()
	terms := make([]string, 0, len(ix.postings))
	for term := range ix.postings {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	for _, term := range terms {
		if err := writer.addTerm(term, ix.postings[term]); err != nil {
			return err
		}
	}
	return writer.finish(uint32(ix.DocCount()), ix.DocLen, ix.ExternalID)
}

type segmentWriter struct {
	dir      string
	sync     bool
	post     *checkedFile
	entries  []dictEntry
	termBlob []byte
	previous string
}

func newSegmentWriter(dir string, sync bool) (*segmentWriter, error) {
	post, err := newCheckedFile(filepath.Join(dir, postName), sync)
	if err != nil {
		return nil, err
	}
	header := binary.LittleEndian.AppendUint32([]byte(postMagic), FormatVersion)
	if _, err := post.Write(header); err != nil {
		post.abort()
		return nil, fmt.Errorf("write %s: %w", post.path, err)
	}
	return &segmentWriter{dir: dir, sync: sync, post: post}, nil
}

func (s *segmentWriter) abort() {
	s.post.abort()
}

func (s *segmentWriter) addTerm(term string, list []postings.Posting) error {
	if len(s.entries) > 0 && term <= s.previous {
		return fmt.Errorf("terms are not strictly ascending: %q", term)
	}
	if uint64(len(s.entries)) >= math.MaxUint32 {
		return fmt.Errorf("index counts exceed format limits")
	}
	encoded := postings.Encode(nil, list)
	if uint64(len(s.termBlob))+uint64(len(term)) > math.MaxUint32 || uint64(len(encoded)) > math.MaxUint32 || uint64(len(list)) > math.MaxUint32 {
		err := fmt.Errorf("term %q exceeds format limits", term)
		return fmt.Errorf("write %s: %w", filepath.Join(s.dir, dictName), err)
	}
	entry := dictEntry{
		termOffset:     uint32(len(s.termBlob)),
		termLen:        uint32(len(term)),
		docFreq:        uint32(len(list)),
		postingsOffset: s.post.size,
		postingsLen:    uint32(len(encoded)),
	}
	if _, err := s.post.Write(encoded); err != nil {
		return fmt.Errorf("write %s: %w", s.post.path, err)
	}
	s.entries = append(s.entries, entry)
	s.termBlob = append(s.termBlob, term...)
	s.previous = term
	return nil
}

func (s *segmentWriter) finish(docCount uint32, docLen func(uint32) uint32, externalID func(uint32) string) error {
	defer s.abort()
	info, err := s.post.finish()
	if err != nil {
		return err
	}
	m := manifest{
		FormatVersion: FormatVersion,
		DocCount:      docCount,
		TermCount:     uint32(len(s.entries)),
		Files:         map[string]fileInfo{postName: info},
	}
	files := []struct {
		name  string
		magic string
		write func(io.Writer) error
	}{
		{dictName, dictMagic, s.writeDict},
		{lensName, lensMagic, func(w io.Writer) error {
			if err := binary.Write(w, binary.LittleEndian, docCount); err != nil {
				return err
			}
			for docID := uint32(0); docID < docCount; docID++ {
				length := docLen(docID)
				m.TotalTerms += uint64(length)
				if err := binary.Write(w, binary.LittleEndian, length); err != nil {
					return err
				}
			}
			return nil
		}},
		{idsName, idsMagic, func(w io.Writer) error {
			return writeIDs(w, docCount, externalID)
		}},
	}
	for _, file := range files {
		info, err := writeFile(filepath.Join(s.dir, file.name), s.sync, func(w io.Writer) error {
			header := binary.LittleEndian.AppendUint32([]byte(file.magic), FormatVersion)
			if _, err := w.Write(header); err != nil {
				return err
			}
			return file.write(w)
		})
		if err != nil {
			return err
		}
		m.Files[file.name] = info
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", manifestName, err)
	}
	_, err = writeFile(filepath.Join(s.dir, manifestName), s.sync, func(w io.Writer) error {
		_, err := w.Write(append(data, '\n'))
		return err
	})
	return err
}

func (s *segmentWriter) writeDict(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(s.entries))); err != nil {
		return err
	}
	for _, entry := range s.entries {
		if _, err := w.Write(entry.appendTo(nil)); err != nil {
			return err
		}
	}
	_, err := w.Write(s.termBlob)
	return err
}

func writeIDs(w io.Writer, docCount uint32, externalID func(uint32) string) error {
	if err := binary.Write(w, binary.LittleEndian, docCount); err != nil {
		return err
	}
	var offset uint64
	for docID := uint32(0); docID < docCount; docID++ {
		if err := binary.Write(w, binary.LittleEndian, offset); err != nil {
			return err
		}
		offset += uint64(len(externalID(docID)))
	}
	if err := binary.Write(w, binary.LittleEndian, offset); err != nil {
		return err
	}
	for docID := uint32(0); docID < docCount; docID++ {
		if _, err := io.WriteString(w, externalID(docID)); err != nil {
			return err
		}
	}
	return nil
}

type checkedFile struct {
	path     string
	file     *os.File
	writer   *bufio.Writer
	checksum hash.Hash32
	size     uint64
	sync     bool
}

func newCheckedFile(path string, sync bool) (*checkedFile, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	return &checkedFile{path: path, file: file, writer: bufio.NewWriter(file), checksum: crc32.New(checksumTable), sync: sync}, nil
}

// Write buffers data and tracks the bytes accepted by the writer.
func (f *checkedFile) Write(data []byte) (int, error) {
	n, err := f.writer.Write(data)
	_, _ = f.checksum.Write(data[:n])
	f.size += uint64(n)
	return n, err
}

func (f *checkedFile) abort() {
	if f.file != nil {
		// The success path checks Close; cleanup preserves the original error.
		_ = f.file.Close()
		f.file = nil
	}
}

func (f *checkedFile) finish() (fileInfo, error) {
	defer f.abort()
	if err := f.writer.Flush(); err != nil {
		return fileInfo{}, fmt.Errorf("flush %s: %w", f.path, err)
	}
	if f.sync {
		if err := f.file.Sync(); err != nil {
			return fileInfo{}, fmt.Errorf("sync %s: %w", f.path, err)
		}
	}
	err := f.file.Close()
	f.file = nil
	if err != nil {
		return fileInfo{}, fmt.Errorf("close %s: %w", f.path, err)
	}
	return fileInfo{Size: f.size, CRC32C: f.checksum.Sum32()}, nil
}

func writeFile(path string, sync bool, write func(io.Writer) error) (fileInfo, error) {
	file, err := newCheckedFile(path, sync)
	if err != nil {
		return fileInfo{}, err
	}
	defer file.abort()
	if err := write(file); err != nil {
		return fileInfo{}, fmt.Errorf("write %s: %w", path, err)
	}
	return file.finish()
}
