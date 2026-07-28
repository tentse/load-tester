# Contributing

Thanks for taking an interest. Issues and pull requests are welcome.

## Before you write code

**Open an issue first** for anything beyond a typo or an obvious bug fix. It saves you writing
something that gets turned down for a reason that was never written anywhere. Bug reports are
most useful with the command you ran, the output you got, and the output you expected.

Good places to start are the entries under
[Known limitations](README.md#known-limitations) in the README — each one is a real, scoped
piece of work.

## Project constraints

Two rules shape most decisions here:

1. **Standard library only in production code.** This is deliberate, not an oversight. If you
   believe a change genuinely needs a third-party dependency, say so in the issue and make the
   case before writing the code. The only current dependency is `go.uber.org/goleak`, and it
   is test-only.
2. **The public API stays in the importable `loadtest` package.** This tool is meant to be
   usable as a library, so `Config`, `Run`, and `Summary` do not move into `internal/`.

`cmd/loadtester` stays thin: parse flags, call the library, render, choose an exit code. Real
logic belongs in `loadtest`.

## Development

```sh
go build ./...
go test ./...
go test -race -count=1 ./...
go vet ./...
gofmt -l .
```

`go test -race` is the one that matters — this is a concurrency project, and race conditions
stay invisible until something goes looking for them. Please run it before opening a PR.

`golangci-lint run` currently reports some known `errcheck` findings in `cmd/loadtester`. They
are on the list to fix; please don't let them block your PR, but do keep your own changes
clean.

## Tests — please write them first

This project is built test-first, following the rhythm in
[Learn Go with Tests](https://quii.gitbook.io/learn-go-with-tests/): write a failing test,
**watch it fail and check it fails for the reason you expect**, make it pass, then tidy up.

That middle step is the one people skip, and it's the one that matters. A test you never saw
fail is a test you have no evidence actually tests anything.

Concretely, for a PR:

- Start from the test. If you're fixing a bug, the test should fail on `main` and pass with
  your change — say so in the PR description.
- Table-driven where it fits.
- HTTP is tested against `httptest.Server` — tests never touch the real network.
- Coordinate concurrency tests with channels, not `time.Sleep`. `TestRunCancellation` in
  `loadtest/run_test.go` is the pattern to copy.
- New behaviour needs a test that fails without your change.

## Pull requests

- Keep them focused — one concern per PR reviews far faster than five.
- Describe what changed and why. If it changes observable behaviour (exit codes, output
  format, the public API), say so explicitly, and update the README in the same PR.
- Make sure `go test -race ./...` passes and `gofmt -l .` is silent.

## Behaviour that is intentional

Please don't "fix" these without discussion — they are decisions, and changing them changes
the public contract:

- A `4xx` response counts as a **success**. The server responded, which is what is being
  measured.
- Percentiles cover **successful requests only**, so failures cannot flatter the numbers.
- A run in which every request failed still exits `0`. The load test succeeded; the results
  are in the summary.
- Cancellation returns a **partial** `Summary` together with `ctx.Err()`, rather than
  discarding the work already done.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
