// Command quarry-index saves a searchable index from a BEIR JSONL corpus.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/ethantao14/quarry/internal/corpus"
	"github.com/ethantao14/quarry/internal/index"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "quarry-index:", err)
		os.Exit(1)
	}
}

// run holds the program logic so tests can call it without starting a process.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("quarry-index", flag.ContinueOnError)
	flags.SetOutput(stderr)
	corpusPath := flags.String("corpus", "", "path to a BEIR JSONL corpus")
	out := flags.String("out", "", "path to a new index directory")
	memBudget := flags.String("mem-budget", "1GB", "chunk memory budget in bytes, KB, MB, or GB")
	workers := flags.Int("workers", runtime.GOMAXPROCS(0), "number of goroutines that analyze documents")
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if *corpusPath == "" {
		return errors.New("--corpus is required")
	}
	if *out == "" {
		return errors.New("--out is required")
	}
	if *workers < 1 {
		return errors.New("--workers must be at least 1")
	}
	budget, err := parseSize(*memBudget)
	if err != nil {
		return err
	}
	segments, err := build(*corpusPath, *out, budget, *workers)
	if err != nil {
		return err
	}
	disk, err := index.Open(*out)
	if err != nil {
		return err
	}
	docCount, termCount := disk.DocCount(), disk.TermCount()
	if err := disk.Close(); err != nil {
		return err
	}
	files, err := os.ReadDir(*out)
	if err != nil {
		return fmt.Errorf("read index directory: %w", err)
	}
	var size int64
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", file.Name(), err)
		}
		size += info.Size()
	}
	_, err = fmt.Fprintf(stdout, "docs\t%d\nterms\t%d\nsegments\t%d\nbytes\t%d\n", docCount, termCount, segments, size)
	return err
}

// build indexes the corpus into out. On failure it removes the partial index, so
// a rerun can start clean. Abort removes only the directory NewBuilder created.
func build(corpusPath, out string, budget int64, workers int) (int, error) {
	builder, err := index.NewBuilder(out, budget)
	if err != nil {
		return 0, err
	}
	err = corpus.ReadFile(corpusPath, workers, builder.Add)
	if err == nil {
		err = builder.Finish()
	}
	if err != nil {
		if removeErr := builder.Abort(); removeErr != nil {
			return 0, fmt.Errorf("%w (and removing %s failed: %v)", err, out, removeErr)
		}
		return 0, err
	}
	return builder.Segments(), nil
}

func parseSize(s string) (int64, error) {
	value := strings.ToUpper(s)
	multiplier := int64(1)
	for _, unit := range []struct {
		suffix string
		size   int64
	}{
		{"KB", 1 << 10},
		{"MB", 1 << 20},
		{"GB", 1 << 30},
	} {
		if strings.HasSuffix(value, unit.suffix) {
			value = strings.TrimSuffix(value, unit.suffix)
			multiplier = unit.size
			break
		}
	}
	if value == "" {
		return 0, fmt.Errorf("invalid memory budget %q", s)
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("invalid memory budget %q", s)
		}
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory budget %q: %w", s, err)
	}
	if size == 0 || size > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("invalid memory budget %q", s)
	}
	return size * multiplier, nil
}
