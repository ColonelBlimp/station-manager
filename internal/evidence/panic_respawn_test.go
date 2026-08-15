package evidence

// L9 — a panic in the evidence write path must:
//   (1) be recovered and logged, attributed to the named worker (not crash the
//       process or vanish into the runtime stderr dump);
//   (2) RESPAWN the worker so capture continues — with no wedged capture path (the
//       respawned writer and CaptureSlot both take s.mu); and
//   (3) count the lost slot as writer_panic, so the gap is OBSERVABLE (durable in
//       loss_intervals) rather than silently dropped while drain reports it processed.
//
// Confusable states (the finding's own): an unexplained process exit vs a named
// subsystem panic; a live service vs a recovered panic that left its worker dead;
// and (the codex L9 review) a counted evidence gap vs a silent one.
//
// Both panic LOCATIONS are exercised: lock-free (during the write) and while s.mu is
// held — the latter would wedge the respawned writer without processSlot's panic-safe
// unlock, and would deadlock the loss record.

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/safego"
)

func TestEvidenceWorker_WriterPanic_RecordsLossRespawnsNoWedge(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(fn func()) (clear func())
	}{
		{"lock-free write path", func(fn func()) func() {
			writerPanicForTest = fn
			return func() { writerPanicForTest = nil }
		}},
		{"while s.mu held", func(fn func()) func() {
			writerPanicUnderLockForTest = fn
			return func() { writerPanicUnderLockForTest = nil }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restore := safego.SetRespawnCooldownForTest(time.Millisecond)
			defer restore()

			var once atomic.Bool
			clear := tc.set(func() {
				if once.CompareAndSwap(false, true) {
					panic("boom: evidence writer exploded")
				}
			})
			defer clear()

			cfg := testConfig(t, true)
			var buf bytes.Buffer
			s := newRunningLogged(t, cfg, &buf)

			s.CaptureSlot(obsSlot(slotAt(0), 14.074, true)) // panics → recovered + respawn
			s.CaptureSlot(obsSlot(slotAt(1), 14.074, true)) // processed by the respawned writer

			// A later slot lands → the writer respawned and neither it nor CaptureSlot wedged.
			waitFor(t, "respawned writer processed a later slot (no wedge)", func() bool {
				db := openRaw(t, cfg.Path)
				return countRows(t, db, `SELECT COUNT(*) FROM observations`) >= 1
			})
			s.Stop()

			// The lost slot is counted exactly once, durably, under its own reason.
			db := openRaw(t, cfg.Path)
			if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE reason = ?`, lossReasonWriterPanic); n != 1 {
				t.Errorf("loss_intervals %s rows = %d, want exactly 1 (the panicked slot counted once)", lossReasonWriterPanic, n)
			}

			// The panic left a structured, attributed trace.
			var logged bool
			for _, l := range defaultVisibleLines(&buf) {
				if strings.Contains(l, "subsystem goroutine panicked") && strings.Contains(l, "evidence.writer") {
					logged = true
					break
				}
			}
			if !logged {
				t.Fatalf("no structured panic line for evidence.writer; log:\n%s", buf.String())
			}
		})
	}
}
