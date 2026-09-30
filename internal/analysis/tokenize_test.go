package analysis

import (
	"slices"
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "simple words", text: "the quick fox", want: []string{"the", "quick", "fox"}},
		{name: "lowercases", text: "The QUICK Fox", want: []string{"the", "quick", "fox"}},
		{name: "splits on punctuation", text: "covid-19, (sars)!", want: []string{"covid", "19", "sars"}},
		{name: "collapses whitespace", text: "  a \t b\n\nc  ", want: []string{"a", "b", "c"}},
		{name: "keeps non-ascii letters", text: "Café naïve Straße", want: []string{"café", "naïve", "straße"}},
		{name: "keeps digits", text: "p53 in 2024", want: []string{"p53", "in", "2024"}},
		{name: "empty", text: "", want: nil},
		{name: "only punctuation", text: "--- !!", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.text)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Tokenize(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
