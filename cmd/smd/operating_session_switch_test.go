package main

import (
	"context"
	"database/sql"
	stderr "errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

/*
   W-0029 part 3, the switch request (criteria B to I), through a real daemon:
   its real archive manager, seals, admission, session record and routes.

   A switch, in order: today's refusals; the FT8 and rig seals; the switch's own
   seal on live Phone / CW entry; the wait for FT8 completion logging already
   earned; the close of the session in the archive being left; only then the
   selector and the restart request. From the close until the process ends, or
   the switch fails, no session is opened (the closing phase).

   Each case reads what is STORED in the departing archive's file and in
   config.json, beside what the routes answer. Where two requests are ordered,
   one is held at a point the test sees and the other is seen parked
   (testbarrier): none sleeps and then looks.
*/

// provision makes another archive to switch to.
func (b *orderBed) provision(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	made, err := b.d.archives.Create(ctx, archive.CreateRequest{RequestKey: "k-switch", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision the other archive: %v", err)
	}
	return made.Entry.ID
}

func (b *orderBed) activate(t *testing.T, id string) (int, []byte) {
	t.Helper()
	return apiCallHeaders(t, b.d, http.MethodPost, "/v1/qso-archives/"+id+"/activate", "", nil)
}

func accepted(r httpResult) bool { return r.code == http.StatusAccepted || r.code == http.StatusOK }

// count reads the departing archive's file on a connection of the test's own.
func (b *orderBed) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+b.d.paths.QSO+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (b *orderBed) openSessions(t *testing.T) int {
	t.Helper()
	return b.count(t, `SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`)
}

// closedAs is how many sessions are closed with reason.
func (b *orderBed) closedAs(t *testing.T, reason string) int {
	t.Helper()
	return b.count(t, `SELECT COUNT(*) FROM operating_session WHERE closed_at IS NOT NULL AND close_reason = ?`, reason)
}

// contact gives the open session a contact: the member row part 5 will write,
// put here by the test, so that its close keeps the session.
func (b *orderBed) contact(t *testing.T) {
	t.Helper()
	b.archiveExec(t, `INSERT INTO qso (uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		SELECT '01920000-0000-7000-8000-00000000c001', 'G4ABC', '40m', 'SSB', 7050000, '20261120', '0915', '0915', '59', '59', 'Test',
			printf('%064d', 1), default_logbook_id FROM archive_metadata WHERE singleton = 1`)
	b.archiveExec(t, `INSERT INTO operating_session_member (qso_id, session_id)
		SELECT (SELECT MAX(id) FROM qso), id FROM operating_session WHERE closed_at IS NULL`)
}

func (b *orderBed) selector() (target, activation string) {
	c := b.d.cfgSvc.Snapshot()
	return c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID
}

// liveRefusedBySwitch: a live submit is refused in the switch's words and
// stores nothing.
func (b *orderBed) liveRefusedBySwitch(t *testing.T, when string) {
	t.Helper()
	before := b.f.qsosIn(t, b.f.a)
	code, body := b.f.submit(t, b.d, b.f.a, "1210")
	if code != http.StatusConflict || !strings.Contains(string(body), `"archive_switch_pending"`) ||
		!strings.Contains(string(body), "check which archive is active before you log the contact again") {
		t.Fatalf("%s a live submit = %d %s; want 409 archive_switch_pending in the ruled words", when, code, body)
	}
	if n := b.f.qsosIn(t, b.f.a); n != before {
		t.Fatalf("%s a refused live submit stored %d QSO(s)", when, n-before)
	}
}

func (b *orderBed) liveStored(t *testing.T, when, timeOn string) {
	t.Helper()
	before := b.f.qsosIn(t, b.f.a)
	if code, body := b.f.submit(t, b.d, b.f.a, timeOn); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("%s a live submit = %d %s; want it logged", when, code, body)
	}
	if n := b.f.qsosIn(t, b.f.a); n != before+1 {
		t.Fatalf("%s the live submit stored %d QSO(s), want 1", when, n-before)
	}
}

func (b *orderBed) txSealed(who string) bool {
	return stderr.Is(b.d.ft8.OperatingAs(who).ArmTx(true), ft8.ErrArchiveSwitchPending)
}

// ---- B4, C2, C3, E, G: a switch that succeeds ----

func TestSwitch_ASuccessClosesTheSessionAndHoldsEverythingUntilTheRestart(t *testing.T) {
	b := newOrderBed(t)
	other := b.provision(t)
	gen := b.started(t)
	b.contact(t)

	if code, body := b.activate(t, other); !accepted(httpResult{code, body}) {
		t.Fatalf("the activation = %d %s; want it accepted", code, body)
	}

	// Stored: the session closed in the departing file, its end confirmed; the
	// selector with its identity.
	if b.openSessions(t) != 0 || b.closedAs(t, "archive_switched") != 1 {
		t.Fatalf("%d open, %d closed as archive_switched; want 0 and 1", b.openSessions(t), b.closedAs(t, "archive_switched"))
	}
	if n := b.count(t, `SELECT COUNT(*) FROM operating_session_interval WHERE end_state = 'confirmed' AND ended_at IS NOT NULL`); n != 1 {
		t.Fatalf("%d intervals ended as confirmed, want 1 (operator ruling 2026-11-20)", n)
	}
	if target, activation := b.selector(); target != other || activation == "" {
		t.Fatalf("selector %q with identity %q; want %q with an identity", target, activation, other)
	}
	if !restartRequested.Load() {
		t.Fatal("no restart was requested")
	}

	// The closing phase. The read names it, at the generation the close left.
	if g, state := b.session(t, orderLifeA); state != "archive_switching" || g != gen+1 {
		t.Fatalf("the read says %q at generation %d; want archive_switching at %d", state, g, gen+1)
	}
	b.liveRefusedBySwitch(t, "in the closing phase")
	if !b.txSealed(orderLifeA) {
		t.Fatal("TX admission is not sealed after an accepted switch")
	}
	// A deliberate submit to another logbook is not the gate's to refuse.
	if code, body := b.f.submit(t, b.d, b.f.c, "1211"); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("a deliberate submit to another logbook in the phase = %d %s; want it logged", code, body)
	}
	if n := b.f.qsosIn(t, b.f.c); n != 1 {
		t.Fatalf("%d QSO(s) in the other logbook, want the deliberate one", n)
	}

	// An app start in the phase is admitted and given no session.
	b.mustAdmit(t, orderLifeB)
	b.stream(t, orderLifeB)
	if _, state := b.session(t, orderLifeB); state != "archive_switching" || b.openSessions(t) != 0 {
		t.Fatalf("after an app start in the phase: %q, %d open; want archive_switching and none opened", state, b.openSessions(t))
	}
	// Its exit is acknowledged, at a generation it need not know, and releases
	// nothing the switch holds.
	if code, body := b.exit(t, orderLifeB, 0); code != http.StatusOK || !b.exitRecorded(t, orderLifeB) {
		t.Fatalf("an exit in the phase = %d %s, recorded %v; want 200 and recorded", code, body, b.exitRecorded(t, orderLifeB))
	}
	b.liveRefusedBySwitch(t, "after an exit in the phase")
	if target, _ := b.selector(); target != other {
		t.Fatalf("after the phase's requests the selector is %q, want %q", target, other)
	}
}

