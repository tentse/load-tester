package loadtest

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// [should-fix] This begins with Config now, but it is a run-on description with grammar
// errors and does not clearly state the validation contract. Keep the first sentence short,
// then document that URL, Method, Concurrency, Requests, and Timeout are required while
// Token and Body are optional.
// Config is the configuration of your request test such as URL, number of concurrency you want
// number of requests you want to hit your url and what should be the timeout limit, what is the method
// of the endpoint or url you are testing and all required parameter for your endpoint such as Token, Body
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

// [should-fix] This function now returns only error, so the `is...` prefix is misleading:
// Go readers expect an `is` helper to return bool. Prefer a validation-oriented name such as
// `validateConfig`, which describes both the action and the error-only result.
func isConfigValid(cfg Config) error {
	// [should-fix] This only rejects an empty URL. A malformed non-empty target still starts
	// every worker, records the same request-construction error repeatedly, and returns no
	// top-level Run error. Validate the target syntax once here so bad configuration fails fast.
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

// [should-fix] Run is exported but has no godoc comment. Document validation, the partial
// Summary returned on cancellation, and that callers should inspect the error with errors.Is
// for context.Canceled/context.DeadlineExceeded.
func Run(ctx context.Context, config Config) (Summary, error) {

	err := isConfigValid(config)
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
