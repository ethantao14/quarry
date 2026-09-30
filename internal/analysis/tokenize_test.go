package analysis

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestTokenize covers raw token boundaries and the token length limit.
func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "preserves case", text: "The QUICK Fox", want: []string{"The", "QUICK", "Fox"}},
		{name: "punctuation", text: "covid-19, (sars)!", want: []string{"covid", "19", "sars"}},
		{name: "whitespace", text: "  a \t b\n\nc\u00a0d\u2003e  ", want: []string{"a", "b", "c", "d", "e"}},
		{name: "unicode letters", text: "Café naïve Straße Ελληνικά 한국어 ภาษาไทย カタカナ", want: []string{"Café", "naïve", "Straße", "Ελληνικά", "한국어", "ภาษาไทย", "カタカナ"}},
		{name: "letters and digits", text: "cd4 1a p53 0x1F ١٢", want: []string{"cd4", "1a", "p53", "0x1F", "١٢"}},
		{name: "other numbers separate", text: "x² H₂O ½ ³", want: []string{"x", "H", "O"}},
		{name: "mid letters", text: "u.s.a don't a:b rock'n'roll", want: []string{"u.s.a", "don't", "a:b", "rock'n'roll"}},
		{name: "mid digits", text: "3.5 1,000 1;2 1:2 1.2.3", want: []string{"3.5", "1,000", "1;2", "1", "2", "1.2.3"}},
		{name: "mixed kinds split", text: "a.1 1.a p.53 3'UTR", want: []string{"a", "1", "1", "a", "p", "53", "3", "UTR"}},
		{name: "single mids only", text: "a..b 1,,2 a.'b 3''4", want: []string{"a", "b", "1", "2", "a", "b", "3", "4"}},
		{name: "mids at edges", text: ".5 5. 'quoted' ‘curly’ a: :b", want: []string{"5", "5", "quoted", "curly", "a", "b"}},
		{name: "connectors", text: "foo_bar _private trailing_ __dunder__ _1 a_1_b ___ a‿b", want: []string{"foo_bar", "_private", "trailing_", "__dunder__", "_1", "a_1_b", "a‿b"}},
		{name: "connectors break mids", text: "a_.b a._b 1_,2 1,_2 a_\u0301.b", want: []string{"a_", "b", "a", "_b", "1_", "2", "1", "_2", "a_\u0301", "b"}},
		{name: "extend categories", text: "e\u0301 क\u093e x\u20dd soft\u00adhyphen", want: []string{"e\u0301", "क\u093e", "x\u20dd", "soft\u00adhyphen"}},
		{name: "extends cannot start", text: "\u0301\u200dabc \u093e \u20dd _\u0301_", want: []string{"abc"}},
		{name: "zero width space and joiner", text: "zero\u200bwidth zero\u200djoiner", want: []string{"zero", "width", "zero\u200djoiner"}},
		{name: "extends around mids", text: "a\u0301.\u200db 1\u0301,\u200d2 a\u0301.\u200d1", want: []string{"a\u0301.\u200db", "1\u0301,\u200d2", "a\u0301", "1"}},
		{name: "mid followed only by extends", text: "a.\u0301 1,\u200d", want: []string{"a", "1"}},
		{name: "ideographs", text: "中文 日本語のテキスト ひらがな a中1", want: []string{"中", "文", "日", "本", "語", "の", "テキスト", "ひ", "ら", "が", "な", "a", "中", "1"}},
		{name: "ideograph extends", text: "中\u0301文 ひ\u200dら _中_", want: []string{"中\u0301", "文", "ひ\u200d", "ら", "中"}},
		{name: "symbol tokens", text: "Elsevier®Inc™© \u00a9\ufe0f 😀 ♥ +", want: []string{"Elsevier", "®", "Inc", "™", "©", "©\ufe0f"}},
		{name: "Hebrew quotes", text: "עברית ו\"ל אב' אב'ג אב'1", want: []string{"עברית", "ו\"ל", "אב'", "אב'ג", "אב'1"}},
		{name: "Hebrew quote boundaries", text: "א\"a a\"א א\" א'' א’ א_'", want: []string{"א", "a", "a", "א", "א", "א'", "א", "א_"}},
		{name: "Hebrew quote extends", text: "א\u05b0\"\u05b0ב א\u05b0'", want: []string{"א\u05b0\"\u05b0ב", "א\u05b0'"}},
		{name: "below limit", text: strings.Repeat("a", 254), want: []string{strings.Repeat("a", 254)}},
		{name: "at limit", text: strings.Repeat("a", 255), want: []string{strings.Repeat("a", 255)}},
		{name: "above limit", text: strings.Repeat("a", 256), want: []string{strings.Repeat("a", 255), "a"}},
		{name: "multiple pieces", text: strings.Repeat("b", 511), want: []string{strings.Repeat("b", 255), strings.Repeat("b", 255), "b"}},
		{name: "counts runes", text: strings.Repeat("é", 256), want: []string{strings.Repeat("é", 255), "é"}},
		{name: "extends count toward limit", text: "a" + strings.Repeat("\u0301", 255), want: []string{"a" + strings.Repeat("\u0301", 254)}},
		{name: "mid at split", text: strings.Repeat("a", 254) + ".bc", want: []string{strings.Repeat("a", 254), "bc"}},
		{name: "empty", text: "", want: nil},
		{name: "only separators", text: "--- !! \u200b", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Tokenize(tt.text); !slices.Equal(got, tt.want) {
				t.Errorf("Tokenize(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

// TestTokenizeMidCharacters checks every supported mid rune in both base contexts.
func TestTokenizeMidCharacters(t *testing.T) {
	tests := []struct {
		name       string
		mids       string
		joinLetter bool
		joinDigit  bool
	}{
		{name: "letter", mids: ":\u00b7\u0387\u055f\u05f4\u2027\ufe13\ufe55\uff1a", joinLetter: true},
		{name: "digit", mids: ",;\u037e\u0589\u060c\u060d\u066c\u07f8\u2044\ufe10\ufe14\ufe50\ufe54\uff0c\uff1b", joinDigit: true},
		{name: "shared", mids: ".\u2018\u2019\u2024\ufe52\uff07\uff0e'", joinLetter: true, joinDigit: true},
	}
	for _, tt := range tests {
		for _, mid := range tt.mids {
			t.Run(fmt.Sprintf("%s/%U", tt.name, mid), func(t *testing.T) {
				letters := "a" + string(mid) + "b"
				digits := "1" + string(mid) + "2"
				wantLetters := []string{"a", "b"}
				if tt.joinLetter {
					wantLetters = []string{letters}
				}
				wantDigits := []string{"1", "2"}
				if tt.joinDigit {
					wantDigits = []string{digits}
				}
				if got := Tokenize(letters); !slices.Equal(got, wantLetters) {
					t.Errorf("Tokenize(%q) = %q, want %q", letters, got, wantLetters)
				}
				if got := Tokenize(digits); !slices.Equal(got, wantDigits) {
					t.Errorf("Tokenize(%q) = %q, want %q", digits, got, wantDigits)
				}
			})
		}
	}
}
