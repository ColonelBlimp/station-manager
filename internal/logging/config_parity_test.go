package logging_test

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// CC-3 parity. Decision (b): config.Validate and the logging consumer validator are
// SEPARATE implementations of the same logging rules (struct tags plus the level,
// skip-frame, and relative-path semantic checks); this test guards them against drift
// by asserting the SAME verdict for every input. It also pins the level enum and the
// omitempty boundaries where hand-mirrored rules are easiest to get subtly wrong.
func TestLoggingValidationParity(t *testing.T) {
	valid := config.DefaultConfig(t.TempDir()).Logging
	vary := func(f func(l *types.LoggingConfig)) types.LoggingConfig {
		l := valid
		f(&l)
		return l
	}
	cases := []struct {
		name string
		lg   types.LoggingConfig
	}{
		{"default valid", valid},
		{"level verbose", vary(func(l *types.LoggingConfig) { l.Level = "verbose" })},
		{"level empty", vary(func(l *types.LoggingConfig) { l.Level = "" })},
		{"level trace", vary(func(l *types.LoggingConfig) { l.Level = "trace" })},
		{"level debug", vary(func(l *types.LoggingConfig) { l.Level = "debug" })},
		{"level info", vary(func(l *types.LoggingConfig) { l.Level = "info" })},
		{"level warn", vary(func(l *types.LoggingConfig) { l.Level = "warn" })},
		{"level error", vary(func(l *types.LoggingConfig) { l.Level = "error" })},
		{"level fatal", vary(func(l *types.LoggingConfig) { l.Level = "fatal" })},
		{"level panic", vary(func(l *types.LoggingConfig) { l.Level = "panic" })},
		{"level disabled (zerolog word, not in oneof)", vary(func(l *types.LoggingConfig) { l.Level = "disabled" })},
		{"skip -1", vary(func(l *types.LoggingConfig) { l.SkipFrameCount = -1 })},
		{"skip 0", vary(func(l *types.LoggingConfig) { l.SkipFrameCount = 0 })},
		{"skip 20", vary(func(l *types.LoggingConfig) { l.SkipFrameCount = 20 })},
		{"skip 21", vary(func(l *types.LoggingConfig) { l.SkipFrameCount = 21 })},
		{"rel dir empty", vary(func(l *types.LoggingConfig) { l.RelLogFileDir = "" })},
		{"rel dir absolute", vary(func(l *types.LoggingConfig) { l.RelLogFileDir = "/var/log/smd" })},
		{"rel dir traversal", vary(func(l *types.LoggingConfig) { l.RelLogFileDir = "../escape" })},
		{"rel dir relative", vary(func(l *types.LoggingConfig) { l.RelLogFileDir = "logs" })},
		{"backups -1", vary(func(l *types.LoggingConfig) { l.LogFileMaxBackups = -1 })},
		{"age -1", vary(func(l *types.LoggingConfig) { l.LogFileMaxAgeDays = -1 })},
		{"size -1", vary(func(l *types.LoggingConfig) { l.LogFileMaxSizeMB = -1 })},
		{"size 0 (omitempty)", vary(func(l *types.LoggingConfig) { l.LogFileMaxSizeMB = 0 })},
		{"size 1", vary(func(l *types.LoggingConfig) { l.LogFileMaxSizeMB = 1 })},
		{"shutdown 5", vary(func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 5 })},
		{"shutdown 0 (omitempty)", vary(func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 0 })},
		{"shutdown 10", vary(func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 10 })},
		{"shutdown 10000", vary(func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 10000 })},
		{"shutdown 10001", vary(func(l *types.LoggingConfig) { l.ShutdownTimeoutMS = 10001 })},
	}
	base := config.DefaultConfig(t.TempDir())
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := base
			cfg.Logging = c.lg
			configRejects := false
			for _, f := range config.Validate(cfg) {
				if f.Code == "invalid_logging" {
					configRejects = true
					break
				}
			}
			lg := c.lg
			consumerRejects := logging.ValidateLoggingConfig(&lg) != nil
			if configRejects != consumerRejects {
				t.Errorf("parity mismatch: config.Validate rejects=%v, logging consumer rejects=%v",
					configRejects, consumerRejects)
			}
		})
	}
}
