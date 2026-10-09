// Command quarry-bench measures search latency and throughput on a saved index.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/bench"
	"github.com/ethantao14/quarry/internal/corpus"
	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "quarry-bench:", err)
		os.Exit(1)
	}
}

// run holds the program logic so tests can call it without starting a process.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("quarry-bench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	indexPath := flags.String("index", "", "path to a saved index directory")
	queriesPath := flags.String("queries", "", "BEIR JSONL or .tsv queries path")
	algoList := flags.String("algos", "exhaustive,wand,bmw", "comma list of retrieval algorithms")
	kList := flags.String("k", "10,1000", "comma list of positive result counts")
	clientList := flags.String("clients", "1,2,4,8", "comma list of positive client counts")
	warmup := flags.Int("warmup", 1, "single-client warm-up passes per algorithm and result count")
	limit := flags.Int("limit", 0, "use the first N queries in sorted ID order (0 = all)")

	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if *indexPath == "" {
		return errors.New("--index is required")
	}
	if *queriesPath == "" {
		return errors.New("--queries is required")
	}
	algos, err := algorithms(*algoList)
	if err != nil {
		return err
	}
	ks, err := positiveInts("k", *kList)
	if err != nil {
		return err
	}
	clients, err := positiveInts("clients", *clientList)
	if err != nil {
		return err
	}
	if *warmup < 0 {
		return errors.New("--warmup must be nonnegative")
	}
	if *limit < 0 {
		return errors.New("--limit must be nonnegative")
	}

	disk, err := index.Open(*indexPath)
	if err != nil {
		return err
	}
	// Unmapping a read-only mapping cannot lose data.
	defer func() { _ = disk.Close() }()
	queries, err := corpus.LoadQueries(*queriesPath)
	if err != nil {
		return err
	}
	prepared := prepareQueries(queries, *limit)
	if err := writeHeader(stdout, *indexPath, len(prepared), *warmup); err != nil {
		return err
	}
	bm25 := scoring.DefaultBM25()
	for _, algo := range algos {
		for _, k := range ks {
			search := func(i int) error {
				q := prepared[i]
				if _, err := query.Search(algo, disk, bm25, q.terms, k); err != nil {
					return fmt.Errorf("query %q: %w", q.id, err)
				}
				return nil
			}
			for range *warmup {
				if _, _, err := bench.Run(1, len(prepared), search); err != nil {
					return err
				}
			}
			for _, count := range clients {
				latencies, elapsed, err := bench.Run(count, len(prepared), search)
				if err != nil {
					return err
				}
				summary := bench.Summarize(latencies, elapsed)
				_, err = fmt.Fprintf(stdout, "%s\t%d\t%d\t%d\t%.2f\t%.1f\t%.3f\t%.3f\t%.3f\t%.3f\n",
					algo, k, count, summary.Queries, summary.Elapsed.Seconds(), summary.QPS,
					summary.MeanMS, summary.P50MS, summary.P95MS, summary.P99MS)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func algorithms(value string) ([]string, error) {
	algos := strings.Split(value, ",")
	seen := make(map[string]bool)
	for _, algo := range algos {
		if algo == "" {
			return nil, errors.New("--algos must be a comma list of algorithms")
		}
		if err := query.CheckAlgorithm(algo); err != nil {
			return nil, err
		}
		if seen[algo] {
			return nil, fmt.Errorf("--algos must not contain duplicates: %q", algo)
		}
		seen[algo] = true
	}
	return algos, nil
}

func positiveInts(name, value string) ([]int, error) {
	var values []int
	for _, field := range strings.Split(value, ",") {
		n, err := strconv.Atoi(field)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("--%s must be a comma list of positive integers", name)
		}
		values = append(values, n)
	}
	return values, nil
}

type preparedQuery struct {
	id    string
	terms []string
}

func prepareQueries(queries map[string]string, limit int) []preparedQuery {
	ids := make([]string, 0, len(queries))
	for id := range queries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if limit > 0 && limit < len(ids) {
		ids = ids[:limit]
	}
	prepared := make([]preparedQuery, len(ids))
	for i, id := range ids {
		prepared[i] = preparedQuery{id: id, terms: analysis.Analyze(queries[id])}
	}
	return prepared
}

func writeHeader(w io.Writer, indexPath string, queries, warmup int) error {
	for _, line := range bench.Hardware() {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	cache := "not primed"
	if warmup > 0 {
		cache = "warm (OS page cache primed by warm-up passes)"
	}
	_, err := fmt.Fprintf(w, "index\t%s\nqueries\t%d\nwarmup_passes\t%d\ncache\t%s\n\n"+
		"algo\tk\tclients\tqueries\tseconds\tqps\tmean_ms\tp50_ms\tp95_ms\tp99_ms\n",
		indexPath, queries, warmup, cache)
	return err
}
