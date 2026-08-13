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
	"strings"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

func parseHeader(s string) (name, value string, err error) {
	name, value, ok := strings.Cut(s, ":")
	if !ok {
		return "", "", fmt.Errorf("invalid header %q: want \"Name: Value\"", s)
	}

	name, value = strings.TrimSpace(name), strings.TrimSpace(value)

	if name == "" {
		return "", "", fmt.Errorf("invalid header %q: empty name", s)
	}
	if strings.ContainsAny(name, " \t") {
		return "", "", fmt.Errorf("invalid header %q: contains whitespace character", name)
	}
	if strings.ContainsAny(name+value, "\r\n") {
		return "", "", fmt.Errorf("invalid header %q: contains CR or LF", s)
	}

	return name, value, nil
}

func parseConfig(args []string, stderr io.Writer) (loadtest.Config, error) {
	fs := flag.NewFlagSet("loadtester", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "loadtester: a small HTTP load tester\n")
		fmt.Fprintf(fs.Output(), "WARNING: this tool generates load. Only point it at systems you own or have explicit permission to test.\n")
		fmt.Fprintf(fs.Output(), "\nUsage:\n")
		fmt.Fprintf(fs.Output(), "  loadtester -url https://example.internal/ -expect 200\n")
		fmt.Fprintf(fs.Output(), "  loadtester -f requests.json     # several endpoints from a JSON file\n\n")
		fs.PrintDefaults()
	}
	targetURL := fs.String("url", "", "target URL (required)")
	concurrency := fs.Int("c", 10, "number of concurrent worker")
	requests := fs.Int("n", 20, "total number of requests")
	method := fs.String("method", http.MethodGet, "HTTP method")
	body := fs.String("body", "", "JSON request body")
	timeout := fs.Duration("timeout", 1*time.Second, "per request timeout")
	expect := fs.Int("expect", 0, "HTTP status code that counts as a success (required)")

	headers := http.Header{}
	fs.Func("H", `custom header, repeatable: -H "Name: Value`, func(s string) error {
		name, value, err := parseHeader(s)
		if err != nil {
			return err
		}
		headers.Add(name, value)
		return nil
	})

	if err := fs.Parse(args); err != nil {
		return loadtest.Config{}, fmt.Errorf("parsing failed: %w", err)
	}
	if *targetURL == "" {
		fmt.Fprint(fs.Output(), "parsing failed: -url is required\n")
		fs.Usage()
		return loadtest.Config{}, errors.New("-url is required")
	}
	if *expect <= 0 {
		fmt.Fprint(fs.Output(), "parsing failed: valid -expect is required\n")
		fs.Usage()
		return loadtest.Config{}, errors.New("valid -expect is required")
	}

	return loadtest.Config{
		URL:         *targetURL,
		Concurrency: *concurrency,
		Requests:    *requests,
		Method:      *method,
		Headers:     headers,
		Body:        *body,
		Timeout:     *timeout,
		Expect:      *expect,
	}, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if hasFileFlag(args) {
		return runFile(ctx, args, stdout, stderr)
	}
	return runSingle(ctx, args, stdout, stderr)
}

func runSingle(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
