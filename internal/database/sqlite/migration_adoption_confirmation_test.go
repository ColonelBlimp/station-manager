package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"testing"
)

// Migration 0017 (ADR 0091, W-0021 5F.3 commit 4b1): the account a confirmation
// belongs to, `logbook_destination.remote_adopted_account` (an HMAC verifier
// of the station account). It is written with remote_adopted_at in one durable
// write. A downgrade below 17 is refused while any binding holds a
// confirmation: a schema-16 build would upload by name without the
// account-confirmation hold.

const (
	accountA = "fp-account-a"
	accountB = "fp-account-b"
)

func TestMigrate0017_AdoptionAccount_UpAndDown(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 17 {
		t.Fatalf("schema version = %d, want 17", v)
	}
	seedSmcloudBinding(t, svc)
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE remote_adopted_account IS NULL`); n != 1 {
		t.Fatalf("rows without an account = %d, want 1", n)
	}
	execT(t, svc, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now')`)
	if _, err := svc.DowngradeLogSchemaTo(16); err != nil {
		t.Fatalf("downgrade to 16 with a reservation and no confirmation: %v", err)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM pragma_table_info('logbook_destination') WHERE name = 'remote_adopted_account'`); n != 0 {
		t.Fatal("remote_adopted_account survived 0017's down step")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at IS NOT NULL`); n != 1 {
		t.Fatal("0017's down step lost the reservation")
	}
}

func TestMigrate0017_Down_RefusesToEraseAConfirmation(t *testing.T) {
	for name, mark := range map[string]string{
		"confirmed":                  `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = 'fp'`,
		"adopted without an account": `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now')`,
		"confirmed, disabled":        `UPDATE logbook_destination SET enabled = 0, remote_adopted_at = datetime('now'), remote_adopted_account = 'fp'`,
		"confirmed on a deleted logbook": `UPDATE logbook_destination SET logbook_id = 2, remote_adopted_at = datetime('now'), remote_adopted_account = 'fp';
			UPDATE logbook SET deleted_at = datetime('now') WHERE id = 2`,
	} {
		t.Run(name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			execT(t, svc, mark)
			if _, err := svc.DowngradeLogSchemaTo(16); err == nil {
				t.Fatal("downgrade to 16 succeeded; want it refused")
			}
			if v := schemaVersion(t, svc); v != 17 {
				t.Fatalf("schema version = %d after a refused downgrade, want 17", v)
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM `+schemaMigrationsTable(MigrationSetLog)+` WHERE dirty = 1`); n != 0 {
				t.Fatal("a refused downgrade left the schema dirty")
			}
		})
	}
}

func confirmedAs(t *testing.T, svc *Service) (adopted bool, account string) {
	t.Helper()
	var at, acct sql.NullString
	if err := svc.handle.QueryRow(`SELECT remote_adopted_at, remote_adopted_account FROM logbook_destination WHERE forwarder_name = 'cloud'`).Scan(&at, &acct); err != nil {
		t.Fatal(err)
	}
	return at.Valid, acct.String
}

func TestRecordLogbookDestinationAdopted_WritesTheConfirmationAndItsAccountTogether(t *testing.T) {
	ctx := context.Background()
	creds := json.RawMessage(`{"logbook":"shack"}`)
	const reserve = `UPDATE logbook_destination SET adoption_reserved_at = datetime('now')`
	t.Run("recorded with its account, and listed", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, reserve)
		ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, accountA)
		if adopted, acct := confirmedAs(t, svc); err != nil || !ok || !adopted || acct != accountA {
			t.Fatalf("record = %v, %v; adopted %v under %q; want recorded under A", ok, err, adopted, acct)
		}
		rows, err := svc.ListLogbookDestinationsWithContext(ctx)
		if err != nil || len(rows) != 1 || rows[0].RemoteAdoptedAccount != accountA || rows[0].RemoteAdoptedAt == nil {
			t.Fatalf("listed %+v, %v; want the account read back", rows, err)
		}
		claims, err := svc.ListAdoptionClaimsWithContext(ctx)
		if err != nil || len(claims) != 1 || claims[0].RemoteAdoptedAccount != accountA {
			t.Fatalf("claims %+v, %v; want the account read back", claims, err)
		}
	})
	t.Run("the same account again changes nothing", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = '2026-01-02 03:04:05', remote_adopted_account = '`+accountA+`'`)
		if ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, accountA); err != nil || ok {
			t.Fatalf("record = %v, %v; want nothing written", ok, err)
		}
		if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE remote_adopted_at = '2026-01-02 03:04:05'`); n != 1 {
			t.Fatal("recording the same account again moved the confirmation")
		}
	})
	for name, prior := range map[string]string{
		"another account":    `, remote_adopted_at = '2026-01-02 03:04:05', remote_adopted_account = '` + accountA + `'`,
		"no account (older)": `, remote_adopted_at = '2026-01-02 03:04:05'`,
	} {
		t.Run("re-confirmed over "+name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			execT(t, svc, reserve+prior)
			ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, accountB)
			if adopted, acct := confirmedAs(t, svc); err != nil || !ok || !adopted || acct != accountB {
				t.Fatalf("record = %v, %v; adopted %v under %q; want re-confirmed under B", ok, err, adopted, acct)
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE remote_adopted_at = '2026-01-02 03:04:05'`); n != 0 {
				t.Fatal("a re-confirmation kept the old confirmation time")
			}
		})
	}
	for name, c := range map[string]struct {
		change  string
		logbook int64
		creds   json.RawMessage
	}{
		"not reserved":      {change: `UPDATE logbook_destination SET adoption_reserved_at = NULL`, logbook: 1, creds: creds},
		"other credentials": {logbook: 1, creds: json.RawMessage(`{"logbook":"portable"}`)},
		"another logbook":   {logbook: 2, creds: creds},
		"a disabled binding": {
			change: `UPDATE logbook_destination SET enabled = 0`, logbook: 1, creds: creds},
		"a deleted logbook's": {change: `UPDATE logbook SET deleted_at = datetime('now') WHERE id = 1`, logbook: 1, creds: creds},
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			execT(t, svc, reserve)
			if c.change != "" {
				execT(t, svc, c.change)
			}
			ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", c.logbook, c.creds, accountA)
			if adopted, acct := confirmedAs(t, svc); err != nil || ok || adopted || acct != "" {
				t.Fatalf("record = %v, %v; adopted %v under %q; want nothing written", ok, err, adopted, acct)
			}
		})
	}
	t.Run("refused: an empty account", func(t *testing.T) {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, reserve)
		if ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, ""); err == nil || ok {
			t.Fatalf("record = %v, %v; want an error", ok, err)
		}
		if adopted, _ := confirmedAs(t, svc); adopted {
			t.Fatal("an empty account recorded a confirmation")
		}
	})
}

