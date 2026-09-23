package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// In-place adoption of the existing installation as its first archive (ADR
// 0071 AC 8; W-0021 slice 1, rulings (a)/(b) 2026-09-22). Database-first: the
// file's identity is written and read back, THEN the catalogue entry is
// persisted with that UUID. A restart never mints a second identity, a
// catalogue that names a different archive for this file fails closed, and the
// default logbook pointer is projected from the file.

// adoptDeps builds the daemon's config, logger and database services on a REAL
// file (newTestDepsWithCfg pins ":memory:" after initialisation, and adoption
// records the file's canonical path, which ":memory:" has none of).
func adoptDeps(t *testing.T, mut func(*config.Config)) (*sqlite.Service, *logging.Service, *config.Service, string) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "db", "station-manager.db")
	cfg := config.DefaultConfig(tmp)
	cfg.Datastore.Path = dbPath
	cfg.SetupComplete = true
	cfg.DefaultLogbookID = 1
	cfg.LoggingStation.StationCallsign = "7Q5MLV"
	cfg.Logging.FileLogging = false
	if mut != nil {
		mut(&cfg)
	}
	cfgSvc := config.New(cfg)
	cfgPath := filepath.Join(tmp, "config.json")
	if _, err := config.WriteJSON(cfgPath, cfg); err != nil {
		t.Fatalf("seed config.json: %v", err)
	}
	cfgSvc.SetPath(cfgPath)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	logSvc := &logging.Service{WorkingDir: cfgSvc.WorkingDir(), ConfigService: cfgSvc}
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	dbSvc := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := dbSvc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	dbSvc.SetMigrationSets(sqlite.MigrationSetLog)
	if err := dbSvc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := dbSvc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	t.Cleanup(func() { _ = dbSvc.Close(); _ = logSvc.Close() })
	return dbSvc, logSvc, cfgSvc, dbPath
}

