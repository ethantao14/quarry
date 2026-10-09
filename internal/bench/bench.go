// Package bench measures query latency and throughput.
package bench

import (
	"errors"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Percentile returns the nearest-rank percentile of sorted durations.
func Percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[rank-1]
}

// Summary holds throughput and latency statistics in milliseconds.
type Summary struct {
	Queries                          int
	Elapsed                          time.Duration
	QPS, MeanMS, P50MS, P95MS, P99MS float64
}

// Summarize computes statistics without changing latencies.
func Summarize(latencies []time.Duration, elapsed time.Duration) Summary {
	if len(latencies) == 0 {
		return Summary{}
	}
	sorted := slices.Clone(latencies)
	slices.Sort(sorted)
	summary := Summary{Queries: len(sorted), Elapsed: elapsed}
	if elapsed != 0 {
		summary.QPS = float64(summary.Queries) / elapsed.Seconds()
	}
	for _, latency := range sorted {
		summary.MeanMS += float64(latency) / float64(time.Millisecond)
	}
	summary.MeanMS /= float64(summary.Queries)
	summary.P50MS = float64(Percentile(sorted, 50)) / float64(time.Millisecond)
	summary.P95MS = float64(Percentile(sorted, 95)) / float64(time.Millisecond)
	summary.P99MS = float64(Percentile(sorted, 99)) / float64(time.Millisecond)
	return summary
}

// Run times n searches across clients workers and waits for all workers to finish.
// After the first error, unstarted entries in the returned slice remain zero.
func Run(clients int, n int, search func(i int) error) ([]time.Duration, time.Duration, error) {
	if clients < 1 {
		return nil, 0, errors.New("clients must be at least 1")
	}
	if n < 0 {
		return nil, 0, errors.New("query count must be nonnegative")
	}
	latencies := make([]time.Duration, n)
	var next atomic.Int64
	var stopped atomic.Bool
	var firstErr error
	var once sync.Once
	var workers sync.WaitGroup
	start := time.Now()
	for range clients {
		workers.Go(func() {
			for {
				// Each worker claims the next unclaimed query, so every index runs once.
				i := int(next.Add(1) - 1)
				if i >= n || stopped.Load() {
					return
				}
				queryStart := time.Now()
				err := search(i)
				latencies[i] = time.Since(queryStart)
				if err != nil {
					once.Do(func() { firstErr = err })
					stopped.Store(true)
					return
				}
			}
		})
	}
	workers.Wait()
	return latencies, time.Since(start), firstErr
}
