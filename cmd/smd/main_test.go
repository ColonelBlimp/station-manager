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

func TestLoadConfig_CwdFallback(t *testing.T) {
	t.Setenv("SM_WORKING_DIR", "")

	cwd := t.TempDir()
	t.Chdir(cwd)
	writeConfigJSON(t, filepath.Join(cwd, "config.json"))

	cfg, firstRunPath, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DataDir != cwd {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, cwd)
	}
	if firstRunPath != "" {
		t.Errorf("firstRunPath = %q, want empty when cwd config already existed", firstRunPath)
	}
}

func TestLoadConfig_FirstRunWritesDefaultInCwd(t *testing.T) {
	// Empty path, unset env, cwd has no config.json. Expectation:
	// loadConfig seeds ./config.json with DefaultConfig(cwd), then
	// loads it back. After the call the file MUST exist on disk —
	// that's the "discoverable, hand-editable" property the first-run
	// write delivers.
	t.Setenv("SM_WORKING_DIR", "")

	cwd := t.TempDir()
	t.Chdir(cwd)

	cfg, firstRunPath, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DataDir != cwd {
		t.Errorf("DataDir = %q, want %q (from os.Getwd)", cfg.DataDir, cwd)
	}
	if cfg.Datastore.Driver == "" {
		t.Error("DefaultConfig should populate Datastore.Driver")
	}

	written := filepath.Join(cwd, "config.json")
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("config.json was not written to cwd: %v", err)
	}
	if firstRunPath != written {
		t.Errorf("firstRunPath = %q, want %q", firstRunPath, written)
	}

	// Reload from the file directly — proves the written content is
	// well-formed JSON that the loader accepts on subsequent startups.
	reloaded, err := config.Load(written)
	if err != nil {
		t.Fatalf("reloading written config: %v", err)
	}
	if reloaded.DataDir != cwd {
		t.Errorf("reloaded DataDir = %q, want %q", reloaded.DataDir, cwd)
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