// ---- B1, I4: a refusal changes nothing, whatever is already held ----

func TestSwitch_ARefusalChangesNothing(t *testing.T) {
	t.Run("a default change is pending: its seals and the session are as they were", func(t *testing.T) {
		b := newOrderBed(t)
		other := b.provision(t)
		gen := b.started(t)
		b.accepted(t)
		b.requireChangeSealsHeld(t, orderLifeA, "before the switch is asked for")

		code, body := b.activate(t, other)

		if code != http.StatusConflict || !strings.Contains(string(body), "default_change_pending") {
			t.Fatalf("the activation = %d %s; want 409 default_change_pending", code, body)
		}
		b.requireChangeSealsHeld(t, orderLifeA, "after the refused switch")
		if g, state := b.session(t, orderLifeA); state != "open" || g != gen {
			t.Fatalf("the session is %q at generation %d; want open at %d", state, g, gen)
		}
		if target, activation := b.selector(); target != "" || activation != "" {
			t.Fatalf("a refused switch left selector %q, identity %q", target, activation)
		}
	})
	t.Run("an activation is pending: a second changes nothing, and a default change is refused", func(t *testing.T) {
		b := newOrderBed(t)
		other := b.provision(t)
		b.started(t)
		b.contact(t)
		if code, body := b.activate(t, other); !accepted(httpResult{code, body}) {
			t.Fatalf("the activation = %d %s; want it accepted", code, body)
		}
		target, activation := b.selector()
		gen, _ := b.session(t, orderLifeA)

		code, body := b.activate(t, other)
		if code != http.StatusConflict || !strings.Contains(string(body), "activation_in_progress") {
			t.Fatalf("a second activation = %d %s; want 409 activation_in_progress", code, body)
		}
		if code, body := b.f.ask(t, b.d, b.f.b); code != http.StatusConflict || !strings.Contains(string(body), "activation_pending") {
			t.Fatalf("a default change under a pending activation = %d %s; want 409 activation_pending", code, body)
		}

		if tgt, act := b.selector(); tgt != target || act != activation {
			t.Fatalf("the selector became %q / %q; want %q / %q unchanged", tgt, act, target, activation)
		}
		if g, state := b.session(t, orderLifeA); state != "archive_switching" || g != gen || b.closedAs(t, "archive_switched") != 1 {
			t.Fatalf("after the refusals: %q at %d, %d closed; want the phase unchanged at %d with its one close", state, g, b.closedAs(t, "archive_switched"), gen)
		}
		b.liveRefusedBySwitch(t, "after the refusals")
		if !b.txSealed(orderLifeA) {
			t.Fatal("a refused request released the first activation's TX seal")
		}
	})
	t.Run("with no session open: none is made, nothing is sealed", func(t *testing.T) {
		b := newOrderBed(t) // nobody admitted
		code, body := b.activate(t, b.archive)
		if code != http.StatusConflict || !strings.Contains(string(body), "archive_active") {
			t.Fatalf("activating the active archive = %d %s; want 409 archive_active", code, body)
		}
		if n := b.count(t, `SELECT COUNT(*) FROM operating_session`); n != 0 || b.d.liveEntry.Closed() {
			t.Fatalf("%d sessions, live entry closed %v; want none and open", n, b.d.liveEntry.Closed())
		}
		if _, state := b.session(t, orderLifeA); state == "archive_switching" {
			t.Fatal("a refused switch began the closing phase")
		}
	})
}

