# Stress Test Report — HTTP Load Tester

I drove your load tester hard against local target servers to find where it breaks. This is
the record of what I ran, the numbers I measured, and what each result means — cross-linked to
the matching finding in `FEEDBACK.md`.

## Method

- **The CLI doesn't exist yet.** `cmd/loadtester/main.go` is just `package main` and fails to
  build ("function main is undeclared"), so there's nothing to `go run`. I exercised the tool
  through its public library API instead — `loadtest.Run(ctx, Config{...})` — which is the
  same engine the CLI will eventually call.
- **Harness location:** a throwaway module in a scratchpad directory with its own `go.mod`
  and a `replace` directive pointing at this repo. It imports only `loadtest` and stands up
  local `net/http` targets. **Nothing was added to your repo** — `git status` shows only the
  two new Markdown files.
- **Baseline before stressing:** `go vet ./...` clean, `go test -race -count=1 ./...` green
  (including the `goleak` check). So every break below is something the unit tests don't yet
  exercise, not a pre-existing red suite.
- **Machine:** Go 1.26.3, darwin/arm64, 10 cores, `GOMAXPROCS=10`, 16 GB RAM, `ulimit -n`
  = 1,048,576. That high fd limit matters: file descriptors are *not* the first wall here —
  memory and ephemeral TCP ports are.
- **Instrumentation per run:** wall time, throughput, the full `Summary`, correctness
  invariants (`Total == Requests`, `Succeeded + Failed == Total`, `P50 ≤ P90 ≤ P99`),
  goroutine count (before / sampled peak / after), and peak heap sampled every 15 ms.

## Executive summary

| # | What I pushed | Result | Verdict |
|---|---|---|---|
| 1 | C=50, N=20 000, fast handler | 110 k req/s, invariants hold | ✅ healthy |
| 2 | Server truncates the body after `200 OK` | Reported **200 ok / 0 failed** | 🔴 **wrong result** — `FEEDBACK.md` #1 |
| 3 | N = 100 k → 2 M | Heap grows **linearly**, ~110 B/req, to 227 MiB | 🟠 unbounded — `FEEDBACK.md` #2 |
| 4 | C=500 000, N=5 | 500 k goroutines made for 5 requests; 152 ms | 🟠 wasteful — `FEEDBACK.md` #5 |
| 5 | Concurrency 200 → 10 000 | **Collapses at C≈2 000**: port exhaustion, throughput 43 k→1.4 k/s | 🔴 **hard wall** — `FEEDBACK.md` #8 |
| 6 | Failing targets (refused / reset) | Reset errors → **5 000 unique map keys** | 🟠 map explodes — `FEEDBACK.md` #3 |
| 7 | Cancel mid-run under load | `context.Canceled` in 157 ms, goroutines 2→peak 1006→**2** | ✅ clean, no leak |

The one-line story: **the concurrency engine is solid, but the tool lies about truncated
responses, grows memory without bound, and falls off a cliff at high concurrency against
localhost.**

---

## §1 — Baseline sanity ✅

`C=50, N=20 000`, fast null handler.

```
elapsed=184ms  throughput=110004 succ-req/s
total=20000 succeeded=20000 failed=0  errKinds=0
latency p50=269µs p90=717µs p99=1.305ms
goroutines: before=2 peak=294 after=236
heap: peak=5.2MiB
invariants: Total==Requests ✓  Succ+Fail==Total ✓  P50<=P90<=P99 ✓
```

Everything holds. ~110 k successful req/s through a 50-worker closed loop, sane percentile
ordering, flat memory at this size. The `after=236` goroutines (vs `before=2`) are **idle
keep-alive connections** on both sides of `httptest`, not a leak — §7 and the `goleak` test
both confirm the engine itself returns to baseline.

---

## §2 — Truncated response counted as success 🔴 (proves `FEEDBACK.md` #1)

Target: a hijacked connection that sends `HTTP/1.1 200 OK` with `Content-Length: 1048576`,
writes 5 bytes, and closes. `C=4, N=200`.

```
loadtest reports: succeeded=200 failed=0 errKinds=0
control: manual io.Copy over the SAME response returns err=unexpected EOF
>> CONFIRMED: body-read failure is invisible to loadtest (0 failures) but real.
```

