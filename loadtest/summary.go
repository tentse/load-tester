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

// Summary reports the completed portion of a load test.
//
// A request succeeds when it completes without an error and its HTTP status is
// less than 500. Request errors and statuses of 500 or greater are failures.
// Total counts completed request attempts, so it can be less than Config.Requests
// after cancellation. Elapsed is the wall-clock run duration. Throughput is
// successful requests per second, and P50, P90, and P99 are nearest-rank
// successful-request latencies. Errors groups failure descriptions by occurrence.
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
		// [nit] Three mutually exclusive branches classifying one value read better as a
		// tagless `switch { case ...: }` — gocritic's ifElseChain flags this exact shape, and
		// a switch also makes it obvious where a fourth outcome would slot in.
		if res.err != nil {
			// [should-fix] (FEEDBACK.md #3, still open) Keying on the full error string means
			// this map doesn't group, it fragments. Go network errors embed the client's local
			// ephemeral port, so "the same failure" renders differently on every occurrence:
			// STRESS_TEST_REPORT.md §6 measured 5000 connection resets producing 5000 distinct
			// keys. That's a second unbounded memory sink on top of run.go's slice, and it
			// destroys the feature — nobody learns anything from 5000 near-identical rows. The
			// idiom is to classify *before* you count: errors.As into net.Error (check
			// Timeout()) and *net.OpError (its .Op is "dial"/"read"/"write"), errors.Is against
			// context.DeadlineExceeded and syscall.ECONNREFUSED, then map each to a small,
			// stable label. The rendered message is for humans; the error's *kind* is for
			// counting. Bounded map, readable output.
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
