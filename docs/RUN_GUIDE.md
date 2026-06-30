# RUN_GUIDE — finishing the engine, from here on

> **What this file is.** A short, sequential checklist that picks up from *exactly where the
> code is today* (the `Run` test is written but red) and runs to the `v0.1.0` tag. It is
> deliberately thin: for the worker-pool *design* it **links into** the deep-dive in
> [`NEXT_STEPS.md`](./NEXT_STEPS.md) rather than repeating it. Read this for the order of
> operations; read `NEXT_STEPS.md` for the *why* and the snippets.
>
> _Scaffolding doc — safe to delete before the `v0.1.0` tag._

---

## Where we are now (2026-06-28)

`TestRun` exists and is **RED** — it asserts `Total/Succeeded/Failed == 10` against an
`httptest` server, but `Run` is still a stub (`run.go` returns `Summary{}, nil`), so the
counts come back 0. That's the correct test-first state: the failing test is written, the
implementation is next.

| Piece | File | Status |
|---|---|---|
| `hit` (one request, drain+close body) | `loadtest/apiCall.go` | ✅ done |
| `percentile` (nearest-rank) | `loadtest/stats.go` | ✅ done |
| `summarize` (`[]result` → `Summary`) | `loadtest/summary.go` | ✅ done |
| `Config` (the run's knobs) | `loadtest/run.go` | ✅ struct exists |
| **`Run`** (the concurrent engine) | `loadtest/run.go` | ❌ **stub — Step 1** |
| `TestRun` | `loadtest/run_test.go` | 🟥 written, red |
| CLI | `cmd/loadtester/main.go` | ❌ `package main` only |

Everything below is **TDD: failing test first, then the code that makes it pass.** Per the
house rules, *you* write the implementation — this guide coaches, it doesn't hand you `Run`.

---

## Step 0 — Clear the inline review comments in `run_test.go`

I left tagged `// [should-fix]` / `// [nit]` notes in `loadtest/run_test.go`. Work them top
to bottom, deleting each comment as you address it:

- [ ] Rename the config variable `test` → `cfg` (it's the run's config, not a test).
- [ ] Strengthen the assertions beyond counts: also check `len(got.Errors) == 0`,
      `got.Throughput > 0`, and `got.P50/P90/P99 > 0`. Assert *relationships* (`> 0`), never
      exact latency numbers. `P50 > 0` is what proves latency timing is actually wired up.
- [ ] Make the happy path **table-driven** (house style — see `summary_test.go`,
      `apiCall_test.go`). Vary `Concurrency`/`Requests`/`Method`, and add an all-500 target
      row (`Failed == Requests`, `Succeeded == 0`).
- [ ] Fix the empty-handler signature and the `occured` → `occurred` typo.

> The cancellation test and the `-race`/goleak reminders are also flagged there — those are
> **Step 2**, not Step 0. Don't try to do them before `Run` exists.

---

## Step 1 — Implement `Run` until `TestRun` is green

This is the core of the project. **Don't reinvent the design — it's already written out.**
Read these sections of `NEXT_STEPS.md` and follow them:

- **§1.3 The types** — `Config` is done; add the godoc; reuse the existing `result` struct as
  the channel payload and return the existing `Summary`.
- **§1.4 The concurrency contract** — the four rules that keep it correct under `-race`.
- **§1.5 Goroutine lifecycle** — the "what stops each goroutine?" table.
- **§1.6 Snippets** — worker loop, closer, producer, sink (shapes, not a finished file).

Checklist for the implementation:

- [ ] **One shared `runner`** per `Run` call via `newRunner(timeout)` — share it across all
      workers. Never one client/runner per worker.
- [ ] **Producer** sends `cfg.Requests` jobs into `jobs`, then is the **only** closer of
      `jobs`; it bails early on `ctx.Done()`.
- [ ] **`cfg.Concurrency` workers**, each timing the full `hit` call
      (`start := time.Now()` … `time.Since(start)`) and sending a `result`.
- [ ] **`results` is closed only after `wg.Wait()`** — by a separate closer goroutine. Closing
      it while a worker might still send is a "send on closed channel" panic.
- [ ] **`Run` itself is the sink** — it ranges over `results` into a `[]result`, then calls
      `summarize(results, time.Since(start))`. (If the sink doesn't drain, everything
      deadlocks — see §1.5.)
- [ ] **Thread `ctx`** all the way into `hit` so cancellation reaches in-flight requests.
- [ ] **Validate `Config`** by *returning an error* (empty `URL`, `Concurrency <= 0`,
      `Requests <= 0`); wrap with `%w`. Only `main` ever turns that into a message + exit code.
- [ ] **Default `Method`** to `http.MethodGet` when `cfg.Method == ""`.

Done when `go test ./loadtest/ -run TestRun` is **green**.

---

## Step 2 — The cancellation test + `-race` + goleak

The headline behavior of a load tester: Ctrl+C / a timeout must abort live requests.

- [ ] **`TestRunCancellation`** — stand up a *slow* handler
      (`select { case <-time.After(200*time.Millisecond): case <-r.Context().Done(): }`, the
      pattern already in `apiCall_test.go:152-199`). Start `Run` with a `ctx` you cancel after
      ~20ms. Assert `Run` **returns fast** (well under the all-requests time) and
      `errors.Is(err, context.Canceled)` (or `DeadlineExceeded`). See `NEXT_STEPS.md §1.7-4`.
- [ ] **`go test -race ./...`** — the only thing that reliably catches unsynchronized access
      or a bad channel close. A green run *without* `-race` proves nothing about concurrency.
- [ ] **goleak `TestMain`** — `goleak.VerifyTestMain(m)` fails the package if any goroutine
      outlives the tests (a leaked worker/producer). The one sanctioned test-only third-party
      dep; justify it in the commit as "test-only leak detection, no production dependency."

---

## Step 3 — Edge-case tests

From `NEXT_STEPS.md §1.8` — add these as rows/tests once `Run` is green:

- [ ] **`Requests == 0`** → empty run; `summarize` gets an empty slice; `Errors` is a non-nil
      empty map.
- [ ] **`Concurrency > Requests`** (e.g. 50 workers, 10 requests) → extra workers exit cleanly
      when `jobs` drains; they must not hang (`-race` + goleak catch it if they do).
- [ ] **All-failing target** (500s, or a closed server for transport errors) →
      `Failed == Requests`, `Succeeded == 0`, `Errors` populated, percentiles 0.

---

## Step 4 — The CLI (`cmd/loadtester/main.go`)

`main.go` is currently just `package main`. Make it runnable: parse flags → validate → build
`Config` → `signal.NotifyContext` for Ctrl+C → `loadtest.Run` → render → exit code. **`main`
is glue only; all real logic stays in the library.** Full design (flags table, exit codes,
signal handling, the testable `render(w io.Writer, s Summary)`, the safety banner) is in
**`NEXT_STEPS.md §2`** — follow it.

---

## Step 5 — Carry-over review items

Small `[should-fix]`s already flagged inline in earlier files; clean up when you touch them
(also listed in `NEXT_STEPS.md §3.3`):

- [ ] `apiCall.go` — capture the `io.Copy(io.Discard, resp.Body)` error instead of discarding
      it; a body-read failure mid-response currently reports as success.
- [ ] `summary.go` — already names `isServerError` and guards `statusErrText`; confirm both
      carry a doc comment stating the policy (5xx = server failed; 4xx counts as success).
- [ ] `stats.go` — `percentile`'s doc comment is already above the signature; confirm it stays
      there.

---

## Definition of done (mirrors `CLAUDE.md`)

A step is done when it **compiles**, its tests **pass under `-race`**, **`golangci-lint run`**
is clean, and the program runs end-to-end for that step's feature. After Step 4 that's v1 —
tag **`v0.1.0`**, and turn new ideas into GitHub issues rather than growing this branch.

### Command cheat-sheet

```
go test -v -run TestRun ./loadtest/   # iterate on just the Run tests
go test ./...                         # full suite
go test -race ./...                   # the one that matters for concurrency
golangci-lint run                     # keep clean before calling a step done
go build ./cmd/loadtester             # once Step 4 lands
```
