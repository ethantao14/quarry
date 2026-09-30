package analysis

// porterStem applies the classic Porter (1980) stemming algorithm, following
// Martin Porter's reference implementation, which Lucene's PorterStemmer uses.
// Words of one or two runes are returned unchanged, as in Lucene.
func porterStem(word string) string {
	s := porterStemmer{word: []rune(word)}
	s.end = len(s.word) - 1
	if s.end > 1 {
		s.step1()
		s.step2()
		s.step3()
		s.step4()
		s.step5()
		s.step6()
	}
	return string(s.word[:s.end+1])
}

// porterStemmer holds the word being stemmed. The current word is word[0..end];
// when a suffix matches, stemEnd marks the last rune before that suffix.
type porterStemmer struct {
	word    []rune
	end     int
	stemEnd int
}

// isConsonant reports whether word[i] is a consonant. 'y' is a consonant
// at the start of a word or after a vowel.
func (s *porterStemmer) isConsonant(i int) bool {
	switch s.word[i] {
	case 'a', 'e', 'i', 'o', 'u':
		return false
	case 'y':
		return i == 0 || !s.isConsonant(i-1)
	default:
		return true
	}
}

// measure counts vowel-consonant sequences in word[0..stemEnd]:
// [C](VC){m}[V] has measure m. For example "tree" is 0, "trouble" is 1.
func (s *porterStemmer) measure() int {
	count := 0
	i := 0
	for i <= s.stemEnd && s.isConsonant(i) {
		i++
	}
	for {
		for i <= s.stemEnd && !s.isConsonant(i) {
			i++
		}
		if i > s.stemEnd {
			return count
		}
		for i <= s.stemEnd && s.isConsonant(i) {
			i++
		}
		count++
	}
}

// stemHasVowel reports whether word[0..stemEnd] contains a vowel.
func (s *porterStemmer) stemHasVowel() bool {
	for i := 0; i <= s.stemEnd; i++ {
		if !s.isConsonant(i) {
			return true
		}
	}
	return false
}

// endsWithDoubleConsonant reports whether word[i-1] and word[i] are the same consonant.
func (s *porterStemmer) endsWithDoubleConsonant(i int) bool {
	if i < 1 || s.word[i] != s.word[i-1] {
		return false
	}
	return s.isConsonant(i)
}

// isShortSyllable reports whether word[i-2..i] is consonant-vowel-consonant
// and the last consonant is not w, x, or y (as in "hop", but not "snow").
func (s *porterStemmer) isShortSyllable(i int) bool {
	if i < 2 || !s.isConsonant(i) || s.isConsonant(i-1) || !s.isConsonant(i-2) {
		return false
	}
	last := s.word[i]
	return last != 'w' && last != 'x' && last != 'y'
}

// endsWith reports whether word[0..end] ends with suffix, and if so sets stemEnd
// to the position just before it.
func (s *porterStemmer) endsWith(suffix string) bool {
	suffixRunes := []rune(suffix)
	start := s.end - len(suffixRunes) + 1
	if start < 0 {
		return false
	}
	for i, r := range suffixRunes {
		if s.word[start+i] != r {
			return false
		}
	}
	s.stemEnd = s.end - len(suffixRunes)
	return true
}

// replaceSuffix replaces word[stemEnd+1..end] with replacement.
func (s *porterStemmer) replaceSuffix(replacement string) {
	s.word = append(s.word[:s.stemEnd+1], []rune(replacement)...)
	s.end = s.stemEnd + len([]rune(replacement))
}

// replaceIfMeasured replaces the matched suffix only when the stem's measure is above 0.
func (s *porterStemmer) replaceIfMeasured(replacement string) {
	if s.measure() > 0 {
		s.replaceSuffix(replacement)
	}
}

// step1 removes plurals and -ed or -ing: caresses -> caress, ponies -> poni,
// agreed -> agree, hopping -> hop, hoping -> hope.
func (s *porterStemmer) step1() {
	if s.word[s.end] == 's' {
		switch {
		case s.endsWith("sses"):
			s.end -= 2
		case s.endsWith("ies"):
			s.replaceSuffix("i")
		case s.word[s.end-1] != 's':
			s.end--
		}
	}

	if s.endsWith("eed") {
		if s.measure() > 0 {
			s.end--
		}
		return
	}
	endsWithEdOrIng := s.endsWith("ed") || s.endsWith("ing")
	if !endsWithEdOrIng || !s.stemHasVowel() {
		return
	}
	s.end = s.stemEnd
	switch {
	case s.endsWith("at"):
		s.replaceSuffix("ate")
	case s.endsWith("bl"):
		s.replaceSuffix("ble")
	case s.endsWith("iz"):
		s.replaceSuffix("ize")
	case s.endsWithDoubleConsonant(s.end):
		last := s.word[s.end]
		if last != 'l' && last != 's' && last != 'z' {
			s.end--
		}
	case s.measure() == 1 && s.isShortSyllable(s.end):
		s.replaceSuffix("e")
	}
}

