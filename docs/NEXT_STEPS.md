# NEXT_STEPS — building the rest of the load tester

> **What this file is.** A handoff guide for the next two milestones of this project:
> **Step 1 — the concurrent engine (`Config` + `Run`)** and **Step 2 — the CLI front end
> (`cmd/loadtester/main.go`)**. It is written so that *any* assistant (or future-me) can pick
> up the work cold, without re-deriving the design or the house rules.
>
> **How to use it.** Read §0 first (it's the context a cold reader needs). Then work Step 1
> top to bottom, then Step 2. The guide **teaches and points** — it deliberately does **not**
> contain finished `Run`/`main` code, because in this project *I write the implementation
> myself, test-first*. If you're an AI helping me: coach me through each numbered step, show
> *small* snippets to illustrate an idea, and let me write the real code. Don't hand me a
> finished file.
>
> _Scaffolding doc — safe to delete before the `v0.1.0` tag._

---

## 0. Where we are now

This is a command-line **HTTP load tester** written in Go. You point it at a URL, say how
many requests to send (`-n`) and how many at once (`-c`), and it reports **throughput**
(requests/sec), **latency percentiles** (p50/p90/p99), and an **error breakdown**. It's a
learning + portfolio project: the whole point is to learn idiomatic Go concurrency
(goroutines, channels, `context`, `sync`) by building this from scratch.

Module path: `github.com/tentse/load-tester` (Go 1.26).

### The house rules (non-negotiable — from `CLAUDE.md`)

1. **Standard library only.** No third-party deps without an explicit justification. (The one
   likely exception is `go.uber.org/goleak` in *tests only* — see Step 1.)
2. **TDD, test-first.** Write the failing test, then the code that makes it pass.
3. **Library code never prints or exits.** No `fmt.Println`, `log.Fatal`, `os.Exit`, or
   `panic` inside the `loadtest` package. It **returns errors**. Only `main` prints and sets
   the exit code.
4. **Wrap errors with `%w`** for context: `fmt.Errorf("do %s: %w", url, err)`.
5. **Report percentiles, not means.** Averages hide tail latency.
6. **Drain and close every response body, on every path** (already done in `hit`).
7. **One shared `http.Client`** with a tuned `Transport` — never one client per request.
8. **The public API lives in the importable `loadtest` package**, never under `internal/`.
   The exported surface is `Config`, `Run`, `Summary`.

### What already exists

| Piece | File | Status | What it does |
|---|---|---|---|
| `hit` | `loadtest/apiCall.go` | ✅ | Fires **one** HTTP request, drains+closes the body, returns `(statusCode int, err error)`. Reports facts, not policy. |
| `percentile` | `loadtest/stats.go` | ✅ | Nearest-rank percentile of a **sorted** `[]time.Duration`; returns 0 for empty. |
| `summarize` | `loadtest/summary.go` | ✅ | Folds `[]result` + elapsed time into a `Summary` (totals, throughput, p50/p90/p99, error map). |
| `Config` | — | ❌ | The exported knobs for a run. **Step 1.** |
| `Run` | — | ❌ | The concurrent engine that drives many `hit`s. **Step 1.** |
| CLI | `cmd/loadtester/main.go` | ❌ | Flags → `Run` → render → exit code. **Step 2.** |

The existing signatures you'll build on:

```go
// loadtest/apiCall.go
func newRunner() *runner
func (r *runner) hit(ctx context.Context, httpMethod, targetURL, token, reqBody string) (int, error)

// loadtest/summary.go
type result struct {            // the per-request record — reuse this as the channel payload
    latency time.Duration
    status  int
    err     error
}
type Summary struct { /* Total, Succeeded, Failed, Elapsed, Throughput, P50, P90, P99, Errors */ }
func summarize(results []result, elapsed time.Duration) Summary
```

### Open review items (don't lose these)

Small `[should-fix]`s already left as inline comments in the code. None blocks Step 1, but
clean them up when you touch those files:

1. **`apiCall.go`** — `io.Copy(io.Discard, resp.Body)` swallows its error; a body-read
   failure mid-response currently reports as a success. Capture and return it.
2. **`summary.go`** — the `status >= 500` failure rule is a magic number. Give it a name
   (e.g. `func isServerError(status int) bool`) and a doc comment stating the policy (5xx =
   server failed; 4xx counts as success because the server correctly rejected the request).
3. **`summary.go`** — `statusErrText` returns `""` for codes outside `http.StatusText`'s
   table (e.g. Cloudflare 520-526). Guard it: fall back to `fmt.Sprintf("HTTP %d", code)`.
4. **`stats.go`** — `percentile`'s doc comment sits *inside* the function body; in Go it
   belongs directly **above** `func percentile`, starting with the word "percentile".
5. **`apiCall_test.go`** — there is **no test that context cancellation aborts an in-flight
   request**. That's the headline feature of a load tester. Add it (see Step 1's cancellation
   test — the same pattern covers both `hit` and `Run`).

