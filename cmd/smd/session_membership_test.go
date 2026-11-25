package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

/*
   W-0029 part 5, slice 1, through a real daemon: the FT8 completed-exchange
   sink and the submit route as the daemon composes them, the real archive file
   with an enabled insert route, and an app admitted to it.

   The membership is connected here as part 6's change will connect it
   (api.Server.ConnectSessionMembership). The daemon's ordinary start does not
   connect it (operator ruling 2026-11-24), and the last test is of that start.
   What each reads is what is STORED: the contact's row, and its member row or
   its absence.
*/

// memberOf is how many member rows put call's contact into the open session.
func (b *orderBed) memberOf(t *testing.T, call string) int {
	t.Helper()
	return b.count(t, `SELECT COUNT(*) FROM operating_session_member m
		JOIN qso q ON q.id = m.qso_id JOIN operating_session s ON s.id = m.session_id
		WHERE q.call = ? AND s.closed_at IS NULL`, call)
}

// phoneContact logs one Phone/CW contact as a page does: it names the default
// logbook and asserts it, so the route classifies it as live.
func (b *orderBed) phoneContact(t *testing.T, lifetime, timeOn string) {
	t.Helper()
	code, body := apiCallHeaders(t, b.d, http.MethodPost, "/v1/qso?logbook=1", strings.ReplaceAll(submitADIF, "%s", timeOn),
		map[string]string{"Content-Type": "application/x-adif", "X-SM-Archive-Id": b.archive, "X-SM-App-Lifetime": lifetime,
			"X-SM-Default-Logbook": b.text(t, `SELECT uuid FROM logbook WHERE id = 1`)})
	if code != http.StatusCreated {
		t.Fatalf("a live Phone/CW submit = %d %s; want it logged", code, body)
	}
}

// A1, A2, B5: with the membership connected, a completed FT8 exchange and a
// live Phone/CW submit each join the session the running app holds. A contact
// logged before the connection stays in no session: connecting adds no row.
func TestMembership_Daemon_AnFt8CompletionAndAPhoneContactJoinTheOpenSession(t *testing.T) {
	b, gen := newExitFt8Bed(t)
	b.earned("K1OLD") // before the membership is connected
	b.d.drainFt8Completions()
	if n := b.count(t, `SELECT COUNT(*) FROM qso WHERE call = 'K1OLD'`); n != 1 {
		t.Fatalf("%d contacts stored before the connection, want 1", n)
	}

	b.d.server.ConnectSessionMembership()
	b.earned("JA1XYZ")
	b.d.drainFt8Completions()
	b.phoneContact(t, orderLifeA, "1210")

	if n := b.count(t, `SELECT COUNT(*) FROM qso`); n != 3 {
		t.Fatalf("%d contacts stored, want 3", n)
	}
	if ft8, phone := b.memberOf(t, "JA1XYZ"), b.count(t, `SELECT COUNT(*) FROM operating_session_member m JOIN qso q ON q.id = m.qso_id
		JOIN operating_session s ON s.id = m.session_id WHERE q.time_on LIKE '1210%' AND s.closed_at IS NULL`); ft8 != 1 || phone != 1 {
		t.Fatalf("member rows: %d for the FT8 contact and %d for the Phone/CW one; want one each, in the open session", ft8, phone)
	}
	if n := b.count(t, `SELECT COUNT(*) FROM operating_session_member`); n != 2 || b.memberOf(t, "K1OLD") != 0 {
		t.Fatalf("%d member rows; want two, and none for the contact logged before the connection", n)
	}
	if b.count(t, `SELECT COUNT(*) FROM qso_upload`) != 3 {
		t.Fatalf("%d upload rows, want one for each contact", b.count(t, `SELECT COUNT(*) FROM qso_upload`))
	}
	// The contacts joined the app's own session: no session was opened or
	// closed for them, and the app is not asked anything.
	if now, state := b.session(t, orderLifeA); now != gen || state != "open" || b.count(t, `SELECT COUNT(*) FROM operating_session`) != 1 {
		t.Fatalf("the session is %q at generation %d with %d sessions in the file; want open, generation %d, one session",
			state, now, b.count(t, `SELECT COUNT(*) FROM operating_session`), gen)
	}
}

