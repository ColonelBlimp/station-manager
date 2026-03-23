package qrz

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
)

// UpdateDatabase updates the local database with the provided QSO record.
// Returns an error if the database update process fails.
// We update the database here rather than at the caller because different services have different requirements
// concerning the fields and values that are stored in the database.
//
// However, the qso_upload table is updated by the facade service worker.
// This method is now public to support serialized database writes through the facade's DB worker.
func (s *Service) UpdateDatabase(qso types.Qso) error {
	const op errors.Op = "forwarder.qrz.UpdateDatabase"
	if qso.ID < 1 {
		return errors.New(op).Msgf("invalid QSO ID, unable to update: %d", qso.ID)
	}

	qso.QrzComUploadStatus = "Y"
	qso.QrzComUploadDate = utils.DateNowAsYYYYMMDD()

	if err := s.DatabaseService.UpdateQso(qso); err != nil {
		return errors.New(op).Err(err).Msg("updating database")
	}

	return nil
}
