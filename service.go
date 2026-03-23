package cat

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/enums/cmds"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/serial"
	"github.com/Station-Manager/types"
)

const (
	ServiceName = types.CatServiceName
	// defaultListenerIntervalMS is used when the configured ListenerRateLimiterInterval
	// is zero or negative, to avoid creating a ticker with a non-positive duration.
	defaultListenerIntervalMS = 50
)

type runState struct {
	shutdownChannel chan struct{}
	wg              sync.WaitGroup
}

type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	LoggerService *logging.Service `di.inject:"loggingservice"`
	config        *types.RigConfig

	serialPort *serial.Port

	supportedCatStates map[string]types.CatState
	maxCatPrefixLen    int

	initialized atomic.Bool
	started     atomic.Bool // guarded via atomic operations; Start/Stop also hold mu for a broader state

	initOnce sync.Once
	mu       sync.Mutex

	currentRun *runState

	statusChannel     chan types.CatStatus
	sendChannel       chan types.CatCommand
	processingChannel chan types.CatState
}

// Initialize ensures the service is properly set up by initializing required components and loading configurations.
// It is safe to call multiple times. The IOCDI container will ensure this method is called.
func (s *Service) Initialize() error {
	const op errors.Op = "cat.Service.Initialize"

	var initErr error
	s.initOnce.Do(func() {
		if s.ConfigService == nil {
			initErr = errors.New(op).Msg(errMsgNilConfigService)
			return
		}

		if s.LoggerService == nil {
			initErr = errors.New(op).Msg(errMsgNilLoggerService)
			return
		}

		cfg, err := s.getRigConfig()
		if err != nil {
			initErr = err
			return
		}

		if initErr = validateConfig(cfg); initErr != nil {
			return
		}

		// Ensure sensible defaults for CAT timing configuration to avoid panics
		// when creating tickers or timeouts with non-positive durations.
		if cfg.CatConfig.ListenerRateLimiterIntervalMS <= 0 {
			cfg.CatConfig.ListenerRateLimiterIntervalMS = defaultListenerIntervalMS
		}
		if cfg.CatConfig.ListenerReadTimeoutMS <= 0 {
			cfg.CatConfig.ListenerReadTimeoutMS = cfg.SerialConfig.ReadTimeoutMS
		}

		s.config = cfg

		if initErr = s.initializeStateSet(); initErr != nil {
			return
		}

		// This channel is non-blocking and buffered to avoid deadlocks. Leaving it a 1 ensures that
		// the status stream is “latest-wins” so that the caller (the frontend) should not lag behind.
		s.statusChannel = make(chan types.CatStatus, 1)
		s.sendChannel = make(chan types.CatCommand, s.config.CatConfig.SendChannelSize)
		s.processingChannel = make(chan types.CatState, s.config.CatConfig.ProcessingChannelSize)

		s.initialized.Store(true)
	})

	return initErr
}

