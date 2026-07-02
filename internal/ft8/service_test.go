package ft8

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeSource is an in-memory captureSource for lifecycle tests. Capture is now
// demand-driven, so the source must survive repeated Start→Stop cycles: each
// Start allocates a fresh sample channel that the matching Stop closes, and
// start/stop counts let tests assert acquire/release transitions without audio
// hardware.
type fakeSource struct {
	startErr error
	mu       sync.Mutex
	ch       chan []int16
	startN   int
	stopN    int
}

func newFakeSource() *fakeSource { return &fakeSource{} }

func (f *fakeSource) Start(_ context.Context) (<-chan []int16, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startN++
	f.ch = make(chan []int16, 4)
	return f.ch, nil
}

func (f *fakeSource) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopN++
	if f.ch != nil {
		close(f.ch)
		f.ch = nil
	}
	return nil
}

func (f *fakeSource) startCount() int  { f.mu.Lock(); defer f.mu.Unlock(); return f.startN }
func (f *fakeSource) stopCount() int   { f.mu.Lock(); defer f.mu.Unlock(); return f.stopN }
func (f *fakeSource) wasStarted() bool { return f.startCount() > 0 }
func (f *fakeSource) wasStopped() bool { return f.stopCount() > 0 }

func TestInitialize_RequiresLogger(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: false}, nil, nil)
	require.Error(t, s.Initialize(), "nil logger must fail Initialize")
}

func TestInitialize_EnabledRequiresSource(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), nil)
	require.Error(t, s.Initialize(), "enabled subsystem with no capture source must fail Initialize")

	ok := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	require.NoError(t, ok.Initialize())

	// Disabled needs no source.
	dis := newService(types.Ft8Config{Enabled: false}, logging.Noop(), nil)
	require.NoError(t, dis.Initialize())
}

// withShortLinger sets captureLinger to a tiny value for the duration of a
// test so release transitions fire quickly, restoring it on cleanup.
func withShortLinger(t *testing.T, d time.Duration) {
	t.Helper()
	prev := captureLinger
	captureLinger = d
	t.Cleanup(func() { captureLinger = prev })
}

// TestStart_Disabled_AcquiresNothing covers the default deployment
// (ft8.enabled=false): Start succeeds, a subscriber never triggers capture,
// and Stop is a clean no-op.
func TestStart_Disabled_AcquiresNothing(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: false}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))

	_, unsub := s.Subscribe()
	require.False(t, src.wasStarted(), "disabled subsystem must not start capture even with a subscriber")
	unsub()
	require.NoError(t, s.Stop())
}

// TestCapture_NoneUntilSubscriber covers the demand-driven core: an enabled,
// Started subsystem holds no device until the first /v1/ft8/events subscriber
// connects; that subscriber acquires it, and Stop drains. Stop returning
// proves the goroutines exited (wg.Wait).
func TestCapture_NoneUntilSubscriber(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	require.False(t, src.wasStarted(), "no subscriber yet — capture must not start at Start")

	_, unsub := s.Subscribe()
	require.True(t, src.wasStarted(), "first subscriber must acquire the capture device")
	defer unsub()

	done := make(chan struct{})
	go func() { _ = s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not drain within 5s")
	}
	require.True(t, src.wasStopped(), "Stop must release the capture device")
}

// TestCapture_ReleasedAfterLastSubscriber covers release-on-empty and
// re-acquire: the device is released a linger after the last subscriber leaves,
// and a later subscriber acquires a fresh session.
func TestCapture_ReleasedAfterLastSubscriber(t *testing.T) {
	withShortLinger(t, 10*time.Millisecond)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub1 := s.Subscribe()
	require.Equal(t, 1, src.startCount(), "first subscriber acquires capture")
	unsub1()
	require.Eventually(t, func() bool { return src.stopCount() == 1 }, time.Second, 5*time.Millisecond,
		"capture must be released after the last subscriber leaves")

	_, unsub2 := s.Subscribe()
	defer unsub2()
	require.Eventually(t, func() bool { return src.startCount() == 2 }, time.Second, 5*time.Millisecond,
		"a later subscriber must re-acquire a fresh capture session")
}

