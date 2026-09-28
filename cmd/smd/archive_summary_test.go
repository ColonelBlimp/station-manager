package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/types"
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
	if lb := got.Logbooks[0]; lb.Name != "L" || lb.Callsign != "G4ABC" || lb.QsoCount != 4 {
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

// On open the daemon builds the active archive's summary in memory, keyed by the
// file's own archive id — the source the API serves and 2b keeps current.
func TestStart_BuildsTheActiveArchiveSummary(t *testing.T) {
	d, orch := startedSummaryDaemon(t)
	_ = orch
	id, got, status := d.activeSummary.Snapshot()
	if id != d.cfgSvc.Snapshot().ActiveQsoArchiveID || status != archive.ContentsCurrent ||
		len(got) != 1 || got[0].Name != "L" || got[0].QsoCount != 3 {
		t.Fatalf("active summary after start = %s %+v %s; want the active archive, L with 3 QSOs, current", id, got, status)
	}
}

func startedSummaryDaemon(t *testing.T) (*daemon, *orchestrator.Orchestrator) {
	t.Helper()
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "G4ABC"
	})
	seedStationQueue(t, logDB) // logbook 1 "L" / G4ABC, three QSOs
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { orch.Shutdown(5*time.Second, nil) })
	return d, orch
}

func submitQso(t *testing.T, d *daemon, call string) {
	t.Helper()
	res, err := d.qso.Submit(context.Background(), 1, adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.200", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G4ABC"},
	}, false)
	if err != nil || res.Status != "stored" {
		t.Fatalf("submit %s: %+v %v", call, res, err)
	}
}

// ADR 0084 slice 2b, end to end: a QSO stored through the daemon reaches the
// active archive's summary — and GET /v1/qso-archives — without a restart.
func TestStart_TheActiveSummaryFollowsQsoWrites(t *testing.T) {
	d, _ := startedSummaryDaemon(t)
	submitQso(t, d, "K1ABC")
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, lbs, status := d.activeSummary.Snapshot()
		if status == archive.ContentsCurrent && len(lbs) == 1 && lbs[0].QsoCount == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("summary never reached 4 QSOs, current: %+v %s", lbs, status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, v := range d.archives.List() {
		if v.State == "active" && (v.ContentsStatus != archive.ContentsCurrent || len(v.Logbooks) != 1 || v.Logbooks[0].QsoCount != 4) {
			t.Fatalf("the list's active archive = %+v; want 4 QSOs, current", v)
		}
	}
}

// blockingSource holds every recount until released: a summary worker stuck on
// a slow database.
type blockingSource struct {
	archive.SummarySource
	release chan struct{}
}

func (b blockingSource) FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error) {
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.SummarySource.FetchAllLogbooksWithContext(ctx)
}

// The QSO path's guarantee (ADR 0084): a stuck summary recount never delays a
// QSO being stored. The summary reads stale meanwhile.
func TestStart_AStuckRecountNeverDelaysAQso(t *testing.T) {
	release := make(chan struct{})
	orig := summarySource
	summarySource = func(d *daemon) archive.SummarySource { return blockingSource{SummarySource: d.db, release: release} }
	t.Cleanup(func() { summarySource = orig; close(release) })
	d, _ := startedSummaryDaemon(t)

	done := make(chan struct{})
	go func() {
		submitQso(t, d, "K1ABC")
		submitQso(t, d, "K2ABC")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("storing a QSO waited on a stuck summary recount")
	}
	if _, _, status := d.activeSummary.Snapshot(); status != archive.ContentsStale {
		t.Fatalf("status while the recount is stuck = %s, want stale", status)
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

// Acceptance criterion 7 at startup: a corrupt sidecar does not stop the daemon;
// smd.log says so once, and the active archive is still summarised live.
func TestStart_ACorruptSidecarIsLoggedOnceAndIgnored(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "G4ABC"
	})
	seedStationQueue(t, logDB)
	sidecar := archive.SummariesPath(d.cfgSvc.Snapshot())
	if err := os.MkdirAll(filepath.Dir(sidecar), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecar, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("a corrupt sidecar stopped the start: %v", err)
	}
	t.Cleanup(func() { orch.Shutdown(5*time.Second, nil) })
	d.archives.List()
	d.archives.List() // every list reads the sidecar; none may repeat the diagnostic
	raw, err := os.ReadFile(filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "archive summaries file could not be used"); n != 1 {
		t.Fatalf("the corrupt sidecar was reported %d times, want once:\n%s", n, raw)
	}
	if _, lbs, status := d.activeSummary.Snapshot(); status != archive.ContentsCurrent || len(lbs) != 1 {
		t.Fatalf("active summary = %+v %s; want built live, current", lbs, status)
	}
}
