package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
)

// The two session controls as routes (ADR 0100 §3, §5, §6, §8; W-0029 part 4,
// criteria A to D and G): POST /v1/operating-session/new and
// POST /v1/operating-session/continue. They are the answers to the recovery
// question and, built whole (operator ruling 2026-11-23), the Session panel's
// controls. Every case is judged on the file's rows and the admission's state
// together, through the real routes.

// answered is what a control answered: a 200's state, or a stale refusal's.
type answered struct {
	status int
	code   string
	view   sessionView
	raw    []byte
}

// control is a session control as the app listener accepts it. archive is the
// archive header ("" for none).
func (b *exitBed) control(lifetime, path, body, archive string) *http.Request {
	b.t.Helper()
	r := b.acceptable(b.t, true, http.MethodPost, path, b.cred)
	if lifetime != "" {
		r.Header.Set(appLifetimeHeader, lifetime)
	}
	r.Header.Del(archiveIDHeader)
	if archive != "" {
		r.Header.Set(archiveIDHeader, archive)
	}
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func (b *exitBed) send(r *http.Request) answered {
	b.t.Helper()
	resp, err := b.c.Do(r)
	if err != nil {
		b.t.Fatalf("%s %s: %v", r.Method, r.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	a := answered{status: resp.StatusCode, raw: raw}
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &a.view); err != nil {
			b.t.Fatalf("%s answered 200 with %s: %v", r.URL.Path, raw, err)
		}
		return a
	}
	a.code = errorCode(b.t, raw)
	var stale struct {
		Session *sessionView `json:"operating_session"`
	}
	if json.Unmarshal(raw, &stale) == nil && stale.Session != nil {
		a.view = *stale.Session
	}
	return a
}

func (b *exitBed) newAs(lifetime string, generation int64) answered {
	b.t.Helper()
	return b.send(b.control(lifetime, "/v1/operating-session/new", fmt.Sprintf(`{"generation":%d}`, generation), testServedArchive))
}

func (b *exitBed) continueAs(lifetime string, generation int64, session string) answered {
	b.t.Helper()
	return b.send(b.control(lifetime, "/v1/operating-session/continue",
		fmt.Sprintf(`{"generation":%d,"session_id":%q}`, generation, session), testServedArchive))
}

// contactN gives a session its n-th contact and returns the QSO's id.
func (b *exitBed) contactN(session string, n int) int64 {
	b.t.Helper()
	b.exec(`INSERT INTO qso (uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		SELECT printf('01920000-0000-7000-8000-0000000%05d', ?), 'G4ABC', '40m', 'SSB', 7050000, '20261120', printf('09%02d', ?), printf('09%02d', ?), '59', '59', 'Test',
			printf('%064d', 100 + ?), id FROM logbook WHERE uuid = ?`, n, n, n, n, b.logbook)
	var id int64
	if err := b.raw.QueryRow(`SELECT MAX(id) FROM qso`).Scan(&id); err != nil {
		b.t.Fatal(err)
	}
	b.exec(`INSERT INTO operating_session_member (qso_id, session_id) VALUES (?, ?)`, id, session)
	return id
}

// newRecoveryBed is the state the recovery question is asked in: lifetime A's
// session with one contact, a daemon restart, and lifetime B admitted. B is
// current and holds no session. It returns what B reads: the question.
func newRecoveryBed(t *testing.T) (*exitBed, sessionView) {
	t.Helper()
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	b.contactN(b.view(lifeA).Session.ID, 1)
	b.restart()
	b.mustBeAdmitted(lifeB)
	q := b.view(lifeB)
	if q.State != sessionStateRecoveryPending || q.Session == nil || q.Session.HeldByYou {
		t.Fatalf("the fixture: B reads %+v with %+v; want the recovery question", q, q.Session)
	}
	return b, q
}

// sameState fails unless two states name the same generation, state and session.
func sameState(t *testing.T, what string, got, want sessionView) {
	t.Helper()
	gotID, wantID := "", ""
	if got.Session != nil {
		gotID = got.Session.ID
	}
	if want.Session != nil {
		wantID = want.Session.ID
	}
	if got.Generation != want.Generation || got.State != want.State || gotID != wantID || got.ArchiveID != want.ArchiveID {
		t.Fatalf("%s: got %s at generation %d with session %q; want %s at %d with %q",
			what, got.State, got.Generation, gotID, want.State, want.Generation, wantID)
	}
}

// touchesNothingElse fails unless an answer left the scopes, the live-entry
// gate, the drain and the admission's generation as they were (B11).
func (b *exitBed) touchesNothingElse(lifetime string, call func()) {
	b.t.Helper()
	scopes, drains := b.scopeEvents(), b.drains
	admission := b.readAdmission(b.t, b.c, lifetime)
	call()
	if after := b.scopeEvents(); !sameStrings(scopes, after) {
		b.t.Fatalf("the answer reached the scopes: %v became %v", scopes, after)
	}
	if b.gate.Closed() || b.drains != drains {
		b.t.Fatalf("the answer sealed live entry or drained (closed %v, drains %d to %d)", b.gate.Closed(), drains, b.drains)
	}
	if after := b.readAdmission(b.t, b.c, lifetime); after != admission {
		b.t.Fatalf("the answer changed the admission: %+v became %+v", admission, after)
	}
}

func count(v *int) int {
	if v == nil {
		return -1
	}
	return *v
}

// ---- A: when the question is asked; D: the count ----

func TestRecovery_TheReadCountsContactsNotDeletedSince(t *testing.T) {
	t.Run("a session of mixed members: the count leaves the deleted one out", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		id := b.view(lifeA).Session.ID
		if v := b.view(lifeA); count(v.Session.ContactCount) != 0 || v.Session.HasContacts {
			t.Fatalf("a new session reads count %d, has_contacts %v; want 0 and false", count(v.Session.ContactCount), v.Session.HasContacts)
		}
		gone := b.contactN(id, 1)
		b.contactN(id, 2)
		b.contactN(id, 3)
		b.exec(`UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, gone)
		if v := b.view(lifeA); count(v.Session.ContactCount) != 2 || !v.Session.HasContacts {
			t.Fatalf("with one of three deleted: count %d, has_contacts %v; want 2 and true", count(v.Session.ContactCount), v.Session.HasContacts)
		}
	})
	t.Run("every contact deleted since: still asked about, showing 0", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		b.exec(`UPDATE qso SET deleted_at = datetime('now')`)
		v := b.view(lifeB)
		if v.State != sessionStateRecoveryPending || count(v.Session.ContactCount) != 0 || !v.Session.HasContacts || v.Generation != q.Generation {
			t.Fatalf("the read says %s, count %d, has_contacts %v; want the question still, with 0 contacts (ruled 2026-11-10)",
				v.State, count(v.Session.ContactCount), v.Session.HasContacts)
		}
	})
}

// ---- B3, B4, B5, B10, B11: the two recovery answers ----

func TestAnswer_New_AsTheRecoveryAnswer(t *testing.T) {
	b, q := newRecoveryBed(t)
	read := b.sessionEvents()
	var a answered
	b.touchesNothingElse(lifeB, func() { a = b.newAs(lifeB, q.Generation) })

	if a.status != http.StatusOK {
		t.Fatalf("New = %d %s; want 200", a.status, a.raw)
	}
	v := a.view
	if v.State != sessionStateOpen || v.Session == nil || v.Session.ID == q.Session.ID || !v.Session.HeldByYou ||
		v.Generation != q.Generation+1 || count(v.Session.ContactCount) != 0 || v.Session.LogbookUUID != b.logbook {
		t.Fatalf("New answered %+v with %+v; want a new session, held by the caller, at generation %d", v, v.Session, q.Generation+1)
	}
	// The remnant is closed, kept with its contact, its sitting's end unconfirmed with no time.
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND closed_at IS NOT NULL AND close_reason = ?`,
		q.Session.ID, sqlite.SessionClosedUnconfirmed); n != 1 {
		t.Fatalf("the remnant is not closed with %s", sqlite.SessionClosedUnconfirmed)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_interval WHERE session_id = ? AND end_state = 'unconfirmed' AND ended_at IS NULL`, q.Session.ID); n != 1 {
		t.Fatal("the remnant's interrupted sitting does not end as unconfirmed with no time")
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_member WHERE session_id = ?`, q.Session.ID); n != 1 {
		t.Fatalf("the remnant holds %d contact(s) after New, want its 1", n)
	}
	if b.openSessions() != 1 || b.sessions() != 2 {
		t.Fatalf("%d open of %d sessions; want the remnant kept and one new one", b.openSessions(), b.sessions())
	}
	if got := read(); len(got) != 1 || got[0] != v.Generation {
		t.Fatalf("events = %v; want one, at generation %d", got, v.Generation)
	}
	sameState(t, "the read after the answer", b.view(lifeB), v)
}

func TestAnswer_Continue_AsTheRecoveryAnswer(t *testing.T) {
	b, q := newRecoveryBed(t)
	read := b.sessionEvents()
	var a answered
	b.touchesNothingElse(lifeB, func() { a = b.continueAs(lifeB, q.Generation, q.Session.ID) })

	if a.status != http.StatusOK {
		t.Fatalf("Continue = %d %s; want 200", a.status, a.raw)
	}
	v := a.view
	if v.State != sessionStateOpen || v.Session == nil || v.Session.ID != q.Session.ID || !v.Session.HeldByYou ||
		v.Generation != q.Generation+1 || count(v.Session.ContactCount) != 1 || !v.Session.OpenedAt.Equal(q.Session.OpenedAt) {
		t.Fatalf("Continue answered %+v with %+v; want the same session, held by the caller, with its contact", v, v.Session)
	}
	// Never closed; the interrupted sitting ends unconfirmed, and a new one is the caller's.
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND closed_at IS NULL AND close_reason IS NULL`, q.Session.ID); n != 1 {
		t.Fatal("the continued session is closed, or has a close reason")
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_interval WHERE session_id = ? AND end_state = 'unconfirmed' AND ended_at IS NULL AND app_lifetime = ?`,
		q.Session.ID, lifeA); n != 1 {
		t.Fatal("the interrupted sitting does not end as unconfirmed with no time")
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_interval WHERE session_id = ? AND end_state = 'open' AND app_lifetime = ?`, q.Session.ID, lifeB); n != 1 {
		t.Fatal("no new sitting is open for the caller")
	}
	if b.sessions() != 1 {
		t.Fatalf("%d sessions after Continue, want the one", b.sessions())
	}
	if got := read(); len(got) != 1 || got[0] != v.Generation {
		t.Fatalf("events = %v; want one, at generation %d", got, v.Generation)
	}
	sameState(t, "the read after the answer", b.view(lifeB), v)
}

// ---- B1, B2: who may answer, and for which archive ----

func TestAnswer_WhoMayAnswer(t *testing.T) {
	for name, tc := range map[string]struct {
		arrange func(b *exitBed)
		who     string
		status  int
		code    string
	}{
		"a request that names no lifetime": {nil, "", http.StatusBadRequest, "app_lifetime_required"},
		"a lifetime replaced in this process": {func(b *exitBed) { b.mustBeAdmitted(lifeC) }, lifeB,
			http.StatusConflict, "app_lifetime_retired"},
		"a lifetime whose exit the file records, after a daemon restart": {func(b *exitBed) {
			if a := b.exitAs(lifeB, b.view(lifeB).Generation, testServedArchive); a.status != http.StatusOK {
				b.t.Fatalf("the fixture's exit: %+v", a)
			}
			b.restart()
		}, lifeB, http.StatusConflict, "app_lifetime_retired"},
		"a lifetime this process has never admitted": {nil, lifeC, http.StatusConflict, "app_lifetime_not_current"},
	} {
		t.Run(name, func(t *testing.T) {
			b, q := newRecoveryBed(t)
			if tc.arrange != nil {
				tc.arrange(b)
			}
			g := b.file("").Generation
			for _, answer := range []func() answered{
				func() answered { return b.newAs(tc.who, g) },
				func() answered { return b.continueAs(tc.who, g, q.Session.ID) },
			} {
				b.untouched("a refused answer", func() {
					if a := answer(); a.status != tc.status || a.code != tc.code {
						t.Fatalf("the answer = %d %s; want %d %s", a.status, a.code, tc.status, tc.code)
					}
				})
			}
			if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND closed_at IS NULL`, q.Session.ID); n != 1 {
				t.Fatal("the remnant is no longer open after refused answers")
			}
		})
	}
}

