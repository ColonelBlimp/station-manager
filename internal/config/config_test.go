package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	if cfg.Datastore.MaxOpenConns != 8 {
		t.Fatalf("Datastore.MaxOpenConns = %d, want 8", cfg.Datastore.MaxOpenConns)
	}
	if cfg.Datastore.MaxIdleConns != 8 {
		t.Fatalf("Datastore.MaxIdleConns = %d, want 8", cfg.Datastore.MaxIdleConns)
	}

	// Logging defaults — Level + dir + the rotated-file int knobs are
	// filled by applyDefaults; the booleans (file_logging, with_timestamp,
	// log_file_compress) are intentionally NOT touched on Load so an
	// operator's explicit false isn't reverted. They come from
	// DefaultConfig only — covered in TestDefaultConfig_LoggingDefaults.
	if cfg.Logging.Level != "info" {
		t.Fatalf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
	if cfg.Logging.RelLogFileDir != "log" {
		t.Fatalf("Logging.RelLogFileDir = %q, want %q", cfg.Logging.RelLogFileDir, "log")
	}
	// Empty-config fallback: when neither console nor file is set, file is on.
	if !cfg.Logging.FileLogging {
		t.Fatal("Logging.FileLogging should default to true when neither console nor file is set")
	}
	if cfg.Logging.LogFileMaxSizeMB != 100 {
		t.Fatalf("Logging.LogFileMaxSizeMB = %d, want 100", cfg.Logging.LogFileMaxSizeMB)
	}
	if cfg.Logging.LogFileMaxBackups != 5 {
		t.Fatalf("Logging.LogFileMaxBackups = %d, want 5", cfg.Logging.LogFileMaxBackups)
	}
	if cfg.Logging.LogFileMaxAgeDays != 30 {
		t.Fatalf("Logging.LogFileMaxAgeDays = %d, want 30", cfg.Logging.LogFileMaxAgeDays)
	}

	// Datastore path uses the ${DataDir}/db/ subdir (matches build/db/.gitkeep).
	wantPath := filepath.Join(dir, "db", "station-manager.db")
	if cfg.Datastore.Path != wantPath {
		t.Fatalf("Datastore.Path = %q, want %q", cfg.Datastore.Path, wantPath)
	}
}

func TestDefaultConfig_LoggingDefaults(t *testing.T) {
	// DefaultConfig is the first-run seed used by the daemon. Booleans
	// must come out true here because there's no operator input to
	// preserve.
	cfg := DefaultConfig(t.TempDir())
	if !cfg.Logging.WithTimestamp {
		t.Error("DefaultConfig: Logging.WithTimestamp should be true")
	}
	if !cfg.Logging.FileLogging {
		t.Error("DefaultConfig: Logging.FileLogging should be true")
	}
	if cfg.Logging.ConsoleLogging {
		t.Error("DefaultConfig: Logging.ConsoleLogging should be false (file-only by default)")
	}
	if !cfg.Logging.LogFileCompress {
		t.Error("DefaultConfig: Logging.LogFileCompress should be true")
	}
}

func TestDefaultConfig_TCPAndServeSPADefaultsOn(t *testing.T) {
	// First-run UX gate: a fresh install must reach the SPA from a
	// browser without operator-side config edits. Protocol=tcp,
	// SocketPath=127.0.0.1:8080 (matches Vite dev proxy default),
	// ServeSPA=true.
	cfg := DefaultConfig(t.TempDir())

	if cfg.Server.Protocol != "tcp" {
		t.Errorf("Server.Protocol = %q, want tcp (first-run default)", cfg.Server.Protocol)
	}
	if cfg.SocketPath != "127.0.0.1:8080" {
		t.Errorf("SocketPath = %q, want 127.0.0.1:8080", cfg.SocketPath)
	}
	if cfg.Server.ServeSPA == nil || !*cfg.Server.ServeSPA {
		t.Error("ServeSPA should default to true on TCP")
	}
}

