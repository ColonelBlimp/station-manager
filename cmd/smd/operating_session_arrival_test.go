package main

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The arrival at an archive, through the daemon's real start (ADR 0100 §4;
// W-0029 part 3, criteria A). An activation has an identity. The start that
// processes it, applied or fallen back, records it in config.json as the
// activation in effect; the archive then served records it too, and a session
// still open there from an earlier visit is closed. Each sequence is run from
// the stores as an interruption would leave them, and judged on the rows of
// both archive files and on config.json, in memory and on disk.

const (
	lifeA = "0190dddd-0000-7000-8000-00000000000a"
	lifeB = "0190dddd-0000-7000-8000-00000000000b"
)

// arrivalFixture is promoteFixture's station (Home adopted and active, Contest
// provisioned) with nothing pending until a test asks for a switch.
type arrivalFixture struct {
	*promoteFixture
	t    *testing.T
	d    *daemon
	orch *orchestrator.Orchestrator
	home string // Home's file
}

func newArrivalFixture(t *testing.T) *arrivalFixture {
	t.Helper()
	p := newPromoteFixture(t, nil)
	f := &arrivalFixture{promoteFixture: p, t: t, home: p.cfgSvc.Snapshot().Datastore.Path}
	f.edit(func(c *config.Config) { c.PendingQsoArchiveID = "" }) // the tests ask for their own switches
	f.withDefaultLogbook(f.home)
	f.withDefaultLogbook(f.contest.Path)
	t.Cleanup(f.stop)
	return f
}

// withDefaultLogbook gives an archive file a default logbook, so that an app's
// admission opens a session in it and does not answer "setup pending".
func (f *arrivalFixture) withDefaultLogbook(path string) {
	f.t.Helper()
	db := raw(f.t, path)
	mustExec(f.t, db, `INSERT INTO logbook (callsign, name, uuid) SELECT 'G4ABC', 'Fixture', ? WHERE NOT EXISTS (SELECT 1 FROM logbook)`, utils.NewUUIDv7())
	mustExec(f.t, db, `UPDATE archive_metadata SET default_logbook_id = (SELECT MIN(id) FROM logbook) WHERE default_logbook_id IS NULL`)
}

// edit changes config.json as a request or an interruption would have left it.
func (f *arrivalFixture) edit(fn func(*config.Config)) {
	f.t.Helper()
	if _, err := f.cfgSvc.Update(func(c *config.Config) error { fn(c); return nil }); err != nil {
		f.t.Fatalf("edit config: %v", err)
	}
}

// request leaves config.json as an accepted switch request leaves it.
func (f *arrivalFixture) request(target, activation string) {
	f.t.Helper()
	f.edit(func(c *config.Config) {
		c.PendingQsoArchiveID = target
		c.PendingQsoArchiveActivationID = activation
	})
}

// start runs the daemon's real start on whatever config.json selects.
func (f *arrivalFixture) start() *daemon {
	f.t.Helper()
	f.stop()
	p, err := archive.ResolveEffective(f.cfgSvc.Snapshot())
	if err != nil {
		f.t.Fatalf("resolve effective: %v", err)
	}
	d, orch, err := startGenerations(f.cfgSvc, p, f.build(f.t))
	if err != nil {
		f.t.Fatalf("start: %v", err)
	}
	f.d, f.orch = d, orch
	return d
}

func (f *arrivalFixture) stop() {
	if f.d == nil {
		return
	}
	f.d.workerCancel()
	f.orch.Shutdown(5*time.Second, nil)
	f.d, f.orch = nil, nil
}

func (f *arrivalFixture) serving(d *daemon, want string) {
	f.t.Helper()
	if d.paths.Entry == nil || d.paths.Entry.ID != want {
		var why error
		if d.activationFailure != nil {
			why = d.activationFailure.Err
		}
		f.t.Fatalf("the daemon serves %s, want archive %s (activation failure: %v)", d.paths.QSO, want, why)
	}
}

