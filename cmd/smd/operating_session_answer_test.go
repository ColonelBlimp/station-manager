package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

/*
   W-0029 part 4, F3 and F4: a session control (the answer to the recovery
   question) against an archive switch and against a change of the default
   logbook, through a real daemon: its real archive manager, its seals, its
   admission and its routes.

   A switch can end four ways for a control that was asked for while it was
   under way, and each leaves the control a different answer. The control is
   seen parked where it takes the admission's order while the switch holds it
   (testbarrier); the switch is held in its drain, after its seals and before
   its close.
*/

const controlTakesOrder = "(*Server).answerOperatingSession"

// of makes an httpResult of a route helper's two return values.
func (httpResult) of(code int, body []byte) httpResult { return httpResult{code, body} }

// controlAnswer is what a control answered: its status, its code, and the
// state it gave (a 200's, or a stale refusal's).
type controlAnswer struct {
	code      int
	errorCode string
	state     string
	heldByYou bool
	session   string
	gen       int64
}

func parseControl(t *testing.T, r httpResult) controlAnswer {
	t.Helper()
	var body struct {
		Code    string `json:"code"`
		Wrapped *struct {
			Generation int64  `json:"generation"`
			State      string `json:"state"`
			Session    *struct {
				ID        string `json:"id"`
				HeldByYou bool   `json:"held_by_you"`
			} `json:"session"`
		} `json:"operating_session"`
		Generation int64  `json:"generation"`
		State      string `json:"state"`
		Session    *struct {
			ID        string `json:"id"`
			HeldByYou bool   `json:"held_by_you"`
		} `json:"session"`
	}
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatalf("a control answered %d %s: %v", r.code, r.body, err)
	}
	a := controlAnswer{code: r.code, errorCode: body.Code, state: body.State, gen: body.Generation}
	s := body.Session
	if body.Wrapped != nil {
		a.state, a.gen, s = body.Wrapped.State, body.Wrapped.Generation, body.Wrapped.Session
	}
	if s != nil {
		a.session, a.heldByYou = s.ID, s.HeldByYou
	}
	return a
}

func (b *orderBed) pressContinue(t *testing.T, lifetime string, generation int64, session string) (int, []byte) {
	t.Helper()
	return b.as(t, lifetime, http.MethodPost, "/v1/operating-session/continue",
		fmt.Sprintf(`{"generation":%d,"session_id":%q}`, generation, session))
}

func (b *orderBed) pressNew(t *testing.T, lifetime string, generation int64) (int, []byte) {
	t.Helper()
	return b.as(t, lifetime, http.MethodPost, "/v1/operating-session/new", fmt.Sprintf(`{"generation":%d}`, generation))
}

// text reads one value from the archive's file on a connection of the test's own.
func (b *orderBed) text(t *testing.T, q string, args ...any) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+b.d.paths.QSO+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var v string
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

// newRecoveringBed is the daemon in the state the recovery question is asked
// in: lifetime A's session with one contact, the daemon stopped and started
// again (an ordinary start: nothing is closed), and lifetime B admitted. B is
// current and holds none. It returns the generation and the remnant's id.
func newRecoveringBed(t *testing.T) (b *orderBed, generation int64, remnant string) {
	t.Helper()
	b = newOrderBed(t)
	b.started(t)
	b.contact(t)
	remnant = b.text(t, `SELECT id FROM operating_session WHERE closed_at IS NULL`)
	b.f.stop(t, b.d)
	b.d = b.f.mustStart(t)
	b.mustAdmit(t, orderLifeB)
	b.stream(t, orderLifeB)
	generation, state := b.session(t, orderLifeB)
	if state != "recovery_pending" {
		t.Fatalf("the fixture: after the restart B reads %q, want recovery_pending", state)
	}
	return b, generation, remnant
}

// ---- F3: a control against an archive switch, each way the switch can end ----

