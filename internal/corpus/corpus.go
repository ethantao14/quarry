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
	if err := Read(r, func(externalID string, terms []string) error {
		ix.Add(externalID, terms)
		return nil
	}); err != nil {
		return nil, err
	}
	return ix, nil
}

// Read analyzes each BEIR JSONL record and calls visit in document order.
// A decoding or visit error stops reading and includes the record number.
func Read(r io.Reader, visit func(externalID string, terms []string) error) error {
	decoder := json.NewDecoder(r)
	for recordNumber := 1; ; recordNumber++ {
		var record struct {
			ID    string `json:"_id"`
			Title string `json:"title"`
			Text  string `json:"text"`
		}
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("record %d: %w", recordNumber, err)
		}
		if record.ID == "" {
			return fmt.Errorf("record %d: _id is empty", recordNumber)
		}
		if err := visit(record.ID, analysis.Analyze(record.Title+" "+record.Text)); err != nil {
			return fmt.Errorf("record %d: %w", recordNumber, err)
		}
	}
}

// LoadFile opens a corpus file and indexes it with Load.
func LoadFile(path string) (*index.Index, error) {
	ix := index.New()
	if err := ReadFile(path, func(externalID string, terms []string) error {
		ix.Add(externalID, terms)
		return nil
	}); err != nil {
		return nil, err
	}
	return ix, nil
}

// ReadFile opens a corpus file and visits its documents with Read.
func ReadFile(path string, visit func(externalID string, terms []string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open corpus: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()
	if err := Read(file, visit); err != nil {
		return fmt.Errorf("load corpus %s: %w", path, err)
	}
	return nil
}
