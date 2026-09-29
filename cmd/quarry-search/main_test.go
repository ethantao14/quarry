package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStdout string
		wantErr    bool
	}{
		{name: "version flag", args: []string{"--version"}, wantStdout: "quarry-search dev\n"},
		{name: "help flag", args: []string{"-h"}},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "no arguments", args: nil, wantErr: true},
		{name: "missing corpus", args: []string{"fish"}, wantErr: true},
		{name: "zero k", args: []string{"--corpus", "testdata/tiny.jsonl", "--k", "0", "fish"}, wantErr: true},
		{name: "negative k", args: []string{"--corpus", "testdata/tiny.jsonl", "--k", "-1", "fish"}, wantErr: true},
		{name: "empty query", args: []string{"--corpus", "testdata/tiny.jsonl"}, wantErr: true},
		{name: "punctuation query", args: []string{"--corpus", "testdata/tiny.jsonl", "!?"}, wantErr: true},
		{name: "nonexistent corpus", args: []string{"--corpus", "testdata/nonexistent.jsonl", "fish"}, wantErr: true},
		{name: "no matches", args: []string{"--corpus", "testdata/tiny.jsonl", "missing"}},
		{
			name:       "real query",
			args:       []string{"--corpus", "testdata/tiny.jsonl", "fish"},
			wantStdout: "1\tfish-short\t0.2795\n2\tfish-twin\t0.2795\n3\tfish-long\t0.2662\n",
		},
		{
			name:       "limited results",
			args:       []string{"--corpus", "testdata/tiny.jsonl", "--k", "2", "fish"},
			wantStdout: "1\tfish-short\t0.2795\n2\tfish-twin\t0.2795\n",
		},
		{
			name:       "joins positional arguments",
			args:       []string{"--corpus", "testdata/tiny.jsonl", "missing", "fish"},
			wantStdout: "1\tfish-short\t0.2795\n2\tfish-twin\t0.2795\n3\tfish-long\t0.2662\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)

			if (err != nil) != tt.wantErr {
				t.Fatalf("run(%q) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("run(%q) stdout = %q, want %q", tt.args, got, tt.wantStdout)
			}
		})
	}
}
