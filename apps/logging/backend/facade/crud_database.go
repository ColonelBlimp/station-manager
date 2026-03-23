package facade

import (
	"context"
	stderr "errors"

	"github.com/Station-Manager/database/sqlite/adapters"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
	"github.com/aarondl/sqlboiler/v4/boil"
)

// openAndLoadFromDatabase initializes the database connection, applies migrations, loads the default
// logbook, and generates a session ID.
func (s *Service) openAndLoadFromDatabase() error {
	const op errors.Op = "facade.Service.openAndLoadFromDatabase"

	// Open and migrate the database. Don't need to ping as opening the database will do that.
	//if err := s.DatabaseService.Open(); err != nil {
	//	err = errors.New(op).Err(err)
	//	s.LoggerService.ErrorWith().Err(err).Msg("Failed to open database.")
	//	return err
	//}
	//if err := s.DatabaseService.Migrate(); err != nil {
	//	err = errors.New(op).Err(err)
	//	s.LoggerService.ErrorWith().Err(err).Msg("Failed to migrate database.")
	//	return err
	//}
	if err := s.openDatabase(); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Open database failed.")
		return err
	}

	// Load the default logbook
	logbook, err := s.DatabaseService.FetchLogbookByID(s.requiredCfgs.DefaultLogbookID)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch logbook.")
		return err
	}
	s.CurrentLogbook = logbook

	// Generate a new session id
	s.sessionID, err = s.DatabaseService.GenerateSession()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to generate new session ID.")
		return err
	}

	return nil
}

func (s *Service) openDatabase() error {
	const op errors.Op = "facade.Service.openDatabase"

	// Open and migrate the database. Don't need to ping as opening the database will do that.
	if err := s.DatabaseService.Open(); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to open database.")
		return err
	}
	if err := s.DatabaseService.Migrate(); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to migrate database.")
		return err
	}

	return nil
}

// insertOrUpdateContactedStation checks if a contacted station exists in the database and inserts or updates it accordingly.
// It fetches the station by callsign, inserts if not found, or updates it if differences are detected.
// Returns an error if database operations fail.
func (s *Service) insertOrUpdateContactedStation(station types.ContactedStation) error {
	const op errors.Op = "facade.Service.insertOrUpdateContactedStation"

	// Does a Contacted Station record exist for this callsign?
	model, err := s.DatabaseService.FetchContactedStationByCallsign(station.Call)
	if err != nil && !stderr.Is(err, errors.ErrNotFound) {
		return errors.New(op).Err(err)
	}

	if err != nil {
		if _, err = s.DatabaseService.InsertContactedStation(station); err != nil {
			s.LoggerService.ErrorWith().Err(err).Msg("Failed to insert contacted station into database.")
			return errors.Root(err)
		}
		return nil
	}

	// Do this before the comparison to ensure the ID is set, and the comparison doesn't fail because of it.
	station.CSID = model.CSID

	if model != station {
		s.LoggerService.DebugWith().Str("callsign", station.Call).Msg("Contacted station exists in database, but needs updating.")
		if err = s.DatabaseService.UpdateContactedStation(station); err != nil {
			s.LoggerService.ErrorWith().Err(err).Msg("Failed to update contacted station in database.")
			return errors.Root(err)
		}
	}

	return nil
}

// insertOrUpdateCountry inserts a new country or updates an existing country's details in the database.
func (s *Service) insertOrUpdateCountry(country types.Country) error {
	const op errors.Op = "facade.Service.insertOrUpdateCountry"

	//	model, err := s.DatabaseService.FetchCountryByName(country.Name)
	model, err := s.DatabaseService.FetchCountryByCallsign(country.Prefix)
	if err != nil && !stderr.Is(err, errors.ErrNotFound) {
		return errors.New(op).Err(err)
	}

	if stderr.Is(err, errors.ErrNotFound) || country.IsNewEntity {
		model.ID, err = s.DatabaseService.InsertCountry(country)
		if err != nil {
			return errors.New(op).Err(err)
		}
		return nil
	}

	// Currently, this is okay as the only real risk is pointer fields being added later
	if model != country {
		country.ID = model.ID
		err = s.DatabaseService.UpdateCountry(country)
		if err != nil {
			return errors.New(op).Err(err)
		}
	}

	return nil
}

// markQsoSliceAsForwardedByEmail marks a slice of QSOs as forwarded by email, updating their status and date in the database.
// Returns an error if the transaction fails or if a QSO model update is unsuccessful.
func (s *Service) markQsoSliceAsForwardedByEmail(slice []types.Qso) error {
	const op errors.Op = "facade.Service.markQsoSliceAsForwardedByEmail"

	if len(slice) == 0 {
		s.LoggerService.DebugWith().Msg("No QSOs to mark as forwarded by email.")
		return nil
	}

	// Use the service context if available, otherwise fall back to background
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	tx, txCancel, err := s.DatabaseService.BeginTxContext(ctx)
	if err != nil {
		return errors.New(op).Err(err)
	}
	defer txCancel()
	defer func() { _ = tx.Rollback() }() // No-op after successful commit

	for _, qso := range slice {
		// Check for context cancellation before each iteration
		if err = ctx.Err(); err != nil {
			return errors.New(op).Err(err).Msg("context cancelled during QSO update loop")
		}

		qso.SmFwrdByEmailStatus = "Y"
		qso.SmFwrdByEmailDate = utils.DateNowAsYYYYMMDD()

		model, qerr := adapters.QsoTypeToModel(qso)
		if qerr != nil {
			qerr = errors.New(op).Err(qerr)
			s.LoggerService.ErrorWith().Err(qerr).Msg("Failed to convert QSO type to model.")
			return qerr
		}

		if _, qerr = model.Update(ctx, tx, boil.Infer()); qerr != nil {
			qerr = errors.New(op).Err(qerr)
			s.LoggerService.ErrorWith().Err(qerr).Msg("Failed to update model")
			return qerr
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.New(op).Err(err)
	}

	s.LoggerService.DebugWith().Msg("Marked QSOs as forwarded by email.")

	return nil
}
