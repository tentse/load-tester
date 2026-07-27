package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

type errorCount struct {
	message string
	count   int
}

func parseConfig(args []string, stderr io.Writer) (loadtest.Config, error) {
	// [should-fix] fs.Usage is never overridden, so `-h` prints a bare list of flags with no
	// safety warning. AGENTS.md requires --help to state that this tool must only be pointed
	// at systems you own or have explicit permission to test. --help is what a new user
	// actually reads; the warning sitting in README.md never reaches them. Set fs.Usage to a
	// func that writes the warning and then calls fs.PrintDefaults().
	fs := flag.NewFlagSet("loadtester", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "loadtester: a small HTTP load tester\n")
		fmt.Fprintf(fs.Output(), "WARNING: this tool generates load. Only point it at systems you own or have explicit permission to test.\n")
		fs.PrintDefaults()
	}
	// [should-fix] With no SetOutput call, flag parsing errors go directly to the process's
	// os.Stderr instead of the writer injected into run. That makes tests noisy and means a
	// caller-provided stderr cannot capture all diagnostics. Choose one owner: either give
	// parseConfig a writer again, or silence FlagSet and let run report the returned error once.
	//
	// [should-fix] Still open — and I confirmed the concrete effect, so it's worth closing:
	// flag dumps its own usage to the process's os.Stderr even when a test injected a
	// bytes.Buffer, and a bad flag is then reported *twice* (once by FlagSet, once by run's
	// "parsing failed"). One owner per message. fs.SetOutput(io.Discard) is the smaller of
	// the two fixes you listed.

	targetURL := fs.String("url", "", "target URL (required)")
	concurrency := fs.Int("c", 10, "number of concurrent worker")
	requests := fs.Int("n", 20, "total number of requests")
	method := fs.String("method", http.MethodGet, "HTTP method")
	// [should-fix] A bearer token passed on the command line leaks: it lands in shell history
	// (~/.zsh_history) and is visible to anyone who can run `ps` while the process is alive.
	// That's a real credential-exposure footgun for a tool meant to authenticate against real
	// services. Keep the flag for convenience, but say so in this help string and prefer an
	// env var (LOADTESTER_TOKEN) or -token-file as the documented path. Related: never echo
	// the token back — don't ever Fprintf a whole Config, because it holds one.
	token := fs.String("token", "", "bearer token")
	body := fs.String("body", "", "JSON request body")
	timeout := fs.Duration("timeout", 1*time.Second, "per request timeout")

	if err := fs.Parse(args); err != nil {
		return loadtest.Config{}, fmt.Errorf("parsing failed: %w", err)
	}
	if *targetURL == "" {
		// [nit] fmt.Errorf with no format verbs is just errors.New with extra steps. Reach for
		// fmt.Errorf only when you're actually formatting a value or wrapping with %w.
		fmt.Fprint(fs.Output(), "parsing failed: -url is required\n")
		fs.Usage()
		return loadtest.Config{}, errors.New("-url is required")
	}

	return loadtest.Config{
		URL:         *targetURL,
		Concurrency: *concurrency,
		Requests:    *requests,
		Method:      *method,
		Token:       *token,
		Body:        *body,
		Timeout:     *timeout,
	}, nil
}

