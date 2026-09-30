// Package corpus reads benchmark document collections into an index.
package corpus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/index"
)

// Load indexes a BEIR-format JSONL corpus, one {"_id", "title", "text"} object per line.
// Title and text are indexed together as one field.
func Load(r io.Reader) (*index.Index, error) {
	ix := index.New()
	decoder := json.NewDecoder(r)
	for recordNumber := 1; ; recordNumber++ {
		var record struct {
			ID    string `json:"_id"`
			Title string `json:"title"`
			Text  string `json:"text"`
		}
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return ix, nil
		}
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", recordNumber, err)
		}
		if record.ID == "" {
			return nil, fmt.Errorf("record %d: _id is empty", recordNumber)
		}
		ix.Add(record.ID, analysis.Analyze(record.Title+" "+record.Text))
	}
}

// LoadFile opens a corpus file and indexes it with Load.
func LoadFile(path string) (*index.Index, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open corpus: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()

	ix, err := Load(file)
	if err != nil {
		return nil, fmt.Errorf("load corpus %s: %w", path, err)
	}
	return ix, nil
}
