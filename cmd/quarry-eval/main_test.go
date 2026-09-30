package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "help", args: []string{"-h"}},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: "flag provided but not defined"},
		{name: "no arguments", wantErr: "--dataset is required"},
		{name: "missing dataset", args: []string{"--run", "unused.trec"}, wantErr: "--dataset is required"},
		{name: "missing run", args: []string{"--dataset", "testdata/tiny"}, wantErr: "--run is required"},
		{name: "zero k", args: []string{"--dataset", "testdata/tiny", "--run", "unused.trec", "--k", "0"}, wantErr: "--k must be at least 1"},
		{name: "negative k", args: []string{"--dataset", "testdata/tiny", "--run", "unused.trec", "--k", "-1"}, wantErr: "--k must be at least 1"},
		{name: "noninteger k", args: []string{"--k", "bad"}, wantErr: "invalid value"},
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runPath := filepath.Join(t.TempDir(), "nested", "runs", "tiny.trec")
			args := append([]string{"--dataset", "testdata/tiny", "--run", runPath}, tt.args...)
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatalf("run(%q) error = %v, want nil", args, err)
			}
			if got := stdout.String(); got != tt.wantStdout {
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
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("run(%q) stdout = %q, want %q", args, got, tt.wantStdout)
			}
		})
	}
}
