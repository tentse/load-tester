package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

func TestRenderSummaries(t *testing.T) {
	summaries := map[string]loadtest.Summary{
		"alpha": {
			Total:      10,
			Succeeded:  8,
			Failed:     2,
			Elapsed:    2 * time.Second,
			Throughput: 4,
			P50:        10 * time.Millisecond,
			P90:        20 * time.Millisecond,
			P99:        30 * time.Millisecond,
		},
		"beta": {
			Total:      5,
			Succeeded:  5,
			Elapsed:    time.Second,
			Throughput: 5,
			P50:        1 * time.Millisecond,
			P90:        2 * time.Millisecond,
			P99:        3 * time.Millisecond,
		},
	}

	var output bytes.Buffer
	err := renderSummaries(&output, summaries)
	if err != nil {
		t.Fatalf("renderSummaries() error = %v, want nil", err)
	}

	want := "" +
		"Name: alpha\n" +
		"Total: 10\n" +
		"Succeeded: 8\n" +
		"Failed: 2\n" +
		"Elapsed: 2s\n" +
		"Throughput: 4.00 req/s\n" +
		"P50: <= 10ms\n" +
		"P90: <= 20ms\n" +
		"P99: <= 30ms\n" +
		"Errors:\n" +
		"n/a\n" +
		"\n" +
		"Name: beta\n" +
		"Total: 5\n" +
		"Succeeded: 5\n" +
		"Failed: 0\n" +
		"Elapsed: 1s\n" +
		"Throughput: 5.00 req/s\n" +
		"P50: <= 1ms\n" +
		"P90: <= 2ms\n" +
		"P99: <= 3ms\n" +
		"Errors:\n" +
		"n/a\n"

	if got := output.String(); got != want {
		t.Errorf("renderSummaries() output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderSummariesAllFailures(t *testing.T) {
	summaries := map[string]loadtest.Summary{
		"alpha": {
			Total:   3,
			Failed:  3,
			Elapsed: time.Second,
		},
	}

	var output bytes.Buffer
	err := renderSummaries(&output, summaries)
	if err != nil {
		t.Fatalf("renderSummaries() error = %v, want nil", err)
	}

	want := "" +
		"Name: alpha\n" +
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
		t.Errorf("renderSummaries() output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderSummariesErrorsSorted(t *testing.T) {
	summaries := map[string]loadtest.Summary{
		"alpha": {
			Total:   8,
			Failed:  8,
			Elapsed: time.Second,
			Errors: map[string]int{
				"timeout":               2,
				"internal server error": 1,
				"connection reset":      2,
				"connection refused":    3,
			},
		},
	}

	var output bytes.Buffer
	err := renderSummaries(&output, summaries)
	if err != nil {
		t.Fatalf("renderSummaries() error = %v, want nil", err)
	}

	want := "" +
		"Name: alpha\n" +
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
		t.Errorf("renderSummaries() output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderSummariesWriteFailure(t *testing.T) {
	err := renderSummaries(failingWriter{}, map[string]loadtest.Summary{
		"alpha": {Total: 1, Succeeded: 1, Elapsed: time.Second},
	})

	if err == nil {
		t.Fatalf("renderSummaries() error = nil, want %s", errWrite.Error())
	}
	if !errors.Is(err, errWrite) {
		t.Errorf("renderSummaries() err = %s, want = %s", err.Error(), errWrite.Error())
	}
}

func TestRenderSummariesNameOrder(t *testing.T) {
	summaries := map[string]loadtest.Summary{
		"zulu":    {Total: 1, Succeeded: 1, Elapsed: time.Second},
		"echo":    {Total: 1, Succeeded: 1, Elapsed: time.Second},
		"mike":    {Total: 1, Succeeded: 1, Elapsed: time.Second},
		"alpha":   {Total: 1, Succeeded: 1, Elapsed: time.Second},
		"charlie": {Total: 1, Succeeded: 1, Elapsed: time.Second},
	}

	want := []string{"alpha", "charlie", "echo", "mike", "zulu"}

	for range 20 {
		var output bytes.Buffer
		if err := renderSummaries(&output, summaries); err != nil {
			t.Fatalf("renderSummaries() error = %v, want nil", err)
		}

		var got []string
		for _, line := range strings.Split(output.String(), "\n") {
			if name, ok := strings.CutPrefix(line, "Name: "); ok {
				got = append(got, name)
			}
		}

		if !slicesEqual(got, want) {
			t.Fatalf("endpoint order = %v, want %v", got, want)
		}
	}
}

func TestRenderSummariesEmpty(t *testing.T) {
	var output bytes.Buffer
	err := renderSummaries(&output, map[string]loadtest.Summary{})
	if err != nil {
		t.Fatalf("renderSummaries() error = %v, want nil", err)
	}
	if got := output.String(); got != "" {
		t.Errorf("renderSummaries() output = %q, want empty", got)
	}
}

func slicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