// both is config.json in memory and on disk.
func (f *arrivalFixture) both() map[string]config.Config {
	f.t.Helper()
	return map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(f.t)}
}

func (f *arrivalFixture) wantInEffect(want string) {
	f.t.Helper()
	for name, c := range f.both() {
		if c.QsoArchiveActivationID != want || c.PendingQsoArchiveID != "" || c.PendingQsoArchiveActivationID != "" {
			f.t.Fatalf("%s: activation in effect %q, selector %q, pending identity %q; want %q in effect and nothing pending",
				name, c.QsoArchiveActivationID, c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID, want)
		}
	}
}

func raw(t *testing.T, path string) *sql.DB {
	t.Helper()
	// It waits for a writer: some of its statements run while a generation of
	// the daemon is starting on the same file, and without the wait one that
	// met another start node's write failed at once as busy.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// remnantIn puts an open session with one contact into an archive file, held
// by lifetime, as an earlier visit to that archive left it. The daemon is not
// running.
func (f *arrivalFixture) remnantIn(path, lifetime string) (session string) {
	f.t.Helper()
	if f.d != nil {
		f.t.Fatal("the fixture writes a remnant only while no daemon runs")
	}
	db := raw(f.t, path)
	session = utils.NewUUIDv7()
	mustExec(f.t, db, `INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at)
		SELECT ?, 'logbook-of-the-remnant', default_epoch, '2026-11-20 09:15:00' FROM archive_metadata WHERE singleton = 1`, session)
	mustExec(f.t, db, `INSERT INTO operating_session_interval (session_id, seq, started_at, end_state, app_lifetime)
		VALUES (?, 1, '2026-11-20 09:15:00', 'open', ?)`, session, lifetime)
	f.addContact(path, session)
	var open int
	if err := db.QueryRow(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`).Scan(&open); err != nil || open != 1 {
		f.t.Fatalf("the fixture: %d sessions open in %s (%v), want the 1 just written", open, path, err)
	}
	return session
}

// addContact stores one QSO in an archive file and makes it a member of the
// session, as a logged contact is. The daemon is not running.
func (f *arrivalFixture) addContact(path, session string) {
	f.t.Helper()
	if f.d != nil {
		f.t.Fatal("the fixture writes a contact only while no daemon runs")
	}
	db := raw(f.t, path)
	mustExec(f.t, db, `INSERT INTO qso (uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		SELECT ?, 'K1ABC', '40m', 'SSB', 7050000, '20261120', '0915', '0915', '59', '59', 'Test', ?, MIN(id) FROM logbook`,
		utils.NewUUIDv7(), fmt.Sprintf("%064s", session[:30]))
	mustExec(f.t, db, `INSERT INTO operating_session_member (qso_id, session_id) SELECT MAX(id), ? FROM qso`, session)
}

type sessionRecord struct {
	exists     bool
	closed     bool
	reason     string
	endState   string
	ended      bool
	generation int64
	// The last interval's own identity, and how many contacts the session holds:
	// two equal records mean the sitting was neither ended nor begun again.
	intervalSeq     int64
	intervalStarted string
	holder          string
	members         int
}

// record reads one session, its last interval, its members and the file's generation.
func record(t *testing.T, path, session string) sessionRecord {
	t.Helper()
	db := raw(t, path)
	var (
		r       sessionRecord
		closed  sql.NullTime
		reason  sql.NullString
		ended   sql.NullTime
		holder  sql.NullString
		started sql.NullString
	)
	if err := db.QueryRow(`SELECT session_generation FROM archive_metadata WHERE singleton = 1`).Scan(&r.generation); err != nil {
		t.Fatalf("generation of %s: %v", path, err)
	}
	err := db.QueryRow(`SELECT s.closed_at, s.close_reason, i.end_state, i.ended_at, i.seq, CAST(i.started_at AS TEXT), i.app_lifetime,
			(SELECT COUNT(*) FROM operating_session_member m WHERE m.session_id = s.id)
		FROM operating_session s JOIN operating_session_interval i ON i.session_id = s.id
		WHERE s.id = ? ORDER BY i.seq DESC LIMIT 1`, session).
		Scan(&closed, &reason, &r.endState, &ended, &r.intervalSeq, &started, &holder, &r.members)
	if err == sql.ErrNoRows {
		return r
	}
	if err != nil {
		t.Fatalf("session %s in %s: %v", session, path, err)
	}
	r.exists, r.closed, r.reason, r.ended = true, closed.Valid, reason.String, ended.Valid
	r.intervalStarted, r.holder = started.String, holder.String
	return r
}

func arrivedUnder(t *testing.T, path string) string {
	t.Helper()
	var s string
	if err := raw(t, path).QueryRow(`SELECT arrived_activation FROM archive_metadata WHERE singleton = 1`).Scan(&s); err != nil {
		t.Fatalf("arrived_activation of %s: %v", path, err)
	}
	return s
}

// wantArrived: the session is closed as an arrival closes it.
func wantArrived(t *testing.T, path, session string) {
	t.Helper()
	r := record(t, path, session)
	if !r.exists || !r.closed || r.reason != sqlite.SessionClosedBySwitch || r.endState != "unconfirmed" || r.ended {
		t.Fatalf("session %s = %+v; want it closed with %s, its interval's end unconfirmed with no time", session, r, sqlite.SessionClosedBySwitch)
	}
}

func wantStillOpen(t *testing.T, path, session, why string) sessionRecord {
	t.Helper()
	r := record(t, path, session)
	if !r.exists || r.closed || r.endState != "open" {
		t.Fatalf("session %s = %+v; want it still open: %s", session, r, why)
	}
	return r
}

// admit is the session step of an app's admission, as the API runs it.
func admit(t *testing.T, d *daemon, lifetime string) (sqlite.OperatingSessionState, sqlite.SessionOutcome) {
	t.Helper()
	st, outcome, err := d.db.OpenOperatingSessionWithContext(context.Background(), lifetime, time.Now())
	if err != nil {
		t.Fatalf("admission of %s: %v", lifetime, err)
	}
	return st, outcome
}

// wantNewSession: the app is given a new session and no question.
func wantNewSession(t *testing.T, d *daemon, lifetime, notThis string) string {
	t.Helper()
	st, outcome := admit(t, d, lifetime)
	if outcome != sqlite.SessionApplied || st.Open == nil || st.Open.ID == notThis || st.Open.Holder != lifetime {
		t.Fatalf("admission of %s: outcome %q, open %+v; want a new session held by it, not %s", lifetime, outcome, st.Open, notThis)
	}
	return st.Open.ID
}

// An applied switch: the start makes the activation final and records it in
// effect, in one update; the archive now served records it and its remnant is
// closed before any app is served; the first app start gets a new session with
// no question. The archive that was left is not touched.
func TestArrival_AnAppliedSwitchClosesTheRemnantOfTheArchiveArrivedAt(t *testing.T) {
	f := newArrivalFixture(t)
	old := f.remnantIn(f.contest.Path, lifeA)
	left := f.remnantIn(f.home, lifeA)
	x := utils.NewUUIDv7()
	f.request(f.contest.Entry.ID, x)

	d := f.start()

	f.serving(d, f.contest.Entry.ID)
	f.wantInEffect(x)
	wantArrived(t, f.contest.Path, old)
	if got := arrivedUnder(t, f.contest.Path); got != x {
		t.Fatalf("the archive arrived at records %q, want %q", got, x)
	}
	wantStillOpen(t, f.home, left, "the archive that was left is not served, so nothing of it changes")
	if got := arrivedUnder(t, f.home); got != "" {
		t.Fatalf("the archive that was left records %q; nothing of it is written", got)
	}
	wantNewSession(t, d, lifeA, old)
}

// An ordinary start is not an arrival: the same app, admitted again, holds the
// session it held.
func TestArrival_AnOrdinaryStartClosesNothing(t *testing.T) {
	f := newArrivalFixture(t)
	x := utils.NewUUIDv7()
	f.request(f.contest.Entry.ID, x)
	d := f.start()
	held := wantNewSession(t, d, lifeA, "")

	d = f.start() // nothing pending: an ordinary start on Contest

	f.serving(d, f.contest.Entry.ID)
	wantStillOpen(t, f.contest.Path, held, "an ordinary start is not an arrival")
	if st, outcome := admit(t, d, lifeA); outcome != sqlite.SessionUnchanged || st.Open == nil || st.Open.ID != held {
		t.Fatalf("after an ordinary start: outcome %q, open %+v; want %s still held", outcome, st.Open, held)
	}
}

// A promotion whose write did not survive, after the archive had served a
// session (codex-213): config.json reverts to "Home active, Contest pending"
// with the same identity, and the next start processes that activation again.
// Contest already records it: the session opened there is not closed, and its
// identity, the generation and its interval are as they were.
func TestArrival_TheSameActivationAppliedTwiceKeepsTheSessionOpenedInBetween(t *testing.T) {
	for name, identity := range map[string]string{
		"a selector with a stored identity":                       utils.NewUUIDv7(),
		"a selector written before identities, loaded twice over": "",
	} {
		t.Run(name, func(t *testing.T) {
			f := newArrivalFixture(t)
			old := f.remnantIn(f.contest.Path, lifeA)
			f.request(f.contest.Entry.ID, identity)
			worked := f.cfgSvc.Snapshot().PendingActivationIdentity()
			d := f.start()
			wantArrived(t, f.contest.Path, old)
			f.stop()
			t0 := f.remnantIn(f.contest.Path, lifeB) // the session Contest served, with a contact
			before := wantStillOpen(t, f.contest.Path, t0, "the fixture")
			if before.members != 1 || before.holder != lifeB {
				t.Fatalf("the fixture: T = %+v; want one contact, held by %s", before, lifeB)
			}

			// The power cut: the promotion is undone, the session's rows are not.
			f.edit(func(c *config.Config) {
				c.ActiveQsoArchiveID = f.promoteFixture.home
				c.PendingQsoArchiveID = f.contest.Entry.ID
				c.PendingQsoArchiveActivationID = identity
				c.QsoArchiveActivationID = ""
			})
			if again := f.cfgSvc.Snapshot().PendingActivationIdentity(); again != worked {
				t.Fatalf("the fixture: the repeated activation's identity is %q, the first was %q", again, worked)
			}

			d = f.start()

			f.serving(d, f.contest.Entry.ID)
			f.wantInEffect(worked)
			after := wantStillOpen(t, f.contest.Path, t0, "the same activation processed again is not another arrival")
			if after != before {
				t.Fatalf("the session changed across the replay: before %+v, after %+v", before, after)
			}
			if st, outcome := admit(t, d, lifeB); outcome != sqlite.SessionUnchanged || st.Open == nil || st.Open.ID != t0 || !st.Open.HasMembers {
				t.Fatalf("the same app after the replay: outcome %q, open %+v; want %s still held, with its contact", outcome, st.Open, t0)
			}
			if again := wantStillOpen(t, f.contest.Path, t0, "the admission that follows changes nothing"); again != before {
				t.Fatalf("the admission after the replay changed the session: before %+v, after %+v", before, again)
			}
		})
	}
}

// breakCandidate makes Contest fail to start: its file holds another archive's
// identity, as in TestLifecycle_CandidateThatCannotOpenFallsBackToLastKnownGood.
func (f *arrivalFixture) breakCandidate() (mend func()) {
	f.t.Helper()
	db := raw(f.t, f.contest.Path)
	var was string
	if err := db.QueryRow(`SELECT archive_uuid FROM archive_metadata`).Scan(&was); err != nil {
		f.t.Fatal(err)
	}
	mustExec(f.t, db, `UPDATE archive_metadata SET archive_uuid = '019fd5c5-efcc-7193-be4f-1fee532ee3ff'`)
	return func() { mustExec(f.t, db, `UPDATE archive_metadata SET archive_uuid = ?`, was) }
}

// A switch that failed with its selector left behind, then a later restart
// (codex-213). The session opened in Home after the failed request is an
// ordinary one until a start processes the retained activation. When that
// start falls back to Home, the session is closed by the arrival, and the next
// app start gets a new one with no question. A second fallback under the same
// activation, the candidate never having been served, closes nothing more.
func TestArrival_AFallbackClosesTheSessionOfTheArchiveFallenBackTo_Once(t *testing.T) {
	f := newArrivalFixture(t)
	s2 := f.remnantIn(f.home, lifeA)
	candidate := f.remnantIn(f.contest.Path, lifeA)
	x := utils.NewUUIDv7()
	f.request(f.contest.Entry.ID, x) // pending_unclear left it; no request is made now
	f.breakCandidate()

	d := f.start()

	f.serving(d, f.promoteFixture.home)
	f.wantInEffect(x)
	wantArrived(t, f.home, s2)
	if got := arrivedUnder(t, f.home); got != x {
		t.Fatalf("Home records %q after the fallback, want %q", got, x)
	}
	wantStillOpen(t, f.contest.Path, candidate, "a candidate that never served is not changed")
	if got := arrivedUnder(t, f.contest.Path); got != "" {
		t.Fatalf("the candidate that never served records %q", got)
	}
	u := wantNewSession(t, d, lifeB, s2)

	// The fallback's record did not survive: the selector is back, with the same
	// identity, and the candidate fails again. U has taken a contact meanwhile.
	f.stop()
	f.addContact(f.home, u)
	f.request(f.contest.Entry.ID, x)
	f.edit(func(c *config.Config) { c.QsoArchiveActivationID = "" })
	before := wantStillOpen(t, f.home, u, "the fixture")
	if before.members != 1 || before.holder != lifeB {
		t.Fatalf("the fixture: U = %+v; want one contact, held by %s", before, lifeB)
	}

	d = f.start()

	f.serving(d, f.promoteFixture.home)
	f.wantInEffect(x)
	if after := wantStillOpen(t, f.home, u, "the same activation falling back again is not another arrival"); after != before {
		t.Fatalf("the session changed across the repeated fallback: before %+v, after %+v", before, after)
	}
	// The same app, admitted again, holds the session it held, with its contact.
	if st, outcome := admit(t, d, lifeB); outcome != sqlite.SessionUnchanged || st.Open == nil || st.Open.ID != u || !st.Open.HasMembers {
		t.Fatalf("the same app after the repeated fallback: outcome %q, open %+v; want %s still held, with its contact", outcome, st.Open, u)
	}
	if after := wantStillOpen(t, f.home, u, "the admission that follows changes nothing"); after != before {
		t.Fatalf("the admission after the repeated fallback changed the session: before %+v, after %+v", before, after)
	}
}

// The write that makes the activation final fails: the daemon falls back to
// the archive it left, under the failed activation's identity, and the session
// open there is closed. The candidate's file is unchanged. config.json on disk
// still holds the selector, so a later start processes the same activation;
// applied then, it closes the candidate's remnant.
func TestArrival_APromotionThatCannotBeWrittenFallsBackUnderTheSameActivation(t *testing.T) {
	f := newArrivalFixture(t)
	inHome := f.remnantIn(f.home, lifeA)
	inContest := f.remnantIn(f.contest.Path, lifeA)
	x := utils.NewUUIDv7()
	f.request(f.contest.Entry.ID, x)
	etc := filepath.Dir(f.cfgSvc.Path)
	if err := os.Chmod(etc, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(etc, 0o700) })

	d := f.start()

	f.serving(d, f.promoteFixture.home)
	if c := f.cfgSvc.Snapshot(); c.QsoArchiveActivationID != x || c.PendingQsoArchiveID != "" || c.PendingQsoArchiveActivationID != "" {
		t.Fatalf("memory: in effect %q, selector %q, pending identity %q; want %q in effect and nothing pending",
			c.QsoArchiveActivationID, c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID, x)
	}
	wantArrived(t, f.home, inHome)
	wantStillOpen(t, f.contest.Path, inContest, "the candidate's file is unchanged when its promotion fails")
	if got := arrivedUnder(t, f.contest.Path); got != "" {
		t.Fatalf("the candidate records %q though it was never made final", got)
	}

	// The disk still holds the request, with its identity.
	f.stop()
	if err := os.Chmod(etc, 0o700); err != nil {
		t.Fatal(err)
	}
	if disk := f.onDisk(t); disk.PendingQsoArchiveID != f.contest.Entry.ID || disk.PendingQsoArchiveActivationID != x {
		t.Fatalf("disk: selector %q, identity %q; want the request kept for the next start", disk.PendingQsoArchiveID, disk.PendingQsoArchiveActivationID)
	}
	fresh := seedConfigService(t, f.cfgSvc.Path) // the next process loads what the disk holds
	f.promoteFixture.cfgSvc = fresh

	d = f.start()

	f.serving(d, f.contest.Entry.ID)
	f.wantInEffect(x)
	wantArrived(t, f.contest.Path, inContest)
}

// failAfterPromotion adds one lifecycle node to the daemons built from now on.
// It starts after the activation has been made final and after the session's
// step, and in the FIRST generation its start fails, synchronously, as a node's
// start does: the orchestrator rolls the generation back and startGenerations
// records the failure and starts again. before runs just ahead of that failure.
func (f *arrivalFixture) failAfterPromotion(before func()) {
	f.t.Helper()
	const node = "test-fails-after-promotion"
	built := 0
	afterRegisterForTest = func(t *testing.T, c *iocdi.Container, o *orchestrator.Orchestrator) {
		t.Helper()
		built++
		first := built == 1
		if err := c.RegisterNode(iocdi.Node{Name: node, StartAfter: []string{nodePromote, nodeSession}}); err != nil {
			t.Fatalf("register the failing node: %v", err)
		}
		if err := o.Register(orchestrator.Adapter{NodeID: node, Start: func(context.Context) error {
			if !first {
				return nil
			}
			if before != nil {
				before()
			}
			return stderr.New("the test fails this generation after its promotion")
		}}); err != nil {
			t.Fatalf("register the failing node's adapter: %v", err)
		}
	}
	f.t.Cleanup(func() { afterRegisterForTest = nil })
}

// A generation that fails AFTER its activation was made final (A13; codex-216).
// startGenerations records the failure, finds nothing pending to end, and starts
// again on the archive now active, which is the candidate. The activation in
// effect survives the failure. The candidate's remnant is closed once, whether
// the failed generation had written the arrival or not, and the first app start
// gets a new session with no question. The archive that was left is not touched.
func TestArrival_AFailureAfterTheActivationIsFinalStartsAgainOnTheCandidate(t *testing.T) {
	for name, arrivalWritten := range map[string]bool{
		"the failed generation had written the arrival":     true,
		"the failed generation could not write the arrival": false,
	} {
		t.Run(name, func(t *testing.T) {
			f := newArrivalFixture(t)
			old := f.remnantIn(f.contest.Path, lifeA)
			left := f.remnantIn(f.home, lifeA)
			leftBefore := wantStillOpen(t, f.home, left, "the fixture")
			generation := record(t, f.contest.Path, old).generation
			x := utils.NewUUIDv7()
			f.request(f.contest.Entry.ID, x)

			db := raw(t, f.contest.Path)
			var beforeFailure func()
			if !arrivalWritten {
				mustExec(t, db, `CREATE TRIGGER refuse_close BEFORE UPDATE ON operating_session
					BEGIN SELECT RAISE(ABORT, 'the test refuses the close'); END`)
				// The file can be written again by the time the second generation starts.
				beforeFailure = func() { mustExec(t, db, `DROP TRIGGER refuse_close`) }
			}
			f.failAfterPromotion(beforeFailure)
			f.builds = 0

			d := f.start()

			if f.builds != 2 {
				t.Fatalf("builds = %d, want 2: the generation that failed after its promotion, and the one that followed", f.builds)
			}
			f.serving(d, f.contest.Entry.ID)
			if d.activationFailure == nil || d.activationFailure.Candidate.ID != f.contest.Entry.ID {
				t.Fatalf("activation failure = %+v, want the failed generation's candidate named", d.activationFailure)
			}
			f.wantInEffect(x)
			for name, c := range f.both() {
				if c.ActiveQsoArchiveID != f.contest.Entry.ID {
					t.Fatalf("%s: active archive %q, want the promoted candidate %s", name, c.ActiveQsoArchiveID, f.contest.Entry.ID)
				}
			}
			wantArrived(t, f.contest.Path, old)
			if got := arrivedUnder(t, f.contest.Path); got != x {
				t.Fatalf("the candidate records %q, want %q", got, x)
			}
			if got := record(t, f.contest.Path, old).generation; got != generation+1 {
				t.Fatalf("the candidate's generation is %d, want %d: the remnant is closed once, by one of the two generations", got, generation+1)
			}
			if after := wantStillOpen(t, f.home, left, "the archive that was left is not served by either generation"); after != leftBefore {
				t.Fatalf("the archive that was left changed: before %+v, after %+v", leftBefore, after)
			}
			if got := arrivedUnder(t, f.home); got != "" {
				t.Fatalf("the archive that was left records %q; nothing of it is written", got)
			}
			wantNewSession(t, d, lifeA, old)
		})
	}
}

// seedConfigService is a config service loaded from a file, as a new process has it.
func seedConfigService(t *testing.T, path string) *config.Service {
	t.Helper()
	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	svc := config.New(c)
	svc.SetPath(path)
	return svc
}

// The daemon killed after the activation is final and before its start has
// tested the session: the next start is an ordinary one. config.json's
// activation in effect differs from the file's, and the remnant is closed.
func TestArrival_AKillAfterTheActivationIsFinalIsStillAnArrival(t *testing.T) {
	f := newArrivalFixture(t)
	old := f.remnantIn(f.contest.Path, lifeA)
	x := utils.NewUUIDv7()
	f.edit(func(c *config.Config) { // promoted, and nothing of the file written yet
		c.ActiveQsoArchiveID = f.contest.Entry.ID
		c.QsoArchiveActivationID = x
	})

	d := f.start()

	f.serving(d, f.contest.Entry.ID)
	wantArrived(t, f.contest.Path, old)
	if got := arrivedUnder(t, f.contest.Path); got != x {
		t.Fatalf("Contest records %q, want %q", got, x)
	}
	wantNewSession(t, d, lifeA, old)
}

// The start's own test cannot be written: it is logged and the start goes on.
// When the file can be written again the admission closes the remnant before it
// gives the app a session. An arrival owed is never skipped by an admission
// that succeeds.
func TestArrival_AStartThatCannotWriteLeavesTheArrivalToTheAdmission(t *testing.T) {
	f := newArrivalFixture(t)
	old := f.remnantIn(f.contest.Path, lifeA)
	db := raw(t, f.contest.Path)
	mustExec(t, db, `CREATE TRIGGER refuse_close BEFORE UPDATE ON operating_session
		BEGIN SELECT RAISE(ABORT, 'the test refuses the close'); END`)
	x := utils.NewUUIDv7()
	f.request(f.contest.Entry.ID, x)

	d := f.start()

	f.serving(d, f.contest.Entry.ID)
	f.wantInEffect(x)
	wantStillOpen(t, f.contest.Path, old, "the fixture: the start's close was refused")
	if got := arrivedUnder(t, f.contest.Path); got != "" {
		t.Fatalf("Contest records %q though its arrival was not written", got)
	}
	if _, _, err := d.db.OpenOperatingSessionWithContext(context.Background(), lifeB, time.Now()); err == nil {
		t.Fatal("an admission succeeded while the arrival could not be written")
	}

	mustExec(t, db, `DROP TRIGGER refuse_close`)
	wantNewSession(t, d, lifeB, old)
	wantArrived(t, f.contest.Path, old)
}

// The default-epoch test is a step of every start (part 2, question 4): an
// open session of an earlier default is closed before the API is served, with
// no app admitted.
func TestArrival_TheDefaultEpochTestRunsAtStart(t *testing.T) {
	f := newArrivalFixture(t)
	old := f.remnantIn(f.home, lifeA)
	mustExec(t, raw(t, f.home), `UPDATE archive_metadata SET default_epoch = default_epoch + 1`)

	f.start()

	r := record(t, f.home, old)
	if !r.closed || r.reason != sqlite.SessionClosedByDefault {
		t.Fatalf("session %s = %+v; want it closed with %s by the start, before any admission", old, r, sqlite.SessionClosedByDefault)
	}
}

// THE ACCEPTED LIMIT (operator ruling 2026-11-21, question 4; codex-214). One
// activation serves Home, then Contest, then Home again, because the fallback's
// record cannot be written, the promotion that follows does not survive, and
// the candidate then fails. Home already records the activation, so the session
// opened there at the first fallback is NOT closed at the second: it is carried
// on, where a new session was due. This asserts what does happen, so the limit
// is seen and does not widen unnoticed. No contact is lost or moved.
func TestArrival_AcceptedLimit_OneActivationServingHomeContestHome(t *testing.T) {
	f := newArrivalFixture(t)
	x := utils.NewUUIDv7()
	f.request(f.contest.Entry.ID, x)
	mend := f.breakCandidate()

	// 1. Contest fails; Home serves under X and opens U, with a contact.
	d := f.start()
	f.serving(d, f.promoteFixture.home)
	f.stop()
	u := f.remnantIn(f.home, lifeA)
	mustExec(t, raw(t, f.home), `UPDATE archive_metadata SET arrived_activation = ?`, x) // as the fallback left it
	// The fallback's record never reached the disk: the selector is still there.
	f.request(f.contest.Entry.ID, x)
	f.edit(func(c *config.Config) { c.QsoArchiveActivationID = "" })

	// 2. Contest comes up and serves under X, and opens T, with a contact.
	mend()
	d = f.start()
	f.serving(d, f.contest.Entry.ID)
	f.stop()
	tSession := f.remnantIn(f.contest.Path, lifeB)
	homeQsos, contestQsos := qsoRowsIn(t, f.home), qsoRowsIn(t, f.contest.Path)

	// 3. The promotion did not survive: Home active, Contest pending, X. Contest fails.
	f.edit(func(c *config.Config) {
		c.ActiveQsoArchiveID = f.promoteFixture.home
		c.QsoArchiveActivationID = ""
	})
	f.request(f.contest.Entry.ID, x)
	f.breakCandidate()

	d = f.start()

	f.serving(d, f.promoteFixture.home)
	wantStillOpen(t, f.home, u, "THE LIMIT: Home already records this activation, so its session is carried on")
	if st, outcome := admit(t, d, lifeA); outcome != sqlite.SessionUnchanged || st.Open == nil || st.Open.ID != u {
		t.Fatalf("the same app after the chain: outcome %q, open %+v; the accepted limit is that %s is carried on", outcome, st.Open, u)
	}
	wantStillOpen(t, f.contest.Path, tSession, "Contest is not served, so its session waits for the next arrival there")
	if h, c := qsoRowsIn(t, f.home), qsoRowsIn(t, f.contest.Path); h != homeQsos || c != contestQsos {
		t.Fatalf("QSOs: Home %d (was %d), Contest %d (was %d); no contact is lost or moved by the chain", h, homeQsos, c, contestQsos)
	}
}
