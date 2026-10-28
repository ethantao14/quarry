package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFontFiles(t *testing.T) {
	t.Run("woff2 magic", func(t *testing.T) {
		font, err := os.ReadFile("fonts/bricolage-grotesque-latin.woff2")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(font, []byte("wOF2")) {
			t.Error("font is missing wOF2 magic bytes")
		}
	})
	t.Run("license", func(t *testing.T) {
		license, err := os.ReadFile("fonts/OFL.txt")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(string(license)), "sil open font license") {
			t.Error("OFL.txt does not mention SIL Open Font License")
		}
	})
}

func TestFontGet(t *testing.T) {
	font, err := os.ReadFile("fonts/bricolage-grotesque-latin.woff2")
	if err != nil {
		t.Fatal(err)
	}
	_, ix := savedIndex(t)
	w := request(testHandler(t, ix, testOptions()), http.MethodGet, "/fonts/bricolage-grotesque.woff2")
	if w.Code != http.StatusOK {
		t.Errorf("GET font status = %d, want 200", w.Code)
	}
	for _, tt := range []struct{ name, want string }{
		{"Content-Type", "font/woff2"},
		{"Cache-Control", "public, max-age=86400"},
	} {
		if got := w.Header().Get(tt.name); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
	if !bytes.Equal(w.Body.Bytes(), font) {
		t.Error("GET font body differs from fonts/bricolage-grotesque-latin.woff2")
	}
}

func TestFontHead(t *testing.T) {
	_, ix := savedIndex(t)
	// Use a real server to verify HEAD's wire response has no body.
	server := httptest.NewServer(testHandler(t, ix, testOptions()))
	defer server.Close()
	response, err := server.Client().Head(server.URL + "/fonts/bricolage-grotesque.woff2")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Errorf("HEAD font status = %d, want 200", response.StatusCode)
	}
	if len(body) != 0 {
		t.Errorf("HEAD font body has %d bytes, want 0", len(body))
	}
	for _, tt := range []struct{ name, want string }{
		{"Content-Type", "font/woff2"},
		{"Cache-Control", "public, max-age=86400"},
	} {
		if got := response.Header.Get(tt.name); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFontRouting(t *testing.T) {
	_, ix := savedIndex(t)
	h := testHandler(t, ix, testOptions())
	t.Run("POST", func(t *testing.T) {
		w := request(h, http.MethodPost, "/fonts/bricolage-grotesque.woff2")
		if got := w.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("Allow = %q, want GET, HEAD", got)
		}
		checkError(t, w, http.StatusMethodNotAllowed, "method not allowed")
	})
	for _, path := range []string{"/fonts/", "/fonts/other.woff2", "/fonts/../page.html"} {
		t.Run(path, func(t *testing.T) {
			checkError(t, request(h, http.MethodGet, path), http.StatusNotFound, "not found")
		})
	}
}
