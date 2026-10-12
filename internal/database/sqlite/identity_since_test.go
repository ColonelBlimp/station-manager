package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"testing"
)

/*
   Migration 0018 and identity_since (W-0021 5F.4 commit 3; ADR 0092; rulings
   F2, H1, 2026-10-10): a binding born on SM Cloud identity is marked
   permanently. No production caller stamps until commit 4.

     MI1  0018 adds identity_since, NULL on every existing row; a downgrade to
          17 with no stamp drops it and keeps everything else.
     MI2  the listing reads the stamp.
     MI3  a stamping save: the stamp lands in the save's one transaction; an
          existing stamp keeps its time; a disabled or non-SM-Cloud row, or a
          binding already reserved or adopted, is refused and nothing in the
          save is written; a later failing row rolls the stamp back; the
          transaction runs on one pinned connection set to FULL before it
          begins, read back inside it and restored after; a failed setting,
          a setting that did not take, and a cancelled context write nothing.
     MI4  never cleared: a disable, a credential edit and a re-enable without
          the flag keep it, to the second.
     MI5  a downgrade below 18 is refused while any binding holds a stamp —
          enabled, disabled, or a deleted logbook's — before any migration:
          the schema stays 18 and clean.
     MI6  adoption exclusion at the write boundary: a stamped binding cannot
          be reserved or confirmed; a reserved one cannot be stamped. Both
          orderings.
*/

const stampedAt = "2026-01-02 03:04:05"

func stampOf(t *testing.T, svc *Service, name string) (bool, string) {
	t.Helper()
	var at sql.NullString
	if err := svc.handle.QueryRow(`SELECT strftime('%Y-%m-%d %H:%M:%S', identity_since) FROM logbook_destination WHERE forwarder_name = ?`, name).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at.Valid, at.String
}

func TestMigrate0018_IdentitySince_UpAndDown(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 18 {
		t.Fatalf("schema version = %d, want 18", v)
	}
	seedSmcloudBinding(t, svc)
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE identity_since IS NULL`); n != 1 {
		t.Fatalf("unstamped rows = %d, want 1", n)
	}
	execT(t, svc, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now')`)
	if _, err := svc.DowngradeLogSchemaTo(17); err != nil {
		t.Fatalf("downgrade to 17 without a stamp: %v", err)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM pragma_table_info('logbook_destination') WHERE name = 'identity_since'`); n != 0 {
		t.Fatal("identity_since survived 0018's down step")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NOT NULL AND forwarder_name = 'cloud'`); n != 1 {
		t.Fatal("0018's down step lost the binding or its reservation")
	}
}

func TestIdentitySince_MI2_ListingReadsTheStamp(t *testing.T) {
	svc := testService(t)
	seedSmcloudBinding(t, svc)
	execT(t, svc, `UPDATE logbook_destination SET identity_since = ?`, stampedAt)
	rows, err := svc.ListLogbookDestinationsWithContext(context.Background())
	if err != nil || len(rows) != 1 || rows[0].IdentitySince == nil || rows[0].IdentitySince.Format("2006-01-02 15:04:05") != stampedAt {
		t.Fatalf("listed %+v, %v; want the stamp read back", rows, err)
	}
}

func stampRow(enabled bool) DestinationUpsert {
	return DestinationUpsert{LogbookID: 1, Destination: "smcloud", ForwarderName: "cloud", Enabled: enabled,
		Credentials: json.RawMessage(`{"logbook":"shack"}`), StampIdentity: true}
}

