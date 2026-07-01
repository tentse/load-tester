package loadtest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type hitCase struct {
	name       string
	timeout    time.Duration
	httpMethod string
	token      string
	wantAuth   string
	reqBody    string
	mockStatus int
}

func checkRequest(t *testing.T, tc hitCase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != tc.httpMethod {
			t.Errorf("method = %q, want %q", r.Method, tc.httpMethod)
		}
		if got := r.Header.Get("Authorization"); got != tc.wantAuth {
			t.Errorf("authorization = %q, want %q", got, tc.wantAuth)
		}

		if tc.reqBody != "" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("error occurred when reading body content. err -> %v", err)
				// [should-fix] Return after reporting this read failure. Continuing compares a
				// partial/invalid body and can produce misleading secondary failures that hide
				// the operation that actually broke.
			}
			if string(body) != tc.reqBody {
				t.Errorf("body = %q, want %q", body, tc.reqBody)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("content-Type = %q, want application/json", got)
			}
		} else if got := r.Header.Get("Content-Type"); got != "" {
			t.Errorf("reqBody empty but content type present = %q", got)
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
			token:      "token",
			wantAuth:   "Bearer token",
			reqBody:    "",
			mockStatus: http.StatusOK,
		},
		{
			name:       "GET, 500 passed through status",
			httpMethod: http.MethodGet,
			timeout:    defaultTimeout,
			token:      "",
			wantAuth:   "",
			reqBody:    "",
			mockStatus: http.StatusInternalServerError,
		},
		{
			name:       "POST with token and body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			token:      "token",
			wantAuth:   "Bearer token",
			reqBody:    `{"body":"hi"}`,
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST with token, no body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			token:      "token",
			wantAuth:   "Bearer token",
			reqBody:    "",
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST no token, with body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			token:      "",
			wantAuth:   "",
			reqBody:    `{"body":"hi"}`,
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST no token, no body",
			httpMethod: http.MethodPost,
			timeout:    defaultTimeout,
			token:      "",
			wantAuth:   "",
			reqBody:    "",
			mockStatus: http.StatusCreated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			mockServer := httptest.NewServer(checkRequest(t, tc))
			defer mockServer.Close()

			r := newRunner(tc.timeout)
			got, err := r.hit(t.Context(), tc.httpMethod, mockServer.URL, tc.token, tc.reqBody)

			if err != nil {
				t.Fatalf("hit() error = %v, want nil", err)
			}
			if got != tc.mockStatus {
				t.Errorf("status = %d, want %d", got, tc.mockStatus)
			}
		})
	}

}

func TestHitTransportError(t *testing.T) {

	mockServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := mockServer.URL
	mockServer.Close()

	r := newRunner(defaultTimeout)
	_, err := r.hit(t.Context(), http.MethodGet, url, "", "")
	if err == nil {
		t.Error("hitting a closed server: want error, got nil")
	}
}

func TestHitURLError(t *testing.T) {
	// Passing "%" as url so that url.Parse (inside http.NewRequestWithContext) rejects it.
	// Otherwise it reads as magic.
	url := "%"

	r := newRunner(defaultTimeout)
	_, err := r.hit(t.Context(), http.MethodGet, url, "", "")

	if err == nil {
		t.Error("providing false URL: want error, got nil")
	}
}

func TestRequestTimeOut(t *testing.T) {

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-time.After(90 * time.Millisecond):
		case <-req.Context().Done():
		}
	}))
	defer mockServer.Close()

	timeout := 10 * time.Millisecond
	r := newRunner(timeout)
	_, err := r.hit(t.Context(), http.MethodGet, mockServer.URL, "", "")

	if err == nil {
		t.Error("expected timeout error, overwaited for the server response")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context deadline exceeded, got %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-time.After(90 * time.Millisecond):
		case <-req.Context().Done():
		}
	}))
	defer mockServer.Close()

	r := newRunner(defaultTimeout)
	// [should-fix] A fixed delay does not prove the handler received the request before
	// cancellation. On a slow machine this can exercise an already-cancelled context instead
	// of an in-flight request. Have the handler signal a `started` channel, then cancel after
	// the test receives that signal; concurrency tests should coordinate events, not timing.
	ctx, cancel := context.WithCancel(t.Context())

	timer := time.AfterFunc(10*time.Millisecond, cancel)

	defer timer.Stop()
	defer cancel()

	_, err := r.hit(ctx, http.MethodGet, mockServer.URL, "", "")
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation message, got %v", err)
	}
}
