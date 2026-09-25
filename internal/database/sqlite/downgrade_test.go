package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/failure"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 slice 1 (rollback drill): the operator path that migrates the log
// schema DOWN to a named version, so a tagged older binary — whose bundled
// migration source stops short of the newer head — can open the file again.
// Down only, log set only, rows retained: the drill's proof is that the QSO and
// upload-queue rows that existed before are identical after.
func TestDowngradeLogSchemaTo_DownToPriorVersion_RetainsRows(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if v := schemaVersion(t, svc); v != 14 {
		t.Fatalf("schema version = %d, want 14 (head)", v)
	}
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	claimOneAndMark(t, svc, "qrz", func(id int64) error {
		return svc.MarkUploadFailedWithContext(ctx, id, "reason", failure.Auth)
	})
	qsoBefore, err := svc.FetchQsoByIdWithContext(ctx, qsoID)
	if err != nil {
		t.Fatalf("fetch qso: %v", err)
	}

	from, err := svc.DowngradeLogSchemaTo(10)
	if err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	if from != 14 {
		t.Fatalf("reported from = %d, want 14", from)
	}
	if v := schemaVersion(t, svc); v != 10 {
		t.Fatalf("schema version after downgrade = %d, want 10", v)
	}
	// 0011's column is gone — the down migration really ran.
	if hasColumn(t, svc, "qso_upload", "failure_class") {
		t.Fatal("qso_upload.failure_class still present after the 0011 down migration")
	}
	// The rows survive byte-for-byte on the fields the older schema knows.
	qsoAfter, err := svc.FetchQsoByIdWithContext(ctx, qsoID)
	if err != nil {
		t.Fatalf("fetch qso after: %v", err)
	}
	if qsoAfter.UUID != qsoBefore.UUID || qsoAfter.Call != qsoBefore.Call || qsoAfter.QsoDate != qsoBefore.QsoDate {
		t.Fatalf("qso row changed across the downgrade: before %+v after %+v", qsoBefore, qsoAfter)
	}
	var uploads int
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM qso_upload WHERE qso_id = ?`, qsoID).Scan(&uploads); err != nil || uploads != 1 {
		t.Fatalf("upload rows after downgrade = %d (%v), want 1", uploads, err)
	}
	if _, err := svc.FetchLogbookByIDWithContext(ctx, lbID); err != nil {
		t.Fatalf("logbook row lost across the downgrade: %v", err)
	}
}

// Down only: the daemon is the one that migrates up. A target at or above the
// current version is refused before anything runs, and the version is untouched.
func TestDowngradeLogSchemaTo_RefusesSameOrHigherTarget(t *testing.T) {
	svc := testService(t)
	for _, target := range []uint{14, 15, 99} {
		if _, err := svc.DowngradeLogSchemaTo(target); err == nil {
			t.Errorf("target %d accepted at head; want a refusal (down only)", target)
		}
	}
	if v := schemaVersion(t, svc); v != 14 {
		t.Fatalf("schema version = %d after refused downgrades, want 14", v)
	}
	// Version 0 is "no schema": 0001's down step drops every table. No build
	// ever ran at 0, so it is refused by an explicit floor, not left to whatever
	// the migration library does with a nonexistent target.
	if _, err := svc.DowngradeLogSchemaTo(0); err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("target 0 = %v; want a refusal naming the floor of 1", err)
	}
	// The load-bearing case: from a LOWERED version, a higher target that the
	// bundled source could satisfy must still be refused — this is what keeps the
	// command from being a hidden "migrate up".
	if _, err := svc.DowngradeLogSchemaTo(11); err != nil {
		t.Fatalf("downgrade to 11: %v", err)
	}
	if _, err := svc.DowngradeLogSchemaTo(12); err == nil {
		t.Fatal("target 12 accepted from version 11; the command migrated UP")
	}
	if v := schemaVersion(t, svc); v != 11 {
		t.Fatalf("schema version = %d after the refused upward target, want 11", v)
	}
}

func hasColumn(t *testing.T, svc *Service, table, column string) bool {
	t.Helper()
	rows, err := svc.handle.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}