---

## 1. STEP 1 — `Config` + `Run`: the concurrent worker pool

**This is the core of the whole project.** Everything before it was setup; this is where
goroutines, channels, and `context` actually appear.

### 1.1 Definition of done

The step is done when:

- `go build ./...` compiles.
- `go test ./...` **and** `go test -race ./...` pass.
- `golangci-lint run` is clean.
- The engine runs end-to-end *through the library* (a test that calls `Run` against an
  `httptest.Server` and gets a correct `Summary` back). No CLI yet — that's Step 2.

### 1.2 Mental model: a closed-loop worker pool

"**Closed-loop**" means: each worker fires a request, **waits** for the response, then fires
the next — there's no fixed external send-rate. With `Concurrency = c`, there are exactly `c`
requests in flight at any moment. (The alternative, "open-loop", sends at a target rate
regardless of how fast responses come back. We are **not** doing that in v1. See the
coordinated-omission note in §1.8.)

The shape is the classic Go **worker pool / pipeline**:

```
                    +------------------+
   producer ----->  |   jobs channel   |  ----> worker 1 --\
  (fills N jobs,     +------------------+        worker 2 ---\        +-------------------+
   then closes)                                   ...   ----> ---->  |  results channel  |
                                                 worker c --/         +-------------------+
                                                                              |
                                                                              v
                                                                 main goroutine drains
                                                                 results -> []result ->
                                                                 summarize(results, elapsed)
```

- **Producer**: sends `Requests` jobs into `jobs`, then **closes `jobs`**.
- **Workers** (`Concurrency` of them): each loops, pulls a job, calls `hit`, **times it**,
  and sends a `result` into `results`.
- **Sink**: the calling goroutine ranges over `results`, collecting them into a `[]result`,
  until `results` is closed; then calls `summarize`.

> A "job" can be as simple as a signal that *one more request* should be sent — every request
> hits the same URL with the same method/body. A `chan struct{}` (zero-size token) is enough.
> You don't need to send rich data on `jobs`; the work is identical each time.

### 1.3 The types

```go
// Config describes a single load-test run. The zero value is not useful on its own
// (URL is required); Run validates it.
type Config struct {
    URL         string        // target URL (required)
    Concurrency int           // number of workers firing at once (-c)
    Requests    int           // total number of requests to send (-n)
    Method      string        // HTTP method; default GET if empty
    Token       string        // optional bearer token
    Body        string        // optional request body (sent as application/json)
    Timeout     time.Duration // per-request timeout; the client already defaults to 30s
}

func Run(ctx context.Context, cfg Config) (Summary, error)
```

