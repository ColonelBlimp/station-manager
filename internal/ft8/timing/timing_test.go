package timing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --------------- Mode helpers ------------------------------------------------

func TestMode_String(t *testing.T) {
	require.Equal(t, "FT8", FT8.String())
	require.Equal(t, "FT4", FT4.String())
	require.Equal(t, "unknown", Mode(99).String())
}

func TestMode_WindowDuration(t *testing.T) {
	require.Equal(t, 15*time.Second, FT8.WindowDuration())
	require.Equal(t, 7500*time.Millisecond, FT4.WindowDuration())
}

func TestMode_TXOffset(t *testing.T) {
	require.Equal(t, 1*time.Second, FT8.TXOffset())
	require.Equal(t, 500*time.Millisecond, FT4.TXOffset())
}

// --------------- Parity ------------------------------------------------------

func TestParity_String(t *testing.T) {
	require.Equal(t, "even", Even.String())
	require.Equal(t, "odd", Odd.String())
}

// --------------- CurrentWindowStart ------------------------------------------

func TestCurrentWindowStart_FT8(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "mid-window",
			now:  time.Date(2026, 4, 6, 14, 30, 7, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 0, 0, time.UTC),
		},
		{
			name: "exactly on boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
		},
		{
			name: "one nanosecond after boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 15, 1, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
		},
		{
			name: "just before next boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 29, 999_999_999, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
		},
		{
			name: "at minute start",
			now:  time.Date(2026, 4, 6, 14, 30, 0, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CurrentWindowStart(FT8, tt.now)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCurrentWindowStart_FT4(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "mid-window",
			now:  time.Date(2026, 4, 6, 14, 30, 3, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 0, 0, time.UTC),
		},
		{
			name: "exactly on 7.5s boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 7, 500_000_000, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 7, 500_000_000, time.UTC),
		},
		{
			name: "just after 7.5s boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 7, 500_000_001, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 7, 500_000_000, time.UTC),
		},
		{
			name: "at 15s boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CurrentWindowStart(FT4, tt.now)
			require.Equal(t, tt.want, got)
		})
	}
}

// --------------- NextWindowStart ---------------------------------------------

func TestNextWindowStart_FT8(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "mid-window returns next boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 7, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
		},
		{
			name: "exactly on boundary returns next",
			now:  time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 30, 0, time.UTC),
		},
		{
			name: "one ns after boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 15, 1, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 30, 0, time.UTC),
		},
		{
			name: "crosses minute boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 50, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 31, 0, 0, time.UTC),
		},
		{
			name: "crosses midnight",
			now:  time.Date(2026, 4, 6, 23, 59, 50, 0, time.UTC),
			want: time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextWindowStart(FT8, tt.now)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNextWindowStart_FT4(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "mid first window",
			now:  time.Date(2026, 4, 6, 14, 30, 3, 0, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 7, 500_000_000, time.UTC),
		},
		{
			name: "exactly on 7.5s boundary returns next",
			now:  time.Date(2026, 4, 6, 14, 30, 7, 500_000_000, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
		},
		{
			name: "just after 7.5s boundary",
			now:  time.Date(2026, 4, 6, 14, 30, 7, 500_000_001, time.UTC),
			want: time.Date(2026, 4, 6, 14, 30, 15, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextWindowStart(FT4, tt.now)
			require.Equal(t, tt.want, got)
		})
	}
}

// --------------- SlotParity --------------------------------------------------

func TestSlotParity_FT8(t *testing.T) {
	// Two consecutive 15s windows should have opposite parity.
	t0 := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC) // slot 0 from some epoch-aligned time
	p0 := SlotParity(FT8, t0)
	p1 := SlotParity(FT8, t0.Add(15*time.Second))
	require.NotEqual(t, p0, p1, "adjacent FT8 slots must have opposite parity")
}

func TestSlotParity_FT4(t *testing.T) {
	t0 := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)
	p0 := SlotParity(FT4, t0)
	p1 := SlotParity(FT4, t0.Add(7500*time.Millisecond))
	require.NotEqual(t, p0, p1, "adjacent FT4 slots must have opposite parity")
}

func TestSlotParity_SameWindow(t *testing.T) {
	// Two times within the same window must have the same parity.
	t0 := time.Date(2026, 4, 6, 14, 30, 1, 0, time.UTC)
	t1 := time.Date(2026, 4, 6, 14, 30, 14, 0, time.UTC)
	require.Equal(t, SlotParity(FT8, t0), SlotParity(FT8, t1))
}

// --------------- TimeUntilTX -------------------------------------------------

func TestTimeUntilTX_FT8(t *testing.T) {
	// At 14:30:07, next boundary is 14:30:15, TX offset +1s → TX at 14:30:16.
	// Remaining: 9 seconds.
	now := time.Date(2026, 4, 6, 14, 30, 7, 0, time.UTC)
	d := TimeUntilTX(FT8, now)
	require.Equal(t, 9*time.Second, d)
}

func TestTimeUntilTX_FT4(t *testing.T) {
	// At 14:30:03, next boundary is 14:30:07.5, TX offset +0.5s → TX at 14:30:08.
	// Remaining: 5 seconds.
	now := time.Date(2026, 4, 6, 14, 30, 3, 0, time.UTC)
	d := TimeUntilTX(FT4, now)
	require.Equal(t, 5*time.Second, d)
}

func TestTimeUntilTX_ExactlyOnBoundary(t *testing.T) {
	// On a boundary, next boundary is +15s, TX offset +1s → 16s total.
	now := time.Date(2026, 4, 6, 14, 30, 0, 0, time.UTC)
	d := TimeUntilTX(FT8, now)
	require.Equal(t, 16*time.Second, d)
}

// --------------- WaitForNext -------------------------------------------------

func TestWaitForNext_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := WaitForNext(ctx, FT8)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitForNext_ReturnsQuickly(t *testing.T) {
	// Use a very short timeout; the function should return before the timeout
	// if the next boundary is in the past (which it won't be), or on cancellation.
	// Here we verify it respects context deadlines.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := WaitForNext(ctx, FT8)
	// The next FT8 boundary is at most 15s away, so a 50ms deadline will expire.
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
