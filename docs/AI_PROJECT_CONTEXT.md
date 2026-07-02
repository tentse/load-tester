# AI Project Context — HTTP Load Tester

> Current-state handoff, last updated **2026-07-01**  
> Module: `github.com/tentse/load-tester`  
> Current development branch: `main`

## Read order and document roles

1. Read `AGENTS.md`. It is authoritative for project constraints, the owner's learning
   workflow, and how code reviews must be delivered.
2. Read this file for the current architecture, behavior, status, and next milestone.
3. Read `README.md` for the public project description and safety warning.
4. Before touching anything, run `git status --short` and reread the current diff. The owner
   often has uncommitted learning work in progress; preserve it.

`CLAUDE.md` is only a compatibility pointer to these sources. Historical milestone guides
were removed because their status and checklists had drifted from the implementation.

## Project snapshot

This is a standard-library-first Go HTTP load tester and a learning project. It sends a
fixed total number of requests through a configurable number of concurrent workers, then
reports successful/failed counts, successful requests per second, p50/p90/p99 successful
latencies, and grouped errors.

The library is intended to be importable. Public types and functions remain in `loadtest`;
the future `cmd/loadtester` package is only CLI glue.

The sole approved third-party dependency is `go.uber.org/goleak`, used by tests to detect
goroutines that survive the test suite. It does not replace the Go race detector.

## Repository map

| Path | Purpose | Current state |
|---|---|---|
| `AGENTS.md` | Authoritative agent and review instructions | Active |
| `README.md` | Public description, commands, and safety warning | Active |
| `loadtest/apiCall.go` | Shared HTTP client and one-request execution | Implemented |
| `loadtest/run.go` | Configuration and concurrent worker pool | Implemented; edge/API review remains |
| `loadtest/stats.go` | Nearest-rank percentile calculation | Implemented |
| `loadtest/summary.go` | Result aggregation and public `Summary` | Implemented |
| `loadtest/*_test.go` | Unit, HTTP, cancellation, race, and leak coverage | Green; review comments remain |
| `cmd/loadtester/main.go` | Future CLI entry point | Only `package main` |

## Public API and current contracts

```text
Config
  URL          target URL
  Concurrency  number of workers
  Requests     total request count
  Timeout      shared client's request timeout
  Method       HTTP method
  Token        optional bearer token
  Body         optional JSON request body

Run(context, Config) -> (Summary, error)

Summary
  Total, Succeeded, Failed
  Elapsed, Throughput
  P50, P90, P99
  Errors
```

Current validation rejects empty URL and Method values, `Concurrency <= 0`,
`Requests <= 0`, and `Timeout <= 0`. Therefore both an empty Method and
`Requests == 0` are intentionally invalid. Preserve those contracts unless the owner
deliberately changes implementation, tests, and documentation together.

The following public-contract details are still review items:

- malformed non-empty URLs are not rejected before workers start;
- `Config` has draft godoc that needs tightening, and `Run` still lacks godoc;
- cancellation returns a partial `Summary` plus `ctx.Err()`;
- `Summary` documentation does not fully explain its success, latency, and throughput policy.

## Runtime architecture

The engine is closed-loop: a worker waits for one response before taking its next job.
There is no target-rate scheduler in v1.

```text
Run
 ├─ validate Config
 ├─ create one runner with one shared http.Client
 ├─ producer goroutine
 │    └─ send up to Requests job tokens; stop on context cancellation; close jobs
 ├─ Concurrency worker goroutines
 │    ├─ stop on context cancellation or closed jobs
 │    ├─ time the complete hit call
 │    └─ send one result
 ├─ closer goroutine
 │    └─ wait for all workers, then close results
 ├─ calling goroutine drains results
 └─ return summarize(partial-or-complete-results), ctx.Err()
```

Channel ownership:

- the producer alone closes `jobs`;
- workers receive jobs and send results;
- the WaitGroup closer alone closes `results`;
- the calling goroutine alone appends to the result slice and aggregates it.