func TestAnswerOrder_F3_AnAnswerAgainstAnArchiveSwitch(t *testing.T) {
	// queued asks for the switch, holds it in its drain, and sends Continue
	// behind it: the control is seen parked at the order.
	queued := func(t *testing.T, b *orderBed, gen int64, remnant string) (activation, control chan httpResult, letGo func()) {
		t.Helper()
		other := b.provision(t)
		activation, letGo = b.heldSwitch(t, other)
		w := orderWatch(t, controlTakesOrder)
		control = async(func() (int, []byte) { return b.pressContinue(t, orderLifeB, gen, remnant) })
		parkedAtOrder(w, "the answer, with a switch under way", control)
		if b.openSessions(t) != 1 {
			t.Fatal("the fixture: the remnant is not open while the switch is held before its close")
		}
		return activation, control, letGo
	}

	t.Run("the answer first: the switch closes what the answer left", func(t *testing.T) {
		b, gen, remnant := newRecoveringBed(t)
		other := b.provision(t)
		a := parseControl(t, httpResult{}.of(b.pressContinue(t, orderLifeB, gen, remnant)))
		if a.code != http.StatusOK || a.session != remnant || !a.heldByYou {
			t.Fatalf("the answer = %+v; want the remnant continued, held by the app", a)
		}
		if code, body := b.activate(t, other); !accepted(httpResult{code, body}) {
			t.Fatalf("the switch = %d %s; want it accepted", code, body)
		}
		if b.openSessions(t) != 0 || b.closedAs(t, "archive_switched") != 1 {
			t.Fatalf("%d open, %d closed as archive_switched; want the continued session closed by the switch", b.openSessions(t), b.closedAs(t, "archive_switched"))
		}
	})
	t.Run("the switch first, and it succeeds: the answer is refused in the closing phase", func(t *testing.T) {
		b, gen, remnant := newRecoveringBed(t)
		activation, control, letGo := queued(t, b, gen, remnant)
		letGo()
		if r := answered(t, "the switch", activation); !accepted(r) {
			t.Fatalf("the switch = %d %s; want it accepted", r.code, r.body)
		}
		a := parseControl(t, answered(t, "the answer", control))
		if a.code != http.StatusConflict || a.errorCode != "operating_session_switching" {
			t.Fatalf("the answer = %+v; want 409 operating_session_switching", a)
		}
		if b.openSessions(t) != 0 || b.closedAs(t, "archive_switched") != 1 {
			t.Fatalf("%d open, %d closed as archive_switched; want the switch's close alone", b.openSessions(t), b.closedAs(t, "archive_switched"))
		}
	})
	t.Run("the switch first, and it fails before its close: the waiting answer is applied", func(t *testing.T) {
		b, gen, remnant := newRecoveringBed(t)
		// Only the switch's own close is refused; the answer writes no close reason.
		b.archiveExec(t, `CREATE TRIGGER fail_switch_close BEFORE UPDATE OF close_reason ON operating_session
			WHEN NEW.close_reason = 'archive_switched'
			BEGIN SELECT RAISE(ABORT, 'forced: the switch cannot close the session'); END`)
		activation, control, letGo := queued(t, b, gen, remnant)
		letGo()
		if r := answered(t, "the switch", activation); r.code != http.StatusServiceUnavailable || !strings.Contains(string(r.body), "session_close_failed") {
			t.Fatalf("the switch = %d %s; want 503 session_close_failed", r.code, r.body)
		}
		a := parseControl(t, answered(t, "the answer", control))
		if a.code != http.StatusOK || a.session != remnant || !a.heldByYou || a.gen != gen+1 {
			t.Fatalf("the answer = %+v; want it applied to the remnant, at generation %d", a, gen+1)
		}
	})
	t.Run("the switch first, and it fails after its close: the answer is stale and is given the session the switch opened", func(t *testing.T) {
		b, gen, remnant := newRecoveringBed(t)
		activation, control, letGo := queued(t, b, gen, remnant)
		var r httpResult
		b.f.configDir(t, 0o500, func() {
			letGo()
			r = answered(t, "the switch", activation)
		})
		if !strings.Contains(string(r.body), "activation_persist_failed") {
			t.Fatalf("the switch = %d %s; want activation_persist_failed", r.code, r.body)
		}
		a := parseControl(t, answered(t, "the answer", control))
		if a.code != http.StatusConflict || a.errorCode != "operating_session_stale" {
			t.Fatalf("the answer = %+v; want 409 operating_session_stale", a)
		}
		if a.state != "open" || !a.heldByYou || a.session == remnant || a.session == "" {
			t.Fatalf("the stale refusal gave %+v; want the session the failed switch opened, held by this app", a)
		}
		if b.closedAs(t, "archive_switched") != 1 || b.openSessions(t) != 1 {
			t.Fatalf("%d closed as archive_switched, %d open; want the switch's close and its one new session", b.closedAs(t, "archive_switched"), b.openSessions(t))
		}
	})
	t.Run("the same, where that session could not be opened: stale, no session, the scopes left stopped", func(t *testing.T) {
		b, gen, remnant := newRecoveringBed(t)
		b.archiveExec(t, `CREATE TRIGGER fail_open BEFORE INSERT ON operating_session
			BEGIN SELECT RAISE(ABORT, 'forced: no session can be opened'); END`)
		activation, control, letGo := queued(t, b, gen, remnant)
		var r httpResult
		b.f.configDir(t, 0o500, func() {
			letGo()
			r = answered(t, "the switch", activation)
		})
		if !strings.Contains(string(r.body), "activation_persist_failed") {
			t.Fatalf("the switch = %d %s; want activation_persist_failed", r.code, r.body)
		}
		a := parseControl(t, answered(t, "the answer", control))
		if a.code != http.StatusConflict || a.errorCode != "operating_session_stale" || a.state != "none" || a.session != "" {
			t.Fatalf("the answer = %+v; want 409 operating_session_stale with no session open", a)
		}
		// The answer repaired nothing: the app still reads as not current.
		if rc, raw := b.as(t, orderLifeB, http.MethodGet, "/v1/app/lifetime", ""); rc != http.StatusOK || !strings.Contains(string(raw), `"current":false`) {
			t.Fatalf("the app's read after the answer = %d %s; want current false: its own admission repairs it", rc, raw)
		}
		if b.openSessions(t) != 0 {
			t.Fatal("a session is open though none could be opened")
		}
	})
}

