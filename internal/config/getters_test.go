package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newInitializedService creates a Service with a fresh temp directory and initializes it.
func newInitializedService(t *testing.T) *Service {
	t.Helper()
	svc := &Service{WorkingDir: t.TempDir()}
	require.NoError(t, svc.Initialize())
	return svc
}

// --- Initialize edge cases ---

func TestInitialize_retryAfterFailureReturnsOriginalError(t *testing.T) {
	// Point at a non-existent directory so loadConfigFile will fail.
	svc := &Service{WorkingDir: "/nonexistent/path/that/does/not/exist"}

	err1 := svc.Initialize()
	require.Error(t, err1, "expected error on first Initialize()")

	err2 := svc.Initialize()
	require.Error(t, err2, "expected same error on retry, not nil")
}

func TestInitialize_preseedLoggingConfigPreserved(t *testing.T) {
	svc := &Service{WorkingDir: t.TempDir()}
	svc.AppConfig.LoggingConfig = types.LoggingConfig{
		Level:         "debug",
		RelLogFileDir: "custom-logs",
	}

	require.NoError(t, svc.Initialize())

	logCfg, err := svc.LoggingConfig()
	require.NoError(t, err)
	assert.Equal(t, "debug", logCfg.Level, "pre-seeded Level should be preserved")
	assert.Equal(t, "custom-logs", logCfg.RelLogFileDir, "pre-seeded RelLogFileDir should be preserved")
}

func TestInitialize_loadsExistingConfigFile(t *testing.T) {
	workDir := t.TempDir()

	// First init generates config.json
	svc1 := &Service{WorkingDir: workDir}
	require.NoError(t, svc1.Initialize())

	// Second independent service reads the existing file
	svc2 := &Service{WorkingDir: workDir}
	require.NoError(t, svc2.Initialize())

	dbCfg, err := svc2.DatastoreConfig()
	require.NoError(t, err)
	assert.Equal(t, types.SqliteDriverName, dbCfg.Driver)
}

func TestInitialize_envAliasPg(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv(EnvSmDefaultDB) })
	require.NoError(t, os.Setenv(EnvSmDefaultDB, "pg"))

	svc := &Service{WorkingDir: t.TempDir()}
	require.NoError(t, svc.Initialize())

	dbCfg, err := svc.DatastoreConfig()
	require.NoError(t, err)
	assert.Equal(t, types.PostgresDriverName, dbCfg.Driver)
}

func TestInitialize_envAliasPostgresql(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv(EnvSmDefaultDB) })
	require.NoError(t, os.Setenv(EnvSmDefaultDB, "postgresql"))

	svc := &Service{WorkingDir: t.TempDir()}
	require.NoError(t, svc.Initialize())

	dbCfg, err := svc.DatastoreConfig()
	require.NoError(t, err)
	assert.Equal(t, types.PostgresDriverName, dbCfg.Driver)
}

// --- ServerConfig ---

func TestServerConfig_notInitialized(t *testing.T) {
	_, err := (&Service{}).ServerConfig()
	assert.Error(t, err)
}

