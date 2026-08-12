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
// trace at all — and last_error self-erases on any later success. Mirror the
// forwarder-caused path's severity: Info for a scheduled retry, Warn for exhaustion.
func TestMarkTransientInternal_LogsRetryAndExhaustion(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	row := h.fetchUpload(qsoID) // Attempts = 0

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	// Attempts 0 → below the cap → schedules a retry → Info, carrying the cause.
	w.markTransientInternal(context.Background(), row, stderrors.New("db fetch boom"))
	retry := withMessage(t, buf, "forwarding: internal transient — will retry")
	if len(retry) != 1 {
		t.Fatalf("internal-retry records = %d, want 1\n%s", len(retry), buf.String())
	}
	if retry[0]["level"] != "info" {
		t.Errorf("retry level = %v, want info", retry[0]["level"])
	}
	if e, _ := retry[0]["error"].(string); !strings.Contains(e, "boom") {
		t.Errorf("retry line must carry the internal cause; error = %v", retry[0]["error"])
	}

	// Attempts = MaxAttempts-1 → next attempt hits the cap → terminal → Warn.
	row.Attempts = int64(defaultCfg("stub").Retry.MaxAttempts - 1)
	w.markTransientInternal(context.Background(), row, stderrors.New("db still down"))
	exh := withMessage(t, buf, "forwarding: internal transient exhausted — row failed")
	if len(exh) != 1 {
		t.Fatalf("internal-exhausted records = %d, want 1\n%s", len(exh), buf.String())
	}
	if exh[0]["level"] != "warn" {
		t.Errorf("exhausted level = %v, want warn", exh[0]["level"])
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
		forwarding.Result{Outcome: "bogus_outcome", Err: stderrors.New("plugin returned garbage")}, 0)

	recs := withMessage(t, buf, "forwarder: returned unrecognised Outcome")
	if len(recs) != 1 {
		t.Fatalf("unrecognised-outcome records = %d, want 1\n%s", len(recs), buf.String())
	}
	if e, _ := recs[0]["error"].(string); !strings.Contains(e, "garbage") {
		t.Errorf("the warning must carry the cause; error = %v", recs[0]["error"])
	}
}
