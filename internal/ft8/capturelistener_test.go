package ft8

/*
   ADR 0064 wiring seam — the bridge's FT8 meter poll "lives and dies with the
   FT8 capture session" (invariant 5), and this listener is how the session
   lifecycle reaches it: cmd/smd wires SetCaptureListener to
   bridge.SetFt8CaptureLive, the same injected-seam direction as SetCatGate
   (neither subsystem imports the other; the boundary tests defend it).

   CONFUSABLE STATES the rules pin: a reconnect INSIDE the linger window is
   session CONTINUITY — the device never released, so the listener must not
   flap false/true (a flap would stop and restart the meter poll mid-slot for
   a page reload); a release AFTER the linger and a daemon Stop are real ends
   and must report false, or the bridge polls the rig for a session that no
   longer exists.
*/

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// captureTransitions records listener calls; the tests assert exact sequences.
type captureTransitions struct {
	mu sync.Mutex
	ts []bool
}

func (c *captureTransitions) add(live bool) {
	c.mu.Lock()
	c.ts = append(c.ts, live)
	c.mu.Unlock()
}

func (c *captureTransitions) get() []bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bool(nil), c.ts...)
}

// CL1 — ACQUIRE REPORTS true, RELEASE REPORTS false, REACQUIRE REPORTS true:
// the full lifecycle, in order, exactly once per transition.
func TestCaptureListener_ReportsLifecycleTransitions(t *testing.T) {
	withShortLinger(t, 10*time.Millisecond)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	tr := &captureTransitions{}
	s.SetCaptureListener(tr.add)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	require.Eventually(t, func() bool { return len(tr.get()) == 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, []bool{true}, tr.get(), "acquire must report live")

	unsub()
	require.Eventually(t, func() bool { return len(tr.get()) == 2 }, time.Second, 5*time.Millisecond,
		"release after the linger must report not-live")
	require.Equal(t, []bool{true, false}, tr.get())

	_, unsub2 := s.Subscribe()
	defer unsub2()
	require.Eventually(t, func() bool { return len(tr.get()) == 3 }, time.Second, 5*time.Millisecond)
	require.Equal(t, []bool{true, false, true}, tr.get(), "reacquire must report live again")
}

// CL2 — A RECONNECT INSIDE THE LINGER IS CONTINUITY, NOT A CYCLE: the device
// never released, so the listener must not flap — a page reload would
// otherwise stop and restart the bridge's meter poll for a session that
// never ended.
func TestCaptureListener_LingerReconnectDoesNotFlap(t *testing.T) {
	withShortLinger(t, 200*time.Millisecond)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	tr := &captureTransitions{}
	s.SetCaptureListener(tr.add)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	require.Eventually(t, func() bool { return len(tr.get()) == 1 }, time.Second, 5*time.Millisecond)
	unsub()
	_, unsub2 := s.Subscribe() // back inside the linger
	defer unsub2()

	time.Sleep(300 * time.Millisecond) // past where the linger would have fired
	require.Equal(t, []bool{true}, tr.get(),
		"a linger-window reconnect must report nothing — the session never ended")
}

// CL3 — STOP IS AN END: a daemon stopping mid-session reports false, so the
// bridge is never left polling for a capture session that no longer exists.
func TestCaptureListener_StopReportsNotLive(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	tr := &captureTransitions{}
	s.SetCaptureListener(tr.add)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))

	_, unsub := s.Subscribe()
	defer unsub()
	require.Eventually(t, func() bool { return len(tr.get()) == 1 }, time.Second, 5*time.Millisecond)

	require.NoError(t, s.Stop())
	got := tr.get()
	require.NotEmpty(t, got)
	require.False(t, got[len(got)-1], "Stop must leave the listener reporting not-live")
}