func TestAnswer_TheArchiveGuardComesFirst(t *testing.T) {
	b, q := newRecoveryBed(t)
	body := fmt.Sprintf(`{"generation":%d}`, q.Generation)
	for name, tc := range map[string]struct {
		archive string
		status  int
		code    string
	}{
		"another archive's page": {otherArchive, http.StatusConflict, "archive_mismatch"},
		"no archive named":       {"", http.StatusBadRequest, "missing_required_field"},
	} {
		t.Run(name, func(t *testing.T) {
			b.untouched("an answer refused by the archive guard", func() {
				if a := b.send(b.control(lifeB, "/v1/operating-session/new", body, tc.archive)); a.status != tc.status || a.code != tc.code {
					t.Fatalf("New = %d %s; want %d %s", a.status, a.code, tc.status, tc.code)
				}
			})
		})
	}
}

func TestAnswer_TheBodyIsChecked(t *testing.T) {
	b, q := newRecoveryBed(t)
	for name, tc := range map[string]struct{ path, body string }{
		"New with no generation":      {"/v1/operating-session/new", `{}`},
		"Continue with no generation": {"/v1/operating-session/continue", fmt.Sprintf(`{"session_id":%q}`, q.Session.ID)},
		"Continue naming no session":  {"/v1/operating-session/continue", fmt.Sprintf(`{"generation":%d}`, q.Generation)},
	} {
		t.Run(name, func(t *testing.T) {
			b.untouched("an answer with a body that is not whole", func() {
				if a := b.send(b.control(lifeB, tc.path, tc.body, testServedArchive)); a.status != http.StatusBadRequest || a.code != "missing_required_field" {
					t.Fatalf("got %d %s; want 400 missing_required_field", a.status, a.code)
				}
			})
		})
	}
}

