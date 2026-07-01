package loadtest

import (
	"math"
	"time"
)

// [should-fix] The documented contract says p is in 0..1, while the implementation clamps
// out-of-range values and the tests promise that behavior. Choose one contract and state it
// explicitly; comments, implementation, and tests should describe the same boundary policy.
// percentile returns the p-th percentile (p in 0..1) of sortedDurations, which must be
// sorted in ascending order. It returns 0 for an empty slice.
func percentile(sortedDurations []time.Duration, p float64) time.Duration {
	n := len(sortedDurations)
	if n == 0 {
		return 0
	}

	index := int(math.Ceil(p*float64(n))) - 1

	index = max(0, min(n-1, index))

	return sortedDurations[index]
}
