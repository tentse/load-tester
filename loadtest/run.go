package loadtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Config defines one closed-loop HTTP load test.
//
// URL and Method must be non-empty. Concurrency, Requests, and Timeout must be
// greater than zero. Timeout covers the complete request, including reading the
// response body. Token and Body are optional; a token is sent as a bearer token,
// and a non-empty body is sent as JSON.
type Config struct {
	URL         string
	Concurrency int
	Requests    int
	Timeout     time.Duration
	Method      string
	Token       string
	Body        string
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

func (r *runner) worker(ctx context.Context, wg *sync.WaitGroup, cfg Config, jobs <-chan struct{}, latencies chan<- time.Duration, statusTracker *statusTracker) {
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
			status, err := r.hit(ctx, cfg.Method, cfg.URL, cfg.Token, cfg.Body)
			statusTracker.IncTotal()
			if err != nil || isServerError(status) {
				statusTracker.IncFailed()
				statusTracker.UpdateErrors(status, err)
			} else {
				statusTracker.IncSucceeded()
				latencies <- time.Since(start)
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
			r.worker(ctx, &wg, config, jobs, latencies, &statusTracker)
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

	return summarize(collectedLatencies, time.Since(elapsedStart), statusTracker.Total, statusTracker.Succeeded, statusTracker.Failed, statusTracker.Errors), ctx.Err()
}
