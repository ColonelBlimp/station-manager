package ft8

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestInitialize_RequiresLogger covers the only validation
// Initialize currently does — the same pattern as the bridge's
// Initialize. A nil logger surfaces as a clear op-tagged error
// rather than blowing up later when the first log call dereferences
// it.
func TestInitialize_RequiresLogger(t *testing.T) {
	s := New(types.Ft8Config{Enabled: false}, nil)
	if err := s.Initialize(); err == nil {
		t.Fatal("Initialize: expected error for nil logger; got nil")
	}
}

// TestStart_Disabled_SpawnsNoGoroutine covers the v1 default deployment
// (ft8.enabled=false): Start succeeds, no goroutine spawned, Stop is
// a clean no-op. This is the shape every operator's daemon will hit
// until they explicitly opt into FT8.
func TestStart_Disabled_SpawnsNoGoroutine(t *testing.T) {
	s := New(types.Ft8Config{Enabled: false}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestStart_Enabled_PlaceholderGoroutineExitsOnStop covers the
// SCAFFOLD lifecycle: when enabled, Start spawns the placeholder
// goroutine; Stop cancels the parent ctx and waits for it to exit.
// Once real decoder workers replace runPlaceholder this test
// becomes the regression check that the supervisor still drains on
// Stop.
func TestStart_Enabled_PlaceholderGoroutineExitsOnStop(t *testing.T) {
	s := New(types.Ft8Config{Enabled: true}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Stop must drain — if it returns, the placeholder goroutine
	// has exited. Hangs here would indicate the placeholder isn't
	// listening on ctx.Done() (or wg.Done isn't being called).
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestLifecycle_Idempotent covers the "Stop returned, therefore,
// stopped" contract under repeat calls. Mirrors the bridge's
// equivalent test — important enough that both subsystems pin it
// explicitly rather than relying on the sync.Once / sync.Mutex
// machinery being correct by inspection.
func TestLifecycle_Idempotent(t *testing.T) {
	s := New(types.Ft8Config{Enabled: true}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize (second call): %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start (second call): %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop (second call): %v", err)
	}
}

// TestStop_BeforeStart_IsTerminal covers the documented "Stop-
// before-Start puts the Service in a terminal state" contract: a
// subsequent Start returns silently, no goroutine spawned, no
// reanimation. Same shape as the bridge.
func TestStop_BeforeStart_IsTerminal(t *testing.T) {
	s := New(types.Ft8Config{Enabled: true}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start after Stop: %v", err)
	}
	if !s.stopped {
		t.Error("Service.stopped should remain true after post-Stop Start")
	}
	if s.cancel != nil {
		t.Error("Service.cancel should remain nil after post-Stop Start (no goroutine spawned)")
	}
}

// TestEnabled_NilSafe covers the documented nil-safety on Enabled().
// Callers (eventually api.Server) may legitimately check
// s.Enabled() against a nil pointer when wiring routes.
func TestEnabled_NilSafe(t *testing.T) {
	var s *Service
	if s.Enabled() {
		t.Error("nil Service should report Enabled() = false")
	}
}
