package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config defines one closed-loop HTTP load test.
//
// URL and Method must be non-empty. Concurrency, Requests, Timeout, and Expect
// must be greater than zero. Timeout covers the complete request, including
// reading the response body. Expect is the HTTP status code that counts as a
// success; any other status is a failure, and it has no default. Headers and Body
// are optional. Headers are sent as given, with repeated values preserved in
// order; a non-empty Body sets a JSON content type unless Headers already carries
// one.
type Config struct {
	URL         string
	Concurrency int
	Requests    int
	Timeout     time.Duration
	Method      string
	Headers     http.Header
	Body        string
	Expect      int
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

func (s *statusTracker) UpdateErrors(status, expect int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.Errors[classifyFailure(err)]++
	} else if status != expect {
		s.Errors[statusErrText(status)]++
	}
}

func classifyFailure(err error) string {
	var networkErr net.Error

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "request timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected EOF"
	case errors.As(err, &networkErr) && networkErr.Timeout():
		return "request timeout"
	default:
		return "request failed"
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
			if err != nil || status != cfg.Expect {
				statusTracker.IncFailed()
				statusTracker.UpdateErrors(status, cfg.Expect, err)
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
	if cfg.Expect <= 0 {
		return fmt.Errorf("%w: invalid expect -> %v", ErrInvalidConfig, cfg.Expect)
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

	r := newRunner(config.Timeout, config.Concurrency)
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

	wg.Wait()

	summary := summarize(&lh, time.Since(elapsedStart), &statusTracker)
	summary.Buckets = buckets(&lh)

	return summary, ctx.Err()
}

// RequestSpec is one endpoint of a multi-endpoint load test.
//
// Name must be non-empty and is the grouping key. Every spec sharing a Name is reported under a
// single Summary, so one endpoint can appear more than once with different bodies or headers and
// still be measured as one thing. Method must be non-empty. URL is joined to FileConfig.BaseURL
// with exactly one slash between them, so a spec can carry only a path when a base is set, and
// neither side has to be careful about its own leading or trailing slash.
//
// Count must be greater than zero and is how many times to fire this spec. Every spec is issued
// in full before the next one starts, so today Count sets how long that endpoint runs for rather
// than how densely it appears in a mixed stream. Expect must be greater than zero and is the HTTP
// status code that counts as a success for this spec, matched exactly, in the same way
// Config.Expect is for a single target run. Header and Body are optional and are sent as given.
type RequestSpec struct {
	Name   string
	URL    string
	Method string
	Header http.Header
	Body   string
	Count  int
	Expect int
}

// FileConfig defines one load test spread across several endpoints.
//
// Version is the format version of the file the config was read from and must be 1. It is checked
// so that a later format is rejected with a clear message rather than silently misread.
//
// Concurrency and Timeout must be greater than zero. Concurrency is the total number of workers,
// shared by every entry in Requests rather than given to each one, so adding an endpoint spreads
// the same pool wider instead of adding load. Timeout applies to each request on its own, not to
// the run as a whole.
//
// Requests holds the endpoints to exercise. BaseURL is optional and is prefixed to every
// RequestSpec.URL; either BaseURL or each spec's own URL must be non-empty, so entries can carry
// a path instead of repeating a host.
type FileConfig struct {
	Version     int
	BaseURL     string
	Concurrency int
	Timeout     time.Duration
	Requests    []RequestSpec
}

type aggregator struct {
	lh *latencyHistogram
	st *statusTracker
}

func (r *runner) fileWorker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan RequestSpec, aggs map[string]*aggregator) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case request, ok := <-jobs:
			if !ok {
				return
			}
			start := time.Now()
			status, err := r.hit(ctx, request.Method, request.URL, request.Body, request.Header)

			agg, ok := aggs[request.Name]

			agg.st.IncTotal()
			if err != nil || status != request.Expect {
				agg.st.IncFailed()
				agg.st.UpdateErrors(status, request.Expect, err)
			} else {
				agg.st.IncSucceeded()
				latency := time.Since(start)
				agg.lh.observe(latency)
			}
		}

	}
}

