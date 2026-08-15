package evidence

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestReconcileProfiles_IteratorFailureDoesNotMutateActiveMapping(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fault := stderrors.New("profile iterator fault")
	rows := sqlmock.NewRows([]string{"profile_uuid"}).
		AddRow("0198-old").
		AddRow("0198-unread").
		RowError(1, fault)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT DISTINCT profile_uuid FROM profile_active").WillReturnRows(rows)
	mock.ExpectRollback()

	s := &Service{db: db}
	err = s.reconcileProfiles(time.Now())
	if !stderrors.Is(err, fault) {
		t.Fatalf("reconcileProfiles error = %v, want iterator fault", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database mutation after partial profile selection: %v", err)
	}
}

func TestPurgeSelections_IteratorFailureRollsBackWithoutReceiptOrDelete(t *testing.T) {
	tests := []struct {
		name  string
		query string
		rows  *sqlmock.Rows
		run   func(*Service) (bool, error)
	}{
		{
			name:  "cloud-present",
			query: "SELECT uuid, slot_start_utc, dial_mhz FROM observations",
			rows: sqlmock.NewRows([]string{"uuid", "slot_start_utc", "dial_mhz"}).
				AddRow("0198-one", "2026-08-15T00:00:00Z", 14.074).
				AddRow("0198-two", "2026-08-15T00:00:15Z", 14.074),
			run: (*Service).purgeAckedChunk,
		},
		{
			name:  "unsynced",
			query: "SELECT uuid, slot_start_utc, dial_mhz,",
			rows: sqlmock.NewRows([]string{"uuid", "slot_start_utc", "dial_mhz", "offered", "quarantined"}).
				AddRow("0198-one", "2026-08-15T00:00:00Z", 14.074, false, false).
				AddRow("0198-two", "2026-08-15T00:00:15Z", 14.074, false, false),
			run: (*Service).purgeUnsyncedChunk,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("open mock database: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			fault := stderrors.New(tt.name + " iterator fault")
			tt.rows.RowError(1, fault)
			mock.ExpectBegin()
			mock.ExpectQuery(tt.query).WillReturnRows(tt.rows)
			mock.ExpectRollback()

			changed, err := tt.run(&Service{db: db})
			if changed {
				t.Fatal("purge reported a committed change after partial iteration")
			}
			if !stderrors.Is(err, fault) {
				t.Fatalf("purge error = %v, want iterator fault", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unexpected receipt/delete after partial purge selection: %v", err)
			}
		})
	}
}

func TestSyncSelection_IteratorFailureEntersBackoffWithoutOfferingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open mock database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fault := stderrors.New("sync iterator fault")
	rows := sqlmock.NewRows([]string{"uuid"}).
		AddRow("0198-one").
		AddRow("0198-two").
		RowError(1, fault)
	mock.ExpectQuery("SELECT uuid FROM profiles").WillReturnRows(rows)

	s := &Service{db: db, log: logging.Noop()}
	if got := s.syncOnce(context.Background(), false); got != syncTransient {
		t.Fatalf("syncOnce result = %v, want transient failure", got)
	}
	if s.syncState != syncStateBackoff {
		t.Fatalf("sync state = %q, want %q", s.syncState, syncStateBackoff)
	}
	if !strings.Contains(s.syncLastErr, fault.Error()) {
		t.Fatalf("sync last error = %q, want iterator fault", s.syncLastErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sync offered or loaded a row after partial selection: %v", err)
	}
}
