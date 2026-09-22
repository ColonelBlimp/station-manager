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

// W-0021 slice 2B: a daemon whose ACTIVE archive is not the adopted one starts
// no forwarder worker, no auth re-arm and no SM Cloud reconciler. Observable
// proof: a credential-rejected row that the re-arm WOULD have returned to
// pending stays failed, no reconciler is constructed, and the QSO service
// reports the gate.
func TestLifecycle_GatedArchiveStartsNoForwardingAtAll(t *testing.T) {
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
	// The managed file: migrated, one failed auth row for the enabled forwarder,
	// and the catalogue's identity (what the provisioner writes).
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	authQso, _ := seedFailedUploads(t, managedPath, "stub-one")
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
	if d.qso.ForwardingAdmitted() {
		t.Fatal("QSO service reports forwarding admitted in a managed archive")
	}
	if d.smcloudRec != nil {
		t.Fatal("an SM Cloud reconciler was constructed in a gated archive")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, authQso)
	if err != nil || len(rows) != 1 {
		t.Fatalf("fetch auth row: %v (%d rows)", err, len(rows))
	}
	if rows[0].Status != "failed" {
		t.Fatalf("auth row status = %q; the boot re-arm ran in a gated archive", rows[0].Status)
	}
}
