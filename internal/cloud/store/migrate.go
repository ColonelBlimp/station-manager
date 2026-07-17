package store

import (
	"database/sql"
	"embed"
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	migrate "github.com/golang-migrate/migrate/v4"
	pgmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies the embedded smcloud migrations to db, bringing the schema
// to the latest version — the runtime applier the store doc promised for
// cmd/smcloud. It uses golang-migrate with the SAME source files and the same
// schema_migrations tracking table as the dev-workflow CLI (`task
// migrate:cloud:up`), so a database migrated one way is a no-op the other.
// Idempotent: an up-to-date schema returns nil.
func Migrate(db *sql.DB) error {
	const op errors.Op = "store.Migrate"
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("open embedded migrations")
	}
	drv, err := pgmigrate.WithInstance(db, &pgmigrate.Config{})
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("postgres migrate driver")
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", drv)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("build migrator")
	}
	if err := m.Up(); err != nil && !stderr.Is(err, migrate.ErrNoChange) {
		return errors.New(op).WithErr(err).WithMsg("apply migrations")
	}
	return nil
}
