package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethantao14/quarry/internal/corpus"
)

func TestRunFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "help", args: []string{"-h"}},
		{name: "invalid algo", args: []string{"--algo", "invalid"}, wantErr: "--algo must be exhaustive or wand"},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: "flag provided but not defined"},
		{name: "no arguments", wantErr: "--dataset is required unless --index, --queries, and --qrels are all set"},
		{name: "missing dataset", args: []string{"--run", "unused.trec"}, wantErr: "--dataset is required unless --index, --queries, and --qrels are all set"},
		{name: "missing corpus source", args: []string{"--queries", "queries.tsv", "--qrels", "qrels.tsv"}, wantErr: "--dataset is required unless --index, --queries, and --qrels are all set"},
		{name: "missing queries path", args: []string{"--index", "idx", "--qrels", "qrels.tsv"}, wantErr: "--dataset is required unless --index, --queries, and --qrels are all set"},
		{name: "missing qrels path", args: []string{"--index", "idx", "--queries", "queries.tsv"}, wantErr: "--dataset is required unless --index, --queries, and --qrels are all set"},
		{name: "explicit paths missing run", args: []string{"--index", "idx", "--queries", "queries.tsv", "--qrels", "qrels.tsv"}, wantErr: "--run is required"},
		{name: "missing run", args: []string{"--dataset", "testdata/tiny"}, wantErr: "--run is required"},
		{name: "zero k", args: []string{"--dataset", "testdata/tiny", "--run", "unused.trec", "--k", "0"}, wantErr: "--k must be at least 1"},
		{name: "negative k", args: []string{"--dataset", "testdata/tiny", "--run", "unused.trec", "--k", "-1"}, wantErr: "--k must be at least 1"},
		{name: "noninteger k", args: []string{"--k", "bad"}, wantErr: "invalid value"},
		{name: "nonexistent index", args: []string{"--dataset", "testdata/tiny", "--run", "unused.trec", "--index", "testdata/nonexistent"}, wantErr: "manifest.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run(%q) error = %v, want %q", tt.args, err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("run(%q) error = %v, want nil", tt.args, err)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStdout string
		wantRun    string
	}{
		{
			name:       "full run with default k",
			wantStdout: "queries\t2\nnDCG@10\t0.8155\nR@100\t1.0000\nR@1000\t1.0000\nMRR@10\t0.7500\n",
			wantRun:    "q1 Q0 a 1 0.364814 quarry\nq1 Q0 b 2 0.364814 quarry\nq2 Q0 c 1 0.633670 quarry\n",
		},
		{
			name:       "limited results",
			args:       []string{"--k", "1"},
			wantStdout: "queries\t2\nnDCG@10\t1.0000\nR@100\t1.0000\nR@1000\t1.0000\nMRR@10\t1.0000\n",
			wantRun:    "q1 Q0 a 1 0.364814 quarry\nq2 Q0 c 1 0.633670 quarry\n",
		},
	}
	// A saved index must give exactly the same output as loading the corpus.
	// Its dataset has no corpus.jsonl, so the run cannot silently use the corpus.
	sources := map[string][]string{
		"corpus": {"--dataset", "testdata/tiny"},
		"index":  {"--dataset", datasetWithoutCorpus(t), "--index", saveTinyIndex(t)},
	}
	for _, tt := range tests {
		for sourceName, sourceArgs := range sources {
			t.Run(tt.name+" from "+sourceName, func(t *testing.T) {
				runPath := filepath.Join(t.TempDir(), "nested", "runs", "tiny.trec")
				args := append([]string{"--run", runPath}, sourceArgs...)
				args = append(args, tt.args...)
				var stdout, stderr bytes.Buffer
				if err := run(args, &stdout, &stderr); err != nil {
					t.Fatalf("run(%q) error = %v, want nil", args, err)
				}
				if got := metricsOutput(t, stdout.String()); got != tt.wantStdout {
					t.Errorf("run(%q) stdout = %q, want %q", args, got, tt.wantStdout)
				}
				if got := stderr.String(); got != "" {
					t.Errorf("run(%q) stderr = %q, want empty", args, got)
				}
				contents, err := os.ReadFile(runPath)
				if err != nil {
					t.Fatalf("ReadFile(%q) error = %v, want nil", runPath, err)
				}
				if got := string(contents); got != tt.wantRun {
					t.Errorf("run(%q) run file = %q, want %q", args, got, tt.wantRun)
				}
			})
		}
	}
}

