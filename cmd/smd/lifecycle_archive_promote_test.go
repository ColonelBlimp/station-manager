package main

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 slice 2D (ADR 0071): the daemon starts on the PENDING candidate and
// promotes it only once the whole database-dependent graph is up; a candidate
// that does not come up is recorded on its entry and the daemon starts again
// on the last-known-good active archive.

// promoteFixture is a station adopted in place ("Home") plus one managed
// archive the provisioner created ("Contest"), with Contest pending.
type promoteFixture struct {
	cfgSvc  *config.Service
	home    string // the adopted archive's id
	contest archive.CreateResult
	builds  int
}

func newPromoteFixture(t *testing.T, mut func(*config.Config)) *promoteFixture {
	t.Helper()
	f := &promoteFixture{cfgSvc: seedOrchestratedConfig(t, mut)}
	// Generation 0: an ordinary start adopts the station's file as "Home".
	d0, orch0 := buildOrchestratedDaemon(t, f.cfgSvc, f.cfgSvc.Snapshot())
	if err := orch0.Start(d0.workerCtx); err != nil {
		t.Fatalf("adopting start failed: %v", err)
	}
	f.home = f.cfgSvc.Snapshot().ActiveQsoArchiveID
	if f.home == "" {
		t.Fatal("the adopting start left no active archive")
	}
	d0.workerCancel()
	orch0.Shutdown(5*time.Second, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := archive.NewManager(f.cfgSvc, d0.logger, func(string) bool { return true })
	res, err := m.Create(ctx, archive.CreateRequest{RequestKey: "k-contest", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision Contest: %v", err)
	}
	f.contest = res
	if _, err := f.cfgSvc.Update(func(c *config.Config) error {
		c.PendingQsoArchiveID = res.Entry.ID
		return nil
	}); err != nil {
		t.Fatalf("set pending: %v", err)
	}
	return f
}

// build is the test generationBuilder: one fresh daemon per call on the
// selection startGenerations chose, exactly as run()'s builder does.
func (f *promoteFixture) build(t *testing.T) generationBuilder {
	return func(p archive.Paths) (*daemon, *orchestrator.Orchestrator, error) {
		f.builds++
		d, orch := buildOrchestratedDaemon(t, f.cfgSvc, f.cfgSvc.Snapshot())
		d.paths = p
		return d, orch, nil
	}
}

func (f *promoteFixture) effective(t *testing.T) archive.Paths {
	t.Helper()
	p, err := archive.ResolveEffective(f.cfgSvc.Snapshot())
	if err != nil {
		t.Fatalf("resolve effective: %v", err)
	}
	if !p.Candidate || p.Entry == nil || p.Entry.ID != f.contest.Entry.ID {
		t.Fatalf("effective selection = %+v, want the pending Contest candidate", p)
	}
	return p
}

func (f *promoteFixture) onDisk(t *testing.T) config.Config {
	t.Helper()
	c, err := config.Load(f.cfgSvc.Path)
	if err != nil {
		t.Fatalf("load config.json: %v", err)
	}
	return c
}

// A valid candidate: the daemon serves it, and after the graph is up the
// catalogue names it active with pending cleared — in memory and on disk.
func TestLifecycle_PendingCandidateIsPromotedOnceTheGraphIsUp(t *testing.T) {
	f := newPromoteFixture(t, nil)
	d, _, err := startGenerations(f.cfgSvc, f.effective(t), f.build(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if f.builds != 1 {
		t.Fatalf("builds = %d, want 1 (a candidate that comes up needs no fallback generation)", f.builds)
	}
	if d.paths.Entry == nil || d.paths.Entry.ID != f.contest.Entry.ID || d.paths.QSO != f.contest.Path {
		t.Fatalf("daemon serves %+v, want the Contest candidate at %s", d.paths, f.contest.Path)
	}
	for name, c := range map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(t)} {
		if c.ActiveQsoArchiveID != f.contest.Entry.ID || c.PendingQsoArchiveID != "" {
			t.Fatalf("%s: active=%q pending=%q, want active=Contest pending cleared", name, c.ActiveQsoArchiveID, c.PendingQsoArchiveID)
		}
		if e := c.QsoArchiveByID(f.contest.Entry.ID); e == nil || e.LastActivationError != "" {
			t.Fatalf("%s: Contest entry = %+v, want present with no activation error", name, e)
		}
	}
	if d.activationFailure != nil {
		t.Fatalf("a successful activation carries a failure: %v", d.activationFailure.Err)
	}
}

// A candidate whose file holds another archive's identity never serves: the
// failure is recorded on its entry, pending is cleared (so the next start is
// an ordinary one), and the daemon starts again on Home.
func TestLifecycle_CandidateThatCannotOpenFallsBackToLastKnownGood(t *testing.T) {
	f := newPromoteFixture(t, nil)
	raw, err := sql.Open("sqlite", "file:"+f.contest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE archive_metadata SET archive_uuid = '019fd5c5-efcc-7193-be4f-1fee532ee3ff'`); err != nil {
		t.Fatalf("relabel candidate: %v", err)
	}
	_ = raw.Close()

	d, _, err := startGenerations(f.cfgSvc, f.effective(t), f.build(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if f.builds != 2 {
		t.Fatalf("builds = %d, want 2 (the candidate generation, then the last-known-good one)", f.builds)
	}
	if d.paths.Candidate || d.paths.Entry == nil || d.paths.Entry.ID != f.home {
		t.Fatalf("daemon serves %+v, want Home", d.paths)
	}
	if d.activationFailure == nil || d.activationFailure.Candidate.ID != f.contest.Entry.ID {
		t.Fatalf("activation failure = %+v, want the Contest candidate named", d.activationFailure)
	}
	for name, c := range map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(t)} {
		if c.ActiveQsoArchiveID != f.home || c.PendingQsoArchiveID != "" {
			t.Fatalf("%s: active=%q pending=%q, want active=Home pending cleared", name, c.ActiveQsoArchiveID, c.PendingQsoArchiveID)
		}
		e := c.QsoArchiveByID(f.contest.Entry.ID)
		if e == nil || e.LastActivationError != archive.FailIdentityMismatch {
			t.Fatalf("%s: Contest entry = %+v, want the stable code %q on the entry (never the error chain)", name, e, archive.FailIdentityMismatch)
		}
	}
}

// The candidate comes up but its promotion cannot be written: the generation
// is rolled back and Home serves. The record is memory-first: this session's
// catalogue is consistent even though config.json could not be touched.
func TestLifecycle_CandidateWhosePromotionCannotPersistFallsBack(t *testing.T) {
	f := newPromoteFixture(t, nil)
	etc := filepath.Dir(f.cfgSvc.Path)
	if err := os.Chmod(etc, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(etc, 0o700) })

	d, _, err := startGenerations(f.cfgSvc, f.effective(t), f.build(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if f.builds != 2 {
		t.Fatalf("builds = %d, want 2", f.builds)
	}
	if d.paths.Candidate || d.paths.Entry == nil || d.paths.Entry.ID != f.home {
		t.Fatalf("daemon serves %+v, want Home", d.paths)
	}
	c := f.cfgSvc.Snapshot()
	if c.ActiveQsoArchiveID != f.home || c.PendingQsoArchiveID != "" {
		t.Fatalf("memory: active=%q pending=%q, want active=Home pending cleared", c.ActiveQsoArchiveID, c.PendingQsoArchiveID)
	}
	e := c.QsoArchiveByID(f.contest.Entry.ID)
	if e == nil || e.LastActivationError != archive.FailPromotionPersist {
		t.Fatalf("Contest entry = %+v, want the promotion failure code %q", e, archive.FailPromotionPersist)
	}
	if d.activationFailure == nil || !strings.Contains(d.activationFailure.Err.Error(), "recording it in config.json failed") {
		t.Fatalf("activation failure = %v, want it to name the unpersisted record", d.activationFailure)
	}
	if err := os.Chmod(etc, 0o700); err != nil {
		t.Fatal(err)
	}
	if disk := f.onDisk(t); disk.PendingQsoArchiveID != f.contest.Entry.ID {
		t.Fatalf("disk: pending=%q; an unwritable config.json keeps the request, so the next start retries it", disk.PendingQsoArchiveID)
	}
}

// Without a candidate there is no second generation: an active archive that
// cannot open is a start failure, as before 2D.
func TestLifecycle_ActiveArchiveFailureHasNoFallbackGeneration(t *testing.T) {
	f := newPromoteFixture(t, nil)
	if _, err := f.cfgSvc.Update(func(c *config.Config) error {
		c.PendingQsoArchiveID = ""
		c.ActiveQsoArchiveID = f.contest.Entry.ID // the catalogue claims Contest; its file says so too …
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.contest.Path); err != nil { // … but the file is gone
		t.Fatal(err)
	}
	p, err := archive.ResolveEffective(f.cfgSvc.Snapshot())
	if err != nil || p.Candidate {
		t.Fatalf("effective = %+v (%v), want the active archive, no candidate", p, err)
	}
	if _, _, err := startGenerations(f.cfgSvc, p, f.build(t)); err == nil {
		t.Fatal("a start on a missing active archive succeeded")
	}
	if f.builds != 1 {
		t.Fatalf("builds = %d, want 1 (no fallback without a candidate)", f.builds)
	}
	if c := f.cfgSvc.Snapshot(); c.ActiveQsoArchiveID != f.contest.Entry.ID {
		t.Fatalf("active = %q; a non-candidate failure must not rewrite the catalogue", c.ActiveQsoArchiveID)
	}
}

// The candidate is the adopted archive itself (switching back to Home from
// Contest) with a forwarder enabled, so its generation runs live workers; the
// promotion cannot be written. The rollback must still complete — a Stop that
// only waited on the workers would never return — and Contest serves.
func TestLifecycle_LegacyCandidateWithWorkersRollsBackWithoutDeadlock(t *testing.T) {
	f := newPromoteFixture(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{{
			Name: "stub-one", Type: stub.Type, Enabled: true,
			Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1,
		}}
	})
	if _, err := f.cfgSvc.Update(func(c *config.Config) error {
		c.ActiveQsoArchiveID = f.contest.Entry.ID
		c.PendingQsoArchiveID = f.home
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	etc := filepath.Dir(f.cfgSvc.Path)
	if err := os.Chmod(etc, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(etc, 0o700) })
	p, err := archive.ResolveEffective(f.cfgSvc.Snapshot())
	if err != nil || !p.Candidate || p.Entry.ID != f.home {
		t.Fatalf("effective = %+v (%v), want the Home candidate", p, err)
	}

	type outcome struct {
		d   *daemon
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		d, _, err := startGenerations(f.cfgSvc, p, f.build(t))
		done <- outcome{d, err}
	}()
	var got outcome
	select {
	case got = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("startGenerations did not return: the candidate generation's rollback deadlocked")
	}
	if got.err != nil {
		t.Fatalf("start: %v", got.err)
	}
	if f.builds != 2 || got.d.paths.Entry == nil || got.d.paths.Entry.ID != f.contest.Entry.ID {
		t.Fatalf("builds=%d serving %+v, want 2 builds and Contest serving", f.builds, got.d.paths)
	}
	c := f.cfgSvc.Snapshot()
	if c.ActiveQsoArchiveID != f.contest.Entry.ID || c.PendingQsoArchiveID != "" {
		t.Fatalf("memory: active=%q pending=%q, want Contest active, pending cleared", c.ActiveQsoArchiveID, c.PendingQsoArchiveID)
	}
	if e := c.QsoArchiveByID(f.home); e == nil || e.LastActivationError != archive.FailPromotionPersist {
		t.Fatalf("Home entry = %+v, want the promotion failure code", e)
	}
}

// ---- Station Events (activation follow-up, 2026-09-24) ----

// notificationEvents polls the daemon's ACTIVE store for notification rows: the
// recorder writes asynchronously, so a just-recorded fact lands within a beat.
func notificationEvents(t *testing.T, d *daemon, want int) []types.OperatorEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows, err := d.db.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 10)
		if err != nil {
			t.Fatalf("fetch events: %v", err)
		}
		if len(rows) >= want || time.Now().After(deadline) {
			return rows
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// archiveEventRowsIn counts archive.* rows in a CLOSED archive file (read-only).
func archiveEventRowsIn(t *testing.T, path string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var n int
	if err := raw.QueryRow(`select count(*) from operator_event where kind like 'archive.%'`).Scan(&n); err != nil {
		t.Fatalf("count archive events in %s: %v", path, err)
	}
	return n
}

func detailOfEvent(t *testing.T, ev types.OperatorEvent) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(ev.Detail, &m); err != nil {
		t.Fatalf("detail %s: %v", ev.Detail, err)
	}
	return m
}

// A proven switch records archive.activated in the NEWLY ACTIVE archive's own
// file, with typed metadata only — and nothing in the archive it left.
func TestLifecycle_PromotionRecordsArchiveActivatedInTheNewArchive(t *testing.T) {
	f := newPromoteFixture(t, nil)
	d, _, err := startGenerations(f.cfgSvc, f.effective(t), f.build(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	rows := notificationEvents(t, d, 1)
	if len(rows) != 1 || rows[0].Kind != "archive.activated" || rows[0].Severity != "info" {
		t.Fatalf("events in the new archive = %+v, want one archive.activated (info)", rows)
	}
	det := detailOfEvent(t, rows[0])
	if det["archive_id"] != f.contest.Entry.ID || det["label"] != "Contest" || len(det) != 2 {
		t.Fatalf("detail = %v, want exactly {archive_id, label}", det)
	}
	home := f.cfgSvc.Snapshot().QsoArchiveByID(f.home)
	if n := archiveEventRowsIn(t, home.Path); n != 0 {
		t.Fatalf("the archive left behind holds %d archive event(s); the success belongs to the new one only", n)
	}
}

// A candidate that does not come up records archive.activation_failed in the
// RECOVERED (last-known-good) archive's file, with the stable code — never the
// error chain — and nothing in the candidate's file.
func TestLifecycle_FallbackRecordsActivationFailedInTheRecoveredArchive(t *testing.T) {
	f := newPromoteFixture(t, nil)
	raw, err := sql.Open("sqlite", "file:"+f.contest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE archive_metadata SET archive_uuid = '019fd5c5-efcc-7193-be4f-1fee532ee3ff'`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	d, _, err := startGenerations(f.cfgSvc, f.effective(t), f.build(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	rows := notificationEvents(t, d, 1)
	if len(rows) != 1 || rows[0].Kind != "archive.activation_failed" || rows[0].Severity != "error" {
		t.Fatalf("events in the recovered archive = %+v, want one archive.activation_failed (error)", rows)
	}
	det := detailOfEvent(t, rows[0])
	if det["archive_id"] != f.contest.Entry.ID || det["label"] != "Contest" || det["code"] != archive.FailIdentityMismatch || len(det) != 3 {
		t.Fatalf("detail = %v, want exactly {archive_id, label, code=%s}", det, archive.FailIdentityMismatch)
	}
	if n := archiveEventRowsIn(t, f.contest.Path); n != 0 {
		t.Fatalf("the candidate's file holds %d archive event(s); the failure belongs to the recovered archive", n)
	}
}

// Drill 5 on the station: the candidate's file vanishes between the accepted
// request and the restart. The start reports it as MISSING — the stable code on
// the entry and on the event, not "no identity".
func TestLifecycle_CandidateFileMissingAtStartIsReportedAsMissing(t *testing.T) {
	f := newPromoteFixture(t, nil)
	first := true
	build := func(p archive.Paths) (*daemon, *orchestrator.Orchestrator, error) {
		if first {
			first = false
			if err := os.Remove(f.contest.Path); err != nil {
				t.Fatal(err)
			}
		}
		return f.build(t)(p)
	}
	d, _, err := startGenerations(f.cfgSvc, f.effective(t), build)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if e := f.cfgSvc.Snapshot().QsoArchiveByID(f.contest.Entry.ID); e.LastActivationError != archive.FailFileMissing {
		t.Fatalf("entry code = %q, want %q", e.LastActivationError, archive.FailFileMissing)
	}
	rows := notificationEvents(t, d, 1)
	if len(rows) != 1 || detailOfEvent(t, rows[0])["code"] != archive.FailFileMissing {
		t.Fatalf("events = %+v, want one archive.activation_failed with code %s", rows, archive.FailFileMissing)
	}
	// The listing says it plainly, without the path.
	views := d.archives.List()
	for _, v := range views {
		if v.ID == f.contest.Entry.ID {
			if v.LastActivationCode != archive.FailFileMissing || v.LastActivationError != archive.FailureMessage(archive.FailFileMissing) || strings.Contains(v.LastActivationError, f.contest.Path) {
				t.Fatalf("view = %+v", v)
			}
		}
	}
}

// An expected refusal — the file is missing at the preflight — is not an event.
func TestLifecycle_RefusedActivationRecordsNoEvent(t *testing.T) {
	t.Setenv("SM_SELF_RESTART", "1")
	d, orch := newOrchestratedDaemon(t, nil)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx := context.Background()
	res, err := d.archives.Create(ctx, archive.CreateRequest{RequestKey: "k", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(res.Path); err != nil {
		t.Fatal(err)
	}
	_, err = d.archives.Activate(ctx, res.Entry.ID)
	var re *archive.RequestError
	if !stderr.As(err, &re) || re.Code != archive.FailFileMissing || re.Message != archive.FailureMessage(archive.FailFileMissing) {
		t.Fatalf("activate = %v, want the plain %s refusal", err, archive.FailFileMissing)
	}
	if rows := notificationEvents(t, d, 1); len(rows) != 0 {
		t.Fatalf("a refused activation recorded %+v; expected refusals are not events", rows)
	}
	if e := d.cfgSvc.Snapshot().QsoArchiveByID(res.Entry.ID); e.LastActivationError != "" {
		t.Fatalf("a refusal marked the entry: %q", e.LastActivationError)
	}
}
