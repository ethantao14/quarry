// Package analysis turns raw text into the terms that get indexed and searched.
package analysis

import (
	"strings"
	"unicode"
)

// Tokenize lowercases text and splits it into runs of letters and digits.
func Tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), isSeparator)
}

func isSeparator(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
