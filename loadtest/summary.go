package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"syscall"
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
// [should-fix] This public contract still describes arbitrary "failure descriptions", while
// the implementation now exposes a fixed set of safe categories. Document those categories
// here and in loadtest/doc.go and README.md; issue #2 also requires removing the two obsolete
// known-limitations entries while retaining the separate command-line-secret warning.
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

// [blocker] Timeout classification is incomplete: many real HTTP timeouts are reported through
// net.Error.Timeout() without wrapping context.DeadlineExceeded, so they currently fall through
// to "request failed". Use errors.As to find a net.Error anywhere in the chain and classify it
// when Timeout() is true; add both direct and *url.Error-wrapped timeout cases to the table.
//
// [should-fix] These strings are part of observable library/CLI output, but literals are repeated
// across production and tests. An unexported failureKind type with named constants makes the
// vocabulary explicit and prevents a typo in one branch from silently creating a new map key.
func classifyFailure(err error) string {
	var networkErr net.Error

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "request timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected EOF"
	case errors.As(err, &networkErr) && networkErr.Timeout():
		return "request timeout"
	default:
		return "request failed"
	}
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
			summary.Errors[classifyFailure(res.err)]++
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
