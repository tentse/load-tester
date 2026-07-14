package main

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

var errWrite = errors.New("deliberate write failure")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWrite
}

type parseConfigCase struct {
	name            string
	args            []string
	wantErrContains string
}

func TestMissingURL(t *testing.T) {
	var stderr bytes.Buffer
	wantErrContains := "-url is required"
	_, err := parseConfig([]string{"-c", "10", "-n", "100"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), wantErrContains) {
		t.Fatalf("parseConfig() err = %v, want to contain %q", err, wantErrContains)
	}
}

func TestInvalidValue(t *testing.T) {
	tests := []parseConfigCase{
		{
			name: "invalid concurrency value",
			args: []string{
				"-url", "http://www.example.com",
				"-c", "invalidConcurrency",
			},
			wantErrContains: "flag -c",
		},
		{
			name: "invalid requests value",
			args: []string{
				"-url", "http://www.example.com",
				"-n", "invalidRequests",
			},
			wantErrContains: "flag -n",
		},
		{
			name: "invalid timeout value",
			args: []string{
				"-url", "http://www.example.com",
				"-timeout", "invalidTimeout",
			},
			wantErrContains: "flag -timeout",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := parseConfig(tc.args, &stderr)
			if err == nil {
				t.Fatalf("parseConfig err = nil, want = %q", tc.wantErrContains)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("parseConfig() err = %q, want = %q", err.Error(), tc.wantErrContains)
			}
		})
	}
}

func TestMissingArgument(t *testing.T) {
	var stderr bytes.Buffer
	wantErrContains := "flag needs an argument: -c"
	_, err := parseConfig([]string{"-url", "http://example.com", "-c"}, &stderr)
	if err == nil {
		t.Fatalf("parseConfig err = nil, want = %q", wantErrContains)
	}
	if !strings.Contains(err.Error(), wantErrContains) {
		t.Errorf("parseConfig() err = %q, want = %q", err.Error(), wantErrContains)
	}

}

func TestUnknownFlag(t *testing.T) {
	var stderr bytes.Buffer
	wantErrContains := "not defined: -unknownFlag"
	_, err := parseConfig([]string{"-unknownFlag", "value"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), wantErrContains) {
		t.Fatalf("parseConfig() err = %v, want to contain %q", err, wantErrContains)
	}
}

func TestParseConfigDefaultValues(t *testing.T) {

	var stderr bytes.Buffer

	args := []string{
		"-url", "http://example.com",
	}
	want := loadtest.Config{
		URL:         "http://example.com",
		Concurrency: 10,
		Requests:    20,
		Timeout:     1 * time.Second,
		Method:      http.MethodGet,
		Token:       "",
		Body:        "",
	}

	got, err := parseConfig(args, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("parseConfig() wrote unexpected output to stderr: %q", stderr.String())
	}
	if err != nil {
		t.Fatalf("parseConfig() unexpected error = %v", err)
	}
	if want != got {
		t.Errorf("parseConfig() got = %v, want = %v", got, want)
	}
}

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
		"P50: 10ms\n" +
		"P90: 20ms\n" +
		"P99: 30ms\n" +
		"Errors:\n" +
		"n/a\n"

	if got := output.String(); got != want {
		t.Errorf("render() output:\n%q\nwant:\n%q", got, want)
	}
}

func TestParseConfigAllValues(t *testing.T) {
	var stderr bytes.Buffer
	args := []string{
		"-url", "http://example.com",
		"-c", "12",
		"-n", "250",
		"-method", "POST",
		"-token", "token",
		"-body", `{"body": "some body"}`,
		"-timeout", "500ms",
	}
	want := loadtest.Config{
		URL:         "http://example.com",
		Concurrency: 12,
		Requests:    250,
		Method:      http.MethodPost,
		Token:       "token",
		Body:        `{"body": "some body"}`,
		Timeout:     500 * time.Millisecond,
	}
	got, err := parseConfig(args, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("parseConfig() wrote unexpected output to stderr: %q", stderr.String())
	}
	if err != nil {
		t.Fatalf("parseConfig err = %q, want = nil", err)
	}
	if want != got {
		t.Errorf("parseConfig() got = %v, want = %v", got, want)
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