Reuse the existing **`result`** struct as the channel payload, and return the existing
**`Summary`**. `Run` is the second piece of your exported API (alongside `Config` and
`Summary`) — give all three **godoc comments** (a full sentence beginning with the
identifier's name).

A few design calls to make deliberately:

- **Validation.** `Run` should reject a bad `Config` by **returning an error** (never
  printing): empty `URL`, `Concurrency <= 0`, `Requests <= 0`. Wrap with `%w` or use
  sentinel errors. Remember: only `main` turns that into a message + exit code.
- **Where the client lives.** `hit` is a method on `runner`, and `runner` holds the **one**
  shared `*http.Client`. Create **one** `runner` per `Run` call (via `newRunner()`) and share
  it across all workers — that's the whole reason the client is on a struct. Do **not** create
  a runner per worker.
- **Method default.** If `cfg.Method == ""`, use `http.MethodGet`.

### 1.4 The concurrency contract (read this twice)

These are the rules that keep the program correct under `-race`. Most concurrency bugs are a
violation of one of them.

1. **One closer per channel.**
   - The **producer** is the only thing that closes `jobs` (after sending all jobs).
   - `results` is closed **only after every worker has finished** — never before. Use a
     `sync.WaitGroup`: each worker `wg.Add(1)` / `defer wg.Done()`, and a separate small
     goroutine does `wg.Wait(); close(results)`. Closing `results` while a worker might still
     send into it is a **panic** ("send on closed channel").

2. **Context cancellation must reach in-flight requests.** Thread `ctx` all the way into
   `hit` (it already takes a `ctx` — good). On Ctrl+C or timeout, an in-flight
   `client.Do(req)` will unblock and return a `context.Canceled` / `DeadlineExceeded` error.
   Workers must **also** stop pulling new jobs once `ctx` is done — use `select` (see §1.6).

3. **Timing wraps the full `hit` call.** Latency is measured *at the worker*, around the call:

   ```go
   start := time.Now()
   status, err := r.hit(ctx, method, url, token, body)
   latency := time.Since(start)
   ```

   This is correct **because the body read (`io.Copy`) happens inside `hit`** — so timing the
   call captures "time to last byte", which is what you want. Don't move the body read out of
   `hit`, or your latency will lie.

4. **No shared mutable state.** Workers don't write to shared maps/slices/counters. They send
   `result` values over `results`; the single sink goroutine does all the aggregation. Share
   memory by communicating, not the other way round. (If you ever *do* need a shared counter,
   it's `sync/atomic` or a `sync.Mutex` — but you shouldn't need one here.)

### 1.5 Goroutine lifecycle — "what stops each one?"

For every goroutine you start, you must be able to answer this. If you can't, it's a leak.

| Goroutine | What stops it |
|---|---|
| **Producer** | It sends exactly `Requests` jobs and returns (closing `jobs`). If `ctx` is cancelled, it should stop early via `select` on `ctx.Done()` so it doesn't block on a full channel forever. |
| **Worker** (×c) | The `for range jobs` loop ends when `jobs` is closed and drained → the worker returns → `wg.Done()`. On cancel, the worker also returns promptly (its `select` sees `ctx.Done()`). |
| **Closer** | `wg.Wait()` returns once all workers are done, then it `close(results)` and returns. |
| **Sink** (the `Run` goroutine itself) | `for range results` ends when `results` is closed. Then it calls `summarize` and returns. |

The deadlock trap: if the **sink never drains `results`**, workers block forever on send, so
`wg.Wait()` never returns, so `results` never closes — everything hangs. The fix is structural:
start the closer goroutine, then have `Run` itself be the sink ranging over `results`. Don't
try to do both "wait for workers" and "drain results" in the same goroutine in the wrong order.

### 1.6 Small illustrative snippets (not the whole thing)

**Worker loop with cancellation.** The nested `select` lets a worker bail the moment `ctx` is
done, even if there are still jobs queued:

```go
func (r *runner) worker(ctx context.Context, cfg Config, jobs <-chan struct{}, results chan<- result) {
    defer wg.Done() // (wg captured from Run, or passed in)
    for {
        select {
        case <-ctx.Done():
            return
        case _, ok := <-jobs:
            if !ok {
                return // jobs closed and drained
            }
            start := time.Now()
            status, err := r.hit(ctx, method, cfg.URL, cfg.Token, cfg.Body)
            results <- result{latency: time.Since(start), status: status, err: err}
        }
    }
}
```

**Close `results` only after all workers finish:**

```go
var wg sync.WaitGroup
for range cfg.Concurrency {
    wg.Add(1)
    go r.worker(ctx, cfg, jobs, results)
}
go func() {
    wg.Wait()
    close(results)
}()
```

**Producer that respects cancellation:**

```go
go func() {
    defer close(jobs)
    for range cfg.Requests {
        select {
        case <-ctx.Done():
            return
        case jobs <- struct{}{}:
        }
    }
}()
```

**Sink (inside `Run`):**

```go
start := time.Now()
// ... start producer, workers, closer ...
var results []result
for res := range resultsCh {
    results = append(results, res)
}
return summarize(results, time.Since(start)), ctx.Err() // ctx.Err() is non-nil if cancelled
```

> These are sketches to show the *shape*. Wire them together yourself, name things your way,
> and let the test drive the details.

### 1.7 TDD order

1. **Write `Config`** (the struct + godoc). Compiles, nothing uses it yet.
2. **Write a failing `Run` test** against an `httptest.Server` that returns 200. Small
   numbers, e.g. `Concurrency: 4, Requests: 20`. Assert on the `Summary`:
   `Total == 20`, `Succeeded == 20`, `Failed == 0`, `len(Errors) == 0`,
   `Throughput > 0`, and `P50/P90/P99 > 0`. Don't assert exact latency numbers (they're
   real timings) — assert *relationships* and *counts*.
