package corpus

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// ReadQueriesTSV reads id<TAB>text queries, skipping empty lines.
func ReadQueriesTSV(r io.Reader) (map[string]string, error) {
	queries := make(map[string]string)
	source := &tsvReader{reader: bufio.NewReader(r)}
	for {
		lineNumber, id, text, err := source.next()
		if errors.Is(err, io.EOF) {
			return queries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		queries[id] = text
	}
}

// LoadQueries reads BEIR JSONL queries, or TSV when the file ends in .tsv.
func LoadQueries(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open queries: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()

	read := ReadQueries
	if strings.EqualFold(filepath.Ext(path), ".tsv") {
		read = ReadQueriesTSV
	}
	queries, err := read(file)
	if err != nil {
		return nil, fmt.Errorf("load queries %s: %w", path, err)
	}
	return queries, nil
}
