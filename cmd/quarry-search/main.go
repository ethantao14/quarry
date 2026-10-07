// Command quarry-search runs queries against a quarry index from the command line.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/corpus"
	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

const version = "dev"

// searchIndex is an index that can also map results back to corpus IDs.
type searchIndex interface {
	query.Index
	ExternalID(docID uint32) string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "quarry-search:", err)
		os.Exit(1)
	}
}

// run holds the program logic so tests can call it without starting a process.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("quarry-search", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print the version and exit")
	corpusPath := flags.String("corpus", "", "path to a BEIR JSONL corpus")
	indexPath := flags.String("index", "", "path to a saved index directory")
	algo := flags.String("algo", "bmw", "retrieval algorithm: bmw, wand, or exhaustive")
	k := flags.Int("k", 10, "number of results to return")

	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	if *showVersion {
		_, err := fmt.Fprintf(stdout, "quarry-search %s\n", version)
		return err
	}
	if err := query.CheckAlgorithm(*algo); err != nil {
		return err
	}
	if *corpusPath == "" && *indexPath == "" {
		return errors.New("--corpus or --index is required")
	}
	if *corpusPath != "" && *indexPath != "" {
		return errors.New("--corpus and --index cannot both be set")
	}
	if *k < 1 {
		return errors.New("--k must be at least 1")
	}
	queryTerms := analysis.Analyze(strings.Join(flags.Args(), " "))
	if len(queryTerms) == 0 {
		return errors.New("query must contain at least one term")
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
		ix, err = corpus.LoadFile(*corpusPath)
	}
	if err != nil {
		return err
	}
	results, err := query.Search(*algo, ix, scoring.DefaultBM25(), queryTerms, *k)
	if err != nil {
		return err
	}
	for i, result := range results {
		if _, err := fmt.Fprintf(stdout, "%d\t%s\t%.4f\n", i+1, ix.ExternalID(result.DocID), result.Score); err != nil {
			return err
		}
	}
	return nil
}
