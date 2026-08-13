package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "requests.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestHasFileFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "absent", args: []string{"-url", "http://example.com", "-expect", "200"}, want: false},
		{name: "no args", args: nil, want: false},
		{name: "separate value", args: []string{"-f", "requests.json"}, want: true},
		{name: "double dash", args: []string{"--f", "requests.json"}, want: true},
		{name: "after other flags", args: []string{"-c", "5", "-f", "requests.json"}, want: true},
		{name: "not a prefix match", args: []string{"-force", "x"}, want: false},
		{name: "another flag's value looks like it", args: []string{"-method", "-f"}, want: true},
		{name: "equals form", args: []string{"-f=requests.json"}, want: true},
		{name: "double dash equals form", args: []string{"--f=requests.json"}, want: true},
		{name: "equals form after other flags", args: []string{"-c", "5", "-f=requests.json"}, want: true},
		{name: "another flag's equals value is not a prefix match", args: []string{"-force=requests.json"}, want: false},
		{name: "equals value containing the name", args: []string{"-body={\"f\":1}"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasFileFlag(tc.args); got != tc.want {
				t.Errorf("hasFileFlag(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestRunFileSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	path := writeConfig(t, `{
		"version": 1,
		"baseUrl": "`+server.URL+`",
		"concurrency": 2,
		"timeout": "2s",
		"requests": [
			{ "name": "alpha", "url": "/a", "count": 3, "expectStatus": 200 },
			{ "name": "beta",  "url": "/b", "count": 2, "expectStatus": 200 }
		]
	}`)

	var stdout, stderr bytes.Buffer
	got := run(t.Context(), []string{"-f", path}, &stdout, &stderr)

	if got != 0 {
		t.Fatalf("run() exit code = %d, want 0 (stderr: %s)", got, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("run() stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"Name: alpha\n",
		"Name: beta\n",
		"Total: 3\n",
		"Total: 2\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("stdout = %q, want it to contain %q", output, want)
		}
	}
}

func TestRunFileRejectsSingleTargetFlags(t *testing.T) {
	path := writeConfig(t, `{
		"version": 1, "concurrency": 1, "timeout": "1s",
		"requests": [{ "name": "alpha", "url": "https://example.com/", "expectStatus": 200 }]
	}`)

	tests := [][]string{
		{"-url", "https://example.com"},
		{"-c", "5"},
		{"-n", "10"},
		{"-method", "POST"},
		{"-body", `{"a":1}`},
		{"-timeout", "5s"},
		{"-expect", "200"},
		{"-H", "X-Tag: a"},
	}

	for _, extra := range tests {
		t.Run(extra[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"-f", path}, extra...)

			got := run(t.Context(), args, &stdout, &stderr)

			if got != 2 {
				t.Fatalf("run(%q) exit code = %d, want 2", args, got)
			}
			if stdout.Len() != 0 {
				t.Errorf("run() stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), extra[0]) {
				t.Errorf("stderr = %q, want it to name %q", stderr.String(), extra[0])
			}
		})
	}
}

func TestRunFileMissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := run(t.Context(), []string{"-f", filepath.Join(t.TempDir(), "absent.json")}, &stdout, &stderr)

	if got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "absent.json") {
		t.Errorf("stderr = %q, want it to name the file", stderr.String())
	}
}

func TestRunFileInvalidJSON(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantErrContains string
	}{
		{name: "syntax", content: `{ "version": 1, }`, wantErrContains: "invalid character '}'"},
		{name: "unknown field", content: `{ "version": 1, "nope": 1 }`, wantErrContains: "unknown field: nope"},
		{
			name:            "positional",
			content:         `{ "version": 1, "concurrency": 1, "timeout": "1s", "requests": [{ "name": "alpha", "url": "https://e.com/" }] }`,
			wantErrContains: "requests[0].expectStatus",
		},
		{
			name:            "bare number timeout",
			content:         `{ "version": 1, "concurrency": 1, "timeout": 30, "requests": [{ "name": "alpha", "url": "https://e.com/", "expectStatus": 200 }] }`,
			wantErrContains: "invalid timeout",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), []string{"-f", writeConfig(t, tc.content)}, &stdout, &stderr)

			if got != 2 {
				t.Fatalf("run() exit code = %d, want 2", got)
			}
			if stdout.Len() != 0 {
				t.Errorf("run() stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantErrContains) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.wantErrContains)
			}
		})
	}
}

func TestRunFileMissingValue(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantErrContains string
	}{
		{
			name:            "no argument",
			args:            []string{"-f"},
			wantErrContains: "flag needs an argument: -f",
		},
		{
			name:            "empty argument",
			args:            []string{"-f", ""},
			wantErrContains: "-f needs a file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tc.args, &stdout, &stderr)

			if got != 2 {
				t.Fatalf("run(%q) exit code = %d, want 2", tc.args, got)
			}
			if stdout.Len() != 0 {
				t.Errorf("run() stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantErrContains) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.wantErrContains)
			}
		})
	}
}

func TestRunFileHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := run(t.Context(), []string{"-f", "requests.json", "-h"}, &stdout, &stderr)

	if got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if stdout.Len() != 0 {
		t.Errorf("run() stdout = %q, want empty: help goes to stderr", stdout.String())
	}
	for _, want := range []string{"permission", "-f requests.json"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	}
}

func TestRunFileStdinNotSupported(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := run(t.Context(), []string{"-f", "-"}, &stdout, &stderr)

	if got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "stdin") {
		t.Errorf("stderr = %q, want it to explain that stdin is unsupported", stderr.String())
	}
}

func TestRunFileStdoutWriteFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	path := writeConfig(t, `{
		"version": 1, "baseUrl": "`+server.URL+`", "concurrency": 1, "timeout": "2s",
		"requests": [{ "name": "alpha", "url": "/a", "count": 1, "expectStatus": 200 }]
	}`)

	var stderr bytes.Buffer
	got := run(t.Context(), []string{"-f", path}, failingWriter{}, &stderr)

	if got != 1 {
		t.Fatalf("run() exit code = %d, want 1", got)
	}
	if !strings.Contains(stderr.String(), "stdout write error") {
		t.Errorf("stderr = %q, want it to report the write failure", stderr.String())
	}
}

func TestRunFileCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	path := writeConfig(t, `{
		"version": 1, "baseUrl": "`+server.URL+`", "concurrency": 1, "timeout": "5s",
		"requests": [{ "name": "alpha", "url": "/a", "count": 50, "expectStatus": 200 }]
	}`)

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	result := make(chan int, 1)
	go func() {
		result <- run(ctx, []string{"-f", path}, &stdout, &stderr)
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
			t.Fatalf("run() exit code = %d, want 130", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() took more than 2 seconds after cancellation")
	}

	if !strings.Contains(stderr.String(), "load test canceled") {
		t.Errorf("stderr = %q, want the cancellation message", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Name: alpha") {
		t.Errorf("stdout = %q, want the partial summary", stdout.String())
	}
}

func TestRunFileCancellationStdoutWriteFailure(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	path := writeConfig(t, `{
		"version": 1, "baseUrl": "`+server.URL+`", "concurrency": 1, "timeout": "5s",
		"requests": [{ "name": "alpha", "url": "/a", "count": 50, "expectStatus": 200 }]
	}`)

	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	result := make(chan int, 1)
	go func() {
		result <- run(ctx, []string{"-f", path}, failingWriter{}, &stderr)
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
			t.Fatalf("run() exit code = %d, want 130", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() took more than 2 seconds after cancellation")
	}

	if !strings.Contains(stderr.String(), "stdout write error") {
		t.Errorf("stderr = %q, want it to report the write failure", stderr.String())
	}
}