Cancellation now reaches all important layers:

- producer sends select between `ctx.Done()` and `jobs`;
- workers select between `ctx.Done()` and jobs;
- HTTP requests are created with the same context;
- `Run` drains worker results before returning the partial summary and context error.

## HTTP and aggregation behavior

`runner.hit`:

- uses one shared client and cloned default transport;
- configures `MaxIdleConns` and `MaxIdleConnsPerHost` to 100;
- adds JSON content type only when a body exists;
- adds a bearer authorization header only when a token exists;
- drains and closes every successful response body;
- reports HTTP status as fact and leaves success policy to aggregation.

The remaining resource bug is documented inline: `io.Copy`'s body-read error is discarded,
so a mid-body connection failure can be misclassified as success.

`summarize` currently defines:

- transport/request errors as failures;
- status codes `>= 500` as failures;
- status codes below 500, including 4xx, as successes;
- percentiles from successful-request latencies only;
- throughput as `Succeeded / elapsed.Seconds()`;
- non-nil empty `Errors` maps for empty/success-only input;
- unknown HTTP status text as `HTTP <code>`.

Do not silently change these semantics. They are public API decisions and must be documented
and test-driven.

## Test architecture and current state

- `apiCall_test.go` uses `httptest.Server` for method, token, body, header, status, malformed
  URL, transport error, timeout, and context cancellation.
- `run_test.go` covers normal concurrency, strict URL/method/numeric validation,
  concurrency greater than request count, all-500 responses, and event-driven cancellation.
  `TestMain` wraps the package with `goleak.VerifyTestMain`.
- `stats_test.go` covers empty, single, known p50/p90/p99, and out-of-range percentile input.
- `summary_test.go` covers mixed outcomes, empty input, zero elapsed time, all failures,
  all successes, one result, and unknown HTTP status text.

At the latest verification point:

- `go test -count=1 ./...` passes;
- `go test -race -count=1 ./...` passes;
- goleak reports no unexpected surviving goroutines;
- `golangci-lint run` reports zero issues;
- the CLI build cannot succeed until `cmd/loadtester/main.go` defines `main()`.

The race detector and goleak answer different questions:

- `-race` detects unsynchronized memory access along executed paths;
- goleak detects goroutines that did not exit.

## Active inline review findings

One inline review finding remains: `runner.hit` discards errors returned while reading the
response body. It is tagged `[blocker]` because a truncated response can currently be counted
as successful, which makes the reported results incorrect.

## Next learning milestone

Fix response-body result correctness before beginning the CLI:

1. Add a deterministic test whose response body returns data followed by a read error.
2. Make response-body read failures observable to aggregation.
3. Run the complete normal, race, leak, lint, and vet checks.

Only then begin the CLI milestone.

## CLI milestone specification

Keep `cmd/loadtester` thin:

```text
parse flags
  -> construct loadtest.Config
  -> signal.NotifyContext for Ctrl+C
  -> loadtest.Run
  -> render Summary
  -> choose exit code
```

Planned standard-library flags:

| Flag | Default | Meaning |
|---|---:|---|
| `-url` | required | Target URL |
| `-c` | `10` | Concurrent workers |
| `-n` | `100` | Total requests |
| `-method` | `GET` | HTTP method |
| `-token` | empty | Bearer token |
| `-body` | empty | JSON request body |
| `-timeout` | `30s` | Per-request client timeout |

Rendering should accept an `io.Writer` so it can be tested with a buffer. Only `main` may
print or exit. Usage/help must warn users to target only systems they own or have explicit
permission to test.

Planned exit codes:

- `0`: run completed and reported;
- `1`: library/run error;
- `2`: invalid command usage.

## Commands

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
golangci-lint run
go build ./cmd/loadtester
```

Use `-count=1` while verifying concurrency changes so Go does not reuse cached test results.
After adding or removing a dependency, the owner should run `go mod tidy`.
