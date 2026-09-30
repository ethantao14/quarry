// Package analysis turns raw text into the terms that get indexed and searched.
package analysis

import "unicode"

type runeKind uint8

const (
	separator runeKind = iota
	ideograph
	letter
	digit
	connector
	extend
	symbol
)

const maxTokenLength = 255

// Tokenize splits text into raw Unicode tokens of at most 255 runes.
func Tokenize(text string) []string {
	runes := []rune(text)
	var tokens []string
	for start := 0; start < len(runes); {
		end := start + 1
		switch classify(runes[start]) {
		case ideograph:
			end = skipExtends(runes, end)
		case symbol:
		case letter, digit, connector:
			var hasBase bool
			end, hasBase = scanWord(runes, start)
			if !hasBase {
				start = end
				continue
			}
		default:
			start = end
			continue
		}
		for start < end {
			pieceEnd := min(start+maxTokenLength, end)
			tokens = append(tokens, string(runes[start:pieceEnd]))
			start = pieceEnd
		}
	}
	return tokens
}

// scanWord returns where the word starting at runes[start] ends, and whether it
// contains a letter or digit (a run of only connectors is not a token).
func scanWord(runes []rune, start int) (int, bool) {
	lastKind := separator
	var lastBase rune
	hasBase := false
	for pos := start; pos < len(runes); pos++ {
		kind := classify(runes[pos])
		switch kind {
		case letter, digit, connector:
			lastKind = kind
			lastBase = runes[pos]
			if kind == letter || kind == digit {
				hasBase = true
			}
		case extend:
		default:
			if runes[pos] == '\'' && isHebrewLetter(lastBase) {
				lastKind = separator
				lastBase = 0
				continue
			}
			next := skipExtends(runes, pos+1)
			if next == len(runes) {
				return pos, hasBase
			}
			joinsLetters := lastKind == letter && classify(runes[next]) == letter && isMidLetter(runes[pos])
			joinsDigits := lastKind == digit && classify(runes[next]) == digit && isMidNum(runes[pos])
			joinsHebrew := runes[pos] == '"' && isHebrewLetter(lastBase) && isHebrewLetter(runes[next])
			if !joinsLetters && !joinsDigits && !joinsHebrew {
				return pos, hasBase
			}
			pos = next - 1
		}
	}
	return len(runes), hasBase
}

func skipExtends(runes []rune, pos int) int {
	for pos < len(runes) && classify(runes[pos]) == extend {
		pos++
	}
	return pos
}

func classify(r rune) runeKind {
	switch {
	case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r):
		return ideograph
	case unicode.IsLetter(r):
		return letter
	case unicode.IsDigit(r):
		return digit
	case unicode.Is(unicode.Pc, r):
		return connector
	case r != '\u200B' && unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me, unicode.Cf):
		return extend
	case r == '©' || r == '®' || r == '™':
		return symbol
	default:
		return separator
	}
}

func isMidLetter(r rune) bool {
	switch r {
	case ':', '\u00B7', '\u0387', '\u055F', '\u05F4', '\u2027', '\uFE13', '\uFE55', '\uFF1A',
		'.', '\u2018', '\u2019', '\u2024', '\uFE52', '\uFF07', '\uFF0E', '\'':
		return true
	default:
		return false
	}
}

func isMidNum(r rune) bool {
	switch r {
	case ',', ';', '\u037E', '\u0589', '\u060C', '\u060D', '\u066C', '\u07F8', '\u2044',
		'\uFE10', '\uFE14', '\uFE50', '\uFE54', '\uFF0C', '\uFF1B',
		'.', '\u2018', '\u2019', '\u2024', '\uFE52', '\uFF07', '\uFF0E', '\'':
		return true
	default:
		return false
	}
}

func isHebrewLetter(r rune) bool {
	return unicode.Is(unicode.Hebrew, r) && unicode.IsLetter(r)
}
