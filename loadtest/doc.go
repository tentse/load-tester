// Package loadtest generates HTTP load and reports throughput, latency percentiles,
// and a breakdown of failures.
//
// There are two ways in. A [Config] executed by [Run] drives a single URL and returns
// one [Summary]. A [FileConfig] executed by [FileRun] drives several endpoints in one
// pass and returns one Summary per endpoint name. Both use the same engine, the same
// worker pool, and the same reporting.
//
// The engine is closed loop: a fixed number of requests is sent in total, spread
// across Concurrency workers, and every worker waits for its response before taking
// the next request. There is no target request rate, and no way to run for a fixed
// duration: a run ends once every request has been sent, or when ctx is canceled.
//
// Authentication is expressed through headers rather than a dedicated field, so any
// scheme works: a bearer token, an API key under whatever name the target expects, or
// several at once. Query parameters must be included in the URL itself.
//
// Config has no defaults. Every field it validates must be set explicitly:
//
//	URL          must be non-empty
//	Method       must be non-empty
//	Concurrency  must be greater than zero
//	Requests     must be greater than zero
//	Timeout      must be greater than zero
//	Expect       must be greater than zero
//
// Headers and Body are the only optional fields. The loadtester command supplies
// its own defaults for the rest before calling Run; the library does not.
//
// Expect is the HTTP status code that counts as a success, and it has no default:
// a Config that leaves it at zero fails validation rather than falling back to a
// range. Requiring it is deliberate. A load test that does not check what came
// back reports a healthy run against the wrong endpoint just as happily as
// against the right one, and a wall of 404s is indistinguishable from a wall of
// 200s. Because the comparison is an exact match rather than a range, an error
// path can be load tested on purpose by setting Expect to 500.
//
// Timeout applies to each request on its own, not to the run as a whole. It
// covers the complete round trip, including reading the response body.
//
// The shared HTTP client keeps an idle connection pool the same size as Concurrency,
// so a worker holds on to its connection between requests instead of opening a fresh
// one each time. A smaller pool would leave every worker past the limit closing and
// reopening a connection per request, which spends a local port each time until the
// machine runs out of them and the run begins failing for reasons that have nothing
// to do with the target.
//
// A request succeeds when it completes without a transport error and its HTTP
// status is exactly the expected status. Every other status is a failure, as is any
// request that never completes. Summary.Throughput counts successful requests per
// second, and Summary.P50, P90, and P99 are latency percentiles over successful
// requests only. Latencies are counted into a fixed bucket ladder rather than
// retained individually, so each percentile is the upper bound of the bucket it
// falls into and can overstate the true latency. Each latency covers the complete
// request, including reading the response body. Redirects are followed using the default net/http
// policy, so the status recorded is the one at the end of the redirect chain.
//
// Run returns an error only for an invalid Config or a canceled context. Failed
// requests are not errors: they are counted in Summary.Failed and described in
// Summary.Errors, so a run in which every request failed still returns a nil
// error. Check the Summary, not just the error, to judge how the target held up.
//
// Run honors context cancellation. When ctx is canceled, Run stops scheduling
// work, waits for any in flight requests to finish, and returns the partial Summary
// together with ctx.Err(). A Config that fails validation produces a zero
// Summary and an error wrapping [ErrInvalidConfig], before any request is sent.
//
// # Several endpoints in one run
//
// [FileRun] takes a [FileConfig], which carries the settings that apply to the whole
// run — Version, Concurrency, Timeout, and an optional BaseURL — together with a list
// of [RequestSpec] values describing the endpoints. FileRun follows the same rules as
// Run for validation, failed requests, and cancellation, and returns the summaries
// gathered so far when ctx is canceled.
//
// Concurrency is the total number of workers, shared by every endpoint rather than
// given to each, so adding an endpoint spreads the same pool wider instead of adding
// load. Timeout still applies to each request on its own.
//
// Results are grouped by RequestSpec.Name, one Summary per name. Specs sharing a Name
// are merged on purpose, so one endpoint can appear more than once with different
// bodies or headers and still be measured as a single thing.
//
// RequestSpec.URL is joined to FileConfig.BaseURL with exactly one slash between them,
// so the base and the path may each carry a trailing or leading slash, or neither,
// without producing a double slash or a run-together URL. An empty BaseURL leaves the
// spec's URL untouched, which is how a spec carries a whole URL of its own.
//
// Two things about the report are worth knowing before reading it. Every Summary
// carries the same Elapsed, the wall-clock duration of the whole run, so a name's
// Throughput is its share of the overall request rate rather than a rate that endpoint
// could sustain alone. And the specs are issued in the order they appear in
// FileConfig.Requests, each one's Count in full before the next begins, so a run walks
// the endpoints in sequence rather than mixing them together. Judge an individual
// endpoint by its percentiles and Buckets, which are its own.
//
// # Load, memory, and the bucket ladder
//
// This package generates real load. Only point it at systems you own or have
// explicit permission to test.
//
// Memory does not scale with the number of requests. Each successful latency is
// counted into one of a fixed set of buckets as it arrives and the timing itself is
// discarded, so a run of ten million requests costs the same as a run of ten.
//
// The buckets are half-open, so [1ms, 2ms) includes exactly 1ms and excludes
// 2ms, and every latency lands in exactly one of them:
//
//	<1ms      1–2ms     2–5ms      5–10ms     10–20ms
//	20–50ms   50–100ms  100–200ms  200–500ms  500ms–1s
//	1–2s      2–5s      5–10s      ≥10s
//
// They are multiplicative rather than evenly spaced, each roughly 2 to 2.5 times
// the width of the last, because latency is skewed: evenly spaced buckets would
// place nearly every request in the first one and spend the rest on an empty tail.
//
// A percentile is read by walking the buckets from fastest to slowest,
// accumulating counts until the target rank is reached, and reporting that
// bucket's upper bound. Summary.P50, P90, and P99 are therefore always one of the
// bounds listed above, except when the percentile falls in the final open-ended
// bucket, where the largest observed latency is reported instead. Memory is
// constant, but a percentile is only known to the width of the bucket it lands in.
//
// The counts themselves are reported in Summary.Buckets, one [Bucket] per step in
// the order listed above, so you can see how the latencies were spread out
// instead of only three numbers taken from them. [Bucket.Label] writes a bucket's
// name exactly as it is listed above. The counts add up to Summary.Succeeded and
// not Summary.Total, because only successful requests are timed. The loadtester
// command prints the ladder under its percentiles.
package loadtest
