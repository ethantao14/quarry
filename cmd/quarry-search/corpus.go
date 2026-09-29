package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/index"
)

// loadCorpus indexes a BEIR-format JSONL corpus, one {"_id", "title", "text"} object per line.
// Title and text are indexed together as one field.
func loadCorpus(r io.Reader) (*index.Index, error) {
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
		ix.Add(record.ID, analysis.Tokenize(record.Title+" "+record.Text))
	}
}
