package loadtest

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestSummary(t *testing.T) {

	// [should-fix] The public status policy is buried inside large fixtures. Add focused,
	// hand-checkable cases at the exact boundary: 499 succeeds and 500 fails. This gives a
	// classification regression one obvious failure instead of making the reader reverse-
	// engineer percentile and count changes in the mixed 15-result case.
	tests := []struct {
		name    string
		results []result
		elapsed time.Duration
		want    Summary
	}{
		{
			name: "both succeeded and failed hit",
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
				{
					err: errors.New("connection refused"),
				},
				{
					err: errors.New("connection refused"),
				},
				{
					err: errors.New("timeout"),
				},
			},
			elapsed: 4 * time.Second,
			want: Summary{
				Total:      15,
				Succeeded:  11,
				Failed:     4,
				Elapsed:    4 * time.Second,
				Throughput: 2.75,
				P50:        28 * time.Millisecond,
				P90:        109 * time.Millisecond,
				P99:        120 * time.Millisecond,
				Errors: map[string]int{
					"connection refused": 2,
					"timeout":            1,
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
					err: errors.New("connection refused"),
				},
				{
					err: errors.New("timeout"),
				},
			},
			elapsed: 2 * time.Second,
			want: Summary{
				Total:      4,
				Succeeded:  0,
				Failed:     4,
				Elapsed:    2 * time.Second,
				Throughput: 0,
				P50:        0,
				P90:        0,
				P99:        0,
				Errors: map[string]int{
					"connection refused": 1,
					"timeout":            1,
					statusErrText(http.StatusInternalServerError): 1,
					statusErrText(http.StatusBadGateway):          1,
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
