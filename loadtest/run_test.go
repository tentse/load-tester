package loadtest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestRun(t *testing.T) {

	// [should-fix] Successful totals do not prove concurrency: an implementation that starts
	// only one worker would pass every case below. Add an event-driven barrier test. Have each
	// handler increment an in-flight counter and block on a release channel; wait until exactly
	// Config.Concurrency handlers have started, release them, and track the maximum in flight.
	// Assert max == Config.Concurrency and never exceeds it. Use a timeout only as a deadlock
	// guard, not as the mechanism that makes requests overlap.
	okMockServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer okMockServer.Close()
	errorMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorMockServer.Close()

	tests := []struct {
		name            string
		cfg             Config
		want            Summary
		wantErrContains string
		wantStats       bool
	}{
		{
			name: "healthy server, concurrency < requests get method",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 5,
				Requests:    10,
				Timeout:     time.Duration(10) * time.Second,
				Method:      http.MethodGet,
			},
			want: Summary{
				Total:     10,
				Succeeded: 10,
				Failed:    0,
			},
			wantStats:       true,
			wantErrContains: "",
		},
		{
			name: "empty get method",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 1,
				Requests:    1,
				Timeout:     time.Duration(10) * time.Second,
				Method:      "",
			},
			want: Summary{
				Total:     1,
				Succeeded: 0,
				Failed:    1,
			},
			wantErrContains: "invalid method",
		},
		{
			name: "concurrency 0, requests > 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 0,
				Requests:    10,
				Method:      http.MethodGet,
			},
			wantErrContains: "invalid concurrency",
		},
		{
			name: "concurrency < 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: -5,
				Requests:    10,
				Method:      http.MethodGet,
			},
			wantErrContains: "invalid concurrency",
		},
		{
			name: "requests < 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 5,
				Requests:    -10,
				Method:      http.MethodGet,
			},
			wantErrContains: "invalid requests",
		},
		{
			name: "concurrency > 0, requests = 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 5,
				Requests:    0,
				Timeout:     time.Duration(10) * time.Second,
				Method:      http.MethodGet,
			},
			wantErrContains: "invalid requests",
		},
		{
			name: "timeout = 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 5,
				Requests:    5,
				Method:      http.MethodGet,
				Timeout:     time.Duration(0),
			},
			wantErrContains: "invalid timeout",
		},
		{
			name: "empty url",
			cfg: Config{
				URL:         "",
				Concurrency: 5,
				Requests:    5,
				Method:      http.MethodGet,
				Timeout:     time.Duration(0),
			},
			wantErrContains: "invalid url",
		},
		{
			name: "all 500s",
			cfg: Config{
				URL:         errorMockServer.URL,
				Concurrency: 5,
				Requests:    10,
				Timeout:     time.Duration(10) * time.Second,
				Method:      http.MethodGet,
			},
			want: Summary{
				Total:     10,
				Succeeded: 0,
				Failed:    10,
			},
			wantStats: false,
		},
		{
			name: "healthy server, concurrency > requests get method",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 10,
				Requests:    5,
				Timeout:     time.Duration(10) * time.Second,
				Method:      http.MethodGet,
			},
			want: Summary{
				Total:     5,
				Succeeded: 5,
				Failed:    0,
			},
			wantStats:       true,
			wantErrContains: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			got, err := Run(t.Context(), tc.cfg)
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("expected error for test %s, got response -> %+v", tc.name, got)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrContains)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error occurred when calling Run(): %v", err)
				}
				if got.Total != tc.want.Total {
					t.Errorf("got total -> %d, want total -> %d", got.Total, tc.want.Total)
				}
				if got.Succeeded != tc.want.Succeeded {
					t.Errorf("got succeeded -> %d, want succeeded -> %d", got.Succeeded, tc.want.Succeeded)
				}
				if got.Failed != tc.want.Failed {
					t.Errorf("got failed -> %d, want failed -> %d", got.Failed, tc.want.Failed)
				}
				if tc.wantStats {
					if got.Throughput <= 0.0 {
						t.Errorf("got throughput -> %f, want throughput > 0", got.Throughput)
					}
					if got.P50 <= 0.0 {
						t.Errorf("got P50 -> %d, want P50 > 0", got.P50)
					}
					if got.P90 <= 0.0 {
						t.Errorf("got P90 -> %d, want P90 > 0", got.P90)
					}
					if got.P99 <= 0.0 {
						t.Errorf("got P99 -> %d, want P99 > 0", got.P99)
					}
					if len(got.Errors) > 0 {
						t.Errorf("expected no errors, got -> %+v", got.Errors)
					}
				} else {
					if got.Throughput != 0 {
						t.Errorf("got throughput -> %f, want throughput 0", got.Throughput)
					}
					if got.P50 != 0 {
						t.Errorf("got P50 -> %d, want P50 0", got.P50)
					}
					if got.P90 != 0 {
						t.Errorf("got P90 -> %d, want P90 0", got.P90)
					}
					if got.P99 != 0 {
						t.Errorf("got P99 -> %d, want P99 0", got.P99)
					}
					if got.Errors["internal server error"] != tc.want.Failed {
						t.Errorf("got internal server error -> %d, want internal server error -> %d", got.Errors["internal server error"], tc.want.Failed)
					}
				}
			}
		})
	}
}

func TestRunCancellation(t *testing.T) {

	// [should-fix] This test checks the context error but not Run's documented partial-Summary
	// contract. Make the count deterministic with Concurrency=1, wait for its one request to
	// start, then cancel. Assert Total=1, Succeeded=0, Failed=1, the error count is one, and
	// Total < Requests. Also add an already-canceled-context case that proves zero HTTP
	// requests are started and a zero-but-initialized partial summary is returned.
	started := make(chan struct{}, 1)

	okMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-req.Context().Done()
	}))
	defer okMockServer.Close()
	cfg := Config{
		URL:         okMockServer.URL,
		Concurrency: 5,
		Requests:    10,
		Timeout:     time.Duration(1) * time.Second,
		Method:      http.MethodGet,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	type runResult struct {
		summary Summary
		err     error
	}
	finished := make(chan runResult, 1)
	go func() {
		summary, err := Run(ctx, cfg)
		finished <- runResult{summary: summary, err: err}
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Request not fired even after 500 milliseconds")
	}

	select {
	case got := <-finished:
		if got.err == nil {
			t.Fatalf("expected context cancellation error, got summary -> %+v, err -> %v", got.summary, got.err)
		}
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("expected context cancellation message, got %v", got.err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return promptly after cancellation")
	}
}
