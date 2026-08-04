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
		name           string
		latencies      []time.Duration
		partialSummary Summary
		total          int
		succeeded      int
		failed         int
		errors         map[string]int
		elapsed        time.Duration
		want           Summary
	}{
		{
			name: "all succeeded with one internal server error",
			latencies: []time.Duration{
				10 * time.Millisecond,
				12 * time.Millisecond,
				109 * time.Millisecond,
				7 * time.Millisecond,
				49 * time.Millisecond,
				21 * time.Millisecond,
				30 * time.Millisecond,
				89 * time.Millisecond,
				120 * time.Millisecond,
				15 * time.Millisecond,
				28 * time.Millisecond,
			},
			total:     12,
			succeeded: 11,
			failed:    1,
			errors: map[string]int{
				statusErrText(http.StatusInternalServerError): 1,
			},
			elapsed: 4 * time.Second,
			want: Summary{
				Total:      12,
				Succeeded:  11,
				Failed:     1,
				Elapsed:    4 * time.Second,
				Throughput: 2.75,
				P50:        50 * time.Millisecond,
				P90:        200 * time.Millisecond,
				P99:        200 * time.Millisecond,
				Errors: map[string]int{
					statusErrText(http.StatusInternalServerError): 1,
				},
			},
		},
		{
			name:      "empty input",
			latencies: []time.Duration{},
			errors:    map[string]int{},
			want:      Summary{Errors: map[string]int{}},
		},
		{
			name: "0 elapsed",
			latencies: []time.Duration{
				10 * time.Millisecond,
			},
			total:     1,
			succeeded: 1,
			failed:    0,
			errors:    map[string]int{},
			elapsed:   0 * time.Second,
			want: Summary{
				Total:      1,
				Succeeded:  1,
				Failed:     0,
				Elapsed:    0 * time.Second,
				Throughput: 0,
				P50:        20 * time.Millisecond,
				P90:        20 * time.Millisecond,
				P99:        20 * time.Millisecond,
				Errors:     map[string]int{},
			},
		},
		{
			name:      "all failures",
			latencies: []time.Duration{},
			elapsed:   2 * time.Second,
			total:     5,
			succeeded: 0,
			failed:    5,
			errors: map[string]int{
				"connection refused":                          1,
				"request timeout":                             1,
				statusErrText(http.StatusInternalServerError): 1,
				statusErrText(http.StatusBadGateway):          1,
				"request failed":                              1,
			},
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
			latencies: []time.Duration{
				10 * time.Millisecond,
				12 * time.Millisecond,
				19 * time.Millisecond,
			},
			elapsed:   1 * time.Second,
			total:     3,
			succeeded: 3,
			failed:    0,
			errors:    map[string]int{},
			want: Summary{
				Total:      3,
				Succeeded:  3,
				Failed:     0,
				Elapsed:    1 * time.Second,
				Throughput: 3,
				P50:        20 * time.Millisecond,
				P90:        20 * time.Millisecond,
				P99:        20 * time.Millisecond,
				Errors:     map[string]int{},
			},
		},
		{
			name: "single result",
			latencies: []time.Duration{
				10 * time.Millisecond,
			},
			total:     1,
			succeeded: 1,
			failed:    0,
			errors:    map[string]int{},
			elapsed:   1 * time.Second,
			want: Summary{
				Total:      1,
				Succeeded:  1,
				Failed:     0,
				Elapsed:    1 * time.Second,
				Throughput: 1,
				P50:        20 * time.Millisecond,
				P90:        20 * time.Millisecond,
				P99:        20 * time.Millisecond,
				Errors:     map[string]int{},
			},
		},
		{
			name:      "unknown status code: 789 response",
			latencies: []time.Duration{},
			total:     1,
			succeeded: 0,
			failed:    1,
			errors: map[string]int{
				"HTTP 789": 1,
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

			lh := latencyHistogram{}

			for _, value := range tc.latencies {
				lh.observe(value)
			}

			got := summarize(lh, tc.elapsed, tc.total, tc.succeeded, tc.failed, tc.errors)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("summarize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

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
