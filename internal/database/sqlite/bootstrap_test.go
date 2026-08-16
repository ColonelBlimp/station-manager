package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func bsExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func bsCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// seedCombinedDB builds a faithful pre-split single-file database: all six
// tables, a single combined `schema_migrations` table at version 2 (the old
// lineage), and one row in each of logbook / country / contacted_station.
func seedCombinedDB(t *testing.T, logPath string) {
	t.Helper()
	db, err := sql.Open(SqliteDriver, bootstrapDSN(logPath))
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Use the real migration sets to build the schema, then collapse the two
	// per-set tracking tables into the single combined one the old layout had.
	if err := applyMigrationSetTo(db, MigrationSetLog); err != nil {
		t.Fatalf("seed log set: %v", err)
	}
	if err := applyMigrationSetTo(db, MigrationSetReference); err != nil {
		t.Fatalf("seed reference set: %v", err)
	}
	bsExec(t, db, "DROP TABLE "+schemaMigrationsTable(MigrationSetLog))
	bsExec(t, db, "DROP TABLE "+schemaMigrationsTable(MigrationSetReference))
	bsExec(t, db, "CREATE TABLE schema_migrations (version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL)")
	bsExec(t, db, "INSERT INTO schema_migrations (version, dirty) VALUES (2, 0)")

	bsExec(t, db, `INSERT INTO logbook (name, callsign) VALUES ('Primary', '7Q5MLV')`)
	bsExec(t, db, `INSERT INTO country (last_refreshed_at, name, cq_zone, itu_zone, continent, prefix, ccode, dxcc_prefix, time_offset)
		VALUES (NULL, 'Malawi', '37', '53', 'AF', '7Q', '0', '7Q', '+2')`)
	bsExec(t, db, `INSERT INTO contacted_station (last_refreshed_at, name, call, country)
		VALUES (NULL, 'Test Op', 'K7IOC', 'United States')`)
}

func TestBootstrapReferenceSplit_MigratesExistingSingleFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "station-manager.db")
	refPath := filepath.Join(dir, "reference.db")
	backupDir := filepath.Join(dir, "backups")

	seedCombinedDB(t, logPath)

	if err := BootstrapReferenceSplit(logPath, refPath, backupDir, nil); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// A single timestamped backup was made.
	backups, err := filepath.Glob(filepath.Join(backupDir, "*.db"))
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %d, want exactly 1", len(backups))
	}

	// ST-6: the backup directory + the backup database it holds contain full QSO records,
	// so they are owner-private (0700 dir, 0600 file). Assert effective modes.
	if fi, err := os.Lstat(backupDir); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o700 {
		t.Errorf("backup dir mode = %04o, want 0700", fi.Mode().Perm())
	}
	if fi, err := os.Lstat(backups[0]); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("backup db mode = %04o, want 0600", fi.Mode().Perm())
	}

	// reference.db carries the cache tables + their seeded rows.
	refDB, err := sql.Open(SqliteDriver, bootstrapDSN(refPath))
	if err != nil {
		t.Fatalf("open reference.db: %v", err)
	}
	defer func() { _ = refDB.Close() }()
	if n := bsCount(t, refDB, "country"); n != 1 {
		t.Errorf("reference.db country rows = %d, want 1", n)
	}
	if n := bsCount(t, refDB, "contacted_station"); n != 1 {
		t.Errorf("reference.db contacted_station rows = %d, want 1", n)
	}

	// The log DB lost the cache tables, kept its own data, and had its tracking
	// table re-keyed to the log set at the same version (so no 0002 re-run).
	logDB, err := sql.Open(SqliteDriver, bootstrapDSN(logPath))
	if err != nil {
		t.Fatalf("reopen log db: %v", err)
	}
	defer func() { _ = logDB.Close() }()

	for _, tbl := range []string{"country", "contacted_station"} {
		if has, _ := hasTable(logDB, tbl); has {
			t.Errorf("log DB still has cache table %q after split", tbl)
		}
	}
	if n := bsCount(t, logDB, "logbook"); n != 1 {
		t.Errorf("log DB logbook rows = %d, want 1 (data must survive)", n)
	}
	has, err := hasTable(logDB, schemaMigrationsTable(MigrationSetLog))
	if err != nil {
		t.Fatalf("hasTable schema_migrations_log: %v", err)
	}
	if !has {
		t.Fatal("log DB missing schema_migrations_log (combined tracking not re-keyed)")
	}
	if oldStill, _ := hasTable(logDB, "schema_migrations"); oldStill {
		t.Error("combined schema_migrations table should have been renamed away")
	}
	var ver int
	if err := logDB.QueryRow("SELECT version FROM " + schemaMigrationsTable(MigrationSetLog)).Scan(&ver); err != nil {
		t.Fatalf("read schema_migrations_log version: %v", err)
	}
	if ver != 2 {
		t.Errorf("schema_migrations_log version = %d, want 2 (head, so log migrate is a no-op)", ver)
	}
}

