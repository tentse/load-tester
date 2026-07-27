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

// [should-fix] (FEEDBACK.md #4, still open) Only an *empty* URL is rejected. "htpp://localhost",
// "nope", and "http://" with no host all sail through validation, and then every single worker
// fails its request identically — one config typo becomes N runtime errors and burns the whole
// run before the user finds out. Validate at the boundary and fail fast: a knowable-up-front
// error should never be deferred into the hot loop. net/url is the tool, but url.Parse alone is
// far too lenient (it happily accepts "nope"), so parse *and then* assert the parts you require:
// scheme is http or https, Host is non-empty.
//
// [should-fix] These errors carry no identity — they're strings and nothing more. That's why
// cmd/loadtester can't tell "the user's flags were wrong" (exit 2) from "the run failed"
// (exit 1) without grepping the message. Give the category a value: declare a package-level
// `var ErrInvalidConfig = errors.New("invalid config")` and wrap it with %w here, so callers
// can errors.Is against it. This is the single highest-value idiom on this page — it's what
// turns an error string into an API.
func validateConfig(cfg Config) error {
	if cfg.URL == "" {
		// [nit] (FEEDBACK.md #11) %q rather than %v for string values: it quotes and reveals
		// whitespace, so an empty URL and a URL of three spaces stop rendering identically in
		// the message. Applies to the Method case just below too.
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
	// [should-fix] Nothing ever releases this client's idle connections. You set
	// MaxIdleConnsPerHost = 100, so a finished run against a real server leaves up to 100
	// sockets open — each with its own read and write goroutine — until Transport's
	// IdleConnTimeout (90s by default) reaps them. Your tests don't catch this and goleak
	// stays quiet because httptest.Server.Close() tears the connections down from the *server*
	// side; a production target won't do you that favour. For the CLI it's harmless (the
	// process exits), but Run is a library entry point, and a library that still holds file
	// descriptors after it returns is leaking. `defer r.client.CloseIdleConnections()` is the
	// whole fix. Principle: whoever creates a resource owns releasing it.
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
	// [should-fix] (FEEDBACK.md #5, still open) You start Concurrency workers even when
	// Requests is smaller; the surplus ones wake up, read the already-closed jobs channel, and
	// exit having done nothing. Correct, but a load tester is precisely the tool that invites
	// people to type a big -c. STRESS_TEST_REPORT.md §4 measured C=500000/N=5 spending 152ms on
	// pure goroutine creation versus 1.6ms at C=5 — 100x wall clock to create workers that
	// can't get work. One line of arithmetic before the loop fixes it: the number that can ever
	// be busy is min(Concurrency, Requests), and min is a builtin since Go 1.21.
	//
	// [nit] The closure buys you nothing here — `go r.worker(ctx, &wg, config, jobs, results)`
	// says exactly the same thing with less ceremony. (A closure would matter if you were
	// capturing a loop variable that needed pinning, but you aren't, and since Go 1.22 the
	// per-iteration variable makes that hazard history anyway.)
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

	// [should-fix] (FEEDBACK.md #2, still open) Every result for the entire run is buffered
	// here and folded exactly once at the end, so memory is O(Requests) in the one tool people
	// deliberately point at enormous request counts. STRESS_TEST_REPORT.md §3 measured ~117 MiB
	// at 1M requests and ~227 MiB at 2M, straight-line. Nothing downstream needs a per-request
	// result once it's been folded in: Summary needs counts, an error map, and the latencies of
	// *successful* requests, and that's all. Fold each result as it arrives and you go from one
	// result struct per request to one time.Duration per success. The principle is general —
	// for a streaming workload, aggregate as data arrives; don't buffer the whole stream just
	// to reduce it once at the end. (Truly flat memory means a bucketed histogram and therefore
	// approximate percentiles. That's a real trade — make it on purpose, and write down which
	// side you picked and why.)
	var collectedResult []result
	for res := range results {
		collectedResult = append(collectedResult, res)
	}

	// [nit] (FEEDBACK.md #12) ctx.Err() is returned unconditionally. The edge case: if the
	// caller's context is canceled in the sliver of time after the last result arrives but
	// before Run returns, the caller receives a complete, correct Summary *and*
	// context.Canceled — which reads as "the run was interrupted" when in fact it finished.
	// Worth deciding deliberately whether a fully-drained run should ever report cancellation.
	return summarize(collectedResult, time.Since(elapsedStart)), ctx.Err()
}
