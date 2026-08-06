package loadtest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Config defines one closed-loop HTTP load test.
//
// URL and Method must be non-empty. Concurrency, Requests, and Timeout must be
// greater than zero. Timeout covers the complete request, including reading the
// response body. Headers and Body are optional. Headers are sent as given, with
// repeated values preserved in order; a non-empty Body sets a JSON content type
// unless Headers already carries one.
type Config struct {
	URL         string
	Concurrency int
	Requests    int
	Timeout     time.Duration
	Method      string
	Headers     http.Header
	Body        string
}

// bucketEdges are the exclusive upper bounds edges of the latency display range
// bucket i covers [bucketEdges[i-1], bucketEdges[i])
// bucket 0 covers [0, bucketEdges[0]) and final bucket covers [last, +Inf)
var bucketEdges = [...]time.Duration{
	1 * time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
}

type latencyHistogram struct {
	counts   [len(bucketEdges) + 1]int64
	min, max time.Duration
	total    int64

	mu sync.Mutex
}

func (lh *latencyHistogram) observe(d time.Duration) {
	lh.mu.Lock()
	defer lh.mu.Unlock()

	lh.counts[bucketIndex(d)]++
	lh.total++
	if lh.total == 1 || d < lh.min {
		lh.min = d
	}
	if d > lh.max {
		lh.max = d
	}
}

func bucketIndex(time time.Duration) int {
	for index, value := range bucketEdges {
		if time < value {
			return index
		}
	}
	return 13
}

type statusTracker struct {
	Total     int
	Succeeded int
	Failed    int
	Errors    map[string]int

	mu sync.Mutex
}

func (s *statusTracker) IncTotal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Total++
}

func (s *statusTracker) IncSucceeded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Succeeded++
}

func (s *statusTracker) IncFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Failed++
}

func (s *statusTracker) UpdateErrors(status int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.Errors[classifyFailure(err)]++
	} else if isServerError(status) {
		s.Errors[statusErrText(status)]++
	}
}

func (r *runner) worker(ctx context.Context, wg *sync.WaitGroup, cfg Config, jobs <-chan struct{}, lh *latencyHistogram, statusTracker *statusTracker) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-jobs:
			if !ok {
				return
			}
			start := time.Now()
			status, err := r.hit(ctx, cfg.Method, cfg.URL, cfg.Body, cfg.Headers)
			statusTracker.IncTotal()
			if err != nil || isServerError(status) {
				statusTracker.IncFailed()
				statusTracker.UpdateErrors(status, err)
			} else {
				statusTracker.IncSucceeded()
				latency := time.Since(start)
				lh.observe(latency)
			}
		}
	}
}

// ErrInvalidConfig indicates that a Config failed validation.
var ErrInvalidConfig = errors.New("invalid config")

func validateConfig(cfg Config) error {

	if cfg.URL == "" {
		return fmt.Errorf("%w: invalid url -> %v", ErrInvalidConfig, cfg.URL)
	}
	if cfg.Method == "" {
		return fmt.Errorf("%w: invalid method -> %v", ErrInvalidConfig, cfg.Method)
	}
	if cfg.Concurrency <= 0 {
		return fmt.Errorf("%w: invalid concurrency -> %d", ErrInvalidConfig, cfg.Concurrency)
	}
	if cfg.Requests <= 0 {
		return fmt.Errorf("%w: invalid requests -> %d", ErrInvalidConfig, cfg.Requests)
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("%w: invalid timeout -> %v", ErrInvalidConfig, cfg.Timeout)
	}
	return nil
}

// Run executes a closed-loop load test using the supplied configuration.
//
// Run returns a zero Summary and an error when config fails validation.
// Individual HTTP request failures are recorded in Summary rather than returned
// as the Run error. If ctx is canceled, Run stops scheduling work, waits for
// in-flight workers to exit, and returns the partial Summary together with
// ctx.Err().
func Run(ctx context.Context, config Config) (Summary, error) {

	err := validateConfig(config)
	if err != nil {
		return Summary{}, err
	}

	jobs := make(chan struct{})
	lh := latencyHistogram{}
	latencies := make(chan time.Duration)

	r := newRunner(config.Timeout)
	defer r.client.CloseIdleConnections()

	elapsedStart := time.Now()

	go func() {
		defer close(jobs)
		for range config.Requests {
			select {
			case <-ctx.Done():
				return
			case jobs <- struct{}{}:
			}
		}
	}()

	statusTracker := statusTracker{
		Errors: map[string]int{},
	}

	var wg sync.WaitGroup
	for i := 1; i <= config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			r.worker(ctx, &wg, config, jobs, &lh, &statusTracker)
		}()
	}

	go func() {
		wg.Wait()
		close(latencies)
	}()

	var collectedLatencies []time.Duration
	for res := range latencies {
		collectedLatencies = append(collectedLatencies, res)
	}

	return summarize(&lh, time.Since(elapsedStart), statusTracker.Total, statusTracker.Succeeded, statusTracker.Failed, statusTracker.Errors), ctx.Err()
}
