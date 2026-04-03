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
