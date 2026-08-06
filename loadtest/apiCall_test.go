package loadtest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

type hitCase struct {
	name        string
	timeout     time.Duration
	httpMethod  string
	headers     http.Header
	wantHeaders http.Header
	reqBody     string
	mockStatus  int
}

func assertEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", field, got, want)
	}
}

func assertPositiveStats(t *testing.T, field string, got float64) {
	t.Helper()
	if got <= 0.0 {
		t.Errorf("%s = %f, want > 0", field, got)
	}
}

func checkRequest(t *testing.T, tc hitCase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assertEqual(t, "method", r.Method, tc.httpMethod)

		for name, want := range tc.wantHeaders {
			if got := r.Header.Values(name); !slices.Equal(got, want) {
				t.Errorf("header %q = %v, want %v", name, got, want)
			}
		}

		if tc.reqBody != "" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("error occurred when reading body content. err -> %v", err)
				w.WriteHeader(tc.mockStatus)
				return
			}
			assertEqual(t, "body", string(body), tc.reqBody)
		}
		w.WriteHeader(tc.mockStatus)
	}
}

func TestHitSendsRequest(t *testing.T) {

	tests := []hitCase{
		{
			name:       "GET with token",
			httpMethod: http.MethodGet,
			timeout:    defaultTimeout,
			headers: http.Header{
				"Authorization": {"Bearer token"},
			},
			wantHeaders: http.Header{
				"Authorization": {"Bearer token"},
				"Content-Type":  nil,
			},
			reqBody:    "",
			mockStatus: http.StatusOK,
		},
		{
			name:       "GET, 500 passed through status",
			httpMethod: http.MethodGet,
			timeout:    defaultTimeout,
			headers:    http.Header{},
			wantHeaders: http.Header{
				"Content-Type": nil,
			},
			reqBody:    "",
			mockStatus: http.StatusInternalServerError,
		},
		{
			name:       "GET without headers sends no content type",
			httpMethod: http.MethodGet,
			timeout:    defaultTimeout,
			headers:    http.Header{},
			wantHeaders: http.Header{
				"Content-Type":  nil,
				"Authorization": nil,
			},
			reqBody:    "",
			mockStatus: http.StatusOK,
		},
		{
			name:       "POST with token and body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			headers: http.Header{
				"Authorization": {"Bearer token"},
				"Content-Type":  {"application/json"},
			},
			wantHeaders: http.Header{
				"Authorization": {"Bearer token"},
				"Content-Type":  {"application/json"},
			},
			reqBody:    `{"body":"hi"}`,
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST with token, no body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			headers: http.Header{
				"Authorization": {"Bearer token"},
			},
			wantHeaders: http.Header{
				"Authorization": {"Bearer token"},
				"Content-Type":  nil,
			},
			reqBody:    "",
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST no token, with body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			headers: http.Header{
				"Content-Type": {"application/json"},
			},
			wantHeaders: http.Header{
				"Authorization": nil,
				"Content-Type":  {"application/json"},
			},
			reqBody:    `{"body":"hi"}`,
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST no token, no body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			headers:    http.Header{},
			wantHeaders: http.Header{
				"Authorization": nil,
				"Content-Type":  nil,
			},
			reqBody:    "",
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST with body and no content type gets json default",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			headers:    http.Header{},
			reqBody:    `{"body":"hi"}`,
			wantHeaders: http.Header{
				"Content-Type": {"application/json"},
			},
			mockStatus: http.StatusCreated,
		},
		{
			name:       "explicit content type overrides json default",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			headers:    http.Header{"Content-Type": {"application/xml"}},
			reqBody:    "<order/>",
			wantHeaders: http.Header{
				"Content-Type": {"application/xml"},
			},
			mockStatus: http.StatusCreated,
		},
		{
			name:        "repeated header keeps input order",
			httpMethod:  http.MethodGet,
			timeout:     defaultTimeout,
			headers:     http.Header{"X-Tag": {"a", "b"}},
			wantHeaders: http.Header{"X-Tag": {"a", "b"}},
			mockStatus:  http.StatusOK,
		},
		{
			name:       "custom headers pass through untouched",
			httpMethod: http.MethodGet,
			timeout:    defaultTimeout,
			headers: http.Header{
				"X-API-Key": {"secret"},
				"X-Reason":  {"load-test"},
			},
			wantHeaders: http.Header{
				"X-API-Key":     {"secret"},
				"X-Reason":      {"load-test"},
				"Authorization": nil,
			},
			mockStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			mockServer := httptest.NewServer(checkRequest(t, tc))
			defer mockServer.Close()

			r := newRunner(tc.timeout)
			got, err := r.hit(t.Context(), tc.httpMethod, mockServer.URL, tc.reqBody, tc.headers)

			if err != nil {
				t.Fatalf("hit() error = %v, want nil", err)
			}
			if got != tc.mockStatus {
				t.Errorf("status = %d, want %d", got, tc.mockStatus)
			}
		})
	}

}

func TestServerNotReachableError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := mockServer.URL
	mockServer.Close()

	r := newRunner(defaultTimeout)
	_, err := r.hit(t.Context(), http.MethodGet, url, "", http.Header{})
	if err == nil {
		t.Error("hitting a closed server: want error, got nil")
	}
}

func TestHitURLError(t *testing.T) {
	// Passing "%" as url so that url.Parse (inside http.NewRequestWithContext) rejects it.
	// Otherwise it reads as magic.
	url := "%"

	r := newRunner(defaultTimeout)
	_, err := r.hit(t.Context(), http.MethodGet, url, "", http.Header{})

	if err == nil {
		t.Error("providing false URL: want error, got nil")
	}
}

func TestRequestTimeout(t *testing.T) {

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(90 * time.Millisecond)
	}))
	defer mockServer.Close()

	timeout := 10 * time.Millisecond
	r := newRunner(timeout)
	got, err := r.hit(t.Context(), http.MethodGet, mockServer.URL, "", http.Header{})

	if err == nil {
		t.Error("expected timeout error, overwaited for the server response")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context deadline exceeded, got res -> %v, err -> %v", got, err)
	}
}

func TestContextCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-req.Context().Done()
	}))
	defer mockServer.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	finished := make(chan error, 1)
	go func() {
		r := newRunner(defaultTimeout)
		_, err := r.hit(ctx, http.MethodGet, mockServer.URL, "", http.Header{})
		finished <- err
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Request not fired even after 500 milliseconds")
	}

	select {
	case gotError := <-finished:
		if gotError == nil {
			t.Fatal("expected context cancellation error, got nil")
		}
		if !errors.Is(gotError, context.Canceled) {
			t.Errorf("expected context cancellation message, got %v", gotError)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return promptly after cancellation")
	}

}

func TestResponseBodyError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("response writer does not support hijacking")
		}

		conn, rw, err := hijacker.Hijack()

		if err != nil {
			t.Errorf("hijack connection: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		_, err = rw.WriteString(
			"HTTP/1.1 200 OK\r\n" +
				"Content-Length: 100\r\n" +
				"Connection: close\r\n" +
				"\r\n" +
				"short",
		)
		if err != nil {
			t.Errorf("write raw response err: %v", err)
		}

		if err := rw.Flush(); err != nil {
			t.Errorf("flush raw response err: %v", err)
		}
	}))
	defer mockServer.Close()

	r := newRunner(defaultTimeout)

	_, err := r.hit(t.Context(), http.MethodGet, mockServer.URL, "", http.Header{})

	if err == nil {
		t.Fatal("hit() error = nil, want body-read error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}
