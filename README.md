# load-tester

[![Go Reference](https://pkg.go.dev/badge/github.com/tentse/load-tester.svg)](https://pkg.go.dev/github.com/tentse/load-tester/loadtest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tentse/load-tester)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A small HTTP load tester written in Go. Point it at a URL, tell it how many requests to send
and how many to run at once, and it tells you how the target held up — throughput, latency
percentiles, the full latency distribution, and a breakdown of whatever went wrong. Point it at
a JSON file instead and it runs several endpoints in one pass, reporting each one separately.

It's a library as well as a command. The public API (`Config`, `Run`, `FileConfig`,
`RequestSpec`, `FileRun`, `Summary`, `Bucket`) lives in an importable `loadtest` package, so you
can drive load tests from your own Go code instead of shelling out to a binary.

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
Elapsed: 115.927083ms
Throughput: 4313.06 req/s
P50: <= 5ms
P90: <= 10ms
P99: <= 20ms
  bucket       count
  <1ms             0
  1–2ms            0
  2–5ms          404   ██████████████████████████████████
  5–10ms          76   ██████▍
  10–20ms         20   █▋
  20–50ms          0
  50–100ms         0
  100–200ms        0
  200–500ms        0
  500ms–1s         0
  1–2s             0
  2–5s             0
  5–10s            0
  ≥10s             0
Errors:
n/a
```

To exercise several endpoints in one run, describe them in a JSON file instead:

```sh
loadtester -f requests.json
```

Each endpoint gets its own summary, and they share one worker pool. See
[Several endpoints from a file](#several-endpoints-from-a-file).

Press `Ctrl+C` at any point and the run stops cleanly: in-flight requests are canceled and
you still get a summary of everything that completed.

## Flags

There are two modes. Pass `-f` to run several endpoints from a JSON file, or use the flags below
to test a single URL.

| Flag | Default | Meaning |
|---|---|---|
| `-f` | *(none)* | JSON file describing several endpoints. Cannot be combined with any flag below |
| `-url` | *(required)* | Target URL |
| `-expect` | *(required)* | HTTP status code that counts as a success. Any other status is a failure |
| `-c` | `10` | Number of concurrent workers |
| `-n` | `20` | Total number of requests to send |
| `-method` | `GET` | HTTP method |
| `-timeout` | `1s` | Per-request timeout, including reading the response body |
| `-H` | *(none)* | Custom request header as `"Name: Value"`. Repeatable — pass it once per header |
| `-body` | *(empty)* | Request body. Sets `Content-Type: application/json` unless you set that header yourself |

`-url` and `-expect` are required only when you are not using `-f`; the file carries its own
equivalents. Every flag accepts either spelling of its value, so `-f requests.json` and
`-f=requests.json` do the same thing.

```sh
loadtester -url https://api.example.internal/users \
  -method POST \
  -body '{"name":"test"}' \
  -H "Authorization: Bearer $API_TOKEN" \
  -expect 201 \
  -c 50 -n 1000 -timeout 5s
```

### Several endpoints from a file

```sh
loadtester -f requests.json
```

```json
{
  "version": 1,
  "baseUrl": "https://api.example.internal",
  "concurrency": 50,
  "timeout": "5s",
  "requests": [
    { "name": "search", "url": "/search?q=foo", "count": 40, "expectStatus": 200 },
    { "name": "create-user", "method": "POST", "url": "/users",
      "body": { "name": "test" },
      "headers": { "Content-Type": "application/json" },
      "count": 10, "expectStatus": 201 }
  ]
}
```

You get one summary per `name`:

```
Name: create-user
Total: 10
Succeeded: 10
Failed: 0
Elapsed: 11.180792ms
Throughput: 894.39 req/s
P50: <= 1ms
P90: <= 1ms
P99: <= 1ms
  bucket       count
  <1ms            10   ██████████████████████████████████
  ...
Errors:
n/a

Name: search
Total: 40
Succeeded: 40
Failed: 0
Elapsed: 11.180792ms
Throughput: 3577.56 req/s
P50: <= 5ms
P90: <= 5ms
P99: <= 5ms
  bucket       count
  2–5ms           40   ██████████████████████████████████
  ...
Errors:
n/a
```

Every request sharing a `name` is reported as one summary, so the same endpoint can appear more
than once with different bodies and still be measured as a single thing. `concurrency` is the
total number of workers, shared across all endpoints rather than given to each, so adding an
endpoint spreads the same pool wider instead of adding load.

`method` defaults to `GET` and `count` to `1`. `name`, `url` and `expectStatus` are required on
every entry — `name` because it is the label your results are grouped and reported under, and a
generated one would leave you matching summaries back to entries by hand.

`baseUrl` and each `url` are joined with exactly one slash between them, so neither side has to
be careful about its own slashes. All four of these produce `https://api.example.internal/users`:

| `baseUrl` | `url` |
|---|---|
| `https://api.example.internal` | `/users` |
| `https://api.example.internal` | `users` |
| `https://api.example.internal/` | `/users` |
| `https://api.example.internal/` | `users` |

Leave `baseUrl` out entirely and each `url` has to be a complete URL of its own.

Passing any single-target flag alongside `-f` exits `2`. The file already carries those settings,
and two sources for one rule is exactly what the format avoids.

The format's design and its trade-offs are written up in
[docs/MULTI_ENDPOINT_DESIGN.md](docs/MULTI_ENDPOINT_DESIGN.md), kept as a record of the reasoning
rather than as current documentation.

> **Two things about this output are known and being changed.** `Elapsed` and `Throughput` are
> repeated identically under every name because they describe the whole run, not that endpoint —
> a name's `Throughput` is only its share of the overall rate, so it is `Total` rescaled by a
> constant and tells you nothing `Total` does not. And endpoints are issued **in order**, each
> one's `count` in full before the next begins, rather than mixed together — so an endpoint's
> percentiles are measured while it has the pool to itself, not while it competes with its
> neighbours. Until that changes, read the per-endpoint numbers as "this endpoint, run alone" and
> ignore the repeated rate. See [Known limitations](#known-limitations).

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
- **The bucket ladder** — the counts behind those percentiles, one row per bucket. Three numbers
  cannot tell you whether the slow requests trail off gently or jump straight to very slow; the
  ladder can. It prints on every run that had at least one success, and is not printed at all
  when nothing succeeded — see
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

Every run prints the full ladder under its percentiles. Here it is again from the 500-request
run shown in [Quick start](#quick-start):

```
  bucket       count
  <1ms             0
  1–2ms           40   █████▏
  2–5ms          260   ██████████████████████████████████
  5–10ms         155   ████████████████████▎
  10–20ms         30   ███▉
  20–50ms          6   ▊
  50–100ms         3   ▍
  100–200ms        5   ▋
  200–500ms        1   ▏
  500ms–1s         0
  1–2s             0
  2–5s             0
  5–10s            0
  ≥10s             0
```

Each bar is sized against the busiest bucket rather than against a fixed number of requests, so
the longest bar is always 34 characters wide and the picture looks the same whether the run sent
500 requests or 50 million. Any bucket with at least one request in it always draws something,
down to a one-eighth sliver of a character, so a single slow request never disappears into a
blank row. The counts cover **successful requests only**, so on a run with failures they add up
to `Succeeded` rather than `Total`.

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
| `2` | Invalid usage — a bad flag, a missing `-url` or `-expect`, a config file that will not load, or an invalid configuration |
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

	for _, bucket := range summary.Buckets {
		if bucket.Count > 0 {
			fmt.Printf("%-10s %d\n", bucket.Label(), bucket.Count)
		}
	}
}
```

`Expect` is required here exactly as `-expect` is on the command line — a `Config` that leaves
it at zero fails validation rather than defaulting to anything.

`Summary.Buckets` gives you the same latency breakdown the command prints, but as data instead
of text. There is one [`Bucket`](https://pkg.go.dev/github.com/tentse/load-tester/loadtest#Bucket)
per step of the ladder, from fastest to slowest. `Lo` and `Hi` are the start and end of the
range, and `Count` is how many successful requests landed in it. `Lo` is part of the range and
`Hi` is not, and the last bucket has no upper limit, so it reports an `Hi` of zero.
`Bucket.Label` gives you the name the command prints, so the 500ms to 1s bucket reads
`500ms–1s`. The counts add up to `Succeeded`, not `Total`. Drawing the bars is left to the
command.

`Run` honors context cancellation: cancel the context and it stops scheduling work, waits for
in-flight requests, and returns the partial `Summary` along with `ctx.Err()`. A `Config` that
fails validation returns a zero `Summary` and an error wrapping `loadtest.ErrInvalidConfig`,
before any request is sent.

### Several endpoints

`FileRun` takes a `FileConfig` and returns a `map[string]Summary`, keyed by each spec's `Name`.
You do not need the JSON file to use it — the file format is just one way to build a `FileConfig`:

```go
summaries, err := loadtest.FileRun(context.Background(), loadtest.FileConfig{
	Version:     1,
	BaseURL:     "http://localhost:8080",
	Concurrency: 10,
	Timeout:     5 * time.Second,
	Requests: []loadtest.RequestSpec{
		{
			Name:   "search",
			URL:    "/search?q=foo",
			Method: http.MethodGet,
			Count:  40,
			Expect: http.StatusOK,
		},
		{
			Name:   "create-user",
			URL:    "/users",
			Method: http.MethodPost,
			Body:   `{"name":"test"}`,
			Count:  10,
			Expect: http.StatusCreated,
		},
	},
})
if err != nil {
	log.Fatal(err)
}

for _, name := range slices.Sorted(maps.Keys(summaries)) {
	s := summaries[name]
	fmt.Printf("%-12s %d/%d succeeded, p99 %v\n", name, s.Succeeded, s.Total, s.P99)
}
```

```
create-user  10/10 succeeded, p99 1ms
search       40/40 succeeded, p99 5ms
```

Map iteration order is random in Go, so sort the keys if you want stable output — the command
does the same thing. Each `Summary` has the same shape as the single-target one, with the caveats
in [Several endpoints from a file](#several-endpoints-from-a-file) about `Elapsed` and
`Throughput` belonging to the run rather than the endpoint.

To load a `FileConfig` from a JSON file yourself, `configfile.Load` takes an `fs.FS` and a
filename and returns a validated `FileConfig`, with every error wrapping
`loadtest.ErrInvalidConfig`.

Full API documentation:
[pkg.go.dev/github.com/tentse/load-tester/loadtest](https://pkg.go.dev/github.com/tentse/load-tester/loadtest)

## Known limitations

Honest about what the tool does not do yet. Each of these is planned work, not a mystery.

- **`Elapsed` and `Throughput` in a file run describe the run, not the endpoint.** Every name
  reports the same `Elapsed`, so a name's `Throughput` is only its `Total` rescaled — compare
  endpoints by their percentiles and bucket ladders instead.
- **A repeated flag silently takes the last value.** `-n 10 -n 5000` sends 5,000 requests, and
  `-f missing.json -f ok.json` runs `ok.json` and exits `0` without ever opening the first file.
- **Endpoints in a file run in sequence, not mixed.** Each entry's `count` is sent in full before
  the next begins, so endpoints never contend with each other and their percentiles are measured
  with the worker pool to themselves.
- **Configuration errors are reported as target failures.** A malformed URL like `-url nope` is
  caught by Go's HTTP client rather than by validation, so the summary blames the target with
  `request failed` and the run still exits `0`.
- **A repeated JSON key is accepted and the last one wins.** `"count": 2, "count": 9999` parses
  cleanly and sends 9,999 requests — unknown fields are rejected, but duplicated known ones are
  not.
- **Anything after the closing brace of the config is ignored.** A truncated or double-pasted
  file can load as though it were perfectly fine.
- **The target can receive more requests than you asked for.** Go's HTTP client retries
  idempotent requests that die on a reused connection, and every redirect adds a hop — `-n 500`
  against a URL that redirects once puts 1,000 requests on the server.
- **A wrong-status failure is named after the status that arrived.** Under `-expect 500` a
  healthy server prints `Errors: ok: 40` beneath `Failed: 40`; the count is right, the wording
  is not.
- **Percentiles are bucketed, not exact.** A percentile is reported as the upper bound of its
  bucket, so it can overstate the true latency by up to about 2.5×, and precision is capped by
  `-n`. The printed ladder shows you how rough the number is.
- **`-expect` takes one exact code, not a range or a list.** There is no way to accept "any 2xx",
  and the value is only checked for being positive, so `-expect 99999` is accepted and fails
  every request.
- **No redirect control.** Redirects are followed automatically, so you only see the status at
  the end of the chain and `-expect 301` can never match a URL that actually redirects.
- **Secrets on the command line are visible** in your shell history and to anyone who can run
  `ps` during the run, whether passed via `-H` or embedded in `-url`.
- **Workers are not capped at `-n`.** Passing `-c 500000 -n 5` creates far more goroutines than
  there is work for. Wasteful, not harmful.
- **No fixed-duration runs.** You say how many requests to send, not how long to run for.

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

### Before opening a PR

```sh
gofmt -l .                      # must print nothing
go vet ./...                    # must print nothing
go test -race -count=1 ./...    # must pass
```

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
