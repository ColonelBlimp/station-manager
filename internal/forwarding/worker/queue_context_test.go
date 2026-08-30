package worker

// L11 — forwarding logs lack queue context and can bury signal during outages
// (docs/reviews/internal-codebase-logging-gaps.md). ATDD criteria, operator rulings
// 2026-08-15. Each is stated as: situation → operator-observable outcome → the nearest
// confusable state it must be told apart from. This file anchors the whole finding; the
// per-behaviour slices (reachability transitions, shutdown-cancel suppression, periodic
// summary) carry their own headers and reference these criteria by number.
//
//	C1 — Attempt records carry queue context.
//	  When any forwarding attempt is logged, its `forwarding: attempt` record carries
//	  upload_id, an UNCONDITIONAL attempt number, queued_at and queue_age_seconds. A
//	  first try (attempt=1) is tellable from a retry (attempt>=2), and a fresh row from
//	  one wedged in the queue for hours (queue_age_seconds), from the record alone.
//	  Nearest confusable: a slow UPSTREAM (high submit_duration_ms) vs a slow QUEUE
//	  (high queue_age_seconds) — two different faults that looked identical before.
//	  Rulings: attempt = row.Attempts+1 (1-based, first try reads 1); queue_age_seconds
//	  is non-negative (a row cannot have negative age — clock skew clamps to 0).
//
//	C2 — Destination down/recovered logged as transitions, not per-retry (reachability_test.go).
//	C3 — Periodic queue-depth summary (queue_summary_test.go, store query depth_test.go).
//	C4 — Expected shutdown cancellation is not an upstream failure (shutdown_cancel_test.go).
//
// The unconditional `attempt` supersedes the old CONDITIONAL `attempts` field, which was
// logged only on the transient/unreachable paths and carried the identical value
// (row.Attempts+1). Two near-identical fields would confuse a grep; the singular replaces
// the plural.

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// C1: every attempt record carries upload_id, an unconditional attempt number, queued_at,
// and a non-negative queue_age_seconds. persistOutcome is driven directly with a row whose
// CreatedAt and the worker clock are both fixed, so the age is exact rather than a tolerance.
func TestQueueContext_AttemptRecordCarriesQueueFields(t *testing.T) {
	h, buf := captureHarness(t)
	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	queuedAt := time.Unix(1_000_000, 0).UTC()
	w.now = func() time.Time { return queuedAt.Add(3661 * time.Second) } // 1h 1m 1s later

	// A row literal with a chosen CreatedAt/Attempts. It is not in the DB, so markSuccess
	// resolves as `rearmed` — irrelevant here: the attempt record still fires and is what
	// this pins. Attempts=2 means THIS is the third try, so attempt must read 3.
	row := types.QsoUpload{
		ID: 42, QsoID: 7, Action: action.Insert.String(),
		ForwarderName: "stub", Origin: "live", Attempts: 2, CreatedAt: queuedAt,
	}
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}, 7*time.Millisecond, forwardTestUUID)

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) != 1 {
		t.Fatalf("attempt records = %d, want exactly 1\n%s", len(recs), buf.String())
	}
	rec := recs[0]

	if id, _ := rec["upload_id"].(float64); int64(id) != 42 {
		t.Errorf("upload_id = %v, want 42 — the queue-row id, distinct from qso_id", rec["upload_id"])
	}
	if a, _ := rec["attempt"].(float64); int64(a) != 3 {
		t.Errorf("attempt = %v, want 3 (row.Attempts+1, this being the third try)", rec["attempt"])
	}
	if _, ok := rec["queued_at"]; !ok {
		t.Errorf("record carries no queued_at")
	}
	if age, ok := rec["queue_age_seconds"].(float64); !ok {
		t.Errorf("record carries no queue_age_seconds")
	} else if int64(age) != 3661 {
		t.Errorf("queue_age_seconds = %v, want 3661 (now-queued_at)", age)
	}
}

// C1: queue_age_seconds is non-negative. A row whose CreatedAt is in the FUTURE relative to
// the worker clock (clock skew between the DB host and this process) must clamp to 0, never
// report a negative age. The fixture makes the two paths differ: an unclamped implementation
// would emit a negative number here.
func TestQueueContext_QueueAgeIsNonNegativeUnderClockSkew(t *testing.T) {
	h, buf := captureHarness(t)
	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	now := time.Unix(1_000_000, 0).UTC()
	w.now = func() time.Time { return now }
	row := types.QsoUpload{
		ID: 43, QsoID: 8, Action: action.Insert.String(),
		ForwarderName: "stub", Origin: "live", CreatedAt: now.Add(5 * time.Second), // future
	}
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}, time.Millisecond, forwardTestUUID)

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) != 1 {
		t.Fatalf("attempt records = %d, want exactly 1\n%s", len(recs), buf.String())
	}
	if age, ok := recs[0]["queue_age_seconds"].(float64); !ok {
		t.Errorf("record carries no queue_age_seconds")
	} else if age < 0 {
		t.Errorf("queue_age_seconds = %v, want >= 0 — a row cannot have negative age", age)
	}
}
