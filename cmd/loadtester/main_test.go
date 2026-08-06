package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	wantErrContains := "-url is required"
	var stderr bytes.Buffer
	_, err := parseConfig([]string{"-c", "10", "-n", "100"}, &stderr)
	if err == nil {
		t.Fatal("expected parsing to fail due to missing url but got err = nil")
	}
	if got := stderr.String(); !strings.Contains(got, wantErrContains) {
		t.Errorf("parseConfig() err = %v, want to contain %q", got, wantErrContains)
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
		{
			name: "invalid header missing colon",
			args: []string{
				"-url", "http://www.example.com",
				"-H", "X-Tag; a",
			},
			wantErrContains: `want "Name: Value"`,
		},
		{
			name: "invalid header missing name",
			args: []string{
				"-url", "http://www.example.com",
				"-H", ": a",
			},
			wantErrContains: "empty name",
		},
		{
			name: "invalid header value contains \n",
			args: []string{
				"-url", "http://www.example.com",
				"-H", "X-Tag: a\nb",
			},
			wantErrContains: "contains CR or LF",
		},
		{
			name: "invalid header value contains \r",
			args: []string{
				"-url", "http://www.example.com",
				"-H", "X-Tag: a\rb",
			},
			wantErrContains: "contains CR or LF",
		},
		{
			name: "invalid header name contains \n",
			args: []string{
				"-url", "http://www.example.com",
				"-H", "X-\nTag: ab",
			},
			wantErrContains: "contains CR or LF",
		},
		{
			name: "invalid header name contains \r",
			args: []string{
				"-url", "http://www.example.com",
				"-H", "X-Ta\rg: ab",
			},
			wantErrContains: "contains CR or LF",
		},
		{
			name: "invalid header contains whitespace in the name",
			args: []string{
				"-url", "http://www.example.com",
				"-H", "X Tag: ab",
			},
			wantErrContains: "contains whitespace character",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := parseConfig(tc.args, &stderr)
			if err == nil {
				t.Fatalf("expected error = %q, got = nil", tc.wantErrContains)
			}
			if got := stderr.String(); !strings.Contains(got, tc.wantErrContains) {
				t.Errorf("parseConfig() err = %q, want = %q", got, tc.wantErrContains)
			}
		})
	}
}

func TestMissingArgument(t *testing.T) {
	wantErrContains := "flag needs an argument: -c"
	var stderr bytes.Buffer
	_, err := parseConfig([]string{"-url", "http://example.com", "-c"}, &stderr)
	if err == nil {
		t.Fatalf("expected error = %q, got = nil", wantErrContains)
	}
	if got := stderr.String(); !strings.Contains(got, wantErrContains) {
		t.Errorf("parseConfig() err = %q, want = %q", got, wantErrContains)
	}

}

func TestUnknownFlag(t *testing.T) {
	wantErrContains := "not defined: -unknownFlag"
	var stderr bytes.Buffer
	_, err := parseConfig([]string{"-unknownFlag", "value"}, &stderr)
	if err == nil {
		t.Fatalf("expected error = %q, got = nil", wantErrContains)
	}
	if got := stderr.String(); !strings.Contains(got, wantErrContains) {
		t.Fatalf("parseConfig() err = %v, want to contain %q", got, wantErrContains)
	}
}

func TestParseConfigDefaultValues(t *testing.T) {

	args := []string{
		"-url", "http://example.com",
	}
	want := loadtest.Config{
		URL:         "http://example.com",
		Concurrency: 10,
		Requests:    20,
		Timeout:     1 * time.Second,
		Method:      http.MethodGet,
		Headers:     http.Header{},
		Body:        "",
	}
	var stderr bytes.Buffer
	got, err := parseConfig(args, &stderr)
	if err != nil {
		t.Fatalf("parseConfig() unexpected error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseConfig() got = %v, want = %v", got, want)
	}
}

