package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Log migration 0020 adds the operating session's record (ADR 0100; W-0029
// part 1, groups A): the session, its intervals, its members, the recorded
// exits, and two counters on the file's identity row. It writes no row.

const (
	testSessionA = "01920000-0000-7000-8000-0000000000a1"
	testSessionB = "01920000-0000-7000-8000-0000000000b2"
)

var operatingSessionTables = []string{
	"operating_session", "operating_session_interval", "operating_session_member", "operating_app_exit",
}

// tableColumns lists a table's columns in their declared order.
func tableColumns(t *testing.T, svc *Service, table string) []string {
	t.Helper()
	rows, err := svc.handle.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("columns of %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil || len(cols) == 0 {
		t.Fatalf("columns of %s: %v (found %d)", table, err, len(cols))
	}
	return cols
}

// dumpColumns reads the named columns of every row, in rowid order, as text.
func dumpColumns(t *testing.T, svc *Service, table string, cols []string) []string {
	t.Helper()
	return dumpQuery(t, svc, `SELECT `+strings.Join(cols, ", ")+` FROM `+table+` ORDER BY rowid`)
}

// dumpQuery reads every row a query returns, in its order, as text.
func dumpQuery(t *testing.T, svc *Service, query string) []string {
	t.Helper()
	rows, err := svc.handle.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		out = append(out, fmt.Sprintf("%#v", vals))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// A file at 19 holding every kind of row the station's file holds comes up to
// 20 with all of them as they were. The columns compared are the ones the file
// had at 19, so the two new counters do not hide a changed row.
func TestMigrate0020_AFileAt19KeepsItsRows(t *testing.T) {
	svc := testServiceAtVersion(t, 19)
	seedLogbookAndQsoRow(t, svc.handle)
	execT(t, svc, `UPDATE logbook SET uuid = '01920000-0000-7000-8000-00000000000a' WHERE id = 1`)
	execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, '01920000-0000-7000-8000-000000000001', 1)`)
	execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (1, 'qrz', 'qrz', 'insert', 'pending', 'live')`)
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (1, 'qrz', 'qrz', 1, '{"api_key":"k1"}')`)
	execT(t, svc, `INSERT INTO contest_serial_allocation (logbook_id, contest_id, occurrence, serial, draft) VALUES (1, 'RSGB-AFS', '20261107', 7, 'draft-1')`)
	execT(t, svc, `INSERT INTO operator_event (category, kind, severity, build, detail) VALUES ('notification', 'forward.failed', 'warn', 'test', '{}')`)

	tables := []string{"logbook", "qso", "qso_upload", "logbook_destination", "contest_serial_allocation", "operator_event", "archive_metadata"}
	columns := map[string][]string{}
	before := map[string][]string{}
	for _, table := range tables {
		columns[table] = tableColumns(t, svc, table)
		before[table] = dumpColumns(t, svc, table, columns[table])
		if len(before[table]) == 0 {
			t.Fatalf("the fixture holds no %s row; the comparison would prove nothing", table)
		}
	}

	migrateToVersion(t, svc, 20)

	if v := schemaVersion(t, svc); v != 20 {
		t.Fatalf("schema version = %d, want 20", v)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM `+schemaMigrationsTable(MigrationSetLog)+` WHERE dirty = 1`); n != 0 {
		t.Fatal("0020 left the file dirty")
	}
	for _, table := range tables {
		if after := dumpColumns(t, svc, table, columns[table]); !reflect.DeepEqual(after, before[table]) {
			t.Errorf("%s changed across 0020:\n before %v\n after  %v", table, before[table], after)
		}
	}
}

