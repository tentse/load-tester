package loadtest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRun(t *testing.T) {

	okMockServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer okMockServer.Close()
	errorMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorMockServer.Close()

	tests := []struct {
		name      string
		cfg       Config
		want      Summary
		err       bool
		wantStats bool
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
			wantStats: true,
			err:       false,
		},
		{
			name: "concurrency 0, requests > 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 0,
				Requests:    10,
				Method:      http.MethodGet,
			},
			err: true,
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
			err: true,
		},
		{
			name: "concurrency = 0, requests = 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 0,
				Requests:    0,
				Timeout:     time.Duration(10) * time.Second,
				Method:      http.MethodGet,
			},
			err: true,
		},
		{
			name: "timout = 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 5,
				Requests:    5,
				Method:      http.MethodGet,
				Timeout:     time.Duration(0),
			},
			err: true,
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
			err:       false,
		},
	}

	for _, tc := range tests {
		got, err := Run(t.Context(), tc.cfg)
		if tc.err == true {
			if err == nil {
				t.Errorf("expected error for test %s, got response -> %+v", tc.name, got)
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
					t.Errorf("go throughput -> %f, want throughput > 0", got.Throughput)
				}
				if got.P50 <= 0.0 {
					t.Errorf("go P50 -> %d, want P50 > 0", got.P50)
				}
				if got.P90 <= 0.0 {
					t.Errorf("go P90 -> %d, want P90 > 0", got.P90)
				}
				if got.P99 <= 0.0 {
					t.Errorf("go P99 -> %d, want P99 > 0", got.P99)
				}
				if len(got.Errors) > 0 {
					t.Errorf("expected no errors, got -> %+v", got.Errors)
				}
			}
		}
	}
}

// [should-fix] Make the happy path table-driven -- it's the house style (see summary_test.go
// and apiCall_test.go, and CLAUDE.md §4). The count-based variants fit one table cleanly:
// different Concurrency/Requests/Method, and an all-500 target (Failed == Requests,
// Succeeded == 0, percentiles 0). Keep the cancellation test below as its own function --
// its shape (slow handler + cancel mid-flight) doesn't fit the same row.
//
// [should-fix] The headline test is missing: context cancellation of an in-flight run. This
// is the single most important test in the project (NEXT_STEPS.md §1.7-4) -- for a load
// tester, Ctrl+C or a timeout MUST abort live requests. Add a separate TestRunCancellation:
// a slow httptest handler (select { case <-time.After(...): case <-r.Context().Done(): },
// the same pattern as apiCall_test.go:152-199), a ctx you cancel after ~20ms, then assert
// Run returns FAST (well under the time to finish all requests) and that the error satisfies
// errors.Is(err, context.Canceled) (or context.DeadlineExceeded).
//
// [should-fix] Reminder: run `go test -race ./...`. Once Run drives many goroutines through
// the shared http.Client, the race detector is the only thing that catches unsynchronized
// access to a shared map/slice or a bad channel close -- a green run without -race proves
// nothing about concurrency. Also add a goleak TestMain (goleak.VerifyTestMain(m)) to catch
// a leaked worker or producer; it's the one sanctioned test-only third-party dependency.