func TestParseConfigHelpErr(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseConfig([]string{"-h"}, &stderr)

	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("parseConfig() got error = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "permission") {
		t.Errorf("usage output missing safety warning: %q", stderr.String())
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
	args := []string{
		"-url", "http://example.com",
		"-c", "12",
		"-n", "250",
		"-method", "POST",
		"-H", "Content-Type: application/json",
		"-H", "Authorization: Bearer token",
		"-body", `{"body": "some body"}`,
		"-timeout", "500ms",
	}
	want := loadtest.Config{
		URL:         "http://example.com",
		Concurrency: 12,
		Requests:    250,
		Method:      http.MethodPost,
		Headers: http.Header{
			"Authorization": {"Bearer token"},
			"Content-Type":  {"application/json"},
		},
		Body:    `{"body": "some body"}`,
		Timeout: 500 * time.Millisecond,
	}
	var stderr bytes.Buffer
	got, err := parseConfig(args, &stderr)
	if err != nil {
		t.Fatalf("parseConfig err = %q, want = nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseConfig() got = %+v, want = %+v", got, want)
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

func TestRunSuccess(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()
	var stdout, stderr bytes.Buffer

	args := []string{
		"-url", mockServer.URL,
		"-c", "1",
		"-n", "2",
		"-method", http.MethodGet,
		"-timeout", "1s",
	}
	got := run(t.Context(), args, &stdout, &stderr)
	if got != 0 {
		t.Fatalf("run() exit code = %d, want exit code = 0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("run() error = %q, want = nil", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"Load test summary\n",
		"Total: 2\n",
		"Succeeded: 2\n",
		"Failed: 0\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("stdout = %q, want it contains %q", output, want)
		}
	}
}

func TestRunSensitiveUrlContent(t *testing.T) {
	var stdout, stderr bytes.Buffer

	args := []string{
		"-url", "http://api_key:actual_api_key",
		"-c", "1",
		"-n", "1",
		"-method", http.MethodGet,
		"-timeout", "1s",
	}
	got := run(t.Context(), args, &stdout, &stderr)
	if got != 0 {
		t.Errorf("run() exit code = %d, want 0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("run() error = %q, want = nil", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"Load test summary\n",
		"Total: 1\n",
		"Succeeded: 0\n",
		"Failed: 1\n",
		"Errors:\n  request failed: 1\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("stdout = %q, want it contains %q", output, want)
		}
	}

	if terminalOutput := output + stderr.String(); strings.Contains(terminalOutput, "actual_api_key") {
		t.Errorf("terminal output leaked sensitive detail: %q", terminalOutput)
	}
}

func TestRunParseFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer

	got := run(t.Context(), []string{}, &stdout, &stderr)
	if got != 2 {
		t.Fatalf("run() exit code = %d, want exit code = 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("run() stdout = %q, want = nil", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatalf("run() error empty")
	}

	errContains := "-url is required"
	err := stderr.String()
	if !strings.Contains(err, errContains) {
		t.Errorf("run() err = %q, want = %q", err, errContains)
	}
}

func TestRunParseFailureWithStderrWriteFailure(t *testing.T) {
	var stdout bytes.Buffer

	got := run(t.Context(), []string{}, &stdout, failingWriter{})

	if got != 2 {
		t.Fatalf("run() got = %d, want = 2", got)
	}
	if stdout.Len() != 0 {
		t.Errorf("run() stdout = %q, want = nil", stdout.String())
	}

}

func TestRunRenderFailureWithStderrWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()
	got := run(t.Context(), []string{"-url", mockServer.URL}, failingWriter{}, &stderr)

	if got != 1 {
		t.Fatalf("run() got = %d, want = 1", got)
	}

}

func TestRunInvalidConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := run(t.Context(), []string{"-url", "http://example.com", "-c", "0"}, &stdout, &stderr)
	if got != 2 {
		t.Fatalf("run() exit code = %d, want exit code = 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("run() stdout = %q, want = nil", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("run() stderr empty")
	}

	errContains := "invalid concurrency"
	err := stderr.String()
	if !strings.Contains(err, errContains) {
		t.Errorf("run() err = %q, want = %q", err, errContains)
	}
}

func TestRunStdoutWriteFail(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()
	var stderr bytes.Buffer
	got := run(t.Context(), []string{"-url", mockServer.URL}, failingWriter{}, &stderr)

	if got != 1 {
		t.Fatalf("run() exit code = %d, want exit code = 1", got)
	}
	if stderr.Len() == 0 {
		t.Fatalf("run() stderr empty")
	}

	errContains := "stdout write error"
	err := stderr.String()
	if !strings.Contains(err, errContains) {
		t.Errorf("run() err = %q, want = %q", err, errContains)
	}
}

func TestRunContextCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer mockServer.Close()

	var stdout, stderr bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan int, 1)
	go func() {
		result <- run(ctx, []string{"-url", mockServer.URL, "-c", "1", "-n", "1"}, &stdout, &stderr)
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}

	select {
	case got := <-result:
		if got != 130 {
			t.Fatalf("error code = %d, expected error code = 130", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() took more than 2 seconds")
	}

	if !strings.Contains(stderr.String(), "load test canceled") {
		t.Errorf("stderr = %q, want cancellation message", stderr.String())
	}

	if stdout.Len() == 0 {
		t.Fatal("run() context cancellation summary = nil")
	}

	output := stdout.String()
	for _, want := range []string{
		"Load test summary\n",
		"Total: 1\n",
		"Succeeded: 0\n",
		"Failed: 1\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("stdout = %q, want it contains %q", output, want)
		}
	}
}

func TestRunContextCancellationStderrWriteFailure(t *testing.T) {
	started := make(chan struct{}, 1)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer mockServer.Close()

	var stdout bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan int, 1)
	go func() {
		result <- run(ctx, []string{"-url", mockServer.URL, "-c", "1", "-n", "1"}, &stdout, failingWriter{})
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}

	select {
	case got := <-result:
		if got != 130 {
			t.Fatalf("error code = %d, expected error code = 130", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() took more than 2 seconds")
	}

	if stdout.Len() == 0 {
		t.Fatal("run() context cancellation summary = nil")
	}

	output := stdout.String()
	for _, want := range []string{
		"Load test summary\n",
		"Total: 1\n",
		"Succeeded: 0\n",
		"Failed: 1\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("stdout = %q, want it contains %q", output, want)
		}
	}
}

func TestRunContextCancellationStdoutWriteFailure(t *testing.T) {
	started := make(chan struct{}, 1)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer mockServer.Close()

	var stderr bytes.Buffer

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan int, 1)
	go func() {
		result <- run(ctx, []string{"-url", mockServer.URL, "-c", "1", "-n", "1"}, failingWriter{}, &stderr)
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}

	select {
	case got := <-result:
		if got != 130 {
			t.Fatalf("error code = %d, expected error code = 130", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() took more than 2 seconds")
	}

	if !strings.Contains(stderr.String(), errWrite.Error()) {
		t.Errorf("stderr = %q, want = %q", stderr.String(), errWrite.Error())
	}
}

func TestRunErrHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := run(t.Context(), []string{"-h"}, &stdout, &stderr)

	if got != 0 {
		t.Errorf("got exit code = %d, want = 0", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("got stdout = %v, want = nil", stdout.String())
	}
	if !strings.Contains(stderr.String(), "permission") {
		t.Fatalf("got stderr = %v, want contains = permission", stderr.String())
	}
}
