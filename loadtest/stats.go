package loadtest

import (
	"math"
	"time"
)

// [nit] (FEEDBACK.md #9) The precondition "this slice must already be sorted" is stated only
// in the parameter's name. It holds today because the single caller sorts first
// (summary.go:64), but a second caller that forgets gets silently wrong percentiles — no
// error, no panic, just quietly incorrect numbers, which is the worst failure mode a stats
// function can have. Write the precondition into a doc comment. A parameter name is a hint,
// not a contract.
func percentile(sortedDurations []time.Duration, p float64) time.Duration {
	n := len(sortedDurations)
	if n == 0 {
		return 0
	}

	index := int(math.Ceil(p*float64(n))) - 1

	index = max(0, min(n-1, index))

	return sortedDurations[index]
}
