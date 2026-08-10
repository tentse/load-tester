package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tentse/load-tester/loadtest"
)

var ladderEdges = []time.Duration{
	1 * time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
}

const blockChars = "▏▎▍▌▋▊▉█"

func ladder(counts ...int64) []loadtest.Bucket {
	out := make([]loadtest.Bucket, len(counts))

	for index, count := range counts {
		var lo, hi time.Duration
		if index > 0 {
			lo = ladderEdges[index-1]
		}
		if index < len(ladderEdges) {
			hi = ladderEdges[index]
		}
		out[index] = loadtest.Bucket{Lo: lo, Hi: hi, Count: count}
	}

	return out
}

func TestBarGuards(t *testing.T) {
	tests := []struct {
		count, max int64
	}{
		{0, 10},
		{5, 0},
		{-1, 10},
		{0, 0},
		{10, -1},
	}

	for _, tc := range tests {
		if got := bar(tc.count, tc.max); got != "" {
			t.Errorf("bar(%d, %d) = %q, want empty", tc.count, tc.max, got)
		}
	}
}

func TestBarAtMaxIsExactlyBarWidth(t *testing.T) {
	for _, count := range []int64{1, 7, 250, 1_000_000, 40_000_000} {
		got := bar(count, count)

		if width := utf8.RuneCountInString(got); width != barWidth {
			t.Errorf("bar(%d, %d) width = %d runes, want %d", count, count, width, barWidth)
		}
		if trimmed := strings.Trim(got, "█"); trimmed != "" {
			t.Errorf("bar(%d, %d) = %q, want only full blocks", count, count, got)
		}
	}
}

func TestBarRunesNotBytes(t *testing.T) {
	got := bar(9, 9)

	if runes := utf8.RuneCountInString(got); runes != barWidth {
		t.Errorf("rune count = %d, want %d", runes, barWidth)
	}
	if bytes := len(got); bytes != barWidth*3 {
		t.Errorf("byte count = %d, want %d", bytes, barWidth*3)
	}
}

func TestBarNeverExceedsBarWidth(t *testing.T) {
	for _, max := range []int64{1, 3, 260, 999_999} {
		for count := int64(0); count <= max; count += max/7 + 1 {
			if width := utf8.RuneCountInString(bar(count, max)); width > barWidth {
				t.Errorf("bar(%d, %d) width = %d runes, want at most %d",
					count, max, width, barWidth)
			}
		}
	}
}

func TestBarTinySliver(t *testing.T) {
	got := bar(1, 1_000_000)

	if got != "▏" {
		t.Errorf("bar(1, 1000000) = %q, want %q", got, "▏")
	}
}

func TestBarPartialBlocks(t *testing.T) {
	tests := []struct {
		count, max int64
		want       string
	}{
		{1, 2, strings.Repeat("█", 17)},
		{1, 4, strings.Repeat("█", 8) + "▌"},
		{1, 8, strings.Repeat("█", 4) + "▎"},
		{1, 3, strings.Repeat("█", 11) + "▎"},
		{40, 260, strings.Repeat("█", 5) + "▏"},
	}

	for _, tc := range tests {
		if got := bar(tc.count, tc.max); got != tc.want {
			t.Errorf("bar(%d, %d) = %q, want %q", tc.count, tc.max, got, tc.want)
		}
	}
}

func TestBarScaleInvariant(t *testing.T) {
	small := bar(40, 260)
	large := bar(40_000_000, 260_000_000)

	if small != large {
		t.Errorf("bar(40, 260) = %q, bar(40000000, 260000000) = %q, want equal", small, large)
	}
}

func TestComma(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{100000, "100,000"},
		{1234567, "1,234,567"},
		{-1000, "-1,000"},
	}

	for _, tc := range tests {
		if got := comma(tc.in); got != tc.want {
			t.Errorf("comma(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderHistogramEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   []loadtest.Bucket
	}{
		{"nil", nil},
		{"empty", []loadtest.Bucket{}},
		{"no request landed anywhere", ladder(make([]int64, 14)...)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderHistogram(tc.in); got != "" {
				t.Errorf("renderHistogram() = %q, want empty", got)
			}
		})
	}
}

