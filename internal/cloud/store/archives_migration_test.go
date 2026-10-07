package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// W-0021 5F.1 (ruled 2026-10-07, ADR 0088): Postgres 0007 adds the archive
// boundary. Every existing logbook lands in its tenant's ONE legacy archive,
// unadopted (no archive or logbook UUID yet), keeping its legacy name — the
// key the name-only wire resolves — beside a display label. Nothing moves:
// rows, counts and the old wire stay as they were.
//
//	A1  upgrading a version-6 database with data: one legacy archive per
//	    tenant (a tenant with no logbooks too), every logbook in it with its
//	    name as legacy_name and label, no UUIDs, every QSO kept.
//	A2  a tenant created after the upgrade gets its legacy archive; re-ensuring
//	    keeps exactly one.
//	A3  the down step restores version 6 losslessly while nothing is adopted,
//	    and refuses — changing nothing — once a managed archive or a logbook
//	    UUID exists.

// openMigrationDB connects, locks, and drops every smcloud table.
func openMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn, skip := ResolveTestDSN()
	if skip != "" {
		t.Skip(skip)
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
	dropAll(t, db)
	t.Cleanup(func() {
		dropAll(t, db)
		_ = db.Close()
	})
	return db
}

// version6WithData lays down migrations 0001–0006, pins the tracking table at
// 6, and seeds two tenants: 7Q5MLV with logbooks main (two QSOs) and portable
// (one), and 7Q8AC with none.
func version6WithData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, f := range []string{"0001_init", "0002_qsos_logbook_tenant_fk", "0003_qsos_revision",
		"0004_qsos_tenant_scoped_uuid", "0005_evidence", "0006_retention"} {
		execSQLFile(t, db, "migrations/"+f+".up.sql")
	}
	seed := `
CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL);
INSERT INTO schema_migrations VALUES (6, false);
INSERT INTO tenants (callsign, name) VALUES ('7Q5MLV', 'Marc'), ('7Q8AC', '');
INSERT INTO logbooks (tenant_id, name) SELECT id, 'main' FROM tenants WHERE callsign = '7Q5MLV';
INSERT INTO logbooks (tenant_id, name) SELECT id, 'portable' FROM tenants WHERE callsign = '7Q5MLV';
INSERT INTO qsos (uuid, tenant_id, logbook_id, modified_at, payload)
SELECT u, l.tenant_id, l.id, now(), '{"call":"DL9UW"}'::jsonb
FROM logbooks l, (VALUES ('0197f9a0-0000-7000-8000-0000000000a1'::uuid), ('0197f9a0-0000-7000-8000-0000000000a2'::uuid)) v(u)
WHERE l.name = 'main';
INSERT INTO qsos (uuid, tenant_id, logbook_id, modified_at, payload)
SELECT '0197f9a0-0000-7000-8000-0000000000a3', l.tenant_id, l.id, now(), '{"call":"EA1B"}'::jsonb
FROM logbooks l WHERE l.name = 'portable'`
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed version-6 data: %v", err)
	}
}

func scalar[T any](t *testing.T, db *sql.DB, q string, args ...any) T {
	t.Helper()
	var v T
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", strings.TrimSpace(q), err)
	}
	return v
}

func TestArchives_A1_UpgradePlacesEveryLogbookInItsTenantsLegacyArchive(t *testing.T) {
	db := openMigrationDB(t)
	version6WithData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate over version-6 data: %v", err)
	}
	if v := scalar[int](t, db, `SELECT version FROM schema_migrations`); v != 7 {
		t.Fatalf("schema version = %d; want 7", v)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM tenants t
		WHERE (SELECT count(*) FROM archives a WHERE a.tenant_id = t.id AND a.legacy AND a.archive_uuid IS NULL) = 1`); n != 2 {
		t.Fatalf("tenants with exactly one unadopted legacy archive = %d; want 2 (7Q8AC has no logbooks)", n)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM archives WHERE NOT legacy`); n != 0 {
		t.Fatalf("non-legacy archives after the upgrade = %d; want 0", n)
	}
	rows, err := db.Query(`SELECT l.legacy_name, l.label, l.uuid IS NULL, a.legacy, a.tenant_id = l.tenant_id
		FROM logbooks l JOIN archives a ON a.id = l.archive_id ORDER BY l.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var legacyName, label string
		var noUUID, legacy, sameTenant bool
		if err := rows.Scan(&legacyName, &label, &noUUID, &legacy, &sameTenant); err != nil {
			t.Fatal(err)
		}
		if label != legacyName || !noUUID || !legacy || !sameTenant {
			t.Fatalf("logbook %q: label %q, no uuid %v, legacy archive %v, same tenant %v", legacyName, label, noUUID, legacy, sameTenant)
		}
		got = append(got, legacyName)
	}
	if strings.Join(got, ",") != "main,portable" {
		t.Fatalf("logbooks = %v; want main, portable", got)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM qsos`); n != 3 {
		t.Fatalf("qsos after the upgrade = %d; want 3", n)
	}
}

func TestArchives_A2_ANewTenantGetsItsLegacyArchiveOnce(t *testing.T) {
	db := openMigrationDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	ctx := context.Background()
	tid, err := s.EnsureTenant(ctx, "7Q5MLV", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureTenant(ctx, "7Q5MLV", "Marc"); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM archives WHERE tenant_id = $1 AND legacy`, tid); n != 1 {
		t.Fatalf("legacy archives for a new tenant = %d; want 1", n)
	}
	lid, err := s.EnsureLogbook(ctx, tid, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !scalar[bool](t, db, `SELECT a.legacy FROM logbooks l JOIN archives a ON a.id = l.archive_id WHERE l.id = $1`, lid) {
		t.Fatal("a name-wire logbook was not placed in the legacy archive")
	}
}

func TestArchives_A3_DownIsLosslessUnadoptedAndRefusesOtherwise(t *testing.T) {
	t.Run("unadopted: back to version 6 with the data", func(t *testing.T) {
		db := openMigrationDB(t)
		version6WithData(t, db)
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
		execSQLFile(t, db, "migrations/0007_archives.down.sql")
		if n := scalar[int](t, db, `SELECT count(*) FROM pg_class WHERE relname = 'archives'`); n != 0 {
			t.Fatal("archives table still present after the down step")
		}
		if got := scalar[string](t, db, `SELECT string_agg(name, ',' ORDER BY id) FROM logbooks`); got != "main,portable" {
			t.Fatalf("logbook names after down = %q", got)
		}
		if n := scalar[int](t, db, `SELECT count(*) FROM qsos`); n != 3 {
			t.Fatalf("qsos after down = %d; want 3", n)
		}
		// Version 6's identity rule is back: a name is unique per tenant.
		_, err := db.Exec(`INSERT INTO logbooks (tenant_id, name) SELECT tenant_id, 'main' FROM logbooks WHERE name = 'main'`)
		if err == nil || !strings.Contains(err.Error(), "unique") {
			t.Fatalf("duplicate (tenant, name) after down: %v; want a unique violation", err)
		}
	})
	for _, c := range []struct{ name, adopt string }{
		{"a managed archive exists", `INSERT INTO archives (tenant_id, archive_uuid, label) SELECT id, '01920000-0000-7000-8000-0000000000ff', 'Contest' FROM tenants WHERE callsign = '7Q5MLV'`},
		{"a logbook carries a uuid", `UPDATE logbooks SET uuid = '01920000-0000-7000-8000-0000000000ee' WHERE legacy_name = 'main'`},
	} {
		t.Run("refused: "+c.name, func(t *testing.T) {
			db := openMigrationDB(t)
			version6WithData(t, db)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(c.adopt); err != nil {
				t.Fatal(err)
			}
			b, err := migrationsFS.ReadFile("migrations/0007_archives.down.sql")
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(string(b))
			if err == nil || !strings.Contains(err.Error(), "identity") {
				t.Fatalf("down with %s: %v; want a refusal naming the identity data", c.name, err)
			}
			if n := scalar[int](t, db, `SELECT count(*) FROM pg_class WHERE relname = 'archives'`); n != 1 {
				t.Fatal("the refused down step changed the schema")
			}
		})
	}
}
