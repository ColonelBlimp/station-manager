package sqlite

import (
	"reflect"
	"testing"
)

// Log migration 0021 (W-0029 part 3, A2): the file records the activation it
// was last arrived at under. It starts empty, as config.json's activation in
// effect does, so the two agree at the upgrade and nothing is closed by it.

// A file at 20 with an open session and a closed one comes up to 21 with every
// row as it was, and the new value empty.
func TestMigrate0021_AFileAt20KeepsItsSessionsAndRecordsNoArrival(t *testing.T) {
	svc := testServiceAtVersion(t, 20)
	seedLogbookAndQsoRow(t, svc.handle)
	execT(t, svc, `UPDATE logbook SET uuid = '01920000-0000-7000-8000-00000000000a' WHERE id = 1`)
	execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id, session_generation, default_epoch)
		VALUES (1, '01920000-0000-7000-8000-000000000001', 1, 7, 3)`)
	closedSessionRow(t, svc, "01920000-0000-7000-8000-0000000000c1")
	openSessionRow(t, svc, "01920000-0000-7000-8000-0000000000a1")

	tables := []string{"archive_metadata", "operating_session", "operating_session_interval", "operating_session_member", "operating_app_exit", "logbook", "qso"}
	columns, before := map[string][]string{}, map[string][]string{}
	for _, table := range tables {
		columns[table] = tableColumns(t, svc, table)
		before[table] = dumpColumns(t, svc, table, columns[table])
	}
	if len(before["operating_session"]) != 2 || len(before["archive_metadata"]) != 1 {
		t.Fatalf("the fixture holds %d sessions and %d identity rows; want 2 and 1", len(before["operating_session"]), len(before["archive_metadata"]))
	}

	migrateToVersion(t, svc, 21)

	if v := schemaVersion(t, svc); v != 21 {
		t.Fatalf("schema version = %d, want 21", v)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM `+schemaMigrationsTable(MigrationSetLog)+` WHERE dirty = 1`); n != 0 {
		t.Fatal("0021 left the file dirty")
	}
	for _, table := range tables {
		if after := dumpColumns(t, svc, table, columns[table]); !reflect.DeepEqual(after, before[table]) {
			t.Errorf("%s changed across 0021:\n before %v\n after  %v", table, before[table], after)
		}
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM archive_metadata WHERE arrived_activation = ''`); n != 1 {
		t.Fatalf("%d identity rows record no arrival after 0021, want 1", n)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`); n != 1 {
		t.Fatalf("%d sessions are open after 0021, want the 1 that was", n)
	}
}

// An identity row made after 0021 records no arrival either.
func TestMigrate0021_ANewIdentityRowRecordsNoArrival(t *testing.T) {
	svc := testServiceAtVersion(t, 21)
	execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid) VALUES (1, '01920000-0000-7000-8000-000000000001')`)
	if n := countT(t, svc, `SELECT COUNT(*) FROM archive_metadata WHERE arrived_activation = ''`); n != 1 {
		t.Fatalf("%d new identity rows record no arrival, want 1", n)
	}
}