// Ruling 1 (2026-11-24): the daemon as its ordinary start composes it. A
// completed FT8 exchange and a live Phone/CW submit are stored, no member row
// is written, and the next app to start is asked nothing.
func TestMembership_Daemon_TheOrdinaryStartConnectsNothing(t *testing.T) {
	b, _ := newExitFt8Bed(t)
	b.earned("JA1XYZ")
	b.d.drainFt8Completions()
	b.phoneContact(t, orderLifeA, "1210")
	if n := b.count(t, `SELECT COUNT(*) FROM qso`); n != 2 {
		t.Fatalf("%d contacts stored, want 2", n)
	}
	if n := b.count(t, `SELECT COUNT(*) FROM operating_session_member`); n != 0 {
		t.Fatalf("the daemon's ordinary start wrote %d member rows; membership is not connected until part 6", n)
	}
	// Another app starts while the first still holds the session, as after a
	// kill: with no member row the leftover session is empty, and nothing is asked.
	b.mustAdmit(t, orderLifeB)
	if _, state := b.session(t, orderLifeB); state != "open" {
		t.Fatalf("the next app reads %q; want a session of its own and no question", state)
	}
}

/*
   Slice 2: the wait before a leftover session is judged empty (E), and what a
   contact stored by a drain belongs to (H), with the daemon's real completion
   jobs and its real drain (drainFt8Completions: the hand-over, then every
   tracked job). The hand-over itself, a contact still between its slot and
   its job when the scopes are stopped, is internal/ft8's own test
   (operating_contact_test.go); that the drain makes that wait first is pinned
   by completion_drain_source_test.go. What is shown here is that an app's
   start is held in that same drain.
*/

// sessionOf is the session a contact is a member of and how that session was
// closed ("open" while it is not): "" and "" when the contact is in none.
func (b *orderBed) sessionOf(t *testing.T, where string, args ...any) (session, closed string) {
	t.Helper()
	q := `FROM operating_session_member m JOIN qso q ON q.id = m.qso_id
		JOIN operating_session s ON s.id = m.session_id WHERE ` + where
	if b.count(t, `SELECT COUNT(*) `+q, args...) == 0 {
		return "", ""
	}
	return b.text(t, `SELECT s.id `+q, args...), b.text(t, `SELECT COALESCE(s.close_reason, 'open') `+q, args...)
}

// E4: an app is gone without a confirmed exit, and a contact it earned is not
// yet stored: its job waits at the archive's write lock. The next app's
// admission is seen waiting in the daemon's drain and answers nothing. When
// the contact is stored the admission succeeds; the new app is asked about the
// earlier session, which holds the contact; no session was opened or discarded.
func TestMembership_Daemon_AnAppStartWaitsForAnEarnedContact(t *testing.T) {
	b, _ := newExitFt8Bed(t)
	b.d.server.ConnectSessionMembership()
	leftover := b.text(t, `SELECT id FROM operating_session WHERE closed_at IS NULL`)
	release := b.holdArchiveWrites(t)
	b.earned("JA1XYZ") // its job waits at the write lock

	w := drainWatch(t)
	start := async(func() (int, []byte) { return b.admit(t, orderLifeB) })
	inDrain(w, "the next app's admission, with an earned contact not yet stored", start)
	if n := b.count(t, `SELECT COUNT(*) FROM operating_session WHERE id = ? AND closed_at IS NULL`, leftover); n != 1 {
		t.Fatal("the leftover session was judged while an earned contact was not yet stored")
	}

	release()
	if r := answered(t, "the admission", start); r.code != http.StatusOK {
		t.Fatalf("the admission, once the contact was stored = %d %s; want 200", r.code, r.body)
	}
	ftModes[0].stored(t, b.d, "JA1XYZ")
	session, closed := b.sessionOf(t, `q.call = 'JA1XYZ'`)
	if session != leftover || closed != "open" || b.count(t, `SELECT COUNT(*) FROM operating_session`) != 1 {
		t.Fatalf("the contact is in %q (%s) and the file has %d sessions; want it in the earlier session %s, still open, and no session opened or discarded",
			session, closed, b.count(t, `SELECT COUNT(*) FROM operating_session`), leftover)
	}
	if _, state := b.session(t, orderLifeB); state != "recovery_pending" {
		t.Fatalf("the new app reads %q; want to be asked about the earlier session", state)
	}
}

