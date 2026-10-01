package index

import (
	"encoding/binary"
	"hash/crc32"
)

// FormatVersion is the on-disk index format this build writes and reads.
const FormatVersion uint32 = 1

const (
	manifestName = "manifest.json"
	dictName     = "seg0.dict"
	postName     = "seg0.post"
	lensName     = "seg0.lens"
	idsName      = "seg0.ids"
	dictMagic    = "QDCT"
	postMagic    = "QPST"
	lensMagic    = "QLEN"
	idsMagic     = "QIDS"

	// Every binary file starts with a 4-byte magic and a uint32 version.
	headerSize = 8
	// Every binary file except seg0.post then stores a uint32 count.
	tableStart    = headerSize + 4
	dictEntrySize = 24
)

var checksumTable = crc32.MakeTable(crc32.Castagnoli)

type manifest struct {
	FormatVersion uint32              `json:"format_version"`
	DocCount      uint32              `json:"doc_count"`
	TotalTerms    uint64              `json:"total_terms"`
	TermCount     uint32              `json:"term_count"`
	Files         map[string]fileInfo `json:"files"`
}

type fileInfo struct {
	Size   uint64 `json:"size"`
	CRC32C uint32 `json:"crc32c"`
}

// dictEntry is one fixed-width row of the seg0.dict table.
type dictEntry struct {
	termOffset     uint32
	termLen        uint32
	docFreq        uint32
	postingsOffset uint64
	postingsLen    uint32
}

func (e dictEntry) appendTo(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, e.termOffset)
	dst = binary.LittleEndian.AppendUint32(dst, e.termLen)
	dst = binary.LittleEndian.AppendUint32(dst, e.docFreq)
	dst = binary.LittleEndian.AppendUint64(dst, e.postingsOffset)
	return binary.LittleEndian.AppendUint32(dst, e.postingsLen)
}

func parseDictEntry(data []byte) dictEntry {
	return dictEntry{
		termOffset:     binary.LittleEndian.Uint32(data[0:4]),
		termLen:        binary.LittleEndian.Uint32(data[4:8]),
		docFreq:        binary.LittleEndian.Uint32(data[8:12]),
		postingsOffset: binary.LittleEndian.Uint64(data[12:20]),
		postingsLen:    binary.LittleEndian.Uint32(data[20:24]),
	}
}