// After 0020 a file holds no session of any kind and both counters read 0,
// on an identity row that was there before and on one made afterwards.
// Existing contacts are in no session (ADR 0100 O6: no backfill).
func TestMigrate0020_NoSessionExistsAndBothCountersStartAtZero(t *testing.T) {
	counters := func(t *testing.T, svc *Service) (generation, epoch int) {
		t.Helper()
		if err := svc.handle.QueryRow(`SELECT session_generation, default_epoch FROM archive_metadata WHERE singleton = 1`).
			Scan(&generation, &epoch); err != nil {
			t.Fatalf("read the counters: %v", err)
		}
		return generation, epoch
	}
	empty := func(t *testing.T, svc *Service) {
		t.Helper()
		for _, table := range operatingSessionTables {
			if n := countT(t, svc, `SELECT COUNT(*) FROM `+table); n != 0 {
				t.Errorf("%s holds %d row(s) after the migration, want none", table, n)
			}
		}
	}
	t.Run("an identity row from before the migration", func(t *testing.T) {
		svc := testServiceAtVersion(t, 19)
		seedLogbookAndQsoRow(t, svc.handle)
		execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, '01920000-0000-7000-8000-000000000001', 1)`)
		migrateToVersion(t, svc, 20)
		empty(t, svc)
		if g, e := counters(t, svc); g != 0 || e != 0 {
			t.Fatalf("generation %d, epoch %d; want 0 and 0", g, e)
		}
	})
	// The two ways an identity row is made after the migration: filled once at
	// adoption, and written with a chosen id by the provisioner. Each case is
	// its own subtest: the in-memory services of one test share one database
	// until that test ends.
	makers := map[string]func(*Service, int64) (ArchiveIdentity, error){
		"ensured": func(svc *Service, def int64) (ArchiveIdentity, error) {
			return svc.EnsureArchiveIdentityWithContext(context.Background(), def)
		},
		"written with a chosen id": func(svc *Service, def int64) (ArchiveIdentity, error) {
			return svc.WriteArchiveIdentityWithContext(context.Background(), "01920000-0000-7000-8000-000000000001", def)
		},
	}
	for name, mk := range makers {
		for _, def := range []int64{0, 1} {
			t.Run(fmt.Sprintf("an identity row made after it, %s, with pointer %d", name, def), func(t *testing.T) {
				svc := testService(t)
				if def != 0 {
					seedLogbookAndQsoRow(t, svc.handle)
				}
				id, err := mk(svc, def)
				if err != nil {
					t.Fatalf("make the identity: %v", err)
				}
				if id.DefaultLogbookID != def {
					t.Fatalf("the fixture's pointer is %d, want %d", id.DefaultLogbookID, def)
				}
				empty(t, svc)
				if g, e := counters(t, svc); g != 0 || e != 0 {
					t.Fatalf("generation %d, epoch %d; want 0 and 0 (creating the row is not a change)", g, e)
				}
			})
		}
	}
}

// A file built from version 1 and a file brought up from 19 with rows in it
// hold the same session tables and the same identity columns.
func TestMigrate0020_SameShapeFromVersion1AndFrom19(t *testing.T) {
	shape := func(t *testing.T, svc *Service) []string {
		t.Helper()
		out := dumpQuery(t, svc, `SELECT type, name, tbl_name, sql FROM sqlite_master WHERE tbl_name LIKE 'operating_%' ORDER BY name`)
		return append(out, dumpQuery(t, svc, `SELECT name, type, "notnull", dflt_value FROM pragma_table_info('archive_metadata') ORDER BY cid`)...)
	}
	// Two files, since the in-memory services of one test share one database.
	fresh, _ := testFileService(t)
	older, _ := testFileServiceWithoutMigrations(t)
	migrateToVersion(t, older, 19)
	if v := schemaVersion(t, older); v != 19 {
		t.Fatalf("the older fixture is at %d, want 19", v)
	}
	seedLogbookAndQsoRow(t, older.handle)
	if err := older.Migrate(); err != nil {
		t.Fatalf("migrate up from 19: %v", err)
	}
	a, b := shape(t, fresh), shape(t, older)
	if n := countT(t, fresh, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE 'operating_%'`); n != len(operatingSessionTables) {
		t.Fatalf("a new file holds %d session tables, want %d", n, len(operatingSessionTables))
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("the shapes differ:\n from 1:  %v\n from 19: %v", a, b)
	}
}

// refused runs a statement that the stored record must not accept, whatever
// code issues it, and wants a constraint failure of the named kind. A missing
// table is not a refusal.
func refused(t *testing.T, svc *Service, kind, q string, args ...any) {
	t.Helper()
	_, err := svc.handle.Exec(q, args...)
	if err == nil {
		t.Fatalf("accepted, want a %s constraint failure: %s", kind, q)
	}
	if !strings.Contains(err.Error(), kind+" constraint failed") {
		t.Fatalf("failed with %q, want a %s constraint failure: %s", err, kind, q)
	}
}

func openSessionRow(t *testing.T, svc *Service, id string) {
	t.Helper()
	execT(t, svc, `INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at) VALUES (?, 'lb', 0, '2026-11-20 09:15:00')`, id)
}

func closedSessionRow(t *testing.T, svc *Service, id string) {
	t.Helper()
	execT(t, svc, `INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at, closed_at, close_reason)
		VALUES (?, 'lb', 0, '2026-11-20 09:15:00', '2026-11-20 10:00:00', 'new_session')`, id)
}

