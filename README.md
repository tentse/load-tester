# load-tester

A small HTTP load tester I'm writing in Go. You point it at a URL, tell it how many
requests to send and how many to run at once, and it reports back throughput
(requests/sec), latency percentiles (p50/p90/p99), and a breakdown of whatever errors
came back.

I'm building it from scratch to get properly comfortable with Go's concurrency model —
goroutines, channels, `context`, `sync` — and to end up with something I'd actually be
happy to put in front of people. It leans on the standard library by design; no
third-party dependencies.

## Status

Work in progress. The request engine — the part that fires a single HTTP request and
measures it — is written and tested. The layer that drives many of those at once, and the
command-line front end, aren't wired up yet, so there's nothing to `go run` at the moment.
The tests are the place to look.

When it's finished, running it should look roughly like:

```
go run ./cmd/loadtester -url http://localhost:8080 -c 50 -n 1000
```

## A word of warning

This thing exists to generate load. Only ever point it at something you own or have clear
permission to hit. Throwing traffic at someone else's server is rude at best and illegal at
worst — keep it to localhost and your own staging boxes.

## Running the tests

For now everything lives behind the test suite. From the repo root:

```
go test ./...
```

Here are the commands I actually lean on while working, and what each one is for:

| Command | What it's for |
|---|---|
| `go test ./...` | Run every test in the project. |
| `go test ./loadtest/...` | Just the load-tester package, when I don't care about the rest. |
| `go test -v -run TestSummary ./loadtest/` | Run a single test while iterating on it. `-run` takes a regex matched against test names; add `/SubtestName` (e.g. `-run TestPercentile/p90`) to drill into one table case. |
| `go test -race ./...` | Run the tests with the race detector on. This is the one that matters — concurrency bugs stay hidden until something goes looking for them, and this is how you look. |
| `go test -cover ./...` | Print a coverage percentage per package. |
| `go test -coverprofile=cover.out ./... && go tool cover -html=cover.out` | Write a coverage profile and open it in the browser, so I can see exactly which lines aren't being hit. |
| `go vet ./...` | Catch the stuff that compiles but is probably wrong — bad format verbs, copied locks, and so on. |
| `golangci-lint run` | The full linter pass. I keep this clean before I call anything done. |

If you run only one of them, make it `go test -race ./...`.

## Layout

- `loadtest/` — the library. The public API (`Config`, `Run`, `Summary`) will live here, so
  the tool can be imported and not just run from the terminal.
- `cmd/loadtester/` — the command-line entry point. Thin on purpose: parse the flags, call
  the library, print the results.
