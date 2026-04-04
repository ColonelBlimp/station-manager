package cat

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/cmds"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func TestInitFailureNilConfigService(t *testing.T) {
	service := &Service{}
	err := service.Initialize()
	require.Error(t, err)
	require.Contains(t, err.Error(), errMsgNilConfigService)
}

func TestInitFailureNilLoggerService(t *testing.T) {
	service := &Service{
		ConfigService: &config.Service{
			WorkingDir: "some/path",
			AppConfig:  types.AppConfig{},
		},
	}
	err := service.Initialize()
	require.Error(t, err)
	require.Contains(t, err.Error(), errMsgNilLoggerService)
}

func TestInitFailureInvalidRigID(t *testing.T) {
	cfgService := &config.Service{
		WorkingDir: ".",
		AppConfig:  types.AppConfig{},
	}
	err := cfgService.Initialize()
	require.NoError(t, err)
	cfgService.AppConfig.RequiredConfigs.DefaultRigID = 0

	service := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}

	err = service.Initialize()
	require.Error(t, err)
	require.Contains(t, err.Error(), errMsgInvalidRigID)
}

// helper to build a minimal, valid ConfigService for tests.
func newTestConfigService(t *testing.T) *config.Service {
	t.Helper()
	cfgService := &config.Service{
		AppConfig: types.AppConfig{
			RequiredConfigs: types.RequiredConfigs{DefaultRigID: 1},
			RigConfigs: []types.RigConfig{{
				ID:           1,
				SerialConfig: types.SerialConfig{},
				CatConfig: types.CatConfig{
					SendChannelSize:       1,
					ProcessingChannelSize: 1,
				},
			}},
		},
	}
	// Mark the config service as initialized so RequiredConfigs/RigConfigByID work.
	require.NoError(t, cfgService.Initialize())
	return cfgService
}

// newTestConfigServiceWithRig creates a config service in a temp directory with
// a custom config.json containing the supplied rig config. This allows tests to
// exercise the full Initialize() path with controlled rig values.
func newTestConfigServiceWithRig(t *testing.T, rig types.RigConfig) *config.Service {
	t.Helper()
	dir := t.TempDir()

	appCfg := types.AppConfig{
		RequiredConfigs: types.RequiredConfigs{DefaultRigID: rig.ID},
		RigConfigs:      []types.RigConfig{rig},
		DatastoreConfig: types.DatastoreConfig{
			Driver: "sqlite",
			Path:   "db/data.db",
		},
		LoggingConfig: types.LoggingConfig{Level: "debug"},
	}

	data, err := json.MarshalIndent(appCfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), data, 0644))

	cfgService := &config.Service{WorkingDir: dir}
	require.NoError(t, cfgService.Initialize())
	return cfgService
}

func TestInitWithContainer(t *testing.T) {
	// This test now verifies initialization and basic start/stop without IOCDI.
	cfgService := newTestConfigService(t)
	loggerService := &logging.Service{}

	cat := &Service{
		ConfigService: cfgService,
		LoggerService: loggerService,
	}

	require.NoError(t, cat.Initialize())

	require.NoError(t, cat.Start())
	require.NoError(t, cat.EnqueueCommand(cmds.Init))

	// Allow workers to spin briefly.
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, cat.Stop())
}

// TestServiceStartStopConcurrent ensures that concurrent calls to Start do not race,
// and that repeated Start/Stop cycles are safe.
func TestServiceStartStopConcurrent(t *testing.T) {
	cfgService := newTestConfigService(t)
	cat := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}

	// Ensure service is initialized before concurrent Start/Stop.
	require.NoError(t, cat.Initialize())

	// First, exercise multiple sequential Start/Stop cycles.
	for i := 0; i < 3; i++ {
		require.NoError(t, cat.Start())
		require.NoError(t, cat.Stop())
	}

	// Now exercise concurrent Start/Stop on a fresh run.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_ = cat.Start()
			// give Stop goroutine a chance to run
			time.Sleep(10 * time.Millisecond)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_ = cat.Stop()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Final clean Start/Stop to ensure consistent end state.
	require.NoError(t, cat.Start())
	require.NoError(t, cat.Stop())
}

