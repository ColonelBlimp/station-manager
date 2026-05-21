package ft8

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/stretchr/testify/require"
)

// --- nextSlotBoundary -------------------------------------------------------

func TestNextSlotBoundary_TableDriven(t *testing.T) {
	mk := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339Nano, s)
		require.NoError(t, err)
		return ts
	}
	cases := []struct {
		now  string
		want string
	}{
		// Mid-slot, sub-second.
		{"2026-05-22T14:30:07.400Z", "2026-05-22T14:30:15Z"},
		// Just-shy-of-boundary.
		{"2026-05-22T14:30:14.999Z", "2026-05-22T14:30:15Z"},
		// Exactly on a boundary — next boundary is the strict successor.
		{"2026-05-22T14:30:15Z", "2026-05-22T14:30:30Z"},
		{"2026-05-22T14:30:30Z", "2026-05-22T14:30:45Z"},
		{"2026-05-22T14:30:45Z", "2026-05-22T14:31:00Z"},
		// Across the minute roll-over.
		{"2026-05-22T14:30:59.000Z", "2026-05-22T14:31:00Z"},
		// Across the hour roll-over.
		{"2026-05-22T14:59:59.500Z", "2026-05-22T15:00:00Z"},
		// Across the day roll-over.
		{"2026-05-22T23:59:55.000Z", "2026-05-23T00:00:00Z"},
		// Non-UTC input — return is normalised to UTC.
		{"2026-05-22T14:30:07.400+02:00", "2026-05-22T12:30:15Z"},
	}
	for _, tc := range cases {
		t.Run(tc.now, func(t *testing.T) {
			got := nextSlotBoundary(mk(tc.now))
			require.Equal(t, mk(tc.want), got)
			require.Equal(t, time.UTC, got.Location(), "boundary must be UTC")
			require.True(t, got.After(mk(tc.now)),
				"boundary must be strictly after now (was: %v vs %v)", got, mk(tc.now))
		})
	}
}

// --- Scheduler integration --------------------------------------------------

// TestScheduler_EmitsSlotWithCorrectShape verifies that once the ring
// has accumulated a full NMAX of samples, the next boundary fire
// produces exactly one Slot with the right size and a sane offset.
//
// The test drives both the source channel and time by pushing
// dsp.NMAX worth of samples upfront, then letting the real timer
// fire at the next UTC boundary. Worst-case wait is just under
// SlotDuration (15s). To keep the test fast in CI, we tolerate this
// by setting a per-test budget of 2 × SlotDuration.
func TestScheduler_EmitsSlotWithCorrectShape(t *testing.T) {
	if testing.Short() {
		t.Skip("scheduler integration test waits for a real UTC boundary; skip in -short")
	}

	source := make(chan []float32, 4)
	sch := NewScheduler(source, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*SlotDuration+5*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	// Pre-fill the ring with NMAX samples so the first boundary fires
	// a real slot (not a cold-start skip).
	full := make([]float32, dsp.NMAX)
	for i := range full {
		full[i] = float32(i%101) / 100.0 // arbitrary distinguishable pattern
	}
	source <- full

	select {
	case slot, ok := <-sch.Slots():
		require.True(t, ok, "slot channel closed before any slot emitted")
		require.Len(t, slot.Samples, dsp.NMAX)
		require.Equal(t, time.UTC, slot.StartUTC.Location())
		// Offset is the firing-late budget. Allow up to 500 ms — generous
		// for CI; healthy on a laptop is well under 50 ms.
		require.LessOrEqual(t, slot.OffsetMs, int64(500),
			"OffsetMs unhealthy: %d ms", slot.OffsetMs)
		// The slot should align to a 15-second boundary on UTC.
		require.Equal(t, 0, int(slot.StartUTC.UnixNano()%int64(SlotDuration)),
			"StartUTC not aligned to %v: %v", SlotDuration, slot.StartUTC)
	case <-ctx.Done():
		t.Fatalf("no slot emitted within %v: %v", 2*SlotDuration, ctx.Err())
	}

	cancel()
	<-runDone
}

func TestScheduler_ColdStartSkipsUntilRingFull(t *testing.T) {
	source := make(chan []float32, 4)
	sch := NewScheduler(source, nil)

	// Drive Run synchronously by stuffing a few small batches that
	// don't fill the ring, then cancelling. emitSlot should be a no-op
	// in this period — confirmed by the slot channel being empty.
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	source <- make([]float32, 100)
	source <- make([]float32, 100)

	// Give the scheduler goroutine a turn to drain the source.
	time.Sleep(20 * time.Millisecond)

	select {
	case slot := <-sch.Slots():
		t.Fatalf("unexpected slot emitted during cold start: %+v", slot)
	default:
		// expected — ring is nowhere near full
	}

	cancel()
	<-runDone
}

func TestScheduler_SourceCloseEndsRun(t *testing.T) {
	source := make(chan []float32, 1)
	sch := NewScheduler(source, nil)

	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(context.Background()) }()

	close(source)

	select {
	case err := <-runDone:
		require.NoError(t, err, "source close should be a clean shutdown")
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after source close")
	}

	// Slots channel must be closed too.
	_, ok := <-sch.Slots()
	require.False(t, ok, "Slots channel must be closed when Run returns")
}

func TestScheduler_DroppedSlot_WhenConsumerStalls(t *testing.T) {
	if testing.Short() {
		t.Skip("scheduler integration test waits for two real UTC boundaries; skip in -short")
	}

	source := make(chan []float32, 4)
	sch := NewScheduler(source, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*SlotDuration+5*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	full := make([]float32, dsp.NMAX)
	source <- full

	// Intentionally do NOT drain Slots; the channel capacity is
	// SchedulerSlotChannelBufferSize=1, so the second boundary fire
	// will increment the dropped counter.
	require.Eventually(t, func() bool {
		return sch.Dropped() > 0
	}, 3*SlotDuration, 100*time.Millisecond, "expected at least one dropped slot")

	cancel()
	<-runDone
}
