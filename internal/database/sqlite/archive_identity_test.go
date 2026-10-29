package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// W-0021 slice 1, ruling (b): the daemon fills the file's identity ONCE, in one
// transaction, after Migrate() — the archive UUID (minted only when the
// singleton row is absent), the default logbook pointer, and a UUID for every
// logbook row that has none. A second call mints nothing: the identities are
// byte-identical, which is what makes a crash between "identity written" and
// "catalogue written" safe (the next start reads the same UUID back).
func TestEnsureArchiveIdentity_MintsOnceThenIsIdempotent(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	// Two pre-0012 style rows: no uuid.
	for _, stmt := range []string{
		`INSERT INTO logbook (id, callsign, name) VALUES (1,'G4ABC','Home')`,
		`INSERT INTO logbook (id, callsign, name) VALUES (2,'G4ABC','Contest')`,
	} {
		if _, err := svc.handle.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if _, err := svc.ArchiveIdentityWithContext(ctx); !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("identity before Ensure = %v, want ErrNotFound", err)
	}

	first, err := svc.EnsureArchiveIdentityWithContext(ctx, 1)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !utils.IsValidUUIDv7(first.ArchiveUUID) {
		t.Fatalf("archive uuid %q is not a UUIDv7", first.ArchiveUUID)
	}
	if first.DefaultLogbookID != 1 || first.CreatedAt.IsZero() {
		t.Fatalf("identity = %+v; want default logbook 1 and a creation time", first)
	}
	lbs, err := svc.FetchAllLogbooksWithContext(ctx)
	if err != nil || len(lbs) != 2 {
		t.Fatalf("logbooks: %v (%d)", err, len(lbs))
	}
	if !utils.IsValidUUIDv7(lbs[0].UUID) || !utils.IsValidUUIDv7(lbs[1].UUID) || lbs[0].UUID == lbs[1].UUID {
		t.Fatalf("logbook uuids after backfill = %q, %q; want two distinct UUIDv7", lbs[0].UUID, lbs[1].UUID)
	}

	second, err := svc.EnsureArchiveIdentityWithContext(ctx, 1)
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if second != first {
		t.Fatalf("second Ensure changed the identity: %+v vs %+v", second, first)
	}
	again, _ := svc.FetchAllLogbooksWithContext(ctx)
	if again[0].UUID != lbs[0].UUID || again[1].UUID != lbs[1].UUID {
		t.Fatal("second Ensure re-minted logbook uuids")
	}
	read, err := svc.ArchiveIdentityWithContext(ctx)
	if err != nil || read != first {
		t.Fatalf("ArchiveIdentity read-back = %+v (%v), want %+v", read, err, first)
	}
}

// A default logbook id that names no row is stored as NULL, never as a dangling
// pointer — the daemon's default-logbook self-heal runs before adoption and
// corrects config; the file must not refuse identity over it.
func TestEnsureArchiveIdentity_UnknownDefaultLogbookStoredAsNull(t *testing.T) {
	svc := testService(t)
	id, err := svc.EnsureArchiveIdentityWithContext(context.Background(), 7)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if id.DefaultLogbookID != 0 {
		t.Fatalf("default logbook = %d, want 0 (NULL) for a row that does not exist", id.DefaultLogbookID)
	}
}

// Runtime creates mint too (review finding 4): a logbook inserted after start
// carries a UUID immediately, so no row is ever born without one.
func TestInsertLogbook_MintsUUID(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	id, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Field Day", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	lb, err := svc.FetchLogbookByIDWithContext(ctx, id)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !utils.IsValidUUIDv7(lb.UUID) {
		t.Fatalf("inserted logbook uuid = %q, want a UUIDv7 minted on insert", lb.UUID)
	}
	// A client-supplied uuid is not honoured: identity is minted here, never accepted.
	id2, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Other", Callsign: "G4ABC", UUID: "019fd5c5-efcc-7193-be4f-1fee532ee315"})
	if err != nil {
		t.Fatalf("insert 2: %v", err)
	}
	lb2, _ := svc.FetchLogbookByIDWithContext(ctx, id2)
	if lb2.UUID == "019fd5c5-efcc-7193-be4f-1fee532ee315" || !utils.IsValidUUIDv7(lb2.UUID) {
		t.Fatalf("client-supplied uuid was stored (%q); the store must mint its own", lb2.UUID)
	}
}

