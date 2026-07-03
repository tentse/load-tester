package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

type parseConfigCase struct {
	name            string
	args            []string
	want            loadtest.Config
	wantErrContains string
}

func TestParseConfigDefaults(t *testing.T) {
	var stderr bytes.Buffer

	got, err := parseConfig(
		[]string{
			"-url", "http://example.com",
		},
		&stderr,
	)
	if err != nil {
		t.Fatalf("parseConfig() error = %v, want nil", err)
	}

	want := loadtest.Config{
		URL:         "http://example.com",
		Concurrency: 1,
		Requests:    1,
		Timeout:     1 * time.Second,
		Method:      http.MethodGet,
		Token:       "",
		Body:        "",
	}

	if got != want {
		t.Errorf("parseConfig() = %+v, want %+v", got, want)
	}

	if got := stderr.String(); got != "" {
		t.Errorf("stderr = %q, want empty", got)
	}
}

func TestParseConfigMissingValue(t *testing.T) {

	tests := []parseConfigCase{
		{
			name: "absent url",
			args: []string{
				"-method", "GET",
				"-timeout", "5s",
				"-n", "1000",
				"-c", "50",
				"-token", "secret-token",
				"-body", `{"name": "test"}`,
			},
			wantErrContains: "-url is required",
		},
		{
			name: "absent concurrency",
			args: []string{
				"-url", "http://example.com",
				"-method", "GET",
				"-timeout", "5s",
				"-n", "1000",
				"-token", "secret-token",
				"-body", `{"name": "test"}`,
			},
			want: loadtest.Config{
				URL:         "http://example.com",
				Concurrency: 1,
				Requests:    1000,
				Timeout:     5 * time.Second,
				Method:      http.MethodGet,
				Token:       "secret-token",
				Body:        `{"name": "test"}`,
			},
		},
		{
			name: "absent requests",
			args: []string{
				"-url", "http://example.com",
				"-method", "GET",
				"-timeout", "5s",
				"-c", "10",
				"-token", "secret-token",
				"-body", `{"name": "test"}`,
			},
			want: loadtest.Config{
				URL:         "http://example.com",
				Concurrency: 10,
				Requests:    1,
				Timeout:     5 * time.Second,
				Method:      http.MethodGet,
				Token:       "secret-token",
				Body:        `{"name": "test"}`,
			},
		},
		{
			name: "absent method",
			args: []string{
				"-url", "http://example.com",
				"-timeout", "5s",
				"-c", "10",
				"-n", "1000",
				"-token", "secret-token",
				"-body", `{"name": "test"}`,
			},
			want: loadtest.Config{
				URL:         "http://example.com",
				Concurrency: 10,
				Requests:    1000,
				Timeout:     5 * time.Second,
				Method:      http.MethodGet,
				Token:       "secret-token",
				Body:        `{"name": "test"}`,
			},
		},
		{
			name: "absent timeout",
			args: []string{
				"-url", "http://example.com",
				"-c", "10",
				"-n", "1000",
				"-token", "secret-token",
				"-body", `{"name": "test"}`,
			},
			want: loadtest.Config{
				URL:         "http://example.com",
				Concurrency: 10,
				Requests:    1000,
				Timeout:     1 * time.Second,
				Method:      http.MethodGet,
				Token:       "secret-token",
				Body:        `{"name": "test"}`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got, err := parseConfig(tc.args, &stderr)
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("parseConfig err = nil, want = %q", tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("parseConfig() err = %q, want = %q", err.Error(), tc.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConfig() unexpected error = %v", err)
			}
			if tc.want != got {
				t.Errorf("parseConfig() got = %v, want = %v", got, tc.want)
			}
		})
	}
}

func TestParseConfigInvalidValue(t *testing.T) {
	tests := []parseConfigCase{
		{
			name: "invalid concurrency",
			args: []string{
				"-url", "http://example.com",
				"-method", "GET",
				"-timeout", "5s",
				"-n", "1000",
				"-c", "invalid",
				"-token", "secret-token",
				"-body", `{"name": "test"}`,
			},
			wantErrContains: "flag -c",
		},
		{
			name: "invalid requests",
			args: []string{
				"-url", "http://example.com",
				"-method", "GET",
				"-timeout", "5s",
				"-n", "invalid",
				"-c", "50",
				"-token", "secret-token",
				"-body", `{"name": "test"}`,
			},
			wantErrContains: "flag -n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := parseConfig(tc.args, &stderr)
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("parseConfig() err = nil, want = %q", tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("parseConfig() err = %q, want = %q", err.Error(), tc.wantErrContains)
				}
				return
			}
		})
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
