package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newTestService builds a Service ready for Initialize/Start. Tests
// that need to exercise the disabled path pass cfg.Enabled=false.
//
// Uses an uninitialised &logging.Service{} — its InfoWith / WarnWith
// / ErrorWith methods all return a noop logger when ConfigService is
// nil, so Service code paths that emit log lines run without writing
// anywhere. Same pattern as internal/lookup/refresher tests.
func newTestService(t *testing.T, cfg types.BridgeConfig) *Service {
	t.Helper()
	return New(cfg, &logging.Service{})
}

// TestInitialize_MissingLogger covers the dependency check. A Service
// constructed without a logger fails Initialize loudly rather than
// nil-panicking later when the publisher goroutine tries to log.
func TestInitialize_MissingLogger(t *testing.T) {
	s := &Service{cfg: types.BridgeConfig{Enabled: true}}
	if err := s.Initialize(); err == nil {
		t.Fatal("expected error when logger is nil")
	}
}

// TestInitialize_Idempotent confirms the project's "all lifecycle
// methods idempotent" rule — Initialize twice is the same as once.
func TestInitialize_Idempotent(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{Enabled: false})
	if err := s.Initialize(); err != nil {
		t.Fatalf("first Initialize: %v", err)
	}
	if err := s.Initialize(); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
}

// TestEnabled_Reports_Cfg confirms the kill-switch is honoured. The
// HTTP wiring layer branches on this to decide whether to register
// `/v1/rig/events` — a wrong answer here means either a dangling
// route or a missing one.
func TestEnabled_Reports_Cfg(t *testing.T) {
	on := newTestService(t, types.BridgeConfig{Enabled: true})
	if !on.Enabled() {
		t.Error("Enabled() = false with cfg.Enabled=true")
	}
	off := newTestService(t, types.BridgeConfig{Enabled: false})
	if off.Enabled() {
		t.Error("Enabled() = true with cfg.Enabled=false")
	}
	var nilSvc *Service
	if nilSvc.Enabled() {
		t.Error("Enabled() on nil receiver should be false")
	}
}

// TestStart_Disabled_NoPublisher covers the master-smd / headless
// shape: cfg.Enabled=false → Start succeeds without spawning the
// publisher goroutine. With no publisher, the subscriber channel
// stays silent until the test deferred-cleanup runs Stop. The
// channel-closes-on-Stop path is covered separately by
// TestStop_ClosesHub_AndDrainsSubscribers — this test only asserts
// the silence.
func TestStart_Disabled_NoPublisher(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{Enabled: false})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	select {
	case <-ch:
		t.Error("disabled bridge published event; want silence")
	case <-time.After(100 * time.Millisecond):
		// expected — no events ever arrive when disabled
	}
}

// TestStart_Enabled_PublishesPipelineEvents covers the M3a.2 happy
// path end-to-end via the fakeSerial harness: cfg.Enabled=true →
// Start spawns the pipeline goroutine → fed line decodes through
// cat → mapStatusToPayload → hub → Subscribe receives the typed
// rig-state event.
func TestStart_Enabled_PublishesPipelineEvents(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
	})
	fake := installFakeSerial(s)
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	fake.feedLine([]byte("ID0800"))

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before any event")
		}
		if evt.Name != EventRigState {
			t.Errorf("first event Name = %q, want %q", evt.Name, EventRigState)
		}
		payload, ok := evt.Payload.(RigStatePayload)
		if !ok {
			t.Fatalf("event payload type = %T, want RigStatePayload", evt.Payload)
		}
		if payload.RigIdentity != "FT-710" {
			t.Errorf("RigIdentity = %q, want %q", payload.RigIdentity, "FT-710")
		}
	case <-time.After(time.Second):
		t.Fatal("no pipeline event received within 1s")
	}
}

// TestStart_Idempotent confirms a second Start is a no-op (no second
// pipeline goroutine spawned, no panic, no double-cancel surprises).
func TestStart_Idempotent(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
	})
	installFakeSerial(s)
	_ = s.Initialize()
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	_ = s.Stop()
}

