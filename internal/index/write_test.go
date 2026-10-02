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
			writer, err := newSegmentWriter(t.TempDir(), temporary)
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
