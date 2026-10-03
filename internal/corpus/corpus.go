// Package corpus reads benchmark document collections into an index.
package corpus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"

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

// job is one record on its way through Read. Each job has its own result
// channel, so the consumer can wait for records in input order.
type job struct {
	recordNumber int
	id           string
	text         string
	err          error
	result       chan []string
}

// Read analyzes BEIR JSONL records in parallel and calls visit in input order on the calling goroutine.
// Decoding and visit errors include the record number and are returned in input order.
// Read waits for all its goroutines, including any in-progress r.Read call, before returning.
func Read(r io.Reader, workers int, visit func(externalID string, terms []string) error) error {
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
		decodeJobs(r, jobs, ordered, done)
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
			return fmt.Errorf("record %d: %w", pending.recordNumber, pending.err)
		}
		terms := <-pending.result
		if err := visit(pending.id, terms); err != nil {
			return fmt.Errorf("record %d: %w", pending.recordNumber, err)
		}
	}
	return nil
}

// decodeJobs sends each record to jobs for analysis and to ordered for the consumer.
// A record that fails to decode goes only to ordered, and then decoding stops.
func decodeJobs(r io.Reader, jobs, ordered chan<- job, done <-chan struct{}) {
	defer close(jobs)
	defer close(ordered)
	decoder := json.NewDecoder(r)
	for recordNumber := 1; ; recordNumber++ {
		var record struct {
			ID    string `json:"_id"`
			Title string `json:"title"`
			Text  string `json:"text"`
		}
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return
		}
		if err == nil && record.ID == "" {
			err = errors.New("_id is empty")
		}
		pending := job{recordNumber: recordNumber, err: err}
		if err == nil {
			pending.id = record.ID
			pending.text = record.Title + " " + record.Text
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

// LoadFile opens a corpus file and indexes it with Load.
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

// ReadFile opens a corpus file and visits its documents with Read.
func ReadFile(path string, workers int, visit func(externalID string, terms []string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open corpus: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()
	if err := Read(file, workers, visit); err != nil {
		return fmt.Errorf("load corpus %s: %w", path, err)
	}
	return nil
}