Every one of the 200 broken downloads was reported as a **success**. To rule out "maybe the
body really was fine," the harness does a plain `io.Copy` over the identical response and gets
`unexpected EOF`. So the failure is real and trivially detectable — `apiCall.go:68`
(`_, _ = io.Copy(...)`) just discards the error. This is the highest-impact bug in the tool:
the failure mode is *silent* and the reported numbers look perfect.

---

## §3 — Memory grows linearly with request count 🟠 (proves `FEEDBACK.md` #2)

`C=50`, fast handler, increasing N. Peak heap sampled during each run:

| Requests | Peak heap | Attributable | Bytes / request |
|---:|---:|---:|---:|
| 100 000 | 10.5 MiB | 10.3 MiB | 108 |
| 500 000 | 39.2 MiB | 37.6 MiB | 79 |
| 1 000 000 | 119.9 MiB | 117.1 MiB | 123 |
| 2 000 000 | 230.6 MiB | 226.8 MiB | 119 |

The line is straight: doubling N roughly doubles peak heap. That's `collectedResult`
(`run.go:106`) holding one `result` per request for the whole run. The underlying struct is
~32 bytes, but `append`'s doubling plus GC timing pushes the live peak to ~110 bytes/req.
Extrapolated, this 16 GB machine exhausts heap somewhere around **10^8 requests** — a real
ceiling for a tool built to send lots of requests. (Per plan, I measured the curve and
extrapolated rather than forcing an actual OOM.)

---

## §4 — Worker over-provisioning 🟠 (proves `FEEDBACK.md` #5)

`Requests=5`, escalating `Concurrency`. Isolated process for clean numbers.

| Concurrency (= workers created) | Peak *simultaneous* goroutines | Elapsed |
|---:|---:|---:|
| 5 | ~0 (too fast to sample) | 1.579 ms |
| 10 000 | 81 | 3.837 ms |
| 100 000 | 129 | 31.22 ms |
| 500 000 | 165 | 152.364 ms |

The interesting nuance: **peak simultaneous goroutines stays tiny** even at C=500 000, because
each surplus worker reads the already-closed `jobs` channel and exits immediately — they never
pile up. So this is *not* a memory blow-up. The cost is pure **creation churn**: making half a
million goroutines to perform five requests' worth of work takes 152 ms versus 1.6 ms for
C=5 — ~100× wall time wasted. A `min(Concurrency, Requests)` cap erases it.

---

## §5 — High-concurrency socket pressure 🔴 (the headline breakage; `FEEDBACK.md` #8)

Fast null handler, escalating concurrency, `N = 20·C` (capped at 100 k), 10 s timeout.

| Concurrency | Requests | Elapsed | Succeeded | Failed | Throughput |
|---:|---:|---:|---:|---:|---:|
| 200 | 4 000 | 52 ms | 4 000 | 0 | 77 326/s |
| 500 | 10 000 | 166 ms | 10 000 | 0 | 60 363/s |
| 1 000 | 20 000 | 461 ms | 20 000 | 0 | 43 512/s |
| **2 000** | 40 000 | **22.5 s** | 32 096 | **7 904** | **1 426/s** |
| 5 000 | 100 000 | 100 s | 44 748 | 55 252 | 447/s |
| 10 000 | 100 000 | 113 s | 5 767 | 94 233 | 51/s |

**The wall is between C=1 000 and C=2 000.** Two things happen at once:

