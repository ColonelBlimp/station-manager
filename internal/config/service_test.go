package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestInitialize_createsDefaultConfig ensures Initialize writes a default config and populates fields.
func TestInitialize_createsDefaultConfig(t *testing.T) {
	workDir := t.TempDir()

	svc := &Service{WorkingDir: workDir}
	require.NoError(t, svc.Initialize())

	// Verify config.json exists
	cfgPath := filepath.Join(workDir, configFileName)
	_, err := os.Stat(cfgPath)
	require.NoError(t, err, "expected %s to exist", cfgPath)

	dbCfg, err := svc.DatastoreConfig()
	require.NoError(t, err)
	assert.Equal(t, types.SqliteDriverName, dbCfg.Driver)

	logCfg, err := svc.LoggingConfig()
	require.NoError(t, err)
	assert.NotEmpty(t, logCfg.Level)
}

// TestInitialize_idempotent ensures multiple Initialize calls are safe and do not error.
func TestInitialize_idempotent(t *testing.T) {
	workDir := t.TempDir()
	svc := &Service{WorkingDir: workDir}

	require.NoError(t, svc.Initialize(), "first Initialize()")
	require.NoError(t, svc.Initialize(), "second Initialize() should be a no-op")
}

func TestInitialize_envSelectsSqlite(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv(EnvSmDefaultDB) })
	require.NoError(t, os.Setenv(EnvSmDefaultDB, "sqlite"))

	svc := &Service{WorkingDir: t.TempDir()}
	require.NoError(t, svc.Initialize())

	dbCfg, err := svc.DatastoreConfig()
	require.NoError(t, err)
	assert.Equal(t, types.SqliteDriverName, dbCfg.Driver)
}

func TestInitialize_envSelectsPostgres(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv(EnvSmDefaultDB) })
	require.NoError(t, os.Setenv(EnvSmDefaultDB, "postgres"))

	svc := &Service{WorkingDir: t.TempDir()}
	require.NoError(t, svc.Initialize())

	dbCfg, err := svc.DatastoreConfig()
	require.NoError(t, err)
	assert.Equal(t, types.PostgresDriverName, dbCfg.Driver)
}

// TestInitialize_unrecognizedEnvVarReturnsError ensures that an unknown SM_DEFAULT_DB
// value causes Initialize to return an error rather than silently using the default.
func TestInitialize_unrecognizedEnvVarReturnsError(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv(EnvSmDefaultDB) })
	require.NoError(t, os.Setenv(EnvSmDefaultDB, "mysql"))

	svc := &Service{WorkingDir: t.TempDir()}
	err := svc.Initialize()
	require.Error(t, err, "expected error for unrecognized SM_DEFAULT_DB value")
	assert.False(t, svc.isInitialized.Load(), "service must not be marked initialized after failure")
}
