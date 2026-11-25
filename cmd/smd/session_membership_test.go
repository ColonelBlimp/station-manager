package main

import (
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
