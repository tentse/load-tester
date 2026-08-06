# load-tester

[![Go Reference](https://pkg.go.dev/badge/github.com/tentse/load-tester.svg)](https://pkg.go.dev/github.com/tentse/load-tester/loadtest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tentse/load-tester)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A small HTTP load tester written in Go. Point it at a URL, tell it how many requests to send
and how many to run at once, and it tells you how the target held up — throughput, latency
percentiles, and a breakdown of whatever went wrong.

It's a library as well as a command. The public API (`Config`, `Run`, `Summary`) lives in an
importable `loadtest` package, so you can drive load tests from your own Go code instead of
shelling out to a binary.

The production code uses **nothing but the Go standard library**. That's a deliberate
constraint, not an accident — the whole point was to learn Go's concurrency model properly
rather than lean on someone else's worker pool. It was built test-first, following
[Learn Go with Tests](https://quii.gitbook.io/learn-go-with-tests/), with AI guiding the
design and reviewing the code rather than writing it.

> **⚠️ This tool generates real traffic.** Only point it at systems you own or have explicit
> permission to test. Load testing someone else's server without permission is rude at best
> and illegal at worst — keep it to localhost and your own staging environments.

## Install

```sh
go install github.com/tentse/load-tester/cmd/loadtester@latest
```

Or build from source:

```sh
git clone https://github.com/tentse/load-tester.git
cd load-tester
go build ./cmd/loadtester
```

Requires Go 1.26 or newer.

## Quick start

```sh
loadtester -url http://localhost:8080/ -c 20 -n 500 -expect 200
```

`-expect` is required: you tell the tool which status code counts as a success, and everything
else is a failure. See [Expected status](#expected-status) for why it has no default.

```
Load test summary
Total: 500
Succeeded: 500
Failed: 0
Elapsed: 249.2195ms
Throughput: 2006.26 req/s
P50: <= 5ms
P90: <= 10ms
P99: <= 200ms
Errors:
n/a
```

Press `Ctrl+C` at any point and the run stops cleanly: in-flight requests are canceled and
you still get a summary of everything that completed.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-url` | *(required)* | Target URL |
| `-expect` | *(required)* | HTTP status code that counts as a success. Any other status is a failure |
| `-c` | `10` | Number of concurrent workers |
| `-n` | `20` | Total number of requests to send |
| `-method` | `GET` | HTTP method |
| `-timeout` | `1s` | Per-request timeout, including reading the response body |
| `-H` | *(none)* | Custom request header as `"Name: Value"`. Repeatable — pass it once per header |
| `-body` | *(empty)* | Request body. Sets `Content-Type: application/json` unless you set that header yourself |

```sh
loadtester -url https://api.example.internal/users \
  -method POST \
  -body '{"name":"test"}' \
  -H "Authorization: Bearer $API_TOKEN" \
  -expect 201 \
  -c 50 -n 1000 -timeout 5s
```

### Expected status

`-expect` takes exactly one status code, and it has no default — every run has to say what it
considers a success. That is deliberate: a load test that does not check what came back can
report a perfectly healthy run while hitting the wrong endpoint entirely. Point the tool at a
typo'd path without `-expect` and a wall of `404`s would look identical to a wall of `200`s.

Match the code to what the endpoint actually returns — a `POST` that creates something usually
answers `201`, not `200`:

```sh
loadtester -url https://api.example.internal/orders -method POST \
  -body '{"item":"x"}' -expect 201 -n 500
```

Because the check is an exact match rather than a range, you can load-test an error path on
purpose. This run treats `500` as the success case and reports anything else as a failure:

```sh
loadtester -url https://api.example.internal/boom -expect 500 -n 200
```

Requests that never get a response at all — timeouts, connection refusals, resets — are always
failures, whatever `-expect` is set to. There is no status code to compare in that case.

### Headers

`-H` takes any header, so authentication is whatever your API expects rather than a fixed
scheme — a bearer token, an API key under whatever name your service uses, or both:

```sh
loadtester -url https://api.example.internal/orders \
  -H "X-API-Key: $API_KEY" \
  -H "X-Request-Source: load-test" \
  -expect 200
```

Repeating the same name sends the header more than once, in the order given:

```sh
loadtester -url https://api.example.internal/search -H "X-Tag: a" -H "X-Tag: b" -expect 200
```

A malformed header — no colon, an empty name, or a newline in either field — is rejected
before a single request is sent, and the run exits `2`.

Keep credentials out of your shell history: prefer a variable you clear afterwards, since
anything on the command line is visible to `ps` while the run is in progress.

## Understanding the output

The engine is **closed-loop**: `-n` requests are sent in total, spread across `-c` workers,
and each worker waits for its response before taking the next request. There is no target
request rate — throughput is whatever the target can absorb.

- **Succeeded / Failed** — a request succeeds when it completes and returns **exactly the status
  you passed to `-expect`**. Every other status is a failure, as are timeouts, connection
  failures, and truncated responses. So under `-expect 200`, a `404` is a failure — the server
  answered, but not with what you asked for.
- **Throughput** — successful requests per second over the wall-clock run.
- **P50 / P90 / P99** — latency percentiles over **successful requests only**, so a wave
  of fast connection refusals cannot flatter your latency numbers. Each measurement covers the
  full request including reading the response body. Percentiles are reported as the **upper
  bound** of a latency bucket and printed with a leading `<=`, so read `P99: <= 200ms` as "99%
  of successful requests finished in under 200ms" — see
  [How latencies are aggregated](#how-latencies-are-aggregated) below.
- **Errors** — safe, stable failure categories grouped by how often they occurred, most
  frequent first. A request that came back with the wrong status is listed under that status's
  name, so under `-expect 200` a run against a missing path reads `not found: 500`. Requests
  that never completed are listed by cause instead: request timeouts, connection refusals,
  connection resets, truncated responses, and unknown request failures use fixed category
  names. The counts always add up to `Failed`. URL user information and
  query values are not included in these categories, and equivalent failures are grouped
  together even when their underlying network errors contain different local ports.

### How latencies are aggregated

A run can send millions of requests, so keeping every latency in memory does not scale.
Instead, each successful request's latency is counted into one of 14 fixed buckets, and only
the counters are kept — the individual timings are discarded as they arrive.

Here is the full ladder, holding the counters behind the 500-request run shown in
[Quick start](#quick-start). These are internal counts, not printed output:

```
  bucket       count
  <1ms             0
  1–2ms           40   ████
  2–5ms          260   ██████████████████████████
  5–10ms         155   ███████████████
  10–20ms         30   ███
  20–50ms          6   ▌
  50–100ms         3   ▎
  100–200ms        5   ▌
  200–500ms        1   ▏
  500ms–1s         0
  1–2s             0
  2–5s             0
  5–10s            0
  ≥10s             0
```

Buckets are half-open: `[1ms, 2ms)` includes exactly 1ms and excludes 2ms. Every latency
therefore lands in exactly one bucket, and the counts always sum to the number of successful
requests — no gaps, no double counting.

The ladder is multiplicative rather than evenly spaced, each bucket roughly 2–2.5× the width
of the last. Latency is skewed, exactly as the counts above show: most requests cluster at the
low end while the interesting tail stretches across orders of magnitude. Fixed-width buckets
would drop nearly everything into the first one and spend the rest on an empty tail.

A percentile is then read by walking the buckets from fastest to slowest, accumulating counts
until the target rank is reached, then reporting that bucket's upper bound. For `P90` above,
the rank is `0.9 × 500 = 450`; the running total passes it in `5–10ms` (40 + 260 + 155 = 455),
so `P90` reports `10ms` and the CLI prints it as `P90: <= 10ms`.

The trade-off is memory for precision. Memory is constant — 14 counters no matter what `-n`
is, so two million requests cost the same as ten — but a percentile is only known to the
width of the bucket it lands in.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The run completed and a summary was printed (`-h` also exits `0`) |
| `1` | The run failed for a reason other than configuration |
| `2` | Invalid usage — a bad flag, a missing `-url` or `-expect`, or an invalid configuration |
| `130` | Interrupted with `Ctrl+C`; a partial summary was printed |

A run whose requests all *failed* still exits `0` — the load test itself succeeded, and the
result is in the summary. Check `Failed` rather than the exit code to judge target health.

## Use as a library

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

func main() {
	summary, err := loadtest.Run(context.Background(), loadtest.Config{
		URL:         "http://localhost:8080/",
		Method:      "GET",
		Concurrency: 10,
		Requests:    100,
		Timeout:     time.Second,
		Expect:      http.StatusOK,
		Headers: http.Header{
			"X-API-Key": {"secret"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d/%d succeeded, p99 %v\n", summary.Succeeded, summary.Total, summary.P99)
}
```

`Expect` is required here exactly as `-expect` is on the command line — a `Config` that leaves
it at zero fails validation rather than defaulting to anything.

`Run` honors context cancellation: cancel the context and it stops scheduling work, waits for
in-flight requests, and returns the partial `Summary` along with `ctx.Err()`. A `Config` that
fails validation returns a zero `Summary` and an error wrapping `loadtest.ErrInvalidConfig`,
before any request is sent.

Full API documentation:
[pkg.go.dev/github.com/tentse/load-tester/loadtest](https://pkg.go.dev/github.com/tentse/load-tester/loadtest)

## Known limitations

Honest about what the tool does not do yet. Each of these is planned work, not a mystery.

- **Configuration errors are reported as target failures.** A malformed URL (`-url nope`) or
  an unusable method is caught by Go's HTTP client, not by validation — so no request ever
  leaves your machine, yet the summary blames the target with `request failed` and the tool
  still exits `0`. Check the summary, not just `$?`, and suspect your own flags first when
  every request fails identically.
- **Percentiles are bucketed, not exact.** Latencies are counted into a fixed ladder of
  buckets — `<1ms`, `1–2ms`, `2–5ms`, `5–10ms`, and so on up to `≥10s` — so a reported
  percentile is the upper bound of its bucket and can overstate the true latency by up to
  about 2.5×. Precision is also capped by `-n`: percentiles resolve only in steps of `1/n`,
  so a p99 from a 100-request run rests on a single observation.
- **Secrets on the command line are visible** in your shell history and to anyone who can run
  `ps` while the test is running. This covers a credential passed via `-H`, and equally a key
  embedded in `-url`. Prefer a shell variable that you clear afterwards.
- **Workers are not capped at `-n`.** Passing `-c 500000 -n 5` creates far more goroutines
  than there is work for. Harmless, but wasteful.
- **Single target only.** One URL, one method, one body per run.
- **`-expect` takes one exact code, not a range or a list.** There is no way to accept "any
  2xx" or "200 or 204" in a single run, so an endpoint that legitimately answers with more than
  one status has to be tested one code at a time. The value is also only checked for being
  positive — `-expect 99999` is accepted and simply fails every request.
- **No redirect control.** Redirects are followed automatically, so a `301` never shows up in
  your results — you get the status at the end of the chain, and the latency covers every hop.
  This means `-expect 301` can never match against a URL that actually redirects. It also means
  `-n` undercounts the load your server receives: against a URL that redirects once, `-n 500`
  puts **1000** requests on the target.
- **No fixed-duration runs.** You say how many requests to send, not how long to run for.

## Roadmap

The next major feature is multi-endpoint runs driven by a JSON file — several requests in one
run, grouped into separate summaries, sharing one bounded worker pool. The design and its
trade-offs are written up in [docs/MULTI_ENDPOINT_DESIGN.md](docs/MULTI_ENDPOINT_DESIGN.md).

## Development

There is no build tooling beyond the Go toolchain itself — every command below is plain `go`,
except the optional linter.

### Build

| Command | What it does |
|---|---|
| `go build ./...` | Compiles every package and reports type errors, without leaving a binary in your working tree. |
| `go build -o loadtester ./cmd/loadtester` | Builds the CLI itself, so you can run it as `./loadtester`. |
| `go install ./cmd/loadtester` | Installs `loadtester` into `$GOBIN` (usually `~/go/bin`) so it's on your `PATH`. |

### Tests

| Command | What it does |
|---|---|
| `go test ./...` | Runs the whole suite — the default check, and the one you'll run most often. |
| `go test -v ./...` | The same run, but prints each test name and result; what you want when something fails. |
| `go test -run TestRunCancellation ./loadtest/` | Runs a single test by name (the argument is a regex), for working on one behaviour at a time. |
| `go test -race -count=1 ./...` | **The one that matters** — runs the suite under the race detector with caching disabled. |
| `go test -cover ./...` | Runs the suite and prints a coverage percentage per package. |
| `go test -count=5 ./...` | Runs the suite five times over, to shake out flakiness a single green run would hide. |

`go test -race` earns its emphasis. This is a concurrency project, and data races stay completely
invisible until something goes looking for them — a suite that passes without `-race` tells you
very little. `-count=1` disables Go's test result cache, so you're testing your actual code
rather than a cached result from an earlier run. Run this before every PR.

Two things keep the suite trustworthy:

- **Nothing touches the network.** HTTP is exercised against `httptest.Server`, so the tests are
  fast, offline, and deterministic.
- **Leaked goroutines fail the build.** `go.uber.org/goleak` is a test-only dependency that fails
  the suite if a goroutine outlives the test that started it — precisely the failure mode a
  worker-pool project is most likely to have.

### Coverage

```sh
go test -cover ./...                        # quick per-package percentage
go test -coverprofile=coverage.out ./...    # write a profile to disk
go tool cover -func=coverage.out            # per-function breakdown, total on the last line
go tool cover -html=coverage.out            # annotated view in your browser
```

`-coverprofile` writes a machine-readable profile; the two `go tool cover` commands render it.
The `-html` view is the one worth reaching for — it colours covered lines green and uncovered
lines red, which is how you catch a branch you only *thought* you'd tested.

Current state: **`loadtest` is at 100%**, `cmd/loadtester` at 91.5%, **96.5% overall**. All the
real logic lives in `loadtest`, and it's meant to stay at 100%.

### Formatting and static analysis

| Command | What it does |
|---|---|
| `gofmt -l .` | Lists files that aren't correctly formatted — silence means everything is fine. |
| `gofmt -w .` | Rewrites those files in place, fixing the formatting for you. |
| `go vet ./...` | Reports suspicious code that still compiles: bad `Printf` verbs, unused results, copied locks. |
| `golangci-lint run` | Runs a bundle of third-party linters in one pass; the only tool here that isn't part of Go. |

`gofmt` and `go vet` both ship with Go, and both are currently clean.

`golangci-lint` is optional and installed separately (`brew install golangci-lint`, or see the
[install docs](https://golangci-lint.run/welcome/install/)). It currently reports **4 `errcheck`
findings**, all of them unchecked `fmt.Fprintf` writes to the CLI's own output stream in
`cmd/loadtester/main.go`. They're known and on the list to fix — don't let them block your PR,
but do keep your own changes clean.

### Before opening a PR

```sh
gofmt -l .                      # must print nothing
go vet ./...                    # must print nothing
go test -race -count=1 ./...    # must pass
```

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
