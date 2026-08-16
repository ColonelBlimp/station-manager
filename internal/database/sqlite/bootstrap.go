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
//     cleanly), or when the log DB shows NO remaining split work (already
//     split).
//   - Otherwise: VACUUM INTO a timestamped backup, create + populate
//     reference.db, drop the cache tables from the log DB, and rename the
//     combined `schema_migrations` tracking table to `schema_migrations_log` so
//     the log connection's subsequent migrate run is a no-op rather than
//     re-running the 0002 table rebuild on live data.
//
// Must run BEFORE the log/reference connections open. A crash mid-split is safe
// to resume: the copy is INSERT OR IGNORE, the drops are IF EXISTS, and the
// rename is guarded — a second run picks up wherever the last one stopped.
// Resume correctness depends on splitState covering EVERY piece of state the
// cleanup mutates, not just one (2026-07-22 sqlite review, finding 5).
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

	state, err := inspectSplitState(logDB)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("detect split state")
	}
	if !state.pending() {
		return nil // already split (or a fresh log-set-only DB)
	}

	if state.hasCacheTables() {
		logInfo(log, "bootstrap: existing single-file database detected; splitting enrichment caches into reference.db")

		// 1. Backup the whole log DB first — VACUUM INTO yields a consistent
		//    single-file copy regardless of WAL state. The timestamped name never
		//    pre-exists (VACUUM INTO requires the target not exist).
		//
		//    Skipped when only the tracking rename is outstanding: no row moves
		//    or gets dropped in that state, and the interrupted first attempt
		//    already left a backup of the richer pre-split shape.
		// The backup directory + the backup database it holds contain full QSO records, so
		// they are owner-private (ST-6): 0700 dir, 0600 file. VACUUM INTO creates the file
		// per umask, so chmod it explicitly afterwards.
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return errors.New(op).WithErr(err).WithMsg("create backup directory")
		}
		base := strings.TrimSuffix(filepath.Base(logPath), filepath.Ext(logPath))
		ts := time.Now().UTC().Format("20060102-150405")
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s-presplit-%s.db", base, ts))
		if _, err := logDB.Exec("VACUUM INTO '" + strings.ReplaceAll(backupPath, "'", "''") + "'"); err != nil {
			return errors.New(op).WithErr(err).WithMsg("backup log database (VACUUM INTO)")
		}
		if err := os.Chmod(backupPath, 0o600); err != nil {
			return errors.New(op).WithErr(err).WithMsg("secure backup database (chmod 0600)")
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

		// 3. Copy the cache rows across (ATTACH the freshly-created reference.db
		//    to the log connection). OR IGNORE makes a resumed run a no-op on
		//    rows already copied. A table already dropped by an interrupted run
		//    is skipped — its rows were copied before the drop.
		if err := copyCachesInto(logDB, refPath, state); err != nil {
			return errors.New(op).WithErr(err)
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
	} else {
		logInfo(log, "bootstrap: resuming an interrupted split — re-keying the migration tracking table")
	}

	// 5. Re-key the migration tracking: the old combined lineage (0001 + 0002)
	//    is version-identical to the log set's lineage, so renaming the table
	//    hands the log connection a "already at head" view — no destructive
	//    0002 re-run. Guarded so a resumed run (where the rename already
	//    happened) doesn't fail.
	if state.trackingPending() {
		if _, err := logDB.Exec("ALTER TABLE schema_migrations RENAME TO " + schemaMigrationsTable(MigrationSetLog)); err != nil {
			return errors.New(op).WithErr(err).WithMsg("rename combined tracking table")
		}
	}

	logInfo(log, "bootstrap: split complete — enrichment caches now in reference.db")
	return nil
}

// splitState is the observed pre-split residue in the log DB. Every piece of
// state the split mutates is represented here, because the "is the split
// already done?" question must be answered from ALL of it. Keying that decision
// on `country` alone classified an interrupted cleanup as complete: a crash
// between "DROP TABLE country" and the tracking rename left contacted_station
// and the old combined lineage in place, and the next start returned early —
// leaving the log connection to re-run migrations 0001+ over live data
// (2026-07-22 sqlite review, finding 5).
type splitState struct {
	country     bool
	contacted   bool
	oldTracking bool // combined `schema_migrations` still present
	logTracking bool // `schema_migrations_log` already in place
}

