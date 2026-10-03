// Command quarry-eval writes and evaluates a TREC run from a BEIR dataset or saved index.
// Queries can be BEIR JSONL or .tsv (id<TAB>text); qrels can be BEIR TSV or TREC.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/corpus"
	"github.com/ethantao14/quarry/internal/eval"
	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

// searchIndex is an index that can also map results back to corpus IDs.
type searchIndex interface {
	query.Index
	ExternalID(docID uint32) string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "quarry-eval:", err)
		os.Exit(1)
	}
}

// run holds the program logic so tests can call it without starting a process.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("quarry-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataset := flags.String("dataset", "", "BEIR directory for the corpus and default queries and qrels paths")
	indexPath := flags.String("index", "", "path to a saved index directory")
	queriesPath := flags.String("queries", "", "BEIR JSONL or .tsv queries path (default <dataset>/queries.jsonl)")
	qrelsPath := flags.String("qrels", "", "BEIR TSV or TREC qrels path (default <dataset>/qrels/test.tsv)")
	runPath := flags.String("run", "", "path to the output TREC run file")
	k := flags.Int("k", 1000, "number of results per query")

	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if *dataset == "" && (*indexPath == "" || *queriesPath == "" || *qrelsPath == "") {
		return errors.New("--dataset is required unless --index, --queries, and --qrels are all set")
	}
	if *runPath == "" {
		return errors.New("--run is required")
	}
	if *k < 1 {
		return errors.New("--k must be at least 1")
	}
	if *queriesPath == "" {
		*queriesPath = filepath.Join(*dataset, "queries.jsonl")
	}
	if *qrelsPath == "" {
		*qrelsPath = filepath.Join(*dataset, "qrels", "test.tsv")
	}

	var ix searchIndex
	if *indexPath != "" {
		disk, err := index.Open(*indexPath)
		if err != nil {
			return err
		}
		// Unmapping a read-only mapping cannot lose data.
		defer func() { _ = disk.Close() }()
		ix = disk
	} else {
		ix, err = corpus.LoadFile(filepath.Join(*dataset, "corpus.jsonl"))
	}
	if err != nil {
		return err
	}
	queries, err := corpus.LoadQueries(*queriesPath)
	if err != nil {
		return err
	}
	qrels, err := loadQrels(*qrelsPath)
	if err != nil {
		return err
	}

	if err := writeRun(*runPath, ix, queries, qrels, *k); err != nil {
		return err
	}
	// Score the saved file, so metrics see scores exactly as trec_eval would.
	summary, err := evaluateRunFile(*runPath, qrels)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "queries\t%d\nnDCG@10\t%.4f\nR@100\t%.4f\nR@1000\t%.4f\nMRR@10\t%.4f\n",
		summary.Queries, summary.NDCG10, summary.Recall100, summary.Recall1000, summary.MRR10)
	return err
}

func loadQrels(path string) (eval.Qrels, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open qrels: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()

	qrels, err := eval.ReadQrels(file)
	if err != nil {
		return nil, fmt.Errorf("read qrels: %w", err)
	}
	return qrels, nil
}

// writeRun searches every judged query, in sorted ID order, and writes a TREC run file.
func writeRun(path string, ix searchIndex, queries map[string]string, qrels eval.Qrels, k int) error {
	queryIDs := make([]string, 0, len(qrels))
	for queryID := range qrels {
		if _, ok := queries[queryID]; !ok {
			return fmt.Errorf("query %q in qrels is missing from queries", queryID)
		}
		queryIDs = append(queryIDs, queryID)
	}
	sort.Strings(queryIDs)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	// Closes on early returns; the success path checks Close below.
	defer func() { _ = file.Close() }()

	for _, queryID := range queryIDs {
		terms := analysis.Analyze(queries[queryID])
		results, err := query.Exhaustive(ix, scoring.DefaultBM25(), terms, k)
		if err != nil {
			return fmt.Errorf("query %q: %w", queryID, err)
		}
		entries := make([]eval.RunEntry, len(results))
		for i, result := range results {
			entries[i] = eval.RunEntry{DocID: ix.ExternalID(result.DocID), Score: result.Score}
		}
		if err := eval.WriteRun(file, queryID, entries, "quarry"); err != nil {
			return fmt.Errorf("write run: %w", err)
		}
	}
	// A failed Close on a written file can mean lost data, so it is an error.
	if err := file.Close(); err != nil {
		return fmt.Errorf("close run: %w", err)
	}
	return nil
}

func evaluateRunFile(path string, qrels eval.Qrels) (eval.Summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return eval.Summary{}, fmt.Errorf("open run: %w", err)
	}
	// The file is only read, so a failed Close cannot lose data.
	defer func() { _ = file.Close() }()

	savedRun, err := eval.ReadRun(file)
	if err != nil {
		return eval.Summary{}, fmt.Errorf("read run: %w", err)
	}
	return eval.Evaluate(savedRun, qrels), nil
}
