package sqlite

import "github.com/ColonelBlimp/station-manager/internal/errors"

// checkService checks if the database service is not nil, has been initialized and is open.
func checkService(op errors.Op, s *Service) error {
	if s == nil {
		return errors.New(op).WithMsg(errMsgNilService)
	}

	if !s.isInitialized.Load() {
		return errors.New(op).WithMsg(errMsgNotInitialized)
	}

	return nil
}
