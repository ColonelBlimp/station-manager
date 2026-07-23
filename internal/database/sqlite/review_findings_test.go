package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// --- M2: QSO updates respect soft-delete + return ErrNotFound ---------------

func TestUpdateQso_RejectsSoftDeletedAndMissing(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	id, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}
	qso.ID = id

	// Sanity: updating the ACTIVE row succeeds.
	qso.QsoDetails.RstRcvd = "599"
	if err := svc.UpdateQsoWithContext(ctx, qso); err != nil {
		t.Fatalf("active update should succeed: %v", err)
	}

	// Soft-delete the QSO.
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := svc.DeleteQsoByIDTx(ctx, tx, id); err != nil {
		_ = tx.Rollback()
		cancel()
		t.Fatalf("soft delete: %v", err)
	}
	if err := tx.Commit(); err != nil {
		cancel()
		t.Fatalf("commit delete: %v", err)
	}
	cancel()

	// Updating a soft-deleted QSO returns ErrNotFound...
	qso.QsoDetails.RstRcvd = "111"
	if err := svc.UpdateQsoWithContext(ctx, qso); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of soft-deleted QSO: got %v, want ErrNotFound", err)
	}
	// ...stays hidden (not resurrected)...
	if _, ferr := svc.FetchQsoByIdWithContext(ctx, id); !stderr.Is(ferr, errors.ErrNotFound) {
		t.Errorf("soft-deleted QSO resurfaced on the active read path after a stale update: %v", ferr)
	}
	// ...and the stale field write did NOT land on the tombstone (the active-row
	// predicate matched zero rows, so RstRcvd stays at the last active value).
	deleted, derr := svc.FetchQsoByIDIncludingDeletedWithContext(ctx, id)
	if derr != nil {
		t.Fatalf("fetch including deleted: %v", derr)
	}
	if deleted.QsoDetails.RstRcvd != "599" {
		t.Errorf("stale update wrote through to the soft-deleted row: RstRcvd = %q, want %q",
			deleted.QsoDetails.RstRcvd, "599")
	}

	// Updating a non-existent ID returns ErrNotFound.
	missing := validTestQso(lbID, "Z99ZZ", "20m", "SSB", "20250508", "0900")
	missing.ID = 999999
	if err := svc.UpdateQsoWithContext(ctx, missing); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of missing ID: got %v, want ErrNotFound", err)
	}
}

