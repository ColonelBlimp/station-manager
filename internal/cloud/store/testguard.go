package store

import (
	"database/sql"
	"fmt"
)

// testSentinelTable marks a database as a disposable test database. Its
// presence is what lets the integration-test harnesses run their destructive
// schema teardown; it survives the migration drops (only the migration tables
// are dropped), so a database wiped once stays marked.
const testSentinelTable = "__sm_test_db__"

// RefuseNonTestDatabase guards the destructive schema teardown the integration
// tests run against a real Postgres (package review, 2026-08-10). The default
// DSN is postgres://smcloud:smcloud@localhost:5432/smcloud, which could equally
// be a developer's working database, and the advisory lock only serialises
// test runs — it does not protect application data. This refuses to proceed
// unless the target is safe to wipe:
//
//   - a database already carrying the test sentinel (a prior test run) → allow;
//   - a database with NO application data → stamp the sentinel and allow;
//   - a database holding application rows but no sentinel → REFUSE, so an
//     ordinary `go test` can never erase a real database. Point the tests at a
//     disposable database via SMCLOUD_TEST_DSN (e.g. `task db:pg:up`).
//
// It is exported so every test harness (store, server, and the smcloud
// forwarder e2e) shares one implementation.
func RefuseNonTestDatabase(db *sql.DB) error {
	var hasSentinel bool
	if err := db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		  WHERE table_schema = 'public' AND table_name = $1)`, testSentinelTable).Scan(&hasSentinel); err != nil {
		return fmt.Errorf("smcloud test guard: probe sentinel: %w", err)
	}
	if hasSentinel {
		return nil
	}

	var hasTenants bool
	if err := db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		  WHERE table_schema = 'public' AND table_name = 'tenants')`).Scan(&hasTenants); err != nil {
		return fmt.Errorf("smcloud test guard: probe tenants: %w", err)
	}
	if hasTenants {
		var rows int
		if err := db.QueryRow(`SELECT COUNT(*) FROM tenants`).Scan(&rows); err != nil {
			return fmt.Errorf("smcloud test guard: count tenants: %w", err)
		}
		if rows > 0 {
			return fmt.Errorf(
				"refusing to run destructive smcloud tests: the target database holds application data (%d tenants) "+
					"and carries no %s sentinel — point the tests at a disposable database via SMCLOUD_TEST_DSN "+
					"(e.g. `task db:pg:up`)", rows, testSentinelTable)
		}
	}

	if _, err := db.Exec(
		`CREATE TABLE IF NOT EXISTS ` + testSentinelTable + ` (created_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("smcloud test guard: stamp sentinel: %w", err)
	}
	return nil
}
