# load-tester

A small HTTP load tester written in Go. Point it at a URL, say how many requests to send and
how many to run at once, and it reports throughput, latency percentiles (p50/p90/p99), and a
breakdown of what failed.

It is also a library: the public API (`Config`, `Run`, `Summary`) lives in an importable
`loadtest` package, so you can drive load tests from your own Go code, not just the terminal.

Production code uses **only the Go standard library** by design.

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
loadtester -url http://localhost:8080/ -c 20 -n 500
```

```
Load test summary
Total: 500
Succeeded: 500
Failed: 0
Elapsed: 249.2195ms
Throughput: 2006.26 req/s
P50: 4.9175ms
P90: 7.433791ms
P99: 127.457334ms
Errors:
n/a
```

Press `Ctrl+C` at any point and the run stops cleanly: in-flight requests are canceled and
you still get a summary of everything that completed.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-url` | *(required)* | Target URL |
| `-c` | `10` | Number of concurrent workers |
| `-n` | `20` | Total number of requests to send |
| `-method` | `GET` | HTTP method |
| `-timeout` | `1s` | Per-request timeout, including reading the response body |
| `-token` | *(empty)* | Bearer token, sent as an `Authorization` header |
| `-body` | *(empty)* | Request body, sent as `application/json` |

```sh
loadtester -url https://api.example.internal/users \
  -method POST \
  -body '{"name":"test"}' \
  -token "$API_TOKEN" \
  -c 50 -n 1000 -timeout 5s
```

## Understanding the output

The engine is **closed-loop**: `-n` requests are sent in total, spread across `-c` workers,
and each worker waits for its response before taking the next request. There is no target
request rate — throughput is whatever the target can absorb.

- **Succeeded / Failed** — a request succeeds when it completes and returns a status **below
  500**. Statuses of 500 and above, along with timeouts, connection failures, and truncated
  responses, are counted as failures. Note that `404` counts as a success: the server
  responded, which is what a load test measures.
- **Throughput** — successful requests per second over the wall-clock run.
- **P50 / P90 / P99** — nearest-rank percentiles over **successful requests only**, so a wave
  of fast connection refusals cannot flatter your latency numbers. Each measurement covers the
  full request including reading the response body.
- **Errors** — failure messages grouped by how often they occurred, most frequent first.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The run completed and a summary was printed (`-h` also exits `0`) |
| `1` | The run failed for a reason other than configuration |
| `2` | Invalid usage — a bad flag, a missing `-url`, or an invalid configuration |
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
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d/%d succeeded, p99 %v\n", summary.Succeeded, summary.Total, summary.P99)
}
```

`Run` honors context cancellation: cancel the context and it stops scheduling work, waits for
in-flight requests, and returns the partial `Summary` along with `ctx.Err()`. A `Config` that
fails validation returns a zero `Summary` and an error wrapping `loadtest.ErrInvalidConfig`,
before any request is sent.

Full API documentation:
[pkg.go.dev/github.com/tentse/load-tester/loadtest](https://pkg.go.dev/github.com/tentse/load-tester/loadtest)

## Known limitations

Honest about what v0.1.0 does not do yet. Each of these is planned work, not a mystery.

- **A malformed URL is not rejected up front.** `-url nope` passes validation, every request
  then fails the same way, and the tool still exits `0`. Check the summary, not just `$?`,
  until this is fixed.
- **Memory grows with `-n`.** Every result is held until the run finishes, so a run of several
  million requests uses hundreds of MB. Fine for typical runs; plan around it for very large
  ones.
- **Error grouping can fragment.** Failures are grouped by their message, and some network
  errors embed the client's local port number. Connection refusals and timeouts group
  correctly, but a server that *resets* connections can produce one line per failed request
  instead of one line per cause.
- **`-token` on the command line is visible** in your shell history and to anyone who can run
  `ps` while the test is running. Prefer a shell variable that you clear afterwards.
- **Workers are not capped at `-n`.** Passing `-c 500000 -n 5` creates far more goroutines
  than there is work for. Harmless, but wasteful.
- **Single target only.** One URL, one method, one body per run.

## Roadmap

v0.2 adds multi-endpoint runs driven by a JSON file — several requests in one run, grouped
into separate summaries, sharing one bounded worker pool. The design and its trade-offs are
written up in [docs/MULTI_ENDPOINT_DESIGN.md](docs/MULTI_ENDPOINT_DESIGN.md).

## Development

```sh
go build ./...
go test ./...
go test -race -count=1 ./...    # the one that matters — this is a concurrency project
go test -cover ./...
go vet ./...
golangci-lint run
```

Tests use `httptest` servers and never touch the network. `go.uber.org/goleak` is a test-only
dependency that fails the suite if a goroutine outlives it.

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
