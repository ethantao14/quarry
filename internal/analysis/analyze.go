package analysis

import (
	"strings"
	"unicode"
)

// Analyze applies English possessive removal, lowercasing, stopwords, and Porter stemming.
func Analyze(text string) []string {
	var terms []string
	for _, token := range Tokenize(text) {
		token = lowercase(removePossessive(token))
		if token == "" || isStopword(token) {
			continue
		}
		terms = append(terms, porterStem(token))
	}
	return terms
}

func removePossessive(token string) string {
	runes := []rune(token)
	if len(runes) < 2 {
		return token
	}
	last := runes[len(runes)-1]
	quote := runes[len(runes)-2]
	if (last == 's' || last == 'S') && (quote == '\'' || quote == '\u2019' || quote == '\uFF07') {
		return string(runes[:len(runes)-2])
	}
	return token
}

func lowercase(token string) string {
	return strings.Map(unicode.ToLower, token)
}

func isStopword(token string) bool {
	switch token {
	case "a", "an", "and", "are", "as", "at", "be", "but", "by", "for", "if", "in", "into", "is", "it",
		"no", "not", "of", "on", "or", "such", "that", "the", "their", "then", "there", "these", "they",
		"this", "to", "was", "will", "with":
		return true
	default:
		return false
	}
}
