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
	fs := flag.NewFlagSet("loadtester", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "loadtester: a small HTTP load tester\n")
		fmt.Fprintf(fs.Output(), "WARNING: this tool generates load. Only point it at systems you own or have explicit permission to test.\n")
		fs.PrintDefaults()
	}
	targetURL := fs.String("url", "", "target URL (required)")
	concurrency := fs.Int("c", 10, "number of concurrent worker")
	requests := fs.Int("n", 20, "total number of requests")
	method := fs.String("method", http.MethodGet, "HTTP method")
	token := fs.String("token", "", "bearer token")
	body := fs.String("body", "", "JSON request body")
	timeout := fs.Duration("timeout", 1*time.Second, "per request timeout")

	if err := fs.Parse(args); err != nil {
		return loadtest.Config{}, fmt.Errorf("parsing failed: %w", err)
	}
	if *targetURL == "" {
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
		return 2
	}

	summary, err := loadtest.Run(ctx, config)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			renderErr := render(stdout, summary)
			if renderErr != nil {
				fmt.Fprintf(stderr, "stdout write error: \n%v\n", renderErr)
			} else {
				fmt.Fprintf(stderr, "load test canceled: %v\n", err.Error())
			}
			return 130
		}
		if errors.Is(err, loadtest.ErrInvalidConfig) {
			fmt.Fprintf(stderr, "loadtest.Run() error: \n%v\n", err)
			return 2
		} else {
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
