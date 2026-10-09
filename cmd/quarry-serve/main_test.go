package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/corpus"
	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

func savedIndex(t *testing.T) (string, *index.Disk) {
	t.Helper()
	ix, err := corpus.LoadFile("testdata/tiny.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "index")
	if err := ix.Write(path); err != nil {
		t.Fatal(err)
	}
	disk, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(); err != nil {
			t.Error(err)
		}
	})
	return path, disk
}

func testOptions() options {
	return options{timeout: 5 * time.Second, maxConcurrent: 2}
}

func testHandler(t *testing.T, ix searcher, opts options) http.Handler {
	t.Helper()
	h := newHandler(ix, opts)
	t.Cleanup(h.stop)
	return h
}

func request(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func checkJSON(t *testing.T, w *httptest.ResponseRecorder, status int, value any) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, status, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if err := json.Unmarshal(w.Body.Bytes(), value); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
}

func checkError(t *testing.T, w *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	var got map[string]string
	checkJSON(t, w, status, &got)
	if !reflect.DeepEqual(got, map[string]string{"error": message}) {
		t.Errorf("body = %v, want error %q", got, message)
	}
}

func TestSearch(t *testing.T) {
	_, ix := savedIndex(t)
	h := testHandler(t, ix, testOptions())
	tests := []struct {
		name string
		q    string
		algo string
		k    string
	}{
		{name: "defaults", q: "fish"},
		{name: "exhaustive", q: "red fish", algo: "exhaustive"},
		{name: "wand", q: "red fish", algo: "wand"},
		{name: "bmw", q: "red fish", algo: "bmw"},
		{name: "limited", q: "fish", k: "2"},
		{name: "no matches", q: "missing"},
		{name: "no analyzed terms", q: "the !?"},
		{name: "whitespace", q: "  Fish  "},
		{name: "maximum bytes", q: strings.Repeat("a", 1000)},
		{name: "maximum unicode bytes", q: strings.Repeat("é", 500)},
		{name: "maximum k", q: "fish", k: "1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := url.Values{"q": {tt.q}}
			algo, k := "bmw", 10
			if tt.algo != "" {
				values.Set("algo", tt.algo)
				algo = tt.algo
			}
			if tt.k != "" {
				values.Set("k", tt.k)
				var err error
				k, err = strconv.Atoi(tt.k)
				if err != nil {
					t.Fatal(err)
				}
			}
			w := request(h, http.MethodGet, "/search?"+values.Encode())
			var got searchResponse
			checkJSON(t, w, http.StatusOK, &got)
			if got.Query != tt.q || got.Algo != algo || got.K != k {
				t.Errorf("response parameters = %+v, want query %q, algo %q, k %d", got, tt.q, algo, k)
			}
			if got.TookMS <= 0 {
				t.Errorf("took_ms = %v, want positive duration", got.TookMS)
			}
			// Compare every algorithm to the same exhaustive baseline, without rounding scores.
			results, err := query.Search("exhaustive", ix, scoring.DefaultBM25(), analysis.Analyze(tt.q), k)
			if err != nil {
				t.Fatal(err)
			}
			want := make([]searchResult, len(results))
			for i, result := range results {
				want[i] = searchResult{Rank: i + 1, ID: ix.ExternalID(result.DocID), Score: result.Score}
			}
			if !reflect.DeepEqual(got.Results, want) {
				t.Errorf("results = %+v, want %+v", got.Results, want)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	_, ix := savedIndex(t)
	h := testHandler(t, ix, testOptions())
	tests := []struct {
		name   string
		params string
		want   string
	}{
		{"missing q", "", "q is required"},
		{"empty q", "q=", "q must not be blank"},
		{"blank q", "q=+%09+", "q must not be blank"},
		{"long q", "q=" + strings.Repeat("a", 1001), "q must be at most 1000 bytes"},
		{"long unicode q", "q=" + url.QueryEscape(strings.Repeat("é", 501)), "q must be at most 1000 bytes"},
		{"zero k", "q=fish&k=0", "k must be an integer from 1 to 1000"},
		{"negative k", "q=fish&k=-1", "k must be an integer from 1 to 1000"},
		{"large k", "q=fish&k=1001", "k must be an integer from 1 to 1000"},
		{"word k", "q=fish&k=abc", "k must be an integer from 1 to 1000"},
		{"fractional k", "q=fish&k=1.5", "k must be an integer from 1 to 1000"},
		{"empty k", "q=fish&k=", "k must be an integer from 1 to 1000"},
		{"bad algo", "q=fish&algo=bogus", "algo must be exhaustive, wand, or bmw"},
		{"empty algo", "q=fish&algo=", "algo must be exhaustive, wand, or bmw"},
		{"repeated q", "q=fish&q=car", "q must be given once"},
		{"repeated k", "q=fish&k=1&k=2", "k must be given once"},
		{"repeated algo", "q=fish&algo=bmw&algo=wand", "algo must be given once"},
		{"bad escape", "q=%zz", "invalid query parameters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := request(h, http.MethodGet, "/search?"+tt.params)
			checkError(t, w, http.StatusBadRequest, tt.want)
		})
	}
}

func TestRouting(t *testing.T) {
	_, ix := savedIndex(t)
	h := testHandler(t, ix, testOptions())
	for _, path := range []string{"/search", "/healthz"} {
		w := request(h, http.MethodPost, path)
		checkError(t, w, http.StatusMethodNotAllowed, "method not allowed")
		if got := w.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("Allow = %q, want GET, HEAD", got)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		checkError(t, request(h, method, "/nope"), http.StatusNotFound, "not found")
	}
	var health struct {
		Status    string `json:"status"`
		Documents int    `json:"documents"`
	}
	checkJSON(t, request(h, http.MethodGet, "/healthz"), http.StatusOK, &health)
	if health.Status != "ok" || health.Documents != ix.DocCount() {
		t.Errorf("health = %+v, want ok with %d documents", health, ix.DocCount())
	}
	// A recorder keeps HEAD bodies, so check HEAD through a real server, which drops them.
	server := httptest.NewServer(h)
	defer server.Close()
	for _, path := range []string{"/search?q=fish", "/healthz"} {
		response, err := http.Head(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || len(body) != 0 || response.Header.Get("Content-Type") != "application/json" {
			t.Errorf("HEAD %s: status = %d, headers = %v, body = %q", path, response.StatusCode, response.Header, body)
		}
	}
}

func TestSearchFailure(t *testing.T) {
	_, ix := savedIndex(t)
	var logs bytes.Buffer
	opts := testOptions()
	opts.errorLog = log.New(&logs, "", 0)
	opts.search = func(string, query.Index, scoring.BM25, []string, int) ([]query.Result, error) {
		return nil, errors.New("broken postings")
	}
	h := testHandler(t, ix, opts)
	checkError(t, request(h, http.MethodGet, "/search?q=fish"), http.StatusInternalServerError, "search failed")
	if !strings.Contains(logs.String(), "search failed: broken postings") {
		t.Errorf("log = %q, want underlying search error", logs.String())
	}
}

func TestSearchTimeout(t *testing.T) {
	_, ix := savedIndex(t)
	opts := testOptions()
	opts.timeout = 50 * time.Millisecond
	opts.search = func(string, query.Index, scoring.BM25, []string, int) ([]query.Result, error) {
		time.Sleep(100 * time.Millisecond)
		return nil, nil
	}
	h := testHandler(t, ix, opts)
	w := request(h, http.MethodGet, "/search?q=fish")
	checkError(t, w, http.StatusServiceUnavailable, "request timed out")
	if w.Body.String() != `{"error":"request timed out"}` {
		t.Errorf("timeout body = %q", w.Body.String())
	}
}

func TestConcurrencyLimit(t *testing.T) {
	_, ix := savedIndex(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int32
	opts := testOptions()
	opts.maxConcurrent = 1
	opts.search = func(algo string, ix query.Index, bm25 scoring.BM25, terms []string, k int) ([]query.Result, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return query.Search(algo, ix, bm25, terms, k)
	}
	h := testHandler(t, ix, opts)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- request(h, http.MethodGet, "/search?q=fish") }()
	await(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/search?q=fish", nil).WithContext(ctx))
	var body map[string]string
	checkJSON(t, w, http.StatusServiceUnavailable, &body)
	// The slot waiter and TimeoutHandler observe the same deadline; either may reply first.
	if body["error"] != "server busy" && body["error"] != "request timed out" {
		t.Errorf("waiting response = %v", body)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("search calls = %d, want 1 while slot is occupied", got)
	}
	releaseOnce.Do(func() { close(release) })
	var result searchResponse
	checkJSON(t, await(t, first), http.StatusOK, &result)
	checkJSON(t, request(h, http.MethodGet, "/search?q=fish"), http.StatusOK, &result)
	if got := calls.Load(); got != 2 {
		t.Errorf("search calls = %d, want 2", got)
	}
}

func TestTimedOutSearchKeepsSlot(t *testing.T) {
	_, ix := savedIndex(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int32
	opts := testOptions()
	opts.timeout = 50 * time.Millisecond
	opts.maxConcurrent = 1
	opts.search = func(string, query.Index, scoring.BM25, []string, int) ([]query.Result, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return nil, nil
	}
	h := testHandler(t, ix, opts)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- request(h, http.MethodGet, "/search?q=fish") }()
	await(t, entered)
	checkError(t, await(t, first), http.StatusServiceUnavailable, "request timed out")
	w := request(h, http.MethodGet, "/search?q=fish")
	var body map[string]string
	checkJSON(t, w, http.StatusServiceUnavailable, &body)
	if body["error"] != "server busy" && body["error"] != "request timed out" {
		t.Errorf("waiting response = %v", body)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("search calls = %d, want timed-out search to keep its slot", got)
	}
	releaseOnce.Do(func() { close(release) })
	var result searchResponse
	checkJSON(t, request(h, http.MethodGet, "/search?q=fish"), http.StatusOK, &result)
}

func TestNewServer(t *testing.T) {
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	srv := newServer("127.0.0.1:0", h, 3*time.Second)
	if srv.Addr != "127.0.0.1:0" || srv.Handler == nil {
		t.Errorf("server address or handler missing: %+v", srv)
	}
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 5 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 10 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 8 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 60 * time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
	if srv.MaxHeaderBytes != 16*1024 {
		t.Errorf("MaxHeaderBytes = %d, want 16384", srv.MaxHeaderBytes)
	}
}

func TestServe(t *testing.T) {
	path, ix := savedIndex(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, path, "127.0.0.1:0", testOptions(), writer, io.Discard) }()
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(reader).ReadString('\n')
		lines <- line
	}()
	var line string
	select {
	case line = <-lines:
	case err := <-done:
		t.Fatalf("serve exited before startup: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server startup")
	}
	const prefix = "quarry-serve listening on "
	if !strings.HasPrefix(line, prefix+"http://127.0.0.1:") {
		t.Fatalf("startup message = %q", line)
	}
	addr := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get(addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var health struct {
		Status    string `json:"status"`
		Documents int    `json:"documents"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || health.Status != "ok" || health.Documents != ix.DocCount() {
		t.Errorf("health response: status = %d, body = %+v", response.StatusCode, health)
	}
	cancel()
	if err := await(t, done); err != nil {
		t.Fatalf("serve returned %v", err)
	}
}

func TestRunFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing index", nil, "--index is required"},
		{"zero timeout", []string{"--index", "unused", "--timeout", "0"}, "--timeout must be greater than 0"},
		{"negative timeout", []string{"--index", "unused", "--timeout", "-1s"}, "--timeout must be greater than 0"},
		{"zero concurrency", []string{"--index", "unused", "--max-concurrent", "0"}, "--max-concurrent must be at least 1"},
		{"negative concurrency", []string{"--index", "unused", "--max-concurrent", "-1"}, "--max-concurrent must be at least 1"},
		{"help", []string{"--help"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tt.want {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for test goroutine")
		var zero T
		return zero
	}
}