func (s splitState) hasCacheTables() bool { return s.country || s.contacted }

// trackingPending reports whether the combined tracking table still has to be
// renamed to the log set's table.
func (s splitState) trackingPending() bool { return s.oldTracking && !s.logTracking }

func (s splitState) pending() bool { return s.hasCacheTables() || s.trackingPending() }

func inspectSplitState(db *sql.DB) (splitState, error) {
	var (
		st  splitState
		err error
	)
	if st.country, err = hasTable(db, "country"); err != nil {
		return st, err
	}
	if st.contacted, err = hasTable(db, "contacted_station"); err != nil {
		return st, err
	}
	if st.oldTracking, err = hasTable(db, "schema_migrations"); err != nil {
		return st, err
	}
	if st.logTracking, err = hasTable(db, schemaMigrationsTable(MigrationSetLog)); err != nil {
		return st, err
	}
	return st, nil
}

// copyCachesInto ATTACHes reference.db to the log connection and moves the
// surviving cache rows across.
func copyCachesInto(logDB *sql.DB, refPath string, state splitState) error {
	const op errors.Op = "sqlite.copyCachesInto"

	if _, err := logDB.Exec("ATTACH DATABASE '" + strings.ReplaceAll(refPath, "'", "''") + "' AS refdb"); err != nil {
		return errors.New(op).WithErr(err).WithMsg("attach reference database")
	}
	detached := false
	defer func() {
		// Best-effort cleanup for the error paths; the success path detaches
		// explicitly so a failure there is reported rather than swallowed.
		if !detached {
			_, _ = logDB.Exec("DETACH DATABASE refdb")
		}
	}()

	for _, tbl := range []struct {
		name    string
		present bool
	}{
		{"country", state.country},
		{"contacted_station", state.contacted},
	} {
		if !tbl.present {
			continue
		}
		if _, err := logDB.Exec("INSERT OR IGNORE INTO refdb." + tbl.name + " SELECT * FROM main." + tbl.name); err != nil {
			return errors.New(op).WithErr(err).WithMsgf("copy %s into reference.db", tbl.name)
		}
	}

	// Re-apply reference migration 0002's rule AFTER the copy. 0002 ran against
	// the empty, freshly-created reference.db, so it could not see these rows —
	// a poisonous one-character prefix cached before 2026-06-25 (the prefix='U'
	// → "European Russia" row that misfiled every Ukrainian UR–UZ call) survived
	// the upgrade and kept corrupting longest-prefix country matches
	// (2026-07-22 sqlite review, finding 1). Deleting a cache row is safe: the
	// next lookup cold-misses to hamnut and re-caches under a validated prefix.
	//
	// This mirrors 0002 deliberately rather than re-running the migration —
	// keep the two in step if that rule ever changes.
	//
	// Unconditional, NOT gated on state.country: a run that resumes after the
	// log DB's country table was already dropped still has to purge rows an
	// interrupted predecessor copied across before it could reach this step.
	// The DELETE is idempotent and refdb.country always exists by now.
	if _, err := logDB.Exec("DELETE FROM refdb.country WHERE LENGTH(TRIM(prefix)) <= 1"); err != nil {
		return errors.New(op).WithErr(err).WithMsg("purge single-char prefixes from copied rows")
	}

	if _, err := logDB.Exec("DETACH DATABASE refdb"); err != nil {
		return errors.New(op).WithErr(err).WithMsg("detach reference database")
	}
	detached = true
	return nil
}

// bootstrapDSN builds a modernc DSN for a transient bootstrap connection with
// the same load-bearing PRAGMAs the Service uses (busy_timeout, WAL,
// foreign_keys, synchronous). Kept separate from Service.getDsn because the
// bootstrap runs before any Service is open and takes a bare path.
//
// _time_format=sqlite matches getDsn — canonical SQLite timestamp serialisation
// (see the note there); the bootstrap copies rows between DBs, so it must write
// the same format the runtime does.
func bootstrapDSN(path string) string {
	return fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=synchronous(normal)&_time_format=sqlite",
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
