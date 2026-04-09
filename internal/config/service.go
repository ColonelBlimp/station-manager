// internal/config/service.go
package config

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/goccy/go-json"
)

type Service struct {
	WorkingDir    string `di.inject:"workingdir"`
	AppConfig     types.AppConfig
	isInitialized atomic.Bool
	initOnce      sync.Once
	initErr       error        // written only inside initOnce.Do; safe to read after Do returns (sync.Once memory guarantee)
	mu            sync.RWMutex // protects AppConfig against races between UpdateAppConfig and getters
}

// appConfig returns a snapshot copy of AppConfig under the read lock.
// All getters must use this to avoid races with concurrent UpdateAppConfig calls.
func (s *Service) appConfig() types.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AppConfig
}

// Initialize initializes the config service. It is safe to call concurrently;
// only the first call performs initialization. If initialization fails, all
// subsequent calls return the same error — the service cannot recover from a
// failed initialization.
func (s *Service) Initialize() error {
	const op errors.Op = "config.Service.Initialize"

	if s.isInitialized.Load() {
		return nil
	}

	s.initOnce.Do(func() {
		var err error

		// This is for situation where the service is not built with an IOCDI container.
		if s.WorkingDir == "" {
			if s.WorkingDir, err = utils.WorkingDir(); err != nil {
				s.initErr = errors.New(op).Err(err).Msg(errMsgWorkingDir)
				return
			}
		}

		// If a LoggingConfig has been pre-seeded (common in tests), preserve it
		// while still loading the remaining configuration from disk.
		preseedLogCfg := s.AppConfig.LoggingConfig

		if err = s.loadConfigFile(); err != nil {
			s.initErr = errors.New(op).Err(err)
			return
		}

		// Restore pre-seeded LoggingConfig if it was provided (Level is our sentinel)
		if preseedLogCfg.Level != "" {
			s.AppConfig.LoggingConfig = preseedLogCfg
		}

		// Early validation of loaded configuration
		if err = validateAppConfig(&s.AppConfig); err != nil {
			s.initErr = errors.New(op).Err(err)
			return
		}

		s.isInitialized.Store(true)
	})

	return s.initErr
}

// DatastoreConfig returns the datastore configuration.
//
// For sqlite, relative paths are resolved against WorkingDir in the returned
// copy only — the on-disk config retains the relative path. Callers must not
// pass the returned DatastoreConfig back to UpdateAppConfig without restoring
// the original relative path, as doing so would make the config non-portable.
func (s *Service) DatastoreConfig() (types.DatastoreConfig, error) {
	const op errors.Op = "config.Service.DatastoreConfig"

	if !s.isInitialized.Load() {
		return types.DatastoreConfig{}, errors.New(op).Msg(errMsgNotInitialized)
	}

	cfg := s.appConfig().DatastoreConfig

	// For sqlite, resolve relative path against WorkingDir (desktop only)
	if cfg.Driver == types.SqliteDriverName && cfg.Path != "" && !filepath.IsAbs(cfg.Path) {
		cfg.Path = filepath.Join(s.WorkingDir, cfg.Path)
	}

	return cfg, nil
}

// LoggingConfig returns the logging configuration.
func (s *Service) LoggingConfig() (types.LoggingConfig, error) {
	const op errors.Op = "config.Service.LoggingConfig"

	if !s.isInitialized.Load() {
		return types.LoggingConfig{}, errors.New(op).Msg(errMsgNotInitialized)
	}

	return s.appConfig().LoggingConfig, nil
}

// ServerConfig returns the server configuration from the application configuration. It requires the service to be initialized.
func (s *Service) ServerConfig() (*types.ServerConfig, error) {
	const op errors.Op = "config.Service.ServerConfig"

	if !s.isInitialized.Load() {
		return nil, errors.New(op).Msg(errMsgNotInitialized)
	}

	return s.appConfig().ServerConfig, nil
}