func TestRenderHistogram(t *testing.T) {
	got := renderHistogram(ladder(0, 40, 260, 155, 30, 6, 3, 5, 1, 0, 0, 0, 0, 0))

	want := "" +
		"  bucket       count\n" +
		"  <1ms             0\n" +
		"  1–2ms           40   █████▏\n" +
		"  2–5ms          260   ██████████████████████████████████\n" +
		"  5–10ms         155   ████████████████████▎\n" +
		"  10–20ms         30   ███▉\n" +
		"  20–50ms          6   ▊\n" +
		"  50–100ms         3   ▍\n" +
		"  100–200ms        5   ▋\n" +
		"  200–500ms        1   ▏\n" +
		"  500ms–1s         0\n" +
		"  1–2s             0\n" +
		"  2–5s             0\n" +
		"  5–10s            0\n" +
		"  ≥10s             0\n"

	if got != want {
		t.Errorf("renderHistogram() =\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderHistogramPrintsEveryBucket(t *testing.T) {
	bs := ladder(0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)

	lines := strings.Split(strings.TrimSuffix(renderHistogram(bs), "\n"), "\n")

	if len(lines) != len(bs)+1 {
		t.Fatalf("line count = %d, want %d (one header plus every bucket)", len(lines), len(bs)+1)
	}

	for index, line := range lines {
		start := strings.IndexAny(line, blockChars)

		if index != 4 {
			if start >= 0 {
				t.Errorf("line %d drew a bar for an empty bucket: %q", index, line)
			}
			continue
		}
		if start < 0 {
			t.Fatalf("the only bucket with a count drew no bar: %q", line)
		}
		if width := utf8.RuneCountInString(line[start:]); width != barWidth {
			t.Errorf("sole non-empty bucket bar width = %d runes, want %d", width, barWidth)
		}
	}
}

func TestRenderHistogramNoTrailingSpace(t *testing.T) {
	got := renderHistogram(ladder(0, 40, 260, 155, 30, 6, 3, 5, 1, 0, 0, 0, 0, 0))

	for index, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line %d has trailing whitespace: %q", index, line)
		}
	}
}

func TestRenderHistogramColumnsAlign(t *testing.T) {
	got := renderHistogram(ladder(0, 40, 260, 155, 30, 6, 3, 5, 1, 0, 0, 0, 0, 0))

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	want := utf8.RuneCountInString(lines[0])

	for index, line := range lines {
		counted := utf8.RuneCountInString(line)
		if strings.ContainsAny(line, blockChars) {
			continue // rows carrying a bar are longer by design
		}
		if counted != want {
			t.Errorf("line %d ends at rune %d, want %d: %q", index, counted, want, line)
		}
	}
}

func TestGetSummaryTextNilBucketsUnchanged(t *testing.T) {
	got := getSummaryText(loadtest.Summary{
		Total:      10,
		Succeeded:  8,
		Failed:     2,
		Elapsed:    2 * time.Second,
		Throughput: 4,
		P50:        10 * time.Millisecond,
		P90:        20 * time.Millisecond,
		P99:        30 * time.Millisecond,
	})

	want := "" +
		"Load test summary\n" +
		"Total: 10\n" +
		"Succeeded: 8\n" +
		"Failed: 2\n" +
		"Elapsed: 2s\n" +
		"Throughput: 4.00 req/s\n" +
		"P50: <= 10ms\n" +
		"P90: <= 20ms\n" +
		"P99: <= 30ms\n" +
		"Errors:\n" +
		"n/a\n"

	if got != want {
		t.Errorf("getSummaryText() =\n%q\nwant:\n%q", got, want)
	}
}

func TestGetSummaryTextHistogramPlacement(t *testing.T) {
	got := getSummaryText(loadtest.Summary{
		Total:      2,
		Succeeded:  2,
		Elapsed:    time.Second,
		Throughput: 2,
		P50:        5 * time.Millisecond,
		P90:        5 * time.Millisecond,
		P99:        5 * time.Millisecond,
		Buckets:    ladder(0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0),
	})

	percentile := strings.Index(got, "P99: <= 5ms")
	histogram := strings.Index(got, "  bucket       count")
	errorSection := strings.Index(got, "Errors:")

	if histogram < 0 {
		t.Fatalf("getSummaryText() printed no histogram:\n%s", got)
	}
	if !(percentile < histogram && histogram < errorSection) {
		t.Errorf("histogram at %d, want between the percentiles (%d) and Errors (%d):\n%s",
			histogram, percentile, errorSection, got)
	}
}