// A held lifetime whose scopes a failed step left stopped may answer, and the
// answer repairs nothing: that is its own admission's to do (part 2, B4). The
// stopped state is reached the real way: a replacement whose start step fails.
func TestAnswer_AStoppedHeldLifetimeAnswers_AndNothingIsRepaired(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	held := b.view(lifeA)
	restore := b.failSessionWrites()
	if status, _ := b.admitted(lifeB); status != http.StatusServiceUnavailable {
		t.Fatalf("the fixture: the replacement = %d; want 503, leaving A's scopes stopped", status)
	}
	restore()
	if st := b.readAdmission(t, b.c, lifeA); st.Current {
		t.Fatalf("the fixture: A reads %+v; want not current while its scopes are stopped", st)
	}
	wantScopes(t, b.sessionBed, "the fixture", "retire", "publish "+lifeA, "retire")

	var a answered
	b.touchesNothingElse(lifeA, func() { a = b.newAs(lifeA, held.Generation) })

	if a.status != http.StatusOK || a.view.Session == nil || !a.view.Session.HeldByYou || a.view.Session.ID == held.Session.ID {
		t.Fatalf("New by the stopped, held lifetime = %d %s; want it applied", a.status, a.raw)
	}
	wantScopes(t, b.sessionBed, "after the answer", "retire", "publish "+lifeA, "retire")
	if st := b.readAdmission(t, b.c, lifeA); st.Current {
		t.Fatalf("after the answer A reads %+v; the answer must not repair the admission", st)
	}
	// Its own admission still does, and changes no session.
	if status, code, posted := b.shellAdmits(lifeA); !posted || status != http.StatusOK {
		t.Fatalf("the app's own flow afterwards: %d %s, admit sent %v; want an admit that succeeds", status, code, posted)
	}
	sameState(t, "the session after the repair", b.view(lifeA), a.view)
}

