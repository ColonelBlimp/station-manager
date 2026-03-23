// Package facade: Logbook-App
package facade

import (
	"context"

	"github.com/Station-Manager/database/sqlite/adapters"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
	"github.com/aarondl/sqlboiler/v4/boil"
)

// openAndLoadFromDatabase opens the database, performs migrations, loads configurations, and sets the default logbook.
func (s *Service) openAndLoadFromDatabase() error {
	const op errors.Op = "facade.Service.loadFromDatabase"

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

	reqCfg, err := s.ConfigService.RequiredConfigs()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch required configs.")
		return err
	}

	// Load the default logbook
	logbook, err := s.DatabaseService.FetchLogbookByID(reqCfg.DefaultLogbookID)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch logbook.")
		return err
	}
	s.currentLogbook = logbook

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
