// Package corpus reads benchmark document collections into an index.
package corpus

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/index"
)

// Load indexes a BEIR-format JSONL corpus, one {"_id", "title", "text"} object per line.
// Title and text are indexed together as one field.
func Load(r io.Reader) (*index.Index, error) {
	ix := index.New()
	if err := Read(r, runtime.GOMAXPROCS(0), func(externalID string, terms []string) error {
		ix.Add(externalID, terms)
		return nil
	}); err != nil {
		return nil, err
	}
	return ix, nil
}

// job is one record on its way through the pipeline. Each job has its own result
// channel, so the consumer can wait for records in input order.
type job struct {
	position int
	id       string
	text     string
	err      error
	result   chan []string
}

// Read analyzes BEIR JSONL records in parallel and calls visit in input order on the calling goroutine.
// Decoding and visit errors include the record number and are returned in input order.
// Read waits for all its goroutines, including any in-progress r.Read call, before returning.
func Read(r io.Reader, workers int, visit func(externalID string, terms []string) error) error {
	decoder := json.NewDecoder(r)
	recordNumber := 0
	next := func() (int, string, string, error) {
		recordNumber++
		var record struct {
			ID    string `json:"_id"`
			Title string `json:"title"`
			Text  string `json:"text"`
		}
		err := decoder.Decode(&record)
		if err == nil && record.ID == "" {
			err = errors.New("_id is empty")
		}
		return recordNumber, record.ID, record.Title + " " + record.Text, err
	}
	return readRecords(next, "record", workers, visit)
}

// ReadTSV analyzes id<TAB>text lines in parallel and calls visit in input order.
// Empty lines are skipped and errors include the input line number.
// ReadTSV waits for all its goroutines before returning.
func ReadTSV(r io.Reader, workers int, visit func(externalID string, terms []string) error) error {
	source := &tsvReader{reader: bufio.NewReader(r)}
	return readRecords(source.next, "line", workers, visit)
}

// tsvReader reads id<TAB>text lines and tracks the 1-based line number.
type tsvReader struct {
	reader     *bufio.Reader
	lineNumber int
}

// next returns the next non-empty line's number, id, and text, or io.EOF at the end.
func (r *tsvReader) next() (int, string, string, error) {
	for {
		line, err := r.reader.ReadString('\n')
		r.lineNumber++
		if err != nil && !errors.Is(err, io.EOF) {
			return r.lineNumber, "", "", err
		}
		if len(line) == 0 && errors.Is(err, io.EOF) {
			return r.lineNumber, "", "", io.EOF
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if !utf8.ValidString(line) {
			return r.lineNumber, "", "", errors.New("invalid UTF-8")
		}
		id, text, ok := strings.Cut(line, "\t")
		if !ok {
			return r.lineNumber, "", "", errors.New("missing tab")
		}
		if id == "" {
			return r.lineNumber, "", "", errors.New("id is empty")
		}
		// id is a substring of line; cloning it lets a kept ID release the whole line.
		return r.lineNumber, strings.Clone(id), text, nil
	}
}

// recordSource returns the next record and its position (record or line number), or io.EOF.
type recordSource func() (position int, id, text string, err error)

// readRecords runs the ordered pipeline; label names the position in errors ("record" or "line").
func readRecords(next recordSource, label string, workers int, visit func(string, []string) error) error {
	if workers < 1 {
		return errors.New("workers must be at least 1")
	}
	jobs := make(chan job)
	// ordered's capacity bounds how many records are in flight at once.
	ordered := make(chan job, 16*workers)
	done := make(chan struct{})
	var wg sync.WaitGroup
	defer func() {
		close(done)
		wg.Wait()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		decodeJobs(next, jobs, ordered, done)
	}()
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			analyzeJobs(jobs, done)
		}()
	}

	for pending := range ordered {
		if pending.err != nil {
			return fmt.Errorf("%s %d: %w", label, pending.position, pending.err)
		}
		terms := <-pending.result
		if err := visit(pending.id, terms); err != nil {
			return fmt.Errorf("%s %d: %w", label, pending.position, err)
		}
	}
	return nil
}

// decodeJobs sends each record to jobs for analysis and to ordered for the consumer.
// A record that fails to decode goes only to ordered, and then decoding stops.
func decodeJobs(next recordSource, jobs, ordered chan<- job, done <-chan struct{}) {
	defer close(jobs)
	defer close(ordered)
	for {
		position, id, text, err := next()
		if errors.Is(err, io.EOF) {
			return
		}
		pending := job{position: position, err: err}
		if err == nil {
			pending.id = id
			pending.text = text
			pending.result = make(chan []string, 1)
			select {
			case jobs <- pending:
			case <-done:
				return
			}
		}
		select {
		case ordered <- pending:
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}

// analyzeJobs analyzes jobs until jobs is closed or done is closed.
// Sending a result never blocks because each result channel holds one value.
func analyzeJobs(jobs <-chan job, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case pending, ok := <-jobs:
			if !ok {
				return
			}
			pending.result <- analysis.Analyze(pending.text)
		}
	}
}

// LoadFile indexes a BEIR JSONL corpus, or TSV when the file ends in .tsv.
func LoadFile(path string) (*index.Index, error) {
	ix := index.New()
	if err := ReadFile(path, runtime.GOMAXPROCS(0), func(externalID string, terms []string) error {
		ix.Add(externalID, terms)
		return nil
	}); err != nil {
		return nil, err
	}
	return ix, nil
}

// ReadFile visits a BEIR JSONL corpus, or TSV when the file ends in .tsv.
func ReadFile(path string, workers int, visit func(externalID string, terms []string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open corpus: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()
	read := Read
	if strings.EqualFold(filepath.Ext(path), ".tsv") {
		read = ReadTSV
	}
	if err := read(file, workers, visit); err != nil {
		return fmt.Errorf("load corpus %s: %w", path, err)
	}
	return nil
}