func datasetWithoutCorpus(t *testing.T) string {
	t.Helper()
	dataset := t.TempDir()
	for _, name := range []string{"queries.jsonl", "qrels/test.tsv"} {
		contents, err := os.ReadFile(filepath.Join("testdata", "tiny", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dataset, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dataset
}

func saveTinyIndex(t *testing.T) string {
	t.Helper()
	ix, err := corpus.LoadFile(filepath.Join("testdata", "tiny", "corpus.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "idx")
	if err := ix.Write(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunDataset(t *testing.T) {
	tests := []struct {
		name       string
		missing    string
		replace    string
		contents   string
		wantErr    string
		wantStdout string
	}{
		{name: "missing corpus", missing: "corpus.jsonl", wantErr: "open corpus:"},
		{name: "missing queries", missing: "queries.jsonl", wantErr: "open queries:"},
		{name: "missing qrels", missing: "qrels/test.tsv", wantErr: "open qrels:"},
		{name: "malformed corpus", replace: "corpus.jsonl", contents: "{", wantErr: "record 1:"},
		{name: "malformed queries", replace: "queries.jsonl", contents: "{", wantErr: "record 1:"},
		{name: "malformed qrels", replace: "qrels/test.tsv", contents: "bad header", wantErr: "read qrels: line 1:"},
		{name: "query missing from queries", replace: "queries.jsonl", contents: `{"_id":"q2","text":"bird"}`, wantErr: `query "q1" in qrels is missing from queries`},
		{
			name:       "no matching documents",
			replace:    "queries.jsonl",
			contents:   "{\"_id\":\"q1\",\"text\":\"missing\"}\n{\"_id\":\"q2\",\"text\":\"!?\"}\n",
			wantStdout: "queries\t2\nnDCG@10\t0.0000\nR@100\t0.0000\nR@1000\t0.0000\nMRR@10\t0.0000\n",
		},
		{
			name:       "empty qrels",
			replace:    "qrels/test.tsv",
			contents:   "query-id\tcorpus-id\tscore\n",
			wantStdout: "queries\t0\nnDCG@10\t0.0000\nR@100\t0.0000\nR@1000\t0.0000\nMRR@10\t0.0000\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataset := t.TempDir()
			for _, name := range []string{"corpus.jsonl", "queries.jsonl", "qrels/test.tsv"} {
				if name == tt.missing {
					continue
				}
				contents, err := os.ReadFile(filepath.Join("testdata", "tiny", name))
				if err != nil {
					t.Fatalf("ReadFile(%q) error = %v, want nil", name, err)
				}
				if name == tt.replace {
					contents = []byte(tt.contents)
				}
				path := filepath.Join(dataset, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
				}
				if err := os.WriteFile(path, contents, 0o600); err != nil {
					t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
				}
			}
			runPath := filepath.Join(t.TempDir(), "tiny.trec")
			args := []string{"--dataset", dataset, "--run", runPath}
			var stdout, stderr bytes.Buffer
			err := run(args, &stdout, &stderr)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run(%q) error = %v, want %q", args, err, tt.wantErr)
				}
				if _, err := os.Stat(runPath); !os.IsNotExist(err) {
					t.Errorf("Stat(%q) error = %v, want file not found", runPath, err)
				}
			} else {
				if err != nil {
					t.Fatalf("run(%q) error = %v, want nil", args, err)
				}
				contents, err := os.ReadFile(runPath)
				if err != nil {
					t.Fatalf("ReadFile(%q) error = %v, want nil", runPath, err)
				}
				if len(contents) != 0 {
					t.Errorf("run(%q) run file = %q, want empty", args, contents)
				}
			}
			if got := metricsOutput(t, stdout.String()); got != tt.wantStdout {
				t.Errorf("run(%q) stdout = %q, want %q", args, got, tt.wantStdout)
			}
		})
	}
}

func TestRunExplicitPaths(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"corpus.jsonl":          "{\"_id\":\"a\",\"text\":\"fish\"}\n{\"_id\":\"b\",\"text\":\"fish\"}\n{\"_id\":\"c\",\"text\":\"bird\"}\n",
		"queries.jsonl":         "{\"_id\":\"q1\",\"text\":\"Fish?\"}\n{\"_id\":\"q2\",\"text\":\"BIRD!\"}\n",
		"qrels/test.tsv":        "query-id\tcorpus-id\tscore\nq1\ta\t1\nq1\tb\t0\nq2\tc\t1\n",
		"queries.dev.small.tsv": "q1\tFish?\nq2\tBIRD!\n",
		"qrels.dev.small.tsv":   "q1\t0\ta\t1\nq1\t0\tb\t0\nq2\t0\tc\t1\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := corpus.LoadFile(filepath.Join(root, "corpus.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, "idx")
	if err := ix.Write(indexPath); err != nil {
		t.Fatal(err)
	}
	queriesPath := filepath.Join(root, "queries.dev.small.tsv")
	qrelsPath := filepath.Join(root, "qrels.dev.small.tsv")
	var baselineRun []byte
	var baselineStdout string
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "BEIR", args: []string{"--dataset", root}},
		{name: "explicit paths without dataset", args: []string{"--index", indexPath, "--queries", queriesPath, "--qrels", qrelsPath}},
		{name: "override queries", args: []string{"--dataset", root, "--queries", queriesPath}},
		{name: "override qrels", args: []string{"--dataset", root, "--qrels", qrelsPath}},
		{name: "override both", args: []string{"--dataset", root, "--queries", queriesPath, "--qrels", qrelsPath}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runPath := filepath.Join(t.TempDir(), "run.trec")
			args := append([]string{"--run", runPath}, tt.args...)
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			data, err := os.ReadFile(runPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) == 0 {
				t.Fatal("run file is empty")
			}
			if tt.name == "BEIR" {
				baselineRun, baselineStdout = data, metricsOutput(t, stdout.String())
				return
			}
			if !bytes.Equal(data, baselineRun) {
				t.Errorf("run = %q, want %q", data, baselineRun)
			}
			if metricsOutput(t, stdout.String()) != baselineStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), baselineStdout)
			}
		})
	}
}

