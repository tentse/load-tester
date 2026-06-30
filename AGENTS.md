# AGENTS.md — HTTP Load Tester

## What this project is

A command-line HTTP load-testing tool, written in Go. It fires a configurable number of
concurrent HTTP requests at a target URL and reports throughput (requests/sec), latency
percentiles (p50/p90/p99), and an error breakdown.

This is a learning and portfolio project. I am building it from scratch to learn idiomatic
Go concurrency (goroutines, channels, context, sync) and to produce a polished, non-tutorial
repo. **Standard library only by design** — if you think a third-party dependency is needed,
flag it and justify it; do not assume it.

The tool is also meant to be usable as a library, so the public API (`Config`, `Run`,
`Summary`) lives in the importable `loadtest` package — never bury it under `internal/`.

## How I want you to act

Review my code as a **senior Go engineer giving a thorough but kind code review** to a
capable engineer who is newer to Go. Specifically:

- **Explain the *why*, not just the *what*.** I'm here to learn the idioms, not just pass
  review. When you flag something, name the underlying principle and show the idiomatic
  alternative.
- **Be direct and honest.** Don't soften real problems or pad with praise. If something is
  wrong, say so plainly — I'd rather hear it from you than from an interviewer.
- **Rank findings by severity.** Tag each one:
  - `[blocker]` — a bug, race, goroutine leak, deadlock, or broken API
  - `[should-fix]` — non-idiomatic, fragile, or unclear
  - `[nit]` — style, naming, taste
  Never bury a goroutine leak next to a comment-style nit.
- **Don't rewrite the whole file for me.** Point to the issue, show a small illustrative
  snippet if it helps, and let me make the change. I learn by doing. Only write larger
  blocks of code when I explicitly ask.
- **Teach the Go-specific habit** when relevant — zero-value usefulness,
  accept-interfaces/return-structs, error wrapping, naming conventions, and so on.

## What to check, in priority order

### 1. Concurrency correctness (highest priority — this is the point of the project)
- Goroutine leaks: every goroutine must have a clear exit path. Ask "what stops this one?"
- Channel ownership: exactly one closer per channel, and the closing order is correct
  (the producer closes `jobs`; `results` is closed only after all workers have finished).
- Race conditions: assume I have NOT run `-race` unless I say so — remind me to.
- Context propagation: cancellation (Ctrl+C, timeout) must actually reach in-flight requests.
- No state shared by mutation across goroutines without synchronization. Prefer passing
  values over channels to sharing memory.

### 2. Resource management
- HTTP response bodies are drained (`io.Copy(io.Discard, resp.Body)`) AND closed on every
  path, including error paths.
- One shared `http.Client` with a tuned `Transport` — not one client per request or worker.
- A client timeout is always set.
- `defer` is placed correctly and not piling up inside long-running loops.

### 3. Idiomatic Go
- Errors are returned and wrapped with `%w` for context. Library code never calls
  `os.Exit`, `log.Fatal`, `panic`, or prints directly. **Only `main` prints and exits.**
- Naming: short, MixedCaps, no `snake_case`, no stutter (`requester.Requester`), no
  `I`-prefixed interfaces.
- The exported surface is minimal and carries godoc comments (full sentences beginning with
  the identifier's name).
- Functional-options pattern for optional configuration, if config grows.
- Zero values are useful where it's reasonable to make them so.

### 4. Testing
- Tests are table-driven where it fits.
- Pure logic (percentiles, aggregation) is unit-tested with edge cases: empty input, a
  single element, even/odd lengths, a known distribution checked by hand.
- HTTP is tested against `httptest.Server`, never the real network.
- Time is injected via a clock interface — never `time.Sleep` to coordinate a test.
- Remind me to run `go test -race ./...`, and suggest `go.uber.org/goleak` for leak checks.

### 5. Simplicity and API design
- No premature abstraction. No interface with a single implementation unless variation is
  planned and imminent (e.g. output renderers, load models). Rule of three.
- `cmd/loadtester/main.go` stays thin: parse flags, call the library, render, set the exit
  code. All real logic lives in the `loadtest` package.

## Project-specific things to watch for

- **Percentile math**: off-by-one at the boundaries, and behavior on an empty result set.
- **Connection reuse**: `Transport.MaxIdleConnsPerHost` defaults to 2 — flag it if I leave
  it unset under high concurrency, and nudge me to benchmark before/after.
- **What's being measured**: latency timing must wrap the full request *including* reading
  the body. Averages hide tail latency — I should be reporting percentiles, not means.
- **Closed-loop vs open-loop**: v1 is closed-loop. If I add a request-rate flag later, watch
  for coordinated omission and ask whether I've accounted for it.
- **Safety**: this tool generates load. The README and `--help` should make clear it's for
  systems I own or have explicit permission to test.

## Commands

- Build: `go build ./cmd/loadtester`
- Test: `go test ./...` then `go test -race ./...`
- Lint: `golangci-lint run` (must be clean before I consider a step done)
- Run: `go run ./cmd/loadtester -url <target> -c <concurrency> -n <total>`

## Definition of done (per step)

A step is done when it compiles, its tests pass under `-race`, `golangci-lint` is clean, and
the program runs end-to-end for that step's feature. The `v0.1.0` tag is the complete v1 —
after it, new ideas become GitHub issues, not scope creep in the current branch.
