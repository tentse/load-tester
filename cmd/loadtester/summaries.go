package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/tentse/load-tester/loadtest"
)

func renderSummaries(w io.Writer, summaries map[string]loadtest.Summary) error {
	_, err := io.WriteString(w, getSummariesText(summaries))
	if err != nil {
		return fmt.Errorf("write summaries: %w", err)
	}
	return nil
}

func getSummariesText(summaries map[string]loadtest.Summary) string {
	var b strings.Builder

	for index, name := range slices.Sorted(maps.Keys(summaries)) {
		if index > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Name: %s\n", name)
		b.WriteString(summaryBody(summaries[name]))
	}

	return b.String()
}
