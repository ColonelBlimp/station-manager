package sqlite_test

import (
	"context"
	"database/sql"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

var (
	pt5InsertFault   = stderrors.New("qso insert boom")
	pt5UploadFault   = stderrors.New("upload insert boom")
	pt5RollbackFault = stderrors.New("rollback boom")
)

// TestSubmitImportBatch_UncertainRollbackAbortsWithoutFallback (PT-5, W-0008). When a
// batched write fails, the import rolls back and drops to per-record replay. If the
// ROLLBACK ITSELF fails, the batch transaction's disposition is unverified, and
// replaying could duplicate or misclassify rows — so the import must ABORT instead of
// entering the fallback, preserving both the write and rollback causes. The two
// failure branches are independent: the QSO insert, and the upload-queue insert. Both
// are driven here against a sqlmock driver (the only way to fail a write AND a rollback
// deterministically and independently), sharing one set of assertions.
//
// The seam is a sqlmock *sql.DB behind a Service (sqlite.NewServiceWithDB, export_test
// only). The test lives in package sqlite_test so it can import qsoservice.
func TestSubmitImportBatch_UncertainRollbackAbortsWithoutFallback(t *testing.T) {
	cases := []struct {
		name       string
		forwarders []types.ForwarderConfig
		forwardTo  []string
		trigger    error // the write error that triggers the (failing) rollback
		expect     func(mock sqlmock.Sqlmock)
	}{
		{
			name:      "batched QSO insert failure",
			forwardTo: nil,
			trigger:   pt5InsertFault,
			expect: func(mock sqlmock.Sqlmock) {
				expectPreTx(mock)
				mock.ExpectBegin()
				mock.ExpectQuery("(?i)insert into .?qso").WillReturnError(pt5InsertFault)
				mock.ExpectRollback().WillReturnError(pt5RollbackFault)
			},
		},
		{
			name:       "upload-queue insert failure",
			forwarders: []types.ForwarderConfig{{Name: "cloud", Type: "smcloud", Enabled: true, ActionFilter: []string{"insert"}}},
			forwardTo:  []string{"cloud"},
			trigger:    pt5UploadFault,
			expect: func(mock sqlmock.Sqlmock) {
				expectPreTx(mock)
				mock.ExpectBegin()
				// The QSO insert SUCCEEDS (INSERT ... RETURNING) and the parent-liveness
				// check passes; the UPLOAD-QUEUE insert then fails.
				mock.ExpectQuery("(?i)insert into .?qso").WillReturnRows(qsoInsertReturning())
				mock.ExpectQuery("(?i)select exists").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				mock.ExpectExec("(?i)insert into .?qso_upload").WillReturnError(pt5UploadFault)
				mock.ExpectRollback().WillReturnError(pt5RollbackFault)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			hub := events.NewHub()
			t.Cleanup(hub.Close)

			var logbuf strings.Builder
			svc := &qsoservice.Service{
				DB:     sqlite.NewServiceWithDB(db),
				Logger: logging.NewForWriter(&logbuf),
				Config: newImportConfig(t, tc.forwarders...),
				Hub:    hub,
			}

			tc.expect(mock)

			rec := adif.Record{
				ContactedStation: types.ContactedStation{Call: "K2BBB"},
				QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1300"},
				LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
			}

			_, err = svc.SubmitImportBatch(context.Background(), 1, []adif.Record{rec}, tc.forwardTo, 10, nil)

			// The import aborts, preserving BOTH causes.
			require.Error(t, err, "an uncertain rollback must abort the import, not silently fall back")
			require.ErrorIs(t, err, tc.trigger, "the triggering write error must remain diagnosable")
			require.ErrorIs(t, err, pt5RollbackFault, "the rollback error must remain diagnosable")

			// No fallback replay: it would issue further DB calls sqlmock never expected
			// (a sqlmock 'unexpected' error) and would log its trigger.
			require.NotContains(t, err.Error(), "unexpected",
				"no unexpected DB call — the per-record fallback must not have run")
			require.NotContains(t, logbuf.String(), "falling back to per-record",
				"the fallback trigger must not be logged when the rollback is unverified")
			require.Contains(t, logbuf.String(), "write atomicity is unverified",
				"the unverified rollback itself must be logged")

			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// expectPreTx queues the two reads every one-record batch makes before Phase 2's
// transaction: the logbook-callsign lookup and the Phase-1 dedupe lookup (no match).
func expectPreTx(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT callsign FROM logbook").
		WillReturnRows(sqlmock.NewRows([]string{"callsign"}).AddRow("G0XYZ"))
	mock.ExpectQuery("dedupe_key").WillReturnError(sql.ErrNoRows)
}

// qsoInsertReturning is the row sqlboiler's qso Insert reads back via RETURNING — the
// DB-defaulted columns the adapter does NOT set (created_at + additional_data are set,
// so they are excluded), scanned by position.
func qsoInsertReturning() *sqlmock.Rows {
	now := time.Now().UTC()
	return sqlmock.NewRows([]string{"id", "modified_at", "deleted_at", "revision"}).
		AddRow(int64(1), now, nil, int64(0))
}

func newImportConfig(t *testing.T, forwarders ...types.ForwarderConfig) *config.Service {
	t.Helper()
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Forwarders = forwarders
	cfgSvc := config.New(cfg)
	require.NoError(t, cfgSvc.Initialize())
	return cfgSvc
}
