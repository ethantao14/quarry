package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
)

func TestRunFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "help", args: []string{"-h"}},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: "flag provided but not defined"},
		{name: "no arguments", wantErr: "--corpus is required"},
		{name: "missing out", args: []string{"--corpus", "testdata/tiny.jsonl"}, wantErr: "--out is required"},
		{name: "missing corpus", args: []string{"--out", "unused"}, wantErr: "--corpus is required"},
		{name: "nonexistent corpus", args: []string{"--corpus", "testdata/nonexistent.jsonl", "--out", "unused"}, wantErr: "open corpus"},
		{name: "existing out", args: []string{"--corpus", "testdata/tiny.jsonl", "--out", "testdata"}, wantErr: "already exists"},
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

func TestRunWritesIndex(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nested", "idx")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--corpus", "testdata/tiny.jsonl", "--out", out}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	files, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var size int64
	for _, file := range files {
		names = append(names, file.Name())
		info, err := file.Info()
		if err != nil {
			t.Fatal(err)
		}
		size += info.Size()
	}
	wantNames := "manifest.json seg0.dict seg0.ids seg0.lens seg0.post"
	if got := strings.Join(names, " "); got != wantNames {
		t.Errorf("index files = %q, want %q", got, wantNames)
	}

	disk, err := index.Open(out)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := disk.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if disk.DocCount() != 3 {
		t.Errorf("DocCount() = %d, want 3", disk.DocCount())
	}
	wantStdout := "docs\t3\nterms\t6\nbytes\t" + strconv.FormatInt(size, 10) + "\n"
	if got := stdout.String(); got != wantStdout {
		t.Errorf("stdout = %q, want %q", got, wantStdout)
	}
}