// What the stored record cannot hold, whatever code writes it (group A5).
// Each refusal has an accepted neighbour, so the fixture tells them apart.
func TestOperatingSessionSchema_RefusesWhatTheRecordMustNeverHold(t *testing.T) {
	const interval = `INSERT INTO operating_session_interval (session_id, seq, started_at, ended_at, end_state, app_lifetime) VALUES (?, ?, '2026-11-20 09:15:00', ?, ?, 'L1')`

	t.Run("two open sessions", func(t *testing.T) {
		svc := testService(t)
		openSessionRow(t, svc, testSessionA)
		refused(t, svc, "UNIQUE", `INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at) VALUES (?, 'lb', 0, '2026-11-20 09:16:00')`, testSessionB)
		closedSessionRow(t, svc, testSessionB) // a closed one beside the open one is ordinary history
	})
	t.Run("a close without its reason, a reason without its closing time", func(t *testing.T) {
		svc := testService(t)
		refused(t, svc, "CHECK", `INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at, closed_at) VALUES (?, 'lb', 0, '2026-11-20 09:15:00', '2026-11-20 10:00:00')`, testSessionA)
		refused(t, svc, "CHECK", `INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at, close_reason) VALUES (?, 'lb', 0, '2026-11-20 09:15:00', 'new_session')`, testSessionA)
		closedSessionRow(t, svc, testSessionA)
	})
	t.Run("an identity that is not a UUID's length", func(t *testing.T) {
		svc := testService(t)
		refused(t, svc, "CHECK", `INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at) VALUES ('short', 'lb', 0, '2026-11-20 09:15:00')`)
	})
	t.Run("two open intervals in one session", func(t *testing.T) {
		svc := testService(t)
		openSessionRow(t, svc, testSessionA)
		execT(t, svc, interval, testSessionA, 1, "2026-11-20 09:30:00", "confirmed")
		execT(t, svc, interval, testSessionA, 2, nil, "unconfirmed")
		execT(t, svc, interval, testSessionA, 3, nil, "open")
		refused(t, svc, "UNIQUE", interval, testSessionA, 4, nil, "open")
	})
	t.Run("an interval end whose state and time disagree", func(t *testing.T) {
		svc := testService(t)
		openSessionRow(t, svc, testSessionA)
		refused(t, svc, "CHECK", interval, testSessionA, 1, nil, "confirmed")
		refused(t, svc, "CHECK", interval, testSessionA, 1, "2026-11-20 09:30:00", "open")
		refused(t, svc, "CHECK", interval, testSessionA, 1, "2026-11-20 09:30:00", "unconfirmed")
		refused(t, svc, "CHECK", interval, testSessionA, 1, nil, "ended")
		execT(t, svc, interval, testSessionA, 1, "2026-11-20 09:30:00", "confirmed")
	})
	t.Run("an interval of no session", func(t *testing.T) {
		svc := testService(t)
		refused(t, svc, "FOREIGN KEY", interval, testSessionA, 1, nil, "open")
	})
	t.Run("a member of no session, a member that is no QSO, one QSO in two sessions", func(t *testing.T) {
		svc := testService(t)
		seedLogbookAndQsoRow(t, svc.handle)
		openSessionRow(t, svc, testSessionA)
		closedSessionRow(t, svc, testSessionB)
		const member = `INSERT INTO operating_session_member (qso_id, session_id) VALUES (?, ?)`
		refused(t, svc, "FOREIGN KEY", member, 1, "01920000-0000-7000-8000-0000000000c3")
		refused(t, svc, "FOREIGN KEY", member, 999, testSessionA)
		execT(t, svc, member, 1, testSessionA)
		refused(t, svc, "UNIQUE", member, 1, testSessionB)
	})
	t.Run("removing a session takes its intervals, and is refused while it has a member", func(t *testing.T) {
		svc := testService(t)
		seedLogbookAndQsoRow(t, svc.handle)
		openSessionRow(t, svc, testSessionA)
		execT(t, svc, interval, testSessionA, 1, nil, "open")
		execT(t, svc, `INSERT INTO operating_session_member (qso_id, session_id) VALUES (1, ?)`, testSessionA)
		refused(t, svc, "FOREIGN KEY", `DELETE FROM operating_session WHERE id = ?`, testSessionA)
		execT(t, svc, `DELETE FROM operating_session_member WHERE qso_id = 1`)
		execT(t, svc, `DELETE FROM operating_session WHERE id = ?`, testSessionA)
		if n := countT(t, svc, `SELECT COUNT(*) FROM operating_session_interval`); n != 0 {
			t.Fatalf("%d interval(s) outlived their session", n)
		}
	})
	t.Run("a recorded exit is one row for a lifetime", func(t *testing.T) {
		svc := testService(t)
		execT(t, svc, `INSERT INTO operating_app_exit (app_lifetime, exited_at) VALUES ('L1', '2026-11-20 10:00:00')`)
		refused(t, svc, "UNIQUE", `INSERT INTO operating_app_exit (app_lifetime, exited_at) VALUES ('L1', '2026-11-20 10:05:00')`)
	})
}
