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
	// A file built up to v6, as that build left it: insert a row as v6 would
	// have (no origin column), then go up through 0007.
	svc := testServiceAtVersion(t, 6)
	db := svc.handle
	seedLogbookAndQsoRow(t, db)
	if _, err := db.Exec(`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status)
		VALUES (1, 'qrz', 'qrz', 'insert', 'pending')`); err != nil {
		t.Fatalf("seed pre-0007 upload row: %v", err)
	}
	migrateToVersion(t, svc, 7)

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

// Both partial indexes must exist before and after the rebuild. A rebuild that
// renames the table without dropping them first leaves them attached to
// qso_upload_old, where DROP TABLE takes them with it — silently, and only
// visible as a slow claim query under load. Checked on a file built up to v6,
// straight after 0007, and at the head.
func TestMigrate0007_PartialIndexesSurviveTheRebuild(t *testing.T) {
	svc := testServiceAtVersion(t, 6)

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

	assertIndexes("at 6, before the rebuild")
	migrateToVersion(t, svc, 7)
	assertIndexes("after 0007")
	if err := svc.Migrate(); err != nil {
		t.Fatalf("migrate to the head: %v", err)
	}
	if v := schemaVersion(t, svc); v != 20 {
		t.Fatalf("schema version = %d, want 20", v)
	}
	assertIndexes("at the head")
}

// A rename-then-rebuild is where foreign keys are silently orphaned. Assert the
// database agrees they are intact with a queue row in place: on a file built up
// to v6, straight after 0007, and at the head.
func TestMigrate0007_ForeignKeysIntactAcrossTheRebuild(t *testing.T) {
	svc := testServiceAtVersion(t, 6)
	seedLogbookAndQsoRow(t, svc.handle)
	if _, err := svc.handle.Exec(`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status)
		VALUES (1, 'qrz', 'stub', 'insert', 'pending')`); err != nil { // v6's shape: no origin column
		t.Fatalf("seed pre-0007 upload row: %v", err)
	}

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
		var n int
		if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM qso_upload u JOIN qso q ON q.id = u.qso_id`).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: queue rows joined to their QSO = %d (%v), want 1", when, n, err)
		}
	}

	check("at 6, before the rebuild")
	migrateToVersion(t, svc, 7)
	check("after 0007")
	if err := svc.Migrate(); err != nil {
		t.Fatalf("migrate to the head: %v", err)
	}
	if v := schemaVersion(t, svc); v != 20 {
		t.Fatalf("schema version = %d, want 20", v)
	}
	check("at the head")
}

// The rebuild must preserve every pre-existing column. It copies an explicit
// column list, which is exactly where a column is silently dropped. Two rows on
// a file built up to v6: one with every column set to a value of its own, and
// one with every nullable column NULL, so a column filled from another, from a
// default or from nothing shows.
func TestMigrate0007_UpPreservesEveryPreExistingColumn(t *testing.T) {
	svc := testServiceAtVersion(t, 6)
	db := svc.handle
	seedLogbookAndQsoRow(t, db)
	if _, err := db.Exec(`INSERT INTO qso_upload
		(id, created_at, modified_at, qso_id, forwarder_name, forwarder_type, action, status,
		 attempts, last_attempt_at, next_attempt_at, last_error, upstream_id)
		VALUES (41, '2026-01-02 03:04:05', '2026-02-03 04:05:06', 1, 'qrz', 'qrz-type', 'update', 'uploaded',
		        3, 111, 222, 'boom', 'logid-9'),
		       (42, '2026-03-04 05:06:07', NULL, 1, 'clublog', 'clublog-type', 'delete', 'failed',
		        0, NULL, 333, NULL, NULL)`); err != nil {
		t.Fatalf("seed pre-0007 upload rows: %v", err)
	}

	migrateToVersion(t, svc, 7)

	type row struct {
		createdAt, fwdName, fwdType, act, st, origin string
		modifiedAt, lastErr, upstream                sql.NullString
		qsoID, attempts, nextAt                      int64
		lastAt                                       sql.NullInt64
	}
	str := func(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }
	for id, want := range map[int64]row{
		41: {"2026-01-02 03:04:05", "qrz", "qrz-type", "update", "uploaded", "legacy",
			str("2026-02-03 04:05:06"), str("boom"), str("logid-9"), 1, 3, 222, sql.NullInt64{Int64: 111, Valid: true}},
		42: {"2026-03-04 05:06:07", "clublog", "clublog-type", "delete", "failed", "legacy",
			sql.NullString{}, sql.NullString{}, sql.NullString{}, 1, 0, 333, sql.NullInt64{}},
	} {
		var got row
		if err := db.QueryRow(`SELECT strftime('%Y-%m-%d %H:%M:%S', created_at), forwarder_name, forwarder_type,
			action, status, origin, strftime('%Y-%m-%d %H:%M:%S', modified_at), last_error, upstream_id,
			qso_id, attempts, next_attempt_at, last_attempt_at
			FROM qso_upload WHERE id = ?`, id).
			Scan(&got.createdAt, &got.fwdName, &got.fwdType, &got.act, &got.st, &got.origin,
				&got.modifiedAt, &got.lastErr, &got.upstream, &got.qsoID, &got.attempts, &got.nextAt, &got.lastAt); err != nil {
			t.Fatalf("read row %d back after 0007: %v", id, err)
		}
		if got != want {
			t.Errorf("row %d after 0007:\n   got %+v\n  want %+v", id, got, want)
		}
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso_upload`); n != 2 {
		t.Errorf("queue rows after 0007 = %d, want 2", n)
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