// Start initializes and starts the service if it has been properly configured and is not yet running.
func (s *Service) Start() error {
	const op errors.Op = "cat.Service.Start"
	if !s.initialized.Load() {
		return errors.New(op).Msg(errMsgServiceNotInit)
	}

	if !s.config.CatConfig.Enabled {
		s.LoggerService.InfoWith().Msg("CAT service is disabled in configuration; not starting.")
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// If already started, treat Start as idempotent and return nil.
	if s.started.Load() {
		return nil
	}

	if err := s.initializeSerialPort(); err != nil {
		return errors.New(op).Err(err).Msg("Failed to initialize serial port.")
	}

	run := &runState{
		shutdownChannel: make(chan struct{}),
	}
	s.currentRun = run

	s.launchWorkerThread(run, s.serialPortListener, "serialPortListener")
	s.launchWorkerThread(run, s.serialPortSender, "serialPortSender")
	s.launchWorkerThread(run, s.lineProcessor, "lineProcessor")

	s.started.Store(true)

	return nil
}

// Stop safely stops the service by shutting down active processes, releasing resources, and closing the serial port.
func (s *Service) Stop() error {
	const op errors.Op = "cat.Service.Stop"
	if !s.initialized.Load() {
		return errors.New(op).Msg(errMsgServiceNotInit)
	}

	if !s.config.CatConfig.Enabled {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// If not started, treat Stop as idempotent and return nil.
	if !s.started.Load() {
		return nil
	}

	run := s.currentRun
	if run != nil && run.shutdownChannel != nil {
		select {
		case <-run.shutdownChannel:
			// already closed; nothing to do
		default:
			close(run.shutdownChannel)
		}
	}
	// NOTE: we do not close any of the other channels here, as they may be in use by other goroutines
	// which would panic on 'send' if the channel were closed. All goroutines exit via shutdownChannel,
	// so these channels will eventually become unreachable and be garbage-collected rather than being
	// explicitly closed. This design avoids close-while-send races at the cost of relying on the Go
	// runtime's garbage collector to reclaim channel resources once the Service is stopped and no
	// references remain.

	if run != nil {
		run.wg.Wait()
	}

	if s.serialPort != nil {
		if err := s.serialPort.Close(); err != nil {
			return errors.New(op).Msgf("Failed to close serial port: %v", err)
		}
		s.serialPort = nil
	}

	s.currentRun = nil
	s.started.Store(false)

	return nil
}

// StatusChannel returns a channel for monitoring cat status changes or an error if the service is uninitialized or closed.
func (s *Service) StatusChannel() (<-chan types.CatStatus, error) {
	const op errors.Op = "cat.Service.StatusChannel"
	if !s.initialized.Load() {
		return nil, errors.New(op).Msg(errMsgServiceNotInit)
	}

	if s.statusChannel == nil {
		return nil, errors.New(op).Msg("Status channel is closed.")
	}

	return s.statusChannel, nil
}

// EnqueueCommand queues a command with the given name and parameters for execution, ensuring the service is initialized
// and started. Returns an error if the service is not ready, the command lookup fails, or the sendChannel is full or closed.
func (s *Service) EnqueueCommand(cmdName cmds.CatCmdName, params ...string) error {
	const op errors.Op = "cat.Service.EnqueueCommand"
	if !s.initialized.Load() {
		return errors.New(op).Msg(errMsgServiceNotInit)
	}

	if !s.config.CatConfig.Enabled {
		s.LoggerService.InfoWith().Msg("CAT service is disabled in configuration")
		return nil
	}

	if !s.started.Load() {
		return errors.New(op).Msg(errMsgServiceNotStarted)
	}

	catCmd, err := s.commandLookup(cmdName)
	if err != nil {
		return errors.New(op).Msgf("Command lookup failed: %v", err)
	}

	paramsInterface := make([]interface{}, len(params))
	for i, v := range params {
		paramsInterface[i] = v
	}

	// Validate the format string against provided parameters to avoid runtime panics from fmt.Sprintf.
	if err := s.validateCommandFormat(catCmd.Cmd, paramsInterface...); err != nil {
		return errors.New(op).Err(err).Msg("Command parameter validation failed")
	}

	pCmd := &catCmd
	pCmd.Cmd = fmt.Sprintf(catCmd.Cmd, paramsInterface...)
	catCmd = *pCmd

	// Command is fully defined in configuration and already validated for format/arity,
	// so no additional sanitization is required here.

	if s.sendChannel != nil {
		select {
		case s.sendChannel <- catCmd:
			return nil
		default:
			return errors.New(op).Msg("Send channel is full.")
		}
	}
	return errors.New(op).Msg("Send channel is closed.")
}

// RigConfig returns the rig configuration for the service, or an empty configuration if the service is not initialized.
// This provides a copy of the current rig configuration, for other consumers, e.g., frontend facades.
func (s *Service) RigConfig() types.RigConfig {
	if s.config == nil {
		return types.RigConfig{}
	}
	return *s.config
}
