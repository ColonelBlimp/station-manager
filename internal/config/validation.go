package config

import (
	"fmt"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
)

// validateAppConfig performs minimal validation to avoid obviously bad configs while remaining permissive.
// It also applies sensible defaults for missing or zero-valued fields.
func validateAppConfig(cfg *types.AppConfig) error {
	const op errors.Op = "config.validateAppConfig"
	if cfg == nil {
		return errors.New(op).Msg("AppConfig is nil")
	}

	db := cfg.DatastoreConfig
	switch db.Driver {
	case types.SqliteDriverName:
		if db.Path == "" {
			return errors.New(op).Msg("sqlite path is required")
		}
	case types.PostgresDriverName:
		// Be permissive here; database service will perform full validation later.
		// Just accept the driver selection.
		_ = db
	default:
		return errors.New(op).Msg(fmt.Sprintf("unsupported driver: %s", db.Driver))
	}

	// Minimal check for logging config
	if cfg.LoggingConfig.Level == "" {
		return errors.New(op).Msg("logging level must be set")
	}

	// Apply defaults for forwarding config if not set (prevents panics from zero values)
	applyForwardingDefaults(&cfg.RequiredConfigs)

	return nil
}

// applyForwardingDefaults sets sensible defaults for forwarding configuration values
// that would cause panics or misbehavior if left at zero.
func applyForwardingDefaults(cfg *types.RequiredConfigs) {
	if cfg.QsoForwardingPollIntervalSeconds <= 0 {
		cfg.QsoForwardingPollIntervalSeconds = defaultRequiredConfigs.QsoForwardingPollIntervalSeconds
	}
	if cfg.QsoForwardingWorkerCount <= 0 {
		cfg.QsoForwardingWorkerCount = defaultRequiredConfigs.QsoForwardingWorkerCount
	}
	if cfg.QsoForwardingQueueSize <= 0 {
		cfg.QsoForwardingQueueSize = defaultRequiredConfigs.QsoForwardingQueueSize
	}
	if cfg.QsoForwardingRowLimit <= 0 {
		cfg.QsoForwardingRowLimit = defaultRequiredConfigs.QsoForwardingRowLimit
	}
	if cfg.DatabaseWriteQueueSize <= 0 {
		cfg.DatabaseWriteQueueSize = defaultRequiredConfigs.DatabaseWriteQueueSize
	}
}