func TestBootstrapReferenceSplit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "station-manager.db")
	refPath := filepath.Join(dir, "reference.db")
	backupDir := filepath.Join(dir, "backups")

	seedCombinedDB(t, logPath)

	if err := BootstrapReferenceSplit(logPath, refPath, backupDir, nil); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	// Second run: the cache tables are gone from the log DB, so it must detect
	// "already split" and do nothing — no error, no duplicate cache rows.
	if err := BootstrapReferenceSplit(logPath, refPath, backupDir, nil); err != nil {
		t.Fatalf("second bootstrap (idempotent): %v", err)
	}

	refDB, err := sql.Open(SqliteDriver, bootstrapDSN(refPath))
	if err != nil {
		t.Fatalf("open reference.db: %v", err)
	}
	defer func() { _ = refDB.Close() }()
	if n := bsCount(t, refDB, "country"); n != 1 {
		t.Errorf("reference.db country rows = %d after re-run, want 1 (no duplication)", n)
	}
}

// TestBootstrapReferenceSplit_ResumesInterruptedCleanup reconstructs the exact
// intermediate state a crash mid-cleanup leaves behind — country dropped,
// contacted_station and the combined `schema_migrations` still present — and
// asserts the next run FINISHES the split. Before the fix the completion check
// keyed on `country` alone, so this state was classified as "already split" and
// returned immediately: contacted_station stayed in the log DB and the old
// combined lineage was never re-keyed, which would then let the log connection
// re-run its migrations over live data (2026-07-22 review, finding 5).
func TestBootstrapReferenceSplit_ResumesInterruptedCleanup(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "station-manager.db")
	refPath := filepath.Join(dir, "reference.db")
	backupDir := filepath.Join(dir, "backups")

	seedCombinedDB(t, logPath)

	// Simulate the interruption: the first run copied the rows and got as far
	// as dropping `country`, then died before dropping contacted_station or
	// renaming the tracking table.
	logDB, err := sql.Open(SqliteDriver, bootstrapDSN(logPath))
	if err != nil {
		t.Fatalf("open log db: %v", err)
	}
	bsExec(t, logDB, "DROP INDEX IF EXISTS idx_country_name")
	bsExec(t, logDB, "DROP TABLE country")
	if err := logDB.Close(); err != nil {
		t.Fatalf("close log db: %v", err)
	}

	if err := BootstrapReferenceSplit(logPath, refPath, backupDir, nil); err != nil {
		t.Fatalf("resume bootstrap: %v", err)
	}

	reopened, err := sql.Open(SqliteDriver, bootstrapDSN(logPath))
	if err != nil {
		t.Fatalf("reopen log db: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	if has, _ := hasTable(reopened, "contacted_station"); has {
		t.Error("contacted_station survived the resumed split — the cleanup was skipped")
	}
	if has, _ := hasTable(reopened, "schema_migrations"); has {
		t.Error("combined schema_migrations survived — the log set would re-run its migrations on live data")
	}
	has, err := hasTable(reopened, schemaMigrationsTable(MigrationSetLog))
	if err != nil {
		t.Fatalf("hasTable schema_migrations_log: %v", err)
	}
	if !has {
		t.Error("schema_migrations_log missing after the resumed split")
	}
	if n := bsCount(t, reopened, "logbook"); n != 1 {
		t.Errorf("log DB logbook rows = %d, want 1 (data must survive a resume)", n)
	}
}