// TestStatusChannelReceiveOnly verifies that StatusChannel returns a receive-only
// channel and that it behaves correctly when the service is initialized.
func TestStatusChannelReceiveOnly(t *testing.T) {
	cfgService := newTestConfigService(t)
	cat := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}

	// Ensure initialization succeeds so StatusChannel is available.
	require.NoError(t, cat.Initialize())

	ch, err := cat.StatusChannel()
	require.NoError(t, err)
	require.NotNil(t, ch)

	// This compile-time-only assertion ensures the returned type is receive-only.
	var _ <-chan types.CatStatus = ch
}

// TestEnqueueCommandFormatValidation ensures that EnqueueCommand fails fast
// when the configured command format string does not match the provided
// parameter count.
func TestEnqueueCommandFormatValidation(t *testing.T) {
	// Build a minimal in-memory Service with a single CatCommand using a format string.
	cfg := &types.RigConfig{
		CatConfig: types.CatConfig{
			Enabled:               true,
			SendChannelSize:       1,
			ProcessingChannelSize: 1,
		},
	}
	cfg.CatCommands = []types.CatCommand{{
		Name: cmds.Init.String(),
		Cmd:  "CMD %s %s", // expects 2 parameters
	}}

	service := &Service{
		ConfigService: &config.Service{},
		LoggerService: &logging.Service{},
		config:        cfg,
		sendChannel:   make(chan types.CatCommand, 1),
		catCommandIndex: map[string]types.CatCommand{
			cmds.Init.String(): cfg.CatCommands[0],
		},
	}
	service.initialized.Store(true)
	service.started.Store(true)

	// Happy path: correct parameter count.
	err := service.EnqueueCommand(cmds.Init, "one", "two")
	require.NoError(t, err)

	// Too few parameters.
	err = service.EnqueueCommand(cmds.Init, "only-one")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Command parameter validation failed")

	// Too many parameters.
	err = service.EnqueueCommand(cmds.Init, "one", "two", "three")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Command parameter validation failed")
}

// TestInitializeFailsOnEmptyCatStatePrefix verifies that the service
// initialization fails fast when a CatState has an empty prefix.
func TestInitializeFailsOnEmptyCatStatePrefix(t *testing.T) {
	cfgService := &config.Service{}
	loggerService := &logging.Service{}

	service := &Service{
		ConfigService: cfgService,
		LoggerService: loggerService,
	}

	// RigConfig with a single CatState that has an empty prefix.
	rigCfg := types.RigConfig{
		CatStates: []types.CatState{{
			Prefix: " ", // whitespace-only, should be treated as empty
		}},
	}

	// Stub out getRigConfig by assigning the config directly and bypassing
	// the normal ConfigService plumbing. Since Initialize ultimately calls
	// initializeStateSet, we can invoke it deterministically here.
	service.config = &rigCfg

	// Directly call initializeStateSet to validate behavior.
	err := service.initializeStateSet()
	require.Error(t, err)
	require.Contains(t, err.Error(), "CAT state entry has an empty prefix")
}

// ---------------------------------------------------------------------------
// Initialize edge cases
// ---------------------------------------------------------------------------

// TestInitializeIdempotent verifies that calling Initialize() twice returns nil both times
// and that the second call is a no-op (sync.Once path).
func TestInitializeIdempotent(t *testing.T) {
	cfgService := newTestConfigService(t)
	svc := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}

	require.NoError(t, svc.Initialize())
	require.NoError(t, svc.Initialize()) // second call is a no-op
}