// metricsOutput validates latency lines and returns the stable metrics prefix.
func metricsOutput(t *testing.T, output string) string {
	t.Helper()
	if output == "" {
		return ""
	}
	metrics, latency, found := strings.Cut(output, "latency_ms_mean\t")
	if !found {
		t.Fatal("missing latency lines")
	}
	lines := strings.Split(strings.TrimSuffix("latency_ms_mean\t"+latency, "\n"), "\n")
	keys := []string{"latency_ms_mean", "latency_ms_p50", "latency_ms_p95", "latency_ms_p99"}
	if len(lines) != len(keys) {
		t.Fatalf("latency lines = %q", lines)
	}
	values := make([]float64, len(keys))
	for i, key := range keys {
		name, value, found := strings.Cut(lines[i], "\t")
		if !found || name != key {
			t.Fatalf("latency line = %q, want %q", lines[i], key)
		}
		number, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			t.Fatalf("invalid latency %q", value)
		}
		_, decimals, _ := strings.Cut(value, ".")
		if len(decimals) != 3 {
			t.Fatalf("latency %q must have three decimal places", value)
		}
		if strings.HasPrefix(metrics, "queries\t0\n") && number != 0 {
			t.Fatalf("empty evaluation latency = %g, want zero", number)
		}
		values[i] = number
	}
	if values[1] > values[2] || values[2] > values[3] {
		t.Fatalf("unordered percentiles: %v", values)
	}
	return metrics
}

func TestPercentile(t *testing.T) {
	for _, tt := range []struct {
		name string
		n    int
		want [3]time.Duration
	}{
		{"empty", 0, [3]time.Duration{0, 0, 0}},
		{"one", 1, [3]time.Duration{1, 1, 1}},
		{"two", 2, [3]time.Duration{1, 2, 2}},
		{"hundred", 100, [3]time.Duration{50, 95, 99}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			durations := make([]time.Duration, tt.n)
			for i := range durations {
				durations[i] = time.Duration(i + 1)
			}
			for i, p := range []float64{50, 95, 99} {
				if got := percentile(durations, p); got != tt.want[i] {
					t.Errorf("percentile(n=%d, p=%g) = %v, want %v", tt.n, p, got, tt.want[i])
				}
			}
		})
	}
}

func TestRunAlgorithms(t *testing.T) {
	for source, sourceArgs := range map[string][]string{
		"corpus": {"--dataset", "testdata/tiny"},
		"disk":   {"--dataset", datasetWithoutCorpus(t), "--index", saveTinyIndex(t)},
	} {
		t.Run(source, func(t *testing.T) {
			var baselineRun []byte
			var baselineMetrics string
			for _, algo := range []string{"", "exhaustive", "wand"} {
				runPath := filepath.Join(t.TempDir(), "run.trec")
				args := append([]string{"--run", runPath}, sourceArgs...)
				if algo != "" {
					args = append(args, "--algo", algo)
				}
				var stdout, stderr bytes.Buffer
				if err := run(args, &stdout, &stderr); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(runPath)
				if err != nil {
					t.Fatal(err)
				}
				metrics := metricsOutput(t, stdout.String())
				if algo == "" {
					baselineRun, baselineMetrics = data, metrics
				} else if !bytes.Equal(data, baselineRun) || metrics != baselineMetrics {
					t.Fatalf("%s results differ from default", algo)
				}
			}
		})
	}
}
