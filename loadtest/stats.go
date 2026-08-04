package loadtest

import (
	"math"
	"time"
)

func percentile(lh latencyHistogram, p float64) time.Duration {

	p = min(float64(1), p)
	p = max(float64(0), p)

	percentileIndex := max(1, int64(math.Ceil(p*float64(lh.total))))

	count := int64(0)

	sizeBucketEdge := len(bucketEdges)

	for index, value := range lh.counts {
		if index == sizeBucketEdge {
			break
		}
		count += value
		if count >= percentileIndex {
			return bucketEdges[index]
		}
	}
	return lh.max
}
