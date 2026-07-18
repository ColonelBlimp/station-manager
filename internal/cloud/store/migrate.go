package store

import (
	"context"
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
	// Build the driver on an explicitly acquired connection, not the *sql.DB:
	// WithInstance checks a connection out of the pool for the PROCESS
	// lifetime (never rotated, silently shrinking a bounded pool by one).
	// Closing the migrator after Up returns this connection to the pool.
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("acquire migration connection")
	}
	drv, err := pgmigrate.WithConnection(ctx, conn, &pgmigrate.Config{})
	if err != nil {
		_ = conn.Close()
		return errors.New(op).WithErr(err).WithMsg("postgres migrate driver")
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", drv)
	if err != nil {
		_ = drv.Close() // releases conn back to the pool
		return errors.New(op).WithErr(err).WithMsg("build migrator")
	}
	upErr := m.Up()
	srcErr, dbErr := m.Close() // driver Close releases conn back to the pool
	if upErr != nil && !stderr.Is(upErr, migrate.ErrNoChange) {
		return errors.New(op).WithErr(upErr).WithMsg("apply migrations")
	}
	if srcErr != nil {
		return errors.New(op).WithErr(srcErr).WithMsg("close migration source")
	}
	if dbErr != nil {
		return errors.New(op).WithErr(dbErr).WithMsg("release migration connection")
	}
	return nil
}
