package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0082 (W-0021 5B, replacing the slice 2B gate): a daemon whose ACTIVE
// archive holds no bindings starts no forwarder worker, no auth re-arm and no
// SM Cloud reconciler, whatever config.json's entries say — and rows queued
// under a name no binding carries are discarded loudly at start (ADR 0039's
// rule, per binding), never drained into the station's accounts.
func TestLifecycle_ArchiveWithoutBindingsStartsNoForwardingAtAll(t *testing.T) {
	const managed = "019fd5c5-efcc-7193-be4f-1fee532ee316"
	var managedPath string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		managedPath = filepath.Join(c.DataDir, "db", "qso-archives", managed+".db")
		c.QsoArchives = []types.QsoArchiveConfig{{ID: managed, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged}}
		c.ActiveQsoArchiveID = managed
		c.Forwarders = []types.ForwarderConfig{{
			Name: "stub-one", Type: stub.Type, Enabled: true,
			Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1,
		}}
	})
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	authQso, dataQso := seedFailedUploads(t, managedPath, "stub-one")
	raw, err := sql.Open("sqlite", "file:"+managedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, ?, 1)`, managed); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	_ = raw.Close()

	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	if d.smcloudRec != nil {
		t.Fatal("an SM Cloud reconciler was constructed in an archive with no bindings")
	}
	if started, _ := workerNamesStarted(t, filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log")); len(started) != 0 {
		t.Fatalf("workers started = %v; want none", started)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range []int64{authQso, dataQso} {
		rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, id)
		if err != nil || len(rows) != 0 {
			t.Fatalf("qso %d rows after start = %d (%v); want 0 — a name no binding carries is discarded", id, len(rows), err)
		}
	}
}
