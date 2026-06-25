package loadtest

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestSummary(t *testing.T) {
	tests := []result{
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
			status:  http.StatusForbidden,
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
	}
	want := Summary{
		Total:      15,
		Succeeded:  12,
		Failed:     3,
		Elapsed:    4 * time.Second,
		Throughput: 3,
		P50:        28 * time.Millisecond,
		P90:        109 * time.Millisecond,
		P99:        120 * time.Millisecond,
		Errors: map[string]int{
			"connection refused": 2,
			"timeout":            1,
		},
	}

	got := summarize(tests, 4*time.Second)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summarize() = %+v, want %+v", got, want)
	}
}
