package loadtest

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	p50 = 0.5
	p90 = 0.9
	p99 = 0.99
)

// Summary reports the completed portion of a load test.
//
// A request succeeds when it completes without an error and its HTTP status is
// less than 500. Request errors and statuses of 500 or greater are failures.
//
// Total counts completed request attempts, so it can be less than Config.Requests
// after cancellation. Elapsed is the wall-clock run duration. Throughput is
// successful requests per second. P50, P90, and P99 are latency percentiles over
// successful requests only. Latencies are counted into a fixed bucket ladder
// rather than retained, so each percentile is the upper bound of the bucket it
// falls into and can overstate the true latency.
//
// Errors maps stable failure categories and HTTP server-error descriptions to
// their occurrence counts. Request failures are classified as request timeout,
// connection refused, connection reset, unexpected EOF, or request failed.
// Raw transport error text, URL user information, and URL query values are not
// included in these error keys.
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

func summarize(lh *latencyHistogram, elapsed time.Duration, total, succeeded, failed int, errors map[string]int) Summary {

	summary := Summary{
		Total:     total,
		Succeeded: succeeded,
		Failed:    failed,
		Errors:    errors,
	}

	summary.P50 = percentile(lh, p50)
	summary.P90 = percentile(lh, p90)
	summary.P99 = percentile(lh, p99)

	summary.Throughput = throughput(summary.Succeeded, elapsed)
	summary.Elapsed = elapsed

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
