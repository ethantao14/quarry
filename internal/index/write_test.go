package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/postings"
)

func TestWriteDirectory(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
		suffix   string
	}{
		{name: "existing directory", existing: true},
		{name: "missing parents"},
		{name: "trailing slash", suffix: string(filepath.Separator)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tt.existing {
				dir = filepath.Join(dir, "nested", "parent", "idx") + tt.suffix
			}
			err := New().Write(dir)
			if tt.existing {
				if err == nil || !strings.Contains(err.Error(), "already exists") {
					t.Fatalf("Write() error = %v, want already exists", err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("existing directory changed: %v, %v", entries, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			disk, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			cleanupDisk(t, disk)
		})
	}
}

// TestSegmentWriterTermOrder rejects duplicates and descending terms, including empty terms.
func TestSegmentWriterTermOrder(t *testing.T) {
	for _, tt := range []struct {
		name   string
		first  string
		second string
	}{
		{"duplicate", "a", "a"},
		{"descending", "b", "a"},
		{"empty duplicate", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writer, err := newSegmentWriter(t.TempDir(), temporary, scoringStats{1, 1, func(uint32) uint32 { return 1 }})
			if err != nil {
				t.Fatal(err)
			}
			defer writer.abort()
			list := []postings.Posting{{DocID: 0, TF: 1}}
			if err := writer.addTerm(tt.first, list); err != nil {
				t.Fatal(err)
			}
			if err := writer.addTerm(tt.second, list); err == nil || !strings.Contains(err.Error(), "strictly ascending") {
				t.Fatalf("addTerm() error = %v, want strictly ascending", err)
			}
		})
	}
}

// An index written through a symlink and ".." must open at the same path,
// so the writer must resolve the path like the OS does.
func TestWriteThroughSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "target", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target", "sub"), filepath.Join(root, "work", "link")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "work", "link") + "/../idx"
	if err := smallIndex().Write(dir); err != nil {
		t.Fatal(err)
	}
	disk, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", dir, err)
	}
	cleanupDisk(t, disk)
	if _, err := os.Stat(filepath.Join(root, "target", "idx", manifestName)); err != nil {
		t.Errorf("index not at the OS-resolved path: %v", err)
	}
}

func TestSplitLastElement(t *testing.T) {
	tests := []struct {
		dir, wantParent, wantName string
	}{
		{"idx", ".", "idx"},
		{"a/idx", "a", "idx"},
		{"a/idx//", "a", "idx"},
		{"/idx", "/", "idx"},
		{"/", "/", ""},
		{"a/..", "a", ".."},
		{"a/link/../idx", "a/link/..", "idx"},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			parent, name := splitLastElement(tt.dir)
			if parent != tt.wantParent || name != tt.wantName {
				t.Errorf("splitLastElement(%q) = %q, %q, want %q, %q", tt.dir, parent, name, tt.wantParent, tt.wantName)
			}
		})
	}
}

func TestWriteInvalidDirectoryName(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{filepath.Join(root, "a") + "/..", filepath.Join(root, "a") + "/.", "/"} {
		if err := New().Write(dir); err == nil || !strings.Contains(err.Error(), "invalid index directory") {
			t.Errorf("Write(%q) error = %v, want invalid index directory", dir, err)
		}
	}
}
