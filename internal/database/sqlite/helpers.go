package sqlite

import "github.com/Station-Manager/errors"

// checkService checks if the database service is not nil, has been initialized and is open.
func checkService(op errors.Op, s *Service) error {
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	if !s.isInitialized.Load() {
		return errors.New(op).Msg(errMsgNotInitialized)
	}

	return nil
}
