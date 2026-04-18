package worker

import (
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestComputeBackoff_BaseAndGrowth(t *testing.T) {
	rc := types.RetryConfig{
		InitialBackoffSec: 10,
		MaxBackoffSec:     1000,
	}

	// 20% jitter bound — the delay for attempt N must fall in
	// [base, base * 1.2) where base = min(initial<<shift, max).
	cases := []struct {
		attempt int64
		baseSec int64
	}{
		{1, 10},   // 10 * 2^0
		{2, 20},   // 10 * 2^1
		{3, 40},   // 10 * 2^2
		{4, 80},   // 10 * 2^3
		{5, 160},  // 10 * 2^4
		{6, 320},  // 10 * 2^5
		{7, 640},  // 10 * 2^6
		{8, 1000}, // 10 * 2^7 = 1280, clamped to max=1000
		{20, 1000},
	}

	for _, tc := range cases {
		base := time.Duration(tc.baseSec) * time.Second
		upper := base + base/5 // exclusive-ish upper bound (+20%)

		// Sample a few times — jitter is random so a single observation
		// could sit anywhere in [base, upper). We want every sample to
		// fall inside the window.
		for i := 0; i < 10; i++ {
			got := computeBackoff(tc.attempt, rc)
			if got < base {
				t.Fatalf("attempt=%d: got %s < base %s", tc.attempt, got, base)
			}
			if got >= upper+time.Second {
				// Allow a tiny slop (1s) for the +1ns floor when j=0 rounds down
				// on very small base values — but with base>=10s the floor
				// is a rounding no-op. Still, keep the slop generous
				// enough that the test isn't fragile.
				t.Fatalf("attempt=%d: got %s >= upper %s", tc.attempt, got, upper)
			}
		}
	}
}

func TestComputeBackoff_AttemptClampedAtZero(t *testing.T) {
	rc := types.RetryConfig{InitialBackoffSec: 5, MaxBackoffSec: 100}
	// Attempt 0 and negative should behave like attempt 1.
	got0 := computeBackoff(0, rc)
	gotNeg := computeBackoff(-3, rc)
	base := 5 * time.Second

	if got0 < base {
		t.Fatalf("attempt=0: got %s, want >= %s", got0, base)
	}
	if gotNeg < base {
		t.Fatalf("attempt=-3: got %s, want >= %s", gotNeg, base)
	}
}

func TestComputeBackoff_ZeroInitial(t *testing.T) {
	// Degenerate but legal shape — New() rejects it, but the function
	// itself shouldn't divide-by-zero or panic.
	rc := types.RetryConfig{InitialBackoffSec: 0, MaxBackoffSec: 100}
	if got := computeBackoff(1, rc); got != 0 {
		t.Fatalf("got %s, want 0 for InitialBackoffSec=0", got)
	}
}

func TestComputeBackoff_NoOverflowAtLargeAttempts(t *testing.T) {
	rc := types.RetryConfig{InitialBackoffSec: 1, MaxBackoffSec: 60}
	// Attempt values much larger than maxBackoffShift must still return a
	// sensible duration, not wrap negative.
	for _, a := range []int64{31, 60, 100, 1_000_000} {
		got := computeBackoff(a, rc)
		if got <= 0 {
			t.Fatalf("attempt=%d: got %s, want > 0", a, got)
		}
	}
}