func TestIdentitySince_MI3_AStampingSave(t *testing.T) {
	ctx := context.Background()
	qrzSecond := DestinationUpsert{LogbookID: 2, Destination: "qrz", ForwarderName: "qrz.second", Enabled: true,
		Credentials: json.RawMessage(`{"api_key":"K"}`)}

	t.Run("enables and stamps in one save", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, `UPDATE logbook_destination SET enabled = 0`)
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true), qrzSecond}); err != nil {
			t.Fatal(err)
		}
		if ok, _ := stampOf(t, svc, "cloud"); !ok || countT(t, svc, `SELECT enabled FROM logbook_destination WHERE forwarder_name = 'cloud'`) != 1 {
			t.Fatal("the save did not enable and stamp the binding")
		}
	})

	t.Run("an existing stamp keeps its time", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, `UPDATE logbook_destination SET identity_since = ?`, stampedAt)
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true)}); err != nil {
			t.Fatal(err)
		}
		if _, at := stampOf(t, svc, "cloud"); at != stampedAt {
			t.Fatalf("stamp = %q; want %q kept exactly", at, stampedAt)
		}
	})

	for name, c := range map[string]struct {
		prior string
		row   DestinationUpsert
	}{
		"a disabled row": {row: stampRow(false)},
		"a non-SM-Cloud row": {row: DestinationUpsert{LogbookID: 2, Destination: "qrz", ForwarderName: "qrz.second", Enabled: true,
			Credentials: json.RawMessage(`{"api_key":"K"}`), StampIdentity: true}},
		"a reserved binding": {prior: `UPDATE logbook_destination SET adoption_reserved_at = datetime('now')`, row: stampRow(true)},
		"an adopted binding": {prior: `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = 'fp'`,
			row: stampRow(true)},
		"an adopted binding without a reservation": {prior: `UPDATE logbook_destination SET remote_adopted_at = datetime('now')`, row: stampRow(true)},
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			execT(t, svc, `UPDATE logbook_destination SET enabled = 0`)
			if c.prior != "" {
				execT(t, svc, c.prior)
			}
			other := DestinationUpsert{LogbookID: 2, Destination: "qrzcq", ForwarderName: "qrzcq.second", Enabled: true}
			if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{other, c.row}); err == nil {
				t.Fatal("the stamping save succeeded; want it refused")
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE identity_since IS NOT NULL OR forwarder_name IN ('qrzcq.second', 'qrz.second') OR enabled = 1`); n != 0 {
				t.Fatalf("%d rows written by a refused save; want nothing", n)
			}
		})
	}

	t.Run("a later failing row rolls the stamp back", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, `UPDATE logbook_destination SET enabled = 0`)
		clash := DestinationUpsert{LogbookID: 2, Destination: "smcloud", ForwarderName: "cloud", Enabled: true} // forwarder_name is unique
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true), clash}); err == nil {
			t.Fatal("the save succeeded; want the clashing row to fail it")
		}
		if ok, _ := stampOf(t, svc, "cloud"); ok || countT(t, svc, `SELECT enabled FROM logbook_destination WHERE forwarder_name = 'cloud'`) != 0 {
			t.Fatal("a failed save kept the stamp or the enable")
		}
	})

	t.Run("cancelled before it runs", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, `UPDATE logbook_destination SET enabled = 0`)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := svc.UpsertLogbookDestinationsWithContext(cctx, []DestinationUpsert{stampRow(true)}); err == nil {
			t.Fatal("a cancelled save succeeded")
		}
		if ok, _ := stampOf(t, svc, "cloud"); ok {
			t.Fatal("a cancelled save stamped the binding")
		}
	})
}

func TestIdentitySince_MI3_CommitsAtSynchronousFull(t *testing.T) {
	ctx := context.Background()
	type call struct {
		level    string
		stamped  int
		readback int
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
			_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM logbook_destination WHERE identity_since IS NOT NULL`).Scan(&c.stamped)
			_ = conn.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&c.readback)
			*calls = append(*calls, c)
			return err
		}
		t.Cleanup(func() { setSynchronous = real })
		return calls
	}
	fresh := func(t *testing.T) *Service {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		return svc
	}
	t.Run("FULL before the write, NORMAL after the commit, on the same connection", func(t *testing.T) {
		svc := fresh(t)
		calls := observe(t, nil)
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true)}); err != nil {
			t.Fatal(err)
		}
		want := []call{{level: "FULL", stamped: 0, readback: 2}, {level: "NORMAL", stamped: 1, readback: 1}}
		if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
			t.Fatalf("synchronous calls = %+v; want %+v", *calls, want)
		}
	})
	t.Run("a failed FULL setting writes nothing", func(t *testing.T) {
		svc := fresh(t)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			if level == "FULL" {
				return stderr.New("pragma refused")
			}
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = "+level)
			return err
		})
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true)}); err == nil {
			t.Fatal("the save succeeded; want an error")
		}
		if ok, _ := stampOf(t, svc, "cloud"); ok {
			t.Fatal("a failed FULL setting stamped the binding")
		}
	})
	t.Run("a FULL setting that reports success but did not take writes nothing", func(t *testing.T) {
		svc := fresh(t)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = NORMAL")
			return err
		})
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true)}); err == nil {
			t.Fatal("the save succeeded; want an error (it would commit at NORMAL)")
		}
		if ok, _ := stampOf(t, svc, "cloud"); ok {
			t.Fatal("a stamp committed at NORMAL")
		}
	})
}

