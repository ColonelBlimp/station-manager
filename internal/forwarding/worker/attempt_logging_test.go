package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Acceptance criteria for Diff A of docs/reviews/forwarding-logging-gaps.md —
// the restructuring half of F1, and all of F6. Operator's wording, 2026-08-01:
//
//	1. When Forwarder.Submit returns, smd.log records exactly one default-visible
//	   attempt-result event after the local queue disposition is resolved. It
//	   carries the forwarder, QSO, upstream outcome, local disposition, and
//	   submit_duration_ms. That duration spans only Forwarder.Submit, so a high
//	   value identifies a slow upstream without attributing queue persistence or
//	   stamp-sync time to it. Severity remains appropriate to the disposition:
//	   Info for success/retry, Warn for terminal/exhausted, and Error for
//	   persistence failure.
//
//	2. A completed-upload record appears only after the queue row and, where
//	   applicable, ADIF stamp are persisted. An upstream-accepted upload whose
//	   persistence failed records persist_failed; one whose row was concurrently
//	   re-armed records rearmed. Neither records a completed upload.
//
//	3. At the default Info level no per-attempt "about to submit" record appears.
//	   With Debug enabled, one appears before each attempt, so an attempt that
//	   never returns leaves a trace.
//
// Scope, deliberately (operator, 2026-08-01):
//
//   - Diff A does NOT close F1. It closes F6 and F1's volume/restructuring half;
//     F1's actual missing-provenance finding needs the `origin` field, which is a
//     qso_upload schema and API-contract change and ships as Diff B.
//   - "where applicable" in criterion 2 is load-bearing: delete actions and
//     forwarders with no AdifPrefix legitimately have no stamp, so a completed
//     upload must not be gated on one they never had.
//   - submit_duration_ms measures ONLY Forwarder.Submit. Including the queue
//     write or the stamp-sync hook would defeat its one job — telling a slow
//     upstream from a slow local write. Note the honest limit the operator
//     flagged: it identifies a slow upstream, it does NOT positively identify a
//     slow local write, which would need a second duration this diff does not add.
//
// The correctness condition (worker.go persistOutcome): the restructure must not
// merely move fields onto the existing success line. Completion must FOLLOW
// persistence, and re-arm must stay distinguishable from persistence failure —
// today the success line is written BEFORE markSuccess runs, so an upstream-
// accepted row that is still queued and will be sent again is indistinguishable
// from a completed upload.

const (
	msgAttempt = "forwarding: attempt"
	msgSubmit  = "forwarding: submit"
)

// Local dispositions expected on the attempt record. Deliberately literal
// strings rather than the production constants: these are the WIRE values an
// operator greps for, so a rename of the Go constant must not silently pass.
const (
	wantPersisted     = "persisted"
	wantPersistFailed = "persist_failed"
	wantRearmed       = "rearmed"
)

func captureHarness(t *testing.T) (*testHarness, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	return newHarnessWithLogger(t, logging.NewForWriter(buf)), buf
}

func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func withMessage(t *testing.T, buf *bytes.Buffer, message string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, rec := range records(t, buf) {
		if rec["message"] == message {
			out = append(out, rec)
		}
	}
	return out
}

// Criterion 1 + 2, the success path: exactly one attempt record, at Info,
// carrying both halves of the outcome and the duration.
func TestAttemptLogging_SuccessRecordsOneEventWithBothHalves(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == "uploaded"
	})

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) != 1 {
		t.Fatalf("attempt records = %d, want exactly 1\n%s", len(recs), buf.String())
	}
	rec := recs[0]
	if rec["level"] != "info" {
		t.Errorf("level = %v, want info for a persisted success", rec["level"])
	}
	if rec["outcome"] != "success" {
		t.Errorf("outcome = %v, want success", rec["outcome"])
	}
	if rec["disposition"] != wantPersisted {
		t.Errorf("disposition = %v, want %q", rec["disposition"], wantPersisted)
	}
	if _, ok := rec["submit_duration_ms"]; !ok {
		t.Errorf("record carries no submit_duration_ms")
	}
	for _, f := range []string{"forwarder", "qso_id", "action", "call"} {
		if _, ok := rec[f]; !ok {
			t.Errorf("record is missing %q", f)
		}
	}
}

