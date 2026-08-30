package worker

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// F5 — internally-caused transients (a DB fetch fault, not the forwarder) reach
// markTransientInternal, which NEVER runs persistOutcome/logAttempt, so they left no
// trace at all. The line must reflect what ACTUALLY persisted (codex 63f29f0b P2): only
// a committed (dispPersisted) transition earns it — a claimed (in_progress) row commits;
// an unclaimed (pending) row re-arms and must NOT be logged as retried/failed.

// claimOne claims the single pending upload for the stub forwarder, returning the
// in_progress row (so a subsequent mark* actually commits).
func claimOne(t *testing.T, h *testHarness) types.QsoUpload {
	t.Helper()
	rows, err := h.db.ClaimPendingUploadsWithContext(context.Background(), "stub", 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("claimed %d rows, want 1", len(rows))
	}
	return rows[0]
}

func TestMarkTransientInternal_CommittedRetryLogsInfo(t *testing.T) {
	h, buf := captureHarness(t)
	q := h.seedLogbookAndQso()
	h.enqueueUpload(q, "stub", stub.Type, action.Insert)
	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	w.markTransientInternal(context.Background(), claimOne(t, h), stderrors.New("db fetch boom"))
	retry := withMessage(t, buf, "forwarding: internal transient — will retry")
	if len(retry) != 1 {
		t.Fatalf("committed-retry records = %d, want 1\n%s", len(retry), buf.String())
	}
	if retry[0]["level"] != "info" {
		t.Errorf("retry level = %v, want info", retry[0]["level"])
	}
	if e, _ := retry[0]["error"].(string); !strings.Contains(e, "boom") {
		t.Errorf("retry line must carry the internal cause; error = %v", retry[0]["error"])
	}
}

func TestMarkTransientInternal_CommittedExhaustionLogsWarn(t *testing.T) {
	h, buf := captureHarness(t)
	q := h.seedLogbookAndQso()
	h.enqueueUpload(q, "stub", stub.Type, action.Insert)
	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := claimOne(t, h)
	row.Attempts = int64(defaultCfg("stub").Retry.MaxAttempts - 1) // next attempt hits the cap
	w.markTransientInternal(context.Background(), row, stderrors.New("db still down"))
	exh := withMessage(t, buf, "forwarding: internal transient exhausted — row failed")
	if len(exh) != 1 {
		t.Fatalf("committed-exhaustion records = %d, want 1\n%s", len(exh), buf.String())
	}
	if exh[0]["level"] != "warn" {
		t.Errorf("exhausted level = %v, want warn", exh[0]["level"])
	}
}

// The P2 pin: an unclaimed (pending) row makes markTransientRetry RE-ARM — the
// transition never commits and the row will upload again — so no "will retry" line
// may be written. Before the fix the line was written before the persist call.
func TestMarkTransientInternal_ReArmedTransitionIsNotLogged(t *testing.T) {
	h, buf := captureHarness(t)
	q := h.seedLogbookAndQso()
	h.enqueueUpload(q, "stub", stub.Type, action.Insert)
	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	// h.fetchUpload returns the PENDING row (never claimed), so markTransientRetry
	// affects 0 rows and re-arms.
	w.markTransientInternal(context.Background(), h.fetchUpload(q), stderrors.New("db boom"))
	if got := withMessage(t, buf, "forwarding: internal transient — will retry"); len(got) != 0 {
		t.Fatalf("a re-armed transition must NOT log a committed retry; got %d lines\n%s", len(got), buf.String())
	}
}

// F7 — a soft-deleted / missing QSO makes fetchQsoForAction terminally fail the upload,
// but the transition was SILENT (an SSE event + last_error, neither durable). "Why did
// this QSO never reach the forwarder?" now has a file answer.
func TestFetchGone_SoftDeletedInsertLogsTerminal(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	h.softDeleteQso(qsoID) // the insert's QSO is gone before it forwards

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool { return u.Status == "failed" })

	recs := withMessage(t, buf, "forwarding: QSO gone before forwarding — upload terminally failed")
	if len(recs) != 1 {
		t.Fatalf("QSO-gone records = %d, want 1\n%s", len(recs), buf.String())
	}
	if recs[0]["level"] != "warn" {
		t.Errorf("level = %v, want warn", recs[0]["level"])
	}
	if r, _ := recs[0]["reason"].(string); !strings.Contains(r, "soft-deleted") {
		t.Errorf("reason = %v, want the soft-deleted reason", recs[0]["reason"])
	}
}

// F16 — an unrecognised forwarder Outcome logs its own warning WITHOUT the cause, while
// passing that same cause to markFailed one line down. The diagnostic detail is now on
// its own warning.
func TestPersistOutcome_UnrecognisedOutcomeLogsError(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	row := h.fetchUpload(qsoID)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	w.persistOutcome(context.Background(), row, "K1ABC",
		forwarding.Result{Outcome: "bogus_outcome", Err: stderrors.New("plugin returned garbage")}, 0, forwardTestUUID)

	recs := withMessage(t, buf, "forwarder: returned unrecognised Outcome")
	if len(recs) != 1 {
		t.Fatalf("unrecognised-outcome records = %d, want 1\n%s", len(recs), buf.String())
	}
	if e, _ := recs[0]["error"].(string); !strings.Contains(e, "garbage") {
		t.Errorf("the warning must carry the cause; error = %v", recs[0]["error"])
	}
}
