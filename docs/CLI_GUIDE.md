# Guide: Building the `cmd/loadtester` CLI (v0.1) and testing it

This is a **teaching guide**, not a solution. It shows the shape, the standard-library
idioms, and the traps — and leaves the actual wiring for you to type. Snippets are
illustrative sketches (signatures, pseudocode, the occasional two-line pattern), never a
finished `main.go`.

It builds on the spec already written in `docs/AI_PROJECT_CONTEXT.md` ("CLI milestone
specification", the flag table and exit codes) and on the existing `loadtest.Config` /
`loadtest.Run` / `loadtest.Summary` (`loadtest/run.go`, `loadtest/summary.go`).

> **Prerequisite is already done.** The `[blocker]` (discarded body-read error) is fixed in
> `apiCall.go`, and `TestResponseBodyError` covers it. Your own `AI_PROJECT_CONTEXT.md` still
> lists that blocker as the "next milestone" / an active finding (lines ~124, ~166) — update
> that doc so it matches the code before you start the CLI.

---

## 1. The one rule that shapes everything: keep `main` thin

`main` is the *only* place in the whole project allowed to read flags, print, or exit
(`AGENTS.md`). Everything else returns values and errors. So the whole program is a short
pipeline:

```
parse args  ->  build loadtest.Config  ->  signal.NotifyContext (Ctrl+C)
            ->  loadtest.Run            ->  render Summary        ->  choose exit code
```

The mistake to avoid is doing this *inside* `func main()`, because `main` can't be tested and
can't return an error. Instead, push all of it into a helper that returns an `int` (or an
`error`), and let `main` be a one-liner. That single decision is what makes the CLI testable —
everything in §5 depends on it.

Think about this signature before you write anything:

```go
// run does the real work and returns a process exit code.
func run(args []string, stdout, stderr io.Writer) int
```

Why pass `args` and the writers in instead of reaching for `os.Args` / `os.Stdout`? Because a
test can then call `run([]string{"-url", ts.URL, "-n", "5"}, &outBuf, &errBuf)` and inspect
the result. Reaching for globals makes that impossible. This is the "accept parameters, don't
grab globals" habit.

---

## 2. The `os.Exit` trap

`os.Exit` **skips every `defer`**. If you scatter `os.Exit(1)` through logic that has open
files or deferred cleanup, that cleanup never runs. So confine exiting to exactly one place:

```go
func main() {
    os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
```

Now `main` is trivial and `run` is a normal, testable function that *returns* a code instead
of killing the process. Everything with a `defer` lives inside `run` and unwinds cleanly
before `main` calls `os.Exit`.

---

## 3. Parsing flags — and why not the global `flag` set

The spec's flags: `-url` (required), `-c` (10), `-n` (100), `-method` (GET), `-token` (""),
`-body` (""), `-timeout` (30s).

Two idioms matter here:

**Use a fresh `flag.FlagSet`, not the package-level `flag.String(...)`.** The global set is
process-wide state; if two tests both call it, they collide, and you can't re-parse. A local
set is re-entrant and testable:

```go
fs := flag.NewFlagSet("loadtester", flag.ContinueOnError)
fs.SetOutput(stderr)
url := fs.String("url", "", "target URL (required)")
// ...define the rest against fs...
if err := fs.Parse(args); err != nil {
    return 2 // flag parse error == usage error
}
```

`flag.ContinueOnError` (instead of the default `ExitOnError`) means a bad flag returns an
error to *you* rather than calling `os.Exit` behind your back — essential for both testing and
your own exit-code control.

**"Required" is your job.** `flag` has no notion of a required flag. After `Parse`, check it
yourself:

```go
if *url == "" {
    fmt.Fprintln(stderr, "error: -url is required")
    return 2
}
```

Then map the parsed flags into a `loadtest.Config{...}`. That mapping is pure and trivially
testable — consider extracting it (e.g. `func configFrom(fs ...) loadtest.Config`) if you want
to unit-test it in isolation.

---

## 4. Ctrl+C, then `loadtest.Run`

