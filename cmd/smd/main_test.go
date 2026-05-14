package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// testNoRetryDefaultType is a forwarder type name registered with a
// constructor but no default RetryConfig — used to exercise the
// "missing default retry" branch of spawnForwarderWorkers.
const testNoRetryDefaultType = "test-no-default-retry"

func init() {
	// Reuses stub.New (it's a valid Constructor signature and tolerates
	// an empty credentials blob). RegisterDefaultRetry is deliberately
	// NOT called for this type, so DefaultRetryFor returns ok=false.
	forwarding.Register(testNoRetryDefaultType, stub.New)
}

// newTestDeps builds a real *sqlite.Service and *logging.Service over
// an in-memory SQLite DB. Matches the pattern used across the codebase
// (see internal/forwarding/worker/worker_test.go). No mocks.
func newTestDeps(t *testing.T) (*sqlite.Service, *logging.Service) {
	t.Helper()

	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"

	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}

	logSvc := &logging.Service{
		WorkingDir:    cfgSvc.WorkingDir(),
		ConfigService: cfgSvc,
	}
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}

	dbSvc := &sqlite.Service{
		ConfigService: cfgSvc,
		LoggerService: logSvc,
	}
	if err := dbSvc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	dbSvc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := dbSvc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := dbSvc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}

	t.Cleanup(func() {
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})

	return dbSvc, logSvc
}

// spawnAndDrain runs spawnForwarderWorkers, immediately cancels the
// context, then waits on wg with a bounded timeout so any launched
// goroutines are joined before the test returns. Returns the spawn
// error (if any) and whether the wg drained cleanly.
func spawnAndDrain(t *testing.T, fwds []types.ForwarderConfig) (spawnErr error, drained bool) {
	t.Helper()
	db, logger := newTestDeps(t)
	hub := events.NewHub()
	t.Cleanup(func() { hub.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	spawnErr = spawnForwarderWorkers(ctx, &wg, fwds, db, logger, hub)

	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		drained = true
	case <-time.After(2 * time.Second):
		drained = false
	}
	return spawnErr, drained
}

func stubCreds(t *testing.T) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"mode": stub.ModeAlwaysSuccess})
	if err != nil {
		t.Fatalf("marshal stub creds: %v", err)
	}
	return b
}

func TestSpawnForwarderWorkers_HappyPath_Single(t *testing.T) {
	fwds := []types.ForwarderConfig{
		{
			Name:            "stub-one",
			Type:            stub.Type,
			Enabled:         true,
			Credentials:     stubCreds(t),
			TickIntervalSec: 1,
			BatchSize:       1,
		},
	}

	err, drained := spawnAndDrain(t, fwds)
	if err != nil {
		t.Fatalf("spawn returned error: %v", err)
	}
	if !drained {
		t.Fatal("worker goroutine did not exit within timeout after ctx cancel")
	}
}

func TestSpawnForwarderWorkers_HappyPath_Multi(t *testing.T) {
	fwds := []types.ForwarderConfig{
		{
			Name:            "stub-a",
			Type:            stub.Type,
			Enabled:         true,
			Credentials:     stubCreds(t),
			TickIntervalSec: 1,
			BatchSize:       1,
		},
		{
			Name:            "stub-b",
			Type:            stub.Type,
			Enabled:         true,
			Credentials:     stubCreds(t),
			TickIntervalSec: 1,
			BatchSize:       2,
		},
	}

	err, drained := spawnAndDrain(t, fwds)
	if err != nil {
		t.Fatalf("spawn returned error: %v", err)
	}
	if !drained {
		t.Fatal("worker goroutines did not exit within timeout after ctx cancel")
	}
}

