package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/corpus"
)

func saveTinyIndex(t *testing.T) string {
	t.Helper()
	ix, err := corpus.LoadFile("testdata/corpus.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun(t *testing.T) {
	indexPath := saveTinyIndex(t)
	for _, queriesPath := range []string{"testdata/queries.tsv", "testdata/queries.jsonl"} {
		t.Run(queriesPath, func(t *testing.T) {
			args := []string{"--index", indexPath, "--queries", queriesPath,
				"--clients", "1,3", "--k", "1,5", "--algos", "exhaustive,bmw", "--warmup", "1"}
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			header, rows := parseOutput(t, stdout.String())
			for key, want := range map[string]string{
				"index": indexPath, "queries": "3", "warmup_passes": "1",
				"cache": "warm (OS page cache primed by warm-up passes)",
			} {
				if header[key] != want {
					t.Errorf("header[%q] = %q, want %q", key, header[key], want)
				}
			}
			if len(rows) != 8 {
				t.Fatalf("rows = %d, want 8", len(rows))
			}
			i := 0
			for _, algo := range []string{"exhaustive", "bmw"} {
				for _, k := range []string{"1", "5"} {
					for _, clients := range []string{"1", "3"} {
						want := []string{algo, k, clients, "3"}
						if !reflect.DeepEqual(rows[i][:4], want) {
							t.Errorf("row %d = %v, want prefix %v", i, rows[i], want)
						}
						i++
					}
				}
			}
		})
	}
}

func parseOutput(t *testing.T, output string) (map[string]string, [][]string) {
	t.Helper()
	metadata, table, found := strings.Cut(output, "\n\n")
	if !found {
		t.Fatalf("missing blank line in output %q", output)
	}
	header := make(map[string]string)
	lines := strings.Split(metadata, "\n")
	keys := []string{"cpu", "cores", "memory_gib", "os", "go", "index", "queries", "warmup_passes", "cache"}
	if len(lines) != len(keys) {
		t.Fatalf("header lines = %d, want %d", len(lines), len(keys))
	}
	for i, line := range lines {
		key, value, found := strings.Cut(line, "\t")
		if !found || key != keys[i] || value == "" {
			t.Fatalf("invalid header line %q, want key %q", line, keys[i])
		}
		header[key] = value
	}
	lines = strings.Split(strings.TrimSuffix(table, "\n"), "\n")
	wantHeader := "algo\tk\tclients\tqueries\tseconds\tqps\tmean_ms\tp50_ms\tp95_ms\tp99_ms"
	if lines[0] != wantHeader {
		t.Fatalf("table header = %q, want %q", lines[0], wantHeader)
	}
	var rows [][]string
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 10 {
			t.Fatalf("row = %q, want 10 fields", line)
		}
		for _, field := range fields[1:4] {
			if value, err := strconv.Atoi(field); err != nil || value < 0 {
				t.Fatalf("invalid integer %q", field)
			}
		}
		values := make([]float64, 6)
		for i, field := range fields[4:] {
			value, err := strconv.ParseFloat(field, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				t.Fatalf("invalid number %q", field)
			}
			_, decimals, _ := strings.Cut(field, ".")
			if len(decimals) != []int{2, 1, 3, 3, 3, 3}[i] {
				t.Errorf("incorrect decimal places in field %d: %q", i+4, field)
			}
			values[i] = value
		}
		if values[3] > values[4] || values[4] > values[5] {
			t.Errorf("unordered percentiles in %q", line)
		}
		rows = append(rows, fields)
	}
	return header, rows
}

