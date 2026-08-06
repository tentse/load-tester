package loadtest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestRun(t *testing.T) {

	okMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okMockServer.Close()

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
				Expect:      200,
			},
			want: Summary{
				Total:     10,
				Succeeded: 10,
				Failed:    0,
			},
			wantStats: true,
		},
		{
			name: "healthy server, concurrency > requests get method",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 10,
				Requests:    5,
				Timeout:     time.Duration(10) * time.Second,
				Method:      http.MethodGet,
				Expect:      200,
			},
			want: Summary{
				Total:     5,
				Succeeded: 5,
				Failed:    0,
			},
			wantStats: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			got, err := Run(t.Context(), tc.cfg)
			if err != nil {
				t.Fatalf("unexpected error occurred when calling Run(): %v", err)
			}
			assertEqual(t, "total", got.Total, tc.want.Total)
			assertEqual(t, "succeeded", got.Succeeded, tc.want.Succeeded)
			assertEqual(t, "failed", got.Failed, tc.want.Failed)

			assertPositiveStats(t, "throughput", got.Throughput)
			assertPositiveStats(t, "P50", float64(got.P50))
			assertPositiveStats(t, "P90", float64(got.P90))
			assertPositiveStats(t, "P99", float64(got.P99))
			if len(got.Errors) > 0 {
				t.Errorf("expected no errors, got -> %+v", got.Errors)
			}

		})
	}
}

func TestServerErrors(t *testing.T) {
	errorMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorMockServer.Close()

	tests := []struct {
		name      string
		cfg       Config
		want      Summary
		wantStats bool
	}{
		{
			name: "all 500s",
			cfg: Config{
				URL:         errorMockServer.URL,
				Concurrency: 5,
				Requests:    10,
				Timeout:     time.Duration(10) * time.Second,
				Method:      http.MethodGet,
				Expect:      200,
			},
			want: Summary{
				Total:     10,
				Succeeded: 0,
				Failed:    10,
			},
			wantStats: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			got, err := Run(t.Context(), tc.cfg)
			if err != nil {
				t.Fatalf("unexpected error occurred when calling Run(): %v", err)
			}
			assertEqual(t, "total", got.Total, tc.want.Total)
			assertEqual(t, "succeeded", got.Succeeded, tc.want.Succeeded)
			assertEqual(t, "failed", got.Failed, tc.want.Failed)

			assertEqual(t, "throughput", got.Throughput, 0)
			assertEqual(t, "P50", got.P50, 0)
			assertEqual(t, "P90", got.P90, 0)
			assertEqual(t, "P99", got.P99, 0)

			if got.Errors["internal server error"] != tc.want.Failed {
				t.Errorf("got internal server error -> %d, want internal server error -> %d", got.Errors["internal server error"], tc.want.Failed)
			}

		})
	}
}

func TestInvalidConfig(t *testing.T) {

	okMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okMockServer.Close()

	tests := []struct {
		name            string
		cfg             Config
		want            Summary
		wantErrContains string
	}{
		{
			name: "empty get method",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 1,
				Requests:    1,
				Timeout:     time.Duration(10) * time.Second,
				Method:      "",
				Expect:      200,
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
				Expect:      200,
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
				Expect:      200,
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
				Expect:      200,
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
				Expect:      200,
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
				Expect:      200,
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
				Timeout:     time.Duration(1),
				Expect:      200,
			},
			wantErrContains: "invalid url",
		},
		{
			name: "invalid expect == 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 5,
				Requests:    5,
				Method:      http.MethodGet,
				Timeout:     time.Duration(1),
				Expect:      0,
			},
			wantErrContains: "invalid expect",
		},
		{
			name: "invalid expect < 0",
			cfg: Config{
				URL:         okMockServer.URL,
				Concurrency: 5,
				Requests:    5,
				Method:      http.MethodGet,
				Timeout:     time.Duration(1),
				Expect:      -1,
			},
			wantErrContains: "invalid expect",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			got, err := Run(t.Context(), tc.cfg)
			if err == nil {
				t.Fatalf("expected error for test %s, got response -> %+v", tc.name, got)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error = %v, want ErrInvalidConfig", err)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrContains)
			}

		})
	}
}

