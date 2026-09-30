package analysis

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// knownDifferences lists golden inputs where we deliberately differ from Lucene.
// Each must still differ; if one starts matching, remove it from this list.
var knownDifferences = map[string]string{
	"emoji 😀 test 👍🏽 thumbs ❤️ heart 🇺🇸 flag 1️⃣ keycap": "emoji tokens are not supported; see docs/DESIGN.md",
}

// TestAnalyzeMatchesLucene compares Analyze with real Lucene output.
// Regenerate testdata/golden_lucene.txt with scripts/lucene-golden.sh.
func TestAnalyzeMatchesLucene(t *testing.T) {
	inputs := readLines(t, "testdata/golden_inputs.txt")
	expected := readLines(t, "testdata/golden_lucene.txt")
	if len(inputs) != len(expected) {
		t.Fatalf("golden files have %d inputs but %d outputs", len(inputs), len(expected))
	}

	for i, input := range inputs {
		got := strings.Join(Analyze(input), " ")
		want := expected[i]
		reason, isKnown := knownDifferences[input]

		switch {
		case isKnown && got == want:
			t.Errorf("line %d now matches Lucene; remove it from knownDifferences: %q", i+1, input)
		case !isKnown && got != want:
			t.Errorf("line %d: Analyze(%q)\n got: %q\nwant: %q", i+1, input, got, want)
		case isKnown:
			t.Logf("line %d differs as expected (%s)", i+1, reason)
		}
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return lines
}