// A binding adopted under another account (or none) may be reserved again for
// its re-confirmation; one confirmed under this account may not.
func TestReserveLogbookDestinationAdoption_ForReconfirmation(t *testing.T) {
	ctx := context.Background()
	creds := json.RawMessage(`{"logbook":"shack"}`)
	for name, c := range map[string]struct {
		prior string
		want  bool
	}{
		"adopted under another account": {prior: `remote_adopted_account = '` + accountA + `'`, want: true},
		"adopted without an account":    {prior: `remote_adopted_account = NULL`, want: true},
		"confirmed under this account":  {prior: `remote_adopted_account = '` + accountB + `'`, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			svc := testService(t)
			seedSmcloudBinding(t, svc)
			execT(t, svc, `UPDATE logbook_destination SET adoption_reserved_at = '2026-01-02 03:04:05', remote_adopted_at = datetime('now'), `+c.prior)
			ok, err := svc.ReserveLogbookDestinationAdoptionWithContext(ctx, "cloud", 1, creds, accountB)
			if err != nil || ok != c.want {
				t.Fatalf("reserve = %v, %v; want %v", ok, err, c.want)
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE adoption_reserved_at = '2026-01-02 03:04:05'`); n != 1 {
				t.Fatal("the reservation time moved")
			}
		})
	}
}

// The confirmation authorizes a permanent wire transition, so it commits like
// the reservation (ADR 0091, 4b ruling): FULL on one pinned connection before
// its transaction, read back inside it, NORMAL after the commit.
func TestRecordLogbookDestinationAdopted_CommitsAtSynchronousFull(t *testing.T) {
	ctx := context.Background()
	creds := json.RawMessage(`{"logbook":"shack"}`)
	type call struct {
		level     string
		confirmed int
		readback  int
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
			_ = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM logbook_destination WHERE remote_adopted_at IS NOT NULL`).Scan(&c.confirmed)
			_ = conn.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&c.readback)
			*calls = append(*calls, c)
			return err
		}
		t.Cleanup(func() { setSynchronous = real })
		return calls
	}
	reserved := func(t *testing.T) *Service {
		svc := testService(t)
		seedSmcloudBinding(t, svc)
		execT(t, svc, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now')`)
		return svc
	}
	t.Run("FULL before the write, NORMAL after the commit, on the same connection", func(t *testing.T) {
		svc := reserved(t)
		calls := observe(t, nil)
		if ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, accountA); err != nil || !ok {
			t.Fatalf("record = %v, %v", ok, err)
		}
		want := []call{{level: "FULL", confirmed: 0, readback: 2}, {level: "NORMAL", confirmed: 1, readback: 1}}
		if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
			t.Fatalf("synchronous calls = %+v; want %+v", *calls, want)
		}
	})
	t.Run("a failed FULL setting records nothing", func(t *testing.T) {
		svc := reserved(t)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			if level == "FULL" {
				return stderr.New("pragma refused")
			}
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = "+level)
			return err
		})
		if ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, accountA); err == nil || ok {
			t.Fatalf("record = %v, %v; want an error", ok, err)
		}
		if adopted, _ := confirmedAs(t, svc); adopted {
			t.Fatal("a failed FULL setting recorded a confirmation")
		}
	})
	t.Run("a FULL setting that did not take records nothing", func(t *testing.T) {
		svc := reserved(t)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = NORMAL")
			return err
		})
		if ok, err := svc.RecordLogbookDestinationAdoptedWithContext(ctx, "cloud", 1, creds, accountA); err == nil || ok {
			t.Fatalf("record = %v, %v; want an error (the transaction would commit at NORMAL)", ok, err)
		}
		if adopted, _ := confirmedAs(t, svc); adopted {
			t.Fatal("a confirmation committed at NORMAL")
		}
	})
}