// ---- B6, B7: the generation guard ----

func TestAnswer_TheSameAnswerTwiceIsAppliedOnce(t *testing.T) {
	b, q := newRecoveryBed(t)
	first := b.newAs(lifeB, q.Generation)
	if first.status != http.StatusOK {
		t.Fatalf("New = %d %s", first.status, first.raw)
	}
	read := b.sessionEvents()

	// The retry after a lost response carries the generation of the first press.
	var again answered
	b.untouched("the answer repeated", func() { again = b.newAs(lifeB, q.Generation) })

	if again.status != http.StatusConflict || again.code != operatingSessionStale {
		t.Fatalf("the repeat = %d %s; want 409 %s", again.status, again.code, operatingSessionStale)
	}
	sameState(t, "the state the stale refusal returned", again.view, first.view)
	if again.view.Session == nil || !again.view.Session.HeldByYou {
		t.Fatal("the stale refusal does not say the caller holds the session the first attempt opened")
	}
	if len(read()) != 0 || b.sessions() != 2 {
		t.Fatalf("the repeat published %v or left %d sessions; want nothing published and the two sessions of the first", read(), b.sessions())
	}
}

func TestAnswer_TwoAnswersAtOneGeneration_TheFirstAppliedWins(t *testing.T) {
	b, q := newRecoveryBed(t)
	kept := b.continueAs(lifeB, q.Generation, q.Session.ID)
	if kept.status != http.StatusOK {
		t.Fatalf("Continue = %d %s", kept.status, kept.raw)
	}
	var late answered
	b.untouched("the other answer", func() { late = b.newAs(lifeB, q.Generation) })
	if late.status != http.StatusConflict || late.code != operatingSessionStale {
		t.Fatalf("New at the generation Continue used = %d %s; want 409 %s", late.status, late.code, operatingSessionStale)
	}
	sameState(t, "the state the stale refusal returned", late.view, kept.view)
}

