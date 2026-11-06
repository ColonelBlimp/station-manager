package sqlite

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/DATA-DOG/go-sqlmock"
)

func rowsAffectedTestService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := &Service{
		handle:         db,
		DatabaseConfig: &types.DatastoreConfig{},
	}
	s.isInitialized.Store(true)
	s.isOpen.Store(true)
	return s, mock
}

func TestUploadCompletion_RowsAffectedFailureDoesNotRunZeroRowClassifier(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Service) error
	}{
		{
			name: "success",
			run: func(s *Service) error {
				return s.MarkUploadSuccessWithContext(context.Background(), 1, "upstream")
			},
		},
		{
			name: "transient retry",
			run: func(s *Service) error {
				return s.MarkUploadTransientRetryWithContext(context.Background(), 1, 2, "retry")
			},
		},
		{
			name: "failed",
			run: func(s *Service) error {
				return s.MarkUploadFailedWithContext(context.Background(), 1, "failed", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, mock := rowsAffectedTestService(t)
			fault := stderrors.New(tt.name + " rows affected fault")
			mock.ExpectExec("UPDATE qso_upload").WillReturnResult(sqlmock.NewErrorResult(fault))

			err := tt.run(s)
			if !stderrors.Is(err, fault) {
				t.Fatalf("completion error = %v, want rows-affected fault", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("zero-row classifier ran after result failure: %v", err)
			}
		})
	}
}

func TestMarkUploadSuccessWithAdifStamp_RowsAffectedFailureRollsBack(t *testing.T) {
	tests := []struct {
		name        string
		firstResult bool
	}{
		{name: "upload update"},
		{name: "qso stamp", firstResult: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, mock := rowsAffectedTestService(t)
			fault := stderrors.New(tt.name + " rows affected fault")
			mock.ExpectBegin()
			if tt.firstResult {
				mock.ExpectExec("UPDATE qso_upload").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("UPDATE qso").WillReturnResult(sqlmock.NewErrorResult(fault))
			} else {
				mock.ExpectExec("UPDATE qso_upload").WillReturnResult(sqlmock.NewErrorResult(fault))
			}
			mock.ExpectRollback()

			err := s.MarkUploadSuccessWithAdifStampWithContext(
				context.Background(), 1, "upstream", 2, "QRZCOM")
			if !stderrors.Is(err, fault) {
				t.Fatalf("stamped completion error = %v, want rows-affected fault", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("transaction committed or classifier ran after result failure: %v", err)
			}
		})
	}
}

func TestMarkSessionEmailed_QueryFailureReturnsError(t *testing.T) {
	s, mock := rowsAffectedTestService(t)
	fault := stderrors.New("session email query fault")
	// The revision-guarded stamp reads its result set via UPDATE ... RETURNING inside
	// an explicit transaction (W-0010 outcome 11), so a DB fault surfaces on the
	// query itself and the transaction is rolled back.
	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE qso").WillReturnError(fault)
	mock.ExpectRollback()

	stamped, err := s.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]SessionEmailTarget{{ID: 1, Revision: 0}, {ID: 2, Revision: 0}}, "20260815")
	if stamped != nil {
		t.Fatalf("stamped = %v, want nil on query failure", stamped)
	}
	if !stderrors.Is(err, fault) {
		t.Fatalf("session stamp error = %v, want query fault", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A statement failure whose explicit rollback ALSO fails leaves the connection's
// state unknown; the mark classifies it so the caller stops retrying.
func TestMarkSessionEmailed_RollbackFailureIsClassified(t *testing.T) {
	s, mock := rowsAffectedTestService(t)
	fault := stderrors.New("session email query fault")
	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE qso").WillReturnError(fault)
	mock.ExpectRollback().WillReturnError(stderrors.New("rollback fault"))

	stamped, err := s.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]SessionEmailTarget{{ID: 1, Revision: 0}}, "20260815")
	if stamped != nil {
		t.Fatalf("stamped = %v, want nil", stamped)
	}
	if !stderrors.Is(err, ErrSessionRollbackFailed) || !stderrors.Is(err, fault) {
		t.Fatalf("err = %v, want ErrSessionRollbackFailed wrapping the statement fault", err)
	}
	if stderrors.Is(err, ErrSessionMarkUncertain) {
		t.Fatalf("a statement failure is not an uncertain commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A failed BEGIN has no transaction to roll back: no rollback is attempted.
func TestMarkSessionEmailed_BeginFailureAttemptsNoRollback(t *testing.T) {
	s, mock := rowsAffectedTestService(t)
	fault := stderrors.New("begin fault")
	mock.ExpectBegin().WillReturnError(fault)

	_, err := s.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]SessionEmailTarget{{ID: 1, Revision: 0}}, "20260815")
	if !stderrors.Is(err, fault) {
		t.Fatalf("err = %v, want the begin fault", err)
	}
	if stderrors.Is(err, ErrSessionRollbackFailed) || stderrors.Is(err, ErrSessionMarkUncertain) {
		t.Fatalf("a failed BEGIN is neither a rollback failure nor an uncertain commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A verification read whose rollback also fails is classified like the mark's, so
// the caller stops instead of retrying on a connection in an unknown state.
func TestFetchCurrentQsos_RollbackFailureIsClassified(t *testing.T) {
	s, mock := rowsAffectedTestService(t)
	fault := stderrors.New("read fault")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnError(fault)
	mock.ExpectRollback().WillReturnError(stderrors.New("rollback fault"))

	_, err := s.FetchCurrentQsosByIDWithContext(context.Background(), []int64{1})
	if !stderrors.Is(err, ErrSessionRollbackFailed) || !stderrors.Is(err, fault) {
		t.Fatalf("err = %v, want ErrSessionRollbackFailed wrapping the read fault", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A verification read whose rollback succeeds returns the read fault alone.
func TestFetchCurrentQsos_ReadFailureRollsBack(t *testing.T) {
	s, mock := rowsAffectedTestService(t)
	fault := stderrors.New("read fault")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnError(fault)
	mock.ExpectRollback()

	_, err := s.FetchCurrentQsosByIDWithContext(context.Background(), []int64{1})
	if !stderrors.Is(err, fault) || stderrors.Is(err, ErrSessionRollbackFailed) {
		t.Fatalf("err = %v, want the read fault, not a rollback failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
