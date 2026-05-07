package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// insertQsoHistory is a test-only helper that wraps
// BeginTx / InsertQsoHistoryTx / Commit. Mirrors enqueueUpload's
// shape so qso_history tests don't repeat the tx dance.
func insertQsoHistory(t *testing.T, svc *Service, qsoUUID string, op action.Action, src source.Source, image []byte) {
	t.Helper()
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	if err = svc.InsertQsoHistoryTx(ctx, tx, qsoUUID, op, src, image); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert qso_history: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestInsertQsoHistoryTx_HappyPath(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQso(qso); err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	image := []byte(`{"call":"M0CMC","band":"40m"}`)
	insertQsoHistory(t, svc, qso.UUID, action.Update, source.API, image)

	rows, err := svc.FetchQsoHistoryByUUIDWithContext(context.Background(), qso.UUID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.QsoUUID != qso.UUID {
		t.Fatalf("qso_uuid = %q, want %q", r.QsoUUID, qso.UUID)
	}
	if r.Op != "update" {
		t.Fatalf("op = %q, want update", r.Op)
	}
	if r.Source != "api" {
		t.Fatalf("source = %q, want api", r.Source)
	}
	if string(r.BeforeImage) != string(image) {
		t.Fatalf("before_image = %q, want %q", string(r.BeforeImage), string(image))
	}
}

func TestInsertQsoHistoryTx_RejectsInsertOp(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()

	// action.Insert is not a valid op for qso_history — Go-side guard
	// should reject it before we hit the SQL CHECK.
	err = svc.InsertQsoHistoryTx(ctx, tx, "00000000-0000-7000-8000-000000000000",
		action.Insert, source.API, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error rejecting action.Insert")
	}
	if !strings.Contains(err.Error(), "invalid op") {
		t.Fatalf("error = %v, want 'invalid op' substring", err)
	}
}

func TestInsertQsoHistoryTx_RejectsEmptyArgs(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()

	cases := []struct {
		name    string
		uuid    string
		src     source.Source
		image   []byte
		wantSub string
	}{
		{"empty uuid", "", source.API, []byte(`{}`), "qsoUUID is empty"},
		{"empty source", "u", source.Source(""), []byte(`{}`), "source is empty"},
		{"empty image", "u", source.API, nil, "beforeImage is empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.InsertQsoHistoryTx(ctx, tx, c.uuid, action.Update, c.src, c.image)
			if err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("error = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

// TestQsoHistory_AppendOnly_TriggersFire verifies that the
// trg_qso_history_no_update / trg_qso_history_no_delete BEFORE
// triggers reject UPDATE and DELETE statements against qso_history.
// These triggers are belt-and-braces over "the daemon never mutates
// this table"; the test guards them so a future migration that drops
// or renames a trigger is caught.
func TestQsoHistory_AppendOnly_TriggersFire(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQso(qso); err != nil {
		t.Fatalf("insert qso: %v", err)
	}
	insertQsoHistory(t, svc, qso.UUID, action.Update, source.API, []byte(`{"x":1}`))

	ctx := context.Background()

	// UPDATE attempt — trigger should ABORT with the message we set.
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE qso_history SET source = 'tampered' WHERE qso_uuid = ?`, qso.UUID)
	if err == nil {
		_ = tx.Rollback()
		cancel()
		t.Fatal("expected UPDATE to be rejected by trigger")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE error = %v, want 'append-only' substring", err)
	}
	_ = tx.Rollback()
	cancel()

	// DELETE attempt — same expectation.
	tx, cancel, err = svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM qso_history WHERE qso_uuid = ?`, qso.UUID)
	if err == nil {
		_ = tx.Rollback()
		cancel()
		t.Fatal("expected DELETE to be rejected by trigger")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("DELETE error = %v, want 'append-only' substring", err)
	}
	_ = tx.Rollback()
	cancel()

	// Row should still be there after both rejected attempts.
	rows, err := svc.FetchQsoHistoryByUUIDWithContext(ctx, qso.UUID)
	if err != nil {
		t.Fatalf("fetch after rejected mutations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (row should survive rejected mutations)", len(rows))
	}
}

func TestFetchQsoHistoryByUUID_OrderedAscending(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQso(qso); err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	// Two appends — the index ASC ordering should preserve insertion order.
	insertQsoHistory(t, svc, qso.UUID, action.Update, source.API, []byte(`{"step":1}`))
	insertQsoHistory(t, svc, qso.UUID, action.Update, source.API, []byte(`{"step":2}`))
	insertQsoHistory(t, svc, qso.UUID, action.Delete, source.API, []byte(`{"step":3}`))

	rows, err := svc.FetchQsoHistoryByUUIDWithContext(context.Background(), qso.UUID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	wantOps := []string{"update", "update", "delete"}
	for i, want := range wantOps {
		if rows[i].Op != want {
			t.Fatalf("rows[%d].Op = %q, want %q", i, rows[i].Op, want)
		}
	}
}

func TestFetchQsoHistoryByUUID_EmptyForUnknownUUID(t *testing.T) {
	svc := testService(t)
	rows, err := svc.FetchQsoHistoryByUUIDWithContext(
		context.Background(), "00000000-0000-7000-8000-000000000000")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}