// TestCapture_LingerSurvivesReconnect covers the linger's purpose: a subscriber
// that disconnects and quickly reconnects within the linger window reuses the
// live session — the device is neither stopped nor reacquired.
func TestCapture_LingerSurvivesReconnect(t *testing.T) {
	withShortLinger(t, 200*time.Millisecond)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub1 := s.Subscribe()
	require.Equal(t, 1, src.startCount())
	unsub1()                   // last subscriber leaves: schedules release in 200ms
	_, unsub2 := s.Subscribe() // reconnect well inside the window: keeps the session
	defer unsub2()

	time.Sleep(300 * time.Millisecond) // past the original linger deadline
	require.Equal(t, 1, src.startCount(), "reconnect must not reacquire — same live session")
	require.Equal(t, 0, src.stopCount(), "reconnect must cancel the pending release")
}

// TestCapture_StartLockedNoOpsWhenCapturing guards F1 (review 2026-07-02): a
// reconnect during a release's disarm window can reach startCaptureLocked while a
// session is still live (subCount 0→1, lingerTimer already nil). The guard must
// keep the live session — starting a second would orphan the first device + pump
// and later deadlock the release drain.
func TestCapture_StartLockedNoOpsWhenCapturing(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	defer unsub()
	require.Equal(t, 1, src.startCount(), "first subscriber acquires capture")

	// Model the in-window reconnect reaching startCaptureLocked with a live session.
	s.mu.Lock()
	s.startCaptureLocked()
	s.mu.Unlock()
	require.Equal(t, 1, src.startCount(), "must not start a second capture session while one is live")
}

// TestCapture_ReleaseReacquiresIfSubscriberPresent guards F2 (review 2026-07-02):
// the drain now runs with s.mu dropped, so a subscriber that reconnects during it
// is left present with no capture — release must re-acquire at the end. Calling
// release with a subscriber still counted models that end state.
func TestCapture_ReleaseReacquiresIfSubscriberPresent(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	defer unsub()
	require.Equal(t, 1, src.startCount())

	s.mu.Lock()
	s.releaseCaptureLocked() // subCount==1 → drain (s.mu dropped) then re-acquire
	s.mu.Unlock()

	require.Equal(t, 1, src.stopCount(), "the live session is drained")
	require.Equal(t, 2, src.startCount(), "release re-acquires while a subscriber is present")
}

// TestCapture_DisarmsTxWhenLastSubscriberLeaves covers the attended-only rule:
// TX must not stay armed after the operator's browser closes. Arming, then losing
// the last /v1/ft8/events subscriber past the linger window, disarms TX — so a
// reopened browser sees disarmed, not a stale armed state carried across sessions.
func TestCapture_DisarmsTxWhenLastSubscriberLeaves(t *testing.T) {
	withShortLinger(t, 10*time.Millisecond)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}}, logging.Noop(), src)
	s.newPlayer = func(string, int) (txPlayer, error) { return newFakeTxPlayer(), nil }
	s.SetTxKeyer(&fakeKeyer{})
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe() // FT8 view open → capture acquired
	require.Eventually(t, func() bool { return src.startCount() == 1 }, time.Second, 5*time.Millisecond)

	require.NoError(t, s.ArmTx(true))
	armed := func() bool { s.txMu.Lock(); defer s.txMu.Unlock(); return s.txArmed }
	require.True(t, armed(), "TX should be armed after ArmTx")

	unsub() // browser closes: last subscriber gone

	require.Eventually(t, func() bool { return !armed() }, time.Second, 5*time.Millisecond,
		"TX must be disarmed once the last subscriber leaves past the linger")
}

