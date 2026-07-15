package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

type errorCount struct {
	message string
	count   int
}

func parseConfig(args []string) (loadtest.Config, error) {
	fs := flag.NewFlagSet("loadtester", flag.ContinueOnError)
	// [should-fix] With no SetOutput call, flag parsing errors go directly to the process's
	// os.Stderr instead of the writer injected into run. That makes tests noisy and means a
	// caller-provided stderr cannot capture all diagnostics. Choose one owner: either give
	// parseConfig a writer again, or silence FlagSet and let run report the returned error once.

	targetURL := fs.String("url", "", "target URL (required)")
	concurrency := fs.Int("c", 10, "number of concurrent worker")
	requests := fs.Int("n", 20, "total number of requests")
	method := fs.String("method", http.MethodGet, "HTTP method")
	token := fs.String("token", "", "bearer token")
	body := fs.String("body", "", "JSON request body")
	timeout := fs.Duration("timeout", 1*time.Second, "per request timeout")

	if err := fs.Parse(args); err != nil {
		return loadtest.Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if *targetURL == "" {
		return loadtest.Config{}, fmt.Errorf("-url is required")
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

// [blocker] The package still has no `func main()`, so `go build ./cmd/loadtester` fails.
// Finish and test this orchestration contract first, then add a tiny main that supplies
// os.Args[1:], os.Stdout, and os.Stderr and exits with the returned code.
func run(args []string, stdout, stderr io.Writer) int {
	config, err := parseConfig(args)
	// [blocker] Every error branch below returns only when writing its diagnostic succeeds.
	// If stderr fails, control falls through with an invalid config/Run error/render error and
	// can eventually return 0. Reporting an error is secondary: once the operation fails, return
	// a non-zero code regardless of whether the diagnostic writer also fails.
	if err != nil {
		if _, err := fmt.Fprintf(stderr, "parsing failed: \n%v\n", err); err == nil {
			return 2
		}
	}

	// [blocker] This context cannot be cancelled while Run is executing: cancel is called only
	// by the defer after Run returns, and it is not connected to Ctrl+C. Prefer accepting a
	// context in the testable orchestration function; main can supply a signal-aware context,
	// while tests can cancel one deterministically after a request starts.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	summary, err := loadtest.Run(ctx, config)
	// [blocker] Once cancellation is wired in, this generic error branch will discard Run's
	// useful partial Summary. Handle context cancellation separately: render the partial result,
	// then return the interruption code you chose; ordinary Run errors should not be rendered.
	if err != nil {
		if _, err := fmt.Fprintf(stderr, "loadtest.Run() error: \n%v\n", err); err == nil {
			return 1
		}
	}

	err = render(stdout, summary)
	if err != nil {
		if _, err := fmt.Fprintf(stderr, "stdout write error: \n%v\n", err); err == nil {
			return 1
		}
	}
	return 0
}
