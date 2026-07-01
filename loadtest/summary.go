package loadtest

import (
	"fmt"
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

// [should-fix] The comment now captures the 5xx policy, but it still omits that throughput
// and percentiles use successful requests only, and it contains grammar/spelling errors.
// State all non-obvious exported semantics in complete sentences so callers can interpret
// the numbers without reading summarize.

// Summary reports the aggregate outcome of a load test.
// Except for 5xx error from the server, all other response status are considered successfull.
// Only 5xx and connection error are considered as Failed.
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
		} else if isServerError(res.status) {
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

	text := http.StatusText(statusCode)
	if text == "" {
		return fmt.Sprintf("HTTP %d", statusCode)
	}
	return strings.ToLower(text)
}

func isServerError(status int) bool {
	return status >= 500
}
