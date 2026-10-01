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
// Like Lucene, a token is matched within a 255-rune window starting at its first
// rune; scanning then resumes right after the token.
func Tokenize(text string) []string {
	runes := []rune(text)
	var tokens []string
	for start := 0; start < len(runes); {
		limit := min(start+maxTokenLength, len(runes))
		end := start + 1
		switch classify(runes[start]) {
		case ideograph:
			end = skipExtends(runes, end, limit)
		case symbol:
			end = scanSymbol(runes, start, limit)
		case letter, digit, connector:
			var hasBase bool
			end, hasBase = scanWord(runes, start, limit)
			if !hasBase {
				start = end
				continue
			}
		default:
			start = end
			continue
		}
		tokens = append(tokens, string(runes[start:end]))
		start = end
	}
	return tokens
}

// scanSymbol returns where the symbol token starting at runes[start] ends. It keeps
// following extend runes, then one optional emoji presentation selector (U+FE0F).
func scanSymbol(runes []rune, start, limit int) int {
	end := start + 1
	for end < limit && classify(runes[end]) == extend && !isVariationSelector(runes[end]) {
		end++
	}
	if end < limit && runes[end] == '\uFE0F' {
		end++
	}
	return end
}

// scanWord returns where the word starting at runes[start] ends, looking no further
// than limit, and whether it contains a letter or digit (a run of only connectors
// is not a token).
func scanWord(runes []rune, start, limit int) (int, bool) {
	lastKind := separator
	var lastBase rune
	hasBase := false
	// A Hebrew letter only takes a quote when it was not itself joined on by a
	// mid character (Lucene's grammar treats "b'א'" differently from "א'").
	afterMid, lastBaseAfterMid := false, false
	// The letter after a Hebrew double quote ends that unit; mids cannot extend it.
	afterDoubleQuote, lastBaseAfterDoubleQuote := false, false
	for pos := start; pos < limit; pos++ {
		kind := classify(runes[pos])
		switch kind {
		case letter, digit, connector:
			lastKind = kind
			lastBase = runes[pos]
			lastBaseAfterMid, afterMid = afterMid, false
			lastBaseAfterDoubleQuote, afterDoubleQuote = afterDoubleQuote, false
			if kind == letter || kind == digit {
				hasBase = true
			}
		case extend:
		default:
			startsHebrewUnit := isHebrewLetter(lastBase) && !lastBaseAfterMid
			if runes[pos] == '\'' && startsHebrewUnit {
				lastKind = separator
				lastBase = 0
				continue
			}
			next := skipExtends(runes, pos+1, limit)
			if next >= limit {
				return pos, hasBase
			}
			joinsLetters := lastKind == letter && !lastBaseAfterDoubleQuote &&
				classify(runes[next]) == letter && isMidLetter(runes[pos])
			joinsDigits := lastKind == digit && classify(runes[next]) == digit && isMidNum(runes[pos])
			joinsHebrew := runes[pos] == '"' && startsHebrewUnit && isHebrewLetter(runes[next])
			if !joinsLetters && !joinsDigits && !joinsHebrew {
				return pos, hasBase
			}
			afterMid = true
			afterDoubleQuote = joinsHebrew
			pos = next - 1
		}
	}
	return limit, hasBase
}

func skipExtends(runes []rune, pos, limit int) int {
	for pos < limit && classify(runes[pos]) == extend {
		pos++
	}
	return pos
}

func classify(r rune) runeKind {
	switch {
	case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r):
		return ideograph
	case unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || isCircledLetter(r):
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

// isCircledLetter reports whether r is a circled Latin letter (Ⓐ to ⓩ), which
// Unicode word breaking treats as a letter although its category is a symbol.
func isCircledLetter(r rune) bool {
	return r >= 'Ⓐ' && r <= 'ⓩ'
}

// isVariationSelector reports whether r selects text (U+FE0E) or emoji (U+FE0F) style.
func isVariationSelector(r rune) bool {
	return r == '\uFE0E' || r == '\uFE0F'
}

func isHebrewLetter(r rune) bool {
	return unicode.Is(unicode.Hebrew, r) && unicode.IsLetter(r)
}
