package main

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

func TestRenderSummary(t *testing.T) {
	summary := loadtest.Summary{
		Total:      10,
		Succeeded:  8,
		Failed:     2,
		Elapsed:    2 * time.Second,
		Throughput: 4,
		P50:        10 * time.Millisecond,
		P90:        20 * time.Millisecond,
		P99:        30 * time.Millisecond,
	}

	var output bytes.Buffer
	err := render(&output, summary)
	if err != nil {
		t.Fatalf("render() error = %v, want nil", err)
	}

	want := "" +
		"Load test summary\n" +
		"Total: 10\n" +
		"Succeeded: 8\n" +
		"Failed: 2\n" +
		"Elapsed: 2s\n" +
		"Throughput: 4.00 req/s\n" +
		"P50: <= 10ms\n" +
		"P90: <= 20ms\n" +
		"P99: <= 30ms\n" +
		"Errors:\n" +
		"n/a\n"

	if got := output.String(); got != want {
		t.Errorf("render() output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderSummaryAllFailures(t *testing.T) {
	summary := loadtest.Summary{
		Total:      3,
		Succeeded:  0,
		Failed:     3,
		Elapsed:    time.Second,
		Throughput: 0,
	}

	var output bytes.Buffer
	err := render(&output, summary)
	if err != nil {
		t.Fatalf("render error = %v, want nil", err)
	}

	want := "" +
		"Load test summary\n" +
		"Total: 3\n" +
		"Succeeded: 0\n" +
		"Failed: 3\n" +
		"Elapsed: 1s\n" +
		"Throughput: 0.00 req/s\n" +
		"P50: n/a\n" +
		"P90: n/a\n" +
		"P99: n/a\n" +
		"Errors:\n" +
		"n/a\n"

	if got := output.String(); got != want {
		t.Errorf("render() output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderSummaryErrorsSorted(t *testing.T) {
	summary := loadtest.Summary{
		Total:   8,
		Failed:  8,
		Elapsed: time.Second,
		Errors: map[string]int{
			"timeout":               2,
			"internal server error": 1,
			"connection reset":      2,
			"connection refused":    3,
		},
	}

	var output bytes.Buffer
	err := render(&output, summary)
	if err != nil {
		t.Fatalf("render() error = %v, want nil", err)
	}

	want := "" +
		"Load test summary\n" +
		"Total: 8\n" +
		"Succeeded: 0\n" +
		"Failed: 8\n" +
		"Elapsed: 1s\n" +
		"Throughput: 0.00 req/s\n" +
		"P50: n/a\n" +
		"P90: n/a\n" +
		"P99: n/a\n" +
		"Errors:\n" +
		"  connection refused: 3\n" +
		"  connection reset: 2\n" +
		"  timeout: 2\n" +
		"  internal server error: 1\n"

	if got := output.String(); got != want {
		t.Errorf("render() output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderWriteFailure(t *testing.T) {

	err := render(failingWriter{}, loadtest.Summary{})
	if err == nil {
		t.Fatalf("render() error = nil, want %s", errWrite.Error())
	}
	if !errors.Is(err, errWrite) {
		t.Errorf("render() err = %s, want = %s", err.Error(), errWrite.Error())
	}
}
