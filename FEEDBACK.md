# Code Review — HTTP Load Tester

A senior-Go-engineer pass over the whole `loadtest` package and its tests, written the way
you asked for it in `AGENTS.md`: direct, ranked by severity, explaining the *why* and the Go
idiom behind each point — not just the *what*.

**How to read this file**

- Findings are tagged `[blocker]`, `[should-fix]`, `[nit]`, most-serious first.
- Each finding has three parts: **Why it bites** (plain English + the underlying principle),
  **Example** (a concrete scenario where it goes wrong), and **Direction to fix** (the idiom
  to reach for and a *tiny* illustrative sketch — never the finished function, so you write
  the actual code yourself and learn it).
- Anything I could prove by running the tool, I proved. Those findings link to
  `STRESS_TEST_REPORT.md`, where a throwaway harness drove your library until it broke.
- I did **not** touch any `.go` file. If you'd rather have these as inline
  `[blocker]/[should-fix]/[nit]` comments at each line (your usual convention), say so and
  I'll add comment-only notes.

**Overall verdict.** The hard part — the concurrency — is *correct*. Channel ownership,
close ordering, `Add`-before-`Wait`, and cancellation reaching in-flight requests are all
right, and your tests prove it under `-race` + `goleak` (I re-ran them: green). The findings
below are almost entirely about **behaviour under failure and at scale**, not the happy path.
That's the right kind of problem to have at this stage.

---

## What's already good (kept short, per your "no padding" rule)

- **Channel lifecycle is textbook.** Exactly one closer per channel: the producer closes
  `jobs` (`run.go:83`), the `WaitGroup` closer alone closes `results` (`run.go:101-104`),
  and the calling goroutine alone drains and appends. No double-close, no send-on-closed.
- **`Add` happens before `Wait`.** All `wg.Add(1)` calls finish in the launch loop
  (`run.go:94-99`) before the closer goroutine starts and calls `wg.Wait()`. That ordering
  is the whole ballgame for `WaitGroup` correctness, and you got it right.
- **The unguarded `results <- ...` send (`run.go:38`) is actually safe** — and it's worth
  understanding *why*, because it looks like a bug. It's safe only because the caller drains
  `results` unconditionally (`for res := range results`) until the channel is closed, and it
  is closed only after every worker has exited. So no worker can ever block forever on that
  send. Keep that invariant in mind if you ever change how the caller consumes results.
- **Cancellation reaches every layer**: the producer's `select` (`run.go:85-89`), the
  worker's `select` (`run.go:29-39`), and the HTTP request via `NewRequestWithContext`
  (`apiCall.go:50`). Proven in `STRESS_TEST_REPORT.md` §7: cancel mid-run, workers drain to
  the baseline goroutine count, `Run` returns `context.Canceled` in ~150 ms.
- **Percentile math is right.** Nearest-rank with `ceil(p·n) − 1`, clamped, empty → 0. It
  matches every case in your table test, including the boundaries.
- Body is drained *and* closed on the success path for connection reuse (`apiCall.go:66-68`);
  one shared, tuned client (`apiCall.go:33-42`); a client timeout is always set.

---

## `[blocker]`

### 1. A truncated response is counted as a success — `apiCall.go:68`

```go
_, _ = io.Copy(io.Discard, resp.Body)
return resp.StatusCode, nil
```

**Why it bites.** You throw away the error `io.Copy` returns. Getting a status line and
getting the *whole body* are two separate events that can fail independently: the server can
send `200 OK` and its headers, then the connection can die halfway through the body. When
that happens `io.Copy` returns an error, but you ignore it and return `(200, nil)`. A load
tester's entire job is to tell the truth about a server; silently reporting a broken download
as a success is the worst kind of bug, because the numbers *look* fine. The Go principle
here is blunt: **an ignored error is a decision to pretend nothing went wrong** — and
`_, _ =` is you writing that decision down twice.