// The default-epoch test comes first, and what it closes is committed and
// published even though the answer is then refused as stale (§5: a refusal is
// not a rollback). No sequence reaches this while the daemon serves an
// admitted app: every writer of the default runs at a daemon start or at
// setup's first save. The fixture moves the epoch by hand.
func TestAnswer_TheDefaultEpochTestsCloseIsKeptThoughTheAnswerIsStale(t *testing.T) {
	for name, answer := range map[string]func(b *exitBed, q sessionView) answered{
		"New":      func(b *exitBed, q sessionView) answered { return b.newAs(lifeB, q.Generation) },
		"Continue": func(b *exitBed, q sessionView) answered { return b.continueAs(lifeB, q.Generation, q.Session.ID) },
	} {
		t.Run(name, func(t *testing.T) {
			b, q := newRecoveryBed(t)
			b.exec(`UPDATE archive_metadata SET default_epoch = default_epoch + 1 WHERE singleton = 1`)
			read := b.sessionEvents()

			a := answer(b, q)

			if a.status != http.StatusConflict || a.code != operatingSessionStale {
				t.Fatalf("the answer = %d %s; want 409 %s", a.status, a.code, operatingSessionStale)
			}
			if a.view.State != sessionStateNone || a.view.Session != nil || a.view.Generation != q.Generation+1 {
				t.Fatalf("the stale refusal returned %+v; want no session open, at generation %d", a.view, q.Generation+1)
			}
			if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = ?`, q.Session.ID, sqlite.SessionClosedByDefault); n != 1 || b.openSessions() != 0 {
				t.Fatalf("the remnant is not closed with %s, or a session is open (%d)", sqlite.SessionClosedByDefault, b.openSessions())
			}
			if got := read(); len(got) != 1 || got[0] != q.Generation+1 {
				t.Fatalf("events = %v; want the close published once, at generation %d", got, q.Generation+1)
			}
			sameState(t, "the read after the refusal", b.view(lifeB), a.view)
		})
	}
}

// ---- B8, B9: refusals with a code of their own, and a failure ----

func TestAnswer_RefusalsWithACodeOfTheirOwnChangeNothing(t *testing.T) {
	t.Run("Continue naming a session that is not eligible", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		own := b.view(lifeA)
		for what, id := range map[string]string{"the session the caller already holds": own.Session.ID, "a session that does not exist": "01a1f83c-da20-751f-80b7-ec9a3f1f93f3"} {
			b.untouched("Continue naming "+what, func() {
				if a := b.continueAs(lifeA, own.Generation, id); a.status != http.StatusConflict || a.code != "operating_session_ineligible" {
					t.Fatalf("Continue naming %s = %d %s; want 409 operating_session_ineligible", what, a.status, a.code)
				}
			})
		}
	})
	t.Run("Continue naming a closed session while a remnant awaits its answer", func(t *testing.T) {
		// S0: closed by A's exit, with a contact. S1: B's, with a contact, left open by a restart. C asks.
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		s0 := b.view(lifeA)
		b.contactN(s0.Session.ID, 1)
		if a := b.exitAs(lifeA, s0.Generation, testServedArchive); a.status != http.StatusOK {
			t.Fatalf("the fixture's exit: %+v", a)
		}
		b.mustBeAdmitted(lifeB)
		b.contactN(b.view(lifeB).Session.ID, 2)
		b.restart()
		b.mustBeAdmitted(lifeC)
		q := b.view(lifeC)
		if q.State != sessionStateRecoveryPending {
			t.Fatalf("the fixture: C reads %s", q.State)
		}
		// Any session but the remnant, one that does not exist included: the
		// question is answered first.
		for what, id := range map[string]string{"a closed session": s0.Session.ID, "a session that does not exist": "01a1f83c-da20-751f-80b7-ec9a3f1f93f3"} {
			b.untouched("Continue of "+what+" before the question is answered", func() {
				if a := b.continueAs(lifeC, q.Generation, id); a.status != http.StatusConflict || a.code != "operating_session_recovery_pending" {
					t.Fatalf("Continue of %s = %d %s; want 409 operating_session_recovery_pending: the question is answered first", what, a.status, a.code)
				}
			})
		}
	})
	t.Run("New with no default logbook", func(t *testing.T) {
		b := newExitBed(t, false)
		b.mustBeAdmitted(lifeA)
		b.untouched("New before setup", func() {
			if a := b.newAs(lifeA, b.view(lifeA).Generation); a.status != http.StatusConflict || a.code != "operating_session_setup_pending" {
				t.Fatalf("got %d %s; want 409 operating_session_setup_pending", a.status, a.code)
			}
		})
	})
	t.Run("either answer in the closing phase of an archive switch", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		if err := b.switchClose(); err != nil {
			t.Fatalf("the switch's close: %v", err)
		}
		g := b.file("").Generation
		for name, answer := range map[string]func() answered{
			"New":      func() answered { return b.newAs(lifeB, g) },
			"Continue": func() answered { return b.continueAs(lifeB, g, q.Session.ID) },
		} {
			b.untouched(name+" in the closing phase", func() {
				if a := answer(); a.status != http.StatusConflict || a.code != "operating_session_switching" {
					t.Fatalf("%s in the closing phase = %d %s; want 409 operating_session_switching", name, a.status, a.code)
				}
			})
		}
		if v := b.view(lifeB); v.State != sessionStateArchiveSwitching {
			t.Fatalf("after the refusals the read says %s; want the phase unchanged", v.State)
		}
	})
}

func TestAnswer_AFailureChangesNothingAndTheQuestionStands(t *testing.T) {
	b, q := newRecoveryBed(t)
	restore := b.failSessionWrites()
	for name, answer := range map[string]func() answered{
		"New":      func() answered { return b.newAs(lifeB, q.Generation) },
		"Continue": func() answered { return b.continueAs(lifeB, q.Generation, q.Session.ID) },
	} {
		b.untouched(name+" while the file cannot be written", func() {
			if a := answer(); a.status != http.StatusServiceUnavailable || a.code != operatingSessionUnavailable {
				t.Fatalf("%s = %d %s; want 503 %s", name, a.status, a.code, operatingSessionUnavailable)
			}
		})
	}
	restore()
	sameState(t, "the question after the failures", b.view(lifeB), q)
	// The same press, at the generation it was first made at, is then applied.
	if a := b.continueAs(lifeB, q.Generation, q.Session.ID); a.status != http.StatusOK {
		t.Fatalf("the answer pressed again = %d %s; want it applied", a.status, a.raw)
	}
}

// ---- C: the routes whole (operator ruling 2026-11-23) ----

func TestAnswer_New_ByTheLifetimeThatHoldsTheSession(t *testing.T) {
	t.Run("a session with a contact is closed and kept, its end confirmed", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		old := b.view(lifeA)
		b.contactN(old.Session.ID, 1)
		a := b.newAs(lifeA, old.Generation)
		if a.status != http.StatusOK || a.view.Session == nil || a.view.Session.ID == old.Session.ID || !a.view.Session.HeldByYou {
			t.Fatalf("New = %d %s; want a new session held by the caller", a.status, a.raw)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session s JOIN operating_session_interval i ON i.session_id = s.id
			WHERE s.id = ? AND s.close_reason = ? AND i.end_state = 'confirmed' AND i.ended_at IS NOT NULL`, old.Session.ID, sqlite.SessionClosedByNew); n != 1 {
			t.Fatalf("the earlier session is not closed with %s and a confirmed end", sqlite.SessionClosedByNew)
		}
	})
	t.Run("an empty session is discarded", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		old := b.view(lifeA)
		if a := b.newAs(lifeA, old.Generation); a.status != http.StatusOK {
			t.Fatalf("New = %d %s", a.status, a.raw)
		}
		if b.sessions() != 1 || b.openSessions() != 1 {
			t.Fatalf("%d sessions, %d open; want the empty one removed and the new one open", b.sessions(), b.openSessions())
		}
	})
}

