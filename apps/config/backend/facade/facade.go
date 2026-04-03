package facade

import (
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func (s *Service) GetConfig() (*types.AppConfig, error) {
	const op errors.Op = "facade.Service.GetConfig"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, err
	}

	return &s.ConfigService.AppConfig, nil
}

func (s *Service) UpdateConfig() error {
	const op errors.Op = "facade.Service.UpdateConfig"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return err
	}

	return nil
}
