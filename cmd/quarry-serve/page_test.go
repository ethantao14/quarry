package main

import (
	"html"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/analysis"
)

func servedPage(t *testing.T) string {
	t.Helper()
	_, ix := savedIndex(t)
	w := request(testHandler(t, ix, testOptions()), http.MethodGet, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", w.Code)
	}
	return w.Body.String()
}

// Match attributes independently of their order, quote style, and whitespace.
func pageAttribute(tag, name string) string {
	re := regexp.MustCompile(`(?i)\s` + regexp.QuoteMeta(name) + `\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	match := re.FindStringSubmatch(tag)
	if match == nil {
		return ""
	}
	return html.UnescapeString(match[1] + match[2] + match[3])
}

func pageHasTag(body, name string, attrs map[string]string) bool {
	for _, tag := range regexp.MustCompile(`(?is)<`+name+`\b[^>]*>`).FindAllString(body, -1) {
		matches := true
		for key, value := range attrs {
			if pageAttribute(tag, key) != value {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func TestDaylightPageControls(t *testing.T) {
	body := servedPage(t)
	for _, tt := range []struct {
		name, tag string
		attrs     map[string]string
	}{
		{"query", "input", map[string]string{"id": "q", "name": "q"}},
		{"result limit", "select", map[string]string{"id": "k", "name": "k"}},
		{"submit", "button", map[string]string{"type": "submit"}},
		{"readout", `[a-z][a-z0-9]*`, map[string]string{"id": "status", "role": "status"}},
		{"results", "ol", map[string]string{"id": "results"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !pageHasTag(body, tt.tag, tt.attrs) {
				t.Errorf("missing %s with attributes %v", tt.tag, tt.attrs)
			}
		})
	}
	t.Run("algorithm fieldset", func(t *testing.T) {
		var fieldset string
		for _, candidate := range regexp.MustCompile(`(?is)<fieldset\b[^>]*>.*?</fieldset\s*>`).FindAllString(body, -1) {
			if regexp.MustCompile(`(?is)<legend\b[^>]*>\s*Retrieval algorithm\s*</legend\s*>`).MatchString(candidate) {
				fieldset = candidate
				break
			}
		}
		if fieldset == "" {
			t.Fatal("missing fieldset with legend Retrieval algorithm")
		}
		labels := regexp.MustCompile(`(?is)<label\b[^>]*>.*?</label\s*>`).FindAllString(fieldset, -1)
		for _, tt := range []struct{ value, label string }{
			{"bmw", "Block-Max WAND"},
			{"wand", "WAND"},
			{"exhaustive", "Exhaustive"},
		} {
			t.Run(tt.value, func(t *testing.T) {
				var matchingLabels []string
				for _, label := range labels {
					if pageHasTag(label, "input", map[string]string{"type": "radio", "name": "algo", "value": tt.value}) {
						matchingLabels = append(matchingLabels, label)
					}
				}
				if len(matchingLabels) != 1 {
					t.Fatalf("algo radio %q has %d containing labels, want 1", tt.value, len(matchingLabels))
				}
				text := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(matchingLabels[0], "")
				text = strings.Join(strings.Fields(html.UnescapeString(text)), " ")
				if text != tt.label {
					t.Errorf("algo radio %q label = %q, want exactly %q", tt.value, text, tt.label)
				}
			})
		}
	})
	t.Run("limit options", func(t *testing.T) {
		var selectHTML string
		for _, candidate := range regexp.MustCompile(`(?is)<select\b[^>]*>.*?</select\s*>`).FindAllString(body, -1) {
			if pageAttribute(candidate[:strings.Index(candidate, ">")+1], "id") == "k" {
				selectHTML = candidate
				break
			}
		}
		for _, value := range []string{"10", "50", "100"} {
			if !pageHasTag(selectHTML, "option", map[string]string{"value": value}) {
				t.Errorf("k select missing option %s", value)
			}
		}
	})
}

func TestDaylightPageCopy(t *testing.T) {
	body := servedPage(t)
	for _, want := range []string{"quarry", "A disk-based search engine written from scratch in Go.", "vitamin d bone density", "sleep deprivation memory", "antibiotic resistance", "Searching…", "Error: ", "Could not reach the server.", "Show more", "Show less"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(html.UnescapeString(body), want) {
				t.Errorf("page missing %q", want)
			}
		})
	}
	footer := regexp.MustCompile(`(?is)<footer\b[^>]*>.*?</footer\s*>`).FindString(body)
	for _, want := range []string{"BEIR", "SciFact", "AI2", "CC BY-NC 2.0", "Type: Bricolage Grotesque (SIL OFL 1.1)."} {
		if !strings.Contains(html.UnescapeString(footer), want) {
			t.Errorf("footer missing credit %q", want)
		}
	}
	if !pageHasTag(footer, "a", map[string]string{"href": "https://github.com/ethantao14/quarry"}) {
		t.Error("footer missing source link")
	}
}

func TestDaylightPageTheme(t *testing.T) {
	body := servedPage(t)
	if regexp.MustCompile(`(?i)prefers-color-scheme`).MatchString(body) {
		t.Error("light-only page must not contain a prefers-color-scheme query")
	}
	for _, tt := range []struct{ name, pattern string }{
		{"light theme", `:root\s*\{[^}]*\bcolor-scheme\s*:\s*light\s*(?:;|})`},
		{"cobalt token", `:root\s*\{[^}]*--cobalt\s*:\s*#2a44ff\s*(?:;|})`},
		{"mint token", `:root\s*\{[^}]*--mint\s*:\s*#2fd6a3\s*(?:;|})`},
		{"reduced motion", `@media\s*[^{}]*\(\s*prefers-reduced-motion(?:\s*:\s*reduce)?\s*\)`},
		{"focus rings", `:focus-visible\b`},
		{"root custom properties", `:root\s*\{[^}]*--[\w-]+\s*:`},
		{"custom property use", `var\(\s*--[\w-]+`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !regexp.MustCompile(`(?is)` + tt.pattern).MatchString(body) {
				t.Errorf("missing %s", tt.name)
			}
		})
	}
}

