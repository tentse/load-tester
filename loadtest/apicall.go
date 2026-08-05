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

func (r *runner) hit(ctx context.Context, httpMethod, targetURL, reqBody string, headers http.Header) (int, error) {
	var body io.Reader
	if reqBody != "" {
		body = strings.NewReader(reqBody)
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, targetURL, body)
	if err != nil {
		return 0, fmt.Errorf("build %s %s: %w", httpMethod, targetURL, err)
	}

	for name, values := range headers {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}

	if reqBody != "" && req.Header.Get(headerContentType) == "" {
		req.Header.Set(headerContentType, contentTypeJSON)
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
