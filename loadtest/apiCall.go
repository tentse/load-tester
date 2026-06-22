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
	defaultTimeout      = 30 * time.Second
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
)

const (
	headerAuth        = "Authorization"
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
	bearerPrefix      = "Bearer "
)

// [should-fix] Exported type, but with an unexported constructor (newRunner) and only an
// unexported method (hit) -- nothing outside the package can build or use it, so the capital R
// earns nothing, and revive/golint will warn "exported type Runner should have a comment."
// Two clean options: (a) unexport to `runner` now -- it's internal plumbing behind the eventual
// Config/Run API, so minimal surface wins (rule of three); or (b) if it's meant to be public,
// give it a godoc comment ("Runner ...") and an exported way to build it. I'd unexport for now.
type Runner struct {
	client *http.Client
}

func newRunner() *Runner {
	return &Runner{client: newClient()}
}

func newClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = maxIdleConns
	t.MaxIdleConnsPerHost = maxIdleConnsPerHost

	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: t,
	}
}

func (r *Runner) hit(ctx context.Context, httpMethod, targetURL, token, reqBody string) (int, error) {
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
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}
