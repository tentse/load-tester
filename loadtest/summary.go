package loadtest

import (
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	p50 = 0.5
	p90 = 0.9
	p99 = 0.99
)

type result struct {
	latency time.Duration
	status  int
	err     error
}

// Summary reports the aggregate outcome of a load test.
type Summary struct {
	Total      int
	Succeeded  int
	Failed     int
	Elapsed    time.Duration
	Throughput float64
	P50        time.Duration
	P90        time.Duration
	P99        time.Duration
	Errors     map[string]int
}

func summarize(results []result, elapsed time.Duration) Summary {
	summary := Summary{
		Total:   len(results),
		Elapsed: elapsed,
		Errors:  make(map[string]int),
	}

	var durations []time.Duration
	for _, res := range results {
		if res.err != nil {
			summary.Errors[res.err.Error()]++
			summary.Failed++
		} else if res.status >= 500 {
			summary.Errors[statusErrText(res.status)]++
			summary.Failed++
		} else {
			durations = append(durations, res.latency)
			summary.Succeeded++
		}
	}

	slices.Sort(durations)

	summary.P50 = percentile(durations, p50)
	summary.P90 = percentile(durations, p90)
	summary.P99 = percentile(durations, p99)

	summary.Throughput = throughput(summary.Succeeded, elapsed)

	return summary
}

func throughput(succeeded int, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(succeeded) / elapsed.Seconds()
}

func statusErrText(statusCode int) string {
	return strings.ToLower(http.StatusText(statusCode))
}