func TestSpawnForwarderWorkers_DisabledSkipped(t *testing.T) {
	// Two configs, one disabled. A disabled entry must not build the
	// forwarder (so even an unregistered type wouldn't error), and must
	// not spawn a goroutine. Using an unregistered type on the disabled
	// entry proves the short-circuit runs before forwarding.Build.
	fwds := []types.ForwarderConfig{
		{
			Name:    "disabled-ghost",
			Type:    "nonexistent-type-ghost",
			Enabled: false,
		},
		{
			Name:            "stub-live",
			Type:            stub.Type,
			Enabled:         true,
			Credentials:     stubCreds(t),
			TickIntervalSec: 1,
			BatchSize:       1,
		},
	}

	err, drained := spawnAndDrain(t, fwds)
	if err != nil {
		t.Fatalf("spawn returned error (disabled entry should short-circuit): %v", err)
	}
	if !drained {
		t.Fatal("worker goroutine did not exit within timeout after ctx cancel")
	}
}

func TestSpawnForwarderWorkers_UnknownType(t *testing.T) {
	fwds := []types.ForwarderConfig{
		{
			Name:            "mystery",
			Type:            "nonexistent-type-xyz",
			Enabled:         true,
			TickIntervalSec: 1,
			BatchSize:       1,
		},
	}

	err, _ := spawnAndDrain(t, fwds)
	if err == nil {
		t.Fatal("expected error for unregistered forwarder type, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-type-xyz") {
		t.Errorf("error should mention the unknown type, got: %v", err)
	}
	if !strings.Contains(err.Error(), "mystery") {
		t.Errorf("error should mention the forwarder name, got: %v", err)
	}
}

func TestSpawnForwarderWorkers_MissingDefaultRetry(t *testing.T) {
	// testNoRetryDefaultType is registered in init() with a constructor
	// but no default retry. Without an explicit Retry block, spawn must
	// fail loudly rather than silently running with a zero-value retry.
	fwds := []types.ForwarderConfig{
		{
			Name:            "no-retry-config",
			Type:            testNoRetryDefaultType,
			Enabled:         true,
			Credentials:     stubCreds(t),
			TickIntervalSec: 1,
			BatchSize:       1,
		},
	}

	err, _ := spawnAndDrain(t, fwds)
	if err == nil {
		t.Fatal("expected error when no retry config and no default is registered, got nil")
	}
	if !strings.Contains(err.Error(), "no-retry-config") {
		t.Errorf("error should mention the forwarder name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "retry") {
		t.Errorf("error should mention retry, got: %v", err)
	}
}

func TestSpawnForwarderWorkers_ExplicitRetryOverridesMissingDefault(t *testing.T) {
	// Same type as the missing-default test, but with an explicit Retry
	// block. Operator config must win — spawn should succeed.
	fwds := []types.ForwarderConfig{
		{
			Name:            "explicit-retry",
			Type:            testNoRetryDefaultType,
			Enabled:         true,
			Credentials:     stubCreds(t),
			TickIntervalSec: 1,
			BatchSize:       1,
			Retry: &types.RetryConfig{
				MaxAttempts:       2,
				InitialBackoffSec: 1,
				MaxBackoffSec:     5,
			},
		},
	}

	err, drained := spawnAndDrain(t, fwds)
	if err != nil {
		t.Fatalf("spawn returned error with explicit retry: %v", err)
	}
	if !drained {
		t.Fatal("worker goroutine did not exit within timeout after ctx cancel")
	}
}

func TestSpawnForwarderWorkers_InvalidWorkerConfig(t *testing.T) {
	// TickIntervalSec=0 fails worker.New's validation (Tick must be > 0).
	// spawnForwarderWorkers must surface that as a startup error rather
	// than launching a doomed goroutine.
	fwds := []types.ForwarderConfig{
		{
			Name:            "bad-tick",
			Type:            stub.Type,
			Enabled:         true,
			Credentials:     stubCreds(t),
			TickIntervalSec: 0,
			BatchSize:       1,
		},
	}

	err, _ := spawnAndDrain(t, fwds)
	if err == nil {
		t.Fatal("expected worker construction error for tick=0, got nil")
	}
	if !strings.Contains(err.Error(), "bad-tick") {
		t.Errorf("error should mention the forwarder name, got: %v", err)
	}
}

func TestSpawnForwarderWorkers_EmptyList(t *testing.T) {
	// No forwarders configured: return nil, no goroutines, nothing to drain.
	err, drained := spawnAndDrain(t, nil)
	if err != nil {
		t.Fatalf("spawn with empty list returned error: %v", err)
	}
	if !drained {
		t.Fatal("empty spawn should drain instantly")
	}
}

// ---- loadConfig tests ----

func writeConfigJSON(t *testing.T, path string) {
	t.Helper()
	body := `{"data_dir": "` + filepath.Dir(path) + `"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestLoadConfig_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "explicit.json")
	writeConfigJSON(t, path)

	// Env shouldn't matter when path is explicit.
	t.Setenv("SM_WORKING_DIR", "/nonexistent")

	cfg, firstRunPath, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DataDir != dir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, dir)
	}
	if firstRunPath != "" {
		t.Errorf("firstRunPath = %q, want empty for explicit existing path", firstRunPath)
	}
}

func TestLoadConfig_EnvVarPath(t *testing.T) {
	// Chdir to an empty dir so cwd-based fallback can't be reached first.
	emptyCwd := t.TempDir()
	t.Chdir(emptyCwd)

	envDir := t.TempDir()
	writeConfigJSON(t, filepath.Join(envDir, "config.json"))
	t.Setenv("SM_WORKING_DIR", envDir)

	cfg, firstRunPath, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DataDir != envDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, envDir)
	}
	if firstRunPath != "" {
		t.Errorf("firstRunPath = %q, want empty when env-dir config already existed", firstRunPath)
	}
}

// TestLoadConfig_CwdFallback removed 2026-05-14. The cwd-as-fallback
// behaviour it exercised was a regression vector — a stray config.json
// in $HOME (the systemd unit's default cwd) silently preempted the
// real install at SM_WORKING_DIR. defaultConfigPath now delegates to
// utils.WorkingDir(); explicit --config remains for per-invocation
// overrides.

func TestLoadConfig_FirstRunWritesDefaultInWorkingDir(t *testing.T) {
	// Empty path, SM_WORKING_DIR pointing at a fresh tempdir, no
	// cwd config. Expectation: loadConfig seeds <workdir>/config.json
	// with DefaultConfig(<workdir>), then loads it back. After the
	// call the file MUST exist on disk — the "discoverable,
	// hand-editable" first-run write property.
	//
	// Replaces the pre-2026-05-14 TestLoadConfig_FirstRunWritesDefaultInCwd
	// which exercised the (now removed) cwd-fallback. The deployed
	// daemon is at /usr/bin/smd with SM_WORKING_DIR set via the
	// systemd unit (or via utils.WorkingDir's XDG fallback); no
	// daemon use case ever seeds into cwd, so the test follows.
	workdir := t.TempDir()
	t.Setenv("SM_WORKING_DIR", workdir)

	// Chdir away to confirm the resolution does NOT touch cwd.
	otherCwd := t.TempDir()
	t.Chdir(otherCwd)

	cfg, firstRunPath, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DataDir != workdir {
		t.Errorf("DataDir = %q, want %q (from SM_WORKING_DIR)", cfg.DataDir, workdir)
	}
	if cfg.Datastore.Driver == "" {
		t.Error("DefaultConfig should populate Datastore.Driver")
	}

	written := filepath.Join(workdir, "config.json")
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("config.json was not written to workdir: %v", err)
	}
	if firstRunPath != written {
		t.Errorf("firstRunPath = %q, want %q", firstRunPath, written)
	}

	// Cwd MUST NOT have grown a stray config.json — that was the
	// 2026-05-14 trap that motivated this refactor.
	if _, err := os.Stat(filepath.Join(otherCwd, "config.json")); err == nil {
		t.Errorf("stray config.json appeared in cwd %q despite SM_WORKING_DIR set", otherCwd)
	}

	// Reload from the file directly — proves the written content is
	// well-formed JSON that the loader accepts on subsequent startups.
	reloaded, err := config.Load(written)
	if err != nil {
		t.Fatalf("reloading written config: %v", err)
	}
	if reloaded.DataDir != workdir {
		t.Errorf("reloaded DataDir = %q, want %q", reloaded.DataDir, workdir)
	}
}

func TestLoadConfig_FirstRunWritesDefaultInEnvDir(t *testing.T) {
	// SM_WORKING_DIR set, but the dir has no config.json. Expectation:
	// loadConfig seeds $SM_WORKING_DIR/config.json (not the cwd
	// fallback), then loads it back.
	envDir := t.TempDir()
	t.Setenv("SM_WORKING_DIR", envDir)

	emptyCwd := t.TempDir()
	t.Chdir(emptyCwd)

	cfg, firstRunPath, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DataDir != envDir {
		t.Errorf("DataDir = %q, want %q (env dir, not cwd)", cfg.DataDir, envDir)
	}

	written := filepath.Join(envDir, "config.json")
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("config.json was not written to env dir: %v", err)
	}
	if firstRunPath != written {
		t.Errorf("firstRunPath = %q, want %q", firstRunPath, written)
	}
	if _, err := os.Stat(filepath.Join(emptyCwd, "config.json")); err == nil {
		t.Error("config.json should NOT have been written to cwd when SM_WORKING_DIR is set")
	}
}

// ----- ensureDefaultLogbook -----

// newTestDepsWithCfg is newTestDeps' sibling; returns the cfgSvc too so
// ensureDefaultLogbook tests can inspect / mutate the snapshot. Stays
// adjacent to keep the deps-construction shape identical.
//
// SetPath is called with a real temp-file path so cfgSvc.Update()
// (which the helper-under-test invokes after correcting a drifted ID)
// can persist back; tests that don't exercise the persist path are
// unaffected.
func newTestDepsWithCfg(t *testing.T, mut func(*config.Config)) (*sqlite.Service, *logging.Service, *config.Service) {
	t.Helper()
	tmp := t.TempDir()
	cfg := config.DefaultConfig(tmp)
	cfg.Datastore.Path = ":memory:"
	if mut != nil {
		mut(&cfg)
	}
	cfgSvc := config.New(cfg)
	cfgPath := filepath.Join(tmp, "config.json")
	if err := config.WriteJSON(cfgPath, cfg); err != nil {
		t.Fatalf("seed config.json: %v", err)
	}
	cfgSvc.SetPath(cfgPath)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	logSvc := &logging.Service{
		WorkingDir:    cfgSvc.WorkingDir(),
		ConfigService: cfgSvc,
	}
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	dbSvc := &sqlite.Service{
		ConfigService: cfgSvc,
		LoggerService: logSvc,
	}
	if err := dbSvc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	dbSvc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := dbSvc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := dbSvc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})
	return dbSvc, logSvc, cfgSvc
}

// TestEnsureDefaultLogbook_SeedsRowWhenMissing covers the headline
// failure mode: config marks setup_complete=true and points at a
// logbook id that doesn't exist in the DB (e.g. operator nuked the
// dev DB without clearing setup_complete). Daemon must self-heal so
// the first QSO submit doesn't FK-violate.
func TestEnsureDefaultLogbook_SeedsRowWhenMissing(t *testing.T) {
	db, logger, cfgSvc := newTestDepsWithCfg(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})

	if err := ensureDefaultLogbook(context.Background(), db, cfgSvc, logger); err != nil {
		t.Fatalf("ensureDefaultLogbook: %v", err)
	}

	got, err := db.FetchLogbookByIDWithContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("logbook id=1 should exist after seeding; got %v", err)
	}
	if got.Callsign != "7Q5MLV" {
		t.Errorf("seeded callsign = %q, want 7Q5MLV", got.Callsign)
	}
}

// TestEnsureDefaultLogbook_NoOpWhenRowExists confirms the helper is
// idempotent — running it on a healthy DB doesn't insert a duplicate
// or change anything.
func TestEnsureDefaultLogbook_NoOpWhenRowExists(t *testing.T) {
	db, logger, cfgSvc := newTestDepsWithCfg(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})

	preID, err := db.InsertLogbookWithContext(context.Background(), types.Logbook{
		Name:     "Existing",
		Callsign: "7Q5MLV",
	})
	if err != nil {
		t.Fatalf("seed existing logbook: %v", err)
	}

	if err := ensureDefaultLogbook(context.Background(), db, cfgSvc, logger); err != nil {
		t.Fatalf("ensureDefaultLogbook: %v", err)
	}

	got, err := db.FetchLogbookByIDWithContext(context.Background(), preID)
	if err != nil {
		t.Fatalf("existing logbook should still be there: %v", err)
	}
	if got.Name != "Existing" {
		t.Errorf("existing logbook clobbered: name = %q, want Existing", got.Name)
	}
}

// TestEnsureDefaultLogbook_NoOpWhenSetupIncomplete — first-run setup
// path must not pre-seed; PUT /v1/config owns that transition. Any
// pre-seeding here would race the setup handler.
func TestEnsureDefaultLogbook_NoOpWhenSetupIncomplete(t *testing.T) {
	db, logger, cfgSvc := newTestDepsWithCfg(t, func(c *config.Config) {
		c.SetupComplete = false
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})

	if err := ensureDefaultLogbook(context.Background(), db, cfgSvc, logger); err != nil {
		t.Fatalf("ensureDefaultLogbook: %v", err)
	}

	if _, err := db.FetchLogbookByIDWithContext(context.Background(), 1); err == nil {
		t.Error("logbook should NOT exist when SetupComplete=false (first-run path is PUT /v1/config's job)")
	}
}

// TestEnsureDefaultLogbook_NoOpWhenCallsignEmpty — operator broke
// config (setup_complete=true with an empty callsign somehow). Daemon
// must come up with a warning rather than failing startup or seeding
// a nonsense callsign-less row.
func TestEnsureDefaultLogbook_NoOpWhenCallsignEmpty(t *testing.T) {
	db, logger, cfgSvc := newTestDepsWithCfg(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "" // explicitly broken
	})

	if err := ensureDefaultLogbook(context.Background(), db, cfgSvc, logger); err != nil {
		t.Fatalf("ensureDefaultLogbook returned an error; should warn-and-return-nil: %v", err)
	}
	if _, err := db.FetchLogbookByIDWithContext(context.Background(), 1); err == nil {
		t.Error("logbook should NOT have been seeded with an empty callsign")
	}
}

// TestEnsureDefaultLogbook_PersistsCorrectedID — the operator
// hand-edited DefaultLogbookID to a non-1 value, then nuked the DB.
// AUTOINCREMENT will assign 1 on insert; helper must persist the
// corrected ID back to config so downstream reads stay consistent.
func TestEnsureDefaultLogbook_PersistsCorrectedID(t *testing.T) {
	db, logger, cfgSvc := newTestDepsWithCfg(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 42 // hand-edited; DB will assign 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})

	if err := ensureDefaultLogbook(context.Background(), db, cfgSvc, logger); err != nil {
		t.Fatalf("ensureDefaultLogbook: %v", err)
	}

	updated := cfgSvc.Snapshot()
	if updated.DefaultLogbookID == 42 {
		t.Fatal("DefaultLogbookID was not corrected after seeding (still 42; AUTOINCREMENT would have assigned 1)")
	}
	if updated.DefaultLogbookID < 1 {
		t.Errorf("corrected DefaultLogbookID = %d, want >= 1", updated.DefaultLogbookID)
	}
	if _, err := db.FetchLogbookByIDWithContext(context.Background(), updated.DefaultLogbookID); err != nil {
		t.Errorf("logbook at corrected id %d not found: %v", updated.DefaultLogbookID, err)
	}
}

// TestEnsureDefaultLogbook_WarnsWhenDefaultIDInvalid — config has
// nonsense (zero or negative DefaultLogbookID). Helper warns and
// returns nil so the daemon still starts; QSO submit will surface
// the broken config to the operator.
func TestEnsureDefaultLogbook_WarnsWhenDefaultIDInvalid(t *testing.T) {
	db, logger, cfgSvc := newTestDepsWithCfg(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 0 // invalid
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})

	if err := ensureDefaultLogbook(context.Background(), db, cfgSvc, logger); err != nil {
		t.Fatalf("ensureDefaultLogbook returned an error; should warn-and-return-nil: %v", err)
	}
}
