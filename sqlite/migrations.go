package sqlite

import (
	"database/sql"
	"embed"
	stderr "errors"

	"github.com/Station-Manager/errors"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func GetMigrationDrivers(handle *sql.DB) (source.Driver, database.Driver, error) {
	const op errors.Op = "sqlite.GetMigrationDrivers"
	srcDriver, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return nil, nil, errors.New(op).Errorf("iofs.New: %w", err)
	}
	dbDriver, err := sqlite3.WithInstance(handle, &sqlite3.Config{})
	if err != nil {
		_ = srcDriver.Close()
		return nil, nil, errors.New(op).Errorf("sqlite3.WithInstance: %w", err)
	}
	return srcDriver, dbDriver, nil
}

func (s *Service) doMigrations() error {
	const op errors.Op = "sqlite.Service.doMigrations"

	if s.DatabaseConfig.Driver != SqliteDriver {
		return errors.New(op).Errorf("Unsupported database driver: %s (expected %q)", s.DatabaseConfig.Driver, SqliteDriver)
	}

	srcDriver, dbDriver, err := GetMigrationDrivers(s.handle)
	if err != nil {
		return errors.New(op).Err(err)
	}
	defer func() { _ = srcDriver.Close() }()

	m, err := migrate.NewWithInstance("iofs", srcDriver, s.DatabaseConfig.Driver, dbDriver)
	if err != nil {
		return errors.New(op).Errorf("migrate.NewWithInstance: %w", err)
	}

	if s.LoggerService != nil {
		s.LoggerService.InfoWith().Str("driver", s.DatabaseConfig.Driver).Msg("starting migrations")
	}

	upErr := m.Up()
	if upErr != nil && !stderr.Is(upErr, migrate.ErrNoChange) {
		if s.LoggerService != nil {
			s.LoggerService.ErrorWith().Err(upErr).Msg("m.Up failed")
		}
		return errors.New(op).Errorf("m.Up: %w", upErr)
	}
	if s.LoggerService != nil {
		s.LoggerService.InfoWith().Msg("m.Up completed or no change")
	}

	missing, chkErr := s.missingCoreTables()
	if chkErr != nil {
		if s.LoggerService != nil {
			s.LoggerService.ErrorWith().Err(chkErr).Msg("schema verification failed")
		}
		return errors.New(op).Err(chkErr).Msg("schema verification failed")
	}
	if len(missing) == 0 {
		if s.LoggerService != nil {
			s.LoggerService.InfoWith().Msg("schema verified")
		}
		return nil
	}

	return errors.New(op).Errorf("schema missing after migrations: %v", missing)
}
