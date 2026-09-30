package eval

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ReadQrels reads BEIR relevance judgments with a query-id, corpus-id, score TSV header.
func ReadQrels(r io.Reader) (Qrels, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("line 1: %w", err)
		}
		return nil, fmt.Errorf("line 1: missing qrels header")
	}
	if scanner.Text() != "query-id\tcorpus-id\tscore" {
		return nil, fmt.Errorf("line 1: expected query-id\tcorpus-id\tscore header")
	}
	qrels := make(Qrels)
	lineNumber := 1
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("line %d: expected 3 tab-separated fields, got %d", lineNumber, len(fields))
		}
		grade, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid grade: %w", lineNumber, err)
		}
		if qrels[fields[0]] == nil {
			qrels[fields[0]] = make(map[string]int)
		}
		qrels[fields[0]][fields[1]] = grade
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
	}
	return qrels, nil
}

// WriteRun writes entries in their given order as six-column TREC run lines.
func WriteRun(w io.Writer, queryID string, entries []RunEntry, tag string) error {
	for i, entry := range entries {
		if _, err := fmt.Fprintf(w, "%s Q0 %s %d %.6f %s\n", queryID, entry.DocID, i+1, entry.Score, tag); err != nil {
			return err
		}
	}
	return nil
}

// ReadRun reads query IDs, document IDs, and scores from a six-column TREC run.
func ReadRun(r io.Reader) (Run, error) {
	scanner := bufio.NewScanner(r)
	run := make(Run)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 6 {
			return nil, fmt.Errorf("line %d: expected 6 fields, got %d", lineNumber, len(fields))
		}
		score, err := strconv.ParseFloat(fields[4], 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid score: %w", lineNumber, err)
		}
		run[fields[0]] = append(run[fields[0]], RunEntry{DocID: fields[2], Score: score})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
	}
	return run, nil
}
