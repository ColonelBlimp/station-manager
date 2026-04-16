package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	content := `{
		"data_dir": "/tmp/test-data",
		"socket_path": "/tmp/test.sock",
		"datastore": {
			"driver": "sqlite",
			"path": "/tmp/test.db"
		},
		"logging": {
			"level": "warn",
			"console_logging": true
		}
	}`

	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.DataDir != "/tmp/test-data" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "/tmp/test-data")
	}
	if cfg.SocketPath != "/tmp/test.sock" {
		t.Fatalf("SocketPath = %q, want %q", cfg.SocketPath, "/tmp/test.sock")
	}
	if cfg.Datastore.Path != "/tmp/test.db" {
		t.Fatalf("Datastore.Path = %q, want %q", cfg.Datastore.Path, "/tmp/test.db")
	}
	if cfg.Logging.Level != "warn" {
		t.Fatalf("Logging.Level = %q, want %q", cfg.Logging.Level, "warn")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("Load expected error for missing file, got nil")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	if err := os.WriteFile(cfgFile, []byte("not json"), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	_, err := Load(cfgFile)
	if err == nil {
		t.Fatal("Load expected error for invalid JSON, got nil")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	// Minimal config — most fields omitted, should get defaults.
	if err := os.WriteFile(cfgFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// DataDir defaults to the config file's directory.
	if cfg.DataDir != dir {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, dir)
	}

	// Server defaults
	if cfg.Server.ReadTimeoutSec != 10 {
		t.Fatalf("Server.ReadTimeoutSec = %d, want 10", cfg.Server.ReadTimeoutSec)
	}
	if cfg.Server.ShutdownTimeoutSec != 10 {
		t.Fatalf("Server.ShutdownTimeoutSec = %d, want 10", cfg.Server.ShutdownTimeoutSec)
	}
	if cfg.Server.MaxBodyBytes != 1<<20 {
		t.Fatalf("Server.MaxBodyBytes = %d, want %d", cfg.Server.MaxBodyBytes, 1<<20)
	}

	// Datastore defaults
	if cfg.Datastore.Driver != types.SqliteDriverName {
		t.Fatalf("Datastore.Driver = %q, want %q", cfg.Datastore.Driver, types.SqliteDriverName)
	}
	if cfg.Datastore.MaxOpenConns != 1 {
		t.Fatalf("Datastore.MaxOpenConns = %d, want 1", cfg.Datastore.MaxOpenConns)
	}

	// Logging defaults
	if cfg.Logging.Level != "info" {
		t.Fatalf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
	if !cfg.Logging.ConsoleLogging {
		t.Fatal("Logging.ConsoleLogging should default to true")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/some/dir")

	if cfg.DataDir != "/some/dir" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "/some/dir")
	}
	if cfg.Datastore.Path != "/some/dir/station-manager.db" {
		t.Fatalf("Datastore.Path = %q, want %q", cfg.Datastore.Path, "/some/dir/station-manager.db")
	}
}

func TestNew_AndGetters(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	svc := New(cfg)

	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	if svc.WorkingDir() == "" {
		t.Fatal("WorkingDir returned empty after Initialize")
	}

	logCfg, err := svc.LoggingConfig()
	if err != nil {
		t.Fatalf("LoggingConfig error: %v", err)
	}
	if logCfg.Level != "info" {
		t.Fatalf("LoggingConfig().Level = %q, want %q", logCfg.Level, "info")
	}

	dsCfg, err := svc.DatastoreConfig()
	if err != nil {
		t.Fatalf("DatastoreConfig error: %v", err)
	}
	if dsCfg.Driver != types.SqliteDriverName {
		t.Fatalf("DatastoreConfig().Driver = %q, want %q", dsCfg.Driver, types.SqliteDriverName)
	}
}