3. **Implement `Run`** until that test passes. Keep it minimal.
4. **Add a cancellation test.** Stand up a **slow** handler (`time.Sleep` *in the test
   server*, not in your code) that takes, say, 200ms. Start `Run` with a `ctx` you cancel
   after ~20ms (`context.WithTimeout` or `context.WithCancel` + a timer). Assert that `Run`
   **returns quickly** (well under the time it would take to finish all requests) and that the
   returned error satisfies `errors.Is(err, context.Canceled)` (or `DeadlineExceeded`). This
   is the test that proves cancellation reaches in-flight requests — the single most important
   test in the project.
5. **Run `go test -race ./...`.** This is where unsynchronized access to a shared map/slice or
   a bad channel close shows up. The race detector is the only thing that catches these
   reliably — a green run *without* `-race` proves nothing about concurrency.
6. **Add a leak check** with `go.uber.org/goleak` (test-only dependency). In a
   `TestMain`, `goleak.VerifyTestMain(m)` fails the package if any goroutine outlives the
   tests — i.e. it catches a leaked worker/producer. This is the **one** third-party dep worth
   pulling in; justify it in the commit as "test-only leak detection, no production
   dependency." If you'd rather stay strictly stdlib, you can skip it and lean on `-race` +
   careful lifecycle reasoning, but goleak is the idiomatic choice here.

### 1.8 Edge cases to test

- **`Requests == 0`** → an empty run. Producer closes `jobs` immediately, no workers do work,
  `summarize` gets an empty slice. Expect a zero-value-ish `Summary` (and remember
  `Errors` should be a non-nil empty map — `summarize` already does `make(map...)`).
