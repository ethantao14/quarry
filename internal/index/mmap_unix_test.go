//go:build unix

package index

import (
	"os"
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
	if got := liveMappings.Load() - before; got != 5 {
		t.Errorf("after Open, %d new mappings, want 5", got)
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

// TestMergeMappingsReleased checks cleanup after successful and failed merges.
func TestMergeMappingsReleased(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*testing.T, *Builder)
	}{
		{name: "success"},
		{name: "open failure", change: func(t *testing.T, b *Builder) {
			writeTestFile(t, b.segmentDir(1), idsName, nil)
		}},
		{name: "decode failure", change: func(t *testing.T, b *Builder) {
			dir := b.segmentDir(1)
			data := readTestFile(t, dir, postName)
			data[9] = 0
			rewriteFile(t, dir, postName, data)
		}},
		{name: "write failure", change: func(t *testing.T, b *Builder) {
			if err := os.Mkdir(filepath.Join(b.dir, dictName), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			builder, err := NewBuilder(filepath.Join(t.TempDir(), "idx"), 1)
			if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if err := builder.Add("doc", []string{"shared"}); err != nil {
					t.Fatal(err)
				}
			}
			if tt.change != nil {
				tt.change(t, builder)
			}
			before := liveMappings.Load()
			err = builder.Finish()
			if (err != nil) != (tt.change != nil) {
				t.Fatalf("Finish() error = %v", err)
			}
			if got := liveMappings.Load() - before; got != 0 {
				t.Errorf("Finish left %d mappings, want 0", got)
			}
			if err != nil {
				for i := range 3 {
					if _, err := os.Stat(builder.segmentDir(i)); err != nil {
						t.Errorf("failed merge removed segment %d: %v", i, err)
					}
				}
				if _, err := os.Stat(filepath.Join(builder.dir, manifestName)); !os.IsNotExist(err) {
					t.Fatalf("failed merge published a manifest: %v", err)
				}
			}
		})
	}
}
