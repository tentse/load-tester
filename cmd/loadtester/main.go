package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tentse/load-tester/loadtest"
)

type errorCount struct {
	message string
	count   int
}

func parseConfig(args []string, stderr io.Writer) (loadtest.Config, error) {

	return loadtest.Config{}, nil
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

	b.WriteString(fmt.Sprintf(
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
	))

	if summary.Succeeded == 0 {
		b.WriteString("P50: n/a\nP90: n/a\nP99: n/a\n")
	} else {
		b.WriteString(fmt.Sprintf(
			"P50: %v\n"+
				"P90: %v\n"+
				"P99: %v\n",
			summary.P50,
			summary.P90,
			summary.P99,
		))
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
		b.WriteString(fmt.Sprintf("  %s: %d\n", entry.message, entry.count))
	}

	return b.String()
}
