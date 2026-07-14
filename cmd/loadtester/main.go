package main

import (
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

// [blocker] This `main` package still has no `func main()`, so `go build ./cmd/loadtester`
// fails with "function main is undeclared." Keep main tiny, but v0.1 cannot ship until the
// testable `run(args, stdout, stderr) int` pipeline exists and main delegates to it.
func parseConfig(args []string, stderr io.Writer) (loadtest.Config, error) {
	fs := flag.NewFlagSet("loadtester", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// [should-fix] These defaults contradict the documented CLI contract: c=10, n=100,
	// timeout=30s. The defaults test currently locks in 1/1/1s, so implementation and test
	// agree with each other while both disagree with the user-facing specification.
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