func TestDaylightPageFont(t *testing.T) {
	body := servedPage(t)
	var face string
	for _, candidate := range regexp.MustCompile(`(?is)@font-face\s*\{[^}]*}`).FindAllString(body, -1) {
		if regexp.MustCompile(`(?i)font-family\s*:\s*["']Bricolage Grotesque["']\s*(?:;|})`).MatchString(candidate) {
			face = candidate
			break
		}
	}
	if face == "" {
		t.Fatal("missing Bricolage Grotesque @font-face")
	}
	for _, tt := range []struct{ name, pattern string }{
		{"font source", `src\s*:\s*url\(\s*["']/fonts/bricolage-grotesque\.woff2["']\s*\)\s*format\(\s*["']woff2["']\s*\)\s*(?:;|})`},
		{"variable weights", `font-weight\s*:\s*400\s+800\s*(?:;|})`},
		{"font display", `font-display\s*:\s*swap\s*(?:;|})`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !regexp.MustCompile(`(?is)` + tt.pattern).MatchString(face) {
				t.Errorf("missing %s in Bricolage Grotesque @font-face", tt.name)
			}
		})
	}
}

func TestDaylightPageSelfContained(t *testing.T) {
	body := servedPage(t)
	for _, tag := range regexp.MustCompile(`(?is)<(?:script|link)\b[^>]*>`).FindAllString(body, -1) {
		if strings.HasPrefix(strings.ToLower(tag), "<script") && pageAttribute(tag, "src") != "" {
			t.Error("page loads a script resource")
		}
		for _, rel := range strings.Fields(strings.ToLower(pageAttribute(tag, "rel"))) {
			if rel == "stylesheet" {
				t.Error("page loads a stylesheet resource")
			}
		}
	}
	for _, pattern := range []string{`(?i)@import\b`, `(?i)url\(\s*["']?\s*(?:https?:)?//`, `(?i)(?:src|href)\s*=\s*["']\s*//`} {
		if regexp.MustCompile(pattern).MatchString(body) {
			t.Errorf("external resource matches %s", pattern)
		}
	}
	for _, address := range regexp.MustCompile(`(?i)https?://[^\s"'<>`+"`"+`\)]+`).FindAllString(body, -1) {
		if address != "https://github.com/ethantao14/quarry" {
			t.Errorf("unexpected absolute URL %q", address)
		}
	}
}

