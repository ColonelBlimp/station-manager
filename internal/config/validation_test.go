package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// --- validateAppConfig ---

func TestValidateAppConfig_nilConfig(t *testing.T) {
	err := validateAppConfig(nil)
	assert.Error(t, err)
}

func TestValidateAppConfig_unsupportedDriver(t *testing.T) {
	cfg := &types.AppConfig{
		DatastoreConfig: types.DatastoreConfig{Driver: "mysql"},
		LoggingConfig:   types.LoggingConfig{Level: "info"},
	}
	err := validateAppConfig(cfg)
	assert.Error(t, err)
}

func TestValidateAppConfig_sqliteWithoutPath(t *testing.T) {
	cfg := &types.AppConfig{
		DatastoreConfig: types.DatastoreConfig{Driver: types.SqliteDriverName, Path: ""},
		LoggingConfig:   types.LoggingConfig{Level: "info"},
	}
	err := validateAppConfig(cfg)
	assert.Error(t, err)
}

func TestValidateAppConfig_sqliteWithPath_valid(t *testing.T) {
	cfg := &types.AppConfig{
		DatastoreConfig: types.DatastoreConfig{Driver: types.SqliteDriverName, Path: "db/data.db"},
		LoggingConfig:   types.LoggingConfig{Level: "info"},
	}
	err := validateAppConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateAppConfig_postgresWithoutHost_valid(t *testing.T) {
	// Postgres validation is deferred to the database service; config is permissive.
	cfg := &types.AppConfig{
		DatastoreConfig: types.DatastoreConfig{Driver: types.PostgresDriverName},
		LoggingConfig:   types.LoggingConfig{Level: "warn"},
	}
	err := validateAppConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateAppConfig_emptyLoggingLevel(t *testing.T) {
	cfg := &types.AppConfig{
		DatastoreConfig: types.DatastoreConfig{Driver: types.SqliteDriverName, Path: "db/data.db"},
		LoggingConfig:   types.LoggingConfig{Level: ""},
	}
	err := validateAppConfig(cfg)
	assert.Error(t, err)
}

func TestValidateAppConfig_appliesForwardingDefaults(t *testing.T) {
	cfg := &types.AppConfig{
		DatastoreConfig: types.DatastoreConfig{Driver: types.SqliteDriverName, Path: "db/data.db"},
		LoggingConfig:   types.LoggingConfig{Level: "info"},
		RequiredConfigs: types.RequiredConfigs{}, // all forwarding fields at zero
	}
	require.NoError(t, validateAppConfig(cfg))

	assert.Equal(t, defaultRequiredConfigs.QsoForwardingPollIntervalSec, cfg.RequiredConfigs.QsoForwardingPollIntervalSec)
	assert.Equal(t, defaultRequiredConfigs.QsoForwardingWorkerCount, cfg.RequiredConfigs.QsoForwardingWorkerCount)
	assert.Equal(t, defaultRequiredConfigs.QsoForwardingQueueSize, cfg.RequiredConfigs.QsoForwardingQueueSize)
	assert.Equal(t, defaultRequiredConfigs.QsoForwardingRowLimit, cfg.RequiredConfigs.QsoForwardingRowLimit)
	assert.Equal(t, defaultRequiredConfigs.DatabaseWriteQueueSize, cfg.RequiredConfigs.DatabaseWriteQueueSize)
}

// --- applyForwardingDefaults ---

func TestApplyForwardingDefaults_zeroValuesGetDefaults(t *testing.T) {
	cfg := &types.RequiredConfigs{}
	applyForwardingDefaults(cfg)

	assert.Equal(t, defaultRequiredConfigs.QsoForwardingPollIntervalSec, cfg.QsoForwardingPollIntervalSec)
	assert.Equal(t, defaultRequiredConfigs.QsoForwardingWorkerCount, cfg.QsoForwardingWorkerCount)
	assert.Equal(t, defaultRequiredConfigs.QsoForwardingQueueSize, cfg.QsoForwardingQueueSize)
	assert.Equal(t, defaultRequiredConfigs.QsoForwardingRowLimit, cfg.QsoForwardingRowLimit)
	assert.Equal(t, defaultRequiredConfigs.DatabaseWriteQueueSize, cfg.DatabaseWriteQueueSize)
}

func TestApplyForwardingDefaults_positiveValuesNotOverwritten(t *testing.T) {
	cfg := &types.RequiredConfigs{
		QsoForwardingPollIntervalSec: 999,
		QsoForwardingWorkerCount:     7,
		QsoForwardingQueueSize:       42,
		QsoForwardingRowLimit:        3,
		DatabaseWriteQueueSize:       50,
	}
	applyForwardingDefaults(cfg)

	assert.Equal(t, 999, cfg.QsoForwardingPollIntervalSec)
	assert.Equal(t, 7, cfg.QsoForwardingWorkerCount)
	assert.Equal(t, 42, cfg.QsoForwardingQueueSize)
	assert.Equal(t, 3, cfg.QsoForwardingRowLimit)
	assert.Equal(t, 50, cfg.DatabaseWriteQueueSize)
}

func TestApplyForwardingDefaults_negativeValuesGetDefaults(t *testing.T) {
	cfg := &types.RequiredConfigs{
		QsoForwardingPollIntervalSec: -1,
		QsoForwardingWorkerCount:     -5,
	}
	applyForwardingDefaults(cfg)

	assert.Equal(t, defaultRequiredConfigs.QsoForwardingPollIntervalSec, cfg.QsoForwardingPollIntervalSec)
	assert.Equal(t, defaultRequiredConfigs.QsoForwardingWorkerCount, cfg.QsoForwardingWorkerCount)
}
