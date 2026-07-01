package loadtest

import (
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {

	// [should-fix] Add an even-sized p50 case. Nearest-rank percentile selection is not the
	// same as averaging the two middle values, so this boundary deserves a hand-checked test
	// that records the intended behavior for future readers.
	tests := []struct {
		name            string
		sortedDurations []time.Duration
		p               float64
		want            time.Duration
	}{
		{
			name:            "p50 returns the median",
			sortedDurations: []time.Duration{10, 20, 30, 40, 50},
			p:               0.5,
			want:            time.Duration(30),
		},
		{
			name:            "single element returns itself",
			sortedDurations: []time.Duration{42},
			p:               0.34,
			want:            time.Duration(42),
		},
		{
			name:            "p90 of 11 values",
			sortedDurations: []time.Duration{10, 12, 24, 26, 35, 45, 56, 57, 58, 68, 89},
			p:               0.9,
			want:            time.Duration(68),
		},
		{
			name:            "p99 of small sample is the max",
			sortedDurations: []time.Duration{10, 12, 24, 26, 35, 45, 56, 57, 58, 68, 89, 200},
			p:               0.99,
			want:            time.Duration(200),
		},
		{
			name:            "p<0 clamps to first",
			sortedDurations: []time.Duration{10, 20, 30, 40, 50},
			p:               -0.1,
			want:            time.Duration(10),
		},
		// [should-fix] Add exact p=0 and p=1 cases. The out-of-range cases exercise clamping,
		// but they do not pin the inclusive boundaries promised by the 0..1 contract; p=0 is
		// especially useful because nearest-rank initially computes index -1 before clamping.
		{
			name:            "p>1 clamps to last",
			sortedDurations: []time.Duration{10, 20, 30, 40, 50},
			p:               1.5,
			want:            time.Duration(50),
		},
		{
			name:            "empty slices returns 0",
			sortedDurations: []time.Duration{},
			p:               0.2,
			want:            time.Duration(0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := percentile(tc.sortedDurations, tc.p)

			if got != tc.want {
				t.Errorf("percentile(%v, %v) = %v, want %v", tc.sortedDurations, tc.p, got, tc.want)
			}
		})
	}
}
