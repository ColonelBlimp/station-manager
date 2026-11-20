package main

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/testbarrier"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0029 part 2, group E, the third step of the current lifetime's exit: FT8
   completion logging already earned finishes before the exit is stored. Through
   a real daemon: the real completion sink and its tracked jobs, the real
   archive with an enabled insert route, and the exit's real route.

   What each reads is what is STORED: the QSO row and its upload row, or their
   absence. A job seen to finish is not a contact stored.
*/

// newExitFt8Bed is a started daemon with an enabled insert route, and lifetime
// A admitted to it.
func newExitFt8Bed(t *testing.T) (*orderBed, int64) {
	t.Helper()
	// newFt8LogDaemon's daemon, started with its socket directory made
	// owner-only first, so that its app listener serves the exit's route.
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: stub.Type, Enabled: true,
			Credentials: stubCreds(t), ActionFilter: []string{"insert"}, TickIntervalSec: 3600, BatchSize: 1}}
	})
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { stopGen(t, orch) })
	// No lookup leaves the process: the chain is one provider that is never asked.
	d.enrich = &lookup.Orchestrator{DB: d.refDB, LogDB: d.db, Logger: d.logger,
		Chain: []lookup.CallsignProvider{&heldProvider{}}}
	b := &orderBed{d: d, archive: d.cfgSvc.Snapshot().ActiveQsoArchiveID}
	b.mustAdmit(t, orderLifeA)
	gen, _ := b.session(t, orderLifeA)
	return b, gen
}

// earned hands the sink a completed exchange with call, as the sequencer does.
func (b *orderBed) earned(call string) {
	b.d.ft8QsoLogger()(context.Background(), ft8.CompletedQso{Mode: "FT8", LogbookID: 1, TheirCall: call,
		HasOurReport: true, OurReport: -10, HasTheirReport: true, TheirReport: -12,
		StartedAt: time.Now().UTC().Add(-time.Minute), DialFreqMHz: 14.074})
}

// holdArchiveWrites takes the archive's write lock on another connection: a
// completion's write, and the exit's own commit, then wait at it.
func (b *orderBed) holdArchiveWrites(t *testing.T) (release func()) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+b.d.paths.QSO)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	done := false
	release = func() {
		if done {
			return
		}
		done = true
		_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		_ = conn.Close()
		_ = db.Close()
	}
	t.Cleanup(release)
	return release
}

func (b *orderBed) archiveExec(t *testing.T, q string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+b.d.paths.QSO+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// drainWatch begins the watch for an exit that will wait for the tracked
// completion jobs in the daemon's drain. It is taken BEFORE the exit is sent.
func drainWatch(t *testing.T) testbarrier.Watch {
	t.Helper()
	return testbarrier.Expect(t, testbarrier.AtWaitGroup, "(*daemon).drainFt8Completions")
}

// inDrain: the exit behind ch, sent after w was taken, is the one goroutine
// parked in the drain, and has answered nothing.
func inDrain(w testbarrier.Watch, what string, ch chan httpResult) {
	testbarrier.Waiting(w, what, ch)
}

// The exit waits for a tracked completion job, whatever the job is waiting
// for: here one that the test alone releases, launched as the sink launches
// its own. An exit that did not wait would be stored at once.
func TestExit_WaitsForATrackedFt8CompletionJob(t *testing.T) {
	b, gen := newExitFt8Bed(t)
	gate := make(chan struct{})
	released := false
	letGo := func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(letGo)
	launchFt8QsoLog(&b.d.qsoLogWG, nil, func(context.Context) { <-gate })

	w := drainWatch(t)
	exit := async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) })
	inDrain(w, "the exit, with a completion job still running", exit)
	if b.exitRecorded(t, orderLifeA) {
		t.Fatal("the exit was stored while a completion job was still running")
	}
	letGo()
	if r := answered(t, "the exit", exit); r.code != http.StatusOK || !b.exitRecorded(t, orderLifeA) {
		t.Fatalf("the exit = %d %s, recorded %v; want 200 and recorded", r.code, r.body, b.exitRecorded(t, orderLifeA))
	}
}

// A contact earned before the exit is stored, with its upload row, by the time
// the exit answers. The contact's write is held at the archive's write lock
// when the exit is asked for.
func TestExit_AnEarnedFt8ContactIsStoredWhenTheExitAnswers(t *testing.T) {
	b, gen := newExitFt8Bed(t)
	release := b.holdArchiveWrites(t)
	b.earned("JA1XYZ") // its job waits at the write lock
	w := drainWatch(t)
	exit := async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) })
	inDrain(w, "the exit, with an earned contact not yet stored", exit)
	release()
	if r := answered(t, "the exit", exit); r.code != http.StatusOK {
		t.Fatalf("the exit = %d %s; want 200", r.code, r.body)
	}
	// Read at once, with no wait: the exit has answered, so the contact is stored.
	ftModes[0].stored(t, b.d, "JA1XYZ")
	if !b.exitRecorded(t, orderLifeA) {
		t.Fatal("the exit is not recorded")
	}
}

// A completion whose write fails stores nothing and does not block the exit.
func TestExit_AFailedFt8CompletionWriteDoesNotBlockTheExit(t *testing.T) {
	b, gen := newExitFt8Bed(t)
	b.archiveExec(t, `CREATE TRIGGER fail_qso BEFORE INSERT ON qso BEGIN SELECT RAISE(ABORT, 'forced: the contact cannot be stored'); END`)
	b.earned("JA1XYZ")
	r := within(t, "the exit after a failed completion write", async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) }))
	if r.code != http.StatusOK || !b.exitRecorded(t, orderLifeA) {
		t.Fatalf("the exit = %d %s, recorded %v; want 200 and recorded", r.code, r.body, b.exitRecorded(t, orderLifeA))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	qsos, err := b.d.db.FetchQsoSliceByLogbookIdWithContext(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(qsos) != 0 {
		t.Fatalf("%d QSO(s) stored by a completion whose write failed", len(qsos))
	}
}
