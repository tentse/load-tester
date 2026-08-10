package loadtest

import (
	"fmt"
	"time"
)

// Bucket is one step of the latency ladder and the number of successful requests
// that fell into it.
//
// Lo is part of the range and Hi is not, so a latency of exactly 1ms belongs to
// the 1ms to 2ms bucket and never to the one below it. Every latency lands in
// exactly one bucket. The first bucket starts at an Lo of zero. The last bucket
// has no upper limit and reports an Hi of zero to say so, which does not mean the
// bucket is empty.
type Bucket struct {
	Lo, Hi time.Duration
	Count  int64
}

// Label names the range a bucket covers.
//
// When both ends use the same unit it is written once, so the 1ms to 2ms bucket
// reads "1–2ms". When the ends use different units both are written, so the 500ms
// to 1s bucket reads "500ms–1s". The first and last buckets read "<1ms" and
// "≥10s".
func (b Bucket) Label() string {
	low, lowUnit := unit(b.Lo)
	high, highUnit := unit(b.Hi)

	switch {
	case b.Lo == 0:
		return fmt.Sprintf("<%d%s", high, highUnit)
	case b.Hi == 0:
		return fmt.Sprintf("≥%d%s", low, lowUnit)
	case lowUnit == highUnit:
		return fmt.Sprintf("%d–%d%s", low, high, highUnit)
	default:
		return fmt.Sprintf("%d%s–%d%s", low, lowUnit, high, highUnit)
	}
}

func unit(d time.Duration) (int64, string) {
	if d < time.Second {
		return int64(d / time.Millisecond), "ms"
	}
	return int64(d / time.Second), "s"
}

func buckets(lh *latencyHistogram) []Bucket {
	out := make([]Bucket, len(lh.counts))

	for index, count := range lh.counts {
		var lo, hi time.Duration
		if index > 0 {
			lo = bucketEdges[index-1]
		}
		if index < len(bucketEdges) {
			hi = bucketEdges[index]
		}
		out[index] = Bucket{Lo: lo, Hi: hi, Count: count}
	}

	return out
}