func render(w io.Writer, summary loadtest.Summary) error {
	summaryText := getSummaryText(summary)
	_, err := io.WriteString(w, summaryText)
	if err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

func getSummaryText(summary loadtest.Summary) string {
	var b strings.Builder

	fmt.Fprintf(&b,
		"Load test summary\n"+
			"Total: %d\n"+
			"Succeeded: %d\n"+
			"Failed: %d\n"+
			"Elapsed: %v\n"+
			"Throughput: %.2f req/s\n",
		summary.Total,
		summary.Succeeded,
		summary.Failed,
		summary.Elapsed,
		summary.Throughput,
	)

	if summary.Succeeded == 0 {
		b.WriteString("P50: n/a\nP90: n/a\nP99: n/a\n")
	} else {
		fmt.Fprintf(&b,
			"P50: %v\n"+
				"P90: %v\n"+
				"P99: %v\n",
			summary.P50,
			summary.P90,
			summary.P99,
		)
	}

	// [should-fix] This local `errors` shadows the imported `errors` package for the rest of
	// the function. It compiles today only because nothing in here calls errors.Is/errors.As
	// — the day someone does, they get "errors.Is undefined (type []errorCount ...)" on a
	// package they can plainly see imported at the top of the file. Shadowing a stdlib
	// package name is a trap you set for your future self. Name it for what it holds:
	// `counts` or `errCounts`.
	errors := make([]errorCount, 0, len(summary.Errors))

	for message, count := range summary.Errors {
		errors = append(errors, errorCount{
			message: message,
			count:   count,
		})
	}

	b.WriteString("Errors:\n")

	if len(errors) == 0 {
		b.WriteString("n/a\n")
		return b.String()
	}

	sort.Slice(errors, func(i, j int) bool {
		if errors[i].count == errors[j].count {
			return errors[i].message < errors[j].message
		}
		return errors[i].count > errors[j].count
	})

	for _, entry := range errors {
		fmt.Fprintf(&b, "  %s: %d\n", entry.message, entry.count)
	}

	return b.String()
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	config, err := parseConfig(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		// [should-fix] There are five unchecked fmt.Fprintf calls in this function and they
		// are the only thing keeping golangci-lint red. (Run it with --max-same-issues=0 —
		// the default cap of 3 is hiding two of them from you.) Your definition of done says
		// lint must be clean before a step counts, so pick ONE deliberate policy for
		// diagnostic writes and apply it everywhere: either `_, _ =` to say out loud "if
		// stderr is broken there is nothing useful left to do", or funnel every diagnostic
		// through one small helper. Five ad-hoc unchecked calls is the one option that isn't
		// a decision.
		//
		// [nit] "parsing failed: \n%v\n" puts a newline mid-sentence, so the message arrives
		// split across two lines for no reason. CLI convention is a single line prefixed with
		// the program name: `loadtester: %v\n`. Same pattern in the three messages below.
		return 2
	}

	// [should-fix] run can't tell a *config* error from a *runtime* error here, because
	// loadtest.Run hands back bare fmt.Errorf strings with no identity to match on. Concrete
	// consequence: `-c 0` is a usage mistake but exits 1 ("library error") instead of 2,
	// contradicting the exit-code contract you wrote in docs/CLI_GUIDE.md §6. Don't fix it by
	// string-matching the message — that's the anti-pattern. The Go idiom is a sentinel the
	// caller can match: declare `var ErrInvalidConfig = errors.New("invalid config")` in
	// loadtest, wrap it with %w in validateConfig, and classify here with errors.Is. That is
	// exactly what %w is for. Errors are values you match on, not strings you grep.
	summary, err := loadtest.Run(ctx, config)

	if err != nil {
		// [should-fix] Only context.Canceled is handled. A context carrying a deadline returns
		// context.DeadlineExceeded, falls into the branch below, and throws away a perfectly
		// valid partial Summary — the user gets an error and none of the results the tool just
		// spent 30 seconds collecting. Nothing sets a deadline today so this is latent; it
		// stops being latent the moment you add a -duration flag. The contract you documented
		// on Run is "partial results plus ctx.Err()", so classify on that: either add
		// `|| errors.Is(err, context.DeadlineExceeded)`, or just ask `ctx.Err() != nil`, which
		// covers every cancellation cause including future ones (context.Cause, WithTimeoutCause).
		if errors.Is(err, context.Canceled) {
			renderErr := render(stdout, summary)
			if renderErr != nil {
				fmt.Fprintf(stderr, "stdout write error: \n%v\n", renderErr)
			} else {
				fmt.Fprintf(stderr, "load test canceled: %v\n", err.Error())
			}
			return 130
			// [should-fix] The if block above ends in `return`, so this `else` is dead weight
			// — revive's indent-error-flow flags it. Drop the else and outdent its body: Go
			// house style is to handle the exceptional case and return, keeping the happy path
			// unindented at the left margin. Note this is a regression, not an oversight — the
			// committed version had the early return and your working-tree diff added the else.
		} else {
			// [nit] "loadtest.Run() error:" leaks an internal function name into user-facing
			// output. The person staring at the terminal doesn't know or care which function
			// returned the error; they want `loadtester: <what went wrong>`. Internal names
			// belong in logs, not in the message a user reads.
			fmt.Fprintf(stderr, "loadtest.Run() error: \n%v\n", err)
			return 1
		}
	}

	err = render(stdout, summary)
	if err != nil {
		fmt.Fprintf(stderr, "stdout write error: \n%v\n", err)
		return 1
	}
	return 0
}

func start() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

func main() {

	os.Exit(start())

}