**Example (measured, not hypothetical).** I stood up a server that sends `200 OK` with
`Content-Length: 1048576` and then writes 5 bytes and hangs up. `STRESS_TEST_REPORT.md` §2:
your tool reported **200 succeeded, 0 failed**. A plain `io.Copy` over the *exact same
response* returns `unexpected EOF`. So the failure is real and detectable — the code just
isn't looking.

**Direction to fix (not the answer).** `io.Copy` already hands you what you need — its shape
is `n, err := io.Copy(dst, src)`. The real design questions are yours to answer:
1. When that `err` is non-nil, what should `hit` return? You already return `(0, err)` for
   transport failures on line 64 — a body-read failure is the same *category* of event.
2. Does `summarize` (`summary.go:52`) then need any change, or does routing a non-nil `err`
   through the path it already has handle it for free? Trace one truncated result through
   `summarize` on paper and see.
Then write the deterministic test the docs already call for: a handler that advertises a big
`Content-Length` and writes a short body (a hijacked raw connection is the most reliable way
to force it — that's what the stress harness does).

---

## `[should-fix]`

### 2. Memory grows without bound — O(number of requests) — `run.go:106-109`

```go
var collectedResult []result
for res := range results {
    collectedResult = append(collectedResult, res)
}
```

**Why it bites.** You hold *every* result for the *entire* run in one slice, then summarize
at the end. A load tester is precisely the tool people point at huge request counts, so its
memory should be flat in `Requests`, not linear. Right now it's linear: each `result` is
~32 bytes, and `append`'s growth-by-doubling means the live peak is roughly double that
again. The principle: **for a streaming workload, aggregate as data arrives; don't buffer
the whole stream just to fold it once at the end.**

**Example (measured).** `STRESS_TEST_REPORT.md` §3 — peak heap climbs straight-line with N:
~10 MiB at 100 k requests, ~117 MiB at 1 M, ~227 MiB at 2 M (~110 bytes/req at peak). Extend
that line and this 16 GB machine runs out of heap somewhere around 10^8 requests — for a tool
whose whole purpose is high request counts, that's a ceiling you'll hit.

**Direction to fix (not the answer).** Notice what the final `Summary` actually *needs*:
counts, an error map, and the **latencies of successful requests** (for percentiles).
Nothing needs the full per-request `result` after it's been folded in. So the shape to aim
for is "fold each result as it arrives," something like:

```go
for res := range results {
    // update Succeeded/Failed, bump the error map,
    // and append only the success latency you'll need later
}
```

That drops you from "one `result` struct per request" to "one `time.Duration` per
*successful* request." If you want truly flat memory, the next step is a latency **histogram**
(fixed buckets) instead of a slice — but then percentiles become approximate. That
exact-vs-approximate, memory-vs-accuracy trade is worth a paragraph in your own notes; decide
it deliberately rather than by accident.

### 3. The "grouped" error map doesn't actually group — `summary.go:53`

```go
summary.Errors[res.err.Error()]++
```

**Why it bites.** You key the map on the *full error string*. Many Go network errors embed a
value that changes on every connection — most notably the client's local ephemeral port. So
"the same failure" produces a *different* string every time, and your map that's supposed to
*group* errors instead grows one entry per failed request. That's a second unbounded memory
sink (on top of finding #2) *and* it destroys the feature's usefulness: a human reading
5 000 near-identical rows learns nothing. Principle: **group by the error's identity (its
kind), not by its rendered message.** The message is for humans; the kind is for counting.

**Example (measured).** `STRESS_TEST_REPORT.md` §6:
- Point it at a refused port → 5 000 failures collapse to **2** keys. Good, because the
  refused-connection message only contains the fixed *target* address.
- Point it at a server that resets connections → 5 000 failures produce **5 000** distinct
  keys, each differing only by the local port:
  `read tcp 127.0.0.1:49164->...`, `...49166->...`, `...49168->...`. The map is now as big as
  the failure count and completely unreadable.

**Direction to fix (not the answer).** The idiom is to *classify* before you count. Reach for
the `errors` package and the typed errors the stdlib gives you — `net.Error` (has
`Timeout()`), `context.DeadlineExceeded`/`context.Canceled`, `*net.OpError` (its `.Op` is
"dial"/"read"/"write"), `syscall.ECONNREFUSED`, and so on:

```go
var netErr net.Error
if errors.As(err, &netErr) && netErr.Timeout() { /* one bucket: "timeout" */ }
```

Map each failure to a small, stable label ("dial: connection refused", "read: connection
reset", "timeout") and count *those*. Bounded map, readable output.

### 4. Malformed URLs aren't rejected up front — `run.go:43`

**Why it bites.** `validateConfig` only rejects an *empty* URL. A non-empty but broken one
("nope", `http://` with no host, `ftp://x`) passes validation, and every worker then fails
its request identically — you turn one config mistake into N runtime errors and waste the
whole run before the user learns they typo'd the URL. Principle: **validate at the boundary
and fail fast**; don't defer a knowable-up-front error into the hot loop.

**Example.** `Run(ctx, Config{URL: "htpp://localhost", ...})` (note the typo) today spins up
all your workers, fires every request, and hands back a summary full of identical "unsupported
protocol scheme" errors instead of one clear "invalid url" *before* any work starts.

**Direction to fix (not the answer).** `net/url` is the tool. `url.Parse` alone is too
lenient (it accepts a lot), so the useful check is parse *plus* assert the parts you require:

```go
u, err := url.Parse(cfg.URL)
if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
    // reject here, in validateConfig
}
```

Decide which schemes/shapes you consider valid and enforce them next to your other checks.
(The same fail-fast thinking applies to `Method`, though `NewRequestWithContext` already
rejects genuinely invalid method tokens — lower priority.)

### 5. You spawn `Concurrency` workers even when there's less work than that — `run.go:94`

```go
for i := 1; i <= config.Concurrency; i++ { ... }
```

**Why it bites.** If `Concurrency` exceeds `Requests`, the extra workers start, immediately
read the already-closed `jobs` channel, and exit having done nothing. Correct, but wasteful —
and a load tester invites people to pass big concurrency numbers. Principle: **don't create
workers you can prove will have no work.**

**Example (measured).** `STRESS_TEST_REPORT.md` §4: `Concurrency=500000, Requests=5` creates
half a million goroutines to perform *five* requests. Peak *simultaneous* goroutines stays
low (they exit as fast as they're made), so it doesn't blow memory — but the pure creation
churn costs **152 ms** versus **1.6 ms** for `Concurrency=5`. That's ~100× wall-clock time
spent creating goroutines that do nothing.

**Direction to fix (not the answer).** One line of arithmetic before the launch loop: the
number of workers that can ever be busy is `min(Concurrency, Requests)`. Go 1.21+ has a
builtin `min`. Cap the loop bound and the waste disappears.

### 6. (test) A test case carries expectations it never checks — `run_test.go:52-67`

**Why it bites.** The "empty get method" case sets both `wantErrContains: "invalid method"`
*and* `want: {Total: 1, Succeeded: 0, Failed: 1}`. But the test body only checks `want` in the
`else` branch — the one that runs when *no* error is expected (`run_test.go:177-201`). So
those `Total/Failed` numbers are **never asserted**. Worse, they're *misleading*: they imply
"one request ran and failed," when in reality validation rejects the config and returns
`Summary{}` before a single request fires. A future reader (or you in six months) will trust
those numbers. Principle: **a test should assert everything it states, and state nothing it
doesn't assert.**

**Direction to fix (not the answer).** Drop the `want` payload from every error-expecting
case (leave just `cfg` + `wantErrContains`). If you want to be strict, also assert that on the
error path the returned summary is the zero value — that documents the real contract ("invalid
config ⇒ `Summary{}`, no requests sent") instead of implying a phantom failed request.

### 7. (test) The `[blocker]` from #1 has no test — the documented next step

**Why it bites.** `AI_PROJECT_CONTEXT.md` itself lists "add a deterministic test whose
response body returns data followed by a read error" as the next milestone, and it's missing.
A blocker without a regression test will silently come back.

**Direction to fix (not the answer).** You need a handler whose body read is *guaranteed* to
fail. A normal `httptest` handler won't do it reliably; the deterministic trick is to hijack
the connection (`w.(http.Hijacker)`), write a status line with a `Content-Length` larger than
the bytes you then write, and close. The client sees the status, then `unexpected EOF` on the
body. The stress harness in `STRESS_TEST_REPORT.md` §2 does exactly this — use it as the model
for the real unit test once you've made the body-read error observable.

---

## `[nit]`

### 8. Unbuffered `jobs` and `results` channels — `run.go:76-77`
Fine for now, because network I/O dwarfs channel-handoff cost. But be aware it's a real
ceiling: a single producer hands one token at a time, and a single consumer drains one result
at a time, so every request pays two rendezvous. `STRESS_TEST_REPORT.md` §5 shows throughput
*falling* as concurrency rises against a trivial local handler (77 k/s at C=200 → 43 k/s at
C=1000) — more workers, less throughput, because the coordination and connection setup, not
the "server", is the bottleneck. Worth understanding before you ever add a `-rate` flag.

### 9. `percentile` trusts that its input is sorted — `stats.go:8`
The contract ("must be pre-sorted") lives only in the parameter *name*, `sortedDurations`.
It's safe today because the one caller sorts first (`summary.go:64`), but a future second
caller that forgets will get silently wrong percentiles with no error. Either document the
precondition in a doc comment, or (defensive but simple) note that an unexported helper with a
name-only contract is a small latent trap.

### 10. Unchecked type assertion can panic — `apiCall.go:34`
```go
t := http.DefaultTransport.(*http.Transport).Clone()
```
If anything ever reassigns `http.DefaultTransport` (some libraries do, in tests), this panics
— and your own `AGENTS.md` says library code must not panic. The comma-ok form
(`t, ok := ...`) lets you fall back to a fresh `&http.Transport{}` instead of crashing.

### 11. Validation errors format strings with `%v` — `run.go:45-57`
`fmt.Errorf("invalid url -> %v", cfg.URL)`. For a *string* value, `%q` is better: it quotes
and reveals whitespace, so `"  "` and `""` are distinguishable in the message. Small, but it's
exactly the kind of thing that saves you when debugging a "why is my URL invalid" report.

### 12. `Run` returns `ctx.Err()` unconditionally — `run.go:111`
Almost always right. The subtle edge: if the caller's context is canceled in the tiny window
*after* all requests finished but *before* `Run` returns, the caller gets a complete, correct
`Summary` *and* a `context.Canceled` error — which reads as "the run was interrupted" when it
wasn't. Worth a line of thought about whether a fully-drained run should ever report
cancellation.

### 13. Filename `apiCall.go` is camelCase
Go filenames are conventionally all-lowercase (`client.go`, `request.go`, or `apicall.go`).
Not a functional issue; just stands out in a package that's otherwise idiomatic.

### 14. No benchmark anywhere
For a *performance* tool, there's no `func Benchmark...(b *testing.B)`. You've got no
repeatable number for "how much load can the harness itself generate before it's the
bottleneck." A benchmark around `hit` (against an `httptest` server) or around `summarize`
would give you that baseline and catch regressions.

---

## Suggested order to tackle

1. **#1** (body-read blocker) + **#7** (its test) — correctness of the core result. Do these
   together; the docs already point you here.
2. **#3** (error-map grouping) and **#2** (streaming aggregation) — both are "unbounded at
   scale," both surfaced in the stress test, and #2's refactor is the natural place to also
   fix #3.
3. **#4** (URL validation) and **#5** (worker cap) — cheap, high-clarity wins.
4. **#6** (test cleanup), then the nits as you pass through each file.

Everything in §"What's already good" — leave it alone; it's right.
