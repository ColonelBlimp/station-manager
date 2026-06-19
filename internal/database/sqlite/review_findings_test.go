package sqlite

import (
	"context"
	stderr "errors"
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

	id, err := svc.InsertCountryWithContext(ctx, types.Country{Prefix: "K", Name: "United States"})
	if err != nil {
		t.Fatalf("insert country: %v", err)
	}

	if err := svc.UpdateCountryWithContext(ctx, types.Country{ID: id, Prefix: "K", Name: "USA"}); err != nil {
		t.Fatalf("active update should succeed: %v", err)
	}

	if err := svc.UpdateCountryWithContext(ctx, types.Country{ID: 999999, Prefix: "K", Name: "x"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of missing id: got %v, want ErrNotFound", err)
	}

	if _, e := svc.handle.Exec("UPDATE country SET deleted_at = datetime('now') WHERE id = ?", id); e != nil {
		t.Fatalf("soft delete: %v", e)
	}
	if err := svc.UpdateCountryWithContext(ctx, types.Country{ID: id, Prefix: "K", Name: "zombie"}); !stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("update of soft-deleted row: got %v, want ErrNotFound", err)
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
	if err := svc.UpsertCountryWithContext(ctx, types.Country{Prefix: "K", Name: "United States"}); err != nil {
		t.Errorf("UpsertCountry rejected a valid prefix: %v", err)
	}
}
