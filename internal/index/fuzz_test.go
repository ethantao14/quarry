package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethantao14/quarry/internal/query"
)

// fuzzFiles are the files FuzzOpen may replace. Binary files get a matching
// manifest checksum, so the fuzzer reaches the validation behind it.
var fuzzFiles = []string{dictName, postName, skipName, lensName, idsName, textName, textOffsetsName, manifestName}

// FuzzOpen checks that no index contents can make Open or reads panic.
func FuzzOpen(f *testing.F) {
	source := filepath.Join(f.TempDir(), "idx")
	docs := []textDoc{{"first", "a a b", []string{"a", "a", "b"}}, {"", "", nil}, {"三", "b 猫", []string{"b", "猫"}}}
	buildTextIndex(f, source, docs, 1<<20, true)
	for i, name := range fuzzFiles {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(uint8(i), data)
		f.Add(uint8(i), data[:len(data)/2])
		flipped := append([]byte(nil), data...)
		flipped[len(flipped)-1] ^= 0x40
		f.Add(uint8(i), flipped)
	}
	f.Fuzz(func(t *testing.T, file uint8, data []byte) {
		dir := copyTestIndex(t, source)
		name := fuzzFiles[int(file)%len(fuzzFiles)]
		if name == manifestName {
			writeTestFile(t, dir, name, data)
		} else {
			rewriteFile(t, dir, name, data)
		}
		disk, err := Open(dir)
		if err != nil {
			return
		}
		cleanupDisk(t, disk)
		readEverything(t, disk)
	})
}

// readEverything touches every document and posting through each read path.
// Errors are allowed; only panics fail.
func readEverything(t *testing.T, disk *Disk) {
	t.Helper()
	_ = disk.AvgDocLen()
	for doc := 0; doc < disk.DocCount(); doc++ {
		_ = disk.DocLen(uint32(doc))
		_ = disk.ExternalID(uint32(doc))
		_ = disk.Text(uint32(doc))
	}
	for i := 0; i < disk.TermCount(); i++ {
		term := string(disk.term(disk.entry(uint32(i))))
		_, _ = disk.Postings(term)
		walkCursor(disk, term, func(c query.Cursor) error { return c.Next() })
		walkCursor(disk, term, func(c query.Cursor) error {
			target := min(uint64(c.DocID())+2, query.NoMoreDocs)
			c.ShallowAdvance(uint32(target))
			_, _ = c.BlockMax()
			return c.Advance(uint32(target))
		})
	}
}

func walkCursor(disk *Disk, term string, step func(query.Cursor) error) {
	c, err := disk.Cursor(term)
	if err != nil || c == nil {
		return
	}
	for c.DocID() != query.NoMoreDocs {
		_ = c.TF()
		if step(c) != nil {
			return
		}
	}
}