- **`Concurrency > Requests`** (e.g. 50 workers, 10 requests) → extra workers must exit
  cleanly when `jobs` drains, not hang. (`-race` + goleak will catch it if they don't.)
- **An all-failing target** (server returns 500, or a closed server for transport errors) →
  `Failed == Requests`, `Succeeded == 0`, the `Errors` map populated, percentiles 0.
- **Cancellation mid-run** → covered by the test in §1.7 step 4.

### 1.9 Pitfalls (the classics)

- **Closing `results` too early** → "send on closed channel" panic. Only close after
  `wg.Wait()`.
- **Closing `jobs` from a worker** → multiple closers, panic. Only the producer closes `jobs`.
- **Forgetting to drain `results`** → deadlock (workers block on send forever). `Run` must be
  the sink.
- **Leaking workers on cancel** → a worker blocked on `results <- ...` while the sink has
  stopped reading. As long as the sink keeps ranging until `results` closes, this won't
  happen — but it's why the `select`/close ordering matters.
- **Coordinated omission (note for later, not v1).** Because this is closed-loop, if the
  server stalls, your tool simply sends fewer requests — it does **not** record the requests
  it *would* have sent as slow. That under-reports tail latency. It only becomes a real
  concern if you later add an open-loop `-rate` flag; flag it then, don't fix it now.

### 1.10 Commands for this step

```
go test -v -run TestRun ./loadtest/     # iterate on just the Run tests
go test ./...                           # full suite
go test -race ./...                     # the one that matters for concurrency
golangci-lint run                       # keep clean before calling it done
```

---

## 2. STEP 2 — `cmd/loadtester/main.go`: the CLI front end

Now make it runnable from a terminal. **`main` is glue and nothing more**: parse flags,
validate, build a `Config`, call `Run`, render the `Summary`, set the exit code. All real
logic stays in the `loadtest` package.

### 2.1 Definition of done

```
go run ./cmd/loadtester -url http://localhost:8080 -c 50 -n 1000
```

…runs end-to-end and prints a readable report, exits 0 on success and non-zero on a bad
flag or a `Run` error. `go build ./cmd/loadtester` compiles; lint is clean.

### 2.2 The shape of `main`

```
parse flags
  -> validate (url set, c > 0, n > 0); on bad input: print usage to stderr, exit 2
  -> build loadtest.Config from the flags
  -> ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt); defer stop()
  -> summary, err := loadtest.Run(ctx, cfg)
       on err: print to stderr, exit 1
  -> render summary to stdout
  -> exit 0
```

> Idiom: keep `main()` tiny and push the work into a `run() error` function, then
> `if err := run(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }`. That keeps the
> single `os.Exit` in exactly one place and makes the rest testable.

### 2.3 Flags

Use the stdlib `flag` package.

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `-url` | string | `""` | Target URL. **Required.** |
| `-c` | int | `10` | Concurrency — workers firing at once. |
| `-n` | int | `100` | Total number of requests. |
| `-method` | string | `GET` | HTTP method. |
| `-token` | string | `""` | Optional bearer token. |
| `-body` | string | `""` | Optional request body (sent as JSON). |
| `-timeout` | duration | `30s` | Per-request timeout. |

Validation lives in `main` (or in `Run` returning an error that `main` reports). Either way,
the **message and the exit code come from `main`**, never from the library.

### 2.4 Signal handling (Ctrl+C)

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
summary, err := loadtest.Run(ctx, cfg)
```

`signal.NotifyContext` cancels `ctx` when the user hits Ctrl+C. Because you threaded `ctx`
into the workers and into `hit` in Step 1, that cancellation reaches in-flight requests and
the run winds down cleanly — and you can still print a partial `Summary`. This is the payoff
for getting the context plumbing right.

### 2.5 Rendering the Summary

A plain, readable block is plenty for v1:

```
Requests:   1000 (succeeded 994, failed 6)
Duration:   4.12s
Throughput: 241.3 req/s
Latency:    p50 28ms   p90 109ms   p99 120ms
Errors:
  connection refused        4
  internal server error     2
```

Notes:
- Only `main` prints. If you want this testable, extract a
  `func render(w io.Writer, s loadtest.Summary)` and unit-test it with a `bytes.Buffer`
  (accept an `io.Writer`, don't hard-code `os.Stdout`).
- Throughput is a `float64` req/s — format with `%.1f` or similar.
- The percentiles are `time.Duration` — they print human-readably on their own
  (`28ms`), so `%v` is fine.

### 2.6 Exit codes

- `0` — ran and reported.
- `1` — `Run` returned an error (validation, or it couldn't even start).
- `2` — bad flags / usage (this is what `flag.Parse` uses by convention).

(Optional, your call: a non-zero exit if `summary.Failed > 0`, like a health gate. Decide
deliberately and document it in `-help`.)

### 2.7 Safety warning

This tool generates load. Put a line in the usage/`-help` text (mirroring the README):

> Only point this at systems you own or have explicit permission to test.

`flag.Usage` can be overridden to print that banner above the flag list.

### 2.8 Testing

`main` itself is mostly untestable glue and that's fine — the logic it calls is already
tested in the library. The one worthwhile unit test is the extracted `render` function
(deterministic input `Summary` → expected text), since formatting is real logic that can
regress.

### 2.9 Commands for this step

```
go build ./cmd/loadtester
go run ./cmd/loadtester -url http://localhost:8080 -c 50 -n 1000
go test ./...
golangci-lint run
```

After Step 2, that's v1. Per the project rules, tag `v0.1.0` and turn any new ideas into
GitHub issues rather than growing the current branch.

---

## 3. Appendix

### 3.1 Concurrency glossary (terms used above)

- **Goroutine** — a lightweight thread managed by the Go runtime; started with `go f()`.
- **Channel** — a typed pipe to pass values between goroutines (`chan T`). Sending/receiving
  blocks until the other side is ready (unless buffered).
- **`select`** — waits on multiple channel operations at once; runs whichever is ready first.
  Used here to race "got a job" against "context cancelled".
- **`sync.WaitGroup`** — a counter to wait for a set of goroutines to finish (`Add`/`Done`/
  `Wait`). Used to know when it's safe to close `results`.
- **`context.Context`** — carries cancellation/deadline across API boundaries. Cancelling it
  signals every goroutine (and `http.Client.Do`) that holds it to stop.
- **Closed-loop vs open-loop** — closed-loop: a fixed number of in-flight requests, each
  worker waits for a response before sending the next (what v1 does). Open-loop: send at a
  target rate regardless of responses.
- **Coordinated omission** — the measurement bias where a stalled server makes a closed-loop
  tester *send fewer* requests instead of recording the would-be-slow ones, hiding tail
  latency. Only relevant if a `-rate` flag is added later.

### 3.2 Command cheat-sheet

```
go build ./cmd/loadtester                       # build the CLI
go run ./cmd/loadtester -url <target> -c 50 -n 1000   # run it
go test ./...                                   # all tests
go test -race ./...                             # tests + race detector (run this!)
go test -v -run TestRun ./loadtest/             # one test while iterating
go test -cover ./...                            # coverage %
golangci-lint run                               # full linter (keep clean)
go vet ./...                                     # cheap correctness checks
```

### 3.3 Pending review items (carry-over checklist)

- [ ] `apiCall.go` — capture the `io.Copy` error instead of discarding it.
- [ ] `summary.go` — name the `>= 500` policy (`isServerError`) + doc comment.
- [ ] `summary.go` — guard `statusErrText` against `http.StatusText` returning `""`.
- [ ] `stats.go` — move `percentile`'s doc comment above the function signature.
- [ ] `apiCall_test.go` — add the ctx-cancellation test (the Step 1 §1.7-4 pattern covers it).
