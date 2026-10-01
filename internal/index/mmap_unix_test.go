//go:build unix

package index

import (
	"path/filepath"
	"testing"
)

// These tests read a package-level counter, so they must not run in parallel.
func TestMappingsReleased(t *testing.T) {
	source := filepath.Join(t.TempDir(), "idx")
	if err := smallIndex().Write(source); err != nil {
		t.Fatal(err)
	}
	// seg0.ids is mapped last and the table checks run after every file is mapped,
	// so these failures happen while other files are still mapped.
	failures := []struct {
		name   string
		change func(t *testing.T, dir string)
	}{
		{"checksum of last file", func(t *testing.T, dir string) {
			data := readTestFile(t, dir, idsName)
			data[len(data)-1] ^= 1
			writeTestFile(t, dir, idsName, data)
		}},
		{"table check after mapping", func(t *testing.T, dir string) {
			rewriteManifest(t, dir, func(m *manifest) { m.TotalTerms++ })
		}},
	}

	before := liveMappings.Load()
	disk, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if got := liveMappings.Load() - before; got != 4 {
		t.Errorf("after Open, %d new mappings, want 4", got)
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
	if got := liveMappings.Load() - before; got != 0 {
		t.Errorf("after Close, %d mappings left, want 0", got)
	}

	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestIndex(t, source)
			tt.change(t, dir)
			before := liveMappings.Load()
			if _, err := Open(dir); err == nil {
				t.Fatal("Open() succeeded, want error")
			}
			if got := liveMappings.Load() - before; got != 0 {
				t.Errorf("failed Open left %d mappings, want 0", got)
			}
		})
	}
}
