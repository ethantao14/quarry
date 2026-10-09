package bench

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	for _, tt := range []struct {
		name string
		n    int
		want [3]time.Duration
	}{
		{"empty", 0, [3]time.Duration{0, 0, 0}},
		{"one", 1, [3]time.Duration{1, 1, 1}},
		{"two", 2, [3]time.Duration{1, 2, 2}},
		{"hundred", 100, [3]time.Duration{50, 95, 99}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			durations := make([]time.Duration, tt.n)
			for i := range durations {
				durations[i] = time.Duration(i + 1)
			}
			for i, p := range []float64{50, 95, 99} {
				if got := Percentile(durations, p); got != tt.want[i] {
					t.Errorf("Percentile(n=%d, p=%g) = %v, want %v", tt.n, p, got, tt.want[i])
				}
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	for _, tt := range []struct {
		name      string
		latencies []time.Duration
		elapsed   time.Duration
		want      Summary
	}{
		{name: "empty", elapsed: time.Second},
		{name: "one", latencies: []time.Duration{1500 * time.Microsecond}, elapsed: time.Second,
			want: Summary{Queries: 1, Elapsed: time.Second, QPS: 1, MeanMS: 1.5, P50MS: 1.5, P95MS: 1.5, P99MS: 1.5}},
		{name: "known values", latencies: []time.Duration{4 * time.Millisecond, time.Millisecond, 3 * time.Millisecond, 2 * time.Millisecond}, elapsed: 2 * time.Second,
			want: Summary{Queries: 4, Elapsed: 2 * time.Second, QPS: 2, MeanMS: 2.5, P50MS: 2, P95MS: 4, P99MS: 4}},
		{name: "zero elapsed", latencies: []time.Duration{time.Millisecond},
			want: Summary{Queries: 1, MeanMS: 1, P50MS: 1, P95MS: 1, P99MS: 1}},
		{name: "fractional QPS", latencies: []time.Duration{0}, elapsed: 2 * time.Second,
			want: Summary{Queries: 1, Elapsed: 2 * time.Second, QPS: 0.5}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := slices.Clone(tt.latencies)
			if got := Summarize(tt.latencies, tt.elapsed); got != tt.want {
				t.Errorf("Summarize() = %+v, want %+v", got, tt.want)
			}
			if !slices.Equal(tt.latencies, before) {
				t.Errorf("Summarize changed latencies: %v, want %v", tt.latencies, before)
			}
		})
	}
}

func TestRun(t *testing.T) {
	for _, clients := range []int{1, 3, 8} {
		for _, n := range []int{0, 1, 100} {
			t.Run(fmt.Sprintf("clients=%d/n=%d", clients, n), func(t *testing.T) {
				calls := make([]atomic.Int64, n)
				latencies, elapsed, err := Run(clients, n, func(i int) error {
					if i < 0 || i >= n {
						return fmt.Errorf("index %d out of range", i)
					}
					calls[i].Add(1)
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(latencies) != n || elapsed <= 0 {
					t.Fatalf("Run() returned %d latencies and elapsed %v", len(latencies), elapsed)
				}
				for i := range calls {
					if got := calls[i].Load(); got != 1 {
						t.Errorf("calls[%d] = %d, want 1", i, got)
					}
					if latencies[i] < 0 || latencies[i] > elapsed {
						t.Errorf("latency[%d] = %v, elapsed = %v", i, latencies[i], elapsed)
					}
				}
			})
		}
	}
}

func TestRunError(t *testing.T) {
	wantErr := errors.New("search failed")
	for _, clients := range []int{1, 3, 8} {
		t.Run(strconv.Itoa(clients), func(t *testing.T) {
			var calls atomic.Int64
			_, _, err := Run(clients, 100, func(i int) error {
				calls.Add(1)
				return wantErr
			})
			if !errors.Is(err, wantErr) {
				t.Fatalf("Run() error = %v, want %v", err, wantErr)
			}
			if got := calls.Load(); got < 1 || got > int64(clients) {
				t.Errorf("calls = %d, want between 1 and %d", got, clients)
			}
		})
	}
}

func TestRunStopsOtherWorkers(t *testing.T) {
	wantErr := errors.New("search failed")
	var calls atomic.Int64
	_, _, err := Run(3, 1000, func(i int) error {
		calls.Add(1)
		if i == 0 {
			return wantErr
		}
		time.Sleep(100 * time.Microsecond)
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	// Workers stop claiming queries after the error instead of finishing all 1000.
	if got := calls.Load(); got > 100 {
		t.Errorf("calls = %d after an early error, want far fewer than 1000", got)
	}
}

func TestRunWaitsForWorkers(t *testing.T) {
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	finished := make(chan error, 1)
	wantErr := errors.New("search failed")
	var completed atomic.Int64
	go func() {
		_, _, err := Run(3, 100, func(i int) error {
			started <- struct{}{}
			<-release
			completed.Add(1)
			return wantErr
		})
		finished <- err
	}()
	for range 3 {
		<-started
	}
	close(release)
	if err := <-finished; !errors.Is(err, wantErr) {
		t.Errorf("Run() error = %v, want %v", err, wantErr)
	}
	if got := completed.Load(); got != 3 {
		t.Errorf("completed = %d, want 3", got)
	}
}

func TestRunInvalid(t *testing.T) {
	for _, tt := range []struct{ clients, n int }{{0, 1}, {-1, 1}, {1, -1}} {
		_, _, err := Run(tt.clients, tt.n, func(i int) error {
			t.Error("search called with invalid arguments")
			return nil
		})
		if err == nil {
			t.Errorf("Run(%d, %d) succeeded, want error", tt.clients, tt.n)
		}
	}
}

func TestHardware(t *testing.T) {
	values := make(map[string]string)
	for _, line := range Hardware() {
		key, value, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("invalid hardware line %q", line)
		}
		values[key] = value
	}
	for _, key := range []string{"cpu", "cores", "memory_gib", "os", "go"} {
		if _, ok := values[key]; !ok {
			t.Errorf("missing hardware key %q", key)
		}
	}
	if cores, err := strconv.Atoi(values["cores"]); err != nil || cores < 1 {
		t.Errorf("cores = %q, want positive integer", values["cores"])
	}
}
