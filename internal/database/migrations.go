package database

import (
	stderr "errors"
	"github.com/Station-Manager/database/postgres"
	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/errors"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/source"
)

func (s *Service) doMigrations() error {
	const op errors.Op = "database.Service.doMigrations"
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	var srcDriver source.Driver
	var dbDriver database.Driver
	var err error

	switch s.DatabaseConfig.Driver {
	case PostgresDriver:
		srcDriver, dbDriver, err = postgres.GetMigrationDrivers(s.handle)
	case SqliteDriver:
		srcDriver, dbDriver, err = sqlite.GetMigrationDrivers(s.handle)
	default:
		return errors.New(op).Msg("Driver not supported.")
	}
	if err != nil {
		return errors.New(op).Err(err)
	}

	m, err := migrate.NewWithInstance("iofs", srcDriver, s.DatabaseConfig.Driver, dbDriver)
	if err != nil {
		_ = srcDriver.Close()
		return errors.New(op).Errorf("migrate.NewWithInstance: %w", err)
	}
	defer func() { _ = srcDriver.Close() }()

	if s.Logger != nil {
		s.Logger.InfoWith().Str("driver", s.DatabaseConfig.Driver).Msg("starting migrations")
	}

	upErr := m.Up()
	if upErr != nil && !stderr.Is(upErr, migrate.ErrNoChange) {
		if s.Logger != nil {
			s.Logger.ErrorWith().Err(upErr).Msg("m.Up failed")
		}
		return errors.New(op).Errorf("m.Up: %w", upErr)
	}
	if s.Logger != nil {
		s.Logger.InfoWith().Msg("m.Up completed or no change")
	}

	missing, chkErr := s.missingCoreTables()
	if chkErr != nil {
		if s.Logger != nil {
			s.Logger.ErrorWith().Err(chkErr).Msg("schema verification failed")
		}
		return errors.New(op).Err(chkErr).Msg("schema verification failed")
	}
	if len(missing) == 0 {
		if s.Logger != nil {
			s.Logger.InfoWith().Msg("schema verified")
		}
		return nil
	}

	// Fallback: apply the initial schema directly if core tables are missing.
	if s.DatabaseConfig.Driver == PostgresDriver {
		if s.Logger != nil {
			s.Logger.WarnWith().Strs("missing", missing).Msg("applying initial schema via fallback")
		}
		if err := postgres.ApplyInitialSchemaSimple(s.handle); err != nil {
			return errors.New(op).Err(err).Msg("fallback initial schema failed")
		}
		missing, chkErr = s.missingCoreTables()
		if chkErr != nil {
			return errors.New(op).Err(chkErr).Msg("schema verification failed (post-fallback)")
		}
		if len(missing) > 0 {
			return errors.New(op).Errorf("schema still missing after fallback: %v", missing)
		}
		if s.Logger != nil {
			s.Logger.InfoWith().Msg("schema created via fallback")
		}
		return nil
	}

	return errors.New(op).Errorf("schema missing after migrations and no fallback for driver %s: %v", s.DatabaseConfig.Driver, missing)
}
