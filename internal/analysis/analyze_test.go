package analysis

import (
	"slices"
	"strings"
	"testing"
)

// TestRemovePossessive checks suffix removal before lowercasing.
func TestRemovePossessive(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "ascii", text: "dog's", want: "dog"},
		{name: "uppercase suffix", text: "BOB'S", want: "BOB"},
		{name: "curly", text: "Mary’s", want: "Mary"},
		{name: "curly uppercase", text: "Mary’S", want: "Mary"},
		{name: "fullwidth", text: "Mary＇s", want: "Mary"},
		{name: "fullwidth uppercase", text: "Mary＇S", want: "Mary"},
		{name: "unicode base", text: "猫's", want: "猫"},
		{name: "internal apostrophe", text: "O'Neil's", want: "O'Neil"},
		{name: "plural possessive", text: "dogs'", want: "dogs'"},
		{name: "contraction", text: "don't", want: "don't"},
		{name: "left quote", text: "dog‘s", want: "dog‘s"},
		{name: "trailing extend", text: "dog's\u0301", want: "dog's\u0301"},
		{name: "suffix only", text: "'s", want: ""},
		{name: "one rune", text: "s", want: "s"},
		{name: "empty", text: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := removePossessive(tt.text); got != tt.want {
				t.Errorf("removePossessive(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

// TestLowercase checks simple Unicode casing without normalization or expansion.
func TestLowercase(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "ascii", text: "MiXeD", want: "mixed"},
		{name: "unicode", text: "CAFÉ РУССКИЙ", want: "café русский"},
		{name: "simple mapping", text: "İẞΣΟΣ", want: "ißσοσ"},
		{name: "combining", text: "E\u0301", want: "e\u0301"},
		{name: "empty", text: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowercase(tt.text); got != tt.want {
				t.Errorf("lowercase(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

// TestStopwords checks all 33 stopwords through the helper and full chain.
func TestStopwords(t *testing.T) {
	words := strings.Fields("a an and are as at be but by for if in into is it no not of on or such that the their then there these they this to was will with")
	for _, word := range words {
		t.Run(word, func(t *testing.T) {
			if !isStopword(word) {
				t.Errorf("isStopword(%q) = false, want true", word)
			}
			if got := Analyze(word + " " + strings.ToUpper(word)); len(got) != 0 {
				t.Errorf("Analyze stopword %q = %q, want no terms", word, got)
			}
		})
	}
	for _, word := range []string{"", "THE", "over", "from", "being"} {
		t.Run("keep/"+word, func(t *testing.T) {
			if isStopword(word) {
				t.Errorf("isStopword(%q) = true, want false", word)
			}
		})
	}
}

// TestStem checks classic Porter stemming without implicit lowercasing.
func TestStem(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "running", want: "run"},
		{text: "ponies", want: "poni"},
		{text: "ties", want: "ti"},
		{text: "relational", want: "relat"},
		{text: "generously", want: "gener"},
		{text: "fairly", want: "fairli"},
		{text: "RUNNING", want: "RUNNING"},
		{text: "café", want: "café"},
		{text: "©", want: "©"},
		{text: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := porterStem(tt.text); got != tt.want {
				t.Errorf("porterStem(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

// TestAnalyze checks filter ordering, repeated terms, and empty results.
func TestAnalyze(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "order and duplicates", text: "Cats running dogs cats", want: []string{"cat", "run", "dog", "cat"}},
		{name: "possessive before stopwords", text: "IT’S Mary's BOB'S", want: []string{"mari", "bob"}},
		{name: "stopwords before stemming", text: "was being", want: []string{"be"}},
		{name: "punctuation retained", text: "Don't foo_bar 1,000", want: []string{"don't", "foo_bar", "1,000"}},
		{name: "empty input", text: "", want: nil},
		{name: "only separators", text: "!? ___ \u200b", want: nil},
		{name: "only stopwords", text: "The and OF", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Analyze(tt.text); !slices.Equal(got, tt.want) {
				t.Errorf("Analyze(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

// FuzzAnalyze checks that arbitrary input never panics or produces empty terms.
func FuzzAnalyze(f *testing.F) {
	for _, text := range []string{"", "The dog's running", "中\u0301文", "א' ו\"ל", "a_\u200d.b 1,000", "\xff\x00", strings.Repeat("y", 512)} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, token := range Analyze(text) {
			if token == "" {
				t.Fatalf("Analyze(%q) emitted an empty token", text)
			}
		}
	})
}
