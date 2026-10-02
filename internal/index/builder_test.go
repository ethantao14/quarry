package index

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type builderDocument struct {
	id    string
	terms []string
}

func compareIndexDirectories(t *testing.T, got, want string) {
	t.Helper()
	gotFiles, err := os.ReadDir(got)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles, err := os.ReadDir(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotFiles) != len(wantFiles) {
		t.Fatalf("file count = %d, want %d", len(gotFiles), len(wantFiles))
	}
	for i, file := range gotFiles {
		if file.IsDir() || strings.HasPrefix(file.Name(), "tmp-seg") {
			t.Fatalf("temporary directory remains: %s", file.Name())
		}
		if file.Name() != wantFiles[i].Name() {
			t.Fatalf("file = %s, want %s", file.Name(), wantFiles[i].Name())
		}
		if !bytes.Equal(readTestFile(t, got, file.Name()), readTestFile(t, want, file.Name())) {
			t.Errorf("%s differs", file.Name())
		}
	}
}

func checkBuilderBudgets(t *testing.T, docs []builderDocument) {
	t.Helper()
	memory := New()
	for _, doc := range docs {
		memory.Add(doc.id, doc.terms)
	}
	want := filepath.Join(t.TempDir(), "reference")
	if err := memory.Write(want); err != nil {
		t.Fatal(err)
	}
	for _, budget := range []int64{1, 64, 512, 4096, 1 << 40} {
		t.Run(fmt.Sprintf("budget=%d", budget), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "idx")
			builder, err := NewBuilder(dir, budget)
			if err != nil {
				t.Fatal(err)
			}
			for _, doc := range docs {
				if err := builder.Add(doc.id, doc.terms); err != nil {
					t.Fatal(err)
				}
			}
			if err := builder.Finish(); err != nil {
				t.Fatal(err)
			}
			if budget == 1 && len(docs) >= 2 && builder.Segments() <= 1 {
				t.Errorf("Segments() = %d, want more than 1", builder.Segments())
			}
			if budget == 1<<40 && builder.Segments() != 0 {
				t.Errorf("Segments() = %d, want 0", builder.Segments())
			}
			compareIndexDirectories(t, dir, want)
		})
	}
}

// TestBuilderRandomCorpora checks every output byte across chunk boundaries.
func TestBuilderRandomCorpora(t *testing.T) {
	for seed := uint64(0); seed < 40; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			random := rand.New(rand.NewPCG(seed, 2))
			vocabulary := []string{"", "a", "b", "c", "猫", "café", "魚", "🙂", "red", "blue", "green", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
			for i := range 4 {
				vocabulary = append(vocabulary, fmt.Sprintf("seed-%d-term-%d", seed, i))
			}
			docs := make([]builderDocument, random.IntN(61))
			for i := range docs {
				docs[i].id = fmt.Sprintf("文書-%d", random.IntN(10))
				if i%7 == 0 {
					docs[i].id = ""
				}
				docs[i].terms = make([]string, random.IntN(41))
				for j := range docs[i].terms {
					docs[i].terms[j] = vocabulary[random.IntN(len(vocabulary))]
				}
			}
			checkBuilderBudgets(t, docs)
		})
	}
}