// TestStop_ClosesHub_AndDrainsSubscribers confirms Stop is the
// canonical shutdown path: pipeline goroutine exits, hub closes,
// any open subscriber's channel signals close (ok=false from
// channel-receive). SSE handlers observe this as "stream ended" and
// return.
func TestStop_ClosesHub_AndDrainsSubscribers(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
	})
	installFakeSerial(s)
	_ = s.Initialize()
	_ = s.Start(context.Background())

	ch, _ := s.Subscribe()

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Drain any in-flight events; eventually the channel must close.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, ok := <-ch
		if !ok {
			return // expected: channel closed
		}
	}
	t.Fatal("subscriber channel did not close within 1s of Stop")
}

// TestStop_Idempotent confirms a second Stop is harmless. Important
// for shutdown paths where deferred Stop interleaves with explicit
// Stop on signal.
func TestStop_Idempotent(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{Enabled: false})
	_ = s.Initialize()
	_ = s.Start(context.Background())
	if err := s.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// TestStart_AfterStop_NoOps pins the post-Stop terminal-state rule
// per the 2026-05-10 internal-bridge-pipeline review (#8). A Stop()
// call on a never-Start()ed Service marks the lifecycle as stopped;
// any subsequent Start() returns silently rather than spinning up a
// pipeline that has nowhere to publish (hub is closed, Subscribe
// returns an already-closed channel). No realistic call site does
// this, but the lifecycle pattern says all transitions should be
// sane.
func TestStart_AfterStop_NoOps(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{Enabled: false})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Errorf("Start after Stop: %v", err)
	}
	// Subscribe should return a closed channel; if Start lied and
	// spun up a pipeline, the channel might be open.
	ch, unsub := s.Subscribe()
	defer unsub()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("post-Stop+Start Subscribe yielded an event; want closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("post-Stop+Start Subscribe channel neither closed nor delivered")
	}
}

// TestSubscribe_AfterStop_ReturnsClosedChannel covers the late-
// subscriber edge case: a SPA tab loads against a daemon that's mid-
// shutdown, opens an EventSource, the bridge has already stopped.
// The subscriber gets an already-closed channel; the SSE handler's
// range loop exits immediately rather than hanging.
func TestSubscribe_AfterStop_ReturnsClosedChannel(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{Enabled: false})
	_ = s.Initialize()
	_ = s.Start(context.Background())
	_ = s.Stop()

	ch, unsub := s.Subscribe()
	defer unsub()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("post-Stop Subscribe channel yielded an event; want closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("post-Stop Subscribe channel neither closed nor delivered")
	}
}

// readClientCount reads the next event, asserts it is a rig-clients broadcast,
// and returns its count. Fails on a wrong event type or timeout.
func readClientCount(t *testing.T, ch <-chan Event) int {
	t.Helper()
	select {
	case evt := <-ch:
		if evt.Name != EventRigClients {
			t.Fatalf("event = %q, want rig-clients", evt.Name)
		}
		p, ok := evt.Payload.(RigClientsPayload)
		if !ok {
			t.Fatalf("payload = %T, want RigClientsPayload", evt.Payload)
		}
		return p.Count
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for rig-clients event")
		return 0
	}
}

// TestSubscribe_BroadcastsClientCount is the multi-tab-awareness regression: the
// rig-clients event fires ONLY for multi-tab situations. A lone tab gets nothing
// (so the single-subscriber event stream every other test relies on is untouched);
// a second tab makes both tabs learn count 2; and when it leaves, the remaining tab
// learns count 1 so its banner clears. Advisory only — no write is gated on it
// (that's the future operating-lock).
func TestSubscribe_BroadcastsClientCount(t *testing.T) {
	s := newTestService(t, types.BridgeConfig{Enabled: true})

	// First tab: lone, so NO rig-clients event.
	ch1, unsub1 := s.Subscribe()
	assertNoEvent(t, ch1)

	// Second tab: now multi-tab — it learns count 2, AND the first tab is notified.
	ch2, unsub2 := s.Subscribe()
	if got := readClientCount(t, ch2); got != 2 {
		t.Fatalf("second subscriber count = %d, want 2", got)
	}
	if got := readClientCount(t, ch1); got != 2 {
		t.Fatalf("first tab not notified of second; count = %d, want 2", got)
	}

	// Second tab closes: the remaining tab learns count dropped to 1 (banner clears).
	unsub2()
	if got := readClientCount(t, ch1); got != 1 {
		t.Fatalf("count after unsub = %d, want 1", got)
	}
	unsub1()
}

// assertNoEvent fails if any event arrives within a short settle window.
func assertNoEvent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("expected no event, got %q", evt.Name)
	case <-time.After(50 * time.Millisecond):
	}
}
