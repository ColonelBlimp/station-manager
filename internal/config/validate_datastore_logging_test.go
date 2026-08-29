package config

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// CC-3. Config.Validate is the single source of truth, but it never validated the
// datastore or the operational logging block — an invalid hand-edit passed Validate
// and Load, then failed later as a generic DI/logger-init error with no field
// attribution. These tests mirror the consumer validators 1:1 (sqlite/validation.go,
// logging/validation.go); the parity tests in those packages guard against drift.
// The confusable each case excludes is the OLD behavior: no finding at the boundary.

func hasFindingCode(findings []Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestValidate_Datastore(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(ds *types.DatastoreConfig)
		wantErr bool
	}{
		{"default is valid", func(*types.DatastoreConfig) {}, false},
		{"driver postgres rejected", func(d *types.DatastoreConfig) { d.Driver = "postgres" }, true},
		{"driver empty rejected", func(d *types.DatastoreConfig) { d.Driver = "" }, true},
		{"driver sqlite ok", func(d *types.DatastoreConfig) { d.Driver = "sqlite" }, false},
		{"empty path rejected", func(d *types.DatastoreConfig) { d.Path = "" }, true},
		{"max_open_conns 0 rejected (min 1)", func(d *types.DatastoreConfig) { d.MaxOpenConns = 0 }, true},
		{"max_open_conns 1 ok (boundary)", func(d *types.DatastoreConfig) { d.MaxOpenConns = 1 }, false},
		{"max_idle_conns 0 rejected (min 1)", func(d *types.DatastoreConfig) { d.MaxIdleConns = 0 }, true},
		{"conn_max_lifetime -1 rejected (min 0)", func(d *types.DatastoreConfig) { d.ConnMaxLifetime = -1 }, true},
		{"conn_max_lifetime 0 ok (boundary)", func(d *types.DatastoreConfig) { d.ConnMaxLifetime = 0 }, false},
		{"conn_max_idle_time -1 rejected (min 0)", func(d *types.DatastoreConfig) { d.ConnMaxIdleTime = -1 }, true},
		{"context_timeout 4 rejected (min 5)", func(d *types.DatastoreConfig) { d.ContextTimeout = 4 }, true},
		{"context_timeout 5 ok (boundary)", func(d *types.DatastoreConfig) { d.ContextTimeout = 5 }, false},
		{"transaction_context_timeout 4 rejected (min 5)", func(d *types.DatastoreConfig) { d.TransactionContextTimeout = 4 }, true},
		{"transaction_context_timeout 5 ok (boundary)", func(d *types.DatastoreConfig) { d.TransactionContextTimeout = 5 }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			c.mutate(&cfg.Datastore)
			got := hasFindingCode(Validate(cfg), "invalid_datastore")
			if got != c.wantErr {
				t.Errorf("invalid_datastore finding = %v, want %v", got, c.wantErr)
			}
		})
	}
}

func TestValidate_Logging(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(lg *types.LoggingConfig)
		wantErr bool
	}{
		{"default is valid", func(*types.LoggingConfig) {}, false},
		{"level verbose rejected", func(l *types.LoggingConfig) { l.Level = "verbose" }, true},
		{"level empty rejected", func(l *types.LoggingConfig) { l.Level = "" }, true},
		{"level info ok", func(l *types.LoggingConfig) { l.Level = "info" }, false},
		{"level trace ok", func(l *types.LoggingConfig) { l.Level = "trace" }, false},
		{"level panic ok", func(l *types.LoggingConfig) { l.Level = "panic" }, false},
		{"skip_frame_count -1 rejected", func(l *types.LoggingConfig) { l.SkipFrameCount = -1 }, true},
		{"skip_frame_count 21 rejected (max 20)", func(l *types.LoggingConfig) { l.SkipFrameCount = 21 }, true},
		{"skip_frame_count 20 ok (boundary)", func(l *types.LoggingConfig) { l.SkipFrameCount = 20 }, false},
		{"rel_log_file_dir empty rejected", func(l *types.LoggingConfig) { l.RelLogFileDir = "" }, true},
		{"rel_log_file_dir absolute rejected", func(l *types.LoggingConfig) { l.RelLogFileDir = "/var/log/smd" }, true},
		{"rel_log_file_dir traversal rejected", func(l *types.LoggingConfig) { l.RelLogFileDir = "../escape" }, true},
		{"rel_log_file_dir relative ok", func(l *types.LoggingConfig) { l.RelLogFileDir = "logs" }, false},
		{"log_file_max_backups -1 rejected", func(l *types.LoggingConfig) { l.LogFileMaxBackups = -1 }, true},
		{"log_file_max_age_days -1 rejected", func(l *types.LoggingConfig) { l.LogFileMaxAgeDays = -1 }, true},
		{"log_file_max_size_mb -1 rejected", func(l *types.LoggingConfig) { l.LogFileMaxSizeMB = -1 }, true},
		{"log_file_max_size_mb 0 ok (omitempty)", func(l *types.LoggingConfig) { l.LogFileMaxSizeMB = 0 }, false},
		{"log_file_max_size_mb 1 ok (boundary)", func(l *types.LoggingConfig) { l.LogFileMaxSizeMB = 1 }, false},
		{"shutdown_timeout_ms 5 rejected (min 10)", func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 5 }, true},
		{"shutdown_timeout_ms 10001 rejected (max 10000)", func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 10001 }, true},
		{"shutdown_timeout_ms 0 ok (omitempty)", func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 0 }, false},
		{"shutdown_timeout_ms 10 ok (boundary)", func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 10 }, false},
		{"shutdown_timeout_ms 10000 ok (boundary)", func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 10000 }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			c.mutate(&cfg.Logging)
			got := hasFindingCode(Validate(cfg), "invalid_logging")
			if got != c.wantErr {
				t.Errorf("invalid_logging finding = %v, want %v", got, c.wantErr)
			}
		})
	}
}

// The operator-observable half of CC-3: a hand-edited invalid datastore/logging value
// stops Load with a field-specific `invalid config (invalid_datastore|invalid_logging)`
// on stderr — the same class of message a bad forwarder/station value already yields —
// instead of starting past Load and failing later as a generic DI/logger-init error.
func TestLoad_RejectsInvalidDatastore(t *testing.T) {
	got := loadErrText(t, `{"version":2,"datastore":{"driver":"postgres"}}`)
	if !strings.Contains(got, "invalid_datastore") {
		t.Fatalf("Load must reject an invalid datastore with a field-specific code, got:\n%s", got)
	}
}

func TestLoad_RejectsInvalidLogging(t *testing.T) {
	got := loadErrText(t, `{"version":2,"logging":{"level":"verbose"}}`)
	if !strings.Contains(got, "invalid_logging") {
		t.Fatalf("Load must reject an invalid logging block with a field-specific code, got:\n%s", got)
	}
}