// TestBootstrapReferenceSplit_PurgesSingleCharPrefixesFromCopiedRows pins
// finding 1: reference migration 0002 deletes one-character country prefixes,
// but the split runs it against the still-EMPTY reference.db and only then
// copies the old rows in — so a prefix='U' row cached before the 2026-06-25
// write-time gate survived the upgrade and kept claiming every Ukrainian UR–UZ
// call as European Russia. The healthy multi-char rows must be untouched.
func TestBootstrapReferenceSplit_PurgesSingleCharPrefixesFromCopiedRows(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "station-manager.db")
	refPath := filepath.Join(dir, "reference.db")
	backupDir := filepath.Join(dir, "backups")

	seedCombinedDB(t, logPath)

	logDB, err := sql.Open(SqliteDriver, bootstrapDSN(logPath))
	if err != nil {
		t.Fatalf("open log db: %v", err)
	}
	seed := func(prefix, name string) {
		t.Helper()
		if _, err := logDB.Exec(
			`INSERT INTO country (name, cq_zone, itu_zone, continent, prefix, ccode, dxcc_prefix, time_offset)
			 VALUES (?, '16', '29', 'EU', ?, 'XX', ?, '2h 0m')`, name, prefix, prefix); err != nil {
			t.Fatalf("seed country %q: %v", prefix, err)
		}
	}
	seed("U", "European Russia") // the dogfood poison row
	seed("UR", "Ukraine")        // healthy — must survive
	if err := logDB.Close(); err != nil {
		t.Fatalf("close log db: %v", err)
	}

	if err := BootstrapReferenceSplit(logPath, refPath, backupDir, nil); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	refDB, err := sql.Open(SqliteDriver, bootstrapDSN(refPath))
	if err != nil {
		t.Fatalf("open reference.db: %v", err)
	}
	defer func() { _ = refDB.Close() }()

	var n int
	if err := refDB.QueryRow(
		`SELECT COUNT(*) FROM country WHERE LENGTH(TRIM(prefix)) <= 1`).Scan(&n); err != nil {
		t.Fatalf("count one-char rows: %v", err)
	}
	if n != 0 {
		t.Errorf("one-char prefix rows carried into reference.db: %d", n)
	}

	var name string
	if err := refDB.QueryRow(`SELECT name FROM country WHERE prefix = 'UR'`).Scan(&name); err != nil {
		t.Fatalf("healthy UR row missing after the split: %v", err)
	}
	if name != "Ukraine" {
		t.Errorf("UR row name = %q, want Ukraine", name)
	}
	// The seeded 7Q row from seedCombinedDB must also survive.
	if err := refDB.QueryRow(`SELECT name FROM country WHERE prefix = '7Q'`).Scan(&name); err != nil {
		t.Fatalf("healthy 7Q row missing after the split: %v", err)
	}
}