// ---- C1, I5: the switch's seal on live entry ----

func TestSwitch_WaitsForAnAdmittedLiveSubmit_AndRefusesALaterOne(t *testing.T) {
	b := newOrderBed(t)
	other := b.provision(t)
	b.started(t)
	release := b.f.holdWrites(t)
	held := async(func() (int, []byte) { return b.f.submit(t, b.d, b.f.a, "1200") })
	waitFor(t, "the submit to be admitted", func() bool { return b.d.liveEntry.InFlight() == 1 })

	activation := async(func() (int, []byte) { return b.activate(t, other) })
	waitFor(t, "the switch to close live entry while the submit is in flight", func() bool {
		return b.d.archives.OperationInFlight() == "activation" && b.d.liveEntry.Closed() && b.d.liveEntry.InFlight() == 1
	})

	late := within(t, "a live submit while the switch waits", async(func() (int, []byte) { return b.f.submit(t, b.d, b.f.a, "1201") }))
	if late.code != http.StatusConflict || !strings.Contains(string(late.body), "archive_switch_pending") {
		t.Fatalf("a live submit while the switch waits = %d %s; want 409 archive_switch_pending", late.code, late.body)
	}
	stillWaiting(t, "the switch", activation)
	if b.openSessions(t) != 1 {
		t.Fatal("the session was closed while an admitted live submit was still running")
	}
	if target, _ := b.selector(); target != "" {
		t.Fatalf("the selector %q was written while an admitted live submit was still running", target)
	}

	release()
	if r := answered(t, "the held submit", held); r.code != http.StatusCreated && r.code != http.StatusOK {
		t.Fatalf("the held submit = %d %s; want it logged", r.code, r.body)
	}
	if r := answered(t, "the switch", activation); !accepted(r) {
		t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
	}
	if n := b.f.qsosIn(t, b.f.a); n != 1 || b.openSessions(t) != 0 {
		t.Fatalf("%d QSO(s) stored, %d sessions open; want the admitted contact stored and the session closed", n, b.openSessions(t))
	}
}

