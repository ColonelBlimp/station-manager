package sqlite

import (
	"database/sql"
	"testing"
)

// Migration 0010 gives upstream_id an immutable success-order companion. Queue
// state timestamps change on re-arm, claim, retry and failure, so they cannot
// safely decide which retained remote id a later delete must use.
func TestMigrate0010_AddsUpstreamIDGenerationAtHead(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 21 {
		t.Fatalf("schema version = %d, want 21", v)
	}

	var count int
	if err := svc.handle.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('qso_upload')
		WHERE name = 'upstream_id_generation' AND type = 'INTEGER'`,
	).Scan(&count); err != nil {
		t.Fatalf("inspect qso_upload columns: %v", err)
	}
	if count != 1 {
		t.Fatalf("upstream_id_generation columns = %d, want 1 INTEGER column", count)
	}
}

func TestMigrate0010_BackfillsOnlyRecoverableSuccessOrder(t *testing.T) {
	svc := testServiceAtVersion(t, 9) // built up to 9, as that build left it
	seedLogbookAndQsoRow(t, svc.handle)

	rows := []struct {
		name, action, status, upstreamID, modifiedAt string
	}{
		{"qrz", "insert", "uploaded", "id-older", "2026-01-01 00:00:01"},
		{"qrz", "update", "uploaded", "id-newer", "2026-01-01 00:00:09"},
		// Its prior success time was destroyed by the later failure before
		// migration 0010 existed, so migration must not invent a generation.
		{"qrz-legacy", "insert", "failed", "id-retained", "2026-01-01 00:00:10"},
	}
	for _, row := range rows {
		if _, err := svc.handle.Exec(`
			INSERT INTO qso_upload
				(qso_id, forwarder_name, forwarder_type, action, status, origin,
				 upstream_id, modified_at)
			VALUES (1, ?, 'qrz', ?, ?, 'legacy', ?, ?)`,
			row.name, row.action, row.status, row.upstreamID, row.modifiedAt,
		); err != nil {
			t.Fatalf("seed %s/%s: %v", row.name, row.action, err)
		}
	}

	migrateToVersion(t, svc, 10)

	for _, tc := range []struct {
		name, action string
		want         sql.NullInt64
	}{
		{"qrz", "insert", sql.NullInt64{Int64: 1, Valid: true}},
		{"qrz", "update", sql.NullInt64{Int64: 2, Valid: true}},
		{"qrz-legacy", "insert", sql.NullInt64{}},
	} {
		var got sql.NullInt64
		if err := svc.handle.QueryRow(`
			SELECT upstream_id_generation FROM qso_upload
			WHERE forwarder_name=? AND action=?`, tc.name, tc.action).Scan(&got); err != nil {
			t.Fatalf("read %s/%s generation: %v", tc.name, tc.action, err)
		}
		if got != tc.want {
			t.Errorf("%s/%s generation = %+v, want %+v", tc.name, tc.action, got, tc.want)
		}
	}
}
