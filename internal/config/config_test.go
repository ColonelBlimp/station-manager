package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if cfg.Server.DefaultPageLimit != 50 {
		t.Fatalf("Server.DefaultPageLimit = %d, want 50", cfg.Server.DefaultPageLimit)
	}
	if cfg.Server.MaxPageLimit != 500 {
		t.Fatalf("Server.MaxPageLimit = %d, want 500", cfg.Server.MaxPageLimit)
	}
	if cfg.Server.MaxContactHistoryResults != 100 {
		t.Fatalf("Server.MaxContactHistoryResults = %d, want 100", cfg.Server.MaxContactHistoryResults)
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

// ---- Forwarder config ----

func TestLoad_Forwarders_FullShape(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	content := `{
		"forwarders": [
			{
				"name": "qrz-primary",
				"type": "qrz",
				"enabled": true,
				"credentials": {"username": "M0CMC", "api_key": "abc"},
				"action_filter": ["insert", "update"],
				"tick_interval_sec": 60,
				"batch_size": 10,
				"retry": {
					"max_attempts": 8,
					"initial_backoff_sec": 15,
					"max_backoff_sec": 900
				}
			}
		]
	}`

	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Forwarders) != 1 {
		t.Fatalf("Forwarders len = %d, want 1", len(cfg.Forwarders))
	}
	fc := cfg.Forwarders[0]
	if fc.Name != "qrz-primary" {
		t.Fatalf("Name = %q", fc.Name)
	}
	if fc.Type != "qrz" {
		t.Fatalf("Type = %q", fc.Type)
	}
	if !fc.Enabled {
		t.Fatalf("Enabled = false, want true")
	}
	if len(fc.ActionFilter) != 2 || fc.ActionFilter[0] != "insert" || fc.ActionFilter[1] != "update" {
		t.Fatalf("ActionFilter = %v", fc.ActionFilter)
	}
	if fc.TickIntervalSec != 60 {
		t.Fatalf("TickIntervalSec = %d, want 60", fc.TickIntervalSec)
	}
	if fc.BatchSize != 10 {
		t.Fatalf("BatchSize = %d, want 10", fc.BatchSize)
	}
	if fc.Retry == nil {
		t.Fatal("Retry nil, want populated")
	}
	if fc.Retry.MaxAttempts != 8 {
		t.Fatalf("Retry.MaxAttempts = %d, want 8", fc.Retry.MaxAttempts)
	}
	// Credentials is opaque json.RawMessage at this layer; just verify it's not empty.
	if len(fc.Credentials) == 0 {
		t.Fatal("Credentials blob is empty")
	}
}

func TestLoad_Forwarders_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	// Minimal forwarder: only name + type; everything else should default.
	content := `{
		"forwarders": [
			{"name": "stub-1", "type": "stub"}
		]
	}`

	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	fc := cfg.Forwarders[0]
	if fc.TickIntervalSec != 120 {
		t.Fatalf("TickIntervalSec = %d, want 120 (default)", fc.TickIntervalSec)
	}
	if fc.BatchSize != 5 {
		t.Fatalf("BatchSize = %d, want 5 (default)", fc.BatchSize)
	}
	if len(fc.ActionFilter) != 3 {
		t.Fatalf("ActionFilter len = %d, want 3 (default all actions)", len(fc.ActionFilter))
	}
	if fc.Retry != nil {
		t.Fatal("Retry should stay nil when not specified; forwarder package supplies defaults")
	}
}

func TestLoad_Forwarders_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "empty name",
			body:    `{"forwarders":[{"name":"","type":"qrz"}]}`,
			wantErr: "name is empty",
		},
		{
			name:    "empty type",
			body:    `{"forwarders":[{"name":"x","type":""}]}`,
			wantErr: "type is empty",
		},
		{
			name:    "duplicate name",
			body:    `{"forwarders":[{"name":"x","type":"qrz"},{"name":"x","type":"clublog"}]}`,
			wantErr: "duplicate name",
		},
		{
			name:    "unknown action in filter",
			body:    `{"forwarders":[{"name":"x","type":"qrz","action_filter":["bogus"]}]}`,
			wantErr: "unknown upload action",
		},
		{
			name:    "retry max_attempts zero",
			body:    `{"forwarders":[{"name":"x","type":"qrz","retry":{"max_attempts":0,"initial_backoff_sec":30,"max_backoff_sec":3600}}]}`,
			wantErr: "retry.max_attempts",
		},
		{
			name:    "retry max < initial",
			body:    `{"forwarders":[{"name":"x","type":"qrz","retry":{"max_attempts":5,"initial_backoff_sec":60,"max_backoff_sec":30}}]}`,
			wantErr: "retry.max_backoff_sec",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "config.json")
			if err := os.WriteFile(cfgFile, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("writing test config: %v", err)
			}
			_, err := Load(cfgFile)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}
