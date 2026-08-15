package txutil

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRollback_JoinsCleanupFailureToPrimary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock database: %v", err)
	}
	defer func() { _ = db.Close() }()
	primary := errors.New("mutation failed")
	rollback := errors.New("rollback failed")
	mock.ExpectBegin()
	mock.ExpectRollback().WillReturnError(rollback)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	resultErr := primary
	Rollback(tx, &resultErr)
	if !errors.Is(resultErr, primary) || !errors.Is(resultErr, rollback) {
		t.Fatalf("joined error = %v, want primary and rollback failures", resultErr)
	}
}

func TestRollback_IgnoresPostCommitErrTxDone(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock database: %v", err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectCommit()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var resultErr error
	Rollback(tx, &resultErr)
	if resultErr != nil {
		t.Fatalf("post-commit guard returned %v, want nil", resultErr)
	}
}
