// Package timing provides precise TX/RX window boundary calculations for
// FT8 and FT4 digital modes.
//
// All pure functions accept a [time.Time] and return deterministic results,
// making them trivially testable. The only side-effecting function is
// [WaitForNext], which blocks until the next window boundary.
//
// The package assumes the system clock is NTP-synced (within ±1 s). It does
// not implement NTP synchronisation itself.
package timing

import (
	"context"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Mode identifies an FT8 or FT4 digital mode, each with its own window
// duration and transmit offset.
type Mode int

const (
	// FT8 uses 15-second windows with a 1.0 s TX offset.
	FT8 Mode = iota
	// FT4 uses 7.5-second windows with a 0.5 s TX offset.
	FT4
)

// String returns the mode name.
func (m Mode) String() string {
	switch m {
	case FT8:
		return "FT8"
	case FT4:
		return "FT4"
	default:
		return "unknown"
	}
}

// WindowDuration returns the period of a single TX/RX window.
//
//   - FT8: 15 s
//   - FT4: 7.5 s
func (m Mode) WindowDuration() time.Duration {
	switch m {
	case FT4:
		return 7500 * time.Millisecond
	default: // FT8
		return 15 * time.Second
	}
}

// TXOffset returns the delay from the window boundary to the actual TX start.
//
//   - FT8: 1.0 s
//   - FT4: 0.5 s
func (m Mode) TXOffset() time.Duration {
	switch m {
	case FT4:
		return 500 * time.Millisecond
	default: // FT8
		return 1 * time.Second
	}
}

// Parity represents whether a window slot is even or odd. FT8/FT4 operators
// choose one parity for transmitting; their QSO partner uses the other.
type Parity int

const (
	Even Parity = iota
	Odd
)

// String returns "even" or "odd".
func (p Parity) String() string {
	if p == Even {
		return "even"
	}
	return "odd"
}

// windowNanos returns the window duration in nanoseconds. Using integer
// arithmetic avoids floating-point drift in boundary calculations.
func windowNanos(m Mode) int64 {
	return m.WindowDuration().Nanoseconds()
}

// CurrentWindowStart returns the start of the window that contains now.
// The result is always ≤ now.
func CurrentWindowStart(m Mode, now time.Time) time.Time {
	wn := windowNanos(m)
	// Nanoseconds elapsed since the Unix epoch.
	ns := now.UnixNano()
	// Floor to the nearest window boundary.
	boundary := ns - (ns % wn)
	return time.Unix(0, boundary).UTC()
}

// NextWindowStart returns the start of the next window boundary strictly
// after now.
func NextWindowStart(m Mode, now time.Time) time.Time {
	wn := windowNanos(m)
	ns := now.UnixNano()
	boundary := ns - (ns % wn) + wn
	return time.Unix(0, boundary).UTC()
}

// SlotParity returns the parity (even/odd) of the window that contains now.
// Slot index 0 (the Unix epoch) is defined as Even.
//
// Note: pre-epoch times (before 1970) may produce unexpected parity due to
// Go's signed modulus operator. This is not a concern for real-world use.
func SlotParity(m Mode, now time.Time) Parity {
	wn := windowNanos(m)
	idx := now.UnixNano() / wn
	if idx%2 == 0 {
		return Even
	}
	return Odd
}

// TimeUntilTX returns the duration from now until the next TX start, which is
// the next window boundary plus the mode's TX offset.
func TimeUntilTX(m Mode, now time.Time) time.Duration {
	return NextWindowStart(m, now).Add(m.TXOffset()).Sub(now)
}

// WaitForNext blocks until the next window boundary for the given mode.
// It returns the window start time, or an error if ctx is cancelled before
// the boundary is reached.
func WaitForNext(ctx context.Context, m Mode) (time.Time, error) {
	const op errors.Op = "timing.WaitForNext"

	target := NextWindowStart(m, time.Now())
	d := time.Until(target)

	if d <= 0 {
		// Already past the boundary (can happen if the goroutine was delayed
		// between NextWindowStart and time.Until by an entire window duration).
		// This defensive guard is intentionally untested — it requires injecting
		// a clock abstraction that isn't warranted for this package's scope.
		return target, nil
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return target, nil
	case <-ctx.Done():
		return time.Time{}, errors.New(op).Err(ctx.Err()).Msg("context cancelled while waiting for window boundary")
	}
}
