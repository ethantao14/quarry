package corpus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadQueries reads BEIR JSONL queries and maps each _id to its text.
func ReadQueries(r io.Reader) (map[string]string, error) {
	queries := make(map[string]string)
	decoder := json.NewDecoder(r)
	for recordNumber := 1; ; recordNumber++ {
		var record struct {
			ID   string `json:"_id"`
			Text string `json:"text"`
		}
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return queries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", recordNumber, err)
		}
		if record.ID == "" {
			return nil, fmt.Errorf("record %d: _id is empty", recordNumber)
		}
		queries[record.ID] = record.Text
	}
}

// LoadQueries opens a queries file and reads it with ReadQueries.
func LoadQueries(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open queries: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()

	queries, err := ReadQueries(file)
	if err != nil {
		return nil, fmt.Errorf("load queries %s: %w", path, err)
	}
	return queries, nil
}
