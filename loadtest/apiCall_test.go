package loadtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type hitCase struct {
	name       string
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
			body, _ := io.ReadAll(r.Body)
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

	// [nit] `caseConfig` -> the conventional Go name for a test table is `tests` or `cases`.
	caseConfig := []hitCase{
		{
			name:       "Hitting GET with valid token",
			httpMethod: http.MethodGet,
			token:      "token",
			wantAuth:   "Bearer token",
			reqBody:    "",
			mockStatus: http.StatusOK,
		},
		{
			name:       "GET, server returns 500",
			httpMethod: http.MethodGet,
			token:      "",
			wantAuth:   "",
			reqBody:    "",
			mockStatus: http.StatusInternalServerError,
		},
		{
			name:       "POST with token and body",
			httpMethod: http.MethodPost,
			token:      "token",
			wantAuth:   "Bearer token",
			reqBody:    `{"body":"hi"}`,
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST with token without body",
			httpMethod: http.MethodPost,
			token:      "token",
			wantAuth:   "Bearer token",
			reqBody:    "",
			mockStatus: http.StatusCreated,
		},
		{
			name:       "POST without token with body",
			httpMethod: http.MethodPost,
			token:      "",
			wantAuth:   "",
			reqBody:    `{"body":"hi"}`,
			mockStatus: http.StatusCreated,
		},
		// [should-fix] Good -- this is the no-token + no-body combination the earlier review
		// flagged as missing. But the name is copy-pasted from the row above: this case has
		// reqBody: "", so it's "POST WITHOUT body". t.Run uses name as the subtest id, so two
		// identical names collide -- Go appends "#01" and the output mislabels which case ran.
		// Rename to "POST without token without body".
		{
			name:       "POST without token with body",
			httpMethod: http.MethodPost,
			token:      "",
			wantAuth:   "",
			reqBody:    "",
			mockStatus: http.StatusCreated,
		},
	}

	for _, tc := range caseConfig {
		t.Run(tc.name, func(t *testing.T) {

			mockServer := httptest.NewServer(checkRequest(t, tc))
			defer mockServer.Close()

			r := newRunner()
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

	r := newRunner()
	_, err := r.hit(t.Context(), http.MethodGet, url, "", "")
	if err == nil {
		t.Error("hitting a closed server: want error, got nil")
	}
}

func TestHitURLError(t *testing.T) {
	// Passing "%" as url so that url.Parse (inside http.NewRequestWithContext) rejects it.
	// Otherwise it reads as magic.
	url := "%"

	r := newRunner()
	_, err := r.hit(t.Context(), http.MethodGet, url, "", "")

	if err == nil {
		t.Error("providing false URL: want error, got nil")
	}
}

// [should-fix] Coverage gap, important for this project: nothing tests that ctx cancellation
// actually reaches an in-flight request. For a load tester that's the headline feature --
// Ctrl+C / a timeout MUST abort live requests. Future test: a slow handler + a context you
// cancel mid-flight, then assert hit() returns quickly with errors.Is(err, context.Canceled).
//
// Reminder: run `go test -race ./...`. Once Run() drives many goroutines through the shared
// http.Client, the race detector is the only thing that'll catch unsynchronized access.
