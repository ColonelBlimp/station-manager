package sqlite

import (
	"context"

	"github.com/Station-Manager/enums/upload"
	"github.com/Station-Manager/enums/upload/action"
	"github.com/Station-Manager/enums/upload/status"
	"github.com/Station-Manager/types"
)

/**********************************************************************************************************************
 * QSO Methods
 **********************************************************************************************************************/

func (s *Service) InsertQso(qso types.Qso) (int64, error) {
	return s.InsertQsoWithContext(context.Background(), qso)
}

func (s *Service) UpdateQso(qso types.Qso) error {
	return s.UpdateQsoWithContext(context.Background(), qso)
}

func (s *Service) FetchQsoSliceBySessionID(id int64) (types.QsoSlice, error) {
	return s.FetchQsoSliceBySessionIDWithContext(context.Background(), id)
}

func (s *Service) FetchQsoSliceByCallsign(callsign string) ([]types.ContactHistory, error) {
	return s.FetchQsoSliceByCallsignWithContext(context.Background(), callsign)
}

func (s *Service) FetchQsoSliceByLogbookId(id int64) (types.QsoSlice, error) {
	return s.FetchQsoSliceByLogbookIdWithContext(context.Background(), id)
}

func (s *Service) FetchQsoCountByLogbookId(id int64) (int64, error) {
	return s.FetchQsoCountByLogbookIdWithContext(context.Background(), id)
}

func (s *Service) InsertQsoUpload(id int64, action action.Action, service upload.OnlineService) error {
	return s.InsertQsoUploadWithContext(context.Background(), id, action, service)
}

func (s *Service) FetchQsoById(id int64) (types.Qso, error) {
	return s.FetchQsoByIdWithContext(context.Background(), id)
}

func (s *Service) FetchQsoSlicePaging(logbookId, pageNum, pageSize int64, ordering Ordering) (types.QsoSlice, error) {
	return s.FetchQsoSlicePagingWithContext(context.Background(), logbookId, pageNum, pageSize, ordering)
}

/**********************************************************************************************************************
 * ContactedStation Methods
 **********************************************************************************************************************/

func (s *Service) FetchContactedStationByCallsign(callsign string) (types.ContactedStation, error) {
	return s.FetchContactedStationByCallsignWithContext(context.Background(), callsign)
}

func (s *Service) InsertContactedStation(station types.ContactedStation) (int64, error) {
	return s.InsertContactedStationWithContext(context.Background(), station)
}

func (s *Service) UpdateContactedStation(station types.ContactedStation) error {
	return s.UpdateContactedStationWithContext(context.Background(), station)
}

/**********************************************************************************************************************
 * Country Methods
 **********************************************************************************************************************/

func (s *Service) FetchCountryByCallsign(callsign string) (types.Country, error) {
	return s.FetchCountryByCallsignWithContext(context.Background(), callsign)
}

func (s *Service) FetchCountryByName(name string) (types.Country, error) {
	return s.FetchCountryByNameWithContext(context.Background(), name)
}

func (s *Service) InsertCountry(country types.Country) (int64, error) {
	return s.InsertCountryWithContext(context.Background(), country)
}

func (s *Service) UpdateCountry(country types.Country) error {
	return s.UpdateCountryWithContext(context.Background(), country)
}

/**********************************************************************************************************************
 * Logbook Methods
 **********************************************************************************************************************/

func (s *Service) FetchLogbookByID(id int64) (types.Logbook, error) {
	return s.FetchLogbookByIDWithContext(context.Background(), id)
}

func (s *Service) FetchAllLogbooks() ([]types.Logbook, error) {
	return s.FetchAllLogbooksWithContext(context.Background())
}

func (s *Service) InsertLogbook(logbook types.Logbook) (int64, error) {
	return s.InsertLogbookWithContext(context.Background(), logbook)
}

func (s *Service) DeleteLogbookByID(id int64) error {
	return s.DeleteLogbookByIDWithContext(context.Background(), id)
}

func (s *Service) CheckDefaultLogbookExists() (bool, error) {
	return s.CheckDefaultLogbookExistsWithContext(context.Background())
}

func (s *Service) UpsertLogbook(logbook types.Logbook) error {
	return s.UpsertLogbookWithContext(context.Background(), logbook)
}

/**********************************************************************************************************************
 * Session Methods
 **********************************************************************************************************************/

func (s *Service) SoftDeleteSessionByID(id int64) error {
	return s.SoftDeleteSessionByIDWithContext(context.Background(), id)
}

func (s *Service) GenerateSession() (int64, error) {
	return s.GenerateSessionWithContext(context.Background())
}

/**********************************************************************************************************************
 * Contest Related Methods
 **********************************************************************************************************************/

func (s *Service) IsContestDuplicateByLogbookID(id int64, callsign, band string) (bool, error) {
	return s.IsContestDuplicateByLogbookIDWithContext(context.Background(), id, callsign, band)
}

/**********************************************************************************************************************
 * Upload Methods
 **********************************************************************************************************************/

func (s *Service) FetchPendingUploads() ([]types.QsoUpload, error) {
	return s.FetchPendingUploadsWithContext(context.Background())
}

func (s *Service) UpdateQsoUploadStatus(id int64, status status.Status, action action.Action, attempts int64, lastError string) error {
	return s.UpdateQsoUploadStatusWithContext(context.Background(), id, status, action, attempts, lastError)
}
