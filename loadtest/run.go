package loadtest

import (
	"context"
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

func (r *runner) worker(ctx context.Context, wg *sync.WaitGroup, cfg Config, jobs <-chan struct{}, results chan<- result) {
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
			results <- result{latency: time.Since(start), status: status, err: err}
		}
	}
}

func validateConfig(cfg Config) error {
	if cfg.URL == "" {
		return fmt.Errorf("invalid url -> %v", cfg.URL)
	}
	if cfg.Method == "" {
		return fmt.Errorf("invalid method -> %v", cfg.Method)
	}
	if cfg.Concurrency <= 0 {
		return fmt.Errorf("invalid concurrency -> %d", cfg.Concurrency)
	}
	if cfg.Requests <= 0 {
		return fmt.Errorf("invalid requests -> %d", cfg.Requests)
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("invalid timeout -> %v", cfg.Timeout)
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
	results := make(chan result)
	r := newRunner(config.Timeout)

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

	var wg sync.WaitGroup
	for i := 1; i <= config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			r.worker(ctx, &wg, config, jobs, results)
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var collectedResult []result
	for res := range results {
		collectedResult = append(collectedResult, res)
	}

	return summarize(collectedResult, time.Since(elapsedStart)), ctx.Err()
}