func TestDaylightPageScriptContract(t *testing.T) {
	body := servedPage(t)
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write"} {
		if strings.Contains(body, banned) {
			t.Errorf("page contains unsafe DOM API %s", banned)
		}
	}
	for _, tt := range []struct{ name, pattern string }{
		{"mark creation", `createElement\(\s*["']mark["']\s*\)`},
		{"history", `history\s*\.\s*pushState\s*\(`},
		{"back and forward", `["']popstate["']`},
		{"search request", `fetch\(\s*["']/search\?["']\s*\+`},
		{"health request", `fetch\(\s*["']/healthz["']\s*\)`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !regexp.MustCompile(tt.pattern).MatchString(body) {
				t.Errorf("missing %s", tt.name)
			}
		})
	}
}

func TestDaylightPageNoPurpleOrOrange(t *testing.T) {
	body := servedPage(t)
	// Consume the whole hex token so longer values cannot be mistaken for #rgb.
	for _, token := range regexp.MustCompile(`(?i)#[0-9a-f]+\b`).FindAllString(body, -1) {
		hex := token[1:]
		if len(hex) != 3 && len(hex) != 4 && len(hex) != 6 && len(hex) != 8 {
			continue
		}
		// Ignore alpha when checking the hue of CSS hex colors.
		if len(hex) == 4 {
			hex = hex[:3]
		} else if len(hex) == 8 {
			hex = hex[:6]
		}
		if len(hex) == 3 {
			hex = strings.Repeat(hex[0:1], 2) + strings.Repeat(hex[1:2], 2) + strings.Repeat(hex[2:3], 2)
		}
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		r, g, b := float64(n>>16)/255, float64((n>>8)&255)/255, float64(n&255)/255
		high, low := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
		delta := high - low
		if delta == 0 {
			continue
		}
		// HSL saturation distinguishes muted greys from saturated colors.
		saturation := delta / (1 - math.Abs(high+low-1))
		var hue float64
		switch high {
		case r:
			hue = 60 * math.Mod((g-b)/delta, 6)
		case g:
			hue = 60 * ((b-r)/delta + 2)
		case b:
			hue = 60 * ((r-g)/delta + 4)
		}
		if hue < 0 {
			hue += 360
		}
		if hue >= 15 && hue <= 45 && saturation > 0.50 {
			t.Errorf("orange color %s: hue %.1f°, saturation %.1f%%", token, hue, saturation*100)
		}
		if hue >= 250 && hue <= 320 && saturation > 0.25 {
			t.Errorf("purple color %s: hue %.1f°, saturation %.1f%%", token, hue, saturation*100)
		}
	}
}