// TestCapture_Error_FailSoft covers the "enabled but capture won't start" case
// (no device, busy, or the CGO-free build's unavailable stub): the subscriber
// connection that triggers acquisition must not panic or error out; the
// subsystem stays idle, and Stop is clean.
func TestCapture_Error_FailSoft(t *testing.T) {
	src := &fakeSource{startErr: stderrors.New("no device")}
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))

	_, unsub := s.Subscribe()
	require.False(t, s.capturing, "failed capture acquire must leave the subsystem idle")
	unsub()
	require.NoError(t, s.Stop())
}

// TestLifecycle_Idempotent covers repeat Start and repeat/concurrent Stop —
// the "Stop returned, therefore stopped" contract holds for every caller.
func TestLifecycle_Idempotent(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.Start(context.Background()), "second Start must be a no-op")

	_, unsub := s.Subscribe() // bring a real capture session up so Stop has work to drain
	defer unsub()
	require.True(t, src.wasStarted())

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); require.NoError(t, s.Stop()) }()
	}
	wg.Wait()
}

// TestStop_BeforeStart_IsTerminal covers Stop-before-Start: a subsequent Start
// must be a no-op, and no subscriber can then acquire capture.
func TestStop_BeforeStart_IsTerminal(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Stop())
	require.NoError(t, s.Start(context.Background()))

	_, unsub := s.Subscribe()
	defer unsub()
	require.False(t, src.wasStarted(), "after Stop, no subscriber may acquire capture")
}

// withReconcileInterval bumps catReconcileInterval up so the background
// CAT-reconcile loop never fires on its own during a test — the test drives
// reconcileCat() directly for determinism — restoring it on cleanup.
func withReconcileInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := catReconcileInterval
	catReconcileInterval = d
	t.Cleanup(func() { catReconcileInterval = prev })
}

// TestCapture_CatGate_DefersWhenRigOff is the bug fix: with a CAT gate installed
// and the rig off, a subscriber (FT8 view open) must NOT grab the microphone.
func TestCapture_CatGate_DefersWhenRigOff(t *testing.T) {
	withReconcileInterval(t, time.Hour) // only the reconcileCat() we call runs
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	var live atomic.Bool // rig off
	s.SetCatGate(live.Load)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe() // FT8 view open, but rig off
	defer unsub()
	require.False(t, src.wasStarted(), "rig off → capture must be deferred (no mic grab)")
}

// TestCapture_CatGate_AcquiresWhenCatComesUp: the deferred capture is acquired
// once CAT goes live with a subscriber still present (operator powered the rig).
func TestCapture_CatGate_AcquiresWhenCatComesUp(t *testing.T) {
	withReconcileInterval(t, time.Hour)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	var live atomic.Bool
	s.SetCatGate(live.Load)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	defer unsub()
	require.False(t, src.wasStarted(), "deferred while rig off")

	live.Store(true) // operator powers the rig
	s.reconcileCat() // the reconcile loop's tick
	require.Equal(t, 1, src.startCount(),
		"capture must acquire once CAT is live with a subscriber present")
}

// TestCapture_CatGate_ReleasesWhenCatDrops: a live session is released when CAT
// drops mid-session (rig powered off), freeing the mic.
func TestCapture_CatGate_ReleasesWhenCatDrops(t *testing.T) {
	withReconcileInterval(t, time.Hour)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	var live atomic.Bool
	live.Store(true) // rig on
	s.SetCatGate(live.Load)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	defer unsub()
	require.Equal(t, 1, src.startCount(), "rig on + subscriber → capture acquired")

	live.Store(false) // rig powered off
	s.reconcileCat()  // releaseCaptureLocked drains synchronously
	require.Equal(t, 1, src.stopCount(), "capture released when CAT drops mid-session")
}

// TestCapture_NoCatGate_DemandDriven: with no gate installed (no CAT configured),
// capture stays purely demand-driven — a subscriber acquires immediately.
func TestCapture_NoCatGate_DemandDriven(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	defer unsub()
	require.True(t, src.wasStarted(), "no CAT gate → subscriber acquires capture immediately")
}