func TestServerConfig_nilWhenNotSet(t *testing.T) {
	svc := newInitializedService(t) // sqlite desktop config has no ServerConfig
	cfg, err := svc.ServerConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

// --- RequiredConfigs ---

func TestRequiredConfigs_notInitialized(t *testing.T) {
	_, err := (&Service{}).RequiredConfigs()
	assert.Error(t, err)
}

func TestRequiredConfigs_returnsDefaults(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.RequiredConfigs()
	require.NoError(t, err)
	assert.Equal(t, defaultRequiredConfigs.PaginationPageSize, cfg.PaginationPageSize)
	assert.Equal(t, defaultRequiredConfigs.DefaultMode, cfg.DefaultMode)
	assert.True(t, cfg.QsoForwardingPollIntervalSec > 0)
}

// --- RigConfigByID ---

func TestRigConfigByID_notInitialized(t *testing.T) {
	_, err := (&Service{}).RigConfigByID(1)
	assert.Error(t, err)
}

func TestRigConfigByID_zeroIDReturnsError(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.RigConfigByID(0)
	assert.Error(t, err)
}

func TestRigConfigByID_negativeIDReturnsError(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.RigConfigByID(-1)
	assert.Error(t, err)
}

func TestRigConfigByID_notFoundReturnsError(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.RigConfigByID(999)
	assert.Error(t, err)
}

func TestRigConfigByID_found(t *testing.T) {
	svc := newInitializedService(t)
	rig, err := svc.RigConfigByID(1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rig.ID)
	assert.NotEmpty(t, rig.Name)
}

// --- CatStateValues ---

func TestCatStateValues_notInitialized(t *testing.T) {
	_, err := (&Service{}).CatStateValues()
	assert.Error(t, err)
}

func TestCatStateValues_returnsValueMappings(t *testing.T) {
	svc := newInitializedService(t)
	sv, err := svc.CatStateValues()
	require.NoError(t, err)
	// The default rig (FTdx10) has value mappings for identity, split, select, mainMode, subMode
	assert.NotEmpty(t, sv, "expected non-empty state values")
	// Spot-check: split has OFF/ON mappings
	split, ok := sv[defaultRigConfigs[0].CatStates[3].Markers[0].Tag]
	assert.True(t, ok, "expected split state values to be present")
	assert.Equal(t, "OFF", split["0"])
	assert.Equal(t, "ON", split["1"])
}

func TestCatStateValues_errorWhenNoDefaultRig(t *testing.T) {
	svc := newInitializedService(t)
	// Override rig list and default rig ID to simulate missing rig
	svc.AppConfig.RigConfigs = []types.RigConfig{}
	svc.AppConfig.RequiredConfigs.DefaultRigID = 42

	_, err := svc.CatStateValues()
	assert.Error(t, err)
}

// --- LoggingStationConfig ---

func TestLoggingStationConfig_notInitialized(t *testing.T) {
	_, err := (&Service{}).LoggingStationConfig()
	assert.Error(t, err)
}

func TestLoggingStationConfig_returnsValue(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.LoggingStationConfig()
	assert.NoError(t, err)
}

// --- LookupServiceConfig ---

func TestLookupServiceConfig_notInitialized(t *testing.T) {
	_, err := (&Service{}).LookupServiceConfig(types.HamNutLookupServiceName)
	assert.Error(t, err)
}

func TestLookupServiceConfig_emptyName(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.LookupServiceConfig("")
	assert.Error(t, err)
}

func TestLookupServiceConfig_whitespaceOnlyName(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.LookupServiceConfig("   ")
	assert.Error(t, err)
}

func TestLookupServiceConfig_notFound(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.LookupServiceConfig("nonexistent-service")
	assert.Error(t, err)
}

func TestLookupServiceConfig_found_hamnut(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.LookupServiceConfig(types.HamNutLookupServiceName)
	require.NoError(t, err)
	assert.Equal(t, types.HamNutLookupServiceName, cfg.Name)
}

func TestLookupServiceConfig_found_qrz(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.LookupServiceConfig(types.QrzLookupServiceName)
	require.NoError(t, err)
	assert.Equal(t, types.QrzLookupServiceName, cfg.Name)
}

// --- ForwarderConfig ---

func TestForwarderConfig_notInitialized(t *testing.T) {
	_, err := (&Service{}).ForwarderConfig(types.QrzForwardingServiceName)
	assert.Error(t, err)
}

func TestForwarderConfig_emptyName(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.ForwarderConfig("")
	assert.Error(t, err)
}

func TestForwarderConfig_whitespaceOnlyName(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.ForwarderConfig("   ")
	assert.Error(t, err)
}

func TestForwarderConfig_notFound(t *testing.T) {
	svc := newInitializedService(t)
	_, err := svc.ForwarderConfig("nonexistent")
	assert.Error(t, err)
}

func TestForwarderConfig_found(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.ForwarderConfig(types.QrzForwardingServiceName)
	require.NoError(t, err)
	assert.Equal(t, types.QrzForwardingServiceName, cfg.Name)
}

// --- ForwarderConfigs ---

func TestForwarderConfigs_notInitialized(t *testing.T) {
	_, err := (&Service{}).ForwarderConfigs()
	assert.Error(t, err)
}

func TestForwarderConfigs_returnsList(t *testing.T) {
	svc := newInitializedService(t)
	cfgs, err := svc.ForwarderConfigs()
	require.NoError(t, err)
	assert.NotEmpty(t, cfgs)
}

// --- EmailConfig ---

func TestEmailConfig_notInitialized(t *testing.T) {
	_, err := (&Service{}).EmailConfig()
	assert.Error(t, err)
}

func TestEmailConfig_returnsValue(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.EmailConfig()
	require.NoError(t, err)
	assert.Equal(t, types.EmailServiceName, cfg.Name)
}

// --- OptionalConfigs ---

func TestOptionalConfigs_notInitialized(t *testing.T) {
	_, err := (&Service{}).OptionalConfigs()
	assert.Error(t, err)
}

func TestOptionalConfigs_returnsValue(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.OptionalConfigs()
	require.NoError(t, err)
	assert.Equal(t, defaultOptionalConfigs.QrzViewUrl, cfg.QrzViewUrl)
}

// --- ListenerConfigs ---

func TestListenerConfigs_notInitialized(t *testing.T) {
	_, err := (&Service{}).ListenerConfigs()
	assert.Error(t, err)
}

func TestListenerConfigs_returnsList(t *testing.T) {
	svc := newInitializedService(t)
	cfgs, err := svc.ListenerConfigs()
	require.NoError(t, err)
	assert.NotEmpty(t, cfgs)
}

// --- AudioPlaybackConfig ---

func TestAudioPlaybackConfig_notInitialized(t *testing.T) {
	_, err := (&Service{}).AudioPlaybackConfig()
	assert.Error(t, err)
}

func TestAudioPlaybackConfig_returnsValue(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.AudioPlaybackConfig()
	require.NoError(t, err)
	// Default desktop config has DeviceIndex -1 and BufferSize 512
	assert.Equal(t, -1, cfg.DeviceIndex)
	assert.Equal(t, uint32(512), cfg.BufferSize)
}

// --- FT8Config ---

func TestFT8Config_notInitialized(t *testing.T) {
	_, err := (&Service{}).FT8Config()
	assert.Error(t, err)
}

func TestFT8Config_returnsValue(t *testing.T) {
	svc := newInitializedService(t)
	cfg, err := svc.FT8Config()
	require.NoError(t, err)
	// Default desktop config has DeviceIndex -1 and BufferSize 512
	assert.Equal(t, -1, cfg.DeviceIndex)
	assert.Equal(t, uint32(512), cfg.BufferSize)
}

// --- UpdateAppConfig ---

func TestUpdateAppConfig_notInitialized(t *testing.T) {
	err := (&Service{}).UpdateAppConfig(types.AppConfig{})
	assert.Error(t, err)
}

func TestUpdateAppConfig_updatesBothDiskAndMemory(t *testing.T) {
	workDir := t.TempDir()
	svc := &Service{WorkingDir: workDir}
	require.NoError(t, svc.Initialize())

	// Build an updated config
	updated := svc.AppConfig
	updated.OptionalConfigs.QrzViewUrl = "https://updated.example.com/"

	require.NoError(t, svc.UpdateAppConfig(updated))

	// In-memory state updated
	optCfg, err := svc.OptionalConfigs()
	require.NoError(t, err)
	assert.Equal(t, "https://updated.example.com/", optCfg.QrzViewUrl)

	// Disk state updated — new service reads the changed file
	svc2 := &Service{WorkingDir: workDir}
	require.NoError(t, svc2.Initialize())
	optCfg2, err := svc2.OptionalConfigs()
	require.NoError(t, err)
	assert.Equal(t, "https://updated.example.com/", optCfg2.QrzViewUrl)
}

func TestUpdateAppConfig_diskWriteFailureDoesNotUpdateMemory(t *testing.T) {
	svc := newInitializedService(t)
	original := svc.AppConfig.OptionalConfigs.QrzViewUrl

	updated := svc.AppConfig
	updated.OptionalConfigs.QrzViewUrl = "https://should-not-be-set.com/"

	// Redirect writes to a non-existent directory so the write always fails,
	// even under root (unlike a chmod-based approach).
	svc.WorkingDir = filepath.Join(t.TempDir(), "nonexistent", "subdir")

	err := svc.UpdateAppConfig(updated)
	assert.Error(t, err, "expected error when write fails")

	// In-memory state must be unchanged
	assert.Equal(t, original, svc.AppConfig.OptionalConfigs.QrzViewUrl)
}

// TestUpdateAppConfig_concurrentReadsDuringWrite verifies that concurrent getter
// calls during an UpdateAppConfig do not race. Run with -race to detect violations.
func TestUpdateAppConfig_concurrentReadsDuringWrite(t *testing.T) {
	svc := newInitializedService(t)

	var wg sync.WaitGroup
	// Spawn readers
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.OptionalConfigs()
			_, _ = svc.LoggingConfig()
			_, _ = svc.DatastoreConfig()
		}()
	}
	// Spawn writers
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := svc.AppConfig
			cfg.OptionalConfigs.QrzViewUrl = "https://concurrent.example.com/"
			_ = svc.UpdateAppConfig(cfg)
		}()
	}
	wg.Wait()
}