func TestRunCancellation(t *testing.T) {

	started := make(chan struct{}, 1)

	okMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-req.Context().Done()
		w.WriteHeader(http.StatusOK)
	}))
	defer okMockServer.Close()
	cfg := Config{
		URL:         okMockServer.URL,
		Concurrency: 5,
		Requests:    10,
		Timeout:     time.Duration(1) * time.Second,
		Method:      http.MethodGet,
		Expect:      200,
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

func TestRunClosesIdleConnections(t *testing.T) {
	idle := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateIdle:
			select {
			case idle <- struct{}{}:
			default:
			}
		case http.StateClosed:
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}

	server.Start()
	defer server.Close()

	_, err := Run(t.Context(), Config{
		URL:         server.URL,
		Concurrency: 1,
		Requests:    1,
		Timeout:     defaultTimeout,
		Method:      http.MethodGet,
		Expect:      200,
	})
	if err != nil {
		t.Fatalf("got err = %q, want = nil", err.Error())
	}

	select {
	case <-idle:
	case <-time.After(defaultTimeout):
		t.Fatal("connection never became idle")
	}
	select {
	case <-closed:
	case <-time.After(defaultTimeout):
		t.Fatal("run returned without closing the idle connection")
	}
}

func TestWorkersUpdatingLatencyHistogramValue(t *testing.T) {
	wantedTotal := 1000
	lh := latencyHistogram{}

	var wg sync.WaitGroup
	wg.Add(wantedTotal)

	for i := 1; i <= wantedTotal; i++ {
		go func() {
			defer wg.Done()
			lh.observe(10 * time.Millisecond)
		}()
	}
	wg.Wait()

	if lh.total != int64(wantedTotal) {
		t.Errorf("got total count: %d, want total count: %d", lh.total, wantedTotal)
	}
}

func TestWorkersUpdatingStatusTrackerValue(t *testing.T) {
	wantedSucceededCount := 1000
	statusTracker := statusTracker{}

	var wg sync.WaitGroup
	wg.Add(wantedSucceededCount)

	for i := 1; i <= wantedSucceededCount; i++ {
		go func() {
			defer wg.Done()
			statusTracker.IncSucceeded()
		}()
	}

	wg.Wait()

	if statusTracker.Succeeded != wantedSucceededCount {
		t.Errorf("got succeeded count: %d, want: %d", statusTracker.Succeeded, wantedSucceededCount)
	}
}

func TestBucketIndex(t *testing.T) {
	tests := []struct {
		time time.Duration
		want int
	}{
		{
			time: 200 * time.Microsecond,
			want: 0,
		},
		{
			time: 10 * time.Millisecond,
			want: 4,
		},
		{
			time: 49 * time.Millisecond,
			want: 5,
		},
		{
			time: 12 * time.Second,
			want: 13,
		},
	}

	for _, tc := range tests {
		got := bucketIndex(tc.time)
		if got != tc.want {
			t.Errorf("got index: %d, want index: %d", got, tc.want)
		}
	}
}

