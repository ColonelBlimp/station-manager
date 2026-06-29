package sqlite

import (
	"database/sql"
	stderr "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	migrate "github.com/golang-migrate/migrate/v4"
)

// BootstrapReferenceSplit performs the ONE-TIME migration of an existing
// single-file database into the reference.db / log-db split: the enrichment
// caches (country + contacted_station) move out of the log DB into reference.db.
//
// It is idempotent and backup-first:
//   - No-op when logPath is empty/":memory:" (fresh or test), when the log DB
//     file doesn't exist yet (fresh install — the two beans create the split
//     cleanly), or when the log DB no longer carries the cache tables (already
//     split).
//   - Otherwise: VACUUM INTO a timestamped backup, create + populate
//     reference.db, drop the cache tables from the log DB, and rename the
//     combined `schema_migrations` tracking table to `schema_migrations_log` so
//     the log connection's subsequent migrate run is a no-op rather than
//     re-running the 0002 table rebuild on live data.
//
// Must run BEFORE the log/reference connections open. A crash mid-split is safe
// to resume: the copy is INSERT OR IGNORE, the drops are IF EXISTS, and the
// rename is guarded — a second run that still sees the cache tables completes
// the remaining steps.
func BootstrapReferenceSplit(logPath, refPath, backupDir string, log *logging.Service) error {
	const op errors.Op = "sqlite.BootstrapReferenceSplit"

	if logPath == emptyString || logPath == ":memory:" {
		return nil
	}
	if _, err := os.Stat(logPath); err != nil {
		if os.IsNotExist(err) {
			return nil // fresh install — nothing to split
		}
		return errors.New(op).WithErr(err).WithMsg("stat log database")
	}

	logDB, err := sql.Open(SqliteDriver, bootstrapDSN(logPath))
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("open log database")
	}
	defer func() { _ = logDB.Close() }()

	// Detection: the cache tables only live in the log DB before the split.
	hasCache, err := hasTable(logDB, "country")
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("detect cache tables")
	}
	if !hasCache {
		return nil // already split (or a fresh log-set-only DB)
	}

	logInfo(log, "bootstrap: existing single-file database detected; splitting enrichment caches into reference.db")

	// 1. Backup the whole log DB first — VACUUM INTO yields a consistent
	//    single-file copy regardless of WAL state. The timestamped name never
	//    pre-exists (VACUUM INTO requires the target not exist).
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return errors.New(op).WithErr(err).WithMsg("create backup directory")
	}
	base := strings.TrimSuffix(filepath.Base(logPath), filepath.Ext(logPath))
	ts := time.Now().UTC().Format("20060102-150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s-presplit-%s.db", base, ts))
	if _, err := logDB.Exec("VACUUM INTO '" + strings.ReplaceAll(backupPath, "'", "''") + "'"); err != nil {
		return errors.New(op).WithErr(err).WithMsg("backup log database (VACUUM INTO)")
	}
	logInfo(log, "bootstrap: backed up to "+backupPath)

	// 2. Create reference.db with the reference schema (also stamps
	//    schema_migrations_reference at head).
	refDB, err := sql.Open(SqliteDriver, bootstrapDSN(refPath))
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("open reference database")
	}
	if err := applyMigrationSetTo(refDB, MigrationSetReference); err != nil {
		_ = refDB.Close()
		return errors.New(op).WithErr(err).WithMsg("migrate reference database")
	}
	if err := refDB.Close(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("close reference database after migrate")
	}

	// 3. Copy the cache rows across (ATTACH the freshly-created reference.db to
	//    the log connection). OR IGNORE makes a resumed run a no-op on rows
	//    already copied.
	if _, err := logDB.Exec("ATTACH DATABASE '" + strings.ReplaceAll(refPath, "'", "''") + "' AS refdb"); err != nil {
		return errors.New(op).WithErr(err).WithMsg("attach reference database")
	}
	copyOK := false
	defer func() {
		// Always detach, even on the error paths below.
		_, _ = logDB.Exec("DETACH DATABASE refdb")
		_ = copyOK
	}()
	for _, tbl := range []string{"country", "contacted_station"} {
		if _, err := logDB.Exec("INSERT OR IGNORE INTO refdb." + tbl + " SELECT * FROM main." + tbl); err != nil {
			return errors.New(op).WithErr(err).WithMsgf("copy %s into reference.db", tbl)
		}
	}
	copyOK = true
	if _, err := logDB.Exec("DETACH DATABASE refdb"); err != nil {
		return errors.New(op).WithErr(err).WithMsg("detach reference database")
	}

	// 4. Drop the cache tables (+ their indexes) from the log DB. No inbound
	//    FKs reference them (qso.country is a denormalized text column), so the
	//    drop is safe regardless of foreign_keys state.
	for _, stmt := range []string{
		"DROP INDEX IF EXISTS idx_country_name",
		"DROP INDEX IF EXISTS uq_contacted_station_active_call",
		"DROP TABLE IF EXISTS country",
		"DROP TABLE IF EXISTS contacted_station",
	} {
		if _, err := logDB.Exec(stmt); err != nil {
			return errors.New(op).WithErr(err).WithMsgf("drop cache schema (%s)", stmt)
		}
	}

	// 5. Re-key the migration tracking: the old combined lineage (0001 + 0002)
	//    is version-identical to the log set's lineage, so renaming the table
	//    hands the log connection a "already at head" view — no destructive
	//    0002 re-run. Guarded so a resumed run (where the rename already
	//    happened) doesn't fail.
	oldTracking, err := hasTable(logDB, "schema_migrations")
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("detect combined tracking table")
	}
	newTracking, err := hasTable(logDB, schemaMigrationsTable(MigrationSetLog))
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("detect log tracking table")
	}
	if oldTracking && !newTracking {
		if _, err := logDB.Exec("ALTER TABLE schema_migrations RENAME TO " + schemaMigrationsTable(MigrationSetLog)); err != nil {
			return errors.New(op).WithErr(err).WithMsg("rename combined tracking table")
		}
	}

	logInfo(log, "bootstrap: split complete — enrichment caches now in reference.db")
	return nil
}

// bootstrapDSN builds a modernc DSN for a transient bootstrap connection with
// the same load-bearing PRAGMAs the Service uses (busy_timeout, WAL,
// foreign_keys, synchronous). Kept separate from Service.getDsn because the
// bootstrap runs before any Service is open and takes a bare path.
func bootstrapDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=synchronous(normal)",
		path, busyTimeoutMS,
	)
}

// applyMigrationSetTo runs one migration set up to head on a raw handle.
func applyMigrationSetTo(handle *sql.DB, set string) error {
	const op errors.Op = "sqlite.applyMigrationSetTo"
	src, drv, err := GetMigrationDrivers(handle, set)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	defer func() { _ = src.Close() }()
	m, err := migrate.NewWithInstance("iofs", src, SqliteDriver, drv)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("migrate.NewWithInstance")
	}
	if err := m.Up(); err != nil && !stderr.Is(err, migrate.ErrNoChange) {
		return errors.New(op).WithErr(err).WithMsg("m.Up")
	}
	return nil
}

// hasTable reports whether a table exists on the given handle.
func hasTable(db *sql.DB, name string) (bool, error) {
	var got string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&got)
	if err == nil {
		return true, nil
	}
	if stderr.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func logInfo(log *logging.Service, msg string) {
	if log != nil {
		log.InfoWith().Msg(msg)
	}
}
