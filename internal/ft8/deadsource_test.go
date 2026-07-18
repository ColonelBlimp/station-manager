package ft8

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The dead-capture-stream watchdog's strike policy (deadsource.go): pure
// monitor tests here (no wall-clock slots — the scheduler owns the timer),
// plus the service-level release+reacquire plumbing at the bottom.

func newFiredMonitor() (*deadSourceMonitor, *[]string) {
	var fired []string
	m := &deadSourceMonitor{onDead: func(reason string) { fired = append(fired, reason) }}
	return m, &fired
}

// liveBatch is a batch with real audio (non-zero samples).
var liveBatch = []int16{0, 3, -7, 12}

func TestDeadSource_FirstBoundaryOnlyBaselines(t *testing.T) {
	m, fired := newFiredMonitor()
	// Nothing delivered before the first boundary — legitimately partial
	// (capture starts mid-slot); must not strike.
	m.onBoundary(0)
	m.onBoundary(0) // strike 1
	require.Empty(t, *fired, "one dead window is a hiccup, not a dead stream")
	m.onBoundary(0) // strike 2 → fire
	require.Equal(t, []string{"starved"}, *fired)
}

func TestDeadSource_SilentWindowsFire(t *testing.T) {
	m, fired := newFiredMonitor()
	filled := int64(0)
	window := func(batch []int16) {
		m.observeBatch(batch)
		filled += SlotSamples // full delivery every window
		m.onBoundary(filled)
	}
	window(make([]int16, 64)) // baseline
	window(make([]int16, 64)) // all-zero, full delta → strike 1 (silent)
	require.Empty(t, *fired)
	window(make([]int16, 64)) // strike 2 → fire
	require.Equal(t, []string{"silent"}, *fired)
}

func TestDeadSource_HealthyWindowResetsStrikes(t *testing.T) {
	m, fired := newFiredMonitor()
	filled := int64(0)
	window := func(live bool, delta int64) {
		if live {
			m.observeBatch(liveBatch)
		}
		filled += delta
		m.onBoundary(filled)
	}
	window(true, SlotSamples) // baseline
	window(false, 0)          // strike 1
	window(true, SlotSamples) // healthy → reset
	window(false, 0)          // strike 1 again
	require.Empty(t, *fired, "a healthy window between dead ones must reset the count")
	window(false, 0) // strike 2 → fire
	require.Equal(t, []string{"starved"}, *fired)
}

func TestDeadSource_FiresOnce(t *testing.T) {
	m, fired := newFiredMonitor()
	m.onBoundary(0)
	for i := 0; i < 6; i++ {
		m.onBoundary(0)
	}
	require.Len(t, *fired, 1, "the latch must limit the callback to once per run")
}

func TestDeadSource_StarvedBelowQuarterSlot(t *testing.T) {
	m, fired := newFiredMonitor()
	filled := int64(0)
	window := func(delta int64) {
		m.observeBatch(liveBatch) // live audio — starvation is about volume, not zeros
		filled += delta
		m.onBoundary(filled)
	}
	window(SlotSamples)              // baseline
	window(minLiveWindowSamples - 1) // under the floor → strike 1
	window(minLiveWindowSamples - 1) // strike 2 → fire
	require.Equal(t, []string{"starved"}, *fired)

	// At-or-above the floor with live audio is healthy.
	m2, fired2 := newFiredMonitor()
	filled = 0
	m2.onBoundary(0)
	for i := 0; i < 5; i++ {
		m2.observeBatch(liveBatch)
		filled += minLiveWindowSamples
		m2.onBoundary(filled)
	}
	require.Empty(t, *fired2)
}

func TestDeadSource_NilCallbackInert(t *testing.T) {
	var m deadSourceMonitor // onDead nil — SetOnDeadSource never called
	m.observeBatch(liveBatch)
	m.onBoundary(0)
	m.onBoundary(0)
	m.onBoundary(0) // must not panic or accumulate
	require.Zero(t, m.strikes)
}

// TestService_DeadSourceRestartsCapture covers the plumbing: the scheduler's
// dead-source verdict must release the dangling session AND re-acquire for the
// still-present subscriber — a fresh source Start (new OS stream) without the
// operator touching anything.
func TestService_DeadSourceRestartsCapture(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	_, unsub := s.Subscribe()
	defer unsub()
	require.Equal(t, 1, src.startCount(), "subscriber acquires the first session")

	s.onDeadCaptureSource("starved")

	require.Eventually(t, func() bool {
		return src.stopCount() >= 1 && src.startCount() == 2
	}, 5*time.Second, 10*time.Millisecond,
		"dead source must release the old session and start a fresh one")

	require.NoError(t, s.Stop())
}
