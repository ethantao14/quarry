package index

import (
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
)

// textWriter streams document text to seg0.text in doc ID order, and each
// document's end offset to seg0.toff, so no text is kept in memory.
type textWriter struct {
	text    *checkedFile
	offsets *checkedFile
	offset  uint64
}

func newTextWriter(dir string) (*textWriter, error) {
	text, err := newCheckedFile(filepath.Join(dir, textName), durable)
	if err != nil {
		return nil, err
	}
	offsets, err := newCheckedFile(filepath.Join(dir, textOffsetsName), durable)
	if err != nil {
		text.abort()
		return nil, err
	}
	w := &textWriter{text: text, offsets: offsets}
	header := binary.LittleEndian.AppendUint32([]byte(textMagic), FormatVersion)
	if _, err := text.Write(header); err != nil {
		w.abort()
		return nil, fmt.Errorf("write %s: %w", text.path, err)
	}
	header = binary.LittleEndian.AppendUint32([]byte(textOffsetMagic), FormatVersion)
	// The first document starts at offset 0.
	header = binary.LittleEndian.AppendUint64(header, 0)
	if _, err := offsets.Write(header); err != nil {
		w.abort()
		return nil, fmt.Errorf("write %s: %w", offsets.path, err)
	}
	return w, nil
}

func (w *textWriter) add(text string) error {
	if _, err := io.WriteString(w.text, text); err != nil {
		return fmt.Errorf("write %s: %w", w.text.path, err)
	}
	w.offset += uint64(len(text))
	if _, err := w.offsets.Write(binary.LittleEndian.AppendUint64(nil, w.offset)); err != nil {
		return fmt.Errorf("write %s: %w", w.offsets.path, err)
	}
	return nil
}

// finish closes both files and returns their manifest entries.
func (w *textWriter) finish() (map[string]fileInfo, error) {
	defer w.abort()
	textInfo, err := w.text.finish()
	if err != nil {
		return nil, err
	}
	offsetsInfo, err := w.offsets.finish()
	if err != nil {
		return nil, err
	}
	return map[string]fileInfo{textName: textInfo, textOffsetsName: offsetsInfo}, nil
}

func (w *textWriter) abort() {
	w.text.abort()
	w.offsets.abort()
}

// validateText checks the optional text files against the document count.
func (d *Disk) validateText() error {
	if d.textOffsets == nil {
		return nil
	}
	if uint64(len(d.textOffsets)) != headerSize+8*(uint64(d.docCount)+1) {
		return fmt.Errorf("%s: size does not match document count", textOffsetsName)
	}
	blobLen := uint64(len(d.text)) - headerSize
	var previous uint64
	for i := uint32(0); i <= d.docCount; i++ {
		offset := d.textOffset(i)
		if (i == 0 && offset != 0) || offset < previous || offset > blobLen {
			return fmt.Errorf("%s: invalid offset %d", textOffsetsName, i)
		}
		previous = offset
	}
	if previous != blobLen {
		return fmt.Errorf("%s: final offset does not match text length", textOffsetsName)
	}
	return nil
}

func (d *Disk) textOffset(i uint32) uint64 {
	start := headerSize + 8*uint64(i)
	return binary.LittleEndian.Uint64(d.textOffsets[start : start+8])
}

// HasText reports whether the index stores document text (quarry-index --store-text).
func (d *Disk) HasText() bool {
	return d.textOffsets != nil
}

// Text returns a document's stored text, or "" if the index stores none.
func (d *Disk) Text(docID uint32) string {
	if docID >= d.docCount {
		panic("document ID out of range")
	}
	if d.textOffsets == nil {
		return ""
	}
	return string(d.text[headerSize+d.textOffset(docID) : headerSize+d.textOffset(docID+1)])
}