func TestLoad_UnixProtocolKeepsUnixSocketDefault(t *testing.T) {
	// Operator who flips back to unix should get a unix-socket
	// SocketPath default (not the TCP one) when they leave
	// socket_path unset — so they don't have to set both fields.
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	content := `{"server":{"protocol":"unix"}}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Protocol != "unix" {
		t.Errorf("Protocol = %q, want unix", cfg.Server.Protocol)
	}
	if !strings.HasSuffix(cfg.SocketPath, "smd.sock") {
		t.Errorf("SocketPath = %q, want a unix-socket-style path ending in smd.sock", cfg.SocketPath)
	}
}

func TestDefaultConfig_LoggingStationEmpty(t *testing.T) {
	// First-run state: no callsign yet — operator sets it via the
	// setup dialog. The empty string is a legitimate pre-setup value.
	cfg := DefaultConfig(t.TempDir())
	if cfg.LoggingStation.StationCallsign != "" {
		t.Errorf("DefaultConfig: LoggingStation.StationCallsign = %q, want empty",
			cfg.LoggingStation.StationCallsign)
	}
}

func TestLoad_LoggingStationCallsignPreserved(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	content := `{
		"logging_station": {"station_callsign": "M0XYZ"}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LoggingStation.StationCallsign != "M0XYZ" {
		t.Errorf("LoggingStation.StationCallsign = %q, want %q",
			cfg.LoggingStation.StationCallsign, "M0XYZ")
	}
}

func TestLoad_OperatorFalsePreserved(t *testing.T) {
	// Regression guard for the *bool trap: an operator who explicitly
	// sets file_logging false (and console_logging true) must NOT have
	// it silently flipped on again by applyDefaults.
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	content := `{
		"logging": {
			"level": "info",
			"console_logging": true,
			"file_logging": false,
			"with_timestamp": false,
			"log_file_compress": false
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logging.FileLogging {
		t.Error("FileLogging was flipped to true despite explicit false")
	}
	if cfg.Logging.WithTimestamp {
		t.Error("WithTimestamp was flipped to true despite explicit false")
	}
	if cfg.Logging.LogFileCompress {
		t.Error("LogFileCompress was flipped to true despite explicit false")
	}
	if !cfg.Logging.ConsoleLogging {
		t.Error("ConsoleLogging should remain true as set")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/some/dir")

	if cfg.DataDir != "/some/dir" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "/some/dir")
	}
	wantPath := filepath.Join("/some/dir", "db", "station-manager.db")
	if cfg.Datastore.Path != wantPath {
		t.Fatalf("Datastore.Path = %q, want %q", cfg.Datastore.Path, wantPath)
	}
}

func TestWriteJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := DefaultConfig(dir)
	if err := WriteJSON(path, original); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.DataDir != original.DataDir {
		t.Errorf("DataDir mismatch: got %q want %q", loaded.DataDir, original.DataDir)
	}
	if loaded.Datastore.Driver != original.Datastore.Driver {
		t.Errorf("Driver mismatch: got %q want %q", loaded.Datastore.Driver, original.Datastore.Driver)
	}
	if loaded.Server.MaxBodyBytes != original.Server.MaxBodyBytes {
		t.Errorf("MaxBodyBytes mismatch: got %d want %d",
			loaded.Server.MaxBodyBytes, original.Server.MaxBodyBytes)
	}
}

func TestWriteJSON_AtomicViaTempFile(t *testing.T) {
	// The temp file must NOT remain after a successful write — rename
	// either replaced the target or the cleanup removed it.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := WriteJSON(path, DefaultConfig(dir)); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file should not exist after successful WriteJSON; stat err = %v", err)
	}
}

// TestUnknownKeys guards review 2026-06-19 L1: hand-edited typos are reported
// as dotted paths (advisory), while a clean default config and arbitrary map
// keys produce no false positives.
func TestUnknownKeys(t *testing.T) {
	// A fully-default config must flag ZERO unknown keys — the critical guard
	// that the reflective schema walk doesn't false-flag a real field.
	clean, err := json.Marshal(DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("marshal default: %v", err)
	}
	if got := UnknownKeys(clean); len(got) != 0 {
		t.Fatalf("default config flagged unknown keys (false positives): %v", got)
	}

	// Top-level + nested typos are reported; real sibling keys are not.
	doc := []byte(`{
		"data_dir": "/x",
		"bogus_top": 1,
		"smtp": {"host": "h", "timeot_sec": 5},
		"bridge": {"enabled": true, "enable": true}
	}`)
	want := map[string]bool{"bogus_top": true, "smtp.timeot_sec": true, "bridge.enable": true}
	got := UnknownKeys(doc)
	for _, k := range got {
		if !want[k] {
			t.Errorf("unexpected key reported as unknown: %q (all: %v)", k, got)
			continue
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("expected unknown key %q was not reported; got %v", k, got)
	}
}

// TestWriteJSON_FileMode guards review 2026-06-19 M1: config.json holds
// plaintext secrets, so a fresh write must be 0600 (owner-only), and a legacy
// wider mode (0644) must be tightened on the next write — while an operator's
// stricter mode (0400) is preserved.
func TestWriteJSON_FileMode(t *testing.T) {
	dir := t.TempDir()

	t.Run("fresh write is 0600", func(t *testing.T) {
		path := filepath.Join(dir, "fresh.json")
		if err := WriteJSON(path, DefaultConfig(dir)); err != nil {
			t.Fatalf("WriteJSON: %v", err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("mode = %o, want 0600", perm)
		}
	})

	t.Run("legacy 0644 tightened to 0600", func(t *testing.T) {
		path := filepath.Join(dir, "legacy.json")
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := WriteJSON(path, DefaultConfig(dir)); err != nil {
			t.Fatalf("WriteJSON: %v", err)
		}
		fi, _ := os.Stat(path)
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("mode = %o, want 0600 (legacy 0644 tightened)", perm)
		}
	})

	t.Run("stricter 0400 preserved", func(t *testing.T) {
		path := filepath.Join(dir, "strict.json")
		if err := os.WriteFile(path, []byte("{}"), 0o400); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := WriteJSON(path, DefaultConfig(dir)); err != nil {
			t.Fatalf("WriteJSON: %v", err)
		}
		fi, _ := os.Stat(path)
		if perm := fi.Mode().Perm(); perm != 0o400 {
			t.Errorf("mode = %o, want 0400 preserved", perm)
		}
	})
}

func TestWriteJSON_FailsOnBadParentDir(t *testing.T) {
	// Parent directory doesn't exist — temp-file write must fail and
	// surface the error rather than silently succeeding.
	err := WriteJSON("/nonexistent/dir/config.json", DefaultConfig("/nonexistent/dir"))
	if err == nil {
		t.Fatal("WriteJSON expected error for nonexistent parent dir, got nil")
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

// TestValidateBridge_TimeoutRangeChecks covers the range-validation
// added with the per-deployment timeout knobs. Zero values mean "use
// daemon default" and always pass. Out-of-band positive values are
// rejected with a message naming the field, so a config-file typo is
// caught at startup rather than going silently into the wrong setting.
func TestValidateBridge_TimeoutRangeChecks(t *testing.T) {
	t.Run("all zeros pass (defaults used)", func(t *testing.T) {
		err := validateBridge(types.BridgeConfig{
			Enabled: false,
		})
		if err != nil {
			t.Fatalf("zero timeouts should be accepted: %v", err)
		}
	})

	t.Run("rejects below-min liveness_ms", func(t *testing.T) {
		err := validateBridge(types.BridgeConfig{
			Timeouts: types.BridgeTimeoutsConfig{LivenessMs: 10},
		})
		if err == nil || !strings.Contains(err.Error(), "liveness_ms") {
			t.Errorf("expected error naming liveness_ms; got %v", err)
		}
	})

	t.Run("rejects above-max backoff_max_ms", func(t *testing.T) {
		err := validateBridge(types.BridgeConfig{
			Timeouts: types.BridgeTimeoutsConfig{BackoffMaxMs: 9_000_000},
		})
		if err == nil || !strings.Contains(err.Error(), "backoff_max_ms") {
			t.Errorf("expected error naming backoff_max_ms; got %v", err)
		}
	})

	t.Run("rejects backoff_initial_ms > backoff_max_ms", func(t *testing.T) {
		err := validateBridge(types.BridgeConfig{
			Timeouts: types.BridgeTimeoutsConfig{
				BackoffInitialMs: 5000,
				BackoffMaxMs:     1000,
			},
		})
		if err == nil || !strings.Contains(err.Error(), "must not exceed") {
			t.Errorf("expected error about initial > max; got %v", err)
		}
	})

	// write_watchdog_ms is the serial-write backstop; it must be range-checked
	// like the others (review H1). A too-small value closes the port on normal
	// jitter; a too-large one leaves command/tune-stop writes blocked far past
	// the intended fault backstop.
	t.Run("rejects below-min write_watchdog_ms", func(t *testing.T) {
		err := validateBridge(types.BridgeConfig{
			Timeouts: types.BridgeTimeoutsConfig{WriteWatchdogMs: 1},
		})
		if err == nil || !strings.Contains(err.Error(), "write_watchdog_ms") {
			t.Errorf("expected error naming write_watchdog_ms; got %v", err)
		}
	})

	t.Run("rejects above-max write_watchdog_ms", func(t *testing.T) {
		err := validateBridge(types.BridgeConfig{
			Timeouts: types.BridgeTimeoutsConfig{WriteWatchdogMs: 9_000_000},
		})
		if err == nil || !strings.Contains(err.Error(), "write_watchdog_ms") {
			t.Errorf("expected error naming write_watchdog_ms; got %v", err)
		}
	})

	t.Run("accepts in-range values", func(t *testing.T) {
		err := validateBridge(types.BridgeConfig{
			Timeouts: types.BridgeTimeoutsConfig{
				LivenessMs:             5000,
				BackoffInitialMs:       1000,
				BackoffMaxMs:           30000,
				SteadyStateThresholdMs: 10000,
				WriteWatchdogMs:        2000,
			},
		})
		if err != nil {
			t.Errorf("in-range timeouts should be accepted: %v", err)
		}
	})
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

// TestWarnings_NonLoopbackTCPBind covers review m4 — operator binding
// the daemon to a non-loopback address with no auth on the API
// surface should produce a startup advisory, not a hard failure.
func TestWarnings_NonLoopbackTCPBind(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		socket   string
		wantWarn bool
	}{
		{name: "default loopback IPv4", protocol: "tcp", socket: "127.0.0.1:8080", wantWarn: false},
		{name: "explicit localhost", protocol: "tcp", socket: "localhost:8080", wantWarn: false},
		{name: "loopback IPv6", protocol: "tcp", socket: "[::1]:8080", wantWarn: false},
		{name: "wildcard bind via empty host", protocol: "tcp", socket: ":8080", wantWarn: true},
		{name: "wildcard bind via 0.0.0.0", protocol: "tcp", socket: "0.0.0.0:8080", wantWarn: true},
		{name: "LAN IP", protocol: "tcp", socket: "192.168.1.10:8080", wantWarn: true},
		{name: "wildcard IPv6", protocol: "tcp", socket: "[::]:8080", wantWarn: true},
		{name: "unix socket — not subject to bind warning", protocol: "unix", socket: "/tmp/smd.sock", wantWarn: false},
		{name: "unrecognised hostname conservatively warned", protocol: "tcp", socket: "not-a-host:8080", wantWarn: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			cfg.Server.Protocol = tc.protocol
			cfg.SocketPath = tc.socket

			warnings := Warnings(cfg)

			haveBindWarn := false
			for _, w := range warnings {
				if strings.Contains(w, "non-loopback") {
					haveBindWarn = true
					break
				}
			}
			if haveBindWarn != tc.wantWarn {
				t.Fatalf("Warnings(%q, %q) bind-warning = %v, want %v\nfull: %v",
					tc.protocol, tc.socket, haveBindWarn, tc.wantWarn, warnings)
			}
		})
	}
}

// --- Rig catalogue (ADR 0028) ---

// TestApplyRigProfiles_MigratesLegacy: a pre-catalogue config (loose
// bridge/ft8 identity, no Rigs) folds into a single id-1 rig that becomes
// active, and the active values project back through the helpers.
func TestApplyRigProfiles_MigratesLegacy(t *testing.T) {
	cfg := Config{
		Bridge: types.BridgeConfig{
			Cat:    &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
			Serial: &types.BridgeSerialConfig{Port: "/dev/ttyUSB0"},
		},
		Ft8:          types.Ft8Config{Device: "USB Audio CODEC"},
		DefaultRigID: 1, // applyDefaults would have stamped this before us
	}
	if err := applyRigProfiles(&cfg); err != nil {
		t.Fatalf("applyRigProfiles: %v", err)
	}
	if len(cfg.Rigs) != 1 {
		t.Fatalf("len(Rigs) = %d, want 1", len(cfg.Rigs))
	}
	rc := cfg.Rigs[0]
	// Audio is NOT synthesised from the legacy ft8.device: it's an index that
	// can't become a name at load time (config.md §10.4 #1), so the rig's Audio
	// stays empty and the loose ft8.device remains a resolved fallback.
	if rc.ID != 1 || rc.Model != "yaesu-ftdx10" || rc.Port != "/dev/ttyUSB0" || rc.Audio != (types.RigAudioConfig{}) {
		t.Fatalf("synthesised rig = %+v, want {1 yaesu-ftdx10 /dev/ttyUSB0 <no audio>}", rc)
	}
	if cfg.DefaultRigID != 1 {
		t.Errorf("DefaultRigID = %d, want 1", cfg.DefaultRigID)
	}
	if b := cfg.ActiveBridge(); b.Cat.Driver != "yaesu-ftdx10" || b.Serial.Port != "/dev/ttyUSB0" {
		t.Errorf("ActiveBridge() = {driver %q port %q}, want {yaesu-ftdx10 /dev/ttyUSB0}", b.Cat.Driver, b.Serial.Port)
	}
	// The rig has no named audio, so the loose ft8.device passes through unclobbered.
	if f := cfg.ActiveFt8(); f.Device != "USB Audio CODEC" {
		t.Errorf("ActiveFt8().Device = %q, want %q", f.Device, "USB Audio CODEC")
	}
}

// TestApplyRigProfiles_NoLooseIdentityNoMigration: a bridge-disabled /
// catalogue-less host (no Rigs, no loose fields) stays empty and the helpers
// pass cfg.Bridge / cfg.Ft8 through unchanged.
func TestApplyRigProfiles_NoLooseIdentityNoMigration(t *testing.T) {
	cfg := Config{
		Bridge:       types.BridgeConfig{Enabled: false, Timeouts: types.BridgeTimeoutsConfig{LivenessMs: 7000}},
		DefaultRigID: 1,
	}
	if err := applyRigProfiles(&cfg); err != nil {
		t.Fatalf("applyRigProfiles: %v", err)
	}
	if len(cfg.Rigs) != 0 {
		t.Fatalf("len(Rigs) = %d, want 0", len(cfg.Rigs))
	}
	if b := cfg.ActiveBridge(); b.Cat.Driver != "" || b.Serial.Port != "" || b.Timeouts.LivenessMs != 7000 {
		t.Errorf("ActiveBridge() = %+v, want passthrough of cfg.Bridge", b)
	}
}

// TestApplyRigProfiles_ResolvesAndProjects: with a multi-rig catalogue,
// DefaultRigID selects which rig the helpers project, and the cross-rig
// bridge knobs are preserved.
func TestApplyRigProfiles_ResolvesAndProjects(t *testing.T) {
	cfg := Config{
		Bridge: types.BridgeConfig{Enabled: true, Timeouts: types.BridgeTimeoutsConfig{LivenessMs: 5000}},
		Rigs: []types.RigConfig{
			{ID: 1, Model: "yaesu-ftdx10", Port: "/dev/ttyUSB0", Audio: types.RigAudioConfig{RX: "codec-a-in", TX: "codec-a-out"}},
			{ID: 2, Model: "yaesu-ft710", Port: "/dev/ttyUSB2", Audio: types.RigAudioConfig{RX: "codec-b-in", TX: "codec-b-out"}},
		},
		DefaultRigID: 2,
	}
	if err := applyRigProfiles(&cfg); err != nil {
		t.Fatalf("applyRigProfiles: %v", err)
	}
	b := cfg.ActiveBridge()
	if b.Cat.Driver != "yaesu-ft710" || b.Serial.Port != "/dev/ttyUSB2" {
		t.Errorf("ActiveBridge() = {driver %q port %q}, want the id-2 rig", b.Cat.Driver, b.Serial.Port)
	}
	if b.Timeouts.LivenessMs != 5000 || !b.Enabled {
		t.Errorf("ActiveBridge() dropped cross-rig knobs: %+v", b)
	}
	// Per-direction projection: RX → Device (capture), TX → TX.Device (playback).
	f := cfg.ActiveFt8()
	if f.Device != "codec-b-in" {
		t.Errorf("ActiveFt8().Device = %q, want codec-b-in", f.Device)
	}
	if f.TX == nil || f.TX.Device != "codec-b-out" {
		t.Errorf("ActiveFt8().TX.Device = %v, want codec-b-out", f.TX)
	}
}

func TestApplyRigProfiles_Errors(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "non-positive id",
			cfg:  Config{Rigs: []types.RigConfig{{ID: 0, Model: "yaesu-ftdx10"}}, DefaultRigID: 1},
			want: "positive integer",
		},
		{
			name: "duplicate id",
			cfg: Config{Rigs: []types.RigConfig{
				{ID: 1, Model: "yaesu-ftdx10"}, {ID: 1, Model: "yaesu-ft710"},
			}, DefaultRigID: 1},
			want: "duplicate id 1",
		},
		{
			name: "empty model",
			cfg:  Config{Rigs: []types.RigConfig{{ID: 1, Model: ""}}, DefaultRigID: 1},
			want: "model must not be empty",
		},
		{
			name: "default_rig_id no match",
			cfg: Config{Rigs: []types.RigConfig{
				{ID: 1, Model: "yaesu-ftdx10"}, {ID: 2, Model: "yaesu-ft710"},
			}, DefaultRigID: 5},
			want: "default_rig_id 5 does not match",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Rig-catalogue validation moved from applyRigProfiles to Validate
			// (config.md §12); assert an error finding carries the message.
			found := false
			for _, f := range Validate(tc.cfg) {
				if !f.Warning && strings.Contains(f.Message, tc.want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Validate: expected an error finding containing %q; got %+v", tc.want, Validate(tc.cfg))
			}
		})
	}
}

// TestLoad_MigratesLegacyBridge: end-to-end Load of a pre-catalogue config
// synthesises the catalogue and validateBridge passes against the projected
// active values.
func TestLoad_MigratesLegacyBridge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	base := DefaultConfig(dir)
	base.Bridge.Enabled = true
	base.Bridge.Cat = &types.BridgeCatConfig{Driver: "yaesu-ftdx10"}
	base.Bridge.Serial = &types.BridgeSerialConfig{Port: "/dev/ttyUSB0"}
	base.Ft8.Device = "codec-a"
	if err := WriteJSON(path, base); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The legacy ft8.device is NOT migrated into the rig's Audio (it's an index,
	// not a name); the synthesised rig has no audio.
	if len(got.Rigs) != 1 || got.Rigs[0].Model != "yaesu-ftdx10" || got.Rigs[0].Audio != (types.RigAudioConfig{}) {
		t.Fatalf("migrated Rigs = %+v", got.Rigs)
	}
	if b := got.ActiveBridge(); b.Cat.Driver != "yaesu-ftdx10" || b.Serial.Port != "/dev/ttyUSB0" {
		t.Errorf("ActiveBridge() = %+v after migration", b)
	}
}

// TestLoad_RigCatalogueRoundTrip: a catalogue config survives Load → WriteJSON
// → Load unchanged (the persistence shape the PUT path relies on).
func TestLoad_RigCatalogueRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "c1.json")
	p2 := filepath.Join(dir, "c2.json")
	base := DefaultConfig(dir)
	base.Bridge.Enabled = true
	base.Rigs = []types.RigConfig{
		{ID: 1, Model: "yaesu-ftdx10", Port: "/dev/ttyUSB0", Audio: types.RigAudioConfig{RX: "codec-a-in", TX: "codec-a-out"}},
		{ID: 2, Model: "yaesu-ft710", Port: "/dev/ttyUSB2", Audio: types.RigAudioConfig{RX: "codec-b-in", TX: "codec-b-out"}},
	}
	base.DefaultRigID = 2
	if err := WriteJSON(p1, base); err != nil {
		t.Fatal(err)
	}
	c1, err := Load(p1)
	if err != nil {
		t.Fatalf("Load p1: %v", err)
	}
	if err := WriteJSON(p2, c1); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p2)
	if err != nil {
		t.Fatalf("Load p2: %v", err)
	}
	if !reflect.DeepEqual(c1.Rigs, c2.Rigs) {
		t.Errorf("Rigs not preserved across round-trip:\n c1 = %+v\n c2 = %+v", c1.Rigs, c2.Rigs)
	}
	if c2.DefaultRigID != 2 {
		t.Errorf("DefaultRigID = %d, want 2", c2.DefaultRigID)
	}
}

// --- §13 versioning & migration scaffold ----------------------------------

func TestLoad_StampsCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// A version-less file is the pre-versioning baseline; Load stamps the current
	// schema version so the in-memory config always carries it.
	content := `{"data_dir": "/tmp/d", "socket_path": "/tmp/s.sock"}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Version != currentConfigVersion {
		t.Fatalf("Version = %d, want currentConfigVersion %d", cfg.Version, currentConfigVersion)
	}
}

func TestLoad_RejectsNewerConfigVersion(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// A document from a newer schema than this build supports is fatal — the
	// downgrade guard (config.md §13.4).
	content := `{"version": 999, "data_dir": "/tmp/d"}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	_, err := Load(cfgFile)
	if err == nil {
		t.Fatal("Load expected error for newer-than-supported config version, got nil")
	}
	if !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("error = %q, want it to mention downgrade", err.Error())
	}
}

// --- §10 slice 2a: per-rig FT8 mode ---------------------------------------

func TestLoad_FoldsGlobalFt8ModeOntoActiveRig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// Legacy global ft8.tx.mode (DATA-L) + a catalogue; Load folds it onto the
	// active rig's per-rig override and clears the global knob.
	content := `{
		"data_dir": "/tmp/d",
		"rigs": [{"id": 1, "model": "yaesu-ftdx10"}],
		"default_rig_id": 1,
		"ft8": {"tx": {"mode": "DATA-L"}}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	rc := cfg.RigByID(1)
	if rc == nil || rc.Ft8Mode == nil || *rc.Ft8Mode != "DATA-L" {
		t.Fatalf("active rig Ft8Mode = %v, want override DATA-L", rc.Ft8Mode)
	}
	if cfg.Ft8.TX != nil && cfg.Ft8.TX.Mode != "" {
		t.Fatalf("global Ft8TXConfig.Mode = %q, want cleared", cfg.Ft8.TX.Mode)
	}
	if got := cfg.ActiveFt8().TX.Mode; got != "DATA-L" {
		t.Fatalf("ActiveFt8().TX.Mode = %q, want DATA-L (the per-rig override)", got)
	}
}

func TestActiveFt8_UsesRigdefDefaultWhenNoOverride(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// No global ft8.tx.mode, no per-rig override → ActiveFt8 falls back to the
	// rigdef's ft8_mode default for the model (FTdx10 ships DATA-U).
	content := `{
		"data_dir": "/tmp/d",
		"rigs": [{"id": 1, "model": "yaesu-ftdx10"}],
		"default_rig_id": 1
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if rc := cfg.RigByID(1); rc == nil || rc.Ft8Mode != nil {
		t.Fatalf("rig Ft8Mode = %v, want nil (rely on rigdef default)", rc.Ft8Mode)
	}
	if got := cfg.ActiveFt8().TX.Mode; got != "DATA-U" {
		t.Fatalf("ActiveFt8().TX.Mode = %q, want rigdef default DATA-U", got)
	}
}

// --- §10 slice 2b: per-rig mode-mappings (+ v1→v2 migration) --------------

func TestLoad_MigratesGlobalModeMappingsToRig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// v1 shape: a global bridge.mode_mappings[driver] override + a rig of that
	// model. The v1→v2 migration folds it onto the rig (keyed by rig literal) and
	// removes the global block.
	content := `{
		"data_dir": "/tmp/d",
		"rigs": [{"id": 1, "model": "yaesu-ftdx10"}],
		"default_rig_id": 1,
		"bridge": {"mode_mappings": {"yaesu-ftdx10": {"DATA-U": {"mode": "FT4"}}}}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Version != currentConfigVersion {
		t.Fatalf("Version = %d, want %d (migrated)", cfg.Version, currentConfigVersion)
	}
	rc := cfg.RigByID(1)
	if rc == nil || rc.ModeMappings["DATA-U"].Mode != "FT4" {
		t.Fatalf("rig ModeMappings = %v, want DATA-U→FT4 folded from the global block", rc.ModeMappings)
	}
}

func TestLoad_RejectsInvalidRigModeMapping(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// A per-rig mode-mapping with a non-ADIF mode is fatal (validation moved from
	// the global bridge.mode_mappings check to per-rig, §10).
	content := `{
		"data_dir": "/tmp/d",
		"rigs": [{"id": 1, "model": "yaesu-ftdx10", "mode_mappings": {"DATA-U": {"mode": "BOGUS"}}}],
		"default_rig_id": 1
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	if _, err := Load(cfgFile); err == nil {
		t.Fatal("Load expected error for invalid per-rig mode mapping, got nil")
	}
}

// --- §10 slice 2c: per-rig serial overrides projected by ActiveBridge ------

func TestActiveBridge_ProjectsRigSerialOverrides(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Bridge.Enabled = true
	cfg.Rigs = []types.RigConfig{{
		ID:        1,
		Model:     "yaesu-ftdx10",
		Port:      "/dev/ttyUSB9",
		Overrides: types.RigOverrides{BaudRate: 4800, Parity: "even"},
	}}
	cfg.DefaultRigID = 1

	b := cfg.ActiveBridge()
	if b.Cat.Driver != "yaesu-ftdx10" || b.Serial.Port != "/dev/ttyUSB9" {
		t.Fatalf("projected driver/port = %q/%q, want yaesu-ftdx10 /dev/ttyUSB9", b.Cat.Driver, b.Serial.Port)
	}
	if b.Serial.Overrides.BaudRate != 4800 || b.Serial.Overrides.Parity != "even" {
		t.Fatalf("projected serial overrides = %+v, want BaudRate 4800 / Parity even", b.Serial.Overrides)
	}
}

// --- §10 slice 2d: per-rig MY_RIG (derive / override / suppress) ----------

func TestLoad_FoldsGlobalMyRigOntoActiveRig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// Legacy config logging_station.my_rig is folded onto the active rig's
	// per-rig override and cleared from the logging_station block.
	content := `{
		"data_dir": "/tmp/d",
		"rigs": [{"id": 1, "model": "yaesu-ftdx10"}],
		"default_rig_id": 1,
		"logging_station": {"my_rig": "FTdx10 + ATU"}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	rc := cfg.RigByID(1)
	if rc == nil || rc.MyRig == nil || *rc.MyRig != "FTdx10 + ATU" {
		t.Fatalf("active rig MyRig = %v, want override 'FTdx10 + ATU'", rc.MyRig)
	}
	if cfg.LoggingStation.MyRig != "" {
		t.Fatalf("logging_station.my_rig = %q, want cleared", cfg.LoggingStation.MyRig)
	}
	if got := cfg.ResolveMyRig(); got != "FTdx10 + ATU" {
		t.Fatalf("ResolveMyRig() = %q, want the per-rig override", got)
	}
}

func TestResolveMyRig_DeriveAndSuppress(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Rigs = []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10"}}
	cfg.DefaultRigID = 1

	// No override → derive the rigdef Name.
	if got := cfg.ResolveMyRig(); got != "Yaesu FTdx10" {
		t.Fatalf("ResolveMyRig() with no override = %q, want rigdef name 'Yaesu FTdx10'", got)
	}

	// Explicit empty override → suppress (publish nothing).
	empty := ""
	cfg.Rigs[0].MyRig = &empty
	if got := cfg.ResolveMyRig(); got != "" {
		t.Fatalf("ResolveMyRig() with \"\" override = %q, want \"\" (suppressed)", got)
	}

	// Explicit value → verbatim.
	custom := "Homebrew TX"
	cfg.Rigs[0].MyRig = &custom
	if got := cfg.ResolveMyRig(); got != "Homebrew TX" {
		t.Fatalf("ResolveMyRig() with override = %q, want 'Homebrew TX'", got)
	}
}

// --- §12a slice: consolidated Validate -------------------------------------

func TestValidate_CollectsErrorsAndWarnings(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Forwarders = []types.ForwarderConfig{{Type: "qrz"}} // empty name → error finding
	cfg.Server.Protocol = "tcp"
	cfg.SocketPath = "0.0.0.0:8080" // non-loopback bind → warning finding

	var gotErr, gotWarn bool
	for _, f := range Validate(cfg) {
		if f.Warning {
			gotWarn = true
		} else {
			gotErr = true
		}
	}
	if !gotErr {
		t.Error("Validate: expected an error finding for the invalid forwarder")
	}
	if !gotWarn {
		t.Error("Validate: expected a warning finding for the non-loopback TCP bind")
	}
}
