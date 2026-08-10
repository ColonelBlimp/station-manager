package store

import "os"

// DefaultTestDSN is the disposable Postgres the integration tests use when the
// default is explicitly enabled (task db:pg:up brings it up; CI runs a fresh
// service container at the same address).
const DefaultTestDSN = "postgres://smcloud:smcloud@localhost:5432/smcloud?sslmode=disable"

// ResolveTestDSN returns the Postgres DSN the integration tests should use and,
// when they must be skipped for lack of an explicit test-database opt-in, a
// non-empty reason (package review, 2026-08-10).
//
// The default DSN (postgres://smcloud:smcloud@localhost:5432/smcloud) could
// equally be a developer's working database, and these tests run destructive
// schema teardown. The safe signal is a CURRENT, explicit opt-in — never a
// persistent marker, which authorizes wiping a database that was empty during
// one run and later repurposed for real:
//
//   - SMCLOUD_TEST_DSN set → the caller named a specific database; that IS the
//     opt-in, so it is used as-is.
//   - otherwise the default localhost DSN is used ONLY when
//     SMCLOUD_TEST_ALLOW_DEFAULT is set (task test and CI set it, since both
//     run against a disposable database); an ordinary `go test` skips rather
//     than risk erasing whatever is at the default address.
//
// Exported so every harness (store, server, and the smcloud forwarder e2e)
// shares one policy.
func ResolveTestDSN() (dsn, skip string) {
	if d := os.Getenv("SMCLOUD_TEST_DSN"); d != "" {
		return d, ""
	}
	if os.Getenv("SMCLOUD_TEST_ALLOW_DEFAULT") == "" {
		return "", "smcloud integration tests skipped: set SMCLOUD_TEST_DSN to a disposable database, " +
			"or SMCLOUD_TEST_ALLOW_DEFAULT=1 to use the default localhost DSN (task test / `task db:pg:up` do this)"
	}
	return DefaultTestDSN, ""
}