func TestDaylightPageAccessibleNames(t *testing.T) {
	body := servedPage(t)
	t.Run("single status readout", func(t *testing.T) {
		count := 0
		for _, tag := range regexp.MustCompile(`(?is)<[a-z][a-z0-9]*\b[^>]*>`).FindAllString(body, -1) {
			if pageAttribute(tag, "role") == "status" {
				count++
				if id := pageAttribute(tag, "id"); id != "status" {
					t.Errorf("element with role=status has id=%q, want status", id)
				}
			}
		}
		if count != 1 {
			t.Errorf("found %d elements with role=status, want exactly 1", count)
		}
	})
	t.Run("query label", func(t *testing.T) {
		if !pageHasTag(body, "label", map[string]string{"for": "q"}) {
			t.Error("missing label for=q")
		}
		for _, tag := range regexp.MustCompile(`(?is)<input\b[^>]*>`).FindAllString(body, -1) {
			if pageAttribute(tag, "id") == "q" && regexp.MustCompile(`(?i)\saria-label(?:\s|=|/?>)`).MatchString(tag) {
				t.Error("input#q has an aria-label; its label should supply its accessible name")
			}
		}
	})
}

func TestDaylightPageStopwords(t *testing.T) {
	body := servedPage(t)
	declarations := regexp.MustCompile(`(?s)\bconst\s+stopwords\s*=\s*new\s+Set\s*\(\s*\[(.*?)\]\s*\)\s*;`).FindAllStringSubmatch(body, -1)
	if len(declarations) != 1 {
		t.Fatalf("found %d const stopwords = new Set([...]); declarations, want 1", len(declarations))
	}
	// The spec requires double-quoted entries; allow whitespace and a trailing comma.
	list := strings.TrimSpace(declarations[0][1])
	list = strings.TrimSuffix(list, ",")
	entries := strings.Split(list, ",")
	words := make(map[string]bool)
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if len(entry) < 2 || entry[0] != '"' || entry[len(entry)-1] != '"' {
			t.Fatalf("stopword entry %q must be a double-quoted string", entry)
		}
		word, err := strconv.Unquote(entry)
		if err != nil {
			t.Fatalf("invalid stopword entry %q: %v", entry, err)
		}
		if words[word] {
			t.Errorf("duplicate stopword %q", word)
		}
		words[word] = true
		t.Run("listed/"+word, func(t *testing.T) {
			if tokens := analysis.Analyze(word); len(tokens) != 0 {
				t.Errorf("Analyze(%q) = %v; listed stopwords must produce no tokens", word, tokens)
			}
		})
	}
	if len(entries) != 33 || len(words) != 33 {
		t.Errorf("stopword list has %d entries and %d unique words, want 33 of each", len(entries), len(words))
	}
	// Include every spec word and retained words, notably "now" alongside "not".
	candidates := strings.Fields("a an and are as at be but by for if in into is it no not of on or such that the their then there these they this to was will with sleep vitamin now")
	for _, word := range candidates {
		t.Run("coverage/"+word, func(t *testing.T) {
			dropped := len(analysis.Analyze(word)) == 0
			if words[word] != dropped {
				t.Errorf("stopwords contains %q = %t, analyzer drops it = %t", word, words[word], dropped)
			}
		})
	}
}

func TestDaylightPageSafari15APIs(t *testing.T) {
	body := servedPage(t)
	for _, banned := range []string{"Object.hasOwn", "requestSubmit"} {
		t.Run(banned, func(t *testing.T) {
			if strings.Contains(body, banned) {
				t.Errorf("page contains API newer than Safari 15: %s", banned)
			}
		})
	}
}