// A live submit whose write fails stores nothing and leaves nothing for the
// switch to wait for.
func TestSwitch_AFailedLiveSubmitDoesNotBlockTheSwitch(t *testing.T) {
	b := newOrderBed(t)
	other := b.provision(t)
	b.started(t)
	b.archiveExec(t, `CREATE TRIGGER fail_qso BEFORE INSERT ON qso BEGIN SELECT RAISE(ABORT, 'forced: the contact cannot be stored'); END`)
	if code, body := b.f.submit(t, b.d, b.f.a, "1200"); code == http.StatusCreated || code == http.StatusOK {
		t.Fatalf("the fixture: the submit = %d %s; want its write to fail", code, body)
	}
	r := within(t, "the switch after a failed live submit", async(func() (int, []byte) { return b.activate(t, other) }))
	if !accepted(r) || b.f.qsosIn(t, b.f.a) != 0 {
		t.Fatalf("the switch = %d %s with %d QSO(s) stored; want it accepted and none stored", r.code, r.body, b.f.qsosIn(t, b.f.a))
	}
}

// ---- D: the FT8 completion drain ----

// newSwitchFt8Bed is the exit's FT8 bed with a restart port and another archive.
func newSwitchFt8Bed(t *testing.T) (b *orderBed, other string) {
	t.Helper()
	withRestartPort(t)
	b, _ = newExitFt8Bed(t)
	return b, b.provision(t)
}

// A contact earned before the switch is stored, with its upload row, before
// the session is closed. Its write is held at the archive's write lock when
// the switch is asked for: the switch is seen in the drain, the session still
// open and nothing requested.
func TestSwitch_AnEarnedFt8ContactIsStoredBeforeTheClose(t *testing.T) {
	b, other := newSwitchFt8Bed(t)
	release := b.holdArchiveWrites(t)
	b.earned("JA1XYZ") // its job waits at the write lock
	w := drainWatch(t)
	activation := async(func() (int, []byte) { return b.activate(t, other) })
	inDrain(w, "the switch, with an earned contact not yet stored", activation)
	if b.openSessions(t) != 1 {
		t.Fatal("the session was closed while an earned contact was not yet stored")
	}
	if target, _ := b.selector(); target != "" || restartRequested.Load() {
		t.Fatalf("selector %q, restart requested %v, while an earned contact was not yet stored", target, restartRequested.Load())
	}

	release()
	if r := answered(t, "the switch", activation); !accepted(r) {
		t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
	}
	// Read at once: the switch has answered, so the contact is stored, in the
	// archive that was left.
	ftModes[0].stored(t, b.d, "JA1XYZ")
	if b.openSessions(t) != 0 {
		t.Fatal("the session is still open after the switch")
	}
}