// Criterion 1, severity: a terminal upstream rejection stays Warn. Info for
// everything would bury the outcome that needs operator attention.
func TestAttemptLogging_TerminalOutcomeIsWarn(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysTerminal, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == "failed"
	})

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) != 1 {
		t.Fatalf("attempt records = %d, want exactly 1\n%s", len(recs), buf.String())
	}
	if recs[0]["level"] != "warn" {
		t.Errorf("level = %v, want warn for a terminal rejection", recs[0]["level"])
	}
	if recs[0]["outcome"] != "terminal" {
		t.Errorf("outcome = %v, want terminal", recs[0]["outcome"])
	}
	if recs[0]["disposition"] != wantPersisted {
		t.Errorf("disposition = %v, want %q — the rejection itself persisted fine",
			recs[0]["disposition"], wantPersisted)
	}
}

// Criterion 1, severity: a transient outcome that will be retried is Info — it
// is normal operation, not a fault.
func TestAttemptLogging_TransientRetryIsInfo(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysTransient, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Attempts >= 1
	})

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) == 0 {
		t.Fatalf("no attempt record\n%s", buf.String())
	}
	if recs[0]["level"] != "info" {
		t.Errorf("level = %v, want info for a transient that will retry", recs[0]["level"])
	}
	if recs[0]["outcome"] != "transient" {
		t.Errorf("outcome = %v, want transient", recs[0]["outcome"])
	}
}

// Criterion 3: at the default level the pre-attempt breadcrumb is absent, so it
// costs nothing; at Debug it is present and PRECEDES the attempt record, which
// is what makes an attempt that never returns visible.
func TestAttemptLogging_SubmitBreadcrumbIsDebugOnly(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == "uploaded"
	})

	// NewForWriter enables every level, so a Debug record is captured here. The
	// criterion's default-level half is asserted by the LEVEL FIELD rather than by
	// absence: a test that merely observed nothing at Info would also pass against
	// an implementation that deleted the breadcrumb entirely, which the operator
	// explicitly rejected (demoting keeps the prospective trace; deleting does not).
	subs := withMessage(t, buf, msgSubmit)
	if len(subs) != 1 {
		t.Fatalf("submit records = %d, want exactly 1 (at debug)\n%s", len(subs), buf.String())
	}
	if subs[0]["level"] != "debug" {
		t.Errorf("submit level = %v, want debug — it must not cost anything at the default level",
			subs[0]["level"])
	}

	// Ordering: the breadcrumb exists to precede a call that may never return.
	var sawSubmit bool
	for _, rec := range records(t, buf) {
		switch rec["message"] {
		case msgSubmit:
			sawSubmit = true
		case msgAttempt:
			if !sawSubmit {
				t.Fatalf("attempt record preceded the submit breadcrumb\n%s", buf.String())
			}
		}
	}
}

// Criterion 2's "where applicable": a DELETE action has no ADIF stamp, and a
// completed upload must not be gated on one it never had.
//
// The fixture is the point — a stampless path is exactly where an implementation
// that waited unconditionally for a stamp would report the wrong disposition.
func TestAttemptLogging_DeleteWithoutStampStillRecordsPersisted(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.seedSuccessfulInsert(qsoID, "stub", stub.Type, "stub-ok")
	h.softDeleteQso(qsoID)
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Delete)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runUntilAction(t, w, h, qsoID, action.Delete, func(u types.QsoUpload) bool {
		return u.Status == "uploaded"
	})

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) == 0 {
		t.Fatalf("no attempt record for the delete\n%s", buf.String())
	}
	last := recs[len(recs)-1]
	if last["disposition"] != wantPersisted {
		t.Errorf("disposition = %v, want %q — a delete has no ADIF stamp to wait for",
			last["disposition"], wantPersisted)
	}
	if last["outcome"] != "success" {
		t.Errorf("outcome = %v, want success", last["outcome"])
	}
}

