package loadtest

import (
	"testing"
	"time"
)

func TestBucketsLadder(t *testing.T) {
	tests := []struct {
		lo, hi time.Duration
	}{
		{0, 1 * time.Millisecond},
		{1 * time.Millisecond, 2 * time.Millisecond},
		{2 * time.Millisecond, 5 * time.Millisecond},
		{5 * time.Millisecond, 10 * time.Millisecond},
		{10 * time.Millisecond, 20 * time.Millisecond},
		{20 * time.Millisecond, 50 * time.Millisecond},
		{50 * time.Millisecond, 100 * time.Millisecond},
		{100 * time.Millisecond, 200 * time.Millisecond},
		{200 * time.Millisecond, 500 * time.Millisecond},
		{500 * time.Millisecond, 1 * time.Second},
		{1 * time.Second, 2 * time.Second},
		{2 * time.Second, 5 * time.Second},
		{5 * time.Second, 10 * time.Second},
		{10 * time.Second, 0},
	}

	got := buckets(&latencyHistogram{})

	if len(got) != len(tests) {
		t.Fatalf("buckets() length = %d, want %d", len(got), len(tests))
	}

	for index, tc := range tests {
		if got[index].Lo != tc.lo || got[index].Hi != tc.hi {
			t.Errorf("buckets()[%d] = [%v, %v), want [%v, %v)",
				index, got[index].Lo, got[index].Hi, tc.lo, tc.hi)
		}
	}
}

func TestBucketsFencepost(t *testing.T) {
	got := buckets(&latencyHistogram{})

	if len(got) != len(bucketEdges)+1 {
		t.Fatalf("buckets() length = %d, want %d", len(got), len(bucketEdges)+1)
	}

	last := len(got) - 1
	lastEdge := bucketEdges[len(bucketEdges)-1]

	if got[0].Lo != 0 {
		t.Errorf("first bucket Lo = %v, want 0", got[0].Lo)
	}
	if got[last].Hi != 0 {
		t.Errorf("last bucket Hi = %v, want 0 (unbounded)", got[last].Hi)
	}
	if got[last-1].Hi != lastEdge {
		t.Errorf("bucket[%d].Hi = %v, want %v", last-1, got[last-1].Hi, lastEdge)
	}
	if got[last].Lo != lastEdge {
		t.Errorf("bucket[%d].Lo = %v, want %v", last, got[last].Lo, lastEdge)
	}

	for index := 1; index < len(got); index++ {
		if got[index].Lo != got[index-1].Hi {
			t.Errorf("gap between bucket %d and %d: %v != %v",
				index-1, index, got[index].Lo, got[index-1].Hi)
		}
	}

	for index, bucket := range got {
		if index != 0 && bucket.Lo == 0 {
			t.Errorf("bucket[%d] has a zero Lo, want only the first bucket to", index)
		}
		if index != last && bucket.Hi == 0 {
			t.Errorf("bucket[%d] has a zero Hi, want only the last bucket to", index)
		}
	}
}

func TestBucketsCarriesCounts(t *testing.T) {
	lh := latencyHistogram{}

	latencies := []time.Duration{
		500 * time.Microsecond,
		7 * time.Millisecond,
		7 * time.Millisecond,
		250 * time.Millisecond,
		30 * time.Second,
	}
	for _, latency := range latencies {
		lh.observe(latency)
	}

	got := buckets(&lh)

	want := map[int]int64{0: 1, 3: 2, 8: 1, 13: 1}

	var total int64
	for index, bucket := range got {
		total += bucket.Count
		if bucket.Count != want[index] {
			t.Errorf("bucket[%d] (%v) count = %d, want %d",
				index, bucket, bucket.Count, want[index])
		}
	}

	if total != lh.total {
		t.Errorf("bucket counts sum to %d, want %d", total, lh.total)
	}
}

func TestBucketsEmptyHistogram(t *testing.T) {
	got := buckets(&latencyHistogram{})

	if len(got) != len(bucketEdges)+1 {
		t.Fatalf("buckets() length = %d, want %d", len(got), len(bucketEdges)+1)
	}

	for index, bucket := range got {
		if bucket.Count != 0 {
			t.Errorf("bucket[%d] count = %d, want 0", index, bucket.Count)
		}
	}
}

func TestBucketsOverflowIsUnbounded(t *testing.T) {
	lh := latencyHistogram{}
	lh.observe(10 * time.Second)
	lh.observe(24 * time.Hour)

	got := buckets(&lh)
	last := got[len(got)-1]

	if last.Count != 2 {
		t.Errorf("overflow bucket count = %d, want 2", last.Count)
	}
	if last.Hi != 0 {
		t.Errorf("overflow bucket Hi = %v, want 0 (unbounded)", last.Hi)
	}
}

func TestBucketEdgesAreWholeMilliseconds(t *testing.T) {
	for index, edge := range bucketEdges {
		if edge <= 0 {
			t.Errorf("bucketEdges[%d] = %v, want a positive duration", index, edge)
		}
		if edge%time.Millisecond != 0 {
			t.Errorf("bucketEdges[%d] = %v, want a whole number of milliseconds", index, edge)
		}
		if edge >= time.Second && edge%time.Second != 0 {
			t.Errorf("bucketEdges[%d] = %v, want a whole number of seconds", index, edge)
		}
	}
}

func TestBucketLabel(t *testing.T) {
	want := []string{
		"<1ms",
		"1–2ms",
		"2–5ms",
		"5–10ms",
		"10–20ms",
		"20–50ms",
		"50–100ms",
		"100–200ms",
		"200–500ms",
		"500ms–1s",
		"1–2s",
		"2–5s",
		"5–10s",
		"≥10s",
	}

	got := buckets(&latencyHistogram{})

	if len(got) != len(want) {
		t.Fatalf("buckets() length = %d, want %d", len(got), len(want))
	}

	for index, bucket := range got {
		if label := bucket.Label(); label != want[index] {
			t.Errorf("buckets()[%d].Label() = %q, want %q", index, label, want[index])
		}
	}
}

func TestUnit(t *testing.T) {
	tests := []struct {
		in    time.Duration
		value int64
		unit  string
	}{
		{1 * time.Millisecond, 1, "ms"},
		{500 * time.Millisecond, 500, "ms"},
		{1 * time.Second, 1, "s"},
		{10 * time.Second, 10, "s"},
	}

	for _, tc := range tests {
		value, suffix := unit(tc.in)
		if value != tc.value || suffix != tc.unit {
			t.Errorf("unit(%v) = (%d, %q), want (%d, %q)",
				tc.in, value, suffix, tc.value, tc.unit)
		}
	}
}
