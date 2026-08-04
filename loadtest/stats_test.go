package loadtest

import (
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {

	tests := []struct {
		name      string
		latencies []time.Duration
		p         float64
		want      time.Duration
	}{
		{
			name: "p50 returns the median",
			latencies: []time.Duration{
				1 * time.Millisecond,
				3 * time.Millisecond,
				7 * time.Millisecond,
				30 * time.Millisecond,
				300 * time.Millisecond,
			},
			p:    0.5,
			want: 10 * time.Millisecond, // median 7ms falls in [5ms, 10ms)
		},
		{
			name:      "single latency",
			latencies: []time.Duration{42 * time.Millisecond},
			p:         0.5,
			want:      50 * time.Millisecond, // [20ms, 50ms) edge clamped to max
		},
		{
			name: "p90 of 11 values",
			latencies: []time.Duration{
				1 * time.Millisecond,
				2 * time.Millisecond,
				3 * time.Millisecond,
				4 * time.Millisecond,
				6 * time.Millisecond,
				8 * time.Millisecond,
				15 * time.Millisecond,
				30 * time.Millisecond,
				60 * time.Millisecond,
				150 * time.Millisecond,
				800 * time.Millisecond,
			},
			p:    0.9,
			want: 200 * time.Millisecond, // 10th value 150ms falls in [100ms, 200ms)
		},
		{
			name: "p99 of small sample is the max",
			latencies: []time.Duration{
				1 * time.Millisecond,
				2 * time.Millisecond,
				3 * time.Millisecond,
				4 * time.Millisecond,
				6 * time.Millisecond,
				8 * time.Millisecond,
				15 * time.Millisecond,
				30 * time.Millisecond,
				60 * time.Millisecond,
				150 * time.Millisecond,
				800 * time.Millisecond,
				12 * time.Second,
			},
			p:    0.99,
			want: 12 * time.Second, // overflow bucket has no edge, so max is reported
		},
		{
			name: "p<0 clamps to first",
			latencies: []time.Duration{
				1 * time.Millisecond,
				3 * time.Millisecond,
				7 * time.Millisecond,
				30 * time.Millisecond,
				300 * time.Millisecond,
			},
			p:    -0.1,
			want: 2 * time.Millisecond, // normalize negative percentile to 0th percentile which falls to [1ms, 2ms)
		},
		{
			name: "p>1 clamps to last",
			latencies: []time.Duration{
				1 * time.Millisecond,
				3 * time.Millisecond,
				7 * time.Millisecond,
				30 * time.Millisecond,
				300 * time.Millisecond,
			},
			p:    1.5,
			want: 500 * time.Millisecond, // rank exceeds total, so max is reported
		},
		{
			name: "p>1 clamps to last with +Inf result",
			latencies: []time.Duration{
				1 * time.Millisecond,
				3 * time.Millisecond,
				7 * time.Millisecond,
				30 * time.Millisecond,
				300 * time.Second,
			},
			p:    1.5,
			want: 300 * time.Second, // rank exceeds total, so max is reported
		},
		{
			name:      "empty histogram returns 0",
			latencies: nil,
			p:         0.2,
			want:      time.Duration(0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			lh := latencyHistogram{}

			for _, value := range tc.latencies {
				lh.observe(value)
			}

			got := percentile(lh, tc.p)

			if got != tc.want {
				t.Errorf("percentile(%v, %v) = %v, want %v", tc.latencies, tc.p, got, tc.want)
			}
		})
	}
}
