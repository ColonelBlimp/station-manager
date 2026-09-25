package sqlite

import (
	"fmt"
	"testing"
)

// Migration 0014 (ADR 0082, W-0021 5B): `logbook_destination` — one binding per
// (logical logbook × destination type) — and the one-time adoption marker
// `archive_metadata.destination_bindings_seeded_at`. The down step first
// collapses UUID-derived queue names back to the destination's default-logbook
// binding name (else the lexicographically first), then drops the table and
// the marker; QSO, queue and logbook rows are otherwise untouched.

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

func TestMigrate0014_BindingTableAndMarker_Up(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 14 {
		t.Fatalf("schema version = %d, want 14", v)
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

func TestMigrate0014_Down_CollapsesQueueNamesToTheDefaultLogbookBinding(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookQueue(t, svc, 1)
	if _, err := svc.DowngradeLogSchemaTo(13); err != nil {
		t.Fatalf("downgrade to 13: %v", err)
	}
	if v := schemaVersion(t, svc); v != 13 {
		t.Fatalf("schema version = %d after the down step, want 13", v)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'logbook_destination'`); n != 0 {
		t.Fatal("logbook_destination survived the down step")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM pragma_table_info('archive_metadata') WHERE name = 'destination_bindings_seeded_at'`); n != 0 {
		t.Fatal("destination_bindings_seeded_at survived the down step")
	}
	// Both queue rows now carry the legacy name; nothing else about them moved.
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso_upload WHERE forwarder_name = 'qrz'`); n != 2 {
		t.Fatalf("rows named qrz after the down step = %d, want 2", n)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso_upload WHERE qso_id = 2 AND forwarder_name = 'qrz' AND status = 'pending' AND origin = 'live'`); n != 1 {
		t.Fatal("the second logbook's row was not collapsed to qrz with its status and origin intact")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso`); n != 2 {
		t.Fatalf("qso rows = %d, want 2", n)
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

func namesOf(t *testing.T, svc *Service) (name1, name2 string) {
	t.Helper()
	if err := svc.handle.QueryRow(`SELECT forwarder_name FROM qso_upload WHERE qso_id = 1`).Scan(&name1); err != nil {
		t.Fatal(err)
	}
	if err := svc.handle.QueryRow(`SELECT forwarder_name FROM qso_upload WHERE qso_id = 2`).Scan(&name2); err != nil {
		t.Fatal(err)
	}
	return name1, name2
}

// The default logbook's binding name wins even when it is NOT the
// lexicographic minimum: default = logbook 2 named "qrz.z", logbook 1 named
// "qrz.a" → both rows end as "qrz.z".
// Codex P2 on c3df0e12: the legacy config name is the ONLY name an older,
// config-driven worker drains, and neither the current default logbook nor
// sort order identifies it. The seed records it on every row it creates
// (`legacy_name`); the down step collapses to that first.
func TestMigrate0014_Down_CollapsesToTheRecordedLegacyNameBeforeAnyInference(t *testing.T) {
	// No default logbook; the legacy name "station-qrz" sorts AFTER "qrz.<uuid>".
	svc := testService(t)
	seedNamedBindings(t, svc, nil, "station-qrz", "qrz.01920000-0000-7000-8000-00000000000b")
	execT(t, svc, `UPDATE logbook_destination SET legacy_name = 'station-qrz'`)
	if _, err := svc.DowngradeLogSchemaTo(13); err != nil {
		t.Fatalf("downgrade to 13: %v", err)
	}
	if n1, n2 := namesOf(t, svc); n1 != "station-qrz" || n2 != "station-qrz" {
		t.Fatalf("names after the down step = %q, %q; want the recorded legacy name for both", n1, n2)
	}
}

// The default logbook CHANGED after the seed: logbook 2 is now the default,
// yet the legacy name lives on logbook 1's row — the recorded name still wins.
func TestMigrate0014_Down_RecordedLegacyNameWinsOverAChangedDefault(t *testing.T) {
	svc := testService(t)
	seedNamedBindings(t, svc, 2, "qrz", "qrz.01920000-0000-7000-8000-00000000000b")
	execT(t, svc, `UPDATE logbook_destination SET legacy_name = 'qrz'`)
	if _, err := svc.DowngradeLogSchemaTo(13); err != nil {
		t.Fatalf("downgrade to 13: %v", err)
	}
	if n1, n2 := namesOf(t, svc); n1 != "qrz" || n2 != "qrz" {
		t.Fatalf("names after the down step = %q, %q; want the recorded legacy name, not the new default's", n1, n2)
	}
}

// Bindings that never had a legacy name (created after the seed, or in a new
// archive) fall back to the ADR 0082 rule: the default logbook's binding name.
func TestMigrate0014_Down_DefaultLogbookBindingWinsOverTheLexicographicMinimum(t *testing.T) {
	svc := testService(t)
	seedNamedBindings(t, svc, 2, "qrz.a", "qrz.z")
	if _, err := svc.DowngradeLogSchemaTo(13); err != nil {
		t.Fatalf("downgrade to 13: %v", err)
	}
	if n1, n2 := namesOf(t, svc); n1 != "qrz.z" || n2 != "qrz.z" {
		t.Fatalf("names after the down step = %q, %q; want the default logbook's qrz.z for both", n1, n2)
	}
}

// With no default-logbook binding the target is the lexicographically first
// binding name — here logbook 2's "qrz.a", NOT the lowest logbook's "qrz.b".
func TestMigrate0014_Down_CollapsesToTheFirstNameWithoutADefaultBinding(t *testing.T) {
	svc := testService(t)
	seedNamedBindings(t, svc, nil, "qrz.b", "qrz.a")
	if _, err := svc.DowngradeLogSchemaTo(13); err != nil {
		t.Fatalf("downgrade to 13: %v", err)
	}
	if n1, n2 := namesOf(t, svc); n1 != "qrz.a" || n2 != "qrz.a" {
		t.Fatalf("names after the down step = %q, %q; want the lexicographically first qrz.a for both", n1, n2)
	}
}
