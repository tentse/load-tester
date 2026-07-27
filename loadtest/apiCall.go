package loadtest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// [should-fix] The loadtest package has no package comment anywhere, even though keeping this
// API importable (rather than burying it under internal/) is an explicit goal in AGENTS.md.
// pkg.go.dev and `go doc` currently show a bare package name and nothing else — the first
// thing a prospective user of your library sees is blank. Convention: one file owns it (often
// a small doc.go), full sentences, starting "Package loadtest ...". revive's package-comments
// rule flags this.
//
// [nit] (FEEDBACK.md #13) The filename apiCall.go is camelCase; Go filenames are conventionally
// all-lowercase — client.go, request.go, apicall.go. Purely cosmetic, but it's the first thing
// a Go reviewer's eye snags on in a package that is otherwise idiomatic.

const (
	// [nit] defaultTimeout is now referenced only from tests — no production path uses it,
	// because Run always passes config.Timeout through — while cmd/loadtester hardcodes its
	// own separate `1*time.Second` flag default. That's two sources of truth for one number,
	// and they will drift the first time you change one. Either export it so the CLI can use
	// it as its flag default, or move it into the test file where it's actually used.
	defaultTimeout      = 1 * time.Second
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
)

const (
	headerAuth        = "Authorization"
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
	bearerPrefix      = "Bearer "
)

type runner struct {
	client *http.Client
}

func newRunner(timeout time.Duration) *runner {
	return &runner{client: newClient(timeout)}
}

func newClient(timeout time.Duration) *http.Client {
	// [nit] (FEEDBACK.md #10) Unchecked type assertion. http.DefaultTransport is a package-level
	// variable of interface type, and plenty of libraries reassign it (tracing wrappers, test
	// doubles, VCR-style recorders). If anything in the process ever does, this line panics —
	// and AGENTS.md says library code must never panic. The comma-ok form lets you fall back to
	// a fresh &http.Transport{} instead of taking the program down.
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = maxIdleConns
	t.MaxIdleConnsPerHost = maxIdleConnsPerHost

	return &http.Client{
		Timeout:   timeout,
		Transport: t,
	}
}

func (r *runner) hit(ctx context.Context, httpMethod, targetURL, token, reqBody string) (int, error) {
	var body io.Reader
	if reqBody != "" {
		body = strings.NewReader(reqBody)
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, targetURL, body)
	if err != nil {
		return 0, fmt.Errorf("build %s %s: %w", httpMethod, targetURL, err)
	}

	if reqBody != "" {
		req.Header.Set(headerContentType, contentTypeJSON)
	}
	if token != "" {
		req.Header.Set(headerAuth, bearerPrefix+token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("do %s %s: %w", httpMethod, targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, fmt.Errorf("read response body: %w", err)
	}

	return resp.StatusCode, nil
}
