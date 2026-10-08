package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"testing"
)

// Migration 0016 (ADR 0091, W-0021 5F.3 commit 4a): the archive-local
// reservation of an SM Cloud cloud name, `logbook_destination.
// adoption_reserved_at`. Up adds it, NULL on every existing row. A downgrade
// below 16 is refused, before any migration runs, while any binding holds a
// reservation or an adoption: either would be erased. Without one it drops the
// column and nothing else.

func seedSmcloudBinding(t *testing.T, svc *Service) {
	t.Helper()
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (1, '01920000-0000-7000-8000-00000000000a', 'M0ABC', 'Main')`)
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (2, '01920000-0000-7000-8000-00000000000b', 'M0XYZ', 'Second')`)
	execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, '01920000-0000-7000-8000-000000000001', 1)`)
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (1, 'smcloud', 'cloud', 1, '{"logbook":"shack"}')`)
}

func TestMigrate0016_AdoptionReservation_Up(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 16 {
		t.Fatalf("schema version = %d, want 16", v)
	}
	seedSmcloudBinding(t, svc)
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NULL`); n != 1 {
		t.Fatalf("unreserved rows = %d, want 1 (a new row is not reserved)", n)
	}
}

func TestMigrate0016_Down_DropsTheColumnWhenNothingIsReserved(t *testing.T) {
	svc := testService(t)
	seedSmcloudBinding(t, svc)
	if _, err := svc.DowngradeLogSchemaTo(15); err != nil {
		t.Fatalf("downgrade to 15: %v", err)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM pragma_table_info('logbook_destination') WHERE name = 'adoption_reserved_at'`); n != 0 {
		t.Fatal("adoption_reserved_at survived 0016's down step")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE forwarder_name = 'cloud' AND credentials = '{"logbook":"shack"}'`); n != 1 {
		t.Fatal("the binding did not survive 0016's down step")
	}
}

func TestMigrate0016_Down_RefusesToEraseAReservationOrAnAdoption(t *testing.T) {
	for name, mark := range map[string]string{
		"reserved": `UPDATE logbook_destination SET adoption_reserved_at = datetime('now')`,
		"adopted":  `UPDATE logbook_destination SET remote_adopted_at = datetime('now')`,
		"reserved on a deleted logbook": `UPDATE logbook_destination SET logbook_id = 2, adoption_reserved_at = datetime('now');
			UPDATE logbook SET deleted_at = datetime('now') WHERE id = 2`,
	} {
		t.Run(name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			execT(t, svc, mark)
			for _, target := range []uint{15, 13} {
				if _, err := svc.DowngradeLogSchemaTo(target); err == nil {
					t.Fatalf("downgrade to %d succeeded; want it refused", target)
				}
				if v := schemaVersion(t, svc); v != 16 {
					t.Fatalf("schema version = %d after a refused downgrade, want 16", v)
				}
				if n := countT(t, svc, `SELECT COUNT(*) FROM `+schemaMigrationsTable(MigrationSetLog)+` WHERE dirty = 1`); n != 0 {
					t.Fatal("a refused downgrade left the schema dirty")
				}
			}
		})
	}
}

func TestReserveLogbookDestinationAdoption(t *testing.T) {
	ctx := context.Background()
	creds := json.RawMessage(`{"logbook":"shack"}`)
	reserved := func(t *testing.T, svc *Service) int {
		return countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NOT NULL`)
	}
	t.Run("reserves the binding read, once", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds)
		if err != nil || !ok || reserved(t, svc) != 1 {
			t.Fatalf("reserve = %v, %v; reserved rows %d", ok, err, reserved(t, svc))
		}
		execT(t, svc, `UPDATE logbook_destination SET adoption_reserved_at = '2026-01-02 03:04:05'`)
		if ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds); err != nil || !ok {
			t.Fatalf("reserve again = %v, %v; want true", ok, err)
		}
		if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at = '2026-01-02 03:04:05'`); n != 1 {
			t.Fatal("reserving again moved the reservation time")
		}
	})
	for name, c := range map[string]struct {
		change  string
		name    string
		logbook int64
		creds   json.RawMessage
	}{
		"other credentials":   {name: "cloud", logbook: 1, creds: json.RawMessage(`{"logbook":"portable"}`)},
		"no credentials":      {name: "cloud", logbook: 1},
		"another logbook":     {name: "cloud", logbook: 2, creds: creds},
		"an unknown binding":  {name: "gone", logbook: 1, creds: creds},
		"a disabled binding":  {change: `UPDATE logbook_destination SET enabled = 0`, name: "cloud", logbook: 1, creds: creds},
		"an adopted binding":  {change: `UPDATE logbook_destination SET remote_adopted_at = datetime('now')`, name: "cloud", logbook: 1, creds: creds},
		"a deleted logbook's": {change: `UPDATE logbook SET deleted_at = datetime('now') WHERE id = 1`, name: "cloud", logbook: 1, creds: creds},
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			if c.change != "" {
				execT(t, svc, c.change)
			}
			ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, c.name, c.logbook, c.creds)
			if err != nil || ok || reserved(t, svc) != 0 {
				t.Fatalf("reserve = %v, %v; reserved rows %d; want nothing reserved", ok, err, reserved(t, svc))
			}
		})
	}
}

func TestListAdoptionClaims_IncludesDisabledAndDeletedLogbooks(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	seedSmcloudBinding(t, svc)
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (3, '01920000-0000-7000-8000-00000000000c', 'M0GON', 'Gone')`)
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, remote_adopted_at) VALUES (3, 'smcloud', 'cloud.gone', 0, '{"logbook":"old"}', datetime('now'))`)
	execT(t, svc, `UPDATE logbook SET deleted_at = datetime('now') WHERE id = 3`)
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (2, 'smcloud', 'cloud.second', 0, '{"logbook":"free"}')`)
	execT(t, svc, `UPDATE logbook_destination SET enabled = 0, adoption_reserved_at = datetime('now') WHERE forwarder_name = 'cloud'`)
	claims, err := svc.ListAdoptionClaimsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range claims {
		got[c.ForwarderName] = string(c.Credentials)
		if c.Destination != "smcloud" || (c.AdoptionReservedAt == nil && c.RemoteAdoptedAt == nil) {
			t.Fatalf("claim %+v", c)
		}
	}
	if len(got) != 2 || got["cloud"] != `{"logbook":"shack"}` || got["cloud.gone"] != `{"logbook":"old"}` {
		t.Fatalf("claims = %v; want the disabled reserved binding and the deleted logbook's adopted one", got)
	}
}

