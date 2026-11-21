package sqlite

import (
	"context"
	"fmt"
	"testing"
)

// Migrations 0014 + 0015 (ADR 0082, W-0021 5B): `logbook_destination` — one
// binding per (logical logbook × destination type) — the one-time adoption
// marker `archive_metadata.destination_bindings_seeded_at`, and (0015) the
// `legacy_name` a seeded binding derives from. The column existed for the way
// down, which collapsed queue names to it; the way down was removed (W-0027,
// 2026-11-19) and the column and its writes stay. A file at 14, in either of
// the two shapes a build left, reaches the head with its bindings.

func execT(t *testing.T, svc *Service, q string, args ...any) {
	t.Helper()
	if _, err := svc.handle.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func countT(t *testing.T, svc *Service, q string, args ...any) int {
	t.Helper()
	var n int
	if err := svc.handle.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// seedTwoLogbookQueue plants two logbooks (1 = the file's default, 2 = a second
// callsign), one QSO in each, a qrz binding on each — the default keeps the
// legacy name `qrz`, the second is `qrz.<uuid>` — and one queue row per QSO
// under its logbook's binding name.
func seedTwoLogbookQueue(t *testing.T, svc *Service, defaultLogbook any) {
	t.Helper()
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (1, '01920000-0000-7000-8000-00000000000a', 'M0ABC', 'Main')`)
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (2, '01920000-0000-7000-8000-00000000000b', 'M0XYZ', 'Second')`)
	execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, '01920000-0000-7000-8000-000000000001', ?)`, defaultLogbook)
	for id, lb := range map[int64]int64{1: 1, 2: 2} {
		execT(t, svc, `INSERT INTO qso (id, uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?, ?, 'K1AAA', '20m', 'SSB', 14074000, '20260101', '1200', '1200', '59', '59', 'Test', ?, ?)`,
			id, "01920000-0000-7000-8000-0000000000"+map[int64]string{1: "10", 2: "20"}[id], fmt.Sprintf("%064d", id), lb)
	}
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (1, 'qrz', 'qrz', 1, '{"api_key":"k1"}')`)
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (2, 'qrz', 'qrz.01920000-0000-7000-8000-00000000000b', 1, '{"api_key":"k2"}')`)
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (1, 'qrz', 'qrz', 'insert', 'uploaded', 'live')`)
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (2, 'qrz.01920000-0000-7000-8000-00000000000b', 'qrz', 'insert', 'pending', 'live')`)
}

func TestMigrate0014And0015_BindingTableMarkerAndLegacyName_Up(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 21 {
		t.Fatalf("schema version = %d, want 21", v)
	}
	seedTwoLogbookQueue(t, svc, 1)
	// One binding per (logbook, destination); forwarder_name unique file-wide.
	if _, err := svc.handle.Exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name) VALUES (1, 'qrz', 'other')`); err == nil {
		t.Error("a second qrz binding on logbook 1 must violate UNIQUE (logbook_id, destination)")
	}
	if _, err := svc.handle.Exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name) VALUES (2, 'clublog', 'qrz')`); err == nil {
		t.Error("reusing forwarder_name 'qrz' must violate its UNIQUE constraint")
	}
	if _, err := svc.handle.Exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled) VALUES (2, 'clublog', 'cl', 2)`); err == nil {
		t.Error("enabled outside {0,1} must violate its CHECK")
	}
	// The marker is NULL until a seed decision is recorded; a new row defaults to disabled.
	if n := countT(t, svc, `SELECT COUNT(*) FROM archive_metadata WHERE destination_bindings_seeded_at IS NULL`); n != 1 {
		t.Fatalf("marker rows NULL = %d, want 1", n)
	}
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name) VALUES (2, 'smcloud', 'smcloud.x')`)
	if n := countT(t, svc, `SELECT enabled FROM logbook_destination WHERE forwarder_name = 'smcloud.x'`); n != 0 {
		t.Fatalf("default enabled = %d, want 0", n)
	}
	// Deleting a logbook cascades its bindings.
	execT(t, svc, `DELETE FROM qso WHERE logbook_id = 2`)
	execT(t, svc, `DELETE FROM logbook WHERE id = 2`)
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE logbook_id = 2`); n != 0 {
		t.Fatalf("bindings of the deleted logbook = %d, want 0 (cascade)", n)
	}
}

