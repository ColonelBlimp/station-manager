// Package facade: logbook-app
package facade

import (
	"context"

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/email"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/iocdi"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	"github.com/go-playground/validator/v10"

	"sync"
	"sync/atomic"
)

const (
	ServiceName = "logbook-app-facade"
)

type Service struct {
	ConfigService   *config.Service  `di.inject:"configservice"`
	LoggerService   *logging.Service `di.inject:"loggingservice"`
	DatabaseService *sqlite.Service  `di.inject:"sqliteservice"`
	EmailService    *email.Service   `di.inject:"emailservice"`

	initialized atomic.Bool
	started     atomic.Bool // guarded via atomic operations; Start/Stop also hold mu for broader state

	initOnce sync.Once
	mu       sync.Mutex

	currentLogbook types.Logbook
	requiredCfgs   *types.RequiredConfigs

	container *iocdi.Container
	ctx       context.Context

	validate *validator.Validate
}

// Initialize sets up the Service by validating dependencies, loading required configurations, and preparing the database connection. It ensures that initialization only occurs once and returns any errors encountered during the process.
func (s *Service) Initialize() error {
	const op errors.Op = "facade.Service.Initialize"

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

		if s.DatabaseService == nil {
			initErr = errors.New(op).Msg(errMsgNilDatabaseService)
			return
		}

		if s.EmailService == nil {
			initErr = errors.New(op).Msg(errMsgNilEmailService)
			return
		}

		reqCfg, err := s.ConfigService.RequiredConfigs()
		if err != nil {
			initErr = errors.New(op).Err(err)
			return
		}
		s.requiredCfgs = &reqCfg

		if err = s.initializeValidation(); err != nil {
			initErr = errors.New(op).Err(err)
			return
		}

		s.initialized.Store(true)
	})

	return initErr
}

// SetContainer sets the IOC container for the Service. Returns an error if the Service is uninitialized or the container is nil.
func (s *Service) SetContainer(container *iocdi.Container) error {
	const op errors.Op = "facade.Service.SetContainer"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return err
	}

	if s.started.Load() {
		return nil
	}

	if container == nil {
		err := errors.New(op).Msg("Container cannot be nil")
		s.LoggerService.ErrorWith().Err(err).Msg("Container cannot be nil")
		return err
	}

	s.container = container

	return nil
}

// Start begins the Service lifecycle by initializing dependencies, opening the database, and marking it as started.
func (s *Service) Start(ctx context.Context) error {
	const op errors.Op = "facade.Service.Start"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return errors.Root(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Use CompareAndSwap to atomically check and set started flag
	if !s.started.CompareAndSwap(false, true) {
		// Service already started
		return nil
	}

	if s.container == nil {
		return errors.New(op).Msg("Container is nil. Please call SetContainer() before calling Start()")
	}

	if ctx == nil || ctx.Err() != nil {
		err := errors.New(op).Msg("Context cannot be nil or cancelled")
		s.LoggerService.ErrorWith().Msg("Context cannot be nil or cancelled")
		return errors.Root(err)
	}
	s.ctx = ctx

	// Start the database service
	if err := s.openAndLoadFromDatabase(); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to open and load from database.")
		return errors.Root(err)
	}

	return nil
}

// Stop closes the database connection and marks the Service as stopped.
func (s *Service) Stop() error {
	const op errors.Op = "facade.Service.Stop"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return err
	}

	// Collect non-fatal shutdown errors for reporting
	var shutdownErrors []error

	// Stop the database service
	if err := s.DatabaseService.Close(); err != nil {
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to close database")
		shutdownErrors = append(shutdownErrors, err)
	}

	// Return combined error if any shutdown steps failed
	if len(shutdownErrors) > 0 {
		return errors.New(op).Msgf("shutdown completed with %d error(s)", len(shutdownErrors))
	}

	return nil
}
