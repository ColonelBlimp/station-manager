package sqlite

import (
	"database/sql"
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