func TestAnswer_Continue_OfAClosedSessionOfTheDefaultLogbook(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	s0 := b.view(lifeA)
	b.contactN(s0.Session.ID, 1)
	s1 := b.newAs(lifeA, s0.Generation)
	if s1.status != http.StatusOK {
		t.Fatalf("the fixture's New: %d %s", s1.status, s1.raw)
	}
	b.contactN(s1.view.Session.ID, 2)
	read := b.sessionEvents()

	a := b.continueAs(lifeA, s1.view.Generation, s0.Session.ID)

	if a.status != http.StatusOK || a.view.Session == nil || a.view.Session.ID != s0.Session.ID || !a.view.Session.HeldByYou || count(a.view.Session.ContactCount) != 1 {
		t.Fatalf("Continue = %d %s; want the earlier session open again, held by the caller, with its one contact", a.status, a.raw)
	}
	// Contacts never move: the session that was current is closed, and keeps its own.
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = ?`, s1.view.Session.ID, sqlite.SessionClosedByContinue); n != 1 {
		t.Fatalf("the session that was current is not closed with %s", sqlite.SessionClosedByContinue)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_member WHERE session_id = ?`, s1.view.Session.ID); n != 1 {
		t.Fatalf("the session that was current holds %d contact(s), want its own 1", n)
	}
	if got := read(); len(got) != 1 || got[0] != a.view.Generation {
		t.Fatalf("events = %v; want one, at generation %d", got, a.view.Generation)
	}
}

