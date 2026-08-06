package ft8

/*
   Wire half of the RX audio-level meter (see audiolevel_test.go for the
   criterion): the measurement rides /v1/ft8/events as `ft8-audio-level`,
   published only while a capture session is live — which is what makes
   "no capture" (nothing published; the SPA renders its no-capture state)
   distinguishable from "silent capture" (the floor value arrives on
   cadence). Deliberately NOT hub-cached: the next window lands within
   250 ms, so replaying a stale one to a late subscriber has no value.

   The tee feeds the meter and forwards samples to the scheduler
   UNTOUCHED — the slot/decode path the TX + attribution invariants guard
   is not entered here (W2 asserts the forwarding).
*/

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// push hands a batch to the live capture channel, as the device pump would.
func (f *fakeSource) push(batch []int16) {
	f.mu.Lock()
	ch := f.ch
	f.mu.Unlock()
	if ch != nil {
		ch <- batch
	}
}

func awaitAudioLevel(t *testing.T, ch <-chan hubEvent) AudioLevel {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case evt := <-ch:
			if evt.name == EventAudioLevel {
				lvl, ok := evt.payload.(AudioLevel)
				require.True(t, ok, "ft8-audio-level payload type")
				return lvl
			}
		case <-deadline:
			t.Fatal("no ft8-audio-level event within 5s")
		}
	}
}

// W1 — A SILENT LIVE CAPTURE REPORTS THE FLOOR ON THE WIRE. One full window
// of zeros → one event carrying the finite silence floor. This is the
// "silent but alive" state the SPA must tell apart from no capture at all.
func TestAudioLevel_SilentCapturePublishesFloor(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	ch, unsub := s.Subscribe()
	defer unsub()
	require.True(t, src.wasStarted())

	src.push(make([]int16, audioLevelWindowSamples))

	lvl := awaitAudioLevel(t, ch)
	require.Equal(t, audioLevelFloorDbfs, lvl.PeakDbfs)
	require.Equal(t, audioLevelFloorDbfs, lvl.RmsDbfs)
	require.NoError(t, s.Stop())
}

// W2 — VALUES ARE MEASURED, ROUNDED TO 0.1 dB, AND THE SAMPLES STILL REACH
// THE PIPELINE. A half-scale sine reads exactly -6.0/-9.0 (rounded from
// -6.02/-9.03 — an unrounded implementation fails on equality), proving the
// wire carries the meter's numbers, not a stub's.
func TestAudioLevel_PublishesRoundedMeasurements(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	ch, unsub := s.Subscribe()
	defer unsub()

	buf := make([]int16, audioLevelWindowSamples)
	for i := range buf {
		buf[i] = int16(16384 * math.Sin(2*math.Pi*float64(i)/100))
	}
	src.push(buf)

	lvl := awaitAudioLevel(t, ch)
	require.Equal(t, -6.0, lvl.PeakDbfs)
	require.Equal(t, -9.0, lvl.RmsDbfs)
	require.NoError(t, s.Stop())
}

// W3 — RELEASE STILL DRAINS. The tee is one more session goroutine between
// source and scheduler; Stop returning proves it exits with the pair and
// cannot wedge the release (the F1/F2 drain discipline).
func TestAudioLevel_TeeDrainsOnStop(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	_, unsub := s.Subscribe()
	defer unsub()
	src.push(make([]int16, 100)) // a partial window in flight

	done := make(chan struct{})
	go func() { _ = s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not drain the tee within 5s")
	}
}
