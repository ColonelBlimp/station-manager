package worker

import (
	"math/rand/v2"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// maxBackoffShift caps the exponent in the backoff formula to protect
// against overflow on pathologically large attempts values. With
// initial_backoff_sec >= 1, shifting by 30 already produces ~34 years —
// the max_backoff_sec clamp will have engaged long before this cap
// matters. It exists so the bit-shift itself can't overflow int64.
const maxBackoffShift = 30

// computeBackoff returns the delay before the next retry for a row that
// just transitioned transient. `attempt` is the post-increment attempts
// value (the first retry uses attempt=1, so the exponent is zero and
// the formula yields the raw initial backoff plus jitter).
//
// Formula — docs/v2-design/forwarding.md §5:
//
//	backoff = min(initial * 2^(attempt-1), max)
//	jitter  = uniform_random in [0, backoff * 0.2)
//	delay   = backoff + jitter
func computeBackoff(attempt int64, rc types.RetryConfig) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	initial := time.Duration(rc.InitialBackoffSec) * time.Second
	maxBackoff := time.Duration(rc.MaxBackoffSec) * time.Second
	if initial <= 0 {
		return 0
	}

	shift := min(attempt-1, maxBackoffShift)

	backoff := initial << shift
	if backoff <= 0 || backoff > maxBackoff {
		backoff = maxBackoff
	}
	if backoff <= 0 {
		return 0
	}

	// 20% jitter. rand.Int64N panics on non-positive n, so floor at 1 ns
	// when backoff/5 rounds to zero.
	j := max(int64(backoff)/5, 1)
	return backoff + time.Duration(rand.Int64N(j))
}
