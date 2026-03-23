package postgres

import (
	"context"
	"database/sql"
	"embed"
	"github.com/Station-Manager/errors"
	"github.com/golang-migrate/migrate/v4/database"
	pg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func GetMigrationDrivers(handle *sql.DB) (source.Driver, database.Driver, error) {
	const op errors.Op = "database.postgres.sourceDriver"
	srcDriver, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return nil, nil, errors.New(op).Errorf("iofs.New: %w", err)
	}
	dbDriver, err := pg.WithInstance(handle, &pg.Config{})
	if err != nil {
		return nil, nil, errors.New(op).Errorf("pg.WithInstance: %w", err)
	}
	return srcDriver, dbDriver, nil
}

// ApplyInitialSchemaSimple is a fallback initializer that applies the initial schema
// without using golang-migrate. It is intended for development/debugging where the
// standard migrator may be blocked by environment constraints.
func ApplyInitialSchemaSimple(handle *sql.DB) error {
	const op errors.Op = "database.postgres.ApplyInitialSchemaSimple"
	if handle == nil {
		return errors.New(op).Msg("nil db handle")
	}
	data, err := migrationFiles.ReadFile("migrations/0001_schema_migrations.up.sql")
	if err != nil {
		return errors.New(op).Errorf("read initial schema: %w", err)
	}
	stmts := splitSQLStatements(string(data))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, s := range stmts {
		ss := strings.TrimSpace(s)
		if ss == "" {
			continue
		}
		if _, err := handle.ExecContext(ctx, ss); err != nil {
			return errors.New(op).Errorf("exec: %w (stmt: %s)", err, truncate(ss, 120))
		}
	}
	return nil
}

// BootstrapExec executes the initial up migration directly for first-time initialization
// when the schema is entirely missing. This avoids potential issues with external
// migration locking on brand new databases.
func BootstrapExec(handle *sql.DB) error {
	const op errors.Op = "database.postgres.BootstrapExec"
	data, err := migrationFiles.ReadFile("migrations/0001_schema_migrations.up.sql")
	if err != nil {
		return errors.New(op).Errorf("read initial migration: %w", err)
	}
	if _, err := handle.Exec(string(data)); err != nil {
		return errors.New(op).Errorf("exec initial migration: %w", err)
	}
	return nil
}

func splitSQLStatements(s string) []string {
	// naive splitter: split on semicolons; safe for our controlled scripts
	parts := strings.Split(s, ";")
	return parts
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