// ---- F4: a control against a change of the default logbook ----

// changeStillHolds: the accepted default change's seals are as it left them.
// A live submit is refused in its words and stores nothing, and TX admission
// is sealed. (requireChangeSealsHeld assumes a logbook with no contact; these
// beds have the remnant's.)
func (b *orderBed) changeStillHolds(t *testing.T, who, when string) {
	t.Helper()
	before := b.f.qsosIn(t, b.f.a)
	if code, body := b.f.submit(t, b.d, b.f.a, "1215"); code != http.StatusConflict || !strings.Contains(string(body), "default_change_pending") {
		t.Fatalf("%s a live submit = %d %s; want 409 default_change_pending", when, code, body)
	}
	if n := b.f.qsosIn(t, b.f.a); n != before {
		t.Fatalf("%s a refused live submit stored %d QSO(s)", when, n-before)
	}
	if !b.txSealed(who) {
		t.Fatalf("%s TX admission is not sealed", when)
	}
}

func TestAnswerOrder_F4_AnAnswerAgainstADefaultChange(t *testing.T) {
	t.Run("the change in flight first: the answer waits for it and is then applied", func(t *testing.T) {
		b, gen, remnant := newRecoveringBed(t)
		held, change, release := b.f.draining(t, b.d) // the change holds the flight and the order, waiting for an admitted submit

		w := orderWatch(t, controlTakesOrder)
		control := async(func() (int, []byte) { return b.pressContinue(t, orderLifeB, gen, remnant) })
		parkedAtOrder(w, "the answer, with a default change in flight", control)

		release()
		if r := answered(t, "the held submit", held); r.code != http.StatusCreated && r.code != http.StatusOK {
			t.Fatalf("the held submit = %d %s; want it logged", r.code, r.body)
		}
		if r := answered(t, "the default change", change); r.code != http.StatusAccepted {
			t.Fatalf("the default change = %d %s; want 202", r.code, r.body)
		}
		a := parseControl(t, answered(t, "the answer", control))
		if a.code != http.StatusOK || a.session != remnant || !a.heldByYou {
			t.Fatalf("the answer after the change = %+v; want it applied, not refused for the change", a)
		}
		// The change is pending, awaiting its restart: a control is still not refused for it.
		if n := parseControl(t, httpResult{}.of(b.pressNew(t, orderLifeB, a.gen))); n.code != http.StatusOK || n.session == remnant || !n.heldByYou {
			t.Fatalf("New under a pending default change = %+v; want it applied", n)
		}
		b.changeStillHolds(t, orderLifeB, "after the controls")
	})
	t.Run("the answer first: the change is then asked for and accepted as today", func(t *testing.T) {
		b, gen, remnant := newRecoveringBed(t)
		a := parseControl(t, httpResult{}.of(b.pressContinue(t, orderLifeB, gen, remnant)))
		if a.code != http.StatusOK {
			t.Fatalf("the answer = %+v; want it applied", a)
		}
		b.accepted(t)
		b.changeStillHolds(t, orderLifeB, "after the answer and then the change")
		if g, state := b.session(t, orderLifeB); state != "open" || g != a.gen {
			t.Fatalf("after the change was accepted the session is %q at %d; want the continued one unchanged at %d until the restart", state, g, a.gen)
		}
	})
}
