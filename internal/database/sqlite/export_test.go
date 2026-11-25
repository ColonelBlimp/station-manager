package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ValidateDatastoreConfig exposes the unexported consumer validator so the CC-3 parity
// test (in package sqlite_test) can confirm it agrees with config.Validate on every
// input. Compiled ONLY under test, so it never becomes a production entry point.
func ValidateDatastoreConfig(cfg *types.DatastoreConfig) error {
	return validateConfig(cfg)
}

// NewServiceWithDB builds a Service backed by an already-open *sql.DB, for tests that
// need a controllable driver (e.g. go-sqlmock) to exercise transaction and rollback
// paths deterministically. It sets the same unexported readiness state the real
// Open() would, and — living in export_test.go — is compiled ONLY under test, so it
// never becomes a production construction path.
func NewServiceWithDB(db *sql.DB) *Service {
	s := &Service{
		handle: db,
		// Non-zero timeouts so ensureCtxTimeout / BeginTxContext don't hand out an
		// already-expired context (seconds, as the real config uses).
		DatabaseConfig: &types.DatastoreConfig{
			ContextTimeout:            10,
			TransactionContextTimeout: 10,
		},
	}
	s.isInitialized.Store(true)
	s.isOpen.Store(true)
	return s
}

// The three below are for operating_session_submit_order_test.go, which forces
// the order of the real submit against the session's controls. It is in
// package sqlite_test because the submit is qsoservice's, which imports this
// package; these hand it the seams the storage contention tests use.

// MigratedFileServiceForTest is testMigratedFileService: a file of the test's
// own at the head, opened through the real Service with the configuration's
// own connection settings.
func MigratedFileServiceForTest(t *testing.T) (*Service, string) {
	t.Helper()
	return testMigratedFileService(t)
}

// HoldAControlForTest installs the contention seams (script) for this test. A
// session control run under ctx stops once it has the writer's position:
// locked is closed then, and the control goes on to its commit at release.
func HoldAControlForTest(t *testing.T) (ctx context.Context, locked <-chan struct{}, release func()) {
	t.Helper()
	c := script(t)
	return asRole(holderRole), c.locked, c.release
}

// HeldDefaultPointerChangeForTest moves the default pointer with the setters'
// own statement in a transaction it does not commit: it holds the writer's
// position until the test commits it.
func HeldDefaultPointerChangeForTest(t *testing.T, s *Service, to int64) *sql.Tx {
	t.Helper()
	return (&sessBed{t: t, svc: s}).heldPointerChange(to)
}
