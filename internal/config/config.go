package config

import (
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ServiceName is the DI bean ID for the config service (used by internal/iocdi).
const ServiceName = types.ConfigServiceName

// Config is the daemon's on-disk configuration. The zero value is not usable;
// construct via New() or with explicit field values.
//
// TODO(milestone 1): add a loader that reads $SM_WORKING_DIR/config.json
// (or the equivalent) and returns a validated *Config. For the initial
// restructure commit this package exists as a minimal stub — it compiles
// with zero-valued defaults so the daemon and carry-forward services
// (internal/database/sqlite, internal/logging) have somewhere to pull their
// configuration shape from without dragging in v1's grab-bag AppConfig.
type Config struct {
	// DataDir is the root directory for on-disk state: sqlite database,
	// log files, any future cache directories. Resolved via
	// utils.WorkingDir() at daemon startup.
	DataDir string `json:"data_dir"`

	// SocketPath is the absolute path to the Unix domain socket the daemon
	// binds its HTTP API to. Clients connect here to submit QSOs and query
	// state.
	SocketPath string `json:"socket_path"`

	// Datastore is the sqlite datastore configuration. Reuses
	// types.DatastoreConfig so the carry-forward sqlite service can consume
	// it without adapter glue.
	Datastore types.DatastoreConfig `json:"datastore"`

	// Logging is the zerolog + lumberjack configuration used by
	// internal/logging. Reused from types for the same reason as Datastore.
	Logging types.LoggingConfig `json:"logging"`

	// Required holds the small set of runtime tunables the kept sqlite
	// service still reads at Open time. Pruned from v1's RequiredConfigs
	// to the single field that is actually referenced.
	Required types.RequiredConfigs `json:"required"`
}

// Service is the runtime wrapper around Config that other services obtain
// via the iocdi container. It exposes the getter methods the kept v1 code
// calls (LoggingConfig, DatastoreConfig, RequiredConfigs, WorkingDir).
//
// The service follows the project's lifecycle convention:
// Initialize() -> Open()/Start() -> Close()/Stop(), idempotent.
// For milestone 1 only Initialize() does real work; Open/Start/Close are
// noop placeholders until a file loader lands.
type Service struct {
	// workingDir is the resolved on-disk data directory. Populated by
	// Initialize() from Config.DataDir or utils.WorkingDir() fallback.
	workingDir string

	// Cfg is the configuration this service hands out via its getter
	// methods. For the initial stub it can be set directly; once a
	// loader exists, Initialize() will populate it from a config file.
	Cfg Config

	initialized atomic.Bool
}

// New constructs a Service with zero-value Config. Callers that need a
// populated config should set Cfg fields directly or wait for the file
// loader to land.
func New() (*Service, error) {
	return &Service{}, nil
}

// Initialize prepares the service for use. Idempotent. For milestone 1
// this simply records that Initialize has been called; once a config file
// loader lands, this is where the file is read and validated.
func (s *Service) Initialize() error {
	s.initialized.Store(true)
	return nil
}

// Close is a no-op for the stub. Exists to satisfy the project's service
// lifecycle convention so the iocdi container can manage this service
// alongside others.
func (s *Service) Close() error {
	s.initialized.Store(false)
	return nil
}

// WorkingDir returns the resolved data directory for the runtime state.
// Returns the empty string if Initialize has not been called or the
// config has not been populated.
func (s *Service) WorkingDir() string {
	if s.workingDir != "" {
		return s.workingDir
	}
	return s.Cfg.DataDir
}

// LoggingConfig returns the logging configuration. The stub returns the
// Cfg.Logging field as-is with no validation; real validation moves here
// when the file loader lands.
func (s *Service) LoggingConfig() (types.LoggingConfig, error) {
	return s.Cfg.Logging, nil
}

// DatastoreConfig returns the sqlite datastore configuration. Consumed by
// internal/database/sqlite at Open time.
func (s *Service) DatastoreConfig() (types.DatastoreConfig, error) {
	return s.Cfg.Datastore, nil
}

// RequiredConfigs returns the pruned v1 "required" tunables. Only
// QsoForwardingRowLimit is referenced by the carry-forward sqlite code;
// see internal/types/required.go for the background.
func (s *Service) RequiredConfigs() (types.RequiredConfigs, error) {
	return s.Cfg.Required, nil
}
