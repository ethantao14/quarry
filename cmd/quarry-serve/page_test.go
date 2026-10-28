package main

import (
	"html"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
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

func TestInstrumentPageControls(t *testing.T) {
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
		for _, algo := range []string{"bmw", "wand", "exhaustive"} {
			if !pageHasTag(fieldset, "input", map[string]string{"type": "radio", "name": "algo", "value": algo}) {
				t.Errorf("missing algo radio %q inside fieldset", algo)
			}
		}
		for _, label := range []string{"Block-Max WAND", "WAND", "Exhaustive"} {
			if !regexp.MustCompile(`(?is)<label\b[^>]*>[^<]*(?:<[^>]+>[^<]*)*?` + regexp.QuoteMeta(label) + `.*?</label\s*>`).MatchString(fieldset) {
				t.Errorf("missing algorithm label %q", label)
			}
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

func TestInstrumentPageCopy(t *testing.T) {
	body := servedPage(t)
	for _, want := range []string{"quarry", "A disk-based search engine written from scratch in Go.", "vitamin d bone density", "sleep deprivation memory", "antibiotic resistance", "Searching…", "Error: ", "Could not reach the server.", "Show more", "Show less"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(html.UnescapeString(body), want) {
				t.Errorf("page missing %q", want)
			}
		})
	}
	footer := regexp.MustCompile(`(?is)<footer\b[^>]*>.*?</footer\s*>`).FindString(body)
	for _, want := range []string{"BEIR", "SciFact", "AI2", "CC BY-NC 2.0"} {
		if !strings.Contains(html.UnescapeString(footer), want) {
			t.Errorf("footer missing credit %q", want)
		}
	}
	if !pageHasTag(footer, "a", map[string]string{"href": "https://github.com/ethantao14/quarry"}) {
		t.Error("footer missing source link")
	}
}

func TestInstrumentPageTheme(t *testing.T) {
	body := servedPage(t)
	for _, tt := range []struct{ name, pattern string }{
		{"dark theme", `@media\s*[^{}]*\(\s*prefers-color-scheme\s*:\s*dark\s*\)`},
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

func TestInstrumentPageSelfContained(t *testing.T) {
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

func TestInstrumentPageScriptContract(t *testing.T) {
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

func TestInstrumentPageNoPurple(t *testing.T) {
	body := servedPage(t)
	// Consume the whole hex token so longer values cannot be mistaken for #rgb.
	for _, token := range regexp.MustCompile(`(?i)#[0-9a-f]+\b`).FindAllString(body, -1) {
		hex := token[1:]
		if len(hex) != 3 && len(hex) != 6 {
			continue
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
		// HSL saturation distinguishes muted greys from saturated purple.
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
		if hue >= 250 && hue <= 320 && saturation > 0.25 {
			t.Errorf("purple color %s: hue %.1f°, saturation %.1f%%", token, hue, saturation*100)
		}
	}
}
