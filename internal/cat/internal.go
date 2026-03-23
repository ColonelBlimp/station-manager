package cat

import (
	"github.com/Station-Manager/enums/cmds"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/serial"
	"github.com/Station-Manager/types"
	"strings"
)

// getRigConfig retrieves the default rig configuration based on the default rig ID from the required configurations.
// It returns an error if no valid default rig ID is set or if fetching the rig configuration fails.
func (s *Service) getRigConfig() (*types.RigConfig, error) {
	const op errors.Op = "cat.Service.getRigConfig"
	rigConfigs, err := s.ConfigService.RequiredConfigs()
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	defaultRigID := rigConfigs.DefaultRigID
	if defaultRigID < 1 {
		return nil, errors.New(op).Msg(errMsgInvalidRigID)
	}

	cfg, err := s.ConfigService.RigConfigByID(defaultRigID)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	return &cfg, nil
}

// initializeSerialPort initializes the serial port using the provided configuration in the Service instance.
// It returns an error if the serial port cannot be opened.
func (s *Service) initializeSerialPort() error {
	const op errors.Op = "cat.Service.initializeSerialPort"

	var err error
	s.serialPort, err = serial.Open(s.config.SerialConfig)
	if err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}

// initializeStateSet initializes the supportedCatStates map based on the configured CatState values in the service.
func (s *Service) initializeStateSet() error {
	const op errors.Op = "cat.Service.initializeStateSet"
	s.supportedCatStates = make(map[string]types.CatState, len(s.config.CatStates))

	maxLen := 0
	for _, state := range s.config.CatStates {
		key := strings.ToUpper(strings.TrimSpace(state.Prefix))
		if key == "" {
			// Treat empty prefixes as configuration errors instead of silently logging.
			return errors.New(op).Msg("CAT state entry has an empty prefix")
		}
		s.supportedCatStates[key] = state
		if l := len(key); l > maxLen {
			maxLen = l
		}
	}

	s.maxCatPrefixLen = maxLen
	return nil
}

// launchWorkerThread starts a new goroutine for the given worker function and manages its lifecycle using a wait group.
func (s *Service) launchWorkerThread(run *runState, workerFunc func(<-chan struct{}), workerName string) {
	run.wg.Add(1)
	go func() {
		defer run.wg.Done()
		s.LoggerService.InfoWith().Str("worker", workerName).Msg("CAT starting")
		workerFunc(run.shutdownChannel)
		s.LoggerService.InfoWith().Str("worker", workerName).Msg("CAT stopped")
	}()
}

// commandLookup retrieves a CatCommand by its name from the service configuration. Returns an error if the command is not found.
func (s *Service) commandLookup(name cmds.CatCmdName) (types.CatCommand, error) {
	const op errors.Op = "cat.Service.commandLookup"
	for _, c := range s.config.CatCommands {
		if c.Name == name.String() {
			return c, nil
		}
	}
	return types.CatCommand{}, errors.New(op).Msgf("command %s not found", name)
}

// validateCommandFormat checks the format of the command string against the provided parameters.
// It returns an error if the number of parameters does not match the format specifiers.
func (s *Service) validateCommandFormat(command string, params ...interface{}) error {
	const op errors.Op = "cat.Service.validateCommandFormat"

	// Count the number of %s in the command string as a simple way to validate format specifiers.
	expectedParams := strings.Count(command, "%s")
	providedParams := len(params)

	if expectedParams != providedParams {
		return errors.New(op).Msgf("invalid command format: expected %d parameters, got %d", expectedParams, providedParams)
	}

	return nil
}