func TestRunFlags(t *testing.T) {
	base := []string{"--index", "unused", "--queries", "unused.tsv"}
	for _, tt := range []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "help", args: []string{"--help"}},
		{name: "no arguments", wantErr: "--index is required"},
		{name: "missing index", args: []string{"--queries", "unused.tsv"}, wantErr: "--index is required"},
		{name: "missing queries", args: []string{"--index", "unused"}, wantErr: "--queries is required"},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: "flag provided but not defined"},
		{name: "unknown algorithm", args: append(base, "--algos", "exhaustive,nope"), wantErr: "--algo must be exhaustive, wand, or bmw"},
		{name: "duplicate algorithm", args: append(base, "--algos", "bmw,bmw"), wantErr: "--algos must not contain duplicates"},
		{name: "empty algorithms", args: append(base, "--algos", ""), wantErr: "--algos must be a comma list of algorithms"},
		{name: "empty algorithm entry", args: append(base, "--algos", "bmw,,wand"), wantErr: "--algos must be a comma list of algorithms"},
		{name: "negative warmup", args: append(base, "--warmup", "-1"), wantErr: "--warmup must be nonnegative"},
		{name: "negative limit", args: append(base, "--limit", "-1"), wantErr: "--limit must be nonnegative"},
		{name: "bad warmup", args: append(base, "--warmup", "bad"), wantErr: "invalid value"},
		{name: "bad limit", args: append(base, "--limit", "bad"), wantErr: "invalid value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			checkFlagError(t, tt.args, tt.wantErr)
		})
	}
	for _, flag := range []string{"k", "clients"} {
		for _, value := range []string{"", "0", "-1", "1,0", "1,-2", "bad", "1.5", "1,", ",1", "1,,3", "999999999999999999999999"} {
			t.Run(flag+"="+value, func(t *testing.T) {
				checkFlagError(t, append(base, "--"+flag, value), "--"+flag+" must be a comma list of positive integers")
			})
		}
	}
}

func checkFlagError(t *testing.T, args []string, wantErr string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if wantErr != "" {
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("run(%q) error = %v, want %q", args, err, wantErr)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunLimit(t *testing.T) {
	indexPath := saveTinyIndex(t)
	for _, tt := range []struct{ limit, want int }{{0, 3}, {1, 1}, {2, 2}, {10, 3}} {
		t.Run(strconv.Itoa(tt.limit), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := []string{"--index", indexPath, "--queries", "testdata/queries.tsv", "--algos", "wand",
				"--k", "1", "--clients", "1", "--warmup", "0", "--limit", strconv.Itoa(tt.limit)}
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			header, rows := parseOutput(t, stdout.String())
			if len(rows) != 1 || rows[0][3] != strconv.Itoa(tt.want) || header["queries"] != strconv.Itoa(tt.want) {
				t.Fatalf("output has wrong query count: %s", stdout.String())
			}
			if header["cache"] != "not primed" || header["warmup_passes"] != "0" {
				t.Errorf("unexpected cache metadata: %v", header)
			}
		})
	}
}

func TestPrepareQueries(t *testing.T) {
	queries, err := corpus.LoadQueries("testdata/queries.tsv")
	if err != nil {
		t.Fatal(err)
	}
	want := []preparedQuery{{id: "q1", terms: []string{"fish"}}, {id: "q10", terms: []string{"miss"}}}
	if got := prepareQueries(queries, 2); !reflect.DeepEqual(got, want) {
		t.Errorf("prepareQueries() = %v, want %v", got, want)
	}
}

func TestRunEmptyQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.tsv")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--index", saveTinyIndex(t), "--queries", path}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	header, rows := parseOutput(t, stdout.String())
	if header["queries"] != "0" || len(rows) != 24 {
		t.Fatalf("unexpected empty output: %s", stdout.String())
	}
	for i, row := range rows {
		algo := []string{"exhaustive", "wand", "bmw"}[i/8]
		k := []int{10, 1000}[(i/4)%2]
		clients := []int{1, 2, 4, 8}[i%4]
		want := fmt.Sprintf("%s\t%d\t%d\t0\t0.00\t0.0\t0.000\t0.000\t0.000\t0.000", algo, k, clients)
		if got := strings.Join(row, "\t"); got != want {
			t.Errorf("row = %q, want %q", got, want)
		}
	}
}