// Renaming or editing a logbook must keep its identity.
func TestUpdateLogbook_KeepsUUID(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	id, _ := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "A", Callsign: "G4ABC"})
	before, _ := svc.FetchLogbookByIDWithContext(ctx, id)
	if err := svc.UpdateLogbookWithContext(ctx, types.Logbook{ID: id, Name: "B", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	name := "C"
	if _, err := svc.UpdateLogbookFieldsWithContext(ctx, id, &name, nil); err != nil {
		t.Fatalf("update fields: %v", err)
	}
	after, _ := svc.FetchLogbookByIDWithContext(ctx, id)
	if after.UUID != before.UUID || after.UUID == "" {
		t.Fatalf("uuid changed across updates: %q → %q", before.UUID, after.UUID)
	}
}

func TestSetArchiveDefaultLogbook(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, 1); err == nil {
		t.Fatal("set default before any identity exists; want an error")
	}
	id, _ := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "A", Callsign: "G4ABC"})
	if _, err := svc.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, id); err != nil {
		t.Fatalf("set default: %v", err)
	}
	got, _ := svc.ArchiveIdentityWithContext(ctx)
	if got.DefaultLogbookID != id {
		t.Fatalf("default logbook = %d, want %d", got.DefaultLogbookID, id)
	}
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, 999); err == nil {
		t.Fatal("a default logbook id that names no row was accepted")
	}
}

func TestPeekArchiveIdentity(t *testing.T) {
	if _, found, err := PeekArchiveIdentity(filepath.Join(t.TempDir(), "missing.db")); err != nil || found {
		t.Fatalf("missing file: found=%v err=%v; want not found, no error", found, err)
	}
	svc, path := testFileService(t)
	if _, found, err := PeekArchiveIdentity(path); err != nil || found {
		t.Fatalf("fresh file without identity: found=%v err=%v", found, err)
	}
	want, err := svc.EnsureArchiveIdentityWithContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := PeekArchiveIdentity(path)
	if err != nil || !found || got.ArchiveUUID != want.ArchiveUUID {
		t.Fatalf("peek after ensure: %+v found=%v err=%v; want %+v", got, found, err, want)
	}
}

// testFileService is testService on a real file: needed where a second,
// independent connection must read the file (PeekArchiveIdentity).
func testFileService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "db", "log.db")
	cfg := config.DefaultConfig(dir)
	cfg.Datastore.Path = path
	cfg.Logging.FileLogging = false
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	logSvc := &logging.Service{ConfigService: cfgSvc, WorkingDir: cfgSvc.WorkingDir()}
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	svc := &Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := svc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	svc.SetMigrationSets(MigrationSetLog)
	if err := svc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close(); _ = logSvc.Close() })
	return svc, path
}

// The provisioner's path: the caller chooses the id (the catalogue entry and the
// file share it). Same id again is a no-op; a different id is refused so a file
// can never be relabelled as another archive.
func TestWriteArchiveIdentity_ChosenIDIdempotentNeverRelabelled(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	const chosen = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	lb, _ := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "L", Callsign: "G4ABC"})
	got, err := svc.WriteArchiveIdentityWithContext(ctx, chosen, lb)
	if err != nil || got.ArchiveUUID != chosen || got.DefaultLogbookID != lb {
		t.Fatalf("write = %+v (%v), want uuid %s default %d", got, err, chosen, lb)
	}
	again, err := svc.WriteArchiveIdentityWithContext(ctx, chosen, lb)
	if err != nil || again != got {
		t.Fatalf("second write with the same id = %+v (%v), want unchanged", again, err)
	}
	if _, err := svc.WriteArchiveIdentityWithContext(ctx, "019fd5c5-efcc-7193-be4f-1fee532ee316", lb); err == nil {
		t.Fatal("relabelling the file as another archive was accepted")
	}
	if _, err := svc.WriteArchiveIdentityWithContext(ctx, "not-a-uuid", lb); err == nil {
		t.Fatal("a malformed id was accepted")
	}
}