func validateFileConfig(cfg FileConfig) error {

	if cfg.Version != 1 {
		return fmt.Errorf("%w: version not supported -> %d", ErrInvalidConfig, cfg.Version)
	}
	if cfg.Concurrency <= 0 {
		return fmt.Errorf("%w: invalid concurrency -> %d", ErrInvalidConfig, cfg.Concurrency)
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("%w: invalid timeout -> %v", ErrInvalidConfig, cfg.Timeout)
	}

	for index, request := range cfg.Requests {
		if request.Name == "" {
			return fmt.Errorf("%w: request %d: empty name", ErrInvalidConfig, index)
		}
		if cfg.BaseURL == "" && request.URL == "" {
			return fmt.Errorf("%w: request %d: empty baseURL and empty URL", ErrInvalidConfig, index)
		}
		if request.Method == "" {
			return fmt.Errorf("%w: request %d: invalid method", ErrInvalidConfig, index)
		}
		if request.Count <= 0 {
			return fmt.Errorf("%w: request %d: invalid count -> %d", ErrInvalidConfig, index, request.Count)
		}
		if request.Expect <= 0 {
			return fmt.Errorf("%w: request %d: invalid expect -> %d", ErrInvalidConfig, index, request.Expect)
		}
	}
	return nil
}

func combineURL(baseURL, url string) string {
	if baseURL == "" {
		return url
	}
	if url == "" {
		return baseURL
	}

	return strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(url, "/")
}

// FileRun executes a load test across several endpoints and reports one Summary per
// RequestSpec.Name.
//
// Every Summary reports the same Elapsed, the wall-clock duration of the whole run, because one
// worker pool is shared by every endpoint. A name's Throughput is therefore its share of the
// overall request rate rather than a rate that endpoint could sustain on its own, and the
// per-name figures sum to the run total. Judge an individual endpoint by its percentiles and
// Buckets instead.
//
// Each spec's URL is joined to cfg.BaseURL with exactly one slash between them, however the two
// were written. Specs are issued in the order they appear in cfg.Requests,
// each one's Count in full before the next begins, so a run walks the endpoints in sequence
// rather than interleaving them.
//
// FileRun returns an empty map and an error when cfg fails validation. Individual HTTP request
// failures are recorded in the Summary they belong to rather than returned as the FileRun error,
// as in Run. If ctx is canceled, FileRun stops scheduling work, waits for in-flight workers to
// exit, and returns the partial summaries together with ctx.Err().
func FileRun(ctx context.Context, cfg FileConfig) (map[string]Summary, error) {

	err := validateFileConfig(cfg)
	if err != nil {
		return map[string]Summary{}, err
	}

	jobs := make(chan RequestSpec)
	aggs := map[string]*aggregator{}

	for _, request := range cfg.Requests {
		if _, ok := aggs[request.Name]; !ok {
			aggs[request.Name] = &aggregator{
				lh: &latencyHistogram{},
				st: &statusTracker{Errors: map[string]int{}},
			}
		}
	}

	elapsedStart := time.Now()

	go func() {
		defer close(jobs)
		for _, request := range cfg.Requests {
			request.URL = combineURL(cfg.BaseURL, request.URL)
			for i := 1; i <= request.Count; i++ {
				select {
				case <-ctx.Done():
					return
				case jobs <- request:
				}
			}
		}
	}()

	r := newRunner(cfg.Timeout, cfg.Concurrency)
	defer r.client.CloseIdleConnections()

	var wg sync.WaitGroup
	for range cfg.Concurrency {
		wg.Add(1)
		go r.fileWorker(ctx, &wg, jobs, aggs)
	}

	wg.Wait()

	elapsed := time.Since(elapsedStart)

	summaries := make(map[string]Summary)
	for name, agg := range aggs {
		summary := summarize(agg.lh, elapsed, agg.st)
		summary.Buckets = buckets(agg.lh)
		summaries[name] = summary
	}
	return summaries, ctx.Err()
}
