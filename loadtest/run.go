package loadtest

import (
	"context"
	"fmt"
	"sync"
	"time"
)

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

func isConfigValid(cfg *Config) (bool, error) {
	if cfg.Concurrency <= 0 {
		return false, fmt.Errorf("invalid concurrency value received -> %d", cfg.Concurrency)
	}
	if cfg.Requests <= 0 {
		return false, fmt.Errorf("invalid requests value received -> %d", cfg.Requests)
	}
	if cfg.Timeout <= 0 {
		return false, fmt.Errorf("invalid timeout value received -> %d", cfg.Timeout)
	}
	return true, nil
}

func Run(ctx context.Context, config Config) (Summary, error) {

	isValidConfig, err := isConfigValid(&config)
	if !isValidConfig {
		return Summary{}, err
	}

	jobs := make(chan struct{})
	results := make(chan result)
	r := newRunner(config.Timeout)

	elapsedStart := time.Now()

	go func() {
		defer close(jobs)
		for i := 1; i <= config.Requests; i++ {
			jobs <- struct{}{}
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

	return summarize(collectedResult, time.Since(elapsedStart)), nil
}
