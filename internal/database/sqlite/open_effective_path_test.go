package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// Review of W-0021 2A: after adoption the catalogue selects the QSO file and
// datastore.path may go stale. The database directory is therefore checked and
// created for the EFFECTIVE path at Open (after SetDatabasePath), not for
// datastore.path at Initialize — a stale, uncreatable datastore.path must not
// stop a daemon whose active archive is elsewhere.
func TestOpen_ChecksTheEffectivePathNotDatastorePath(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(tmp)
	cfg.Datastore.Path = filepath.Join(blocker, "stale", "station-manager.db") // its directory cannot be created
	cfg.Logging.FileLogging = false
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logSvc := &logging.Service{WorkingDir: cfgSvc.WorkingDir(), ConfigService: cfgSvc}
	if err := logSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logSvc.Close() })
	svc := &Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize must not touch datastore.path's directory: %v", err)
	}
	good := filepath.Join(tmp, "db", "qso-archives", "b.db")
	svc.SetDatabasePath(good)
	svc.SetMigrationSets(MigrationSetLog)
	if err := svc.Open(); err != nil {
		t.Fatalf("Open on the effective path: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if fi, err := os.Stat(filepath.Dir(good)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("effective directory not created 0700: %v %v", fi, err)
	}
}