// A retry of the chosen-id write must complete an interrupted backfill: a
// logbook row left without a uuid (an earlier run that died after the identity
// commit) receives one on the next write with the same id.
func TestWriteArchiveIdentity_RetryCompletesTheBackfill(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	const chosen = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	if _, err := svc.WriteArchiveIdentityWithContext(ctx, chosen, 0); err != nil {
		t.Fatal(err)
	}
	// Simulate the interrupted state: a row that has no uuid although the
	// identity row exists.
	if _, err := svc.handle.Exec(`INSERT INTO logbook (id, callsign, name) VALUES (5,'G4ABC','Orphan')`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteArchiveIdentityWithContext(ctx, chosen, 0); err != nil {
		t.Fatalf("retry: %v", err)
	}
	lb, err := svc.FetchLogbookByIDWithContext(ctx, 5)
	if err != nil || !utils.IsValidUUIDv7(lb.UUID) {
		t.Fatalf("retry left logbook 5 without a uuid: %q (%v)", lb.UUID, err)
	}
}

// W-0024 slice 3a (ADR 0097 §1): the start's commit point. It writes the
// pointer only to a LIVE logbook (deletion is a soft delete, which never fires
// the pointer's foreign key), and only as a durable transaction: one pinned
// connection set to synchronous=FULL before its transaction begins, read back
// inside it, and restored to NORMAL after the commit, as the adoption
// reservation does. These tests observe that connection through the
// setSynchronous seam; none can prove power-loss durability itself.
func TestSetArchiveDefaultLogbookDurably(t *testing.T) {
	ctx := context.Background()
	type call struct {
		level    string
		pointer  int64 // the pointer on the pinned connection when the level was set
		readback int   // PRAGMA synchronous on that connection afterwards
	}
	var conns []*sql.Conn // the connection each call was made on, in order
	setup := func(t *testing.T) (svc *Service, a, b, c int64) {
		t.Helper()
		svc = testService(t)
		a, _ = svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "A", Callsign: "G4ABC"})
		b, _ = svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "B", Callsign: "G4ABC"})
		c, _ = svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "C", Callsign: "G4ABC"})
		if _, err := svc.EnsureArchiveIdentityWithContext(ctx, a); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		return svc, a, b, c
	}
	pointer := func(t *testing.T, svc *Service) int64 {
		t.Helper()
		got, err := svc.ArchiveIdentityWithContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return got.DefaultLogbookID
	}
	observe := func(t *testing.T, override func(ctx context.Context, conn *sql.Conn, level string) error) *[]call {
		t.Helper()
		calls := &[]call{}
		conns = nil
		real := setSynchronous
		setSynchronous = func(ctx context.Context, conn *sql.Conn, level string) error {
			var err error
			if override != nil {
				err = override(ctx, conn, level)
			} else {
				err = real(ctx, conn, level)
			}
			c := call{level: level}
			var p sql.NullInt64
			if qerr := conn.QueryRowContext(ctx, `SELECT default_logbook_id FROM archive_metadata WHERE singleton = 1`).Scan(&p); qerr != nil {
				t.Errorf("observe the pointer on the pinned connection: %v", qerr)
			}
			c.pointer = p.Int64
			if qerr := conn.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&c.readback); qerr != nil {
				t.Errorf("observe synchronous on the pinned connection: %v", qerr)
			}
			*calls = append(*calls, c)
			conns = append(conns, conn)
			return err
		}
		t.Cleanup(func() { setSynchronous = real })
		return calls
	}
	t.Run("FULL before the write, NORMAL after the commit, on the same connection", func(t *testing.T) {
		svc, a, b, _ := setup(t)
		calls := observe(t, nil)
		if err := svc.SetArchiveDefaultLogbookDurablyWithContext(ctx, b); err != nil {
			t.Fatalf("durable set: %v", err)
		}
		want := []call{{level: "FULL", pointer: a, readback: 2}, {level: "NORMAL", pointer: b, readback: 1}}
		if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
			t.Fatalf("synchronous calls = %+v; want %+v", *calls, want)
		}
		if len(conns) != 2 || conns[0] == nil || conns[0] != conns[1] {
			t.Fatalf("connections = %p; want FULL and NORMAL set on one pinned connection", conns)
		}
		if p := pointer(t, svc); p != b {
			t.Fatalf("pointer = %d; want %d", p, b)
		}
	})
	t.Run("a failed FULL setting writes nothing", func(t *testing.T) {
		svc, a, b, _ := setup(t)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			if level == "FULL" {
				return stderr.New("pragma refused")
			}
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = "+level)
			return err
		})
		if err := svc.SetArchiveDefaultLogbookDurablyWithContext(ctx, b); err == nil {
			t.Fatal("durable set succeeded; want an error")
		}
		if p := pointer(t, svc); p != a {
			t.Fatalf("pointer = %d after a failed FULL setting; want %d", p, a)
		}
	})
	t.Run("a FULL setting that did not take writes nothing", func(t *testing.T) {
		svc, a, b, _ := setup(t)
		observe(t, func(ctx context.Context, conn *sql.Conn, level string) error {
			_, err := conn.ExecContext(ctx, "PRAGMA synchronous = NORMAL") // claims success, leaves NORMAL
			return err
		})
		if err := svc.SetArchiveDefaultLogbookDurablyWithContext(ctx, b); err == nil {
			t.Fatal("durable set succeeded; want an error (the transaction would commit at NORMAL)")
		}
		if p := pointer(t, svc); p != a {
			t.Fatalf("pointer = %d after a commit at NORMAL was refused; want %d", p, a)
		}
	})
	t.Run("only a live logbook", func(t *testing.T) {
		svc, a, _, c := setup(t)
		if err := svc.DeleteLogbookByIDWithContext(ctx, c); err != nil {
			t.Fatalf("soft-delete C: %v", err)
		}
		for name, id := range map[string]int64{"a missing logbook": 999, "a soft-deleted logbook": c} {
			if err := svc.SetArchiveDefaultLogbookDurablyWithContext(ctx, id); err == nil {
				t.Fatalf("%s was accepted", name)
			}
			if p := pointer(t, svc); p != a {
				t.Fatalf("after refusing %s, pointer = %d; want %d unchanged", name, p, a)
			}
		}
	})
}