// seedNamedBindings: two logbooks (1, 2), one QSO each, a qrz binding per
// logbook under the given names, and one queue row per QSO under its binding.
func seedNamedBindings(t *testing.T, svc *Service, defaultLogbook any, name1, name2 string) {
	t.Helper()
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (1, '01920000-0000-7000-8000-00000000000a', 'M0ABC', 'Main')`)
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (2, '01920000-0000-7000-8000-00000000000b', 'M0XYZ', 'Second')`)
	execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, '01920000-0000-7000-8000-000000000001', ?)`, defaultLogbook)
	for id, lb := range map[int64]int64{1: 1, 2: 2} {
		execT(t, svc, `INSERT INTO qso (id, uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?, ?, 'K1AAA', '20m', 'SSB', 14074000, '20260101', '1200', '1200', '59', '59', 'Test', ?, ?)`,
			id, "01920000-0000-7000-8000-0000000000"+map[int64]string{1: "10", 2: "20"}[id], fmt.Sprintf("%064d", id), lb)
	}
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled) VALUES (1, 'qrz', ?, 1)`, name1)
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled) VALUES (2, 'qrz', ?, 1)`, name2)
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (1, ?, 'qrz', 'insert', 'uploaded', 'live')`, name1)
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (2, ?, 'qrz', 'insert', 'pending', 'live')`, name2)
}

// Upgrade path (Codex P2 on b1231672): a file the 0014 build migrated — table
// without legacy_name, marker present — reaches the head through the ordinary
// Migrate() and gains the column; bindings it already holds list with an empty
// LegacyName. The file is built up to 14, as that build left it.
func TestMigrate0015_UpgradesAFileAlreadyAt14(t *testing.T) {
	svc := testServiceAtVersion(t, 14)
	if n := countT(t, svc, `SELECT COUNT(*) FROM pragma_table_info('logbook_destination') WHERE name = 'legacy_name'`); n != 0 {
		t.Fatal("the fixture at 14 already has legacy_name; it is not the 0014 build's shape")
	}
	seedNamedBindings(t, svc, 1, "qrz", "qrz.01920000-0000-7000-8000-00000000000b") // the parent's shape, no legacy_name
	if err := svc.Migrate(); err != nil {
		t.Fatalf("migrate up from 14: %v", err)
	}
	if v := schemaVersion(t, svc); v != 21 {
		t.Fatalf("schema version = %d, want 21", v)
	}
	rows, err := svc.ListLogbookDestinationsWithContext(context.Background())
	if err != nil || len(rows) != 2 || rows[0].LegacyName != "" {
		t.Fatalf("rows after the upgrade = %+v (%v); want 2 with no legacy name", rows, err)
	}
}

// Upgrade path, second shape (Codex P1 on ea56170f): a file migrated by the
// b1231672 build reports 14 but already HAS legacy_name (that build's 0014
// carried it). 0015 must reach the same final shape from either 14 variant, so
// it rebuilds the table instead of adding the column; rows survive, and the
// column's content from that shape is not carried (no real file of that shape
// holds seeded bindings — that build wired no seed).
func TestMigrate0015_UpgradesAFileAt14ThatAlreadyHasTheColumn(t *testing.T) {
	svc := testServiceAtVersion(t, 14)
	execT(t, svc, `ALTER TABLE logbook_destination ADD COLUMN legacy_name TEXT`) // the b1231672 shape
	seedNamedBindings(t, svc, 1, "qrz", "qrz.01920000-0000-7000-8000-00000000000b")
	execT(t, svc, `UPDATE logbook_destination SET legacy_name = 'qrz'`)
	// Advance the AUTOINCREMENT sequence beyond the surviving maximum: a row
	// with id 9, deleted again, leaves seq = 9 with MAX(id) = 2.
	execT(t, svc, `INSERT INTO logbook_destination (id, logbook_id, destination, forwarder_name) VALUES (9, 2, 'clublog', 'gone')`)
	execT(t, svc, `DELETE FROM logbook_destination WHERE id = 9`)
	if err := svc.Migrate(); err != nil {
		t.Fatalf("migrate up from the column-bearing 14: %v", err)
	}
	if v := schemaVersion(t, svc); v != 21 {
		t.Fatalf("schema version = %d, want 21 (the head)", v)
	}
	rows, err := svc.ListLogbookDestinationsWithContext(context.Background())
	if err != nil || len(rows) != 2 || rows[0].ForwarderName != "qrz" || rows[1].ForwarderName != "qrz.01920000-0000-7000-8000-00000000000b" {
		t.Fatalf("rows after the upgrade = %+v (%v); want both bindings kept", rows, err)
	}
	// The constraints of the rebuilt table hold.
	if _, err := svc.handle.Exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name) VALUES (1, 'qrz', 'other')`); err == nil {
		t.Error("UNIQUE (logbook_id, destination) lost in the rebuild")
	}
	// The AUTOINCREMENT high-water mark survives the rebuild: the sequence was
	// advanced past the surviving rows before the migration (see the setup
	// above), so the next insert must continue from it, not from MAX(id).
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name) VALUES (1, 'clublog', 'clublog')`)
	if n := countT(t, svc, `SELECT id FROM logbook_destination WHERE forwarder_name = 'clublog'`); n != 10 {
		t.Fatalf("id of the first insert after the rebuild = %d, want 10 (high-water mark 9 carried)", n)
	}
}
