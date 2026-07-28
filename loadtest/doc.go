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
// Currently, the request config supports only TOKEN and BODY, and any query
// parameters must be included in the URL itself.
//
// Config has no defaults. Every field it validates must be set explicitly:
//
//	URL          must be non-empty
//	Method       must be non-empty
//	Concurrency  must be greater than zero
//	Requests     must be greater than zero
//	Timeout      must be greater than zero
//
// Token and Body are the only optional fields. The loadtester command supplies
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
// second, and Summary.P50, P90, and P99 are nearest rank latencies over
// successful requests only. Each latency covers the complete request, including
// reading the response body. Redirects are followed using the default net/http
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
// For now, it is best to keep the number of requests moderate, since all results are
// currently stored in memory and large runs can consume a significant amount of RAM.
package loadtest