// Ruling 1 (2026-11-24): the daemon's ordinary start does not connect the
// wait. An admission answers while a completion job is still running, as it
// always has.
func TestMembership_Daemon_TheOrdinaryStartDoesNotWaitAtAnAppStart(t *testing.T) {
	b, _ := newExitFt8Bed(t)
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

	r := within(t, "an admission with a completion job still running", async(func() (int, []byte) { return b.admit(t, orderLifeB) }))
	if r.code != http.StatusOK {
		t.Fatalf("the admission = %d %s; want 200", r.code, r.body)
	}
}

// H: an FT8 contact earned before a switch and stored by its drain is a member
// of the session the switch closes, which is kept.
func TestMembership_Daemon_ASwitchKeepsTheSessionOfAContactItsDrainStored(t *testing.T) {
	b, other := newSwitchFt8Bed(t)
	b.d.server.ConnectSessionMembership()
	session := b.text(t, `SELECT id FROM operating_session WHERE closed_at IS NULL`)
	release := b.holdArchiveWrites(t)
	b.earned("JA1XYZ")
	w := drainWatch(t)
	activation := async(func() (int, []byte) { return b.activate(t, other) })
	inDrain(w, "the switch, with an earned contact not yet stored", activation)

	release()
	if r := answered(t, "the switch", activation); !accepted(r) {
		t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
	}
	if got, closed := b.sessionOf(t, `q.call = 'JA1XYZ'`); got != session || closed != "archive_switched" || b.openSessions(t) != 0 {
		t.Fatalf("the contact is in %q, closed as %q, with %d sessions open; want it in %s, closed by the switch and kept",
			got, closed, b.openSessions(t), session)
	}
}

// H: a Phone/CW contact admitted before the switch's seal and stored while the
// switch waits for it is a member of the session the switch closes.
func TestMembership_Daemon_ASwitchKeepsTheSessionOfAnAdmittedPhoneContact(t *testing.T) {
	b := newOrderBed(t)
	b.d.server.ConnectSessionMembership()
	other := b.provision(t)
	b.started(t)
	session := b.text(t, `SELECT id FROM operating_session WHERE closed_at IS NULL`)
	release := b.f.holdWrites(t)
	held := async(func() (int, []byte) { return b.f.submit(t, b.d, b.f.a, "1200") })
	waitFor(t, "the submit to be admitted", func() bool { return b.d.liveEntry.InFlight() == 1 })
	activation := async(func() (int, []byte) { return b.activate(t, other) })
	waitFor(t, "the switch to close live entry while the submit is in flight", func() bool {
		return b.d.archives.OperationInFlight() == "activation" && b.d.liveEntry.Closed() && b.d.liveEntry.InFlight() == 1
	})

	release()
	if r := answered(t, "the held submit", held); r.code != http.StatusCreated {
		t.Fatalf("the held submit = %d %s; want it logged", r.code, r.body)
	}
	if r := answered(t, "the switch", activation); !accepted(r) {
		t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
	}
	if got, closed := b.sessionOf(t, `q.time_on LIKE '1200%'`); got != session || closed != "archive_switched" || b.openSessions(t) != 0 {
		t.Fatalf("the contact is in %q, closed as %q, with %d sessions open; want it in %s, closed by the switch and kept",
			got, closed, b.openSessions(t), session)
	}
}

// H: an FT8 contact earned before an exit and stored by its drain is a member
// of the session the exit closes.
func TestMembership_Daemon_AnExitClosesTheSessionOfAContactItsDrainStored(t *testing.T) {
	b, gen := newExitFt8Bed(t)
	b.d.server.ConnectSessionMembership()
	session := b.text(t, `SELECT id FROM operating_session WHERE closed_at IS NULL`)
	release := b.holdArchiveWrites(t)
	b.earned("JA1XYZ")
	w := drainWatch(t)
	exit := async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) })
	inDrain(w, "the exit, with an earned contact not yet stored", exit)

	release()
	if r := answered(t, "the exit", exit); r.code != http.StatusOK {
		t.Fatalf("the exit = %d %s; want 200: a contact joining the open session moves no generation", r.code, r.body)
	}
	if got, closed := b.sessionOf(t, `q.call = 'JA1XYZ'`); got != session || closed != "app_closed" || !b.exitRecorded(t, orderLifeA) {
		t.Fatalf("the contact is in %q, closed as %q, exit recorded %v; want it in %s, closed by the exit",
			got, closed, b.exitRecorded(t, orderLifeA), session)
	}
}
