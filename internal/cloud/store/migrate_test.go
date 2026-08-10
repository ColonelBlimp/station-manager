package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestMigrate_UpgradesExistingDatabaseWithData rehearses the deployed-box
// upgrade path: a database already migrated to version 1 and HOLDING DATA
// (tenant, logbook, QSOs) must come up to the latest version via the same
// store.Migrate call cmd/smcloud runs at boot — later migrations add
// constraints that VALIDATE existing rows, so an incompatibility surfaces
// here instead of as a boot failure on the production box. Same
// Postgres-or-skip gate as the rest of the suite.
func TestMigrate_UpgradesExistingDatabaseWithData(t *testing.T) {
	dsn := os.Getenv("SMCLOUD_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("smcloud store tests need a dev Postgres (task db:pg:up): open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("smcloud store tests need a dev Postgres (task db:pg:up): ping: %v", err)
	}
	lockTestDatabase(t, db)

	// Rebuild the version-1 world: schema from 0001 only, migration tracking
	// pinned at 1, and live rows in every table.
	execSQLFile(t, db, "migrations/0001_init.down.sql")
	if _, err := db.Exec(`DROP TABLE IF EXISTS schema_migrations`); err != nil {
		t.Fatalf("drop schema_migrations: %v", err)
	}
	execSQLFile(t, db, "migrations/0001_init.up.sql")
	seed := `
CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL);
INSERT INTO schema_migrations VALUES (1, false);
INSERT INTO tenants (callsign, name) VALUES ('7Q5MLV', 'Marc');
INSERT INTO logbooks (tenant_id, name) SELECT id, 'main' FROM tenants;
INSERT INTO qsos (uuid, tenant_id, logbook_id, modified_at, payload)
SELECT '0197f9a0-0000-7000-8000-00000000aaaa', t.id, l.id, now(), '{"call":"DL9UW"}'::jsonb
FROM tenants t JOIN logbooks l ON l.tenant_id = t.id`
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed version-1 data: %v", err)
	}
	t.Cleanup(func() {
		execSQLFile(t, db, "migrations/0005_evidence.down.sql")
		execSQLFile(t, db, "migrations/0001_init.down.sql")
		_, _ = db.Exec(`DROP TABLE IF EXISTS schema_migrations`)
		_ = db.Close()
	})

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate over existing v1 data: %v", err)
	}

	// The 0002 constraints exist and the data survived.
	var constraints int
	if err := db.QueryRow(`SELECT count(*) FROM pg_constraint
		WHERE conname IN ('qsos_logbook_tenant_fk', 'logbooks_id_tenant_key')`).Scan(&constraints); err != nil {
		t.Fatalf("count constraints: %v", err)
	}
	if constraints != 2 {
		t.Errorf("0002 constraints present = %d, want 2", constraints)
	}
	// 0004 rebuilt the PK over existing rows: two columns, not one.
	var pkCols int
	if err := db.QueryRow(`SELECT array_length(conkey, 1) FROM pg_constraint
		WHERE conrelid = 'qsos'::regclass AND contype = 'p'`).Scan(&pkCols); err != nil {
		t.Fatalf("read qsos pk: %v", err)
	}
	if pkCols != 2 {
		t.Errorf("qsos primary key spans %d columns, want 2 (tenant_id, uuid)", pkCols)
	}
	var qsos, version int
	if err := db.QueryRow(`SELECT count(*) FROM qsos`).Scan(&qsos); err != nil {
		t.Fatalf("count qsos: %v", err)
	}
	if err := db.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if qsos != 1 || version != 5 {
		t.Errorf("after upgrade: qsos = %d (want 1), schema version = %d (want 5)", qsos, version)
	}
}
