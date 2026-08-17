package evidence

// LC-4 Half B (docs/reviews/internal-lifecycle-concurrency-audit.md) — the sync loop already
// held a Stop-cancellable context (loopCtx, cancelled when s.quit closes), but it reached only
// the HTTP request; the surrounding DB work (select/load/markOffered/applyOutcomes) used the
// non-context sql forms, so an in-flight SQLite statement could hold evidenceSvc.Stop() past
// shutdown. Threading that context through the DB ops (BeginTx/ExecContext/QueryContext) lets
// Stop's cancel interrupt an in-flight statement; a cancelled WRITE rolls back, leaving the row
// retriable. modernc.org/sqlite v1.48.1 interrupts a running statement on ctx cancel
// (interruptOnDone — sqlite.go:78, wired at stmt.go:105/261 and tx.go:71).
//
// AC-2 (operator-observable): when the sync context is cancelled while a sync DB statement runs,
// the statement returns a cancellation error and its transaction rolls back — the send-intent
// (offered_at) and the terminal mark (synced) are NOT persisted, so the row is retried next
// round. Confusable state broken: a half-applied sync that consumed a row without a completed
// exchange, or a statement that runs to completion and holds Stop.
//
// Reversion proofs (revert the ctx usage of the function under test; verified 2026-08-17):
//   markOffered:   BeginTx/ExecContext → Begin/Exec  → the tx commits, offered_at is set.
//   applyOutcomes: BeginTx/ExecContext → Begin/Exec  → the tx commits, synced flips to 1.
//   loadSyncRow:   the observation QueryRowContext → QueryRow → the read completes, returns nil.

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
)

// seedObservation captures one slot and returns a syncRow for its observation.
func seedObservation(t *testing.T, s *Service, path string) syncRow {
	t.Helper()
	s.CaptureSlot(richSlot(slotAt(0)))
	drain(t, s)
	return syncRow{kind: evidencewire.KindObservation, uuid: obsUUID(t, path, slotAt(0))}
}

// AC-2: a cancelled markOffered leaves offered_at NULL — the durable send-intent must not
// persist when the write is interrupted (rollback), so the row stays offerable.
func TestSyncMarkOffered_CancelledContextRollsBack(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	row := seedObservation(t, s, cfg.Path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.markOffered(ctx, []syncRow{row}); err == nil {
		t.Fatal("markOffered on a cancelled context returned nil; want a cancellation error")
	}
	if _, offeredAt, _ := syncMark(t, cfg.Path, "observations", row.uuid); offeredAt != nil {
		t.Errorf("offered_at = %q after a cancelled markOffered; the send-intent must NOT persist (rollback)", *offeredAt)
	}
}

// AC-2: a cancelled applyOutcomes leaves synced = 0 — a terminal mark must not persist when the
// write is interrupted, so the row is re-offered rather than silently consumed.
func TestSyncApplyOutcomes_CancelledContextRollsBack(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	row := seedObservation(t, s, cfg.Path)

	// Mark it offered with a live context first (the normal pre-apply state).
	if err := s.markOffered(context.Background(), []syncRow{row}); err != nil {
		t.Fatalf("setup markOffered: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcomes := []evidencewire.RowOutcome{{UUID: row.uuid, Outcome: evidencewire.OutcomeAccepted}}
	if err := s.applyOutcomes(ctx, []syncRow{row}, outcomes); err == nil {
		t.Fatal("applyOutcomes on a cancelled context returned nil; want a cancellation error")
	}
	if synced, _, _ := syncMark(t, cfg.Path, "observations", row.uuid); synced != 0 {
		t.Errorf("synced = %d after a cancelled applyOutcomes; the terminal mark must NOT persist (rollback)", synced)
	}
}

// AC-2 (read side): a cancelled row load returns a cancellation error rather than reading the
// archive to completion — so Stop is not held by an in-flight sync SELECT. (loadSyncRow is the
// read the selection path spends most of its statements in; selectKind's own QueryContext is
// threaded too — verified by the absence of any non-ctx sql form in the syncOnce chain — but it
// cannot be reversion-isolated because selectKind then calls loadSyncRow.)
func TestSyncLoadRow_CancelledContextReturnsError(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	row := seedObservation(t, s, cfg.Path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.loadSyncRow(ctx, evidencewire.KindObservation, row.uuid); err == nil {
		t.Fatal("loadSyncRow on a cancelled context returned nil; want a cancellation error")
	}
}
