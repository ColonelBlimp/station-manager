package sqlite

import (
	"database/sql"
	"testing"
)

// Migration 0011 gives a failed row a durable, typed reason class so recovery
// can re-arm exactly the rows a corrected credential stranded, without parsing
// redacted provider text (W-0010 outcome 9, ruling 2026-09-20 (c)).
func TestMigrate0011_AddsFailureClassAtHead(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 21 {
		t.Fatalf("schema version = %d, want 21", v)
	}

	var count int
	if err := svc.handle.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('qso_upload')
		WHERE name = 'failure_class' AND type = 'TEXT'`,
	).Scan(&count); err != nil {
		t.Fatalf("inspect qso_upload columns: %v", err)
	}
	if count != 1 {
		t.Fatalf("failure_class columns = %d, want 1 TEXT column", count)
	}
}

// Ruling (d): existing rows — the preserved 2026-08-06 QRZ fixture included —
// stay NULL. The migration must not infer a class from last_error text, however
// recognisable the prefix.
func TestMigrate0011_LeavesExistingFailedRowsUnclassified(t *testing.T) {
	svc := testServiceAtVersion(t, 10) // built up to 10, as that build left it
	seedLogbookAndQsoRow(t, svc.handle)
	if _, err := svc.handle.Exec(`
		INSERT INTO qso_upload
			(qso_id, forwarder_name, forwarder_type, action, status, origin, last_error)
		VALUES (1, 'qrz', 'qrz', 'insert', 'failed', 'live',
		        'qrz.classifyResponse: QRZ authentication rejected: invalid api key [REDACTED]')`,
	); err != nil {
		t.Fatalf("seed pre-0011 failed row: %v", err)
	}

	migrateToVersion(t, svc, 11)

	var got sql.NullString
	if err := svc.handle.QueryRow(`SELECT failure_class FROM qso_upload WHERE forwarder_name='qrz'`).Scan(&got); err != nil {
		t.Fatalf("read failure_class: %v", err)
	}
	if got.Valid {
		t.Fatalf("failure_class after up = %q, want NULL (no backfill from last_error)", got.String)
	}
}

func TestMigrate0011_RejectsUnknownClass(t *testing.T) {
	svc := testService(t)
	seedLogbookAndQsoRow(t, svc.handle)
	_, err := svc.handle.Exec(`
		INSERT INTO qso_upload
			(qso_id, forwarder_name, forwarder_type, action, status, origin, failure_class)
		VALUES (1, 'qrz', 'qrz', 'insert', 'failed', 'live', 'rejected')`)
	if err == nil {
		t.Fatal("insert with failure_class='rejected' succeeded, want CHECK violation")
	}
}
