package bridge

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
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
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
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
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
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
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "nonexistent-rig"},
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
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
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

// TestSupervisor_SteadyRunDisconnectPublishesDespiteEarlierKey — codex
// 2026-08-09 P2. CRITERION: when the rig disconnects after a steady session,
// the SPA receives the rig-disconnected toast even if a brief earlier outage
// failed with the SAME code — distinguishable from the retry-flood dedup
// (previous test) because there a short run repeats the key within the
// threshold and stays suppressed.
//
// The defect shape: runSupervisor clears lastPublishedExitKey only after
// runPipeline RETURNS and is judged steady, but the terminating error has
// already been through the deduplicating publisher inside the run — so a key
// left by an earlier short run (here: fake #1 dying right after INIT) eats
// the steady session's real disconnect, and connected SPA clients keep
// rendering live rig state through the outage. The steady-state judgement
// must therefore also happen AT PUBLISH TIME.
//
// Timing: the steady threshold is scaled to 300ms and the test waits 400ms+
// measured from fake #2's open before killing it. Against the FIXED code the
// test cannot flake under load in either direction — both runs publish
// whether or not run #1 accidentally counts as steady. The margins matter
// only for the certification direction (run #1, a ~10ms sequence, must stay
// inside the threshold for the OLD code to fail), so the threshold carries
// ~30× headroom over run #1's healthy duration.
func TestSupervisor_SteadyRunDisconnectPublishesDespiteEarlierKey(t *testing.T) {
	scaleSupervisorBackoff(t, 1*time.Millisecond, 2*time.Millisecond, 300*time.Millisecond)

	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
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

	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// Run 1: die on the read path right after INIT lands, well inside the
	// steady threshold — this leaves the serial_port_error key set.
	var first *fakeSerial
	select {
	case first = <-fakeCh:
	case <-time.After(time.Second):
		t.Fatal("first open did not happen within 1s")
	}
	if !waitForFirstWrite(first, []byte("AI1;"), time.Second) {
		t.Fatalf("INIT did not reach fake #1; writes=%q", first.recordedWrites())
	}
	_ = first.Close()

	// Run 2: live past the steady threshold, then die the same way.
	var second *fakeSerial
	select {
	case second = <-fakeCh:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not reopen after the first failure")
	}
	secondOpenedAt := time.Now()
	if !waitForFirstWrite(second, []byte("AI1;"), time.Second) {
		t.Fatalf("INIT did not reach fake #2; writes=%q", second.recordedWrites())
	}
	for time.Since(secondOpenedAt) < 400*time.Millisecond {
		time.Sleep(10 * time.Millisecond)
	}
	_ = second.Close()

	// Both outages must reach the subscriber. The old code delivers only the
	// first: run 2's disconnect carries the same key and the supervisor's
	// clear runs too late.
	disconnects := 0
	deadline := time.After(2 * time.Second)
	for disconnects < 2 {
		select {
		case evt, ok := <-ch:
			if !ok {
				t.Fatalf("hub closed with %d rig-disconnected events; want 2", disconnects)
			}
			if evt.Name == EventRigDisconnected {
				disconnects++
			}
		case <-deadline:
			t.Fatalf("saw %d rig-disconnected events; want 2 — the steady run's real disconnect was suppressed by the earlier run's key", disconnects)
		}
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

// TestServiceNew_SnapshotsConfiguredTimeouts covers the wiring from
// cfg.Timeouts.* (operator-supplied milliseconds) through resolveTimeout
// to the Service's per-instance Duration fields. Non-zero values
// override the package-var defaults; zero values fall through to the
// package var. Snapshot is taken at New, so a runtime config edit
// doesn't reach a running Service — matches the BridgeConfig pattern.
func TestServiceNew_SnapshotsConfiguredTimeouts(t *testing.T) {
	t.Run("non-zero values override the package-var defaults", func(t *testing.T) {
		s := New(types.BridgeConfig{
			Timeouts: types.BridgeTimeoutsConfig{
				LivenessMs:             7000,
				BackoffInitialMs:       500,
				BackoffMaxMs:           60000,
				SteadyStateThresholdMs: 15000,
			},
		}, &logging.Service{})

		if got, want := s.livenessTimeout, 7*time.Second; got != want {
			t.Errorf("livenessTimeout = %v, want %v", got, want)
		}
		if got, want := s.supervisorInitialBackoff, 500*time.Millisecond; got != want {
			t.Errorf("supervisorInitialBackoff = %v, want %v", got, want)
		}
		if got, want := s.supervisorMaxBackoff, 60*time.Second; got != want {
			t.Errorf("supervisorMaxBackoff = %v, want %v", got, want)
		}
		if got, want := s.supervisorSteadyStateThreshold, 15*time.Second; got != want {
			t.Errorf("supervisorSteadyStateThreshold = %v, want %v", got, want)
		}
	})

	t.Run("zero values fall through to the package-var defaults", func(t *testing.T) {
		s := New(types.BridgeConfig{}, &logging.Service{})

		if got, want := s.livenessTimeout, livenessTimeout; got != want {
			t.Errorf("livenessTimeout = %v, want %v (package default)", got, want)
		}
		if got, want := s.supervisorInitialBackoff, supervisorInitialBackoff; got != want {
			t.Errorf("supervisorInitialBackoff = %v, want %v (package default)", got, want)
		}
		if got, want := s.supervisorMaxBackoff, supervisorMaxBackoff; got != want {
			t.Errorf("supervisorMaxBackoff = %v, want %v (package default)", got, want)
		}
		if got, want := s.supervisorSteadyStateThreshold, supervisorSteadyStateThreshold; got != want {
			t.Errorf("supervisorSteadyStateThreshold = %v, want %v (package default)", got, want)
		}
	})
}

// TestReadLoop_ProbeReissuesINITAndREADOnNoData covers the
// operator-reported scenario from session 66: daemon up before rig,
// device node persists across rig power-state (some USB-serial
// adapters do this), openClient succeeds but INIT lands on dead
// air. When the rig is powered on later, the readLoop's no-data
// probe must re-send INIT (to arm AUTO mode on the now-alive rig)
// followed by READ (to force an immediate snapshot). Without the
// probe the rig stays silent forever — AUTO never arms because the
// rig never received the original INIT.
//
// Drives readLoop directly with a tight livenessTimeout so the test
// completes in well under a second. Asserts that within a few
// livenessTimeout windows, INIT and READ both appear at least twice
// (the runPipeline-time pair is bypassed by the direct readLoop
// call, so two appearances proves probe-driven re-issue).
func TestReadLoop_ProbeReissuesINITAndREADOnNoData(t *testing.T) {
	prev := livenessTimeout
	livenessTimeout = 20 * time.Millisecond
	t.Cleanup(func() { livenessTimeout = prev })

	s, fake := newPipelineTestService(t)
	// Don't call Start — drive readLoop directly so we can assert
	// purely on probe-driven writes, without the runPipeline startup
	// INIT+READ pair muddying the counts.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	initBytes := []byte("AI1;")
	readBytes := []byte("ID;FA;FB;ST;VS;MD0;MD1;PC;")

	def, ok := cat.Lookup("yaesu-ft710")
	if !ok {
		t.Fatal("rigdef yaesu-ft710 not found")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.readLoop(ctx, fake, def, initBytes, readBytes)
	}()

	// Wait long enough for at least three timeout windows: one
	// initial no-data + two probe cycles. Each cycle writes INIT and
	// READ once.
	deadline := time.Now().Add(livenessTimeout*6 + 50*time.Millisecond)
	var initCount, readCount int
	for time.Now().Before(deadline) {
		writes := fake.recordedWrites()
		initCount, readCount = 0, 0
		for _, w := range writes {
			if bytes.Equal(w, initBytes) {
				initCount++
			} else if bytes.Equal(w, readBytes) {
				readCount++
			}
		}
		if initCount >= 2 && readCount >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if initCount < 2 {
		t.Errorf("expected probe to re-issue INIT at least twice; got %d INIT writes (writes=%v)", initCount, fake.recordedWrites())
	}
	if readCount < 2 {
		t.Errorf("expected probe to re-issue READ at least twice; got %d READ writes (writes=%v)", readCount, fake.recordedWrites())
	}
}