// TestBuilderEdgeCorpora covers empty chunks and terms shared across every segment.
func TestBuilderEdgeCorpora(t *testing.T) {
	tests := []struct {
		name string
		docs []builderDocument
	}{
		{name: "empty"},
		{name: "empty documents", docs: []builderDocument{{id: "first"}, {}, {id: "last"}}},
		{name: "segment terms", docs: []builderDocument{
			{id: "first", terms: []string{"", "shared", "first-only", "shared"}},
			{id: "middle", terms: []string{"", "shared", "猫"}},
			{id: "last", terms: []string{"", "shared", "last-only"}},
		}},
		{name: "varint boundaries", docs: []builderDocument{
			{id: "first", terms: strings.Fields(strings.Repeat("shared ", 300))},
			{id: "last", terms: []string{"shared"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkBuilderBudgets(t, tt.docs) })
	}
}

// TestNewBuilder checks directory handling and budget validation.
func TestNewBuilder(t *testing.T) {
	tests := []struct {
		name     string
		budget   int64
		existing bool
		suffix   string
		wantErr  string
	}{
		{name: "existing directory", budget: 1, existing: true, wantErr: "already exists"},
		{name: "zero budget", wantErr: "memory budget must be positive"},
		{name: "negative budget", budget: -1, wantErr: "memory budget must be positive"},
		{name: "missing parents", budget: 1},
		{name: "trailing slash", budget: 1, suffix: string(filepath.Separator)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tt.existing {
				dir = filepath.Join(dir, "nested", "idx") + tt.suffix
			}
			builder, err := NewBuilder(dir, tt.budget)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewBuilder() error = %v, want %q", err, tt.wantErr)
				}
				if tt.existing {
					files, err := os.ReadDir(dir)
					if err != nil || len(files) != 0 {
						t.Fatalf("existing directory changed: %v, %v", files, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := builder.Finish(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestBuilderFinished rejects reuse after finishing, with or without a merge.
func TestBuilderFinished(t *testing.T) {
	for _, budget := range []int64{1, 1 << 40} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			builder, err := NewBuilder(filepath.Join(t.TempDir(), "idx"), budget)
			if err != nil {
				t.Fatal(err)
			}
			if err := builder.Add("doc", []string{"a"}); err != nil {
				t.Fatal(err)
			}
			if err := builder.Finish(); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{"Add", func() error { return builder.Add("late", nil) }},
				{"Finish", builder.Finish},
			} {
				t.Run(operation.name, func(t *testing.T) {
					if err := operation.run(); err == nil {
						t.Fatal("operation after Finish succeeded")
					}
				})
			}
		})
	}
}

// TestBuilderDocCountLimit checks the global limit before mutating the chunk.
func TestBuilderDocCountLimit(t *testing.T) {
	builder, err := NewBuilder(filepath.Join(t.TempDir(), "idx"), 1)
	if err != nil {
		t.Fatal(err)
	}
	builder.docCount = math.MaxUint32
	if err := builder.Add("overflow", nil); err == nil || err.Error() != "index counts exceed format limits" {
		t.Fatalf("Add() error = %v", err)
	}
	if builder.chunk.DocCount() != 0 || builder.Segments() != 0 {
		t.Fatal("overflowing Add changed the builder")
	}
}

// TestBuilderMemoryEstimate checks distinct terms and one posting per document term.
func TestBuilderMemoryEstimate(t *testing.T) {
	builder := &Builder{chunk: New()}
	builder.chunk.Add("old", []string{"existing"})
	got := builder.estimateDocument("id", []string{"existing", "猫", "猫", ""})
	want := int64(4 + 16 + 2 + 3*8 + 64 + len("猫") + 64)
	if got != want {
		t.Fatalf("estimateDocument() = %d, want %d", got, want)
	}
}

// TestBuilderFlushFailure prevents further writes after a failed chunk flush.
func TestBuilderFlushFailure(t *testing.T) {
	builder, err := NewBuilder(filepath.Join(t.TempDir(), "idx"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(builder.segmentDir(0), 0o755); err != nil {
		t.Fatal(err)
	}
	failure := builder.Add("first", []string{"shared"})
	if failure == nil || !strings.Contains(failure.Error(), "already exists") {
		t.Fatalf("Add() error = %v", failure)
	}
	if err := builder.Add("second", nil); err != failure {
		t.Fatalf("second Add() error = %v, want %v", err, failure)
	}
	if err := builder.Finish(); err != failure {
		t.Fatalf("Finish() error = %v, want %v", err, failure)
	}
	if _, err := os.Stat(filepath.Join(builder.dir, manifestName)); !os.IsNotExist(err) {
		t.Fatalf("failed build published a manifest: %v", err)
	}
}

// TestBuilderFlushesAtBudget checks where chunks end. Each document below is
// estimated at 4 + 16 + 2 (ID) + 8 (posting) + 2 + 64 (new term) = 96 bytes.
func TestBuilderFlushesAtBudget(t *testing.T) {
	tests := []struct {
		budget       int64
		wantSegments int
	}{
		{budget: 96, wantSegments: 6},
		{budget: 192, wantSegments: 3},
		{budget: 288, wantSegments: 2},
		{budget: 1000, wantSegments: 0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.budget), func(t *testing.T) {
			builder, err := NewBuilder(filepath.Join(t.TempDir(), "idx"), tt.budget)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 6 {
				if err := builder.Add(fmt.Sprintf("d%d", i), []string{fmt.Sprintf("t%d", i)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := builder.Finish(); err != nil {
				t.Fatal(err)
			}
			if got := builder.Segments(); got != tt.wantSegments {
				t.Errorf("Segments() = %d, want %d", got, tt.wantSegments)
			}
		})
	}
}

// TestBuilderAbort removes the directory and its temporary segments, and ends the build.
func TestBuilderAbort(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "idx")
	builder, err := NewBuilder(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := builder.Add(id, []string{"shared"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.Abort(); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Abort left %s behind: %v", dir, err)
	}
	if err := builder.Add("late", nil); err == nil {
		t.Error("Add after Abort succeeded")
	}
}
