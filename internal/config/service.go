package config

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
	"github.com/goccy/go-json"
)

type Service struct {
	WorkingDir    string `di.inject:"workingdir"`
	AppConfig     types.AppConfig
	isInitialized atomic.Bool
	initOnce      sync.Once
}

// Initialize initializes the config service.
func (s *Service) Initialize() error {
	const op errors.Op = "config.Service.Initialize"

	if s.isInitialized.Load() {
		return nil // Exit gracefully
	}

	var initErr error
	s.initOnce.Do(func() {
		var err error

		// This is for situation where the service is not built with an IOCDI container.
		if s.WorkingDir == "" {
			if s.WorkingDir, err = utils.WorkingDir(s.WorkingDir); err != nil {
				initErr = errors.New(op).Err(err).Msg(errMsgWorkingDir)
				return
			}
		}

		// If a LoggingConfig has been pre-seeded (common in tests), preserve it
		// while still loading the remaining configuration from disk.
		preseedLogCfg := s.AppConfig.LoggingConfig

		if err = s.loadConfigFile(); err != nil {
			initErr = errors.New(op).Err(err)
			return
		}

		// Restore pre-seeded LoggingConfig if it was provided (Level is our sentinel)
		if preseedLogCfg.Level != "" {
			s.AppConfig.LoggingConfig = preseedLogCfg
		}

		// Early validation of loaded configuration
		if err = validateAppConfig(&s.AppConfig); err != nil {
			initErr = errors.New(op).Err(err)
			return
		}

		s.isInitialized.Store(true)
	})

	return initErr
}

// DatastoreConfig returns the datastore configuration.
func (s *Service) DatastoreConfig() (types.DatastoreConfig, error) {
	const op errors.Op = "config.Service.DatastoreConfig"
	emptyRetVal := types.DatastoreConfig{}

	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}

	return s.AppConfig.DatastoreConfig, nil
}

// LoggingConfig returns the logging configuration.
func (s *Service) LoggingConfig() (types.LoggingConfig, error) {
	const op errors.Op = "config.Service.LoggingConfig"
	emptyRetVal := types.LoggingConfig{}

	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}

	return s.AppConfig.LoggingConfig, nil
}

// ServerConfig returns the server configuration from the application configuration. It requires the service to be initialized.
func (s *Service) ServerConfig() (*types.ServerConfig, error) {
	const op errors.Op = "config.Service.ServerConfig"

	if !s.isInitialized.Load() {
		return nil, errors.New(op).Msg(errMsgNotInitialized)
	}

	return s.AppConfig.ServerConfig, nil
}

// RequiredConfigs retrieves the required configurations for the application. Returns an error if the service is uninitialized.
func (s *Service) RequiredConfigs() (types.RequiredConfigs, error) {
	const op errors.Op = "config.Service.RequiredConfigs"

	if !s.isInitialized.Load() {
		return types.RequiredConfigs{}, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.AppConfig.RequiredConfigs, nil
}

// RigConfigByID retrieves the RigConfig for the given rig ID from the service's AppConfig. Returns an error if unavailable.
func (s *Service) RigConfigByID(rigID int64) (types.RigConfig, error) {
	const op errors.Op = "config.Service.RigConfigByID"
	emptyRetVal := types.RigConfig{}

	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}
	if rigID == 0 {
		return emptyRetVal, errors.New(op).Errorf("Invalid rig ID: %d", rigID)
	}

	for _, rig := range s.AppConfig.RigConfigs {
		if rig.ID == rigID {
			return rig, nil
		}
	}

	return emptyRetVal, nil
}

// CatStateValues retrieves the CAT state values for the default rig configuration in the service's application configuration.
// Returns a map of state values organized by tags or an error if the service is uninitialized or fails to retrieve the configuration.
func (s *Service) CatStateValues() (types.StateValues, error) {
	const op errors.Op = "config.Service.CatStateValues"

	if !s.isInitialized.Load() {
		return nil, errors.New(op).Msg(errMsgNotInitialized)
	}

	stateValues := make(types.StateValues)
	rigConfig, err := s.RigConfigByID(s.AppConfig.RequiredConfigs.DefaultRigID)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	for _, state := range rigConfig.CatStates {
		for _, marker := range state.Markers {
			if len(marker.ValueMappings) == 0 {
				continue
			}
			values := make(map[string]string, len(marker.ValueMappings))
			for _, mapping := range marker.ValueMappings {
				values[mapping.Key] = mapping.Value
			}
			stateValues[marker.Tag] = values
		}
	}

	return stateValues, nil
}