// Criterion 2's other two dispositions. Both are reachable deterministically, so
// there is no excuse for leaving them unpinned — the criterion names them by
// value, and without these the implementation could return `persisted` for every
// path and still pass everything above.
//
// persistOutcome is driven directly rather than through the tick loop: the states
// being modelled (a row that is no longer claimed, a database that has gone away)
// are precisely the ones the loop is built to avoid producing.

// A row that is NOT in_progress fails the conditional queue write with
// ErrUploadReArmed — the same result a concurrent operator edit produces. The
// upstream said success; the row will be submitted again, so it must NOT read as
// a completed upload.
func TestAttemptLogging_RearmedRowIsNotReportedAsCompleted(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	row := h.fetchUpload(qsoID) // still `pending`: never claimed

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}, 7*time.Millisecond)

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) != 1 {
		t.Fatalf("attempt records = %d, want exactly 1\n%s", len(recs), buf.String())
	}
	if recs[0]["disposition"] != wantRearmed {
		t.Fatalf("disposition = %v, want %q — the row was re-armed and will be sent again",
			recs[0]["disposition"], wantRearmed)
	}
	if recs[0]["outcome"] != "success" {
		t.Errorf("outcome = %v, want success — the UPSTREAM still accepted it", recs[0]["outcome"])
	}
}

// An upstream-accepted upload whose local write cannot happen at all. Error
// severity, because this is the one case where SM believes something the queue
// does not record.
func TestAttemptLogging_PersistFailureIsErrorAndNotCompleted(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	row := h.fetchUpload(qsoID)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	// Take the database away between the upstream call and the queue write.
	if cerr := h.db.Close(); cerr != nil {
		t.Fatalf("close db: %v", cerr)
	}
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stub-ok"}, 7*time.Millisecond)

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) != 1 {
		t.Fatalf("attempt records = %d, want exactly 1\n%s", len(recs), buf.String())
	}
	if recs[0]["disposition"] != wantPersistFailed {
		t.Fatalf("disposition = %v, want %q", recs[0]["disposition"], wantPersistFailed)
	}
	if recs[0]["level"] != "error" {
		t.Errorf("level = %v, want error — the upstream accepted it and the queue did not record that",
			recs[0]["level"])
	}
}

// A panicking OnQsoStamped hook must not erase the attempt record for an upload
// that ALREADY persisted.
//
// Found by clean-room review of f31738bc, and it is a regression this diff
// introduced: before the restructure the success line was written BEFORE
// markSuccess ran, so a failing hook could not suppress it. Moving the record
// after persistence — which is the whole point of the change — put the
// best-effort hook in front of it. The record must sit between the database
// transition and any fallible callback.
//
// The panic also makes processRowSafely reset an already-uploaded row for retry,
// which is pre-existing behaviour and out of scope here; what this pins is that
// the completed attempt still leaves a trace.
func TestAttemptLogging_StampHookPanicDoesNotSuppressTheAttemptRecord(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "qrz", "qrz", action.Insert)

	fwd := &stampingForwarder{
		typeName: "qrz",
		prefix:   "QRZCOM",
		result:   forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "logid-1001"},
	}
	cfg := defaultCfg("qrz")
	cfg.OnQsoStamped = func(context.Context, int64) { panic("stamp-sync hook blew up") }

	w, err := New(cfg, fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	// Claim the row first: processRowSafely on an unclaimed row would resolve as
	// `rearmed` and never reach the stamping branch the hook hangs off, so the
	// fixture would prove nothing about hook ordering.
	claimed, cerr := h.db.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	if cerr != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v (rows=%d)", cerr, len(claimed))
	}
	w.processRowSafely(context.Background(), claimed[0])

	recs := withMessage(t, buf, msgAttempt)
	if len(recs) != 1 {
		t.Fatalf("attempt records = %d, want exactly 1 — the upload persisted, "+
			"so a failing best-effort hook must not erase its trace\n%s", len(recs), buf.String())
	}
	if recs[0]["disposition"] != wantPersisted {
		t.Errorf("disposition = %v, want %q", recs[0]["disposition"], wantPersisted)
	}
}