// The switch waits for a tracked completion job whatever it is waiting for,
// and cancels none.
func TestSwitch_WaitsForATrackedFt8CompletionJob(t *testing.T) {
	b, other := newSwitchFt8Bed(t)
	gate := make(chan struct{})
	released, cancelled := false, false
	letGo := func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(letGo)
	launchFt8QsoLog(&b.d.qsoLogWG, nil, func(ctx context.Context) {
		<-gate
		cancelled = ctx.Err() != nil
	})

	w := drainWatch(t)
	activation := async(func() (int, []byte) { return b.activate(t, other) })
	inDrain(w, "the switch, with a completion job still running", activation)
	if b.openSessions(t) != 1 {
		t.Fatal("the session was closed while a completion job was still running")
	}
	letGo()
	if r := answered(t, "the switch", activation); !accepted(r) {
		t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
	}
	if cancelled {
		t.Fatal("the switch cancelled a completion job")
	}
}

// A completion whose write fails stores nothing and does not block the switch.
func TestSwitch_AFailedFt8CompletionWriteDoesNotBlockTheSwitch(t *testing.T) {
	b, other := newSwitchFt8Bed(t)
	b.archiveExec(t, `CREATE TRIGGER fail_qso BEFORE INSERT ON qso BEGIN SELECT RAISE(ABORT, 'forced: the contact cannot be stored'); END`)
	b.earned("JA1XYZ")
	r := within(t, "the switch after a failed completion write", async(func() (int, []byte) { return b.activate(t, other) }))
	if !accepted(r) {
		t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
	}
	if n := b.count(t, `SELECT COUNT(*) FROM qso`); n != 0 {
		t.Fatalf("%d QSO(s) stored by a completion whose write failed", n)
	}
}

// ---- E4: a close that fails ----

func TestSwitch_ACloseThatFailsRequestsNothingAndTheSittingGoesOn(t *testing.T) {
	b := newOrderBed(t)
	other := b.provision(t)
	gen := b.started(t)
	b.contact(t)
	b.archiveExec(t, `CREATE TRIGGER fail_close BEFORE UPDATE OF session_generation ON archive_metadata
		BEGIN SELECT RAISE(ABORT, 'forced: the session cannot be closed'); END`)

	code, body := b.activate(t, other)
	b.archiveExec(t, `DROP TRIGGER fail_close`)

	if code != http.StatusServiceUnavailable || !strings.Contains(string(body), `"session_close_failed"`) {
		t.Fatalf("the activation = %d %s; want 503 session_close_failed", code, body)
	}
	if target, activation := b.selector(); target != "" || activation != "" || restartRequested.Load() {
		t.Fatalf("selector %q, identity %q, restart requested %v; want nothing requested", target, activation, restartRequested.Load())
	}
	if g, state := b.session(t, orderLifeA); state != "open" || g != gen || b.closedAs(t, "archive_switched") != 0 {
		t.Fatalf("the session is %q at generation %d; want it open at %d, not closed", state, g, gen)
	}
	// Every seal the switch took is released.
	b.liveStored(t, "after the failed close", "1210")
	if b.txSealed(orderLifeA) {
		t.Fatal("TX admission is still sealed after a switch that requested nothing")
	}
	// And a later switch can be asked for.
	if code, body := b.activate(t, other); !accepted(httpResult{code, body}) {
		t.Fatalf("a switch after the failed one = %d %s; want it accepted", code, body)
	}
}

// ---- F: a failure after the close ----

