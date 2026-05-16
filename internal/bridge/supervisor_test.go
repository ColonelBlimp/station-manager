package bridge

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// scaleSupervisorBackoff dials supervisor timing down for tests that
// need multiple retry cycles to land within a sub-second test budget.
// Defer restoration via t.Cleanup so tests don't leak state to each
// other.
func scaleSupervisorBackoff(t *testing.T, initial, max, steadyState time.Duration) {
	t.Helper()
	prevInitial := supervisorInitialBackoff
	prevMax := supervisorMaxBackoff
	prevSteady := supervisorSteadyStateThreshold
	supervisorInitialBackoff = initial
	supervisorMaxBackoff = max
	supervisorSteadyStateThreshold = steadyState
	t.Cleanup(func() {
		supervisorInitialBackoff = prevInitial
		supervisorMaxBackoff = prevMax
		supervisorSteadyStateThreshold = prevSteady
	})
}

// TestSupervisor_RetriesUntilOpenSucceeds covers the Flavour 2
// startup ordering: daemon up before rig. The first N openClient
// calls fail (port file absent because rig is off); the (N+1)th
// succeeds (rig powered on). Supervisor must reach the live pipeline
// without operator intervention — proved by INIT firing on the
// successfully-opened fake.
func TestSupervisor_RetriesUntilOpenSucceeds(t *testing.T) {
	scaleSupervisorBackoff(t, 5*time.Millisecond, 10*time.Millisecond, 10*time.Second)

	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake"},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	const failBefore = 3
	var attempts int32
	liveFakeCh := make(chan *fakeSerial, 1)
	s.openClient = func(_ serial.Config) (serial.Client, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < failBefore {
			return nil, errOpenFailed
		}
		f := newFakeSerial()
		select {
		case liveFakeCh <- f:
		default:
		}
		return f, nil
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	var live *fakeSerial
	select {
	case live = <-liveFakeCh:
	case <-time.After(time.Second):
		t.Fatalf("supervisor did not reach a successful open within 1s (attempts=%d)", atomic.LoadInt32(&attempts))
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		writes := live.recordedWrites()
		if len(writes) >= 1 && bytes.Equal(writes[0], []byte("AI1;")) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("INIT did not fire on recovered pipeline; writes=%v", live.recordedWrites())
}

// TestSupervisor_ReopensAfterTerminalReadError covers the power-spike
// recovery case the user named as load-bearing: a live session with
// the SPA mid-QSO must survive a momentary rig disruption without a
// daemon restart. Pipeline runs, fake is closed (simulates EIO from
// a yanked / power-cycled USB), supervisor must reopen the port and
// re-arm AUTO mode on the second pipeline instance.
func TestSupervisor_ReopensAfterTerminalReadError(t *testing.T) {
	scaleSupervisorBackoff(t, 5*time.Millisecond, 10*time.Millisecond, 10*time.Second)

	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake"},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	fakeCh := make(chan *fakeSerial, 4)
	s.openClient = func(_ serial.Config) (serial.Client, error) {
		f := newFakeSerial()
		select {
		case fakeCh <- f:
		default:
		}
		return f, nil
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	var first *fakeSerial
	select {
	case first = <-fakeCh:
	case <-time.After(time.Second):
		t.Fatal("first open did not happen within 1s")
	}

	// Wait for INIT on first fake so we know the pipeline is in its
	// read loop before we yank the port.
	if !waitForFirstWrite(first, []byte("AI1;"), time.Second) {
		t.Fatalf("first pipeline never sent INIT; writes=%v", first.recordedWrites())
	}

	// Simulate the port going away mid-stream. fake.Close closes the
	// lines channel, so the next ReadResponseBytes returns
	// serial.ErrClosed — the terminal-read-error branch.
	_ = first.Close()

	var second *fakeSerial
	select {
	case second = <-fakeCh:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not reopen after terminal read error within 1s")
	}

	if !waitForFirstWrite(second, []byte("AI1;"), time.Second) {
		t.Fatalf("recovered pipeline never sent INIT; writes=%v", second.recordedWrites())
	}
}

// TestSupervisor_PermanentErrorDoesNotRetry covers the "operator
// typo'd the rig driver" case: cat.Lookup fails. Supervisor must
// publish the bridge-error once and STOP — retrying would just spam
// the operator with the same toast forever.
func TestSupervisor_PermanentErrorDoesNotRetry(t *testing.T) {
	scaleSupervisorBackoff(t, 1*time.Millisecond, 2*time.Millisecond, 10*time.Second)

	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake"},
		Cat:     types.BridgeCatConfig{Driver: "nonexistent-rig"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	var openCalls int32
	s.openClient = func(_ serial.Config) (serial.Client, error) {
		atomic.AddInt32(&openCalls, 1)
		return newFakeSerial(), nil
	}

	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// First event: the bridge-error for the unknown driver.
	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before bridge-error")
		}
		if evt.Name != EventBridgeError {
			t.Errorf("first event = %q, want %q", evt.Name, EventBridgeError)
		}
		p, ok := evt.Payload.(BridgeErrorPayload)
		if !ok {
			t.Fatalf("payload type = %T, want BridgeErrorPayload", evt.Payload)
		}
		if p.Code != BridgeErrCodeUnknownDriver {
			t.Errorf("Code = %q, want %q", p.Code, BridgeErrCodeUnknownDriver)
		}
	case <-time.After(time.Second):
		t.Fatal("no bridge-error within 1s")
	}

	// Wait long enough that multiple retry cycles WOULD have run
	// against the dialed-down backoff; assert no more events arrive.
	select {
	case evt, ok := <-ch:
		if !ok {
			return // hub closed by Stop racing this select — also fine
		}
		t.Errorf("unexpected second event after permanent error: %+v", evt)
	case <-time.After(50 * time.Millisecond):
		// expected — supervisor exited
	}

	if calls := atomic.LoadInt32(&openCalls); calls != 0 {
		t.Errorf("openClient called %d times for permanent (cat.Lookup) failure; expected 0", calls)
	}
}

// TestSupervisor_DedupesIdenticalOpenFailureAcrossRetries covers the
// toast-flood guard: when openClient fails N times with the same
// error, only ONE bridge-error fires (the first). Without dedup the
// SPA would surface a fresh "Serial port open failed" toast every
// few seconds for as long as the rig stayed off.
func TestSupervisor_DedupesIdenticalOpenFailureAcrossRetries(t *testing.T) {
	scaleSupervisorBackoff(t, 1*time.Millisecond, 2*time.Millisecond, 10*time.Second)

	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake"},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	var attempts int32
	s.openClient = func(_ serial.Config) (serial.Client, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, errOpenFailed
	}

	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// Wait until the supervisor has clearly retried more than once.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&attempts) >= 4 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&attempts); got < 4 {
		t.Fatalf("only %d open attempts; backoff scaling failed", got)
	}

	// Collect events that have arrived so far. Exactly one
	// bridge-error must be present — subsequent identical failures
	// dedup against lastPublishedExitKey.
	collected := 0
	drainDeadline := time.After(20 * time.Millisecond)
drain:
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				break drain
			}
			if evt.Name == EventBridgeError {
				collected++
			}
		case <-drainDeadline:
			break drain
		}
	}

	if collected != 1 {
		t.Errorf("expected exactly 1 bridge-error after %d open attempts; got %d", atomic.LoadInt32(&attempts), collected)
	}
}

// waitForFirstWrite polls fake.recordedWrites until the first write
// equals want, or the deadline expires. Returns true on match.
// Helper kept local because the existing pipeline tests all repeat
// the same polling shape inline; extracting it just for the
// supervisor tests keeps the noise contained.
func waitForFirstWrite(f *fakeSerial, want []byte, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		writes := f.recordedWrites()
		if len(writes) >= 1 && bytes.Equal(writes[0], want) {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}
