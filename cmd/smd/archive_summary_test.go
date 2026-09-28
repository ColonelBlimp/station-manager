package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
)

// ADR 0084 slice 2a: on a CLEAN shutdown, after the writers have drained, the
// daemon recounts the active archive, checkpoints it (TRUNCATE), closes it, and
// only then takes the file's signature and merges the summary into the sidecar —
// so the next start, seeing the archive inactive, reads it as current. A QSO
// written while running must be in the count (the recount is final), and no WAL
// may survive (the signature is of the finished file).
func TestShutdown_RecordsTheActiveArchiveSummaryAfterTheClose(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})
	seedStationQueue(t, logDB) // logbook 1 "L" / G4ABC, three QSOs
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	// A fourth QSO while the daemon runs: only a final recount sees it.
	db, err := sql.Open("sqlite", "file:"+logDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO qso
		(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
		 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		VALUES (4,'01920000-0000-7000-8000-000000000004','JA1ABC','40m','SSB',7050000,
		        '20250508','0900','0900','59','59','Test',?,1)`, fmt.Sprintf("%064d", 4)); err != nil {
		t.Fatalf("insert while running: %v", err)
	}
	_ = db.Close()

	d.cleanShutdown = true
	orch.Shutdown(5*time.Second, nil)

	snap := d.cfgSvc.Snapshot()
	sums, miss := archive.ReadSummaries(archive.SummariesPath(snap))
	if miss != "" {
		t.Fatalf("sidecar: miss %q", miss)
	}
	got, ok := sums[snap.ActiveQsoArchiveID]
	if !ok || len(got.Logbooks) != 1 {
		t.Fatalf("summary for %s = %+v (present %v); want the one logbook", snap.ActiveQsoArchiveID, got, ok)
	}
	if lb := got.Logbooks[0]; lb.Name != "L" || lb.Callsign != "G4ABC" || lb.QSOCount != 4 {
		t.Fatalf("logbook summary = %+v; want L / G4ABC / 4 QSOs (the running write included)", lb)
	}
	if _, err := os.Stat(logDB + "-wal"); err == nil {
		t.Fatal("a WAL survived the shutdown; the signature is not of the finished file")
	}
	now, err := archive.SignatureOf(logDB)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Current(now) {
		t.Fatalf("the recorded summary reads stale against the closed file (%+v vs %+v)", got.Signature, now)
	}
}

// A start-failure rollback or an unclean stop is not a clean close: nothing is
// recorded, and the next start rebuilds the active archive from the file.
func TestShutdown_OnlyACleanShutdownRecordsTheSummary(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})
	seedStationQueue(t, logDB)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	orch.Shutdown(5*time.Second, nil) // cleanShutdown left false
	if sums, _ := archive.ReadSummaries(archive.SummariesPath(d.cfgSvc.Snapshot())); len(sums) != 0 {
		t.Fatalf("an unclean stop recorded summaries: %+v", sums)
	}
}

// On open the daemon builds the active archive's summary in memory — the
// source 2b keeps current and the API serves.
func TestStart_BuildsTheActiveArchiveSummary(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})
	seedStationQueue(t, logDB)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { orch.Shutdown(5*time.Second, nil) })
	got := d.activeLogbooks
	if len(got) != 1 || got[0].Name != "L" || got[0].QSOCount != 3 {
		t.Fatalf("active summary after start = %+v; want L with 3 QSOs", got)
	}
}

// A checkpoint that does not complete leaves WAL frames the main file's signature
// does not cover, while the recount included them: the summary would be stamped
// current against a file that is not the whole archive. The database is still
// closed; no summary is recorded (review of slice 2a).
func TestShutdown_NoSummaryWhenTheCheckpointFails(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})
	seedStationQueue(t, logDB)
	orig := checkpointArchive
	checkpointArchive = func(context.Context, *sqlite.Service) error { return errors.New("busy") }
	t.Cleanup(func() { checkpointArchive = orig })
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	d.cleanShutdown = true
	orch.Shutdown(5*time.Second, nil)
	if sums, _ := archive.ReadSummaries(archive.SummariesPath(d.cfgSvc.Snapshot())); len(sums) != 0 {
		t.Fatalf("a summary was stamped after a failed checkpoint: %+v", sums)
	}
	if _, err := d.db.FetchAllLogbooksWithContext(context.Background()); err == nil {
		t.Fatal("the database was left open after a failed checkpoint")
	}
}

// stopLogDB works within the orchestrator's shutdown budget — the context it is
// handed — not a deadline of its own that could outlive it. A context already
// spent means no summary work: the database is closed, nothing recorded.
func TestStopLogDB_UsesTheShutdownContext(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})
	seedStationQueue(t, logDB)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { orch.Shutdown(5*time.Second, nil) })
	d.cleanShutdown = true
	spent, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.stopLogDB(spent); err != nil {
		t.Fatalf("stopLogDB: %v", err)
	}
	if sums, _ := archive.ReadSummaries(archive.SummariesPath(d.cfgSvc.Snapshot())); len(sums) != 0 {
		t.Fatalf("summary work ran on a spent shutdown context: %+v", sums)
	}
	if _, err := d.db.FetchAllLogbooksWithContext(context.Background()); err == nil {
		t.Fatal("the database was left open")
	}
}
