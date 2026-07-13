package sqlite

import (
	"testing"

	migratepkg "github.com/golang-migrate/migrate/v4"
)

// applyReferenceMigrationSteps drives the REFERENCE set's migrations by a
// fixed number of steps — the sibling of applyMigrationSteps (log set).
func applyReferenceMigrationSteps(t *testing.T, svc *Service, steps int) {
	t.Helper()
	srcDriver, dbDriver, err := GetMigrationDrivers(svc.handle, MigrationSetReference)
	if err != nil {
		t.Fatalf("reference migration drivers: %v", err)
	}
	defer func() { _ = srcDriver.Close() }()

	m, err := migratepkg.NewWithInstance("iofs", srcDriver, svc.DatabaseConfig.Driver, dbDriver)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	if err := m.Steps(steps); err != nil {
		t.Fatalf("reference migrate steps(%d): %v", steps, err)
	}
}

// TestMigrate0002Reference_PurgesSingleCharPrefixes seeds the exact poison
// shape found on the dogfood DB — a prefix='U' → "European Russia" row that
// the longest-prefix read matched for every Ukrainian UR–UZ call — plus a
// healthy two-char row, then applies 0002 and asserts only the one-char row
// is purged. (Write-time rejection of new one-char rows is pinned by
// TestUpsertCountry_RejectsSingleCharPrefix; this covers rows cached before
// the gate existed.)
func TestMigrate0002Reference_PurgesSingleCharPrefixes(t *testing.T) {
	svc := testServiceWithoutMigrations(t)
	applyReferenceMigrationSteps(t, svc, 1) // 0001 — schema with the country table
	db := svc.handle

	seed := func(prefix, name string) {
		t.Helper()
		if _, err := db.Exec(
			`INSERT INTO country (name, cq_zone, itu_zone, continent, prefix, ccode, dxcc_prefix, time_offset)
			 VALUES (?, '16', '29', 'EU', ?, 'XX', ?, '2h 0m')`,
			name, prefix, prefix); err != nil {
			t.Fatalf("seed country %q: %v", prefix, err)
		}
	}
	seed("U", "European Russia") // the dogfood poison row (2026-06-25)
	seed("R", "European Russia") // same class, also over-matches Asiatic Russia / Kaliningrad
	seed("UR", "Ukraine")        // healthy two-char row — must survive

	applyReferenceMigrationSteps(t, svc, 1) // 0002 — the purge

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM country WHERE LENGTH(TRIM(prefix)) <= 1`).Scan(&n); err != nil {
		t.Fatalf("count one-char rows: %v", err)
	}
	if n != 0 {
		t.Errorf("one-char prefix rows survived 0002: %d", n)
	}

	var name string
	if err := db.QueryRow(`SELECT name FROM country WHERE prefix = 'UR'`).Scan(&name); err != nil {
		t.Fatalf("healthy UR row missing after 0002: %v", err)
	}
	if name != "Ukraine" {
		t.Errorf("UR row name = %q, want Ukraine", name)
	}
}