// ---- G: restarts ----

func TestRecovery_ADaemonRestartLeavesTheQuestionOrTheAnswerAsItWas(t *testing.T) {
	t.Run("while the question shows: it still stands, at the generation the file holds", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		b.restart()
		b.mustBeAdmitted(lifeB)
		sameState(t, "the question after the restart", b.view(lifeB), q)
	})
	t.Run("after an answer committed and before its response: a session the app holds, and no question", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		a := b.continueAs(lifeB, q.Generation, q.Session.ID) // the response is taken as lost
		b.restart()
		b.mustBeAdmitted(lifeB)
		v := b.view(lifeB)
		sameState(t, "the read after the restart", v, a.view)
		if !v.Session.HeldByYou || b.sessions() != 1 {
			t.Fatalf("held by the app %v, %d sessions; want the one session, held", v.Session.HeldByYou, b.sessions())
		}
	})
	t.Run("a confirmed exit after an answer is an ordinary close", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		a := b.continueAs(lifeB, q.Generation, q.Session.ID)
		if x := b.exitAs(lifeB, a.view.Generation, testServedArchive); x.status != http.StatusOK {
			t.Fatalf("the exit: %+v", x)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = ?`, q.Session.ID, sqlite.SessionClosedByExit); n != 1 {
			t.Fatalf("the session is not closed with %s", sqlite.SessionClosedByExit)
		}
	})
}

// After a daemon restart no lifetime is current until the shell admits its own
// again. The page's stream reconnects first and reads in that interval. The
// read must not tell the app that holds the session that the session awaits an
// answer: admitting the same lifetime again changes nothing in the file, so no
// generation advances and no event follows to correct a question once drawn,
// and "Start a new session" pressed on it would close the session the running
// app holds (the clean-room review of 2c362c3d). The state is named for the
// caller (part 2, D1): a session the caller holds is open to it.
func TestRecovery_ADaemonRestartDoesNotAskTheAppAboutItsOwnSession(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	held := b.view(lifeA)
	b.contactN(held.Session.ID, 1)
	b.restart() // a new process: nobody is current yet
	read := b.sessionEvents()

	v := b.view(lifeA)
	if v.State != sessionStateOpen || v.Session == nil || v.Session.ID != held.Session.ID || !v.Session.HeldByYou {
		t.Fatalf("before it is admitted again the holder reads %s (held by it: %v); want open: it is asked about its own session",
			v.State, v.Session != nil && v.Session.HeldByYou)
	}
	// Anyone else is still told that a session awaits its answer.
	for who, lifetime := range map[string]string{"another lifetime": lifeB, "a caller with no lifetime": ""} {
		if w := b.view(lifetime); w.State != sessionStateRecoveryPending || w.Session == nil || w.Session.HeldByYou {
			t.Fatalf("%s reads %s; want %s, not held by it", who, w.State, sessionStateRecoveryPending)
		}
	}

	// The shell admits the same lifetime again: nothing changes and nothing is published.
	b.mustBeAdmitted(lifeA)
	sameState(t, "the holder's read after it is admitted again", b.view(lifeA), v)
	if got := read(); len(got) != 0 || b.file(lifeA).Generation != held.Generation {
		t.Fatalf("events %v, generation %d; want none and %d: nothing would ever have corrected a wrong reading", got, b.file(lifeA).Generation, held.Generation)
	}
}
