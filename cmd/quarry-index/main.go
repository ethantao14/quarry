// Command quarry-index saves a searchable index from a BEIR JSONL corpus.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ethantao14/quarry/internal/corpus"
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
	ix, err := corpus.LoadFile(*corpusPath)
	if err != nil {
		return err
	}
	if err := ix.Write(*out); err != nil {
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
	_, err = fmt.Fprintf(stdout, "docs\t%d\nterms\t%d\nbytes\t%d\n", ix.DocCount(), ix.TermCount(), size)
	return err
}