func TestSwitch_AFailureAfterTheCloseOpensOneNewSessionAndGivesOperationBack(t *testing.T) {
	b := newOrderBed(t)
	other := b.provision(t)
	b.started(t)
	b.contact(t)

	var code int
	var body []byte
	b.f.configDir(t, 0o500, func() { code, body = b.activate(t, other) })

	if code != http.StatusInternalServerError || !strings.Contains(string(body), "activation_persist_failed") {
		t.Fatalf("the activation = %d %s; want 500 activation_persist_failed", code, body)
	}
	// The close stands; one new session is open for the app that is current,
	// before the switch answered.
	if b.closedAs(t, "archive_switched") != 1 || b.openSessions(t) != 1 {
		t.Fatalf("%d closed as archive_switched, %d open; want 1 and 1", b.closedAs(t, "archive_switched"), b.openSessions(t))
	}
	if n := b.count(t, `SELECT COUNT(*) FROM operating_session_member m JOIN operating_session s ON s.id = m.session_id WHERE s.closed_at IS NOT NULL`); n != 1 {
		t.Fatalf("the closed session holds %d contact(s), want its 1", n)
	}
	if _, state := b.session(t, orderLifeA); state != "open" {
		t.Fatalf("the read says %q after the failed switch; want open, with no question", state)
	}
	if target, _ := b.selector(); target != "" {
		t.Fatalf("selector %q after a switch that could not record it", target)
	}
	b.liveStored(t, "after the failed switch", "1210")
	if b.txSealed(orderLifeA) {
		t.Fatal("TX admission is still sealed after the failed switch")
	}

	// Retries do not pile sessions up: each closes the empty session the last
	// one opened, which is removed.
	for i := 0; i < 2; i++ {
		b.f.configDir(t, 0o500, func() { code, body = b.activate(t, other) })
		if !strings.Contains(string(body), "activation_persist_failed") {
			t.Fatalf("retry %d = %d %s; want activation_persist_failed", i, code, body)
		}
	}
	if total := b.count(t, `SELECT COUNT(*) FROM operating_session`); total != 2 || b.openSessions(t) != 1 {
		t.Fatalf("%d sessions, %d open, after three failed switches; want the one with a contact and one open", total, b.openSessions(t))
	}
}

// If the new session cannot be opened the switch still answers its own
// failure, and the app's own next admission opens the session.
func TestSwitch_AFailedReopenIsRepairedByTheAppsNextAdmission(t *testing.T) {
	b := newOrderBed(t)
	other := b.provision(t)
	b.started(t)
	b.contact(t)
	// The close writes, and then the file fails: the open that follows cannot.
	b.archiveExec(t, `CREATE TRIGGER fail_open BEFORE INSERT ON operating_session
		BEGIN SELECT RAISE(ABORT, 'forced: no session can be opened'); END`)

	var code int
	var body []byte
	b.f.configDir(t, 0o500, func() { code, body = b.activate(t, other) })

	if !strings.Contains(string(body), "activation_persist_failed") {
		t.Fatalf("the activation = %d %s; want its own failure, activation_persist_failed", code, body)
	}
	if b.openSessions(t) != 0 {
		t.Fatal("a session is open though none could be opened")
	}
	// The app's read says it is not current, so its flow admits again.
	rc, raw := b.as(t, orderLifeA, http.MethodGet, "/v1/app/lifetime", "")
	if rc != http.StatusOK || !strings.Contains(string(raw), `"current":false`) {
		t.Fatalf("the app's read = %d %s; want current false", rc, raw)
	}
	if rc, raw := b.admit(t, orderLifeA); rc != http.StatusServiceUnavailable {
		t.Fatalf("the admission while the file fails = %d %s; want 503", rc, raw)
	}

	b.archiveExec(t, `DROP TRIGGER fail_open`)
	b.mustAdmit(t, orderLifeA)
	if _, state := b.session(t, orderLifeA); state != "open" || b.openSessions(t) != 1 {
		t.Fatalf("after the app's admission: %q, %d open; want open with one session", state, b.openSessions(t))
	}
}

// ---- H1: a power cut after the close and before the selector is durable ----

