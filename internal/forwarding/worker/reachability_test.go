package worker

// L11-C2 — Destination down/recovered logged as transitions, not per-retry.
// (docs/reviews/internal-codebase-logging-gaps.md; criteria in queue_context_test.go.)
//
//	When a destination goes unreachable and stays that way across many retries, the log
//	shows ONE "destination unreachable" record at the transition — not one Info per retry —
//	and ONE "destination recovered" record when it is reachable again. "it just went down"
//	is tellable from "it has been down a while and this is another silent retry", and
//	"recovered" from an ordinary success.
//
// Rulings 2026-08-15: the FIRST OutcomeUnreachable marks the destination down; the FIRST
// non-Unreachable outcome marks recovery, INCLUDING a Terminal rejection (reaching the host
// to be rejected still proves the host is up). The per-attempt `forwarding: attempt` record
// for an in-outage retry drops to Debug — that is the flood the finding is about; the
// default-level signal for the outage is carried entirely by the two transition records.
//
// FIXTURE NOTE (why real claimed rows, not literals): the demotion is the difference between
// Debug and Info, but logAttempt's FIRST case is disp==persist_failed → Error. A row literal
// that is not in the DB fails markUnreachable's conditional write with "not found" → Error,
// so a demotion assertion against it would pass without ever exercising the Debug branch —
// the two paths would agree. The level-asserting test therefore seeds and CLAIMS a real row,
// so the disposition is persisted/rearmed and the Debug demotion is what actually sets the
// level. The pure transition-COUNT tests may use either, since the transition fires on the
// reachability state regardless of the local queue disposition.

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const (
	msgDestUnreachable = "forwarding: destination unreachable"
	msgDestRecovered   = "forwarding: destination recovered"
)

func newReachWorker(t *testing.T, h *testHarness) *Worker {
	t.Helper()
	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	return w
}

// seedClaimedRow seeds a QSO, enqueues a stub upload and claims it, returning the row in
// its in_progress state — so markUnreachable/markSuccess resolve as a real persisted/rearmed
// disposition rather than the "not found" persist_failed a bare literal would produce.
func seedClaimedRow(t *testing.T, h *testHarness) types.QsoUpload {
	t.Helper()
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	claimed, err := h.db.ClaimPendingUploadsWithContext(context.Background(), "stub", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v (rows=%d)", err, len(claimed))
	}
	return claimed[0]
}

func driveUnreachable(w *Worker, row types.QsoUpload, cause error) {
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeUnreachable, Err: cause}, time.Millisecond, forwardTestUUID)
}

func driveSuccess(w *Worker, row types.QsoUpload) {
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}, time.Millisecond, forwardTestUUID)
}

// C2: many unreachable retries produce exactly ONE default-level "destination unreachable"
// record, and every in-outage per-attempt record is Debug — not the Info (or, on a broken
// fixture, Error) flood the finding reports. Two drives on a real row: the first persists
// (in_progress→pending), the second re-arms (already pending) — both must be Debug.
func TestReachability_DownEdgeLoggedOnce_RetriesSilenced(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h)

	cause := stderrors.New("dial tcp: connection refused")
	driveUnreachable(w, row, cause) // down edge; row → pending
	driveUnreachable(w, row, cause) // still down; re-armed, no new transition

	down := withMessage(t, buf, msgDestUnreachable)
	if len(down) != 1 {
		t.Fatalf("destination-unreachable records = %d, want exactly 1 (transition-only)\n%s", len(down), buf.String())
	}
	if down[0]["level"] != "warn" {
		t.Errorf("down-edge level = %v, want warn", down[0]["level"])
	}
	if _, ok := down[0]["error"]; !ok {
		t.Errorf("down-edge record carries no error cause\n%s", buf.String())
	}

	// The flood the finding is about: every in-outage per-attempt record must be below the
	// default level. Info, Warn AND Error are all default-visible and all wrong here.
	attempts := withMessage(t, buf, msgAttempt)
	if len(attempts) != 2 {
		t.Fatalf("attempt records = %d, want 2 (one per drive)\n%s", len(attempts), buf.String())
	}
	for _, rec := range attempts {
		if lvl := rec["level"]; lvl != "debug" {
			t.Errorf("in-outage attempt record at %v, want debug — this is the flood L11 removes", lvl)
		}
	}
}

// C2: after an outage, the first outcome that reaches the host logs exactly one recovery
// Info carrying how long the outage lasted. Duration uses the injected reachability clock.
func TestReachability_RecoveryEdgeLoggedOnceWithDuration(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h)

	clk := time.Unix(2_000_000, 0).UTC()
	w.reach.now = func() time.Time { return clk }

	driveUnreachable(w, row, stderrors.New("connection refused")) // down at clk
	clk = clk.Add(30 * time.Second)                               // outage lasts 30s
	driveSuccess(w, row)                                          // reaches host → recovery

	rec := withMessage(t, buf, msgDestRecovered)
	if len(rec) != 1 {
		t.Fatalf("destination-recovered records = %d, want exactly 1\n%s", len(rec), buf.String())
	}
	if rec[0]["level"] != "info" {
		t.Errorf("recovery level = %v, want info", rec[0]["level"])
	}
	if s, _ := rec[0]["unreachable_seconds"].(float64); int64(s) != 30 {
		t.Errorf("unreachable_seconds = %v, want 30", rec[0]["unreachable_seconds"])
	}
}

// C2 ruling: recovery fires on the first NON-Unreachable outcome, INCLUDING a Terminal
// rejection — the host was reached, so it is reachable, even though it rejected the upload.
// Fixture recovers via Terminal: an implementation that only recovered on success stays
// "down" forever here and logs no recovery.
func TestReachability_TerminalCountsAsRecovery(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h)

	driveUnreachable(w, row, stderrors.New("connection refused"))
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeTerminal, Err: stderrors.New("400 rejected")}, time.Millisecond, forwardTestUUID)

	if rec := withMessage(t, buf, msgDestRecovered); len(rec) != 1 {
		t.Fatalf("recovered records = %d, want 1 — a Terminal rejection proves the host is reachable\n%s",
			len(rec), buf.String())
	}
}

// C2: transitions, not a one-shot. A second down→up cycle logs a second pair. An
// implementation that latched "already reported" after the first outage fails the second half.
func TestReachability_SecondCycleLogsSecondPair(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h)

	driveUnreachable(w, row, stderrors.New("refused")) // down 1
	driveSuccess(w, row)                               // up 1
	driveUnreachable(w, row, stderrors.New("refused")) // down 2
	driveSuccess(w, row)                               // up 2

	if down := withMessage(t, buf, msgDestUnreachable); len(down) != 2 {
		t.Errorf("destination-unreachable records = %d, want 2 (one per outage)\n%s", len(down), buf.String())
	}
	if up := withMessage(t, buf, msgDestRecovered); len(up) != 2 {
		t.Errorf("destination-recovered records = %d, want 2 (one per recovery)\n%s", len(up), buf.String())
	}
}
