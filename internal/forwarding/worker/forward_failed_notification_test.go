package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Producer proofs for W-0001 / ADR 0076 — the daemon-side forward.failed writer
// wired into Worker.markFailed. Recording is best-effort and per-boundary; it
// must never persist the provider Reason, never persist a corrupt action
// verbatim, and never change the forward disposition or suppress the hub event.

type forwardFailedDetailFields struct {
	QsoID     int64  `json:"qso_id"`
	Forwarder string `json:"forwarder"`
	Action    string `json:"action"`
	Attempts  int    `json:"attempts"`
}

// A committed terminal failure records a typed operator_event whose detail holds
// qso_id/forwarder/action/attempts and NEVER the upstream provider Reason.
func TestForwardFailed_RecordsTypedDetailWithoutProviderReason(t *testing.T) {
	h := newHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h) // forwarder "stub", action insert, attempts 0

	const secret = "token=SECRET-abc123"
	reason := "401 unauthorized: " + secret + " — do not store"
	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeTerminal, Err: stderrors.New(reason)}, time.Millisecond)

	evs, err := h.db.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 10)
	if err != nil {
		t.Fatalf("fetch events: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("operator_event rows = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Kind != "forward.failed" {
		t.Errorf("kind = %q, want forward.failed", ev.Kind)
	}
	if ev.Severity != "warn" {
		t.Errorf("severity = %q, want warn", ev.Severity)
	}
	if ev.Build == "" {
		t.Errorf("build is empty — the daemon build version must be stamped")
	}
	var d forwardFailedDetailFields
	if err := json.Unmarshal(ev.Detail, &d); err != nil {
		t.Fatalf("detail not JSON: %v (%s)", err, ev.Detail)
	}
	if d.QsoID != row.QsoID || d.Forwarder != "stub" || d.Action != "insert" || d.Attempts != 1 {
		t.Errorf("detail = %+v, want qso_id=%d forwarder=stub action=insert attempts=1", d, row.QsoID)
	}
	// The provider Reason must never reach the durable store.
	lower := strings.ToLower(string(ev.Detail))
	if strings.Contains(string(ev.Detail), secret) || strings.Contains(lower, "unauthorized") {
		t.Errorf("detail leaked the provider Reason: %s", ev.Detail)
	}
}

// forwardFailedDetail bounds the action: known values pass through, anything else
// becomes the sentinel "unknown". It never emits a reason/last_error field.
func TestForwardFailedDetail_BoundsActionAndOmitsReason(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"insert", "insert"},
		{"update", "update"},
		{"delete", "delete"},
		{"garbage", "unknown"},
		{"", "unknown"},
		{"insert; DROP TABLE qso", "unknown"},
	} {
		row := types.QsoUpload{QsoID: 42, Action: tc.in, Attempts: 2}
		raw := forwardFailedDetail(row, "qrz")

		var d forwardFailedDetailFields
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("action %q: detail not JSON: %v", tc.in, err)
		}
		if d.Action != tc.want {
			t.Errorf("action %q -> %q, want %q", tc.in, d.Action, tc.want)
		}
		if d.QsoID != 42 || d.Forwarder != "qrz" || d.Attempts != 3 {
			t.Errorf("action %q: detail = %+v, want qso_id=42 forwarder=qrz attempts=3", tc.in, d)
		}
		for _, forbidden := range []string{"reason", "last_error"} {
			if strings.Contains(strings.ToLower(string(raw)), forbidden) {
				t.Errorf("action %q: detail must never carry a %q field: %s", tc.in, forbidden, raw)
			}
		}
	}
}

// A forced operator_event insert failure is best-effort: the upload stays
// terminally failed and the ephemeral hub event still fires.
func TestForwardFailed_RecordFailureDoesNotDisturbForwardOutcome(t *testing.T) {
	h := newHarness(t)
	w := newReachWorker(t, h)
	row := seedClaimedRow(t, h)

	// Force RecordOperatorEvent's INSERT to fail via a second connection that
	// installs a BEFORE INSERT abort trigger on operator_event.
	blockOperatorEventInserts(t, h)

	sub, unsub := h.hub.Subscribe()
	defer unsub()

	w.persistOutcome(context.Background(), row, "G0ABC",
		forwarding.Result{Outcome: forwarding.OutcomeTerminal, Err: stderrors.New("400 rejected")}, time.Millisecond)

	// The ephemeral hub event still fires — the record failure must not suppress it.
	select {
	case ev := <-sub:
		if ev.Name != events.NameForwardFailed {
			t.Fatalf("published %q, want %q", ev.Name, events.NameForwardFailed)
		}
	case <-time.After(time.Second):
		t.Fatal("no ForwardFailed hub event — the record failure suppressed it")
	}

	// The upload is still terminally failed (disposition unchanged).
	if got := h.fetchUpload(row.QsoID); got.Status != status.Failed.String() {
		t.Errorf("upload status = %q, want failed — the record failure changed the disposition", got.Status)
	}

	// Nothing was recorded (the insert was blocked): best-effort, not fatal.
	evs, err := h.db.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 10)
	if err != nil {
		t.Fatalf("fetch events: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("operator_event rows = %d, want 0 (insert was blocked)", len(evs))
	}
}

// blockOperatorEventInserts opens a second connection to the harness's file DB
// and installs a BEFORE INSERT trigger that aborts, so the producer's
// RecordOperatorEvent insert fails while MarkUploadFailedWithContext still commits.
func blockOperatorEventInserts(t *testing.T, h *testHarness) {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)",
		h.db.DatabaseConfig.Path)
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(
		`CREATE TRIGGER test_block_oe_insert BEFORE INSERT ON operator_event
		 BEGIN SELECT RAISE(ABORT, 'blocked by test'); END;`); err != nil {
		t.Fatalf("install abort trigger: %v", err)
	}
}
