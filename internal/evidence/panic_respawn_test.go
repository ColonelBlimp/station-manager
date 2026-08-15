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
	stderrors "errors"
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

func assertPanicLogged(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	for _, l := range defaultVisibleLines(buf) {
		if strings.Contains(l, "subsystem goroutine panicked") && strings.Contains(l, "evidence.writer") {
			return
		}
	}
	t.Fatalf("no structured panic line for evidence.writer; log:\n%s", buf.String())
}

// A panic AFTER the slot durably commits (e.g. in notifyLive) must NOT record a
// writer_panic loss — the evidence exists (codex L9 review, case 1).
func TestEvidenceWorker_PanicAfterCommit_NoFalseLoss(t *testing.T) {
	restore := safego.SetRespawnCooldownForTest(time.Millisecond)
	defer restore()

	var once atomic.Bool
	writerPanicAfterCommitForTest = func() {
		if once.CompareAndSwap(false, true) {
			panic("boom: post-commit")
		}
	}
	defer func() { writerPanicAfterCommitForTest = nil }()

	cfg := testConfig(t, true)
	var buf bytes.Buffer
	s := newRunningLogged(t, cfg, &buf)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true)) // committed, THEN panics
	s.CaptureSlot(obsSlot(slotAt(1), 14.074, true)) // respawned writer commits this

	waitFor(t, "respawned writer committed a later slot", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations`) >= 1
	})
	s.Stop()

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE reason = ?`, lossReasonWriterPanic); n != 0 {
		t.Errorf("writer_panic rows = %d, want 0 (a committed slot is not lost)", n)
	}
	assertPanicLogged(t, &buf)
}

// A panic AFTER the cap branch counted the slot (in the checkpoint) must NOT record a
// SECOND loss under writer_panic — that would double-count it (codex L9 review, case 2).
func TestEvidenceWorker_PanicAfterCapCount_NoDuplicateLoss(t *testing.T) {
	restore := safego.SetRespawnCooldownForTest(time.Millisecond)
	defer restore()
	oldHeadroom := headroomBytes
	headroomBytes = 1024
	defer func() { headroomBytes = oldHeadroom }()

	var once atomic.Bool
	checkpointHook = func() {
		if once.CompareAndSwap(false, true) {
			panic("boom: post-cap-count checkpoint")
		}
	}
	defer func() { checkpointHook = nil }()

	cfg := testConfig(t, true)
	cfg.CapBytes = 4096 // watermark ≈ 3 KiB; a fresh DB exceeds it → cap path from slot 1
	var buf bytes.Buffer
	s := newRunningLogged(t, cfg, &buf)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true)) // cap-dropped, THEN panics in checkpoint
	s.CaptureSlot(obsSlot(slotAt(1), 14.074, true)) // respawned writer cap-drops this

	drain(t, s) // both processed → the writer survived (no wedge)
	s.Stop()

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE reason = ?`, lossReasonCap); n < 1 {
		t.Errorf("cap rows = %d, want >= 1 (the slot was cap-dropped)", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE reason = ?`, lossReasonWriterPanic); n != 0 {
		t.Errorf("writer_panic rows = %d, want 0 (a cap-counted slot must not double-count)", n)
	}
	assertPanicLogged(t, &buf)
}

// A panic in measurementDrop's post-count health callback (after the accumulator is
// updated) must NOT record a SECOND loss under writer_panic (codex L9 review P2).
func TestEvidenceWorker_PanicAfterMeasurementCount_NoDuplicateLoss(t *testing.T) {
	restore := safego.SetRespawnCooldownForTest(time.Millisecond)
	defer restore()

	measureFailHook = func(string) error { return stderrors.New("measure boom") } // force the measurement-drop path
	defer func() { measureFailHook = nil }()

	var once atomic.Bool
	measurementDropPanicForTest = func() {
		if once.CompareAndSwap(false, true) {
			panic("boom: post-measurement health callback")
		}
	}
	defer func() { measurementDropPanicForTest = nil }()

	cfg := testConfig(t, true)
	var buf bytes.Buffer
	s := newRunningLogged(t, cfg, &buf)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true)) // measurement-dropped, THEN panics in the callback
	s.CaptureSlot(obsSlot(slotAt(1), 14.074, true)) // respawned writer measurement-drops this

	drain(t, s) // both processed → the writer survived
	s.Stop()

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE reason = ?`, lossReasonMeasurement); n < 1 {
		t.Errorf("measurement_error rows = %d, want >= 1 (the slot was measurement-dropped)", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE reason = ?`, lossReasonWriterPanic); n != 0 {
		t.Errorf("writer_panic rows = %d, want 0 (a measurement-counted slot must not double-count)", n)
	}
	assertPanicLogged(t, &buf)
}