// RequiredConfigs retrieves the required configurations for the application. Returns an error if the service is uninitialized.
func (s *Service) RequiredConfigs() (types.RequiredConfigs, error) {
	const op errors.Op = "config.Service.RequiredConfigs"

	if !s.isInitialized.Load() {
		return types.RequiredConfigs{}, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.appConfig().RequiredConfigs, nil
}

// RigConfigByID retrieves the RigConfig for the given rig ID from the service's AppConfig. Returns an error if unavailable.
func (s *Service) RigConfigByID(rigID int64) (types.RigConfig, error) {
	const op errors.Op = "config.Service.RigConfigByID"

	if !s.isInitialized.Load() {
		return types.RigConfig{}, errors.New(op).Msg(errMsgNotInitialized)
	}
	if rigID <= 0 {
		return types.RigConfig{}, errors.New(op).Errorf("rig ID must be positive, got: %d", rigID)
	}

	for _, rig := range s.appConfig().RigConfigs {
		if rig.ID == rigID {
			return rig, nil
		}
	}

	return types.RigConfig{}, errors.New(op).Errorf("rig not found for ID: %d", rigID)
}

// CatStateValues retrieves the CAT state values for the default rig configuration in the service's application configuration.
// Returns a map of state values organized by tags or an error if the service is uninitialized or fails to retrieve the configuration.
func (s *Service) CatStateValues() (types.StateValues, error) {
	const op errors.Op = "config.Service.CatStateValues"

	if !s.isInitialized.Load() {
		return nil, errors.New(op).Msg(errMsgNotInitialized)
	}

	cfg := s.appConfig()

	var rigConfig types.RigConfig
	found := false
	for _, rig := range cfg.RigConfigs {
		if rig.ID == cfg.RequiredConfigs.DefaultRigID {
			rigConfig = rig
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New(op).Errorf("rig not found for ID: %d", cfg.RequiredConfigs.DefaultRigID)
	}

	stateValues := make(types.StateValues)
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

// LoggingStationConfig retrieves the logging station configuration from the service's application configuration.
func (s *Service) LoggingStationConfig() (types.LoggingStation, error) {
	const op errors.Op = "config.Service.LoggingStationConfig"

	if !s.isInitialized.Load() {
		return types.LoggingStation{}, errors.New(op).Msg(errMsgNotInitialized)
	}

	return s.appConfig().LoggingStation, nil
}

// LookupServiceConfig fetches the configuration for a given service by its name from the loaded application settings.
func (s *Service) LookupServiceConfig(serviceName string) (types.LookupConfig, error) {
	const op errors.Op = "config.Service.LookupServiceConfig"

	if !s.isInitialized.Load() {
		return types.LookupConfig{}, errors.New(op).Msg(errMsgNotInitialized)
	}

	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return types.LookupConfig{}, errors.New(op).Msg("service name cannot be empty")
	}

	for _, cfg := range s.appConfig().LookupServiceConfigs {
		if cfg.Name == serviceName {
			return cfg, nil
		}
	}

	return types.LookupConfig{}, errors.New(op).Msgf("service config not found for: %s", serviceName)
}

// ForwarderConfig retrieves the forwarder configuration for the specified service name.
// Returns a ForwarderConfig object and nil error if found, otherwise returns an empty object and an appropriate error.
func (s *Service) ForwarderConfig(serviceName string) (types.ForwarderConfig, error) {
	const op errors.Op = "config.Service.ForwarderConfig"

	if !s.isInitialized.Load() {
		return types.ForwarderConfig{}, errors.New(op).Msg(errMsgNotInitialized)
	}

	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return types.ForwarderConfig{}, errors.New(op).Msg("service name cannot be empty")
	}

	for _, cfg := range s.appConfig().ForwardingConfigs {
		if cfg.Name == serviceName {
			return cfg, nil
		}
	}

	return types.ForwarderConfig{}, errors.New(op).Msgf("service config not found for: %s", serviceName)
}

// ForwarderConfigs retrieves the list of forwarder configurations from the application configuration.
func (s *Service) ForwarderConfigs() ([]types.ForwarderConfig, error) {
	const op errors.Op = "config.Service.ForwarderConfigs"

	if !s.isInitialized.Load() {
		return nil, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.appConfig().ForwardingConfigs, nil
}

// EmailConfig retrieves the email configuration from the application configuration. Returns an error if uninitialized.
func (s *Service) EmailConfig() (types.EmailConfig, error) {
	const op errors.Op = "config.Service.EmailConfig"

	if !s.isInitialized.Load() {
		return types.EmailConfig{}, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.appConfig().EmailConfig, nil
}

// OptionalConfigs retrieves optional configuration settings from the service if it has been properly initialized.
func (s *Service) OptionalConfigs() (types.OptionalConfigs, error) {
	const op errors.Op = "config.Service.OptionalConfigs"

	if !s.isInitialized.Load() {
		return types.OptionalConfigs{}, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.appConfig().OptionalConfigs, nil
}

// ListenerConfigs retrieves the listener configuration from the application configuration.
func (s *Service) ListenerConfigs() ([]types.ListenerConfig, error) {
	const op errors.Op = "config.Service.ListenerConfigs"

	if !s.isInitialized.Load() {
		return nil, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.appConfig().ListenerConfigs, nil
}

// AudioPlaybackConfig retrieves the audio playback configuration from the application configuration.
func (s *Service) AudioPlaybackConfig() (types.AudioPlaybackConfig, error) {
	const op errors.Op = "config.Service.AudioPlaybackConfig"

	if !s.isInitialized.Load() {
		return types.AudioPlaybackConfig{}, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.appConfig().AudioPlaybackConfig, nil
}

// FT8Config retrieves the FT8 digital mode configuration from the application configuration.
func (s *Service) FT8Config() (types.FT8Config, error) {
	const op errors.Op = "config.Service.FT8Config"

	if !s.isInitialized.Load() {
		return types.FT8Config{}, errors.New(op).Msg(errMsgNotInitialized)
	}
	return s.appConfig().FT8Config, nil
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

	if err = writeDataToFile(data, filepath.Join(s.WorkingDir, configFileName)); err != nil {
		return err
	}

	s.mu.Lock()
	s.AppConfig = cfg
	s.mu.Unlock()
	return nil
}