func TestSwitch_APowerCutAfterTheCloseIsAnOrdinaryStartWithNoQuestion(t *testing.T) {
	b := newOrderBed(t)
	b.started(t)
	b.contact(t)
	// The files as the cut leaves them: the close committed, as the request
	// makes it, and no selector written.
	release := b.d.server.HoldSessionOrder()
	err := b.d.server.SwitchSession().Close()
	release()
	if err != nil {
		t.Fatalf("the close: %v", err)
	}
	b.f.stop(t, b.d)
	if c := b.f.disk(t); c.PendingQsoArchiveID != "" || c.PendingQsoArchiveActivationID != "" {
		t.Fatalf("the fixture: config.json holds selector %q", c.PendingQsoArchiveID)
	}

	b.d = b.f.mustStart(t) // an ordinary start on the archive that was being left

	if b.d.cfgSvc.Snapshot().ActiveQsoArchiveID != b.archive {
		t.Fatal("the start is not on the departing archive")
	}
	if b.openSessions(t) != 0 || b.closedAs(t, "archive_switched") != 1 {
		t.Fatalf("after the start: %d open, %d closed as archive_switched; want the close as it was left", b.openSessions(t), b.closedAs(t, "archive_switched"))
	}
	b.mustAdmit(t, orderLifeB)
	if _, state := b.session(t, orderLifeB); state != "open" {
		t.Fatalf("the app start reads %q; want open: a new session and no recovery question", state)
	}
	if n := b.count(t, `SELECT COUNT(*) FROM operating_session s JOIN operating_session_interval i ON i.session_id = s.id
		WHERE s.closed_at IS NULL AND i.end_state = 'open' AND i.app_lifetime = ?`, orderLifeB); n != 1 {
		t.Fatalf("%d open sessions held by the new app, want 1", n)
	}
	if n := b.count(t, `SELECT COUNT(*) FROM operating_session_member m JOIN operating_session s ON s.id = m.session_id
		WHERE s.close_reason = 'archive_switched'`); n != 1 {
		t.Fatalf("the closed session holds %d contact(s), want its 1", n)
	}
}

// ---- I: the one order ----

// heldSwitch asks for the switch and holds it in its drain, after its seals
// and before its close, on a completion job the test alone releases.
func (b *orderBed) heldSwitch(t *testing.T, other string) (activation chan httpResult, letGo func()) {
	t.Helper()
	gate := make(chan struct{})
	released := false
	letGo = func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(letGo)
	launchFt8QsoLog(&b.d.qsoLogWG, nil, func(context.Context) { <-gate })
	w := drainWatch(t)
	activation = async(func() (int, []byte) { return b.activate(t, other) })
	inDrain(w, "the switch, held in its drain", activation)
	return activation, letGo
}

func TestSwitchOrder_I1_AnExitAgainstASwitch(t *testing.T) {
	t.Run("the exit first: the switch waits, then finds no session to close", func(t *testing.T) {
		b := newOrderBed(t)
		other := b.provision(t)
		gen := b.started(t)
		b.contact(t)
		exit, release := b.heldExit(t, gen)

		w := orderWatch(t, archiveOpTakesOrder)
		activation := async(func() (int, []byte) { return b.activate(t, other) })
		parkedAtOrder(w, "the switch, with an exit under way", activation)
		if target, _ := b.selector(); target != "" {
			t.Fatalf("the switch recorded %q while the exit was still under way", target)
		}

		release()
		if r := answered(t, "the exit", exit); r.code != http.StatusOK {
			t.Fatalf("the exit = %d %s; want 200", r.code, r.body)
		}
		if r := answered(t, "the switch", activation); !accepted(r) {
			t.Fatalf("the switch after the exit = %d %s; want it accepted", r.code, r.body)
		}
		// One close of the session, by the exit, which came first.
		if b.closedAs(t, "archive_switched") != 0 || b.openSessions(t) != 0 || b.count(t, `SELECT COUNT(*) FROM operating_session`) != 1 {
			t.Fatalf("%d closed as archive_switched, %d open; want the exit's close alone", b.closedAs(t, "archive_switched"), b.openSessions(t))
		}
		// The exit released its own seal before the switch sealed: the switch's holds.
		b.liveRefusedBySwitch(t, "after the exit and then the switch")
	})
	t.Run("the switch first: the exit waits, then is acknowledged in the closing phase", func(t *testing.T) {
		b := newOrderBed(t)
		other := b.provision(t)
		gen := b.started(t)
		b.contact(t)
		activation, letGo := b.heldSwitch(t, other)

		w := orderWatch(t, exitTakesOrder)
		exit := async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) })
		parkedAtOrder(w, "the exit, with a switch under way", exit)
		if b.exitRecorded(t, orderLifeA) {
			t.Fatal("the exit was stored while the switch was under way")
		}

		letGo()
		if r := answered(t, "the switch", activation); !accepted(r) {
			t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
		}
		// The exit named the generation from before the close, and is not sent round again.
		if r := answered(t, "the exit", exit); r.code != http.StatusOK || !b.exitRecorded(t, orderLifeA) {
			t.Fatalf("the exit = %d %s, recorded %v; want 200 and recorded", r.code, r.body, b.exitRecorded(t, orderLifeA))
		}
		// One close of the session, by the switch.
		if b.closedAs(t, "archive_switched") != 1 || b.count(t, `SELECT COUNT(*) FROM operating_session`) != 1 {
			t.Fatalf("%d closed as archive_switched of %d sessions; want the switch's close alone",
				b.closedAs(t, "archive_switched"), b.count(t, `SELECT COUNT(*) FROM operating_session`))
		}
		b.liveRefusedBySwitch(t, "after the switch and then the exit")
		if target, _ := b.selector(); target != other {
			t.Fatalf("the selector is %q after the exit, want %q", target, other)
		}
	})
}