// TestUpdateLogbook_RejectsSoftDeletedAndMissing is the logbook sibling of the
// QSO soft-delete fix (api review M3): PATCH /v1/logbook/{id} is API-reachable,
// so a stale update after a concurrent delete must not resurrect the tombstone.
func TestUpdateLogbook_RejectsSoftDeletedAndMissing(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	id, err := svc.InsertLogbook(types.Logbook{Name: "L1", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	// Active update succeeds.
	if err := svc.UpdateLogbookWithContext(ctx, types.Logbook{ID: id, Name: "L1-renamed", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("active logbook update should succeed: %v", err)
	}

	// Soft-delete it.
	if err := svc.DeleteLogbookByIDWithContext(ctx, id); err != nil {
		t.Fatalf("soft delete logbook: %v", err)
	}

	// Updating a soft-deleted logbook → ErrNotFound, not resurrected.
	if err := svc.UpdateLogbookWithContext(ctx, types.Logbook{ID: id, Name: "zombie", Callsign: "G4ABC"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of soft-deleted logbook: got %v, want ErrNotFound", err)
	}
	if _, ferr := svc.FetchLogbookByIDWithContext(ctx, id); !stderr.Is(ferr, errors.ErrNotFound) {
		t.Errorf("soft-deleted logbook resurfaced on the active read path: %v", ferr)
	}

	// Missing id → ErrNotFound.
	if err := svc.UpdateLogbookWithContext(ctx, types.Logbook{ID: 999999, Name: "x", Callsign: "G4ABC"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of missing logbook: got %v, want ErrNotFound", err)
	}
}

// --- 2026-07-22 finding 2: no QSO may live under a soft-deleted logbook -----

// TestInsertQso_RejectsSoftDeletedLogbook pins the insert-side half of the fix.
// The FK is ON DELETE RESTRICT, which fires on hard deletes only — a
// soft-deleted logbook still physically satisfies it — so before the fix a QSO
// submitted after (or concurrently with) a logbook deletion committed as a live
// row under a deleted parent, invisible to every logbook-scoped query.
func TestInsertQso_RejectsSoftDeletedLogbook(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	if err := svc.DeleteLogbookByIDWithContext(ctx, lbID); err != nil {
		t.Fatalf("soft delete logbook: %v", err)
	}

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")

	// Non-transactional path.
	if _, err := svc.InsertQsoWithContext(ctx, qso); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("InsertQsoWithContext under a soft-deleted logbook: got %v, want ErrNotFound", err)
	}

	// Transactional path — the one the live submit/import flows use.
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	if _, err := svc.InsertQsoTx(ctx, tx, qso); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("InsertQsoTx under a soft-deleted logbook: got %v, want ErrNotFound", err)
	}
	_ = tx.Rollback()

	// A logbook that never existed is rejected the same way.
	orphan := validTestQso(999999, "M0CMC", "40m", "SSB", "20250508", "0900")
	if _, err := svc.InsertQsoWithContext(ctx, orphan); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("insert under a missing logbook: got %v, want ErrNotFound", err)
	}
}

// TestReadFirstTxStrandsOnConcurrentCommit is a CHARACTERIZATION test for the
// SQLite behaviour that makes InsertQsoTx's statement order load-bearing
// (2026-07-23 review of 0517354b). A transaction whose first statement is a READ
// starts as a reader on a WAL snapshot; SQLite then refuses its write upgrade
// with SQLITE_BUSY_SNAPSHOT ("database is locked", 517) if any other connection
// committed in between. Note what triggers it below: an UNRELATED logbook insert,
// not a delete of the row being read — so when the soft-deleted-parent guard sat
// before the insert, two ordinary simultaneous submits were enough to turn one of
// them into a 500. Guarding AFTER the insert keeps the transaction a writer from
// its first statement.
//
// This pins the PREMISE, not the call site: the damaging interleave lives strictly
// between InsertQsoTx's two statements, which no outside caller can drive without
// a hook inside it (a deferred tx takes its snapshot at the first read, so a commit
// landing before that read is harmless — an end-to-end test passes under either
// order and would give false assurance). If a driver or SQLite upgrade ever makes
// read-first safe, this test flips and the ordering comment can be revisited.
//
// Needs a real pool: the shared :memory: helpers pin MaxOpenConns to 1, where no
// second connection exists to invalidate a snapshot.
func TestReadFirstTxStrandsOnConcurrentCommit(t *testing.T) {
	svc := concurrentCacheService(t, filepath.Join(t.TempDir(), "order.db"))
	ctx := context.Background()

	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("seed logbook: %v", err)
	}

	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()

	// 1. Read first — exactly what a guard placed before the insert would do.
	var live bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM logbook WHERE id = ? AND deleted_at IS NULL)`,
		lbID).Scan(&live); err != nil {
		t.Fatalf("guard read: %v", err)
	}
	if !live {
		t.Fatalf("test setup: seeded logbook should read live")
	}

	// 2. An UNRELATED commit on another pool connection.
	if _, err := svc.InsertLogbook(types.Logbook{Name: "Other", Callsign: "G9ZZZ"}); err != nil {
		t.Fatalf("concurrent commit: %v", err)
	}

	// 3. The write upgrade is now refused — the reason the guard moved after it.
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQsoTx(ctx, tx, qso); err == nil {
		t.Fatal("read-first tx completed its write after a concurrent commit — " +
			"SQLite no longer strands the upgrade, so InsertQsoTx's ordering comment is stale")
	}
}

// TestDeleteLogbook_ConditionalOutcomes covers the delete-side half — now a
// single conditional UPDATE carrying both preconditions. The classification of
// its no-op case must still distinguish "has live QSOs" from "not live", and
// the pre-existing rule that soft-deleted QSOs do NOT block a delete has to
// survive the rewrite.
func TestDeleteLogbook_ConditionalOutcomes(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// A logbook holding a live QSO cannot be deleted.
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L1", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	qsoID, err := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}
	if err := svc.DeleteLogbookByIDWithContext(ctx, lbID); !stderr.Is(err, errors.ErrLogbookHasQsos) {
		t.Errorf("delete with a live QSO: got %v, want ErrLogbookHasQsos", err)
	}
	if _, ferr := svc.FetchLogbookByIDWithContext(ctx, lbID); ferr != nil {
		t.Errorf("a rejected delete must leave the logbook live: %v", ferr)
	}

	// Once that QSO is soft-deleted the logbook is deletable again — the
	// pre-fix .Exists() check counted live rows only, and so must the
	// NOT EXISTS that replaced it.
	dtx, dcancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := svc.DeleteQsoByIDTx(ctx, dtx, qsoID); err != nil {
		_ = dtx.Rollback()
		dcancel()
		t.Fatalf("soft delete qso: %v", err)
	}
	if err := dtx.Commit(); err != nil {
		dcancel()
		t.Fatalf("commit qso delete: %v", err)
	}
	dcancel()
	if err := svc.DeleteLogbookByIDWithContext(ctx, lbID); err != nil {
		t.Errorf("delete with only soft-deleted QSOs should succeed: %v", err)
	}

	// Deleting it again, and deleting an id that never existed, are both
	// ErrNotFound — matching the deleted_at-filtered read path.
	if err := svc.DeleteLogbookByIDWithContext(ctx, lbID); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("re-delete of a soft-deleted logbook: got %v, want ErrNotFound", err)
	}
	if err := svc.DeleteLogbookByIDWithContext(ctx, 999999); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("delete of a missing logbook: got %v, want ErrNotFound", err)
	}
}

// --- M1 (2026-06-19): legacy update/upsert helpers respect active-row + not-found ---

func TestUpdateContactedStation_RejectsSoftDeletedAndMissing(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	id, err := svc.InsertContactedStationWithContext(ctx, types.ContactedStation{Call: "M0CMC", Name: "Marc"})
	if err != nil {
		t.Fatalf("insert contacted station: %v", err)
	}

	// Active update succeeds.
	if err := svc.UpdateContactedStationWithContext(ctx, types.ContactedStation{CSID: id, Call: "M0CMC", Name: "Updated"}); err != nil {
		t.Fatalf("active update should succeed: %v", err)
	}

	// Missing id → ErrNotFound (was a silent nil before).
	if err := svc.UpdateContactedStationWithContext(ctx, types.ContactedStation{CSID: 999999, Call: "Z99ZZ"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of missing id: got %v, want ErrNotFound", err)
	}

	// Soft-deleted row → ErrNotFound, not written through.
	if _, e := svc.handle.Exec("UPDATE contacted_station SET deleted_at = datetime('now') WHERE id = ?", id); e != nil {
		t.Fatalf("soft delete: %v", e)
	}
	if err := svc.UpdateContactedStationWithContext(ctx, types.ContactedStation{CSID: id, Call: "M0CMC", Name: "zombie"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of soft-deleted row: got %v, want ErrNotFound", err)
	}
}

func TestUpdateCountry_RejectsSoftDeletedAndMissing(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	id, err := svc.InsertCountryWithContext(ctx, types.Country{Prefix: "K1", Name: "United States"})
	if err != nil {
		t.Fatalf("insert country: %v", err)
	}

	if err := svc.UpdateCountryWithContext(ctx, types.Country{ID: id, Prefix: "K1", Name: "USA"}); err != nil {
		t.Fatalf("active update should succeed: %v", err)
	}

	if err := svc.UpdateCountryWithContext(ctx, types.Country{ID: 999999, Prefix: "K1", Name: "x"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of missing id: got %v, want ErrNotFound", err)
	}

	if _, e := svc.handle.Exec("UPDATE country SET deleted_at = datetime('now') WHERE id = ?", id); e != nil {
		t.Fatalf("soft delete: %v", e)
	}
	if err := svc.UpdateCountryWithContext(ctx, types.Country{ID: id, Prefix: "K1", Name: "zombie"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of soft-deleted row: got %v, want ErrNotFound", err)
	}
}

// --- Timestamp storage: Go-written DATETIME values are SQLite-canonical UTC ---
// (review 2026-07-05 finding 1, fix-forward half). modernc's DEFAULT time.Time
// serialisation baked in the monotonic-clock suffix (`m=+…`, meaningless across
// processes) and a local-zone name — a string SQLite's own datetime() returns
// NULL on, and the wrong basis for the SM Cloud reconcile on qso.modified_at.
// The `_time_format=sqlite` DSN option + `time.Now().UTC()` writers make every
// Go-written stamp canonical UTC. (The trigger/default `localtime`→UTC change +
// the normalisation of pre-fix debris rows ship in a STAGED migration — this
// test covers only the Go-writer fix-forward.)
func TestModifiedAt_StoredCanonicalUTC(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	id, err := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	qso.ID = id
	// An operator edit is the Go writer path that sets modified_at = time.Now().UTC().
	qso.QsoDetails.RstRcvd = "599"
	if err := svc.UpdateQsoWithContext(ctx, qso); err != nil {
		t.Fatalf("update qso: %v", err)
	}

	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	// quote() returns the RAW stored bytes — the on-disk format the SM Cloud
	// reconcile reads. (Scanning `modified_at` into a Go string would show
	// modernc's RFC3339 re-render of the parsed time, not what's on disk.)
	var stored string
	var parsed sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT quote(modified_at), datetime(modified_at) FROM qso WHERE id = ?`, id,
	).Scan(&stored, &parsed); err != nil {
		_ = tx.Rollback()
		t.Fatalf("read modified_at: %v", err)
	}
	_ = tx.Rollback()

	// No monotonic-clock debris (the old time.Time.String() shape).
	if strings.Contains(stored, "m=+") || strings.Contains(stored, " m=") {
		t.Errorf("modified_at carries monotonic-clock debris: %s", stored)
	}
	// SQLite can parse its own column — datetime() returned NULL on the old format.
	if !parsed.Valid || parsed.String == "" {
		t.Errorf("datetime(modified_at) is NULL — value not SQLite-canonical: stored=%s", stored)
	}
	// Serialised as UTC (+00:00), not the writer's local zone.
	if !strings.Contains(stored, "+00:00") {
		t.Errorf("modified_at not stored as UTC (+00:00): %s", stored)
	}
}

