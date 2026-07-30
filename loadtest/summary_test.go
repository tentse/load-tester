package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"syscall"
	"testing"
	"time"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "network operation timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

func TestSummary(t *testing.T) {

	tests := []struct {
		name    string
		results []result
		elapsed time.Duration
		want    Summary
	}{
		{
			name: "all succeeded with one internal server error",
			results: []result{
				{
					latency: 10 * time.Millisecond,
					status:  http.StatusOK,
				},
				{
					latency: 12 * time.Millisecond,
					status:  http.StatusOK,
				},
				{
					latency: 109 * time.Millisecond,
					status:  http.StatusCreated,
				},
				{
					latency: 7 * time.Millisecond,
					status:  http.StatusForbidden,
				},
				{
					latency: 49 * time.Millisecond,
					status:  http.StatusProcessing,
				},
				{
					latency: 21 * time.Millisecond,
					status:  http.StatusAccepted,
				},
				{
					latency: 30 * time.Millisecond,
					status:  http.StatusOK,
				},
				{
					latency: 89 * time.Millisecond,
					status:  http.StatusOK,
				},
				{
					latency: 120 * time.Millisecond,
					status:  http.StatusCreated,
				},
				{
					latency: 74 * time.Millisecond,
					status:  http.StatusInternalServerError,
				},
				{
					latency: 15 * time.Millisecond,
					status:  http.StatusProcessing,
				},
				{
					latency: 28 * time.Millisecond,
					status:  http.StatusAccepted,
				},
			},
			elapsed: 4 * time.Second,
			want: Summary{
				Total:      12,
				Succeeded:  11,
				Failed:     1,
				Elapsed:    4 * time.Second,
				Throughput: 2.75,
				P50:        28 * time.Millisecond,
				P90:        109 * time.Millisecond,
				P99:        120 * time.Millisecond,
				Errors: map[string]int{
					statusErrText(http.StatusInternalServerError): 1,
				},
			},
		},
		{
			name:    "empty input",
			results: []result{},
			want:    Summary{Errors: map[string]int{}},
		},
		{
			name: "0 elapsed",
			results: []result{
				{
					latency: 10 * time.Millisecond,
					status:  http.StatusOK,
				},
			},
			elapsed: 0 * time.Second,
			want: Summary{
				Total:      1,
				Succeeded:  1,
				Failed:     0,
				Elapsed:    0 * time.Second,
				Throughput: 0,
				P50:        10 * time.Millisecond,
				P90:        10 * time.Millisecond,
				P99:        10 * time.Millisecond,
				Errors:     map[string]int{},
			},
		},
		{
			name: "all failures",
			results: []result{
				{
					latency: 74 * time.Millisecond,
					status:  http.StatusInternalServerError,
				},
				{
					latency: 44 * time.Millisecond,
					status:  http.StatusBadGateway,
				},
				{
					err: context.DeadlineExceeded,
				},
				{
					err: syscall.ECONNREFUSED,
				},
				{
					err: errors.New("some new error"),
				},
			},
			elapsed: 2 * time.Second,
			want: Summary{
				Total:      5,
				Succeeded:  0,
				Failed:     5,
				Elapsed:    2 * time.Second,
				Throughput: 0,
				P50:        0,
				P90:        0,
				P99:        0,
				Errors: map[string]int{
					"connection refused":                          1,
					"request timeout":                             1,
					statusErrText(http.StatusInternalServerError): 1,
					statusErrText(http.StatusBadGateway):          1,
					"request failed":                              1,
				},
			},
		},
		{
			name: "all success, no failure",
			results: []result{
				{
					latency: 10 * time.Millisecond,
					status:  http.StatusOK,
				},
				{
					latency: 12 * time.Millisecond,
					status:  http.StatusOK,
				},
				{
					latency: 19 * time.Millisecond,
					status:  http.StatusCreated,
				},
			},
			elapsed: 1 * time.Second,
			want: Summary{
				Total:      3,
				Succeeded:  3,
				Failed:     0,
				Elapsed:    1 * time.Second,
				Throughput: 3,
				P50:        12 * time.Millisecond,
				P90:        19 * time.Millisecond,
				P99:        19 * time.Millisecond,
				Errors:     map[string]int{},
			},
		},
		{
			name: "single result",
			results: []result{
				{
					latency: 10 * time.Millisecond,
					status:  http.StatusOK,
				},
			},
			elapsed: 1 * time.Second,
			want: Summary{
				Total:      1,
				Succeeded:  1,
				Failed:     0,
				Elapsed:    1 * time.Second,
				Throughput: 1,
				P50:        10 * time.Millisecond,
				P90:        10 * time.Millisecond,
				P99:        10 * time.Millisecond,
				Errors:     map[string]int{},
			},
		},
		{
			name: "unknown status code: 789 response",
			results: []result{
				{
					latency: 10 * time.Millisecond,
					status:  789,
				},
			},
			elapsed: 1 * time.Second,
			want: Summary{
				Total:      1,
				Succeeded:  0,
				Failed:     1,
				Elapsed:    1 * time.Second,
				Throughput: 0,
				P50:        0,
				P90:        0,
				P99:        0,
				Errors: map[string]int{
					"HTTP 789": 1,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summarize(tc.results, tc.elapsed)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("summarize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// [should-fix] The issue requires deterministic synthetic chains, but this table only covers
// direct errors and one fmt wrapper. Add *url.Error -> *net.OpError -> *os.SyscallError chains
// for refusal/reset, a net.Error timeout, and a wrapped io.ErrUnexpectedEOF. Separately exercise
// summarize with two reset chains whose local ports differ and assert one "connection reset" key
// with count 2; that proves the low-cardinality behavior at the actual aggregation boundary.
func TestSummaryStandardErrorCategory(t *testing.T) {

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: "request timeout",
		},
		{
			name: "wrapped deadline exceeded",
			err:  fmt.Errorf("send request: %w", context.DeadlineExceeded),
			want: "request timeout",
		},
		{
			name: "connection refused",
			err:  syscall.ECONNREFUSED,
			want: "connection refused",
		},
		{
			name: "wrapped connection refused",
			err:  fmt.Errorf("dial target: %w", syscall.ECONNREFUSED),
			want: "connection refused",
		},
		{
			name: "connection reset",
			err:  syscall.ECONNRESET,
			want: "connection reset",
		},
		{
			name: "unexpected EOF",
			err:  io.ErrUnexpectedEOF,
			want: "unexpected EOF",
		},
		{
			name: "plain EOF is not unexpected EOF",
			err:  io.EOF,
			want: "request failed",
		},
		{
			name: "message text must not control classification",
			err:  errors.New("connection refused"),
			want: "request failed",
		},
		{
			name: "unknown sensitive error",
			err: errors.New(
				"request https://user:password@example.com/users?token=secret failed",
			),
			want: "request failed",
		},
		{
			name: "network timeout",
			err:  timeoutError{},
			want: "request timeout",
		},
		{
			name: "wrapped network timeout",
			err:  fmt.Errorf("send request: %w", timeoutError{}),
			want: "request timeout",
		},
		{
			name: "network timeout inside URL error",
			err: &url.Error{
				Op:  "Get",
				URL: "https://user:password@example.com/users?token=secret",
				Err: timeoutError{},
			},
			want: "request timeout",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFailure(tc.err); got != tc.want {
				t.Fatalf("classifyFailure() = %q, want = %q", got, tc.want)
			}
		})
	}
}