// TestBootstrapReferenceSplit_PurgesOnResumeAfterCountryDropped covers the
// awkward corner where the two fixes meet: an interrupted run already copied
// the country rows (poison included) and dropped the log DB's country table,
// but died before the purge. The resuming run has no country table left to
// copy from, so a purge gated on "country still present" would never fire and
// the poison row would live on in reference.db forever.
func TestBootstrapReferenceSplit_PurgesOnResumeAfterCountryDropped(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "station-manager.db")
	refPath := filepath.Join(dir, "reference.db")
	backupDir := filepath.Join(dir, "backups")

	seedCombinedDB(t, logPath)

	// Stand up reference.db as the interrupted run left it: schema at head,
	// country rows already copied across — poison and all.
	refDB, err := sql.Open(SqliteDriver, bootstrapDSN(refPath))
	if err != nil {
		t.Fatalf("open reference.db: %v", err)
	}
	if err := applyMigrationSetTo(refDB, MigrationSetReference); err != nil {
		t.Fatalf("migrate reference.db: %v", err)
	}
	bsExec(t, refDB, `INSERT INTO country (name, cq_zone, itu_zone, continent, prefix, ccode, dxcc_prefix, time_offset)
		VALUES ('European Russia', '16', '29', 'EU', 'U', 'XX', 'U', '2h 0m')`)
	bsExec(t, refDB, `INSERT INTO country (name, cq_zone, itu_zone, continent, prefix, ccode, dxcc_prefix, time_offset)
		VALUES ('Ukraine', '16', '29', 'EU', 'UR', 'XX', 'UR', '2h 0m')`)
	if err := refDB.Close(); err != nil {
		t.Fatalf("close reference.db: %v", err)
	}

	// ...and the log DB with country already dropped, contacted_station not.
	logDB, err := sql.Open(SqliteDriver, bootstrapDSN(logPath))
	if err != nil {
		t.Fatalf("open log db: %v", err)
	}
	bsExec(t, logDB, "DROP INDEX IF EXISTS idx_country_name")
	bsExec(t, logDB, "DROP TABLE country")
	if err := logDB.Close(); err != nil {
		t.Fatalf("close log db: %v", err)
	}

	if err := BootstrapReferenceSplit(logPath, refPath, backupDir, nil); err != nil {
		t.Fatalf("resume bootstrap: %v", err)
	}

	reopenedRef, err := sql.Open(SqliteDriver, bootstrapDSN(refPath))
	if err != nil {
		t.Fatalf("reopen reference.db: %v", err)
	}
	defer func() { _ = reopenedRef.Close() }()

	var n int
	if err := reopenedRef.QueryRow(
		`SELECT COUNT(*) FROM country WHERE LENGTH(TRIM(prefix)) <= 1`).Scan(&n); err != nil {
		t.Fatalf("count one-char rows: %v", err)
	}
	if n != 0 {
		t.Errorf("poison prefix survived the resumed split: %d row(s)", n)
	}
	var name string
	if err := reopenedRef.QueryRow(`SELECT name FROM country WHERE prefix = 'UR'`).Scan(&name); err != nil {
		t.Fatalf("healthy UR row missing after the resume: %v", err)
	}
}

func TestBootstrapReferenceSplit_FreshAndMemoryAreNoops(t *testing.T) {
	dir := t.TempDir()
	// Non-existent log file → fresh install → no-op, no error, no files created.
	logPath := filepath.Join(dir, "does-not-exist.db")
	refPath := filepath.Join(dir, "reference.db")
	if err := BootstrapReferenceSplit(logPath, refPath, filepath.Join(dir, "backups"), nil); err != nil {
		t.Fatalf("fresh-install bootstrap should be a no-op: %v", err)
	}
	if _, err := sql.Open(SqliteDriver, bootstrapDSN(refPath)); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, statErr := filepathGlobLen(t, dir, "reference.db"); statErr {
		t.Error("reference.db should not be created on a fresh-install no-op")
	}

	// :memory: → always a no-op.
	if err := BootstrapReferenceSplit(":memory:", refPath, dir, nil); err != nil {
		t.Fatalf(":memory: bootstrap should be a no-op: %v", err)
	}
}

// filepathGlobLen reports whether a file matching name exists in dir.
func filepathGlobLen(t *testing.T, dir, name string) (int, bool) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return len(matches), len(matches) > 0
}
