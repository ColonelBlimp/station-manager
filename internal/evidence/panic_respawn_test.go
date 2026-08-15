package evidence

// L9 — a panic in a long-lived evidence worker must be recovered and logged (not
// crash the process or vanish into the runtime stderr dump), the worker must RESPAWN
// (respawn=true), and Stop must still shut down cleanly — wg.Done means "permanently
// exited", so there is no double-close and no hang across the respawn.
//
// Confusable states (the finding's own): an unexplained process exit vs a named
// subsystem panic; a live service vs a recovered panic that left its worker dead.

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/safego"
)

func TestEvidenceWorker_PanicRespawnsAndStopsCleanly(t *testing.T) {
	restore := safego.SetRespawnCooldownForTest(time.Millisecond)
	defer restore()

	var firedOnce atomic.Bool
	writerPanicForTest = func() {
		if firedOnce.CompareAndSwap(false, true) {
			panic("boom: evidence writer exploded")
		}
	}
	defer func() { writerPanicForTest = nil }()

	cfg := testConfig(t, true)
	var buf bytes.Buffer
	s := newRunningLogged(t, cfg, &buf)

	// slot1 panics in processSlot → safego recovers + logs + respawns the writer.
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	// slot2 is processed by the RESPAWNED writer — the observable proof it came back.
	s.CaptureSlot(obsSlot(slotAt(1), 14.074, true))

	waitFor(t, "respawned writer processed a later slot", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations`) >= 1
	})

	// Stop joins the workers via wg.Wait — no hang, no double-close panic. (The harness
	// also Stops via t.Cleanup; Stop is idempotent.)
	s.Stop()

	// The panic was recorded structurally, attributed to the named worker.
	var found bool
	for _, l := range defaultVisibleLines(&buf) {
		if strings.Contains(l, "subsystem goroutine panicked") && strings.Contains(l, "evidence.writer") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no structured panic line for evidence.writer — a recovered worker panic must leave a named trace; log:\n%s", buf.String())
	}
}
