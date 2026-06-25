package loadtest

import (
	"math"
	"time"
)

func percentile(sortedDurations []time.Duration, p float64) time.Duration {
	// percentile returns the p-th percentile (p in 0..1) of sortedDurations, which must be
	// sorted in ascending order. It returns 0 for an empty slice.
	n := len(sortedDurations)
	if n == 0 {
		return 0
	}

	index := int(math.Ceil(p*float64(n))) - 1

	index = max(0, min(n-1, index))

	return sortedDurations[index]
}
