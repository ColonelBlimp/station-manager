package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
)

// Migration proof for Diff B of docs/reviews/forwarding-logging-gaps.md — the
// qso_upload.origin column that closes F1's provenance half.
//
// Shape decided by the operator (2026-08-01): NOT NULL, no permanent default, a
// closed CHECK over the seven values, and existing rows copied with the literal
// 'legacy'. No default is the load-bearing part — a future raw or generated
// insert that omits provenance must fail LOUDLY rather than silently acquiring a
// value nobody chose. (A DB-side default would also let SQLBoiler treat the field
// as optional and omit a zero value, which is the opposite of the invariant.)
//
// SCOPE: this file proves the SCHEMA contract only. The Go-boundary guard
// (origin.Parse, mirroring action.Parse) is pinned with the enum package when it
// exists — it is deliberately NOT claimed here, because nothing in this file
// exercises it.
//
// SQLite cannot ALTER a CHECK, so 0007 follows the house rename-then-rebuild
// pattern (0002/0004/0006). Unlike those, it rebuilds a CHILD table, so the
// all-three-tables rebuild they use for `qso` does not apply — but three named
// schema objects hang off qso_upload and would otherwise stay attached to
// qso_upload_old and vanish with it:
//
//   - trg_qso_upload_set_updated_at  (modified_at maintenance)
//   - idx_qso_upload_pending         (partial: status IN ('pending','in_progress'))
//   - idx_qso_upload_uploaded        (partial: status = 'uploaded')
//
// The unique (qso_id, forwarder_name, action) constraint is recreated with the
// table itself and needs no separate step.

// schemaVersion reads the log migration set's current version.
func schemaVersion(t *testing.T, svc *Service) int {
	t.Helper()
	var v int
	if err := svc.handle.QueryRow(
		`SELECT version FROM ` + schemaMigrationsTable(MigrationSetLog)).Scan(&v); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	return v
}

// seedLogbookAndQsoRow inserts the logbook + QSO that a qso_upload row hangs off.
// Plain INSERTs, not INSERT OR IGNORE: an ignored failure here surfaces later as
// a bare FOREIGN KEY error on the upload row, which says nothing about the cause
// (it cost a round to find that dedupe_key must be the full 64-char digest).
func seedLogbookAndQsoRow(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO logbook (id, callsign, name) VALUES (1,'G4ABC','L')`); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO qso
		(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
		 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		VALUES (1,'01920000-0000-7000-8000-000000000001','G4ABC','40m','SSB',7050000,
		        '20250508','0845','0845','59','59','Test',?,1)`,
		fmt.Sprintf("%064d", 1)); err != nil {
		t.Fatalf("seed qso: %v", err)
	}
}

