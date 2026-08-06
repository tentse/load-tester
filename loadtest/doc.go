// Package loadtest generates HTTP load against a single target and reports
// throughput, latency percentiles, and a breakdown of failures.
//
// A loadtest is described by a [Config] and executed by [Run], which returns a
// [Summary]. The engine is closed loop: Config.Requests requests are sent in
// total, spread across Config.Concurrency workers, and every worker waits for
// its response before taking the next request. There is no target request rate,
// and no way to run for a fixed duration: a run ends once Config.Requests
// requests have been sent, or when ctx is canceled.
//
// Authentication is expressed through Config.Headers rather than a dedicated
// field, so any scheme works: a bearer token, an API key under whatever name the
// target expects, or several at once. Query parameters must be included in the
// URL itself.
//
// Config has no defaults. Every field it validates must be set explicitly:
//
//	URL          must be non-empty
//	Method       must be non-empty
//	Concurrency  must be greater than zero
//	Requests     must be greater than zero
//	Timeout      must be greater than zero
//
// Headers and Body are the only optional fields. The loadtester command supplies
// its own defaults for the rest before calling Run; the library does not.
//
// Timeout applies to each request on its own, not to the run as a whole. It
// covers the complete round trip, including reading the response body.
//
// The shared HTTP client sets MaxIdleConns and MaxIdleConnsPerHost to 100. These
// are not configurable.
//
// A request succeeds when it completes without a transport error and its HTTP
// status is below 500. As of now statuses of 500 and above, and requests that never
// complete, are considered as failures. Summary.Throughput counts successful requests per
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
// This package generates real load. Only point it at systems you own or have
// explicit permission to test.
//
// Memory does not scale with Config.Requests. Each successful latency is counted
// into one of a fixed set of buckets as it arrives and the timing itself is
// discarded, so a run of ten million requests costs the same as a run of ten.
//
// The buckets are half-open, so [1ms, 2ms) includes exactly 1ms and excludes
// 2ms, and every latency lands in exactly one of them:
//
//	<1ms      1-2ms     2-5ms      5-10ms     10-20ms
//	20-50ms   50-100ms  100-200ms  200-500ms  500ms-1s
//	1-2s      2-5s      5-10s      >=10s
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
package loadtest
