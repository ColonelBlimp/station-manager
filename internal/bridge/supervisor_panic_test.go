package bridge

// L9 — a panic in the bridge SUPERVISOR (which owns rig control) must be recovered
// and logged (attributed to the named goroutine), and the supervisor must RESPAWN so
// a supervisor bug doesn't crash the daemon or silently leave the rig uncontrolled.
// Stop stays clean (safego.GoTracked owns wg.Add/Done — no negative WaitGroup across
// the respawn).
//
// Confusable states (the L9 finding's own): an unexplained process exit vs a named
// subsystem panic; a live service vs a recovered panic that left its worker dead.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/safego"
)

func TestBridgeSupervisor_PanicRespawnsAndStopsCleanly(t *testing.T) {
	restore := safego.SetRespawnCooldownForTest(time.Millisecond)
	defer restore()

	var once atomic.Bool
	supervisorPanicForTest = func() {
		if once.CompareAndSwap(false, true) {
			panic("boom: bridge supervisor exploded")
		}
	}
	defer func() { supervisorPanicForTest = nil }()

	s, buf := newIdentityLogTestService(t, "yaesu-ft710")
	fake := installFakeSerial(s)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// The supervisor panics on its FIRST loop iteration — before runPipeline opens the
	// port and sends INIT — so a write appearing at all proves the RESPAWNED supervisor
	// ran the pipeline (no wedge, no dead worker).
	waitForWriteCount(t, fake, 1, 2*time.Second)

	// The panic left a structured, attributed trace.
	waitFor(t, func() bool {
		return countLines(buf, "subsystem goroutine panicked") >= 1 &&
			countLines(buf, "bridge.supervisor") >= 1
	}, "no structured supervisor panic line")
}
