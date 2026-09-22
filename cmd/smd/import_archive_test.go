package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 slice 2 (review finding 2): `smd import` resolves its target through
// the same catalogue as the daemon — the ACTIVE archive by default, a named one
// with --archive — and never through datastore.path once a catalogue exists.
// Global stores stay under <data_dir>/db whatever the QSO file's directory.

const archA = "019fd5c5-efcc-7193-be4f-1fee532ee315"
const archB = "019fd5c5-efcc-7193-be4f-1fee532ee316"

// provisionManagedFile creates an empty, migrated archive at the managed path
// (what slice 2C's provisioner will do; here by hand so the resolver is under test).
func provisionManagedFile(t *testing.T, cfg config.Config, id string) string {
	t.Helper()
	path := archive.PathFor(cfg, types.QsoArchiveConfig{ID: id, Ownership: types.QsoArchiveOwnershipManaged})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	c := cfg
	c.Datastore.Path = path
	cfgSvc := config.New(c)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logSvc := &logging.Service{WorkingDir: cfgSvc.WorkingDir(), ConfigService: cfgSvc}
	if err := logSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	db := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := db.Initialize(); err != nil {
		t.Fatal(err)
	}
	db.SetMigrationSets(sqlite.MigrationSetLog)
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	lbID, err := db.InsertLogbookWithContext(context.Background(), types.Logbook{Name: "Contest", Callsign: "M0TEST"})
	if err != nil {
		t.Fatal(err)
	}
	// A provisioned archive carries its identity and its default logbook (what
	// slice 2C's provisioner writes); a file without one is refused as a target.
	if _, err := db.EnsureArchiveIdentityWithContext(context.Background(), lbID); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	_ = logSvc.Close()
	return path
}

func openArchiveFile(t *testing.T, cfg config.Config, path string) *sqlite.Service {
	t.Helper()
	c := cfg
	c.Datastore.Path = path
	cfgSvc := config.New(c)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logSvc := &logging.Service{WorkingDir: cfgSvc.WorkingDir(), ConfigService: cfgSvc}
	if err := logSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	db := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := db.Initialize(); err != nil {
		t.Fatal(err)
	}
	db.SetMigrationSets(sqlite.MigrationSetLog)
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); _ = logSvc.Close() })
	return db
}

func TestImport_TargetsTheActiveArchiveOrTheNamedOne(t *testing.T) {
	var legacyPath string
	tmp := setupImportTestbed(t, func(c *config.Config) {
		legacyPath = c.Datastore.Path
		c.QsoArchives = []types.QsoArchiveConfig{
			{ID: archA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: legacyPath},
			{ID: archB, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged},
		}
		c.ActiveQsoArchiveID = archA
	})
	cfg, err := config.Load(filepath.Join(tmp, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	managedPath := provisionManagedFile(t, cfg, archB)
	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord)

	// Default: the ACTIVE archive (A).
	if err := runImport([]string{adifPath}); err != nil {
		t.Fatalf("import (active): %v", err)
	}
	a := reopenDB(t, tmp)
	if q, _ := a.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1); len(q) != 1 {
		t.Fatalf("active archive has %d QSOs after the default import, want 1", len(q))
	}
	b := openArchiveFile(t, cfg, managedPath)
	if q, _ := b.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1); len(q) != 0 {
		t.Fatalf("the inactive archive received %d QSOs from a default import", len(q))
	}

	// --archive B: the named one, with the daemon stopped.
	adif2 := writeADIF(t, tmp, "input2.adi", secondRecord)
	if err := runImport([]string{"--archive", archB, adif2}); err != nil {
		t.Fatalf("import --archive: %v", err)
	}
	if q, _ := b.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1); len(q) != 1 {
		t.Fatalf("named archive has %d QSOs after --archive import, want 1", len(q))
	}
	if q, _ := a.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1); len(q) != 1 {
		t.Fatalf("active archive changed under an --archive import to another: %d QSOs", len(q))
	}

	// An unknown archive is refused by name, before any write.
	if err := runImport([]string{"--archive", "019fd5c5-efcc-7193-be4f-1fee532ee399", adif2}); err == nil {
		t.Fatal("unknown --archive accepted")
	}
}

// reference.db is station-global: with an EXTERNAL QSO file and no catalogue
// (pre-adoption), import must create it under <data_dir>/db, not beside the QSO file.
func TestImport_ReferenceDBIsGlobalNotBesideAnExternalQsoFile(t *testing.T) {
	external := filepath.Join(t.TempDir(), "elsewhere", "log.db")
	if err := os.MkdirAll(filepath.Dir(external), 0o700); err != nil {
		t.Fatal(err)
	}
	tmp := setupImportTestbed(t, func(c *config.Config) { c.Datastore.Path = external })
	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord)
	if err := runImport([]string{adifPath}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "db", "reference.db")); err != nil {
		t.Fatalf("global reference.db missing under <data_dir>/db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(external), "reference.db")); err == nil {
		t.Fatal("reference.db was created beside the external QSO file (derived from its directory)")
	}
}

// With --archive B the default logbook is B's own (the file's identity row),
// never the active archive's config projection: B's default may be a different
// id, or an id that in B names another logbook entirely.
func TestImport_ArchiveFlagUsesThatArchivesDefaultLogbook(t *testing.T) {
	var legacyPath string
	tmp := setupImportTestbed(t, func(c *config.Config) {
		legacyPath = c.Datastore.Path
		c.QsoArchives = []types.QsoArchiveConfig{
			{ID: archA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: legacyPath},
			{ID: archB, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged},
		}
		c.ActiveQsoArchiveID = archA
		c.DefaultLogbookID = 1 // A's default; must not leak into B
	})
	cfg, err := config.Load(filepath.Join(tmp, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	managedPath := provisionManagedFile(t, cfg, archB) // logbook 1 "Contest"
	b := openArchiveFile(t, cfg, managedPath)
	second, err := b.InsertLogbookWithContext(context.Background(), types.Logbook{Name: "Second", Callsign: "M0TEST"})
	if err != nil || second != 2 {
		t.Fatalf("insert second logbook: %v (%d)", err, second)
	}
	if err := b.SetArchiveDefaultLogbookWithContext(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()

	adifPath := writeADIF(t, tmp, "input.adi", sampleRecord)
	if err := runImport([]string{"--archive", archB, adifPath}); err != nil {
		t.Fatalf("import --archive: %v", err)
	}
	b2 := openArchiveFile(t, cfg, managedPath)
	if q, _ := b2.FetchQsoSliceByLogbookIdWithContext(context.Background(), 2); len(q) != 1 {
		t.Fatalf("B's default logbook (2) has %d QSOs, want 1", len(q))
	}
	if q, _ := b2.FetchQsoSliceByLogbookIdWithContext(context.Background(), 1); len(q) != 0 {
		t.Fatalf("B's logbook 1 (the ACTIVE archive's default id) received %d QSOs", len(q))
	}
}
