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
	bearerToken       = "Bearer "
)

var client = newClient()

func newClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = maxIdleConns
	t.MaxIdleConnsPerHost = maxIdleConnsPerHost

	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: t,
	}
}

func hit(ctx context.Context, httpMethod, targetURL, token, reqBody string) (int, error) {

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
		req.Header.Set(headerAuth, bearerToken+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("do %s %s: %w", httpMethod, targetURL, err)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil

}
