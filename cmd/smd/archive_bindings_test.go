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

// ADR 0082 part 4 (W-0021 5B): a managed archive never seeds from config —
// its start records the empty decision (the marker) and leaves no binding.
// The adopted archive's seed lands with routing and workers by binding; until
// then its marker stays NULL and nothing is renamed (a rename now would strand
// rows under names no config-driven worker drains).

func TestLifecycle_ManagedArchiveStartRecordsAnEmptySeed(t *testing.T) {
	const managed = "019fd5c5-efcc-7193-be4f-1fee532ee318"
	var managedPath string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		managedPath = filepath.Join(c.DataDir, "db", "qso-archives", managed+".db")
		c.QsoArchives = []types.QsoArchiveConfig{{ID: managed, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged}}
		c.ActiveQsoArchiveID = managed
		c.Forwarders = []types.ForwarderConfig{{
			Name: "qrz", Type: stub.Type, Enabled: true, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1,
		}}
	})
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	seedFailedUploads(t, managedPath, "qrz")
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if at, err := d.db.DestinationBindingsSeededAtWithContext(ctx); err != nil || at == nil {
		t.Fatalf("marker = %v (%v); want set with no rows", at, err)
	}
	if rows, err := d.db.ListLogbookDestinationsWithContext(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("bindings = %+v (%v); want none in a managed archive", rows, err)
	}
}

func TestLifecycle_LegacyArchiveStartLeavesTheSeedUndecidedUntilRoutingFollowsBindings(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{
			Name: "qrz", Type: stub.Type, Enabled: true, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1,
		}}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if at, err := d.db.DestinationBindingsSeededAtWithContext(ctx); err != nil || at != nil {
		t.Fatalf("marker = %v (%v); want NULL on the adopted archive at this boundary", at, err)
	}
	if rows, err := d.db.ListLogbookDestinationsWithContext(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("bindings = %+v (%v); want none yet", rows, err)
	}
}
