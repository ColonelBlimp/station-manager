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
		server.New(st, db, quiet, map[string]int64{"tok-e2e": tenant}, "test", 0).Handler())
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

// Reconcile's producer mapping for Diff B (docs/reviews/forwarding-logging-gaps.md
// F1). Covers BOTH distinct enqueue call sites, because reconcile shares
// EnqueueUploads/EnqueueDeleteUploads with the MANUAL backfill — the pair most
// easily collapsed onto one constant, and the pair whose confusion made "why is
// smcloud busy?" a day-bucketing exercise in the first place. `manual` is pinned
// from the other side in internal/api, so a single hard-coded value fails one of
// the two.
//
// Each half asserts the expected queue ROW AND ACTION exists BEFORE asserting its
// origin. Without that, a fixture that never reached the enqueue path would report
// origin "" and look like honest RED while proving nothing — the same masquerade a
// skipped test performs, and the reason this file's Postgres gate matters.
// reconcileOriginStack builds a local stack + reconciler against the fake cloud,
// plus a helper that reads the origin recorded on a QSO's upload row.
//
// The helper asserts the expected row AND action EXIST before returning an
// origin. Without that, a fixture that never reached the enqueue path would
// yield "" and read as honest RED while proving nothing — the same masquerade a
// skipped test performs, which is exactly what this file's Postgres gate hid
// until it was brought up.
func reconcileOriginStack(t *testing.T) (
	*qsoservice.Service, *sqlite.Service, *Reconciler, forwarding.Forwarder, int64,
	func(uuid string, act forwarding.Action) string,
) {
	t.Helper()
	cloud := newCloudStack(t) // skips without a dev Postgres (task db:pg:up)
	ctx := context.Background()

	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, cloud.URL)
	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	rec, err := NewReconciler(fc, lbID, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	fwd, err := New(fc)
	require.NoError(t, err)

	originFor := func(uuid string, act forwarding.Action) string {
		t.Helper()
		q, err := dbSvc.FetchQsoByUUIDIncludingDeletedWithContext(ctx, uuid)
		require.NoError(t, err)
		rows, err := dbSvc.FetchUploadsByQsoIDWithContext(ctx, q.ID)
		require.NoError(t, err)
		for _, r := range rows {
			if r.Action == act.String() {
				return r.Origin
			}
		}
		t.Fatalf("no %s upload row for %s — the fixture never reached the enqueue path, "+
			"so any origin assertion would prove nothing", act, uuid)
		return ""
	}
	return qsoSvc, dbSvc, rec, fwd, lbID, originFor
}

// Reconcile's FIRST enqueue call site (EnqueueUploads, cloud-missing repair) for
// Diff B. Split from the delete case so BOTH call sites are independently
// demonstrable: with them in one test the first require would abort before the
// second ever ran, and only half the pair would ever be seen to fail.
//
// reconcile shares EnqueueUploads with the MANUAL backfill — pinned from the
// other side in internal/api — so a single hard-coded constant fails one of them.
func TestReconcile_UpsertRepairCarriesReconcileOrigin(t *testing.T) {
	ctx := context.Background()
	qsoSvc, _, rec, _, lbID, originFor := reconcileOriginStack(t)

	u1 := importQso(t, qsoSvc, lbID, "DL9UW", "120000")
	sum, err := rec.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, sum.EnqueuedUpserts, "fixture must reach the upsert enqueue: %+v", sum)

	require.Equal(t, "reconcile", originFor(u1, action.Insert),
		"a cloud-missing repair must be distinguishable from the manual backfill "+
			"that shares EnqueueUploads with it")
}

// Reconcile's SECOND enqueue call site (EnqueueDeleteUploads, missed-delete
// repair). Also re-proves contract 2's replace rule at this site: the local
// Delete already wrote a delete row with origin `edit`, which the reconcile
// repair re-enqueues over.
func TestReconcile_DeleteRepairCarriesReconcileOrigin(t *testing.T) {
	ctx := context.Background()
	qsoSvc, dbSvc, rec, fwd, lbID, originFor := reconcileOriginStack(t)

	u1 := importQso(t, qsoSvc, lbID, "DL9UW", "120000")
	_, err := rec.RunOnce(ctx)
	require.NoError(t, err)
	drainTo(t, fwd, dbSvc, u1, action.Insert)

	q1, err := dbSvc.FetchQsoByUUIDWithContext(ctx, u1)
	require.NoError(t, err)
	require.NoError(t, qsoSvc.Delete(ctx, q1, source.Source("test")))
	// The local Delete has now written a delete row with origin `edit` (pinned in
	// internal/api). Deliberately NOT asserted here: pre-implementation it is also
	// "", so asserting it would abort before the reconcile assertion below and this
	// call site would never be seen to fail on its own rule. Post-implementation the
	// edit -> reconcile transition is what contract 2's replace rule looks like at
	// this site.
	sum, err := rec.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, sum.EnqueuedDeletes, "fixture must reach the delete enqueue: %+v", sum)

	require.Equal(t, "reconcile", originFor(u1, action.Delete),
		"the delete repair must record reconcile, replacing the edit origin")
}
