package ft8

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeSource is an in-memory captureSource for lifecycle tests: it hands the
// Service a channel the test can drive (or not), and records start/stop so
// the wiring can be asserted without audio hardware.
type fakeSource struct {
	ch        chan []int16
	startErr  error
	mu        sync.Mutex
	started   bool
	stopped   bool
	closeOnce sync.Once
}

func newFakeSource() *fakeSource { return &fakeSource{ch: make(chan []int16, 4)} }

func (f *fakeSource) Start(_ context.Context) (<-chan []int16, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.mu.Lock()
	f.started = true
	f.mu.Unlock()
	return f.ch, nil
}

func (f *fakeSource) Stop() error {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	f.closeOnce.Do(func() { close(f.ch) })
	return nil
}

func (f *fakeSource) wasStarted() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.started }
func (f *fakeSource) wasStopped() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.stopped }

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

// TestStart_Disabled_AcquiresNothing covers the default deployment
// (ft8.enabled=false): Start succeeds, the capture source is never started,
// and Stop is a clean no-op.
func TestStart_Disabled_AcquiresNothing(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: false}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	require.False(t, src.wasStarted(), "disabled subsystem must not start capture")
	require.NoError(t, s.Stop())
}

// TestStart_Enabled_StartsCaptureAndDrainsOnStop covers the live lifecycle:
// Start acquires the source and spawns the goroutines; Stop cancels,
// releases the device, and drains. Stop returning proves the goroutines
// exited (wg.Wait).
func TestStart_Enabled_StartsCaptureAndDrainsOnStop(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, Device: "test"}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	require.True(t, src.wasStarted(), "enabled subsystem must start capture")

	done := make(chan struct{})
	go func() { _ = s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not drain within 5s")
	}
	require.True(t, src.wasStopped(), "Stop must release the capture device")
}

// TestStart_CaptureError_FailSoft covers the "enabled but capture won't
// start" case (no device, busy, or the CGO-free build's unavailable stub):
// Start must NOT return an error that aborts daemon startup; it logs and
// leaves the subsystem idle.
func TestStart_CaptureError_FailSoft(t *testing.T) {
	src := &fakeSource{ch: make(chan []int16), startErr: stderrors.New("no device")}
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()), "capture failure must be fail-soft, not a Start error")
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

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); require.NoError(t, s.Stop()) }()
	}
	wg.Wait()
}

// TestStop_BeforeStart_IsTerminal covers Stop-before-Start: a subsequent
// Start must be a no-op (no capture acquired) rather than spinning up work
// with nowhere to land.
func TestStop_BeforeStart_IsTerminal(t *testing.T) {
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), src)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Stop())
	require.NoError(t, s.Start(context.Background()))
	require.False(t, src.wasStarted(), "Start after Stop must not acquire capture")
}
