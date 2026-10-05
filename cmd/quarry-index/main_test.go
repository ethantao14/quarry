package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
)

// tempOut in test arguments is replaced with a fresh temporary output path.
const tempOut = "<temp out>"

func TestMain(m *testing.M) {
	old := debug.SetMemoryLimit(-1)
	code := m.Run()
	debug.SetMemoryLimit(old)
	os.Exit(code)
}

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
	wantNames := "manifest.json seg0.dict seg0.ids seg0.lens seg0.post seg0.skip"
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
	stats := parseBuildStats(t, stdout.String())
	postInfo, err := os.Stat(filepath.Join(out, "seg0.post"))
	if err != nil {
		t.Fatal(err)
	}
	wantStdout := fmt.Sprintf("docs\t3\nterms\t6\npostings\t8\nsegments\t0\nbytes\t%d\nseconds\t%.2f\ndocs_per_second\t%.0f\nbits_per_posting\t%.2f\n",
		size, stats.seconds, stats.docsPerSecond, 8*float64(postInfo.Size())/8)
	if got := stdout.String(); got != wantStdout {
		t.Errorf("stdout = %q, want %q", got, wantStdout)
	}
}

func TestChunkBudget(t *testing.T) {
	tests := []struct {
		budget int64
		want   int64
	}{
		{budget: 1, want: 1},
		{budget: 2, want: 1},
		{budget: 3, want: 1},
		{budget: 300, want: 100},
		{budget: 1 << 30, want: (1 << 30) / 3},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.budget), func(t *testing.T) {
			if got := chunkBudget(tt.budget); got != tt.want {
				t.Errorf("chunkBudget(%d) = %d, want %d", tt.budget, got, tt.want)
			}
		})
	}
}

func TestApplyMemoryLimit(t *testing.T) {
	old := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(old) })
	tests := []struct {
		name   string
		start  int64
		budget int64
		want   int64
	}{
		{name: "below minimum", start: math.MaxInt64, budget: 1 << 20, want: math.MaxInt64},
		{name: "minimum", start: math.MaxInt64, budget: limitedBudgetMin, want: limitedBudgetMin},
		{name: "larger budget", start: math.MaxInt64, budget: 1 << 30, want: 1 << 30},
		{name: "lower existing limit", start: 512 << 20, budget: 1 << 30, want: 512 << 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			debug.SetMemoryLimit(tt.start)
			if got := applyMemoryLimit(tt.budget); got != tt.want {
				t.Errorf("applyMemoryLimit(%d) = %d, want %d", tt.budget, got, tt.want)
			}
			if got := debug.SetMemoryLimit(-1); got != tt.want {
				t.Errorf("memory limit = %d, want %d", got, tt.want)
			}
		})
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
			stats := parseBuildStats(t, stdout.String())
			if stats.docs != 80 || stats.terms != 3 || stats.postings != 240 || stats.size <= 0 || (budget == "" && stats.segments != 0) || (budget != "" && stats.segments <= 0) {
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

type buildStats struct {
	docs, terms, segments                  int
	postings                               uint64
	size                                   int64
	seconds, docsPerSecond, bitsPerPosting float64
}

func parseBuildStats(t *testing.T, output string) buildStats {
	t.Helper()
	var stats buildStats
	const scanFormat = "docs\t%d\nterms\t%d\npostings\t%d\nsegments\t%d\nbytes\t%d\nseconds\t%f\ndocs_per_second\t%f\nbits_per_posting\t%f\n"
	n, err := fmt.Sscanf(output, scanFormat, &stats.docs, &stats.terms, &stats.postings, &stats.segments, &stats.size, &stats.seconds, &stats.docsPerSecond, &stats.bitsPerPosting)
	if err != nil || n != 8 {
		t.Fatalf("parse stdout %q: fields = %d, error = %v", output, n, err)
	}
	want := fmt.Sprintf("docs\t%d\nterms\t%d\npostings\t%d\nsegments\t%d\nbytes\t%d\nseconds\t%.2f\ndocs_per_second\t%.0f\nbits_per_posting\t%.2f\n",
		stats.docs, stats.terms, stats.postings, stats.segments, stats.size, stats.seconds, stats.docsPerSecond, stats.bitsPerPosting)
	if output != want {
		t.Fatalf("stdout = %q, want format %q", output, want)
	}
	for _, value := range []float64{stats.seconds, stats.docsPerSecond, stats.bitsPerPosting} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			t.Fatalf("invalid numeric metric in %q", output)
		}
	}
	if stats.docs > 0 && stats.docsPerSecond <= 0 {
		t.Fatalf("docs_per_second must be positive: %q", output)
	}
	if stats.postings > 0 && stats.bitsPerPosting <= 0 {
		t.Fatalf("bits_per_posting must be positive: %q", output)
	}
	return stats
}

