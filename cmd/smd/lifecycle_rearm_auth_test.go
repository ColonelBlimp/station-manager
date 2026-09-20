package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/golang-migrate/migrate/v4"
)

// seedFailedUploads migrates the daemon's log database file ahead of orch.Start
// and plants two failed insert rows for forwarder fwd: one of class auth, one
// unclassified. Returns the two QSO ids. Plain SQL, like the migration tests: a
// prior daemon run is what would have written these, and the test must not
// depend on the very re-arm path it proves.
func seedFailedUploads(t *testing.T, logDB, fwd string) (authQso, dataQso int64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+logDB+"?_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer func() { _ = db.Close() }()

	src, drv, err := sqlite.GetMigrationDrivers(db, sqlite.MigrationSetLog)
	if err != nil {
		t.Fatalf("migration drivers: %v", err)
	}
	defer func() { _ = src.Close() }()
	m, err := migrate.NewWithInstance("iofs", src, sqlite.SqliteDriver, drv)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO logbook (id, callsign, name) VALUES (1,'G4ABC','L')`); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}
	seedQso := func(id int64, uuid, call string) {
		if _, err := db.Exec(`INSERT INTO qso
			(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
			 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?,?,?,'40m','SSB',7050000,'20250508','0845','0845','59','59','Test',?,1)`,
			id, uuid, call, fmt.Sprintf("%064d", id)); err != nil {
			t.Fatalf("seed qso %d: %v", id, err)
		}
	}
	seedQso(1, "01920000-0000-7000-8000-000000000001", "M0CMC")
	seedQso(2, "01920000-0000-7000-8000-000000000002", "EA1B")
	seedUpload := func(qsoID int64, class any) {
		if _, err := db.Exec(`INSERT INTO qso_upload
			(qso_id, forwarder_name, forwarder_type, action, status, origin, attempts, last_error, failure_class)
			VALUES (?, ?, ?, 'insert', 'failed', 'live', 1, 'rejected', ?)`,
			qsoID, fwd, stub.Type, class); err != nil {
			t.Fatalf("seed upload for qso %d: %v", qsoID, err)
		}
	}
	seedUpload(1, "auth")
	seedUpload(2, nil)
	return 1, 2
}

// W-0010 outcome 9, ruling (b): the workers node re-arms an enabled forwarder's
// auth-class failed rows at start, and only those — the unclassified row (the
// preserved fixture's shape) stays failed.
func TestLifecycle_StartRearmsAuthFailedRowsForEnabledForwarder(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.Forwarders = []types.ForwarderConfig{{
			Name: "stub-one", Type: stub.Type, Enabled: true,
			Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1,
		}}
	})
	authQso, dataQso := seedFailedUploads(t, logDB, "stub-one")

	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	auth, err := d.db.FetchUploadsByQsoIDWithContext(ctx, authQso)
	if err != nil || len(auth) != 1 {
		t.Fatalf("fetch auth row: %v (%d rows)", err, len(auth))
	}
	// The stub worker may already have claimed or uploaded the re-armed row;
	// what must be true is that it is no longer a failed row of class auth.
	if auth[0].Status == "failed" || auth[0].FailureClass != "" {
		t.Fatalf("auth row after start = status %q class %q; want re-armed (not failed, class cleared)",
			auth[0].Status, auth[0].FailureClass)
	}
	data, err := d.db.FetchUploadsByQsoIDWithContext(ctx, dataQso)
	if err != nil || len(data) != 1 {
		t.Fatalf("fetch data row: %v (%d rows)", err, len(data))
	}
	if data[0].Status != "failed" || data[0].Attempts != 1 {
		t.Fatalf("unclassified row after start = status %q attempts %d; want failed, untouched",
			data[0].Status, data[0].Attempts)
	}
}