// TestInitializeDefaultListenerInterval verifies that a zero or negative
// ListenerRateLimiterIntervalMS is replaced with the default (50).
func TestInitializeDefaultListenerInterval(t *testing.T) {
	cfgService := newTestConfigServiceWithRig(t, types.RigConfig{
		ID: 1,
		CatConfig: types.CatConfig{
			ListenerRateLimiterIntervalMS: 0, // zero → should be replaced
			SendChannelSize:               1,
			ProcessingChannelSize:         1,
		},
	})
	svc := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}
	require.NoError(t, svc.Initialize())
	require.Equal(t, defaultListenerIntervalMS, svc.config.CatConfig.ListenerRateLimiterIntervalMS)
}

// TestInitializeDefaultListenerReadTimeout verifies that a zero ListenerReadTimeoutMS
// falls back to SerialConfig.ReadTimeoutMS.
func TestInitializeDefaultListenerReadTimeout(t *testing.T) {
	cfgService := newTestConfigServiceWithRig(t, types.RigConfig{
		ID:           1,
		SerialConfig: types.SerialConfig{ReadTimeoutMS: 42},
		CatConfig: types.CatConfig{
			ListenerReadTimeoutMS: 0, // zero → should fall back
			SendChannelSize:       1,
			ProcessingChannelSize: 1,
		},
	})

	svc := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}
	require.NoError(t, svc.Initialize())
	require.Equal(t, 42, svc.config.CatConfig.ListenerReadTimeoutMS)
}

// TestInitializeChannelCreation verifies that after a successful init the three
// internal channels are non-nil and have the configured buffer sizes.
func TestInitializeChannelCreation(t *testing.T) {
	cfgService := newTestConfigServiceWithRig(t, types.RigConfig{
		ID: 1,
		CatConfig: types.CatConfig{
			SendChannelSize:       7,
			ProcessingChannelSize: 3,
		},
	})

	svc := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}
	require.NoError(t, svc.Initialize())

	require.NotNil(t, svc.statusChannel)
	require.Equal(t, 1, cap(svc.statusChannel)) // always 1
	require.NotNil(t, svc.sendChannel)
	require.Equal(t, 7, cap(svc.sendChannel))
	require.NotNil(t, svc.processingChannel)
	require.Equal(t, 3, cap(svc.processingChannel))
}

// ---------------------------------------------------------------------------
// Start edge cases
// ---------------------------------------------------------------------------

func TestStartNotInitialized(t *testing.T) {
	svc := &Service{}
	err := svc.Start()
	require.Error(t, err)
	require.Contains(t, err.Error(), errMsgServiceNotInit)
}

func TestStartDisabledConfig(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: false},
		},
	}
	svc.initialized.Store(true)

	require.NoError(t, svc.Start())
	require.False(t, svc.started.Load(), "service should not be marked as started when disabled")
}

func TestStartIdempotent(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
	}
	svc.initialized.Store(true)
	svc.started.Store(true) // simulate already started

	// Second Start should return nil (idempotent).
	require.NoError(t, svc.Start())
}

// ---------------------------------------------------------------------------
// Stop edge cases
// ---------------------------------------------------------------------------

func TestStopNotInitialized(t *testing.T) {
	svc := &Service{}
	err := svc.Stop()
	require.Error(t, err)
	require.Contains(t, err.Error(), errMsgServiceNotInit)
}

func TestStopDisabledConfig(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: false},
		},
	}
	svc.initialized.Store(true)

	require.NoError(t, svc.Stop())
}

func TestStopIdempotentNotStarted(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
	}
	svc.initialized.Store(true)

	// Stop when not started should be a no-op.
	require.NoError(t, svc.Stop())
}

func TestStopShutdownChannelAlreadyClosed(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
	}
	svc.initialized.Store(true)
	svc.started.Store(true)

	// Pre-close the shutdown channel to exercise the select/default path.
	shutdown := make(chan struct{})
	close(shutdown)
	svc.currentRun = &runState{shutdownChannel: shutdown}

	require.NoError(t, svc.Stop())
	require.False(t, svc.started.Load())
}

// ---------------------------------------------------------------------------
// StatusChannel edge cases
// ---------------------------------------------------------------------------

