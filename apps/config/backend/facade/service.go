// Package facade: config
package facade

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	LoggerService *logging.Service `di.inject:"loggingservice"`

	container *iocdi.Container
	ctx       context.Context

	initialized atomic.Bool
	started     atomic.Bool // guarded via atomic operations; Start/Stop also hold mu for a broader state

	initOnce sync.Once
	mu       sync.Mutex
}

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

		s.initialized.Store(true)
	})

	return initErr
}

func (s *Service) Start(ctx context.Context) error {
	return nil
}

func (s *Service) Stop() error {
	return nil
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
