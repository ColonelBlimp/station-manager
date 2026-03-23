package facade

import "github.com/Station-Manager/errors"

// IsContestDuplicate checks if a contest entry with the given callsign and band already exists in the current logbook.
func (s *Service) IsContestDuplicate(callsign, band string) (bool, error) {
	const op errors.Op = "facade.Service.IsContestDuplicate"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return false, err
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return false, err
	}

	s.LoggerService.DebugWith().Str("callsign", callsign).Str("band", band).Msg("Checking for contest duplicates")

	exists, err := s.DatabaseService.IsContestDuplicateByLogbookID(s.CurrentLogbook.ID, callsign, band)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to check for contest duplicates")
		return false, errors.Root(err)
	}

	s.LoggerService.DebugWith().Bool("exists", exists).Msg("Contest duplicate check complete")

	return exists, nil
}

// TotalQsosByLogbookId retrieves the total number of QSOs for the specified logbook ID.
// Returns the count of QSOs and an error if the service is not initialized, started, or the logbook ID is invalid.
func (s *Service) TotalQsosByLogbookId(logbookId int64) (int64, error) {
	const op errors.Op = "facade.Service.TotalQsosByLogbookId"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return 0, err
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return 0, errors.Root(err)
	}

	if logbookId < 1 {
		err := errors.New(op).Msg("Invalid logbook id")
		s.LoggerService.ErrorWith().Err(err).Msg("Invalid logbook id")
		return 0, errors.Root(err)
	}

	count, err := s.DatabaseService.FetchQsoCountByLogbookId(logbookId)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to get QSO count by logbook ID")
		return 0, errors.Root(err)
	}

	return count, nil
}
