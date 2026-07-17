package smcloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/cloud/server"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The full S4 story against REAL parts on both sides: local sqlite +
// qsoservice, cloud internal/cloud/server + Postgres store. Empty cloud →
// first backfill enqueues; drained → in sync; local edit / missed delete →
// targeted heal rows. Skips without the dev Postgres (task db:pg:up).

// newLocalStack builds the daemon-side harness (mirrors qsoservice's
// newTestService) with one enabled smcloud forwarder pointed at cloudURL.
func newLocalStack(t *testing.T, cloudURL string) (*qsoservice.Service, *sqlite.Service, *logging.Service, types.ForwarderConfig) {
	t.Helper()
	creds, err := json.Marshal(map[string]string{"url": cloudURL, "token": "tok-e2e", "logbook": "main"})
	require.NoError(t, err)
	fc := types.ForwarderConfig{
		Name: "smcloud", Type: Type, Enabled: true,
		ActionFilter: []string{"insert", "update", "delete"},
		Credentials:  creds,
	}

	// A temp-FILE database, not :memory: — the shared-cache in-memory DB is
	// process-wide, and the restore test runs TWO local stacks ("machines")
	// that must be genuinely separate databases.
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "log.db")
	cfg := config.DefaultConfig(workDir)
	cfg.Datastore.Path = dbPath
	cfg.Forwarders = []types.ForwarderConfig{fc}
	cfgSvc := config.New(cfg)
	require.NoError(t, cfgSvc.Initialize())

	logSvc := &logging.Service{}
	logSvc.ConfigService = cfgSvc
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	require.NoError(t, logSvc.Initialize())

	dbSvc := &sqlite.Service{}
	dbSvc.ConfigService = cfgSvc
	dbSvc.LoggerService = logSvc
	require.NoError(t, dbSvc.Initialize())
	dbSvc.DatabaseConfig = &types.DatastoreConfig{
		Driver: "sqlite", Path: dbPath,
		MaxOpenConns: 1, MaxIdleConns: 1,
		ContextTimeout: 10, TransactionContextTimeout: 10,
	}
	require.NoError(t, dbSvc.Open())
	require.NoError(t, dbSvc.Migrate())

	hub := events.NewHub()
	t.Cleanup(func() {
		hub.Close()
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})
	return &qsoservice.Service{DB: dbSvc, Logger: logSvc, Config: cfgSvc, Hub: hub}, dbSvc, logSvc, fc
}

// newCloudStack stands up the real cloud service over Postgres (same
// skip-gate + advisory lock as the other smcloud suites).
func newCloudStack(t *testing.T) *httptest.Server {
	t.Helper()
	dsn := os.Getenv("SMCLOUD_TEST_DSN")
	if dsn == "" {
		dsn = roundtripTestDSN
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("needs a dev Postgres (task db:pg:up): open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("needs a dev Postgres (task db:pg:up): ping: %v", err)
	}
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, 0x534d434c)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, 0x534d434c)
		_ = conn.Close()
	})

	drop := `DROP TABLE IF EXISTS qsos; DROP TABLE IF EXISTS logbooks;
DROP TABLE IF EXISTS tenants; DROP TABLE IF EXISTS schema_migrations`
	_, err = db.Exec(drop)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(db))
	t.Cleanup(func() {
		_, _ = db.Exec(drop)
		_ = db.Close()
	})

	st := store.New(db)
	tenant, err := st.EnsureTenant(context.Background(), "7Q5MLV", "")
	require.NoError(t, err)
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cloud := httptest.NewServer(
		server.New(st, db, quiet, map[string]int64{"tok-e2e": tenant}, "test").Handler())
	t.Cleanup(cloud.Close)
	return cloud
}

func importQso(t *testing.T, qsoSvc *qsoservice.Service, lbID int64, call, timeOn string) string {
	t.Helper()
	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260717", TimeOn: timeOn},
		LoggingStation:   types.LoggingStation{StationCallsign: "7Q5MLV"},
	}
	res, err := qsoSvc.SubmitImport(context.Background(), lbID, rec, false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
	return res.UUID
}

// drainTo pushes a QSO to the cloud via the real forwarder Submit — standing
// in for the worker's drain of the queue rows the reconciler creates.
func drainTo(t *testing.T, f forwarding.Forwarder, dbSvc *sqlite.Service, uuid string, act forwarding.Action) {
	t.Helper()
	var (
		qso types.Qso
		err error
	)
	if act == action.Delete {
		qso, err = dbSvc.FetchQsoByUUIDIncludingDeletedWithContext(context.Background(), uuid)
	} else {
		qso, err = dbSvc.FetchQsoByUUIDWithContext(context.Background(), uuid)
	}
	require.NoError(t, err)
	res := f.Submit(context.Background(), qso, act, "")
	require.Equal(t, forwarding.OutcomeSuccess, res.Outcome, "drain %s %s: %v", act, uuid, res.Err)
}

func TestReconciler_EndToEnd(t *testing.T) {
	cloud := newCloudStack(t)
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, cloud.URL)
	ctx := context.Background()

	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)

	rec, err := NewReconciler(fc, lbID, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	fwd, err := New(fc)
	require.NoError(t, err)

	u1 := importQso(t, qsoSvc, lbID, "DL9UW", "120000")
	u2 := importQso(t, qsoSvc, lbID, "9A4ZM", "120100")

	// 1. Empty cloud → the whole logbook is divergence (the first backfill).
	sum, err := rec.RunOnce(ctx)
	require.NoError(t, err)
	require.False(t, sum.InSync)
	require.EqualValues(t, 0, sum.CloudLogbookID, "logbook not on the cloud yet")
	require.Equal(t, 2, sum.LocalCount)
	require.Equal(t, 2, sum.EnqueuedUpserts)
	require.Equal(t, 0, sum.EnqueuedDeletes)

	// 2. Drain the queue (worker stand-in) → in sync.
	drainTo(t, fwd, dbSvc, u1, action.Insert)
	drainTo(t, fwd, dbSvc, u2, action.Insert)
	sum, err = rec.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, sum.InSync, "after drain: %+v", sum)
	require.Equal(t, 2, sum.CloudCount)

	// 3. Miss a delete: soft-delete u2 locally, don't push → reconcile queues
	//    exactly one tombstone and no upserts.
	q2, err := dbSvc.FetchQsoByUUIDWithContext(ctx, u2)
	require.NoError(t, err)
	require.NoError(t, qsoSvc.Delete(ctx, q2, source.Source("test")))
	sum, err = rec.RunOnce(ctx)
	require.NoError(t, err)
	require.False(t, sum.InSync)
	require.Equal(t, 1, sum.EnqueuedDeletes, "%+v", sum)
	require.Equal(t, 0, sum.EnqueuedUpserts, "%+v", sum)

	// 4. Drain the tombstone → in sync again (1 live row both sides).
	drainTo(t, fwd, dbSvc, u2, action.Delete)
	sum, err = rec.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, sum.InSync, "after tombstone drain: %+v", sum)
	require.Equal(t, 1, sum.LocalCount)
	require.Equal(t, 1, sum.CloudCount)
}
