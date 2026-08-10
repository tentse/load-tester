package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tentse/load-tester/loadtest"
)

var blocks = []rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

const (
	barWidth = 34
	labelGap = 4
	barGap   = 3

	bucketHeader = "bucket"
	countHeader  = "count"
)

func bar(count, max int64) string {
	if count <= 0 || max <= 0 {
		return ""
	}

	slices := len(blocks)
	eighths := int(float64(count) / float64(max) * barWidth * float64(slices))

	if eighths == 0 {
		eighths = 1
	}
	full, rem := eighths/slices, eighths%slices

	var b strings.Builder
	for range full {
		b.WriteRune(blocks[slices-1])
	}
	if rem > 0 {
		b.WriteRune(blocks[rem-1])
	}

	return b.String()
}

func comma(n int64) string {
	digits := strconv.FormatInt(n, 10)

	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}

	var b strings.Builder
	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(digits[i])
	}

	return sign + b.String()
}

func renderHistogram(bs []loadtest.Bucket) string {
	if len(bs) == 0 {
		return ""
	}

	labels := make([]string, len(bs))
	counts := make([]string, len(bs))

	var total, peak int64
	labelWidth := utf8.RuneCountInString(bucketHeader)
	countWidth := utf8.RuneCountInString(countHeader)

	for index, bucket := range bs {
		labels[index] = bucket.Label()
		counts[index] = comma(bucket.Count)

		total += bucket.Count
		peak = max(peak, bucket.Count)

		labelWidth = max(labelWidth, utf8.RuneCountInString(labels[index]))
		countWidth = max(countWidth, utf8.RuneCountInString(counts[index]))
	}

	if total == 0 {
		return ""
	}

	var b strings.Builder

	writeRow := func(label, count, barText string) {
		row := fmt.Sprintf("  %-*s%s%*s%s%s",
			labelWidth, label,
			strings.Repeat(" ", labelGap),
			countWidth, count,
			strings.Repeat(" ", barGap),
			barText,
		)
		b.WriteString(strings.TrimRight(row, " "))
		b.WriteByte('\n')
	}

	writeRow(bucketHeader, countHeader, "")
	for index, bucket := range bs {
		writeRow(labels[index], counts[index], bar(bucket.Count, peak))
	}

	return b.String()
}
