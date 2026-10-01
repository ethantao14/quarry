package index

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
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
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("create index parent: %w", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("index directory %s already exists: %w", dir, err)
		}
		return fmt.Errorf("create index directory: %w", err)
	}
	terms := make([]string, 0, len(ix.postings))
	for term := range ix.postings {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	m := manifest{
		FormatVersion: FormatVersion,
		DocCount:      uint32(ix.DocCount()),
		TotalTerms:    ix.totalLen,
		TermCount:     uint32(len(terms)),
		Files:         make(map[string]fileInfo),
	}
	files := []struct {
		name  string
		magic string
		write func(io.Writer) error
	}{
		{dictName, dictMagic, func(w io.Writer) error { return ix.writeDict(w, terms) }},
		{postName, postMagic, func(w io.Writer) error { return ix.writePostings(w, terms) }},
		{lensName, lensMagic, ix.writeLens},
		{idsName, idsMagic, ix.writeIDs},
	}
	for _, file := range files {
		info, err := writeFile(filepath.Join(dir, file.name), func(w io.Writer) error {
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
	_, err = writeFile(filepath.Join(dir, manifestName), func(w io.Writer) error {
		_, err := w.Write(append(data, '\n'))
		return err
	})
	return err
}

func (ix *Index) writeDict(w io.Writer, terms []string) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(terms))); err != nil {
		return err
	}
	var termOffset uint64
	postOffset := uint64(headerSize)
	var encoded []byte
	for _, term := range terms {
		encoded = postings.Encode(encoded[:0], ix.postings[term])
		if termOffset+uint64(len(term)) > math.MaxUint32 || uint64(len(encoded)) > math.MaxUint32 {
			return fmt.Errorf("term %q exceeds format limits", term)
		}
		entry := dictEntry{
			termOffset:     uint32(termOffset),
			termLen:        uint32(len(term)),
			docFreq:        uint32(len(ix.postings[term])),
			postingsOffset: postOffset,
			postingsLen:    uint32(len(encoded)),
		}
		if _, err := w.Write(entry.appendTo(nil)); err != nil {
			return err
		}
		termOffset += uint64(len(term))
		postOffset += uint64(len(encoded))
	}
	for _, term := range terms {
		if _, err := io.WriteString(w, term); err != nil {
			return err
		}
	}
	return nil
}

func (ix *Index) writePostings(w io.Writer, terms []string) error {
	var encoded []byte
	for _, term := range terms {
		encoded = postings.Encode(encoded[:0], ix.postings[term])
		if _, err := w.Write(encoded); err != nil {
			return err
		}
	}
	return nil
}

func (ix *Index) writeLens(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(ix.DocCount())); err != nil {
		return err
	}
	for _, length := range ix.docLens {
		if err := binary.Write(w, binary.LittleEndian, length); err != nil {
			return err
		}
	}
	return nil
}

func (ix *Index) writeIDs(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(ix.DocCount())); err != nil {
		return err
	}
	var offset uint64
	for _, id := range ix.externalIDs {
		if err := binary.Write(w, binary.LittleEndian, offset); err != nil {
			return err
		}
		offset += uint64(len(id))
	}
	if err := binary.Write(w, binary.LittleEndian, offset); err != nil {
		return err
	}
	for _, id := range ix.externalIDs {
		if _, err := io.WriteString(w, id); err != nil {
			return err
		}
	}
	return nil
}

type byteCounter struct {
	size uint64
}

func (c *byteCounter) Write(data []byte) (int, error) {
	c.size += uint64(len(data))
	return len(data), nil
}

func writeFile(path string, write func(io.Writer) error) (fileInfo, error) {
	file, err := os.Create(path)
	if err != nil {
		return fileInfo{}, fmt.Errorf("create %s: %w", path, err)
	}
	// Closes on early returns; the success path checks Close below.
	defer func() { _ = file.Close() }()
	checksum := crc32.New(checksumTable)
	counter := &byteCounter{}
	writer := bufio.NewWriter(io.MultiWriter(file, checksum, counter))
	if err := write(writer); err != nil {
		return fileInfo{}, fmt.Errorf("write %s: %w", path, err)
	}
	if err := writer.Flush(); err != nil {
		return fileInfo{}, fmt.Errorf("flush %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fileInfo{}, fmt.Errorf("sync %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fileInfo{}, fmt.Errorf("close %s: %w", path, err)
	}
	return fileInfo{Size: counter.size, CRC32C: checksum.Sum32()}, nil
}
