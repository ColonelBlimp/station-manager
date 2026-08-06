package ft8

/*
   Wire half of the RX audio-level meter (see audiolevel_test.go for the
   criterion): the measurement rides /v1/ft8/events as `ft8-audio-level`,
   published only while a capture session is live — which is what makes
   "no capture" (nothing published; the SPA renders its no-capture state)
   distinguishable from "silent capture" (the floor value arrives on
   cadence).

   DELIVERY IS PULL, NOT PUSH (clean-room review d22eff6b, P2): the meter
   ticks ~4 times a second, and pushed through the hub they filled the
   8-slot subscriber buffer within a ~2 s SSE write stall — after which the
   NEXT event evicted the subscriber, tearing down the stream; through the
   capture linger that can disarm TX and abandon an active QSO. A write
   stall that was harmless before the meter existed must stay harmless: the
   hub keeps only the LATEST reading (+ generation), and each SSE writer
   emits it from its own short ticker when it changed. Conflation is
   structural — a stalled reader simply emits the newest value when it
   recovers — and the eviction ruling's arithmetic ("buffers stay 8",
   2026-08-01) is restored exactly: real events keep the whole buffer.

   The tee feeds the meter and forwards samples to the scheduler UNTOUCHED —
   the slot/decode path the TX + attribution invariants guard is not entered.
*/

import (
	"bufio"
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
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

// H1 — METER TRAFFIC NEVER OCCUPIES NOR EVICTS A SUBSCRIBER BUFFER (the
// review-d22eff6b rule). A subscriber that drains NOTHING while ten meter
// windows flow must survive with an EMPTY buffer — under push delivery the
// first eight windows filled it and the ninth evicted (channel closed), and
// even a non-evicting push would leave real events fighting meter ticks for
// slots. The follow-up real event proves the subscription is still live and
// the whole buffer is still there for it.
func TestAudioLevel_MeterTrafficCannotEvict(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	ch, unsub := s.Subscribe()
	defer unsub()
	require.True(t, src.wasStarted())

	// Ten full windows with nobody draining — more than the buffer holds.
	for range 10 {
		src.push(make([]int16, audioLevelWindowSamples))
	}

	// The subscriber is still registered and its buffer holds NO meter
	// events: the next real event arrives as the FIRST thing in the channel.
	s.hub.publish(hubEvent{name: EventDecode, payload: DecodeReport{}})
	select {
	case evt, ok := <-ch:
		require.True(t, ok, "subscriber was evicted by meter traffic")
		require.Equal(t, EventDecode, evt.name,
			"meter events must not occupy the subscriber buffer")
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
	}
	require.NoError(t, s.Stop())
}

// sseAudioLevel reads SSE lines until an ft8-audio-level frame's data arrives.
func sseAudioLevel(t *testing.T, body *bufio.Scanner) string {
	t.Helper()
	sawEvent := false
	for body.Scan() {
		line := body.Text()
		if strings.HasPrefix(line, "event: "+EventAudioLevel) {
			sawEvent = true
			continue
		}
		if sawEvent && strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("stream ended without an %s frame: %v", EventAudioLevel, body.Err())
	return ""
}

func dialDownAudioEmit(t *testing.T) {
	t.Helper()
	prev := audioLevelEmitInterval
	audioLevelEmitInterval = 20 * time.Millisecond
	t.Cleanup(func() { audioLevelEmitInterval = prev })
}

// W1 — A SILENT LIVE CAPTURE REPORTS THE FLOOR ON THE WIRE: one window of
// zeros → an SSE frame carrying the finite silence floor. This is the
// "silent but alive" state the SPA must tell apart from no capture at all.
func TestAudioLevel_SilentCapturePublishesFloor(t *testing.T) {
	dialDownAudioEmit(t)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	srv := httptest.NewServer(s.HTTPHandler(make(chan struct{})))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	waitForOccSubscribers(t, s, 1, time.Second)

	src.push(make([]int16, audioLevelWindowSamples))

	data := sseAudioLevel(t, bufio.NewScanner(resp.Body))
	require.Contains(t, data, `"peak_dbfs":-120`)
	require.Contains(t, data, `"rms_dbfs":-120`)
	require.NoError(t, s.Stop())
}

// W2 — VALUES ARE MEASURED, ROUNDED TO 0.1 dB, AND REACH THE WIRE: a
// half-scale sine reads exactly -6/-9 (rounded from -6.02/-9.03 — an
// unrounded implementation fails on the JSON text), proving the wire carries
// the meter's numbers, not a stub's.
func TestAudioLevel_PublishesRoundedMeasurements(t *testing.T) {
	dialDownAudioEmit(t)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	srv := httptest.NewServer(s.HTTPHandler(make(chan struct{})))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	waitForOccSubscribers(t, s, 1, time.Second)

	buf := make([]int16, audioLevelWindowSamples)
	for i := range buf {
		buf[i] = int16(16384 * math.Sin(2*math.Pi*float64(i)/100))
	}
	src.push(buf)

	data := sseAudioLevel(t, bufio.NewScanner(resp.Body))
	require.Contains(t, data, `"peak_dbfs":-6`)
	require.Contains(t, data, `"rms_dbfs":-9`)
	require.NoError(t, s.Stop())
}

// H4 — A STALE LOOP-EXIT CALLBACK CANNOT TOUCH A SESSION IT DOES NOT OWN
// (clean-room review ed13a9c6, P1): both scheduler and decoder defer
// onCaptureLoopExit, and both can pass its UNLOCKED runCtx fast-path before
// either locks. The second callback, running after a replacement capture has
// started, then tore the REPLACEMENT down — capturing=false, the
// replacement's context cancelled, its source stopped, and (post the
// session-token fix) its publish token retired, permanently muting the live
// meter. Ownership is a capture GENERATION captured at goroutine spawn and
// revalidated under the lock: a callback whose generation is not current is
// a no-op. The fixture calls the handler directly with a live ctx (passing
// the unlocked fast-path, as in the race) and a stale generation — the
// deterministic form of the interleaving.
func TestCaptureLoopExit_StaleCallbackCannotKillReplacement(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	ch, unsub := s.Subscribe()
	defer unsub()
	require.True(t, src.wasStarted(), "fixture: the live session this callback must not touch")

	s.mu.Lock()
	staleGen := s.captureGen - 1 // a prior session's generation
	s.mu.Unlock()
	s.onCaptureLoopExit(context.Background(), staleGen, "test-stale")

	s.mu.Lock()
	stillCapturing := s.capturing
	s.mu.Unlock()
	require.True(t, stillCapturing, "stale callback tore down a session it does not own")
	require.Zero(t, src.stopCount(), "stale callback stopped the live session's source")

	// The live session's meter still publishes — its token was not retired.
	src.push(make([]int16, audioLevelWindowSamples))
	lvl := awaitAudioLevelOn(t, ch, s)
	require.Equal(t, audioLevelFloorDbfs, lvl.RmsDbfs)
	require.NoError(t, s.Stop())
}

// awaitAudioLevelOn polls the hub's latest-audio cache (pull delivery) until a
// reading lands — the subscriber channel never carries audio events.
func awaitAudioLevelOn(t *testing.T, _ <-chan hubEvent, s *Service) AudioLevel {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if evt, gen := s.hub.latestAudio(); evt != nil && gen > 0 {
			lvl, ok := evt.payload.(AudioLevel)
			require.True(t, ok)
			return lvl
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no audio level landed in the hub within 5s")
	return AudioLevel{}
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
