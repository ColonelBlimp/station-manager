package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0084: before a clean close the daemon checkpoints the archive with
// TRUNCATE, so the main file holds everything and the WAL is empty while the
// connection is still open — the daemon test cannot see this, because SQLite's
// own last-connection close checkpoints too.
func TestCheckpointTruncate_EmptiesTheWALWhileOpen(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig(dir)
	cfg.Datastore.Path = filepath.Join(dir, "log.db")
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logSvc := &logging.Service{ConfigService: cfgSvc, WorkingDir: cfgSvc.WorkingDir()}
	if err := logSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	svc := &Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	svc.SetMigrationSets(MigrationSetLog)
	if err := svc.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(); _ = logSvc.Close() })
	if err := svc.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InsertLogbookWithContext(context.Background(), types.Logbook{Name: "L", Callsign: "G4ABC"}); err != nil {
		t.Fatal(err)
	}
	wal := cfg.Datastore.Path + "-wal"
	if fi, err := os.Stat(wal); err != nil || fi.Size() == 0 {
		t.Fatalf("fixture: expected WAL frames before the checkpoint (%v)", err)
	}
	if err := svc.CheckpointTruncateWithContext(context.Background()); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if fi, err := os.Stat(wal); err == nil && fi.Size() != 0 {
		t.Fatalf("WAL holds %d bytes after a TRUNCATE checkpoint", fi.Size())
	}
}