1. **Throughput *drops* as concurrency rises, well before any failures** — 77 k/s at C=200
   down to 43 k/s at C=1 000. Against a handler this cheap, the bottleneck is the tool's own
   coordination and connection setup, not the "server." More workers make it *slower*. (This
   is the unbuffered-channel / closed-loop point, `FEEDBACK.md` #8.)
2. **At C≈2 000 it collapses** into ephemeral-port exhaustion. The dominant error:
   ```
   dial tcp 127.0.0.1:50197: connect: can't assign requested address   (×7 424 at C=2000)
   context deadline exceeded (Client.Timeout exceeded while awaiting headers)   (×480)
   ```
   With `MaxIdleConnsPerHost = 100` (`apiCall.go:15`), any concurrency above ~100 forces
   connections to be opened and closed faster than the OS can recycle ports; the closed ones
   sit in `TIME_WAIT` and the ephemeral-port range (~16 k ports on macOS) drains. Throughput
   falls off a cliff — from 43 k/s to 1.4 k/s — and stays there.

This is the classic localhost load-testing trap, and it's worth documenting for users: past a
few hundred workers against one host you measure *your own machine's socket limits*, not the
target. It also argues for making `MaxConnsPerHost` / the idle settings configurable and for a
`--help` note about it.

---

## §6 — Error-map grouping 🟠 (proves `FEEDBACK.md` #3)

Two failing targets, `C=50, N=5 000` each.

**(a) Connection refused** — nothing listening on the port:
```
failed=5000  distinct error keys=2
  (×4998) ... dial tcp 127.0.0.1:61306: connect: connection refused
  (×2)    ... dial tcp 127.0.0.1:61306: connect: invalid argument
```
Groups beautifully (2 keys) — because the message contains only the *fixed* target address.

**(b) Connection reset** — a server that accepts and resets each connection:
```
failed=5000  distinct error keys=5000   (ideal grouping would be ~1)
  (×1) ... read tcp 127.0.0.1:49164->127.0.0.1:64285: read: connection reset by peer
  (×1) ... read tcp 127.0.0.1:49166->127.0.0.1:64285: read: connection reset by peer
  (×1) ... read tcp 127.0.0.1:49168->127.0.0.1:64285: read: connection reset by peer
```
**One map entry per failed request.** The only difference between keys is the client's local
port (49164, 49166, 49168…), which changes every connection. So keying on `err.Error()`
(`summary.go:53`) turns "5 000 identical failures" into 5 000 unreadable rows — and a second
unbounded memory sink alongside §3. Classify by error *kind* instead (`FEEDBACK.md` #3).

---

## §7 — Cancellation under load ✅ (no leak)

Slow handler that blocks until the request context is done. `C=200, N=1 000 000`, cancelled
~150 ms in. Isolated process.

```
returned after 157ms: err=context canceled
partial summary: total=200 (of 1000000 requested) succeeded=0 failed=200
goroutines: before=2 peak=1006 after=2
```

Exactly the intended behaviour, and it holds up under load:
- **Stops promptly** — returns 157 ms after a 150 ms cancel, not after draining a million jobs.
- **Returns a partial `Summary`** (200 completed of 1 M requested) plus `context.Canceled` —
  the documented contract.
- **No goroutine leak.** ~1 006 goroutines at peak (200 workers + server-side connections),
  back to the baseline **2** afterward. This corroborates the `goleak` test: the engine's
  cancellation and shutdown paths are correct even when interrupted mid-flight.

The 200 in-flight requests are recorded as failures (their `context.Canceled` errors), which
is reasonable — though note it interacts with `FEEDBACK.md` #3: cancellation errors also carry
per-connection detail, so a cancel during a huge run adds many map keys too.

---

## Where it breaks — the short version

1. **Correctness:** truncated / mid-body-failed responses are silently counted as successes
   (§2). Fix first — it undermines every number the tool produces.
2. **Scale (memory):** heap is O(requests) and the error map is O(distinct-failure-strings),
   which under connection resets is O(failures) (§3, §6). Both are unbounded.
3. **Scale (sockets):** against a single localhost target the tool collapses at C≈2 000 from
   ephemeral-port exhaustion, and throughput actually *declines* with concurrency long before
   that (§5). Partly physics, partly the fixed `MaxIdleConnsPerHost` and closed-loop design.
4. **Waste:** `Concurrency > Requests` creates goroutines that do nothing (§4).

## What held up under stress

- Concurrency correctness: no deadlock, no race, no goroutine leak — even mid-cancellation
  under a million-request configuration (§7), matching the `-race`/`goleak` baseline.
- Result invariants (`Total`, `Succeeded+Failed`, percentile ordering) held in every run where
  requests completed (§1, §5).
- Prompt, correct cancellation with a partial summary (§7).

---

*Reproduce:* the harness lives in the session scratchpad
(`scratchpad/stress/`, module `stress`, `replace` → this repo). Run `go run . <scenario#>` or
with no args for all seven. It is intentionally outside the repo, per the `AGENTS.md` rule
that implementation/test code stays yours to write.
