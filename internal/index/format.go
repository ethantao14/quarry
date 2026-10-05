package index

import (
	"encoding/binary"
	"hash/crc32"
	"math"
)

// FormatVersion is the on-disk index format this build writes and reads.
const FormatVersion uint32 = 2

const (
	manifestName = "manifest.json"
	dictName     = "seg0.dict"
	postName     = "seg0.post"
	skipName     = "seg0.skip"
	lensName     = "seg0.lens"
	idsName      = "seg0.ids"
	dictMagic    = "QDCT"
	postMagic    = "QPST"
	skipMagic    = "QSKP"
	lensMagic    = "QLEN"
	idsMagic     = "QIDS"

	// Every binary file starts with a 4-byte magic and a uint32 version.
	headerSize = 8
	// The dictionary, lengths and IDs files then store a uint32 count.
	tableStart    = headerSize + 4
	dictEntrySize = 36
	skipEntrySize = 12
)

var checksumTable = crc32.MakeTable(crc32.Castagnoli)

type manifest struct {
	FormatVersion uint32              `json:"format_version"`
	DocCount      uint32              `json:"doc_count"`
	TotalTerms    uint64              `json:"total_terms"`
	TermCount     uint32              `json:"term_count"`
	BM25K1        *float64            `json:"bm25_k1"`
	BM25B         *float64            `json:"bm25_b"`
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
	skipOffset     uint64
	maxScore       float32
}

func (e dictEntry) appendTo(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, e.termOffset)
	dst = binary.LittleEndian.AppendUint32(dst, e.termLen)
	dst = binary.LittleEndian.AppendUint32(dst, e.docFreq)
	dst = binary.LittleEndian.AppendUint64(dst, e.postingsOffset)
	dst = binary.LittleEndian.AppendUint32(dst, e.postingsLen)
	dst = binary.LittleEndian.AppendUint64(dst, e.skipOffset)
	return binary.LittleEndian.AppendUint32(dst, math.Float32bits(e.maxScore))
}

func parseDictEntry(data []byte) dictEntry {
	return dictEntry{
		termOffset:     binary.LittleEndian.Uint32(data[0:4]),
		termLen:        binary.LittleEndian.Uint32(data[4:8]),
		docFreq:        binary.LittleEndian.Uint32(data[8:12]),
		postingsOffset: binary.LittleEndian.Uint64(data[12:20]),
		postingsLen:    binary.LittleEndian.Uint32(data[20:24]),
		skipOffset:     binary.LittleEndian.Uint64(data[24:32]),
		maxScore:       math.Float32frombits(binary.LittleEndian.Uint32(data[32:36])),
	}
}

type skipEntry struct {
	lastDocID uint32
	offset    uint32
	blockMax  float32
}

func (e skipEntry) appendTo(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, e.lastDocID)
	dst = binary.LittleEndian.AppendUint32(dst, e.offset)
	return binary.LittleEndian.AppendUint32(dst, math.Float32bits(e.blockMax))
}

func parseSkipEntry(data []byte) skipEntry {
	return skipEntry{
		lastDocID: binary.LittleEndian.Uint32(data[:4]),
		offset:    binary.LittleEndian.Uint32(data[4:8]),
		blockMax:  math.Float32frombits(binary.LittleEndian.Uint32(data[8:12])),
	}
}

// roundScoreUp returns the smallest float32 no less than x.
func roundScoreUp(x float64) float32 {
	f := float32(x)
	if float64(f) < x {
		f = math.Nextafter32(f, float32(math.Inf(1)))
	}
	return f
}
