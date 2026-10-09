// Command quarry-serve serves an HTTP search API over a saved index.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ethantao14/quarry/internal/analysis"
	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/query"
	"github.com/ethantao14/quarry/internal/scoring"
)

// searcher is a query index that can also map results back to external document IDs.
type searcher interface {
	query.Index
	ExternalID(uint32) string
}

var (
	_ searcher = (*index.Disk)(nil)
	_ searcher = (*index.Index)(nil)
)

type options struct {
	timeout       time.Duration
	maxConcurrent int
	search        func(string, query.Index, scoring.BM25, []string, int) ([]query.Result, error)
	errorLog      *log.Logger
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "quarry-serve:", err)
		os.Exit(1)
	}
}

// run holds the program logic so tests can call it without starting a process.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("quarry-serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	indexPath := flags.String("index", "", "path to a saved index directory")
	addr := flags.String("addr", "127.0.0.1:8080", "HTTP listen address")
	timeout := flags.Duration("timeout", 5*time.Second, "per-request time limit")
	maxConcurrent := flags.Int("max-concurrent", 2*runtime.NumCPU(), "maximum concurrent searches")
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if *indexPath == "" {
		return errors.New("--index is required")
	}
	if *timeout <= 0 {
		return errors.New("--timeout must be greater than 0")
	}
	if *maxConcurrent < 1 {
		return errors.New("--max-concurrent must be at least 1")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opts := options{timeout: *timeout, maxConcurrent: *maxConcurrent}
	return serve(ctx, *indexPath, *addr, opts, stdout, stderr)
}

func serve(ctx context.Context, indexPath, addr string, opts options, stdout, stderr io.Writer) error {
	ix, err := index.Open(indexPath)
	if err != nil {
		return err
	}
	// Unmapping a read-only mapping cannot lose data.
	defer func() { _ = ix.Close() }()
	opts.errorLog = log.New(stderr, "quarry-serve: ", log.LstdFlags)
	h := newHandler(ix, opts)
	defer h.stop()
	srv := newServer(addr, h, opts.timeout)
	srv.ErrorLog = opts.errorLog
	defer func() { _ = srv.Close() }()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	if _, err := fmt.Fprintf(stdout, "quarry-serve listening on http://%s\n", listener.Addr()); err != nil {
		return err
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	select {
	case err = <-served:
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = srv.Shutdown(shutdownCtx)
		if err != nil {
			_ = srv.Close()
		}
		<-served
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func newServer(addr string, h http.Handler, timeout time.Duration) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      timeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
}

type handler struct {
	ix      searcher
	opts    options
	slots   chan struct{}
	timed   http.Handler
	mu      sync.RWMutex
	stopped bool
}

func newHandler(ix searcher, opts options) *handler {
	if opts.search == nil {
		opts.search = query.Search
	}
	if opts.errorLog == nil {
		opts.errorLog = log.New(os.Stderr, "quarry-serve: ", log.LstdFlags)
	}
	h := &handler{ix: ix, opts: opts, slots: make(chan struct{}, opts.maxConcurrent)}
	// Search cannot be cancelled mid-query. Timed-out searches finish in the
	// background while holding their semaphore slots, bounding total work.
	h.timed = http.TimeoutHandler(http.HandlerFunc(h.handle), opts.timeout, `{"error":"request timed out"}`)
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set this outside TimeoutHandler so its timeout response is also JSON.
	w.Header().Set("Content-Type", "application/json")
	h.timed.ServeHTTP(w, r)
}

// stop waits for background handlers before the index can be unmapped.
func (h *handler) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
}

func (h *handler) handle(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.stopped {
		writeError(w, http.StatusServiceUnavailable, "server busy")
		return
	}
	var handle http.HandlerFunc
	switch r.URL.Path {
	case "/search":
		handle = h.search
	case "/healthz":
		handle = h.health
	default:
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handle(w, r)
}

type searchParams struct {
	query string
	algo  string
	k     int
}

func parseParams(raw string) (searchParams, error) {
	p := searchParams{algo: "bmw", k: 10}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return p, errors.New("invalid query parameters")
	}
	for _, name := range []string{"q", "k", "algo"} {
		if len(values[name]) > 1 {
			return p, fmt.Errorf("%s must be given once", name)
		}
	}
	if !values.Has("q") {
		return p, errors.New("q is required")
	}
	p.query = values.Get("q")
	if strings.TrimSpace(p.query) == "" {
		return p, errors.New("q must not be blank")
	}
	if len(p.query) > 1000 {
		return p, errors.New("q must be at most 1000 bytes")
	}
	if values.Has("k") {
		p.k, err = strconv.Atoi(values.Get("k"))
		if err != nil || p.k < 1 || p.k > 1000 {
			return p, errors.New("k must be an integer from 1 to 1000")
		}
	}
	if values.Has("algo") {
		p.algo = values.Get("algo")
	}
	if err := query.CheckAlgorithm(p.algo); err != nil {
		return p, errors.New(strings.TrimPrefix(err.Error(), "--"))
	}
	return p, nil
}

type searchResult struct {
	Rank  int     `json:"rank"`
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

type searchResponse struct {
	Query   string         `json:"query"`
	Algo    string         `json:"algo"`
	K       int            `json:"k"`
	TookMS  float64        `json:"took_ms"`
	Results []searchResult `json:"results"`
}

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	p, err := parseParams(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "server busy")
		return
	}
	if r.Context().Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "server busy")
		return
	}
	start := time.Now()
	terms := analysis.Analyze(p.query)
	results, err := h.opts.search(p.algo, h.ix, scoring.DefaultBM25(), terms, p.k)
	took := float64(time.Since(start)) / float64(time.Millisecond)
	if err != nil {
		h.opts.errorLog.Printf("search failed: %v", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	response := searchResponse{
		Query: p.query, Algo: p.algo, K: p.k, TookMS: took,
		Results: make([]searchResult, len(results)),
	}
	for i, result := range results {
		response.Results[i] = searchResult{Rank: i + 1, ID: h.ix.ExternalID(result.DocID), Score: result.Score}
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Status    string `json:"status"`
		Documents int    `json:"documents"`
	}{Status: "ok", Documents: h.ix.DocCount()})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
