package loadtest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
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

	// [should-fix] You're swallowing the io.Copy error. Draining is right (it lets the
	// connection return to the pool for reuse), but under load a connection can drop
	// mid-body -- that's a failed request, yet here it returns (status, nil) and gets
	// counted as a success. Capture it: `if _, err := io.Copy(io.Discard, resp.Body); err
	// != nil { return resp.StatusCode, fmt.Errorf("read body %s %s: %w", httpMethod,
	// targetURL, err) }`. Returning the status alongside the error is fine -- the caller
	// already treats err != nil as the failure signal.
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}
