package worker

// L11-C4 — Expected shutdown cancellation is not an upstream failure.
// (docs/reviews/internal-codebase-logging-gaps.md; criteria in queue_context_test.go.)
//
//	When the daemon is shutting down and an in-flight attempt is cancelled by context, NO
//	record classifies it as an upstream/forwarder failure, and the destination is not marked
//	unreachable. "we stopped because we're shutting down" is tellable from "the upstream
//	actually failed" — and a real upstream failure that merely coincides with shutdown must
//	STILL log and update reachability.
//
// Ruling 7 (2026-08-15): fully suppress ONLY when cancellation is causally attributable to
// the shutting-down context — gated on BOTH the worker ctx being cancelled AND the cause
// being context.Canceled. A coincident real upstream failure (res.Err is a real error, not
// Canceled) falls through and is handled normally. "Fully suppress" is read as: nothing at
// the operator's default level, and the row is left untouched (in_progress) for the next
// startup's orphan reset — the same contract processRowSafely's panic path already uses. A
// single Debug breadcrumb records why the row stayed in_progress (demote-don't-delete).
//
// The two cases are driven side by side because the CONTRAST is the criterion: the nearest
// confusable state is a genuine unreachable host, and suppressing it too would silently drop
// real outages during any period the ctx happened to be done.

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

const msgShutdownCancel = "forwarding: attempt cancelled by shutdown"

// C4: a mid-flight cancel during shutdown emits no default-level failure, does not mark the
// destination unreachable, and leaves the row in_progress for the orphan reset to reclaim.
func TestShutdownCancel_AttributableCancelIsFullySuppressed(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h) // in_progress

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // daemon is shutting down
	w.persistOutcome(ctx, row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeUnreachable, Err: context.Canceled}, time.Millisecond, forwardTestUUID)

	if recs := withMessage(t, buf, msgAttempt); len(recs) != 0 {
		t.Errorf("attempt records = %d, want 0 — a shutdown cancel is not an attempt result\n%s", len(recs), buf.String())
	}
	if recs := withMessage(t, buf, msgDestUnreachable); len(recs) != 0 {
		t.Errorf("destination-unreachable records = %d, want 0 — shutdown did not make the host unreachable", len(recs))
	}
	if recs := withMessage(t, buf, msgShutdownCancel); len(recs) != 1 {
		t.Errorf("shutdown-cancel breadcrumb records = %d, want 1 (at debug)\n%s", len(recs), buf.String())
	} else if recs[0]["level"] != "debug" {
		t.Errorf("breadcrumb level = %v, want debug", recs[0]["level"])
	}

	// The row must be left untouched for the next startup's orphan reset. persistOutcome
	// returned before any mark, so it stays in_progress — never marked pending/failed here.
	got := h.fetchUpload(row.QsoID)
	if got.Status != status.InProgress.String() {
		t.Errorf("row status = %q, want %q — a suppressed shutdown cancel must not mutate the row",
			got.Status, status.InProgress.String())
	}
}

// C4: the coincident real failure. ctx is cancelled (shutting down) but the forwarder
// returned a REAL unreachable error, not context.Canceled. This must NOT be suppressed: the
// destination is marked unreachable (the operator-visible Warn) and the attempt is logged.
// This fixture differs from the one above only in res.Err — which is the whole distinction.
func TestShutdownCancel_CoincidentRealFailureStillLogsAndMarksDown(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.persistOutcome(ctx, row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeUnreachable, Err: stderrors.New("no route to host")}, time.Millisecond, forwardTestUUID)

	if recs := withMessage(t, buf, msgShutdownCancel); len(recs) != 0 {
		t.Errorf("shutdown-cancel breadcrumb records = %d, want 0 — this was a real upstream failure", len(recs))
	}
	if recs := withMessage(t, buf, msgDestUnreachable); len(recs) != 1 {
		t.Errorf("destination-unreachable records = %d, want 1 — a real failure must still update reachability\n%s",
			len(recs), buf.String())
	}
	if recs := withMessage(t, buf, msgAttempt); len(recs) == 0 {
		t.Errorf("attempt records = 0, want >= 1 — a real failure must still be logged\n%s", buf.String())
	}
}

// C4 belt-and-braces: a SUCCESS that lands exactly as the ctx cancels must still persist. The
// guard keys on context.Canceled being the cause; a success has a nil cause, so errors.Is is
// false and the guard does not fire. Without this, a coarse `ctx.Err()!=nil` guard would drop
// an upload the upstream had already accepted.
func TestShutdownCancel_SuccessDuringCancelStillRecorded(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.persistOutcome(ctx, row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}, time.Millisecond, forwardTestUUID)

	if recs := withMessage(t, buf, msgShutdownCancel); len(recs) != 0 {
		t.Errorf("shutdown-cancel breadcrumb records = %d, want 0 — a success is not a cancel", len(recs))
	}
	if recs := withMessage(t, buf, msgAttempt); len(recs) != 1 {
		t.Errorf("attempt records = %d, want 1 — an accepted upload must still be recorded\n%s", len(recs), buf.String())
	} else if got, _ := recs[0]["outcome"].(string); got != "success" {
		t.Errorf("outcome = %q, want success", got)
	}
}
