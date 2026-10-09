// Package retry provides capped exponential delays without deciding which
// failures are retryable or how many attempts a caller may make.
package retry

import "time"

type Policy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

// Delay returns the wait after a consecutive failure. The first failure is 1;
// callers reset their count after success. Zero values use 5s and 640s.
func (p Policy) Delay(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	base, limit := p.BaseDelay, p.MaxDelay
	if base <= 0 {
		base = 5 * time.Second
	}
	if limit <= 0 {
		limit = 640 * time.Second
	}
	delay := min(base, limit)
	for n := 1; n < failures && delay < limit; n++ {
		// Compare before doubling to avoid overflowing time.Duration.
		if delay > limit-delay {
			return limit
		}
		delay *= 2
	}
	return delay
}
