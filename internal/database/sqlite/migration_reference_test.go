package sqlite

import (
	"strings"
	"testing"

	migratepkg "github.com/golang-migrate/migrate/v4"
)

// applyReferenceMigrationSteps drives the REFERENCE set's migrations by a
// fixed number of steps — the sibling of applyMigrationSteps (log set).
func applyReferenceMigrationSteps(t *testing.T, svc *Service, steps int) {
	t.Helper()
	if steps <= 0 {
		t.Fatalf("reference migrate steps(%d): up only; there is no way down", steps)
	}
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

// insertContactedStation is a raw-SQL writer for the enrichment cache, used by
// the 0003 CHECK tests so they exercise the column constraint directly rather
// than through the Upsert helper's Go-side validation.
func insertContactedStation(svc *Service, call string) error {
	_, err := svc.handle.Exec(
		`INSERT INTO contacted_station (name, call, country) VALUES ('N', ?, 'Test')`, call)
	return err
}

// TestMigrate0003Reference_WidensContactedStationCall pins the fix for the
// 2026-07-22 review finding 3: log migration 0006 widened qso.call to 32 but
// left the reference-domain cache at 20, so a valid 21..32-char callsign logged
// fine while every enrichment-cache write for it failed the CHECK — a silent,
// permanent cold miss (the write is best-effort, so nothing surfaced). Past 32
// the CHECK still bites, so the guard is widened rather than removed.
func TestMigrate0003Reference_WidensContactedStationCall(t *testing.T) {
	svc := testService(t) // all sets at head, including reference 0003 and later

	longCall := "A" + strings.Repeat("1", 31) // 32 chars, mirroring IsValidCallsign's ceiling
	if len(longCall) != 32 {
		t.Fatalf("test setup: longCall is %d chars, want 32", len(longCall))
	}
	if err := insertContactedStation(svc, longCall); err != nil {
		t.Fatalf("32-char call should cache after 0003: %v", err)
	}
	if err := insertContactedStation(svc, "A"+strings.Repeat("1", 32)); err == nil {
		t.Fatalf("33-char call should still violate CHECK(length(trim(call)) <= 32)")
	}

	// The rebuild must carry the partial unique index across — without it a
	// second active row for the same call would silently duplicate the cache.
	if err := insertContactedStation(svc, "G4ABC"); err != nil {
		t.Fatalf("seed G4ABC: %v", err)
	}
	if err := insertContactedStation(svc, "G4ABC"); err == nil {
		t.Fatalf("duplicate active call should violate uq_contacted_station_active_call")
	}
}

// TestMigrate0003Reference_AFileAt2KeepsItsRowsAndWidens crosses reference
// 0003 on a reference set built up to 2, as that build left it: the narrower
// CHECK is in force there, and the table rebuild keeps the row it holds.
func TestMigrate0003Reference_AFileAt2KeepsItsRowsAndWidens(t *testing.T) {
	svc := testServiceWithoutMigrations(t)
	applyReferenceMigrationSteps(t, svc, 2) // 0001 and 0002

	if err := insertContactedStation(svc, "G4ABC"); err != nil {
		t.Fatalf("seed G4ABC at 2: %v", err)
	}
	if err := insertContactedStation(svc, "A"+strings.Repeat("1", 31)); err == nil {
		t.Fatalf("at 2, a 32-char call should violate CHECK(call <= 20); the fixture is not the older shape")
	}

	applyReferenceMigrationSteps(t, svc, 1) // 0003

	var n int
	if err := svc.handle.QueryRow(
		`SELECT COUNT(*) FROM contacted_station WHERE call = 'G4ABC' AND name = 'N' AND country = 'Test'`).Scan(&n); err != nil {
		t.Fatalf("count G4ABC after 0003: %v", err)
	}
	if n != 1 {
		t.Errorf("G4ABC rows after 0003 = %d, want 1 — the rebuild dropped data", n)
	}
	if err := insertContactedStation(svc, "A"+strings.Repeat("1", 31)); err != nil {
		t.Fatalf("after 0003, a 32-char call should cache: %v", err)
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