// seedUploadRow inserts a logbook + qso + one qso_upload row, returning the
// upload row's id. Kept explicit rather than reusing a helper so the columns this
// proof depends on are visible at the call site.
func seedUploadRow(t *testing.T, svc *Service, forwarder string) int64 {
	t.Helper()
	db := svc.handle
	seedLogbookAndQsoRow(t, db)
	res, err := db.Exec(`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin)
		VALUES (1, ?, 'stub', 'insert', 'pending', 'live')`, forwarder)
	if err != nil {
		t.Fatalf("seed qso_upload: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// The column exists, rejects anything outside the closed set, and — critically —
// rejects an OMITTED value rather than defaulting one.
func TestMigrate0007_OriginIsClosedAndHasNoDefault(t *testing.T) {
	svc := testService(t)
	db := svc.handle
	seedUploadRow(t, svc, "qrz")

	for _, v := range []string{"live", "import", "edit", "manual", "stamp_sync", "reconcile", "legacy"} {
		if _, err := db.Exec(`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin)
			VALUES (1, ?, 'stub', 'update', 'pending', ?)`, "f-"+v, v); err != nil {
			t.Errorf("origin %q should be accepted: %v", v, err)
		}
	}

	if _, err := db.Exec(`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin)
		VALUES (1, 'bad', 'stub', 'insert', 'pending', 'startup_recovery')`); err == nil {
		t.Error("an origin outside the closed set should violate the CHECK")
	}

	// The one that matters most: omission must fail, not default.
	if _, err := db.Exec(`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status)
		VALUES (1, 'omitted', 'stub', 'insert', 'pending')`); err == nil {
		t.Error("omitting origin should fail (NOT NULL, no default) — a row with " +
			"provenance nobody chose is exactly what this column exists to prevent")
	}
}

// A pre-0007 row becomes `legacy`, and ordinary retry bookkeeping leaves it
// alone. Contract 2 governs the re-enqueue case separately: an explicit
// re-enqueue REPLACES origin, so this asserts only the paths that must not.
func TestMigrate0007_ExistingRowsBecomeLegacyAndSurviveRetry(t *testing.T) {
	svc := testService(t)
	db := svc.handle

	// Assert we are actually AT 0007 before stepping back, or the rollback targets
	// 0006 instead — which rebuilds `qso` and makes the seed below die on a foreign
	// key, a failure that says nothing about origin. A proof that dies in setup is
	// worthless however red it looks.
	if v := schemaVersion(t, svc); v != 8 {
		t.Fatalf("schema version = %d, want 8 — head moved (a migration was added past 0008); "+
			"the step counts below assume 0008 is head, so update them or this proof tests nothing", v)
	}

	// Roll back to v6 (0008 down, then 0007 down), insert a row as v6 would have
	// (no origin column), then roll forward through 0007.
	applyMigrationSteps(t, svc, -2)
	seedLogbookAndQsoRow(t, db)
	if _, err := db.Exec(`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status)
		VALUES (1, 'qrz', 'qrz', 'insert', 'pending')`); err != nil {
		t.Fatalf("seed pre-0007 upload row: %v", err)
	}
	applyMigrationSteps(t, svc, 1)

	var origin string
	if err := db.QueryRow(`SELECT origin FROM qso_upload WHERE forwarder_name='qrz'`).Scan(&origin); err != nil {
		t.Fatalf("read migrated origin: %v", err)
	}
	if origin != "legacy" {
		t.Fatalf("migrated row origin = %q, want legacy", origin)
	}

	// An ordinary transient retry must preserve it — driven through the REAL
	// production method, not a hand-written UPDATE that could diverge from it.
	var id int64
	if err := db.QueryRow(`SELECT id FROM qso_upload WHERE forwarder_name='qrz'`).Scan(&id); err != nil {
		t.Fatalf("read id: %v", err)
	}
	if _, err := db.Exec(`UPDATE qso_upload SET status='in_progress' WHERE id=?`, id); err != nil {
		t.Fatalf("claim row: %v", err)
	}
	if err := svc.MarkUploadTransientRetryWithContext(t.Context(), id, 999, "boom"); err != nil {
		t.Fatalf("transient retry: %v", err)
	}

	// So must the startup orphan reset — which only acts on in_progress rows, so
	// the row is put BACK there first. Without this the reset is a no-op and the
	// assertion below passes against an implementation that clobbers origin on
	// every reset.
	if _, err := db.Exec(`UPDATE qso_upload SET status='in_progress' WHERE id=?`, id); err != nil {
		t.Fatalf("re-claim row: %v", err)
	}
	n, err := svc.ResetOrphanedUploadsWithContext(t.Context())
	if err != nil {
		t.Fatalf("orphan reset: %v", err)
	}
	if n != 1 {
		t.Fatalf("orphan reset touched %d rows, want 1 — the fixture left nothing to reset, "+
			"so this proves nothing about origin preservation", n)
	}
	if err := db.QueryRow(`SELECT origin FROM qso_upload WHERE forwarder_name='qrz'`).Scan(&origin); err != nil {
		t.Fatalf("re-read origin: %v", err)
	}
	if origin != "legacy" {
		t.Fatalf("origin = %q after retry + orphan reset, want legacy preserved", origin)
	}
}

// The rebuild must carry the trigger forward. Without it, modified_at maintenance
// silently stops — a failure with no error and no log line.
func TestMigrate0007_UpdatingAMigratedRowStillAdvancesModifiedAt(t *testing.T) {
	svc := testService(t)
	db := svc.handle
	id := seedUploadRow(t, svc, "qrz")

	if _, err := db.Exec(`UPDATE qso_upload SET modified_at = NULL WHERE id = ?`, id); err != nil {
		t.Fatalf("clear modified_at: %v", err)
	}
	if _, err := db.Exec(`UPDATE qso_upload SET attempts = attempts + 1 WHERE id = ?`, id); err != nil {
		t.Fatalf("bump attempts: %v", err)
	}

	var modified any
	if err := db.QueryRow(`SELECT modified_at FROM qso_upload WHERE id = ?`, id).Scan(&modified); err != nil {
		t.Fatalf("read modified_at: %v", err)
	}
	if modified == nil {
		t.Fatal("modified_at is NULL after an UPDATE — trg_qso_upload_set_updated_at " +
			"did not survive the table rebuild")
	}
}

// Both partial indexes must exist after up AND after down. A rebuild that renames
// the table without dropping them first leaves them attached to qso_upload_old,
// where DROP TABLE takes them with it — silently, and only visible as a slow
// claim query under load.
func TestMigrate0007_PartialIndexesSurviveBothDirections(t *testing.T) {
	svc := testService(t)

	// Without this the step-back targets 0006 and the test checks indexes that were
	// never at risk — passing before 0007 exists and proving nothing about it.
	if v := schemaVersion(t, svc); v != 8 {
		t.Fatalf("schema version = %d, want 8 — head moved past 0008; update the step counts below", v)
	}

	assertIndexes := func(when string) {
		t.Helper()
		// The PREDICATE is the point, not the name: an index recreated without its
		// WHERE clause still exists, still belongs to qso_upload, and silently stops
		// being the partial index the claim query was planned against.
		for _, want := range []struct{ name, predicate string }{
			{"idx_qso_upload_pending", "where status in ('pending', 'in_progress')"},
			{"idx_qso_upload_uploaded", "where status = 'uploaded'"},
		} {
			var ddl string
			err := svc.handle.QueryRow(
				`SELECT sql FROM sqlite_master WHERE type='index' AND name=? AND tbl_name='qso_upload'`,
				want.name).Scan(&ddl)
			if err != nil {
				t.Errorf("%s: index %s missing from qso_upload (%v)", when, want.name, err)
				continue
			}
			norm := strings.Join(strings.Fields(strings.ToLower(ddl)), " ")
			if !strings.Contains(norm, want.predicate) {
				t.Errorf("%s: index %s lost its partial predicate\n  want substring: %s\n  got: %s",
					when, want.name, want.predicate, norm)
			}
		}
	}

	assertIndexes("after up")
	applyMigrationSteps(t, svc, -2) // 0008 down, then 0007 down — reach v6
	assertIndexes("after down")
	applyMigrationSteps(t, svc, 2)
	assertIndexes("after re-up")
}

// A rename-then-rebuild is where foreign keys are silently orphaned. Assert the
// database agrees they are intact in both directions.
func TestMigrate0007_ForeignKeysIntactBothDirections(t *testing.T) {
	svc := testService(t)

	if v := schemaVersion(t, svc); v != 8 {
		t.Fatalf("schema version = %d, want 8 — head moved past 0008; update the step counts below", v)
	}
	seedUploadRow(t, svc, "qrz")

	check := func(when string) {
		t.Helper()
		rows, err := svc.handle.Query(`PRAGMA foreign_key_check`)
		if err != nil {
			t.Fatalf("%s: foreign_key_check: %v", when, err)
		}
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			t.Errorf("%s: PRAGMA foreign_key_check reported a violation", when)
		}
	}

	check("after up")
	applyMigrationSteps(t, svc, -2) // 0008 down, then 0007 down — reach v6
	check("after down")
	applyMigrationSteps(t, svc, 2)
	check("after re-up")
}

// Down-migration must preserve every pre-existing column. The rebuild copies an
// explicit column list, which is exactly where a column is silently dropped.
func TestMigrate0007_DownPreservesEveryPreExistingColumn(t *testing.T) {
	svc := testService(t)
	db := svc.handle
	id := seedUploadRow(t, svc, "qrz")

	if _, err := db.Exec(`UPDATE qso_upload SET
		forwarder_type='qrz', action='update', status='uploaded', attempts=3,
		last_attempt_at=111, next_attempt_at=222, last_error='boom', upstream_id='logid-9'
		WHERE forwarder_name='qrz'`); err != nil {
		t.Fatalf("populate every column: %v", err)
	}

	if v := schemaVersion(t, svc); v != 8 {
		t.Fatalf("schema version = %d, want 8 — head moved past 0008; the -2 below assumes 0008 is head", v)
	}
	applyMigrationSteps(t, svc, -2) // 0008 down, then 0007 down (the rebuild under test)

	var (
		gotID, gotQsoID                              int64
		createdAt, modifiedAt                        string
		fwdName, fwdType, act, st, lastErr, upstream string
		attempts, lastAt, nextAt                     int64
	)
	if err := db.QueryRow(`SELECT id, created_at, modified_at, qso_id, forwarder_name,
		forwarder_type, action, status, attempts, last_attempt_at, next_attempt_at,
		last_error, upstream_id
		FROM qso_upload WHERE forwarder_name='qrz'`).
		Scan(&gotID, &createdAt, &modifiedAt, &gotQsoID, &fwdName, &fwdType, &act, &st,
			&attempts, &lastAt, &nextAt, &lastErr, &upstream); err != nil {
		t.Fatalf("read back after down: %v", err)
	}

	// EVERY pre-0007 column, not a sample: the rebuild copies an explicit column
	// list, and an omission there is silent data loss.
	for _, c := range []struct{ name, got, want string }{
		{"forwarder_name", fwdName, "qrz"},
		{"forwarder_type", fwdType, "qrz"},
		{"action", act, "update"},
		{"status", st, "uploaded"},
		{"last_error", lastErr, "boom"},
		{"upstream_id", upstream, "logid-9"},
	} {
		if c.got != c.want {
			t.Errorf("down migration lost %s: got %q, want %q", c.name, c.got, c.want)
		}
	}
	if gotID != id {
		t.Errorf("down migration changed id: got %d, want %d", gotID, id)
	}
	if gotQsoID != 1 {
		t.Errorf("down migration lost qso_id: got %d, want 1", gotQsoID)
	}
	if createdAt == "" {
		t.Error("down migration lost created_at")
	}
	if modifiedAt == "" {
		t.Error("down migration lost modified_at")
	}
	if attempts != 3 || lastAt != 111 || nextAt != 222 {
		t.Errorf("down migration lost numeric columns: attempts=%d last=%d next=%d",
			attempts, lastAt, nextAt)
	}
}

// Contract 2's re-enqueue half is NOT proven here. It was, briefly, by a test that
// embedded the intended ON CONFLICT ... origin = excluded.origin statement in the
// test body — which would have passed against any production UPSERT at all,
// including one that never touched origin. A proof of test-owned SQL is not a
// proof. It now lives in internal/api/uploads_origin_test.go, driven through two
// real producers (live logging, then a manual backfill of the same QSO), which
// also pins the shared EnqueueUploads path.

// The Go-boundary guard on origin (InsertQsoUploadTx's origin.Parse call).
//
// It exists ALONGSIDE the column CHECK, not instead of it: this one fails at the
// call site, naming both the offending VALUE and the OPERATION, before issuing
// any qso_upload SQL. (Not "before the transaction does any work" — the caller
// has usually already written the QSO row in the same tx.) The CHECK is the
// backstop for anything that reaches SQL by another route.
//
// Added after review found it untested (2026-08-01) — the schema half was pinned
// and the Go half was asserted only by the code's own comment.
func TestInsertQsoUpload_RejectsUnknownOriginAtTheGoBoundary(t *testing.T) {
	svc := testService(t)
	seedLogbookAndQsoRow(t, svc.handle)

	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()

	err = svc.InsertQsoUploadTx(ctx, tx, 1, action.Insert, "qrz", "qrz", origin.Origin("invalid"))
	if err == nil {
		t.Fatal("an unknown origin must be refused; the closed value set is the whole point of the column")
	}
	// All three, because the comment above claims all three. Without the value and
	// the operation this is no better than the bare CHECK violation the guard
	// exists to improve on — which is exactly what removing origin.Parse produces:
	//   "constraint failed: CHECK constraint failed: origin IN (…)"
	for _, want := range []string{
		"unknown upload origin",            // what went wrong
		`"invalid"`,                        // WHICH value
		"sqlite.Service.InsertQsoUploadTx", // WHICH operation
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

// The valid case, so the guard above cannot pass by refusing everything.
func TestInsertQsoUpload_AcceptsAKnownOrigin(t *testing.T) {
	svc := testService(t)
	seedLogbookAndQsoRow(t, svc.handle)

	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()

	if err := svc.InsertQsoUploadTx(ctx, tx, 1, action.Insert, "qrz", "qrz", origin.Manual); err != nil {
		_ = tx.Rollback()
		t.Fatalf("a known origin must be accepted: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