// The reservation must survive a power loss once it is reported (ADR 0091,
// operator P1 on 4a): the pool runs WAL with synchronous=NORMAL, under which a
// committed transaction may roll back after a power failure. The reservation
// commits on ONE pinned connection set to FULL before its transaction begins,
// read back inside it, and restored to NORMAL after the commit. These tests
// observe that connection through the setSynchronous seam; no test here can
// prove power-loss durability itself.
func TestReserveLogbookDestinationAdoption_CommitsAtSynchronousFull(t *testing.T) {
	ctx := context.Background()
	creds := json.RawMessage(`{"logbook":"shack"}`)
	type call struct {
		level    string
		reserved int // reserved rows visible on the pinned connection when the level was set
		readback int // PRAGMA synchronous on that connection afterwards
	}
	observe := func(t *testing.T, override func(ctx context.Context, conn *sql.Conn, level string) error) *[]call {
		t.Helper()
		calls := &[]call{}
		real := setSynchronous
		setSynchronous = func(ctx context.Context, conn *sql.Conn, level string) error {
			var err error
			if override != nil {
				err = override(ctx, conn, level)
			} else {
				err = real(ctx, conn, level)
			}
			c := call{level: level}
			_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NOT NULL`).Scan(&c.reserved)
			_ = conn.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&c.readback)
			*calls = append(*calls, c)
			return err
		}
		t.Cleanup(func() { setSynchronous = real })
		return calls
	}
	t.Run("FULL before the write, NORMAL after the commit, on the same connection", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		calls := observe(t, nil)
		if ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds); err != nil || !ok {
			t.Fatalf("reserve = %v, %v", ok, err)
		}
		want := []call{{level: "FULL", reserved: 0, readback: 2}, {level: "NORMAL", reserved: 1, readback: 1}}
		if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
			t.Fatalf("synchronous calls = %+v; want %+v", *calls, want)
		}
	})
	t.Run("a failed FULL setting reserves nothing", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			if level == "FULL" {
				return stderr.New("pragma refused")
			}
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = "+level)
			return err
		})
		ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds)
		if err == nil || ok {
			t.Fatalf("reserve = %v, %v; want an error", ok, err)
		}
		if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NOT NULL`); n != 0 {
			t.Fatalf("reserved rows = %d after a failed FULL setting, want 0", n)
		}
	})
	t.Run("a FULL setting that did not take reserves nothing", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = NORMAL") // claims success, leaves NORMAL
			return err
		})
		ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds)
		if err == nil || ok {
			t.Fatalf("reserve = %v, %v; want an error (the transaction would commit at NORMAL)", ok, err)
		}
		if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NOT NULL`); n != 0 {
			t.Fatalf("reserved rows = %d after a commit at NORMAL was refused, want 0", n)
		}
	})
}