// --- M1: Initialize is retryable after a failed first call ------------------

func TestInitialize_RetryableAfterFailure(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	logSvc := &logging.Service{}
	logSvc.ConfigService = cfgSvc
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}

	svc := &Service{}
	svc.LoggerService = logSvc
	// ConfigService deliberately nil — the first Initialize must fail.
	if err := svc.Initialize(); err == nil {
		t.Fatal("Initialize should fail with no config service injected")
	}
	if svc.isInitialized.Load() {
		t.Fatal("isInitialized set despite a failed Initialize (must not latch on failure)")
	}

	// Fix the injection and retry — must actually initialise this time (the old
	// sync.Once consumed the guard and returned nil-but-uninitialised).
	svc.ConfigService = cfgSvc
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize retry after fixing config should succeed: %v", err)
	}
	if !svc.isInitialized.Load() {
		t.Fatal("isInitialized not set after a successful retry")
	}
	if svc.DatabaseConfig == nil {
		t.Fatal("DatabaseConfig not set after a successful retry")
	}

	// And Open can now proceed.
	svc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := svc.Open(); err != nil {
		t.Fatalf("Open after successful retry: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
}

// --- L1: every durable country writer rejects wildcard prefixes -------------

func TestCountryWriters_RejectWildcardPrefix(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	for _, bad := range []string{"M%", "_", `M\A`, "", "   "} {
		c := types.Country{Prefix: bad, Name: "X"}
		if _, err := svc.InsertCountryWithContext(ctx, c); err == nil {
			t.Errorf("InsertCountry accepted invalid prefix %q", bad)
		}
		if err := svc.UpdateCountryWithContext(ctx, c); err == nil {
			t.Errorf("UpdateCountry accepted invalid prefix %q", bad)
		}
		if err := svc.UpsertCountryWithContext(ctx, c); err == nil {
			t.Errorf("UpsertCountry accepted invalid prefix %q", bad)
		}
	}

	// A plain alphanumeric prefix is accepted by the durable writer.
	if err := svc.UpsertCountryWithContext(ctx, types.Country{Prefix: "K1", Name: "United States"}); err != nil {
		t.Errorf("UpsertCountry rejected a valid prefix: %v", err)
	}
}
