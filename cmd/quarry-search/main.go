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
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

const version = "dev"

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
	if *corpusPath == "" {
		return errors.New("--corpus is required")
	}
	if *k < 1 {
		return errors.New("--k must be at least 1")
	}
	queryTerms := analysis.Analyze(strings.Join(flags.Args(), " "))
	if len(queryTerms) == 0 {
		return errors.New("query must contain at least one term")
	}

	ix, err := corpus.LoadFile(*corpusPath)
	if err != nil {
		return err
	}
	results := query.Exhaustive(ix, scoring.DefaultBM25(), queryTerms, *k)
	for i, result := range results {
		if _, err := fmt.Fprintf(stdout, "%d\t%s\t%.4f\n", i+1, ix.ExternalID(result.DocID), result.Score); err != nil {
			return err
		}
	}
	return nil
}
