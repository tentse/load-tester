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
	want            loadtest.Config
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
	tests := []parseConfigCase{
		{
			name: "missing URL argument",
			args: []string{
				"-url",
			},
			wantErrContains: "flag needs an argument: -url",
		},
		{
			name: "missing concurrency argument",
			args: []string{
				"-url", "http://example.com",
				"-c",
			},
			wantErrContains: "flag needs an argument: -c",
		},
		{
			name: "missing requests argument",
			args: []string{
				"-url", "http://example.com",
				"-n",
			},
			wantErrContains: "flag needs an argument: -n",
		},
		{
			name: "missing method argument",
			args: []string{
				"-url", "http://example.com",
				"-method",
			},
			wantErrContains: "flag needs an argument: -method",
		},
		{
			name: "missing token argument",
			args: []string{
				"-url", "http://example.com",
				"-token",
			},
			wantErrContains: "flag needs an argument: -token",
		},
		{
			name: "missing body argument",
			args: []string{
				"-url", "http://example.com",
				"-body",
			},
			wantErrContains: "flag needs an argument: -body",
		},
		{
			name: "missing timeout argument",
			args: []string{
				"-url", "http://example.com",
				"-timeout",
			},
			wantErrContains: "flag needs an argument: -timeout",
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

func TestUnknownFlag(t *testing.T) {
	var stderr bytes.Buffer
	wantErrContains := "not defined: -unknownFlag"
	_, err := parseConfig([]string{"-unknownFlag", "value"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), wantErrContains) {
		t.Fatalf("parseConfig() err = %v, want to contain %q", err, wantErrContains)
	}
}

// [should-fix] Despite its name, this test still contains no flag missing its value; most
// cases merely omit an optional flag and duplicate the defaults contract. Keep missing URL
// as its own required-flag case, and exercise a real malformed pair such as
// `[]string{"-url", validURL, "-c"}` expecting "flag needs an argument: -c".
func TestParseConfigDefaultValues(t *testing.T) {

	test := parseConfigCase{
		name: "default values",
		args: []string{
			"-url", "http://example.com",
		},
		want: loadtest.Config{
			URL:         "http://example.com",
			Concurrency: 10,
			Requests:    20,
			Timeout:     1 * time.Second,
			Method:      http.MethodGet,
			Token:       "",
			Body:        "",
		},
	}

	var stderr bytes.Buffer
	got, err := parseConfig(test.args, &stderr)
	if test.wantErrContains != "" {
		if err == nil {
			t.Fatalf("parseConfig err = nil, want = %q", test.wantErrContains)
		}
		if !strings.Contains(err.Error(), test.wantErrContains) {
			t.Errorf("parseConfig() err = %q, want = %q", err.Error(), test.wantErrContains)
		}
		return
	}
	if err != nil {
		t.Fatalf("parseConfig() unexpected error = %v", err)
	}
	if test.want != got {
		t.Errorf("parseConfig() got = %v, want = %v", got, test.want)
	}
}

// [should-fix] `render` promises to return and wrap writer failures, but no test protects
// that contract. Pass a small test writer whose Write returns a sentinel error, then assert
// `errors.Is(renderErr, sentinel)`; otherwise a future swallowed output error goes unnoticed.
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
	test := parseConfigCase{
		name: "all value supplied",
		args: []string{
			"-url", "http://example.com",
			"-c", "12",
			"-n", "250",
			"-method", "POST",
			"-token", "token",
			"-body", `{"body": "some body"}`,
			"-timeout", "500ms",
		},
		want: loadtest.Config{
			URL:         "http://example.com",
			Concurrency: 12,
			Requests:    250,
			Method:      http.MethodPost,
			Token:       "token",
			Body:        `{"body": "some body"}`,
			Timeout:     500 * time.Millisecond,
		},
	}
	var stderr bytes.Buffer
	got, err := parseConfig(test.args, &stderr)
	if err != nil {
		t.Fatalf("parseConfig err = %q, want = nil", err)
	}
	if test.want != got {
		t.Errorf("parseConfig() got = %v, want = %v", got, test.want)
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

func TestWriteFailure(t *testing.T) {
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

	err := render(failingWriter{}, summary)
	if err == nil {
		t.Fatalf("render() error = nil, want %s", errWrite.Error())
	}
	if !errors.Is(err, errWrite) {
		t.Errorf("render() err = %s, want = %s", err.Error(), errWrite.Error())
	}
}