// LoggingStationConfigs retrieves the logging station configuration from the service's application configuration.
func (s *Service) LoggingStationConfigs() (types.LoggingStation, error) {
	const op errors.Op = "config.Service.LoggingStationConfigs"
	emptyRetVal := types.LoggingStation{}
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}

	return s.AppConfig.LoggingStation, nil
}

// LookupServiceConfig fetches the configuration for a given service by its name from the loaded application settings.
func (s *Service) LookupServiceConfig(serviceName string) (types.LookupConfig, error) {
	const op errors.Op = "config.Service.LookupServiceConfig"
	emptyRetVal := types.LookupConfig{}
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}

	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return emptyRetVal, errors.New(op).Msg("service name cannot be empty")
	}

	for _, cfg := range s.AppConfig.LookupServiceConfigs {
		if cfg.Name == serviceName {
			return cfg, nil
		}
	}

	return emptyRetVal, errors.New(op).Msgf("service config not found for: %s", serviceName)
}

// ForwarderConfig retrieves the forwarder configuration for the specified service name.
// Returns a ForwarderConfig object and nil error if found, otherwise returns an empty object and an appropriate error.
func (s *Service) ForwarderConfig(serviceName string) (types.ForwarderConfig, error) {
	const op errors.Op = "config.Service.ForwarderConfig"
	emptyRetVal := types.ForwarderConfig{}
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}

	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return emptyRetVal, errors.New(op).Msg("service name cannot be empty")
	}

	for _, cfg := range s.AppConfig.ForwardingConfigs {
		if cfg.Name == serviceName {
			return cfg, nil
		}
	}

	return emptyRetVal, errors.New(op).Msgf("service config not found for: %s", serviceName)
}

// ForwarderConfigs retrieves the list of forwarder configurations from the application configuration.
func (s *Service) ForwarderConfigs() ([]types.ForwarderConfig, error) {
	const op errors.Op = "config.Service.ForwarderConfigs"
	var emptyRetVal []types.ForwarderConfig
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.AppConfig.ForwardingConfigs, nil
}

// EmailConfig retrieves the email configuration from the application configuration. Returns an error if uninitialized.
func (s *Service) EmailConfig() (types.EmailConfig, error) {
	const op errors.Op = "config.Service.EmailConfig"
	emptyRetVal := types.EmailConfig{}
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.AppConfig.EmailConfigs, nil
}

// OptionalConfigs retrieves optional configuration settings from the service if it has been properly initialized.
func (s *Service) OptionalConfigs() (types.OptionalConfigs, error) {
	const op errors.Op = "config.Service.OptionalConfigs"
	emptyRetVal := types.OptionalConfigs{}
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.AppConfig.OptionalConfigs, nil
}

// ListenerConfigs retrieves the listener configuration from the application configuration.
// Returns an empty slice if the service is not initialized.
func (s *Service) ListenerConfigs() ([]types.ListenerConfig, error) {
	const op errors.Op = "config.Service.ListenerConfigs"
	var emptyRetVal []types.ListenerConfig
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.AppConfig.ListenerConfigs, nil
}

// UpdateAppConfig updates the application configuration and writes it to the configuration file.
func (s *Service) UpdateAppConfig(cfg types.AppConfig) error {
	const op errors.Op = "config.Service.UpdateAppConfig"
	if !s.isInitialized.Load() {
		return errors.New(op).Msg(errMsgNotInitialized)
	}

	// Pretty-print selected configuration for readability
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return errors.New(op).Err(err)
	}

	return writeDataToFile(data, filepath.Join(s.WorkingDir, configFileName))
}
