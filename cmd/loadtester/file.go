package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tentse/load-tester/configfile"
	"github.com/tentse/load-tester/loadtest"
)

// stdinPath is the conventional name for reading the config from standard input, which is not
// supported yet. It is recognised only so the failure says so plainly.
const stdinPath = "-"

func hasFileFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-f" || arg == "--f" {
			return true
		}
	}
	return false
}

func parseFileConfig(args []string, stderr io.Writer) (loadtest.FileConfig, error) {
	fs := flag.NewFlagSet("loadtester", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "loadtester: a small HTTP load tester\n")
		fmt.Fprintf(fs.Output(), "WARNING: this tool generates load. Only point it at systems you own or have explicit permission to test.\n")
		fmt.Fprintf(fs.Output(), "\nUsage:\n")
		fmt.Fprintf(fs.Output(), "  loadtester -f requests.json\n")
		fmt.Fprintf(fs.Output(), "  loadtester -f ./configs/staging.json\n\n")
		fmt.Fprintf(fs.Output(), "Every endpoint in the file runs in one pass, sharing one worker pool.\n")
		fmt.Fprintf(fs.Output(), "The single-target flags (-url, -c, -n, -method, -body, -timeout, -expect, -H)\n")
		fmt.Fprintf(fs.Output(), "cannot be combined with -f.\n")
		fs.PrintDefaults()
	}
	path := fs.String("f", "", "path to a JSON `file` describing the requests, given as a separate argument: -f requests.json")

	if err := fs.Parse(args); err != nil {
		return loadtest.FileConfig{}, fmt.Errorf("parsing failed: %w", err)
	}
	if *path == "" {
		fmt.Fprint(fs.Output(), "parsing failed: -f needs a file\n")
		fs.Usage()
		return loadtest.FileConfig{}, errors.New("-f needs a file")
	}
	if *path == stdinPath {
		return loadtest.FileConfig{}, errors.New("reading the config from stdin is not supported yet")
	}

	cfg, err := configfile.Load(os.DirFS(filepath.Dir(*path)), filepath.Base(*path))
	if err != nil {
		return loadtest.FileConfig{}, err
	}

	return cfg, nil
}

func runFile(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	config, err := parseFileConfig(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}

	summaries, err := loadtest.FileRun(ctx, config)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			renderErr := renderSummaries(stdout, summaries)
			if renderErr != nil {
				fmt.Fprintf(stderr, "stdout write error: \n%v\n", renderErr)
			} else {
				fmt.Fprintf(stderr, "load test canceled: %v\n", err.Error())
			}
			return 130
		}
		if errors.Is(err, loadtest.ErrInvalidConfig) {
			fmt.Fprintf(stderr, "loadtest.FileRun() error: \n%v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "loadtest.FileRun() error: \n%v\n", err)
		return 1
	}

	err = renderSummaries(stdout, summaries)
	if err != nil {
		fmt.Fprintf(stderr, "stdout write error: \n%v\n", err)
		return 1
	}
	return 0
}
