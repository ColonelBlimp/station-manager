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
	if v := schemaVersion(t, svc); v != 14 {
		t.Fatalf("schema version = %d, want 14", v)
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
	svc := testService(t)
	migrateToVersion(t, svc, 9)
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

func TestMigrate0010_DownDropsGenerationWithoutLosingQueueRows(t *testing.T) {
	svc := testService(t)
	seedLogbookAndQsoRow(t, svc.handle)
	if _, err := svc.handle.Exec(`
		INSERT INTO qso_upload
			(qso_id, forwarder_name, forwarder_type, action, status, origin,
			 upstream_id, upstream_id_generation)
		VALUES (1, 'qrz', 'qrz', 'insert', 'uploaded', 'live', 'logid-1', 1)`); err != nil {
		t.Fatalf("seed qso_upload: %v", err)
	}

	migrateToVersion(t, svc, 9)

	var columns, rows int
	if err := svc.handle.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('qso_upload')
		WHERE name='upstream_id_generation'`).Scan(&columns); err != nil {
		t.Fatalf("inspect columns after down: %v", err)
	}
	if columns != 0 {
		t.Errorf("upstream_id_generation columns after down = %d, want 0", columns)
	}
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM qso_upload WHERE upstream_id='logid-1'`).Scan(&rows); err != nil {
		t.Fatalf("count queue rows after down: %v", err)
	}
	if rows != 1 {
		t.Errorf("queue rows after down = %d, want 1", rows)
	}
}
