package worker

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// AW-1 alpha.2: every forward.* SSE event carries the QSO's canonical uuid (qso_uuid),
// never empty. Normal outcomes thread the already-hydrated uuid; the early terminal paths —
// which never hydrate the QSO — resolve it via a narrow including-deleted read done BEFORE
// the terminal write. Per the operator amendment these NON-happy terminal paths must be
// exercised as sequences, not by calling markFailed directly:
//   - a soft-deleted insert/update (the QSO is gone from the live view),
//   - an unknown action (parse fails before any fetch),
//   - an internal-fetch retry exhaustion whose full hydration is broken by malformed
//     additional_data — the case the narrow uuid-only query exists to survive.
func TestForwardEvents_CarryQsoUUID(t *testing.T) {
	mustWorker := func(t *testing.T, h *testHarness, mutate func(*Config)) *Worker {
		t.Helper()
		cfg := defaultCfg("stub")
		if mutate != nil {
			mutate(&cfg)
		}
		w, err := New(cfg, buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
		if err != nil {
			t.Fatalf("new worker: %v", err)
		}
		return w
	}
	uuidOf := func(t *testing.T, h *testHarness, qsoID int64) string {
		t.Helper()
		uuid, err := h.db.FetchQsoUUIDByIDWithContext(context.Background(), qsoID)
		if err != nil {
			t.Fatalf("fetch qso uuid: %v", err)
		}
		return uuid
	}

	// --- Normal paths: the hydrated uuid is threaded through persistOutcome. ---

	t.Run("forward.succeeded threads the uuid", func(t *testing.T) {
		h := newHarness(t)
		row := seedClaimedRow(t, h)
		want := uuidOf(t, h, row.QsoID)
		w := mustWorker(t, h, nil)

		sub, unsub := h.hub.Subscribe()
		defer unsub()
		w.persistOutcome(context.Background(), row, "M0CMC",
			forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "logid-1"}, time.Millisecond, want)

		ev := awaitEvent(t, sub, events.NameForwardSucceeded)
		if got := ev.Payload.(events.ForwardSucceededPayload).QsoUUID; got != want {
			t.Errorf("qso_uuid = %q, want %q", got, want)
		}
	})

	t.Run("forward.failed threads the uuid (normal terminal)", func(t *testing.T) {
		h := newHarness(t)
		row := seedClaimedRow(t, h)
		want := uuidOf(t, h, row.QsoID)
		w := mustWorker(t, h, nil)

		sub, unsub := h.hub.Subscribe()
		defer unsub()
		w.persistOutcome(context.Background(), row, "M0CMC",
			forwarding.Result{Outcome: forwarding.OutcomeTerminal, Err: stderrors.New("400 rejected")}, time.Millisecond, want)

		ev := awaitEvent(t, sub, events.NameForwardFailed)
		if got := ev.Payload.(events.ForwardFailedPayload).QsoUUID; got != want {
			t.Errorf("qso_uuid = %q, want %q", got, want)
		}
	})

	// --- Early terminal paths: the uuid is resolved (row not hydrated). ---

	for _, tc := range []struct {
		name string
		act  action.Action
	}{
		{"soft-deleted insert", action.Insert},
		{"soft-deleted update", action.Update},
	} {
		t.Run("forward.failed resolves the uuid ("+tc.name+")", func(t *testing.T) {
			h := newHarness(t)
			qsoID := h.seedLogbookAndQso()
			want := uuidOf(t, h, qsoID)
			h.enqueueUpload(qsoID, "stub", stub.Type, tc.act)
			h.softDeleteQso(qsoID)
			w := mustWorker(t, h, nil)

			sub, unsub := h.hub.Subscribe()
			defer unsub()
			runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
				return u.Status == status.Failed.String()
			})

			ev := awaitEvent(t, sub, events.NameForwardFailed)
			if got := ev.Payload.(events.ForwardFailedPayload).QsoUUID; got != want {
				t.Errorf("qso_uuid = %q, want %q (resolved including soft-deleted)", got, want)
			}
		})
	}

	t.Run("forward.failed resolves the uuid (unknown action)", func(t *testing.T) {
		h := newHarness(t)
		row := seedClaimedRow(t, h)
		want := uuidOf(t, h, row.QsoID)
		w := mustWorker(t, h, nil)
		row.Action = "garbage" // action.Parse fails before any QSO fetch → markFailed early

		sub, unsub := h.hub.Subscribe()
		defer unsub()
		w.processRow(context.Background(), row)

		ev := awaitEvent(t, sub, events.NameForwardFailed)
		if got := ev.Payload.(events.ForwardFailedPayload).QsoUUID; got != want {
			t.Errorf("qso_uuid = %q, want %q", got, want)
		}
	})

	// The narrow uuid-only read must resolve identity even when full hydration is broken:
	// a QSO with malformed additional_data fails FetchQsoByIdWithContext, so the row cycles
	// through markTransientInternal to exhaustion — and the forward.failed there must still
	// carry the uuid the narrow SELECT recovers.
	t.Run("forward.failed resolves the uuid (internal-fetch exhaustion, malformed additional_data)", func(t *testing.T) {
		h := newHarness(t)
		qsoID := h.seedLogbookAndQso()
		want := uuidOf(t, h, qsoID) // read the uuid BEFORE corrupting the row
		h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
		corruptAdditionalData(t, h, qsoID)
		w := mustWorker(t, h, func(c *Config) { c.Retry.MaxAttempts = 1 }) // first transient exhausts

		sub, unsub := h.hub.Subscribe()
		defer unsub()
		runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
			return u.Status == status.Failed.String()
		})

		ev := awaitEvent(t, sub, events.NameForwardFailed)
		if got := ev.Payload.(events.ForwardFailedPayload).QsoUUID; got != want {
			t.Errorf("qso_uuid = %q, want %q (narrow read survives malformed additional_data)", got, want)
		}
	})
}

// awaitEvent returns the next event with the given name, skipping any others, or fails.
func awaitEvent(t *testing.T, ch <-chan events.Event, name string) events.Event {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Name == name {
				return ev
			}
		case <-timeout:
			t.Fatalf("no %s event within the deadline", name)
			return events.Event{}
		}
	}
}

// corruptAdditionalData writes well-formed JSON of the WRONG SHAPE ('[]', an array) into a
// QSO's additional_data via a second connection: it satisfies the json_valid CHECK
// constraint but fails QsoModelToType's unmarshal into the types.Qso struct, so full
// hydration errors while the row — and its uuid — still exist. Mirrors
// blockOperatorEventInserts' raw-connection approach.
func corruptAdditionalData(t *testing.T, h *testHarness, qsoID int64) {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)",
		h.db.DatabaseConfig.Path)
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(`UPDATE qso SET additional_data = '[]' WHERE id = ?`, qsoID); err != nil {
		t.Fatalf("corrupt additional_data: %v", err)
	}
}
