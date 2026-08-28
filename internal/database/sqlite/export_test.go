package sqlite

import (
	"database/sql"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

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