// step2 turns a final y into i when the stem has another vowel: happy -> happi.
func (s *porterStemmer) step2() {
	if s.endsWith("y") && s.stemHasVowel() {
		s.word[s.end] = 'i'
	}
}

// step3Rules maps double suffixes to single ones, grouped by the suffix's
// second-to-last rune: relational -> relate, digitizer -> digitize.
var step3Rules = map[rune][][2]string{
	'a': {{"ational", "ate"}, {"tional", "tion"}},
	'c': {{"enci", "ence"}, {"anci", "ance"}},
	'e': {{"izer", "ize"}},
	'l': {{"bli", "ble"}, {"alli", "al"}, {"entli", "ent"}, {"eli", "e"}, {"ousli", "ous"}},
	'o': {{"ization", "ize"}, {"ation", "ate"}, {"ator", "ate"}},
	's': {{"alism", "al"}, {"iveness", "ive"}, {"fulness", "ful"}, {"ousness", "ous"}},
	't': {{"aliti", "al"}, {"iviti", "ive"}, {"biliti", "ble"}},
	'g': {{"logi", "log"}},
}

// step4Rules handles -ic-, -full, -ness and similar, grouped by the suffix's last rune.
var step4Rules = map[rune][][2]string{
	'e': {{"icate", "ic"}, {"ative", ""}, {"alize", "al"}},
	'i': {{"iciti", "ic"}},
	'l': {{"ical", "ic"}, {"ful", ""}},
	's': {{"ness", ""}},
}

func (s *porterStemmer) step3() {
	if s.end == 0 {
		return
	}
	s.applyFirstMatch(step3Rules[s.word[s.end-1]])
}

func (s *porterStemmer) step4() {
	s.applyFirstMatch(step4Rules[s.word[s.end]])
}

// applyFirstMatch applies the first rule whose suffix matches, then stops,
// even if the stem's measure is too small for the replacement to happen.
func (s *porterStemmer) applyFirstMatch(rules [][2]string) {
	for _, rule := range rules {
		if s.endsWith(rule[0]) {
			s.replaceIfMeasured(rule[1])
			return
		}
	}
}

// step5Suffixes are removed when the stem's measure is above 1, grouped by the
// suffix's second-to-last rune: revival -> reviv, adjustment -> adjust.
var step5Suffixes = map[rune][]string{
	'a': {"al"},
	'c': {"ance", "ence"},
	'e': {"er"},
	'i': {"ic"},
	'l': {"able", "ible"},
	'n': {"ant", "ement", "ment", "ent"},
	'o': {"ion", "ou"},
	's': {"ism"},
	't': {"ate", "iti"},
	'u': {"ous"},
	'v': {"ive"},
	'z': {"ize"},
}

func (s *porterStemmer) step5() {
	if s.end == 0 {
		return
	}
	for _, suffix := range step5Suffixes[s.word[s.end-1]] {
		if !s.endsWith(suffix) {
			continue
		}
		// "-ion" is only removed after s or t: adoption -> adopt, but not "lion".
		if suffix == "ion" && (s.stemEnd < 0 || (s.word[s.stemEnd] != 's' && s.word[s.stemEnd] != 't')) {
			continue
		}
		if s.measure() > 1 {
			s.end = s.stemEnd
		}
		return
	}
}

// step6 removes a final -e when the measure allows it, and reduces a final
// double l: probate -> probat, controll -> control.
func (s *porterStemmer) step6() {
	s.stemEnd = s.end
	if s.word[s.end] == 'e' {
		m := s.measure()
		if m > 1 || (m == 1 && !s.isShortSyllable(s.end-1)) {
			s.end--
		}
	}
	if s.word[s.end] == 'l' && s.endsWithDoubleConsonant(s.end) && s.measure() > 1 {
		s.end--
	}
}
