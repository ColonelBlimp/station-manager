package sqlite

import (
	"database/sql"
	"testing"
)

// Migration 0012 (ADR 0071, W-0021 slice 1, rulings (a)/(b) 2026-09-22): the
// QSO file becomes self-identifying. `archive_metadata` is a singleton row —
// the archive's immutable UUID, its creation time and its database-local
// default logbook — and `logbook.uuid` gives each logical logbook a stable
// identity for cross-file and cloud use. Both UUID columns are nullable at the
// SQL level because SQLite cannot mint a UUIDv7: the daemon fills them, once,
// after Migrate() (and mints on every runtime insert). A file an older build
// left crosses the migration without a logbook or QSO row touched.

func TestMigrate0012_ArchiveMetadataAndLogbookUUIDAtHead(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 21 {
		t.Fatalf("schema version = %d, want 21", v)
	}
	var cols int
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('archive_metadata')
		WHERE name IN ('singleton','archive_uuid','created_at','default_logbook_id')`).Scan(&cols); err != nil {
		t.Fatalf("inspect archive_metadata: %v", err)
	}
	if cols != 4 {
		t.Fatalf("archive_metadata has %d of the 4 expected columns", cols)
	}
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('logbook') WHERE name='uuid' AND type='TEXT'`).Scan(&cols); err != nil || cols != 1 {
		t.Fatalf("logbook.uuid TEXT columns = %d (%v), want 1", cols, err)
	}
	// Fresh file: no identity yet — the daemon writes it, the migration never does.
	var rows int
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM archive_metadata`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("archive_metadata rows after migration = %d (%v), want 0 (identity is minted by the daemon)", rows, err)
	}
}

// The metadata row is a singleton: exactly one archive identity per file.
func TestMigrate0012_ArchiveMetadataIsASingleton(t *testing.T) {
	svc := testService(t)
	const a = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	const b = "019fd5c5-efcc-7193-be4f-1fee532ee316"
	if _, err := svc.handle.Exec(`INSERT INTO archive_metadata (singleton, archive_uuid) VALUES (1, ?)`, a); err != nil {
		t.Fatalf("first identity row: %v", err)
	}
	if _, err := svc.handle.Exec(`INSERT INTO archive_metadata (singleton, archive_uuid) VALUES (2, ?)`, b); err == nil {
		t.Fatal("a second identity row (singleton=2) was accepted; the CHECK must pin singleton = 1")
	}
	if _, err := svc.handle.Exec(`INSERT INTO archive_metadata (singleton, archive_uuid) VALUES (1, ?)`, b); err == nil {
		t.Fatal("a second identity row (singleton=1) was accepted; the primary key must refuse it")
	}
	if _, err := svc.handle.Exec(`INSERT INTO archive_metadata (singleton, archive_uuid) VALUES (1, 'short')`); err == nil {
		t.Fatal("a malformed archive_uuid was accepted")
	}
}

// logbook.uuid: unique when present, NULL allowed (the pre-backfill state and
// a row inserted by an older build before the daemon's next start).
func TestMigrate0012_LogbookUUIDUniqueWhenPresentNullAllowed(t *testing.T) {
	svc := testService(t)
	const u = "019fd5c5-efcc-7193-be4f-1fee532ee317"
	for i, stmt := range []string{
		`INSERT INTO logbook (id, callsign, name) VALUES (1,'G4ABC','A')`,
		`INSERT INTO logbook (id, callsign, name) VALUES (2,'G4ABC','B')`,
		`INSERT INTO logbook (id, callsign, name, uuid) VALUES (3,'G4ABC','C','` + u + `')`,
	} {
		if _, err := svc.handle.Exec(stmt); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if _, err := svc.handle.Exec(`INSERT INTO logbook (id, callsign, name, uuid) VALUES (4,'G4ABC','D','` + u + `')`); err == nil {
		t.Fatal("a duplicate logbook uuid was accepted")
	}
	var nulls int
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM logbook WHERE uuid IS NULL`).Scan(&nulls); err != nil || nulls != 2 {
		t.Fatalf("NULL uuids = %d (%v), want 2", nulls, err)
	}
}

// A file an older build left at 11, holding a logbook and a QSO, crosses 0012
// with both rows as they were: the identity table arrives empty and the
// logbook's uuid is NULL until the daemon's backfill. The file is built up to
// 11, as that build left it.
func TestMigrate0012_AFileAt11KeepsItsLogbooksAndQsos(t *testing.T) {
	svc := testServiceAtVersion(t, 11)
	var n int
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='archive_metadata'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("archive_metadata tables at 11 = %d (%v), want 0: not the older shape", n, err)
	}
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('logbook') WHERE name='uuid'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("logbook.uuid columns at 11 = %d (%v), want 0: not the older shape", n, err)
	}
	seedLogbookAndQsoRow(t, svc.handle)

	for _, step := range []struct {
		when string
		up   func()
	}{
		{"after 0012", func() { migrateToVersion(t, svc, 12) }},
		{"at the head", func() {
			if err := svc.Migrate(); err != nil {
				t.Fatalf("migrate to the head: %v", err)
			}
		}},
	} {
		step.up()
		if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM archive_metadata`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: identity rows = %d (%v), want 0 (the migration invents none)", step.when, n, err)
		}
		var lb, q int
		if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM logbook`).Scan(&lb); err != nil || lb != 1 {
			t.Fatalf("%s: logbook rows = %d (%v), want 1", step.when, lb, err)
		}
		if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM qso WHERE call = 'G4ABC' AND logbook_id = 1`).Scan(&q); err != nil || q != 1 {
			t.Fatalf("%s: qso rows = %d (%v), want 1", step.when, q, err)
		}
		var name, uuid sql.NullString
		if err := svc.handle.QueryRow(`SELECT name, uuid FROM logbook WHERE id = 1`).Scan(&name, &uuid); err != nil || name.String != "L" || uuid.Valid {
			t.Fatalf("%s: logbook row = name %v, uuid %v (%v); want L with no uuid", step.when, name, uuid, err)
		}
	}
}