func TestSwitchOrder_I2_AnAppStartAgainstASwitch(t *testing.T) {
	t.Run("the start first: its new session is the one the switch closes", func(t *testing.T) {
		b := newOrderBed(t)
		other := b.provision(t)
		gen := b.started(t) // A, with an empty session
		release := b.f.holdWrites(t)
		// B's start replaces A. It stops the scopes and then its start step
		// waits at the archive's write lock, inside the admission.
		start := async(func() (int, []byte) { return b.admit(t, orderLifeB) })
		waitFor(t, "the app start to stop the scopes, inside the admission", func() bool {
			return stderr.Is(b.d.ft8.OperatingAs(orderLifeA).AbandonQso(), ft8.ErrLifetimeNotCurrent)
		})

		w := orderWatch(t, archiveOpTakesOrder)
		activation := async(func() (int, []byte) { return b.activate(t, other) })
		parkedAtOrder(w, "the switch, with an app start under way", activation)
		unanswered(t, "the app start, held at its start step", start)

		release()
		if r := answered(t, "the app start", start); r.code != http.StatusOK {
			t.Fatalf("the app start = %d %s; want 200", r.code, r.body)
		}
		if r := answered(t, "the switch", activation); !accepted(r) {
			t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
		}
		// Two changes of the session, in that order: the start discarded A's
		// empty session and opened B's; the switch closed B's, which was empty
		// and is removed. (The switch first would be one change.)
		if n := b.count(t, `SELECT COUNT(*) FROM operating_session`); n != 0 {
			t.Fatalf("%d sessions after the switch; want the start's empty one closed and removed", n)
		}
		if g, state := b.session(t, orderLifeB); state != "archive_switching" || g != gen+2 {
			t.Fatalf("the read says %q at generation %d; want archive_switching at %d: the start's open, then the switch's close", state, g, gen+2)
		}
	})
	t.Run("the switch first: the start is admitted into the closing phase and opens nothing", func(t *testing.T) {
		b := newOrderBed(t)
		other := b.provision(t)
		b.started(t)
		b.contact(t)
		activation, letGo := b.heldSwitch(t, other)

		w := orderWatch(t, "(*appAdmission).read")
		start := async(func() (int, []byte) { return b.admit(t, orderLifeB) })
		parkedAtOrder(w, "the app start, with a switch under way", start)

		letGo()
		if r := answered(t, "the switch", activation); !accepted(r) {
			t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
		}
		if r := answered(t, "the app start", start); r.code != http.StatusOK {
			t.Fatalf("the app start = %d %s; want it admitted, not refused", r.code, r.body)
		}
		if b.openSessions(t) != 0 || b.count(t, `SELECT COUNT(*) FROM operating_session`) != 1 {
			t.Fatalf("%d open of %d sessions; want none opened in the phase", b.openSessions(t), b.count(t, `SELECT COUNT(*) FROM operating_session`))
		}
		if _, state := b.session(t, orderLifeB); state != "archive_switching" {
			t.Fatalf("the new app reads %q; want archive_switching", state)
		}
	})
}