func TestIdentitySince_MI4_NeverCleared(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	seedSmcloudBinding(t, svc)
	execT(t, svc, `UPDATE logbook_destination SET identity_since = ?`, stampedAt)
	for _, row := range []DestinationUpsert{
		{LogbookID: 1, Destination: "smcloud", ForwarderName: "cloud", Enabled: false, Credentials: json.RawMessage(`{"logbook":"shack"}`)},
		{LogbookID: 1, Destination: "smcloud", ForwarderName: "cloud", Enabled: false, Credentials: json.RawMessage(`{"logbook":"other"}`)},
		{LogbookID: 1, Destination: "smcloud", ForwarderName: "cloud", Enabled: true},
	} {
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{row}); err != nil {
			t.Fatal(err)
		}
		if _, at := stampOf(t, svc, "cloud"); at != stampedAt {
			t.Fatalf("after %+v the stamp is %q; want %q", row, at, stampedAt)
		}
	}
}

func TestMigrate0018_Down_RefusesToEraseAStamp(t *testing.T) {
	for name, mark := range map[string]string{
		"enabled":  `UPDATE logbook_destination SET identity_since = datetime('now')`,
		"disabled": `UPDATE logbook_destination SET enabled = 0, identity_since = datetime('now')`,
		"a deleted logbook's": `UPDATE logbook_destination SET logbook_id = 2, identity_since = datetime('now');
			UPDATE logbook SET deleted_at = datetime('now') WHERE id = 2`,
	} {
		for _, target := range []uint{17, 16} {
			t.Run(name, func(t *testing.T) {
				svc := testService(t)
				seedSmcloudBinding(t, svc)
				execT(t, svc, mark)
				if _, err := svc.DowngradeLogSchemaTo(target); err == nil {
					t.Fatalf("downgrade to %d succeeded; want it refused", target)
				}
				if v := schemaVersion(t, svc); v != 18 {
					t.Fatalf("schema version = %d after a refused downgrade, want 18", v)
				}
				if n := countT(t, svc, `SELECT COUNT(*) FROM `+schemaMigrationsTable(MigrationSetLog)+` WHERE dirty = 1`); n != 0 {
					t.Fatal("a refused downgrade left the schema dirty")
				}
			})
		}
	}
}

func TestIdentitySince_MI6_AdoptionExcludedAtTheWrite(t *testing.T) {
	ctx := context.Background()
	creds := json.RawMessage(`{"logbook":"shack"}`)

	t.Run("stamp first: the reservation is refused", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true)}); err != nil {
			t.Fatal(err)
		}
		ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds, accountA)
		if err != nil || ok {
			t.Fatalf("reserve = %v, %v; want nothing written", ok, err)
		}
		if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NOT NULL`); n != 0 {
			t.Fatal("a stamped binding was reserved")
		}
	})

	t.Run("reservation first: the stamp is refused", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		if ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds, accountA); err != nil || !ok {
			t.Fatalf("reserve = %v, %v", ok, err)
		}
		if err := svc.UpsertLogbookDestinationsWithContext(ctx, []DestinationUpsert{stampRow(true)}); err == nil {
			t.Fatal("a reserved binding was stamped")
		}
		if ok, _ := stampOf(t, svc, "cloud"); ok {
			t.Fatal("a reserved binding holds a stamp")
		}
	})

	t.Run("a stamped binding is never confirmed", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		// The mixed state no build writes, planted to prove the confirmation's own check.
		execT(t, svc, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), identity_since = datetime('now')`)
		ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, accountA)
		if adopted, _ := confirmedAs(t, svc); err != nil || ok || adopted {
			t.Fatalf("record = %v, %v; adopted %v; want nothing written", ok, err, adopted)
		}
	})
}
