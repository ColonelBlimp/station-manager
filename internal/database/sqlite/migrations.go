package sqlite

import (
	"database/sql"
	"embed"
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/log/*.sql migrations/reference/*.sql
var migrationFiles embed.FS

// Migration sets identify the two schema domains (ADR: reference.db / log-db
// split). Each set lives in its own migrations/<set>/ subdir, is versioned
// independently (each starts at 0001), and is tracked in its own
// schema_migrations_<set> table — so the log and reference schemas can be
// applied to separate physical files without their version histories
// colliding. A single connection can run both sets (the pre-split shape, and
// the shape every test uses); the file-split wiring runs one set per
// connection.
const (
	MigrationSetLog       = "log"
	MigrationSetReference = "reference"
)

// coreTablesBySet lists the tables each set must produce. Used for the
// post-migration schema verification — a "version current" but partially
// damaged schema (a missing table) must fail startup, not surface later as a
// runtime query error.
var coreTablesBySet = map[string][]string{
	MigrationSetLog:       {"logbook", "qso", "qso_upload", "qso_history", "operator_event"},
	MigrationSetReference: {"contacted_station", "country"},
}

// allMigrationSets is the order sets run when a Service runs every domain
// (the default — one connection holding all tables).
var allMigrationSets = []string{MigrationSetLog, MigrationSetReference}

// GetMigrationDrivers builds the source + database drivers for one migration
// set. The database driver is pinned to that set's dedicated tracking table
// so the two sets' version histories stay independent on a shared connection.
func GetMigrationDrivers(handle *sql.DB, set string) (source.Driver, database.Driver, error) {
	const op errors.Op = "sqlite.GetMigrationDrivers"
	srcDriver, err := iofs.New(migrationFiles, "migrations/"+set)
	if err != nil {
		return nil, nil, errors.New(op).WithErr(err).WithMsgf("iofs.New (set %q)", set)
	}
	dbDriver, err := migratesqlite.WithInstance(handle, &migratesqlite.Config{
		MigrationsTable: schemaMigrationsTable(set),
	})
	if err != nil {
		_ = srcDriver.Close()
		return nil, nil, errors.New(op).WithErr(err).WithMsg("migratesqlite.WithInstance")
	}
	return srcDriver, dbDriver, nil
}

// schemaMigrationsTable is the golang-migrate tracking table for a set.
func schemaMigrationsTable(set string) string { return "schema_migrations_" + set }

// resolvedMigrationSets returns the sets this Service runs. An empty
// migrationSets means "all domains in one connection" (the default, used by
// the single-DB daemon and every test); the file-split wiring sets it to a
// single domain per connection via SetMigrationSets.
func (s *Service) resolvedMigrationSets() []string {
	if len(s.migrationSets) == 0 {
		return allMigrationSets
	}
	return s.migrationSets
}

func (s *Service) doMigrations() error {
	const op errors.Op = "sqlite.Service.doMigrations"

	if s.DatabaseConfig.Driver != SqliteDriver {
		return errors.New(op).WithMsgf("Unsupported database driver: %s (expected %q)", s.DatabaseConfig.Driver, SqliteDriver)
	}

	for _, set := range s.resolvedMigrationSets() {
		if err := s.runMigrationSet(set); err != nil {
			return errors.New(op).WithErr(err).WithMsgf("migration set %q", set)
		}
	}

	missing, chkErr := s.missingCoreTables()
	if chkErr != nil {
		if s.LoggerService != nil {
			s.LoggerService.ErrorWith().Err(chkErr).Msg("schema verification failed")
		}
		return errors.New(op).WithErr(chkErr).WithMsg("schema verification failed")
	}
	if len(missing) != 0 {
		return errors.New(op).WithMsgf("schema missing after migrations: %v", missing)
	}
	if s.LoggerService != nil {
		s.LoggerService.InfoWith().Msg("schema verified")
	}
	return nil
}

// runMigrationSet applies one set's pending migrations up to head.
func (s *Service) runMigrationSet(set string) error {
	const op errors.Op = "sqlite.Service.runMigrationSet"

	srcDriver, dbDriver, err := GetMigrationDrivers(s.handle, set)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	defer func() { _ = srcDriver.Close() }()

	m, err := migrate.NewWithInstance("iofs", srcDriver, s.DatabaseConfig.Driver, dbDriver)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("migrate.NewWithInstance")
	}

	if s.LoggerService != nil {
		s.LoggerService.InfoWith().Str("set", set).Str("driver", s.DatabaseConfig.Driver).Msg("starting migrations")
	}

	upErr := m.Up()
	if upErr != nil && !stderr.Is(upErr, migrate.ErrNoChange) {
		if s.LoggerService != nil {
			s.LoggerService.ErrorWith().Str("set", set).Err(upErr).Msg("m.Up failed")
		}
		return errors.New(op).WithErr(upErr).WithMsg("m.Up")
	}
	if s.LoggerService != nil {
		s.LoggerService.InfoWith().Str("set", set).Msg("m.Up completed or no change")
	}
	return nil
}