`loadtest.Run` already takes a `context.Context` and cancels in-flight requests when it's
done. Your job is to make Ctrl+C cancel that context. The modern idiom is one call:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
```

`signal.NotifyContext` gives you a context that's cancelled on the signal — no manual
`signal.Notify` + channel + goroutine plumbing. Pass `ctx` straight into `loadtest.Run(ctx,
cfg)`. When the user hits Ctrl+C, the producer and workers stop and `Run` returns a *partial*
`Summary` plus `ctx.Err()` (it returns `context.Canceled`) — that's the contract documented on
`Run`.

---

## 5. Rendering: take an `io.Writer`, don't `Println`

The single most useful testability move for output: don't print inside `run`; write to an
`io.Writer` you were handed.

```go
func render(w io.Writer, s loadtest.Summary) error
```

Because a test can pass a `bytes.Buffer` and assert on the text, while `main`/`run` passes the
real `os.Stdout`. Inside, `fmt.Fprintf(w, ...)` the fields you care about (Total, Succeeded,
Failed, Throughput, P50/P90/P99, and the `Errors` map). Keep formatting decisions here, not
scattered around.

A couple of things to decide (not code for you to copy — questions to answer):
- Where do latencies and throughput go when the run was **all failures**? (`Summary` leaves
  P50/P90/P99 at zero and Throughput at zero — render them sensibly, e.g. "n/a".)
- How do you present the `Errors` map so it's readable — sorted by count? (Tie-in: the
  error-map can currently contain one entry per failed request under connection resets — see
  the review's finding #3 — so don't assume it's small.)

---

## 6. Exit codes

The spec: `0` = ran and reported, `1` = library/run error, `2` = invalid usage. Keep the
decision in one tiny, testable function:

```go
func exitCode(err error) int
```

The subtlety is **cancellation**. When the user hits Ctrl+C, `Run` returns a valid partial
`Summary` *and* `context.Canceled`. That's not really a "run error" — you probably still want
to render the partial results, then choose a code. Decide deliberately:
- validation error from `Run` (bad `Config`) → treat as usage, `2`;
- `errors.Is(err, context.Canceled)` → render the partial summary, then pick `0` or a distinct
  code — your call, but document it;
- any other non-nil `Run` error → `1`;
- success → `0`.

---

## 7. Token safety (new, and important for a CLI)

The moment `-token` exists, be careful: **a bearer token passed as a CLI flag leaks.** It lands
in shell history (`~/.zsh_history`) and is visible to any user who runs `ps`/`/proc` while your
process is alive. For a load tester that authenticates against real services, that's a real
credential-exposure footgun.

Guidance:
- Prefer reading the token from an **environment variable** (e.g. `LOADTESTER_TOKEN`) or a
  **file** (`-token-file`), and keep the `-token` flag only as a convenience with a documented
  warning.
- Never echo the token back in output, usage, or error messages (don't `Fprintf` the `Config`
  wholesale if it holds the token).

---

## 8. Usage / help and the safety warning

Override `fs.Usage` to include the project's standing warning: **only target systems you own
or have explicit permission to test.** The README already says it; the `--help` output should
too, because that's what a new user actually reads.

---

## 9. How to test all this

The extractions above are precisely what make testing possible. None of these tests touch the
network beyond a local `httptest.Server`, and none call `os.Exit`.

**Render — table-driven, against a buffer.** This is the easiest win:

```go
var buf bytes.Buffer
_ = render(&buf, loadtest.Summary{Total: 3, Succeeded: 3, /* ... */})
// assert buf.String() contains the fields you expect
```

Cover the edge cases the `Summary` contract creates: all-success, all-failure (zero
percentiles), a populated `Errors` map, and an empty/zero `Summary`.

**`exitCode` — table-driven, trivial.** Map each input error to its code: a validation error
(`errors.New("invalid url ...")` shape) → 2, `context.Canceled` → your chosen code, a generic
error → 1, `nil` → 0.

**Flag parsing / `Config` mapping.** Because you used `flag.NewFlagSet`, you can parse a slice
of args in a test and assert the resulting `Config` (or the error). Test the required-`-url`
rejection and a bad-flag rejection both return the usage path.

**End-to-end, through `run`.** Stand up an `httptest.Server`, then call `run` with real-looking
args pointing at it and a `bytes.Buffer` for output:

```go
ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
defer ts.Close()
var out bytes.Buffer
code := run([]string{"-url", ts.URL, "-n", "5", "-c", "2"}, &out, io.Discard)
// assert code == 0 and out.String() reports 5 successes
```

Things to deliberately *not* do (they're in `AGENTS.md`, and they bite):
- Don't test by calling `main()` — it exits the test binary. Test `run`.
- Don't use `time.Sleep` to coordinate; for the cancellation test, reuse the event-driven
  `started`-channel pattern already in `run_test.go`'s `TestRunCancellation`.
- Remember `go test -race ./...`; the CLI itself is single-goroutine, but the end-to-end test
  drives the concurrent engine.

---

## 10. Definition of done (mirrors `AGENTS.md`)

- `go build ./cmd/loadtester` produces a binary (it currently fails — `main` is undeclared).
- `go test -race -count=1 ./...` is green, including new `cmd/loadtester` tests.
- `golangci-lint run` is clean.
- `go run ./cmd/loadtester -url http://localhost:8080 -c 10 -n 100` works end-to-end, prints a
  summary, and returns the right exit code (try Ctrl+C mid-run and confirm the partial summary
  + code).
- `AI_PROJECT_CONTEXT.md` updated: the blocker is done; the CLI is now the current milestone.

When that's all true, tag `v0.1.0` — then multi-endpoint JSON (see
`docs/MULTI_ENDPOINT_DESIGN.md`) becomes v0.2, not scope creep in this step.
