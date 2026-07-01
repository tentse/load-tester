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
		assertEqual(t, "authorization", r.Header.Get("Authorization"), tc.wantAuth)

		if tc.reqBody != "" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("error occurred when reading body content. err -> %v", err)
				w.WriteHeader(tc.mockStatus)
				return
			}
			assertEqual(t, "body", string(body), tc.reqBody)
			assertEqual(t, "content-type", r.Header.Get("Content-Type"), "application/json")
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

func TestServerNotReachableError(t *testing.T) {
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

func TestRequestTimeout(t *testing.T) {

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(90 * time.Millisecond)
	}))
	defer mockServer.Close()

	timeout := 10 * time.Millisecond
	r := newRunner(timeout)
	got, err := r.hit(t.Context(), http.MethodGet, mockServer.URL, "", "")

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
		_, err := r.hit(ctx, http.MethodGet, mockServer.URL, "", "")
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
