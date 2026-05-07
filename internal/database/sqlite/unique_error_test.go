package sqlite

import (
	"context"
	stderr "errors"
	"strings"
	"testing"

	moderncsqlite "modernc.org/sqlite"
)

// Regression for review-finding M5 (driver/typed-error mismatch).
// Pre-fix, isUniqueConstraintError detected mattn/go-sqlite3's typed
// error type while the daemon's actual driver is modernc.org/sqlite —
// the typed-error branch never matched at runtime, and correctness
// silently relied on the substring fallback. Post-fix, the helper
// matches modernc's *sqlite.Error type and the typed branch fires.
//
// The test runs raw SQL against the in-memory DB to trigger a real
// UNIQUE violation and asserts that (a) the helper returns true,
// and (b) the underlying error type is the modernc-driver type, not
// some other shape. The (b) assertion is the load-bearing one for
// future-proofing — if a driver swap reintroduces the mismatch,
// errors.As against *modernc.org/sqlite.Error will stop matching
// and this test fails with a clear "driver type drift" message.

// rawUniqueViolation runs raw SQL that is guaranteed to violate the
// UNIQUE index on logbook.name and returns the raw driver error
// (untranslated by any sqlite.Service method that wraps it into
// errors.ErrDuplicateName).
func rawUniqueViolation(t *testing.T, svc *Service) error {
	t.Helper()
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx,
		`INSERT INTO logbook (name, callsign) VALUES (?, ?)`,
		"RawDup", "G4ABC",
	); err != nil {
		t.Fatalf("first raw insert: %v", err)
	}
	// Second insert violates the UNIQUE index on logbook.name.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO logbook (name, callsign) VALUES (?, ?)`,
		"RawDup", "G4XYZ",
	)
	if err == nil {
		t.Fatal("expected UNIQUE violation on duplicate name; got nil")
	}
	return err
}

func TestIsUniqueConstraintError_RealViolation(t *testing.T) {
	svc := testService(t)

	err := rawUniqueViolation(t, svc)
	if !IsUniqueConstraintError(err) {
		t.Fatalf("IsUniqueConstraintError returned false for a real UNIQUE violation: %v\n"+
			"Likely cause: the helper is detecting the wrong driver's error "+
			"type. The daemon registers modernc.org/sqlite, so the typed "+
			"branch must match *modernc.org/sqlite.Error.", err)
	}
}

// TestIsUniqueConstraintError_TypedBranchMatches pins the property
// the M5 finding identified — the *typed* path (errors.As against
// *modernc.org/sqlite.Error) must succeed. Pre-fix this branch never
// matched and detection silently relied on the substring fallback.
func TestIsUniqueConstraintError_TypedBranchMatches(t *testing.T) {
	svc := testService(t)

	err := rawUniqueViolation(t, svc)
	var moderncErr *moderncsqlite.Error
	if !stderr.As(err, &moderncErr) {
		t.Fatalf("real driver error doesn't unwrap to *modernc.org/sqlite.Error — driver-type drift!\n"+
			"err: %v (%T)", err, err)
	}
	// And the substring-fallback message is the documented stable shape.
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Errorf("driver error text changed: %q does not contain \"UNIQUE constraint failed\". "+
			"The fallback path in IsUniqueConstraintError relies on this substring; "+
			"if upstream renamed the message, both branches need an audit.", err.Error())
	}
}

func TestIsUniqueConstraintError_NilAndUnrelated(t *testing.T) {
	if IsUniqueConstraintError(nil) {
		t.Error("nil error should not be classified as unique-constraint")
	}
	if IsUniqueConstraintError(errPlain("some other failure")) {
		t.Error("unrelated error should not be classified as unique-constraint")
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }
