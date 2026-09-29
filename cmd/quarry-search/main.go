// Command quarry-search runs queries against a quarry index from the command line.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	return errors.New("search is not implemented yet")
}
