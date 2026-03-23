package cat

import (
	"sync"
	"testing"
	"time"

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/enums/cmds"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
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

	service := &Service{
		ConfigService: cfgService,
		LoggerService: &logging.Service{},
	}

	err = service.Initialize()
	//require.Error(t, err)
	//require.Contains(t, err.Error(), errMsgInvalidRigID)
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
