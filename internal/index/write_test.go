package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			if _, err := Open(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}
