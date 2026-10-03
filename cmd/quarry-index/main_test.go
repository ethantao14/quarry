package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
)

// tempOut in test arguments is replaced with a fresh temporary output path.
const tempOut = "<temp out>"

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
		{name: "nonexistent corpus", args: []string{"--corpus", "testdata/nonexistent.jsonl", "--out", tempOut}, wantErr: "open corpus"},
		{name: "existing out", args: []string{"--corpus", "testdata/tiny.jsonl", "--out", "testdata"}, wantErr: "already exists"},
		{name: "zero workers", args: []string{"--corpus", "testdata/tiny.jsonl", "--out", tempOut, "--workers", "0"}, wantErr: "--workers must be at least 1"},
		{name: "negative workers", args: []string{"--corpus", "testdata/tiny.jsonl", "--out", tempOut, "--workers", "-2"}, wantErr: "--workers must be at least 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Output goes to a temp dir, so a failed run cannot leave files in the package.
			args := slices.Clone(tt.args)
			for i, arg := range args {
				if arg == tempOut {
					args[i] = filepath.Join(t.TempDir(), "idx")
				}
			}
			var stdout, stderr bytes.Buffer
			err := run(args, &stdout, &stderr)
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

// A failed build must not leave a partial directory that blocks the next run.
func TestRunRemovesFailedIndex(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(malformed, []byte("{\"_id\": \"d1\", \"text\": \"fish\"}\n{"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, corpusPath := range []string{"testdata/nonexistent.jsonl", malformed} {
		out := filepath.Join(t.TempDir(), "idx")
		args := []string{"--corpus", corpusPath, "--out", out, "--mem-budget", "1KB"}
		var stdout, stderr bytes.Buffer
		if err := run(args, &stdout, &stderr); err == nil {
			t.Fatalf("run(%q) succeeded, want error", args)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("run(%q) left %s behind: %v", args, out, err)
		}
	}
}

// A path through a symlink and ".." must never make cleanup remove another directory.
func TestRunFailureKeepsUnrelatedDirectory(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"target/sub", "target/idx", "work"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	precious := filepath.Join(root, "target", "idx", "precious.txt")
	if err := os.WriteFile(precious, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target", "sub"), filepath.Join(root, "work", "link")); err != nil {
		t.Fatal(err)
	}
	// The OS resolves work/link/../idx to target/idx, which already exists.
	out := filepath.Join(root, "work", "link") + "/../idx"
	args := []string{"--corpus", "testdata/nonexistent.jsonl", "--out", out}
	var stdout, stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err == nil {
		t.Fatalf("run(%q) succeeded, want error", args)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Errorf("failed run removed an unrelated file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "idx")); !os.IsNotExist(err) {
		t.Errorf("failed run left work/idx behind: %v", err)
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
	wantStdout := "docs\t3\nterms\t6\nsegments\t0\nbytes\t" + strconv.FormatInt(size, 10) + "\n"
	if got := stdout.String(); got != wantStdout {
		t.Errorf("stdout = %q, want %q", got, wantStdout)
	}
}

// TestParseSize checks units, positive values, and overflow.
func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"1", 1}, {"001", 1}, {"1024", 1024},
		{"1KB", 1 << 10}, {"2kb", 2 << 10}, {"3Mb", 3 << 20},
		{"1GB", 1 << 30}, {"7gB", 7 << 30},
		{"9223372036854775807", 1<<63 - 1}, {"8589934591GB", 8589934591 << 30},
		{"", 0}, {"0", 0}, {"0KB", 0}, {"-1", 0}, {"-1GB", 0},
		{"1TB", 0}, {"1B", 0}, {"KB", 0}, {"1.5MB", 0}, {"+1", 0},
		{" 1KB", 0}, {"1 KB", 0}, {"1KB ", 0},
		{"9223372036854775808", 0}, {"8589934592GB", 0}, {"9007199254740992KB", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSize(tt.input)
			if tt.want == 0 {
				if err == nil {
					t.Fatalf("parseSize(%q) = %d, want error", tt.input, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseSize(%q) = %d, %v, want %d", tt.input, got, err, tt.want)
			}
		})
	}
}

// TestRunMemoryBudgets compares CLI output files across chunk sizes.
func TestRunMemoryBudgets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "corpus.jsonl")
	var input strings.Builder
	for i := range 80 {
		fmt.Fprintf(&input, "{\"_id\":\"doc-%d\",\"title\":\"Red\",\"text\":\"fish blue fish\"}\n", i)
	}
	if err := os.WriteFile(path, []byte(input.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var baseline map[string][]byte
	for _, budget := range []string{"", "1KB"} {
		t.Run("budget="+budget, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "idx")
			args := []string{"--corpus", path, "--out", out}
			if budget != "" {
				args = append(args, "--mem-budget", budget)
			}
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			var docs, terms, segments int
			var size int64
			if _, err := fmt.Sscanf(stdout.String(), "docs\t%d\nterms\t%d\nsegments\t%d\nbytes\t%d\n", &docs, &terms, &segments, &size); err != nil {
				t.Fatal(err)
			}
			if docs != 80 || terms != 3 || size <= 0 || (budget == "" && segments != 0) || (budget != "" && segments <= 0) {
				t.Fatalf("unexpected stdout: %q", stdout.String())
			}
			got := readIndexFiles(t, out)
			if baseline == nil {
				baseline = got
				return
			}
			if len(got) != len(baseline) {
				t.Fatalf("file count = %d, want %d", len(got), len(baseline))
			}
			for name, data := range baseline {
				if actual, ok := got[name]; !ok || !bytes.Equal(actual, data) {
					t.Errorf("%s differs across budgets", name)
				}
			}
		})
	}
}

func TestRunWorkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.jsonl")
	texts := []string{
		"Red fish swim in the river.",
		"Running runners follow winding trails!",
		"Blue birds and quiet gardens.",
		"Scientists study distant stars and planets.",
		"Builders repair old bridges by the harbor.",
	}
	var input strings.Builder
	for i := range 300 {
		text := strings.Repeat(texts[i%len(texts)]+" ", i%11+1)
		fmt.Fprintf(&input, "{\"_id\":\"d%d\",\"title\":\"Observation %d\",\"text\":%q}\n", i, i, text)
	}
	if err := os.WriteFile(path, []byte(input.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var baseline map[string][]byte
	for _, workers := range []int{1, 2, 7} {
		for _, budget := range []string{"", "1KB"} {
			t.Run(fmt.Sprintf("workers=%d/budget=%s", workers, budget), func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "idx")
				args := []string{"--corpus", path, "--out", out, "--workers", strconv.Itoa(workers)}
				if budget != "" {
					args = append(args, "--mem-budget", budget)
				}
				var stdout, stderr bytes.Buffer
				if err := run(args, &stdout, &stderr); err != nil {
					t.Fatal(err)
				}
				got := readIndexFiles(t, out)
				if workers == 1 && budget == "" {
					baseline = got
					return
				}
				if len(got) != len(baseline) {
					t.Fatalf("file count = %d, want %d", len(got), len(baseline))
				}
				for name, data := range baseline {
					if actual, ok := got[name]; !ok || !bytes.Equal(actual, data) {
						t.Errorf("%s differs from workers=1 with default budget", name)
					}
				}
			})
		}
	}
}

func readIndexFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	contents := make(map[string][]byte)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		contents[file.Name()] = data
	}
	return contents
}
