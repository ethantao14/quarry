package eval

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const beirQrelsHeader = "query-id\tcorpus-id\tscore"

// ReadQrels reads BEIR judgments with a query-id, corpus-id, score TSV header,
// or four-field TREC qrels without a header. Blank lines are skipped.
func ReadQrels(r io.Reader) (Qrels, error) {
	scanner := bufio.NewScanner(r)
	qrels := make(Qrels)
	lineNumber := 0
	// The first non-blank line decides the format.
	var parseLine func(line string) (queryID, docID, grade string, err error)
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if parseLine == nil {
			if line == beirQrelsHeader {
				parseLine = parseBEIRQrel
				continue
			}
			parseLine = parseTRECQrel
		}
		queryID, docID, gradeText, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		grade, err := strconv.Atoi(gradeText)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid grade: %w", lineNumber, err)
		}
		if qrels[queryID] == nil {
			qrels[queryID] = make(map[string]int)
		}
		qrels[queryID][docID] = grade
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
	}
	if parseLine == nil {
		return nil, fmt.Errorf("line 1: missing qrels header or data")
	}
	return qrels, nil
}

// parseBEIRQrel splits a query-id, corpus-id, score line.
func parseBEIRQrel(line string) (queryID, docID, grade string, err error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 3 {
		return "", "", "", fmt.Errorf("expected 3 tab-separated fields, got %d", len(fields))
	}
	return fields[0], fields[1], fields[2], nil
}

// parseTRECQrel splits a query-id, iteration, doc-id, grade line; the iteration is ignored.
func parseTRECQrel(line string) (queryID, docID, grade string, err error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return "", "", "", fmt.Errorf("expected 4 fields, got %d", len(fields))
	}
	return fields[0], fields[2], fields[3], nil
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