func catalogueOnDisk(t *testing.T, cfgSvc *config.Service) config.Config {
	t.Helper()
	data, err := os.ReadFile(cfgSvc.Path)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var doc struct {
		Version int                      `json:"version"`
		Active  string                   `json:"active_qso_archive_id"`
		Entries []types.QsoArchiveConfig `json:"qso_archives"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	if doc.Version != 5 {
		t.Fatalf("config.json version = %d, want 5 after adoption", doc.Version)
	}
	return config.Config{ActiveQsoArchiveID: doc.Active, QsoArchives: doc.Entries}
}

func TestAdoptArchive_FreshInstallRegisteredInPlaceThenIdempotent(t *testing.T) {
	db, logger, cfgSvc, dbPath := adoptDeps(t, nil)
	ctx := context.Background()
	if err := ensureDefaultLogbook(ctx, db, cfgSvc, logger); err != nil {
		t.Fatalf("ensureDefaultLogbook: %v", err)
	}

	if err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	identity, err := db.ArchiveIdentityWithContext(ctx)
	if err != nil {
		t.Fatalf("identity after adopt: %v", err)
	}
	snap := cfgSvc.Snapshot()
	if snap.ActiveQsoArchiveID != identity.ArchiveUUID {
		t.Fatalf("active = %q, want the file's uuid %q", snap.ActiveQsoArchiveID, identity.ArchiveUUID)
	}
	entry := snap.QsoArchiveByID(identity.ArchiveUUID)
	if entry == nil || entry.Ownership != types.QsoArchiveOwnershipLegacy || entry.Label == "" {
		t.Fatalf("catalogue entry = %+v; want a labelled legacy entry", entry)
	}
	wantPath, _ := filepath.EvalSymlinks(dbPath)
	if entry.Path != wantPath || !filepath.IsAbs(entry.Path) {
		t.Fatalf("entry path = %q, want the canonical absolute %q", entry.Path, wantPath)
	}
	if identity.DefaultLogbookID != 1 {
		t.Fatalf("file default logbook = %d, want 1 (taken from config at adoption)", identity.DefaultLogbookID)
	}
	// Persisted, not just in memory.
	disk := catalogueOnDisk(t, cfgSvc)
	if disk.ActiveQsoArchiveID != identity.ArchiveUUID || len(disk.QsoArchives) != 1 {
		t.Fatalf("on disk: active %q, %d entries", disk.ActiveQsoArchiveID, len(disk.QsoArchives))
	}
	lb, _ := db.FetchLogbookByIDWithContext(ctx, 1)
	if !utils.IsValidUUIDv7(lb.UUID) {
		t.Fatalf("default logbook uuid = %q after adoption", lb.UUID)
	}

	// Second start: nothing is minted or rewritten.
	if err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc)); err != nil {
		t.Fatalf("adopt again: %v", err)
	}
	again, _ := db.ArchiveIdentityWithContext(ctx)
	lb2, _ := db.FetchLogbookByIDWithContext(ctx, 1)
	snap2 := cfgSvc.Snapshot()
	if again != identity || lb2.UUID != lb.UUID || snap2.ActiveQsoArchiveID != snap.ActiveQsoArchiveID || len(snap2.QsoArchives) != 1 {
		t.Fatal("a second adoption changed an identity")
	}
}

// A crash between "identity written to the file" and "catalogue persisted":
// the next start finds the embedded UUID and registers exactly that one.
func TestAdoptArchive_ReusesAnIdentityAlreadyInTheFile(t *testing.T) {
	db, logger, cfgSvc, _ := adoptDeps(t, nil)
	ctx := context.Background()
	pre, err := db.EnsureArchiveIdentityWithContext(ctx, 0)
	if err != nil {
		t.Fatalf("pre-write identity: %v", err)
	}
	if err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := cfgSvc.Snapshot().ActiveQsoArchiveID; got != pre.ArchiveUUID {
		t.Fatalf("active = %q, want the pre-existing file uuid %q (no second mint)", got, pre.ArchiveUUID)
	}
}

// The catalogue names one archive, the file holds another: refuse to start
// (ADR 0071: a wrong file at a registered path fails closed), touching nothing.
func TestAdoptArchive_CatalogueUUIDMismatchFailsClosed(t *testing.T) {
	const other = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	db, logger, cfgSvc, _ := adoptDeps(t, func(c *config.Config) {
		c.QsoArchives = []types.QsoArchiveConfig{{ID: other, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: c.Datastore.Path}}
		c.ActiveQsoArchiveID = other
	})
	ctx := context.Background()
	if _, err := db.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatalf("identity: %v", err)
	}
	err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc))
	if err == nil {
		t.Fatal("adoption accepted a file whose uuid differs from the catalogue's active entry")
	}
	if !strings.Contains(err.Error(), other) {
		t.Fatalf("refusal %q does not name the catalogue's uuid", err.Error())
	}
	if got := cfgSvc.Snapshot().ActiveQsoArchiveID; got != other {
		t.Fatalf("refusal rewrote the catalogue: active = %q", got)
	}
}

// default_logbook_id is a projection of the file (ADR 0071): when the file
// names an existing default, config takes it; when the file has none, the file
// learns config's (already self-healed) value.
func TestAdoptArchive_ProjectsDefaultLogbookFromTheFile(t *testing.T) {
	db, logger, cfgSvc, _ := adoptDeps(t, nil)
	ctx := context.Background()
	if err := ensureDefaultLogbook(ctx, db, cfgSvc, logger); err != nil {
		t.Fatal(err)
	}
	second, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Contest", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	// File says the default is the second logbook; config still says 1.
	if _, err := db.EnsureArchiveIdentityWithContext(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := cfgSvc.Snapshot().DefaultLogbookID; got != second {
		t.Fatalf("config default_logbook_id = %d, want the file's %d", got, second)
	}

	// The other direction: a file with no default learns config's value.
	db2, logger2, cfgSvc2, _ := adoptDeps(t, nil)
	if err := ensureDefaultLogbook(ctx, db2, cfgSvc2, logger2); err != nil {
		t.Fatal(err)
	}
	if _, err := db2.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := adoptArchive(ctx, db2, cfgSvc2, logger2, resolvedPaths(t, cfgSvc2)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if id, _ := db2.ArchiveIdentityWithContext(ctx); id.DefaultLogbookID != 1 {
		t.Fatalf("file default logbook = %d, want config's 1", id.DefaultLogbookID)
	}
}

// A stale config default must not run the old self-heal before adoption can
// project the file's authoritative default. The stale row is soft-deleted and
// another live row already owns the name self-heal would try to insert.
func TestAdoptArchive_FileDefaultWinsBeforeConfigSelfHeal(t *testing.T) {
	db, logger, cfgSvc, _ := adoptDeps(t, nil)
	ctx := context.Background()
	stale, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Stale", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	wanted, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Default", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	if stale != 1 || wanted != 2 {
		t.Fatalf("fixture IDs = %d, %d; want 1, 2", stale, wanted)
	}
	if err := db.DeleteLogbookByIDWithContext(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureArchiveIdentityWithContext(ctx, wanted); err != nil {
		t.Fatal(err)
	}
	if err := ensureDefaultLogbook(ctx, db, cfgSvc, logger); err != nil {
		t.Fatalf("stale config self-heal ran before file default projection: %v", err)
	}
	if err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc)); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := cfgSvc.Snapshot().DefaultLogbookID; got != wanted {
		t.Fatalf("config default = %d, want file's %d", got, wanted)
	}
	if logbooks, err := db.FetchAllLogbooksWithContext(ctx); err != nil || len(logbooks) != 1 {
		t.Fatalf("live logbooks = %d (%v), want only the file's default", len(logbooks), err)
	}
}

// A catalogue persist failure is not fatal — the identity is in the file, the
// in-memory catalogue is set, and the NEXT start (with a writable config)
// registers the same UUID.
func TestAdoptArchive_CataloguePersistFailureIsRetriedNextStartWithoutANewMint(t *testing.T) {
	db, logger, cfgSvc, _ := adoptDeps(t, nil)
	ctx := context.Background()
	dir := filepath.Dir(cfgSvc.Path)
	if err := os.Chmod(dir, 0o500); err != nil { // config.json cannot be replaced
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc)); err != nil {
		t.Fatalf("adopt with an unwritable config must warn and continue, got %v", err)
	}
	first, _ := db.ArchiveIdentityWithContext(ctx)
	if cfgSvc.Snapshot().ActiveQsoArchiveID != first.ArchiveUUID {
		t.Fatal("in-memory catalogue not set after a persist failure")
	}
	_ = os.Chmod(dir, 0o700)
	if err := adoptArchive(ctx, db, cfgSvc, logger, resolvedPaths(t, cfgSvc)); err != nil {
		t.Fatalf("adopt after the directory is writable again: %v", err)
	}
	disk := catalogueOnDisk(t, cfgSvc)
	if disk.ActiveQsoArchiveID != first.ArchiveUUID {
		t.Fatalf("on disk active = %q, want the first identity %q", disk.ActiveQsoArchiveID, first.ArchiveUUID)
	}
}

// resolvedPaths is what run() hands the daemon: the effective selection.
func resolvedPaths(t *testing.T, cfgSvc *config.Service) archive.Paths {
	t.Helper()
	p, err := archive.ResolveEffective(cfgSvc.Snapshot())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return p
}