func TestRunCorpusFormats(t *testing.T) {
	var baseline map[string][]byte
	for _, format := range []struct {
		ext   string
		input string
	}{
		{ext: ".jsonl", input: "{\"_id\":\"a\",\"title\":\"\",\"text\":\"red fish fish\"}\n{\"_id\":\"b\",\"title\":\"\",\"text\":\"blue\\tfish\"}\n{\"_id\":\"c\",\"title\":\"\",\"text\":\"\"}\n"},
		{ext: ".tsv", input: "a\tred fish fish\nb\tblue\tfish\nc\t\n"},
		{ext: ".TSV", input: "a\tred fish fish\nb\tblue\tfish\nc\t\n"},
	} {
		for _, budget := range []string{"1GB", "1"} {
			t.Run(format.ext+"/budget="+budget, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "corpus"+format.ext)
				if err := os.WriteFile(path, []byte(format.input), 0o600); err != nil {
					t.Fatal(err)
				}
				out := filepath.Join(root, "idx")
				var stdout, stderr bytes.Buffer
				if err := run([]string{"--corpus", path, "--out", out, "--mem-budget", budget}, &stdout, &stderr); err != nil {
					t.Fatal(err)
				}
				stats := parseBuildStats(t, stdout.String())
				if stats.docs != 3 || stats.terms != 3 || stats.postings != 4 {
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
						t.Errorf("%s differs across formats", name)
					}
				}
			})
		}
	}
}

func TestRunEmptyCorpusStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.tsv")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--corpus", path, "--out", filepath.Join(t.TempDir(), "idx")}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	stats := parseBuildStats(t, stdout.String())
	if stats.docs != 0 || stats.terms != 0 || stats.postings != 0 || stats.docsPerSecond != 0 || stats.bitsPerPosting != 0 {
		t.Fatalf("unexpected empty corpus stats: %q", stdout.String())
	}
}

// TestRunUsesBudget checks that run limits the Go heap and gives chunks a third of the budget.
func TestRunUsesBudget(t *testing.T) {
	old := debug.SetMemoryLimit(math.MaxInt64)
	t.Cleanup(func() { debug.SetMemoryLimit(old) })
	path := filepath.Join(t.TempDir(), "corpus.tsv")
	var input strings.Builder
	for i := range 80 {
		fmt.Fprintf(&input, "doc-%d\tRed fish blue fish\n", i)
	}
	if err := os.WriteFile(path, []byte(input.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	// The corpus is estimated at about 6 KB: a 12 KB chunk would hold it all, a 4 KB chunk cannot.
	for _, tt := range []struct {
		budget    string
		wantLimit int64
	}{
		{budget: "12KB", wantLimit: math.MaxInt64},
		{budget: "512MB", wantLimit: 512 << 20},
	} {
		t.Run(tt.budget, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := []string{"--corpus", path, "--out", filepath.Join(t.TempDir(), "idx"), "--mem-budget", tt.budget}
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			if got := debug.SetMemoryLimit(-1); got != tt.wantLimit {
				t.Errorf("memory limit = %d, want %d", got, tt.wantLimit)
			}
			if stats := parseBuildStats(t, stdout.String()); tt.budget == "12KB" && stats.segments == 0 {
				t.Errorf("segments = 0, want a flush with a third of a 12 KB budget")
			}
		})
	}
}