func TestStatusChannelNotInitialized(t *testing.T) {
	svc := &Service{}
	ch, err := svc.StatusChannel()
	require.Error(t, err)
	require.Nil(t, ch)
	require.Contains(t, err.Error(), errMsgServiceNotInit)
}

func TestStatusChannelNilChannel(t *testing.T) {
	svc := &Service{statusChannel: nil}
	svc.initialized.Store(true)

	ch, err := svc.StatusChannel()
	require.Error(t, err)
	require.Nil(t, ch)
	require.Contains(t, err.Error(), "Status channel is closed")
}

// ---------------------------------------------------------------------------
// EnqueueCommand edge cases
// ---------------------------------------------------------------------------

func TestEnqueueCommandNotInitialized(t *testing.T) {
	svc := &Service{}
	err := svc.EnqueueCommand(cmds.Init)
	require.Error(t, err)
	require.Contains(t, err.Error(), errMsgServiceNotInit)
}

func TestEnqueueCommandDisabledConfig(t *testing.T) {
	svc := &Service{
		LoggerService: &logging.Service{},
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: false},
		},
	}
	svc.initialized.Store(true)

	require.NoError(t, svc.EnqueueCommand(cmds.Init))
}

func TestEnqueueCommandNotStarted(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
	}
	svc.initialized.Store(true)
	// started is false

	err := svc.EnqueueCommand(cmds.Init)
	require.Error(t, err)
	require.Contains(t, err.Error(), errMsgServiceNotStarted)
}

func TestEnqueueCommandLookupFailure(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
		catCommandIndex: map[string]types.CatCommand{}, // empty index
		sendChannel:     make(chan types.CatCommand, 1),
	}
	svc.initialized.Store(true)
	svc.started.Store(true)

	err := svc.EnqueueCommand(cmds.Init)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Command lookup failed")
}

func TestEnqueueCommandChannelFull(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
		catCommandIndex: map[string]types.CatCommand{
			cmds.Init.String(): {Name: cmds.Init.String(), Cmd: "INIT;"},
		},
		sendChannel: make(chan types.CatCommand, 1),
	}
	svc.initialized.Store(true)
	svc.started.Store(true)

	// Fill the channel.
	svc.sendChannel <- types.CatCommand{}

	err := svc.EnqueueCommand(cmds.Init)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Send channel is full")
}

func TestEnqueueCommandNilChannel(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
		catCommandIndex: map[string]types.CatCommand{
			cmds.Init.String(): {Name: cmds.Init.String(), Cmd: "INIT;"},
		},
		sendChannel: nil,
	}
	svc.initialized.Store(true)
	svc.started.Store(true)

	err := svc.EnqueueCommand(cmds.Init)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Send channel is closed")
}

func TestEnqueueCommandHappyNoParams(t *testing.T) {
	svc := &Service{
		config: &types.RigConfig{
			CatConfig: types.CatConfig{Enabled: true},
		},
		catCommandIndex: map[string]types.CatCommand{
			cmds.Init.String(): {Name: cmds.Init.String(), Cmd: "FA;"},
		},
		sendChannel: make(chan types.CatCommand, 1),
	}
	svc.initialized.Store(true)
	svc.started.Store(true)

	require.NoError(t, svc.EnqueueCommand(cmds.Init))

	cmd := <-svc.sendChannel
	require.Equal(t, "FA;", cmd.Cmd)
}

// ---------------------------------------------------------------------------
// RigConfig edge cases
// ---------------------------------------------------------------------------

func TestRigConfigNilConfig(t *testing.T) {
	svc := &Service{}
	require.Equal(t, types.RigConfig{}, svc.RigConfig())
}

func TestRigConfigReturnsValue(t *testing.T) {
	expected := types.RigConfig{ID: 42}
	svc := &Service{config: &expected}

	got := svc.RigConfig()
	require.Equal(t, int64(42), got.ID)

	// Verify it's a copy (mutating the return value doesn't affect the service).
	got.ID = 99
	require.Equal(t, int64(42), svc.config.ID)
}
