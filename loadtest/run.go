package loadtest

import (
	"context"
	"fmt"
	"sync"
	"time"
)

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
			// [should-fix] Direct time.Now/time.Since calls prevent a deterministic test of the
			// most important metric: latency must include the entire body read. Inject a small
			// clock/timing seam so a test can advance time around hit without sleeping; otherwise
			// the current `P50 > 0` assertions would still pass if the timing boundaries moved.
			start := time.Now()
			status, err := r.hit(ctx, cfg.Method, cfg.URL, cfg.Token, cfg.Body)
			results <- result{latency: time.Since(start), status: status, err: err}
		}
	}
}

func validateConfig(cfg Config) error {
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

func Run(ctx context.Context, config Config) (Summary, error) {

	err := validateConfig(config)
	if err != nil {
		return Summary{}, err
	}

	jobs := make(chan struct{})
	results := make(chan result)
	r := newRunner(config.Timeout)
	// [should-fix] Each Run owns a freshly cloned Transport. Once all workers finish, explicitly
	// close its idle connections before returning; otherwise repeated library calls can leave
	// many idle sockets and transport goroutines around until IdleConnTimeout expires. This is
	// separate from closing response bodies: body closure makes a connection reusable, while
	// CloseIdleConnections releases the per-run pool when the run is over.

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