func TestLatencyHistogram(t *testing.T) {

	tests := []struct {
		name      string
		latencies []time.Duration
		want      latencyHistogram
	}{
		{
			name: "mix latencies",
			latencies: []time.Duration{
				200 * time.Microsecond,
				10 * time.Millisecond,
				49 * time.Millisecond,
				12 * time.Second,
			},
			want: latencyHistogram{
				counts: [...]int64{
					1, // -Inf <= t < 1ms
					0, // 1ms <= t < 2ms
					0, // 2ms <= t < 5ms
					0, // 5ms <= t < 10ms
					1, // 10ms <= t < 20ms
					1, // 20ms <= t < 50ms
					0, // 50ms <= t < 100ms
					0, // 100ms <= t < 200ms
					0, // 200ms <= t < 500ms
					0, // 500ms <= t < 1s
					0, // 1s <= t < 2s
					0, // 2s <= t < 5s
					0, // 5s <= t < 10s
					1, // 10s <= t < +Inf
				},
				min:   200 * time.Microsecond,
				max:   12 * time.Second,
				total: 4,
			},
		},
		{
			name: "boundary latencies",
			latencies: []time.Duration{
				1 * time.Millisecond,
				10 * time.Second,
			},
			want: latencyHistogram{
				counts: [...]int64{
					0, // -Inf <= t < 1ms
					1, // 1ms <= t < 2ms
					0, // 2ms <= t < 5ms
					0, // 5ms <= t < 10ms
					0, // 10ms <= t < 20ms
					0, // 20ms <= t < 50ms
					0, // 50ms <= t < 100ms
					0, // 100ms <= t < 200ms
					0, // 200ms <= t < 500ms
					0, // 500ms <= t < 1s
					0, // 1s <= t < 2s
					0, // 2s <= t < 5s
					0, // 5s <= t < 10s
					1, // 10s <= t < +Inf
				},
				min:   1 * time.Millisecond,
				max:   10 * time.Second,
				total: 2,
			},
		},
	}

	for i := range tests {
		tc := &tests[i]
		t.Run(tc.name, func(t *testing.T) {

			got := &latencyHistogram{}

			for _, value := range tc.latencies {
				got.observe(value)
			}

			want := &tc.want
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got latency histogram: %+v, want latency histogram: %+v", got, &tc.want)
			}
		})
	}
}

func TestStatusMatchesExpectStatus(t *testing.T) {
	mock200Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mock200Server.Close()
	mock500Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mock500Server.Close()

	tests := []struct {
		name string
		cfg  Config
		want Summary
	}{
		{
			name: "expected status 200 calling 200 status server",
			cfg: Config{
				URL:         mock200Server.URL,
				Timeout:     1 * time.Second,
				Concurrency: 1,
				Requests:    1,
				Method:      http.MethodGet,
				Expect:      200,
			},
			want: Summary{
				Total:     1,
				Succeeded: 1,
				Failed:    0,
			},
		},
		{
			name: "expected status 500 calling 500 status server",
			cfg: Config{
				URL:         mock500Server.URL,
				Concurrency: 1,
				Timeout:     1 * time.Second,
				Requests:    1,
				Method:      http.MethodGet,
				Expect:      500,
			},
			want: Summary{
				Total:     1,
				Succeeded: 1,
				Failed:    0,
			},
		},
		{
			name: "expected status 200 calling non 200 status server",
			cfg: Config{
				URL:         mock500Server.URL,
				Timeout:     1 * time.Second,
				Concurrency: 1,
				Requests:    1,
				Method:      http.MethodGet,
				Expect:      200,
			},
			want: Summary{
				Total:     1,
				Succeeded: 0,
				Failed:    1,
			},
		},
		{
			name: "expected status 500 calling non 500 status server",
			cfg: Config{
				URL:         mock200Server.URL,
				Timeout:     1 * time.Second,
				Concurrency: 1,
				Requests:    1,
				Method:      http.MethodGet,
				Expect:      500,
			},
			want: Summary{
				Total:     1,
				Succeeded: 0,
				Failed:    1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			got, err := Run(t.Context(), tc.cfg)
			if err != nil {
				t.Fatalf("unexpected error occurred when calling Run(): %v", err)
			}
			assertEqual(t, "total", got.Total, tc.want.Total)
			assertEqual(t, "succeeded", got.Succeeded, tc.want.Succeeded)
			assertEqual(t, "failed", got.Failed, tc.want.Failed)
		})
	}
}