func TestDaylightPageReviewAddendum(t *testing.T) {
	body := servedPage(t)
	script := strings.Join(regexp.MustCompile(`(?is)<script\b[^>]*>(.*?)</script\s*>`).FindAllString(body, -1), "\n")
	css := strings.Join(regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style\s*>`).FindAllString(body, -1), "\n")

	t.Run("animated latency hidden from screen readers", func(t *testing.T) {
		if count := strings.Count(script, `setAttribute("aria-hidden", "true")`); count < 2 {
			t.Errorf("script has %d aria-hidden=true calls, want at least 2", count)
		}
		// Capture the element identifier, then require that same element to be
		// hidden directly or by a for...of loop containing it in an array literal.
		latency := regexp.MustCompile(`\b(?:const|let|var)\s+([\w$]+)\s*=\s*[\w$.]+\([^;\n]*["']latency["'][^;\n]*\)`).FindStringSubmatch(script)
		if latency == nil {
			t.Fatal(`script missing element creation with class "latency"`)
		}
		hideCall := `\s*\.\s*setAttribute\(\s*["']aria-hidden["']\s*,\s*["']true["']\s*\)`
		hidden := regexp.MustCompile(`\b` + regexp.QuoteMeta(latency[1]) + hideCall).MatchString(script)
		for _, loop := range regexp.MustCompile(`(?s)\bfor\s*\(\s*(?:const|let|var)\s+([\w$]+)\s+of\s*\[([^\]]*)\]\s*\)\s*(?:\{([^{}]*)\}|([^;{}]*;))`).FindAllStringSubmatch(script, -1) {
			for _, element := range strings.Split(loop[2], ",") {
				if strings.TrimSpace(element) == latency[1] && regexp.MustCompile(`\b`+regexp.QuoteMeta(loop[1])+hideCall).MatchString(loop[3]+loop[4]) {
					hidden = true
				}
			}
		}
		if !hidden {
			t.Errorf("visible latency element %s must have aria-hidden=true", latency[1])
		}
	})
	t.Run("visually hidden status text", func(t *testing.T) {
		if !strings.Contains(script, `"visually-hidden"`) {
			t.Error(`script missing "visually-hidden" class for status text`)
		}
		if !regexp.MustCompile(`["']Latency ["']\s*\+`).MatchString(script) {
			t.Error(`script must build status text with the prefix "Latency "`)
		}
		if !regexp.MustCompile(`\.visually-hidden\s*\{[^}]*}`).MatchString(css) {
			t.Error("CSS missing .visually-hidden rule")
		}
	})

	// Restrict declarations to the search input's base and focus rules.
	searchSelector := `input\[type\s*=\s*["']?search["']?\]`
	t.Run("search focus-visible forced-colors outline", func(t *testing.T) {
		rule := regexp.MustCompile(searchSelector + `:focus-visible\s*\{([^}]*)}`).FindStringSubmatch(css)
		if rule == nil {
			t.Fatal("CSS missing search input :focus-visible rule")
		}
		if !regexp.MustCompile(`(?:^|;)\s*outline\s*:\s*2px\s+solid\s+transparent\s*(?:;|$)`).MatchString(rule[1]) {
			t.Error("search input :focus-visible must set outline: 2px solid transparent")
		}
		if regexp.MustCompile(`(?:^|;)\s*outline\s*:\s*none\s*(?:!important\s*)?(?:;|$)`).MatchString(rule[1]) {
			t.Error("search input :focus-visible must not set outline: none")
		}
	})
	baseRule := regexp.MustCompile(searchSelector + `\s*\{([^}]*)}`).FindStringSubmatch(css)
	focusRules := regexp.MustCompile(searchSelector+`:focus(?:-visible)?\s*\{([^}]*)}`).FindAllStringSubmatch(css, -1)
	var focusCSS string
	for _, rule := range focusRules {
		focusCSS += rule[1] + ";"
	}
	for _, property := range []string{"box-shadow", "background"} {
		t.Run("search focus "+property, func(t *testing.T) {
			if !regexp.MustCompile(`(?:^|;)\s*` + property + `\s*:\s*[^;\s][^;]*`).MatchString(focusCSS) {
				t.Errorf("search input focus rule missing %s", property)
			}
		})
	}
	t.Run("search base border width", func(t *testing.T) {
		if baseRule == nil {
			t.Fatal("CSS missing search input base rule")
		}
		if !regexp.MustCompile(`(?:^|;)\s*border-bottom(?:-width)?\s*:\s*4px(?:\s|;|$)`).MatchString(baseRule[1]) {
			t.Error("search input base rule must set border-bottom width to 4px")
		}
	})
}
