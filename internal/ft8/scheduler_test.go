package ft8

import (
	"context"
	"testing"
	"time"

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

// TestScheduler_EmitsSlotWithCorrectShape verifies that once the ring has
// accumulated a full SlotSamples of audio, the next boundary fire produces
// exactly one Slot with the right size and a sane offset.
//
// The test pre-fills the ring, then lets the real timer fire at the next UTC
// boundary. Worst-case wait is just under SlotDuration (15s), so it skips
// under -short and runs with a 2×SlotDuration budget otherwise.
func TestScheduler_EmitsSlotWithCorrectShape(t *testing.T) {
	if testing.Short() {
		t.Skip("scheduler integration test waits for a real UTC boundary; skip in -short")
	}

	source := make(chan []int16, 4)
	sch := NewScheduler(source, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*SlotDuration+5*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	// Pre-fill the ring with SlotSamples samples so the first boundary fires
	// a real slot (not a cold-start skip).
	full := make([]int16, SlotSamples)
	for i := range full {
		full[i] = int16(i % 1000) // arbitrary distinguishable pattern
	}
	source <- full

	select {
	case slot, ok := <-sch.Slots():
		require.True(t, ok, "slot channel closed before any slot emitted")
		require.Len(t, slot.Samples, SlotSamples)
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
	source := make(chan []int16, 4)
	sch := NewScheduler(source, nil)

	// Drive Run by stuffing a few small batches that don't fill the ring,
	// then cancelling. emitSlot should be a no-op in this period — confirmed
	// by the slot channel being empty.
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	source <- make([]int16, 100)
	source <- make([]int16, 100)

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
	source := make(chan []int16, 1)
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

	source := make(chan []int16, 4)
	sch := NewScheduler(source, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*SlotDuration+5*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	full := make([]int16, SlotSamples)
	source <- full

	// Intentionally do NOT drain Slots; the channel capacity is
	// SchedulerSlotChannelBufferSize=1, so the second boundary fire will
	// increment the dropped counter.
	require.Eventually(t, func() bool {
		return sch.Dropped() > 0
	}, 3*SlotDuration, 100*time.Millisecond, "expected at least one dropped slot")

	cancel()
	<-runDone
}

// --- slot→dial attribution --------------------------------------------------

// TestScheduler_AttributesSlotToDialAtBothEnds pins the rule that makes an
// occupancy report self-identifying: a slot carries a frequency only when the
// rig demonstrably held that frequency for the WHOLE window. The boundary that
// emits a slot is also the start of the next one, so the reading taken there and
// the previous boundary's reading bracket the emitted slot exactly.
func TestScheduler_AttributesSlotToDialAtBothEnds(t *testing.T) {
	full := make([]int16, SlotSamples)
	target := time.Date(2026, 7, 27, 12, 0, 15, 0, time.UTC)

	cases := []struct {
		name        string
		prevDial    float64
		prevOK      bool
		dial        float64
		dialOK      bool
		wantDial    float64
		wantChanged bool
	}{
		{"held one frequency", 14.074, true, 14.074, true, 14.074, false},
		// The straddling slot: audio from two bands, describing neither.
		{"dial moved mid-slot", 14.074, true, 7.074, true, 7.074, true},
		// One end unknown cannot prove the rig held anything: stay unattributed
		// rather than claim a band. Covers CAT dropping or coming up mid-slot,
		// and the very first boundary after Run starts.
		{"unknown at slot start", 0, false, 14.074, true, 0, false},
		{"unknown at slot end", 14.074, true, 0, false, 0, false},
		{"no CAT at all", 0, false, 0, false, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sch := NewScheduler(make(chan []int16), nil)
			sch.prevDial, sch.prevDialOK = tc.prevDial, tc.prevOK
			ring := newSampleRing(SlotSamples)
			ring.Append(full)

			sch.emitSlot(ring, target, target, tc.dial, tc.dialOK)

			select {
			case slot := <-sch.out:
				require.Equal(t, tc.wantDial, slot.DialMHz)
				require.Equal(t, tc.wantChanged, slot.DialChanged)
			default:
				t.Fatal("emitSlot published nothing")
			}
		})
	}
}

// An unset dial source is the no-CAT deployment, not an error: every slot is
// simply unattributed and the SPA falls back to its own view of the band.
func TestScheduler_ReadDial_UnsetSourceReportsUnknown(t *testing.T) {
	sch := NewScheduler(make(chan []int16), nil)
	_, ok := sch.readDial()
	require.False(t, ok, "nil dial source must report unknown, not zero-as-truth")

	sch.SetDialSource(func() (float64, bool) { return 14.074, true })
	mhz, ok := sch.readDial()
	require.True(t, ok)
	require.Equal(t, 14.074, mhz)
}
