package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
)

// A session control in the one order (W-0029 part 4, F; part 2, G; part 3, I).
// A control starts at the admission's order, as an app start, an exit report
// and setup's first save do, so any two are wholly one before the other.
//
// Each case holds one side at a point the test sees and observes the other
// parked where it takes the order, having answered nothing (testbarrier). An
// exit is held in its drain. A control or an app start is held at the archive's
// write lock, inside the order: the test sees the order held before it sends
// the other side. The cases against an archive switch and a default change are
// in cmd/smd, through a real daemon.

const answerTakesOrder = "(*Server).answerOperatingSession"

// holdWrites takes the archive's write lock on a connection of the test's own:
// a control, or an app start's session step, then waits at it, holding the
// admission's order.
func (b *exitBed) holdWrites() (release func()) {
	b.t.Helper()
	ctx := context.Background()
	conn, err := b.raw.Conn(ctx)
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		b.t.Fatal(err)
	}
	done := false
	release = func() {
		if done {
			return
		}
		done = true
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		_ = conn.Close()
	}
	b.t.Cleanup(release)
	return release
}

// orderIsHeld returns once the admission's order is held by the operation the
// test has just sent, which is waiting at the write lock inside it.
func (b *exitBed) orderIsHeld(what string) {
	b.t.Helper()
	a := &b.srv.admission
	deadline := time.Now().Add(20 * time.Second)
	for a.mu.TryLock() {
		a.mu.Unlock()
		if time.Now().After(deadline) {
			b.t.Fatalf("%s never took the admission's order", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (b *exitBed) answerAsync(answer func() answered) <-chan answered {
	ch := make(chan answered, 1)
	go func() { ch <- answer() }()
	return ch
}

// startWithoutTheDatabase is an app start, its read and then its admit, sent
// as bare requests. The bed's usual helper reads the default logbook through
// the server's database service, for a header these two routes do not look at.
// This fixture gives that service ONE connection (testDatastoreConfig), and a
// control held at the write lock has it pinned: the helper's read cannot begin,
// the start is never sent, and the test, which releases the lock only once it
// has seen the start at the order, waits for ever (Codex's reading of the
// source, codex-224). It is the fixture's pool and the test's own release
// order, not a starved read in a daemon.
func (b *exitBed) startWithoutTheDatabase(lifetime string) <-chan admitAnswer {
	ch := make(chan admitAnswer, 1)
	go func() {
		read := b.send(b.bare(lifetime, http.MethodGet, "/v1/app/lifetime", ""))
		var st admissionState
		if read.status != http.StatusOK || json.Unmarshal(read.raw, &st) != nil {
			ch <- admitAnswer{read.status, "the read: " + read.code}
			return
		}
		admit := b.send(b.bare(lifetime, http.MethodPost, "/v1/app/lifetime",
			fmt.Sprintf(`{"daemon_instance":%q,"generation":%d}`, st.Instance, st.Generation)))
		ch <- admitAnswer{admit.status, admit.code}
	}()
	return ch
}

// bare is a request as the app listener accepts it, with nothing read from the
// database to build it.
func (b *exitBed) bare(lifetime, method, path, body string) *http.Request {
	r := b.control(lifetime, path, body, testServedArchive)
	r.Method = method
	if body == "" {
		r.Body, r.ContentLength = http.NoBody, 0
		r.Header.Del("Content-Type")
	}
	return r
}

func (b *exitBed) remnantOpen(id string) bool {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND closed_at IS NULL`, id) == 1
}

// F1: an answer against an exit of the same app.
func TestAnswerOrder_F1_AnAnswerAgainstAnExitOfTheSameApp(t *testing.T) {
	t.Run("the exit first: the answer is refused, and the question is left for the next start", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		entered, release := b.heldInDrain()
		exit := b.exitAsync(lifeB, q.Generation)
		arrived(t, "the exit to reach its drain", entered)

		w := orderWatch(t, answerTakesOrder)
		answer := b.answerAsync(func() answered { return b.continueAs(lifeB, q.Generation, q.Session.ID) })
		shutOut(w, "the answer, with the app's exit under way", answer)

		release()
		if x := finished(t, "the exit", exit); x.status != http.StatusOK {
			t.Fatalf("the exit: %+v", x)
		}
		if a := finished(t, "the answer", answer); a.status != http.StatusConflict || a.code != "app_lifetime_retired" {
			t.Fatalf("the answer after the exit = %d %s; want 409 app_lifetime_retired", a.status, a.code)
		}
		if !b.remnantOpen(q.Session.ID) {
			t.Fatal("the remnant was closed though no answer was applied")
		}
		b.mustBeAdmitted(lifeC)
		if v := b.view(lifeC); v.State != sessionStateRecoveryPending || v.Session.ID != q.Session.ID {
			t.Fatalf("the next start reads %s; want the question again, for the same session", v.State)
		}
	})
	t.Run("the answer first: the exit then closes the session the answer left to this app", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		release := b.holdWrites()
		answer := b.answerAsync(func() answered { return b.continueAs(lifeB, q.Generation, q.Session.ID) })
		b.orderIsHeld("the answer")

		w := orderWatch(t, exitTakesOrder)
		exit := b.exitAsync(lifeB, q.Generation)
		shutOut(w, "the exit, with the answer under way", exit)

		release()
		a := finished(t, "the answer", answer)
		if a.status != http.StatusOK {
			t.Fatalf("the answer = %d %s; want it applied", a.status, a.raw)
		}
		// The exit named the generation from before the answer: by its own
		// rule it is sent again at the one it is given (part 2, E).
		x := finished(t, "the exit", exit)
		if x.status != http.StatusConflict || x.code != operatingSessionStale || x.Generation != a.view.Generation {
			t.Fatalf("the exit after the answer: %+v; want 409 %s at generation %d", x, operatingSessionStale, a.view.Generation)
		}
		if again := b.exitAs(lifeB, x.Generation, testServedArchive); again.status != http.StatusOK {
			t.Fatalf("the exit sent again: %+v", again)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = ?`, q.Session.ID, sqlite.SessionClosedByExit); n != 1 {
			t.Fatalf("the session the answer continued is not closed with %s", sqlite.SessionClosedByExit)
		}
	})
}

// F2: an answer against a second app start.
func TestAnswerOrder_F2_AnAnswerAgainstASecondAppStart(t *testing.T) {
	t.Run("the start first: the first app's answer is refused, and the second has the question", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		release := b.holdWrites()
		start := b.admitAsync(lifeC)
		b.orderIsHeld("the second app's start")

		w := orderWatch(t, answerTakesOrder)
		answer := b.answerAsync(func() answered { return b.newAs(lifeB, q.Generation) })
		shutOut(w, "the first app's answer, with the second app's start under way", answer)

		release()
		if s := finished(t, "the second app's start", start); s.status != http.StatusOK {
			t.Fatalf("the second app's start: %+v", s)
		}
		if a := finished(t, "the first app's answer", answer); a.status != http.StatusConflict || a.code != "app_lifetime_retired" {
			t.Fatalf("the first app's answer = %d %s; want 409 app_lifetime_retired", a.status, a.code)
		}
		if v := b.view(lifeC); v.State != sessionStateRecoveryPending || v.Session.ID != q.Session.ID || !b.remnantOpen(q.Session.ID) {
			t.Fatalf("the second app reads %s; want the question, for the remnant still open", v.State)
		}
	})
	t.Run("the answer first: the second start meets a session with contacts held by another, and asks", func(t *testing.T) {
		b, q := newRecoveryBed(t)
		release := b.holdWrites()
		answer := b.answerAsync(func() answered { return b.continueAs(lifeB, q.Generation, q.Session.ID) })
		b.orderIsHeld("the answer")

		w := orderWatch(t, admissionReadsOrder)
		start := b.startWithoutTheDatabase(lifeC)
		shutOut(w, "the second app's start, with the answer under way", start)

		release()
		a := finished(t, "the answer", answer)
		if a.status != http.StatusOK {
			t.Fatalf("the answer = %d %s; want it applied", a.status, a.raw)
		}
		if s := finished(t, "the second app's start", start); s.status != http.StatusOK {
			t.Fatalf("the second app's start: %+v", s)
		}
		v := b.view(lifeC)
		if v.State != sessionStateRecoveryPending || v.Session.ID != q.Session.ID || v.Session.HeldByYou || v.Generation != a.view.Generation {
			t.Fatalf("the second app reads %+v with %+v; want the question about the session the first app continued", v, v.Session)
		}
	})
}

// F5: an answer against an answer, at one generation.
func TestAnswerOrder_F5_AnAnswerAgainstAnAnswer(t *testing.T) {
	b, q := newRecoveryBed(t)
	release := b.holdWrites()
	first := b.answerAsync(func() answered { return b.newAs(lifeB, q.Generation) })
	b.orderIsHeld("the first answer")

	w := orderWatch(t, answerTakesOrder)
	second := b.answerAsync(func() answered { return b.continueAs(lifeB, q.Generation, q.Session.ID) })
	shutOut(w, "the second answer, with the first under way", second)

	release()
	applied := finished(t, "the first answer", first)
	if applied.status != http.StatusOK {
		t.Fatalf("the first answer = %d %s; want it applied", applied.status, applied.raw)
	}
	late := finished(t, "the second answer", second)
	if late.status != http.StatusConflict || late.code != operatingSessionStale {
		t.Fatalf("the second answer = %d %s; want 409 %s", late.status, late.code, operatingSessionStale)
	}
	sameState(t, "the state the second answer was given", late.view, applied.view)
	if b.remnantOpen(q.Session.ID) || b.openSessions() != 1 || b.sessions() != 2 {
		t.Fatalf("remnant open %v, %d open of %d; want New's result alone", b.remnantOpen(q.Session.ID), b.openSessions(), b.sessions())
	}
}

// F6: the setup-save boundary. Before the default exists New has nothing to
// bind to; once the save has opened the first session, a control carrying the
// earlier generation is stale.
func TestAnswerOrder_F6_SetupsFirstSaveIsABoundary(t *testing.T) {
	b := newSetupBed(t, 1)
	b.admit(lifeA)
	before, err := b.srv.db.OperatingSessionStateWithContext(context.Background(), lifeA)
	if err != nil {
		t.Fatal(err)
	}
	pressNew := func() controlResult {
		return b.srv.answerOperatingSession(lifeA, func(ctx context.Context, lifetime string, now time.Time) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
			return b.srv.db.NewOperatingSessionWithContext(ctx, lifetime, before.Generation, now)
		})
	}
	if res := pressNew(); res.status != http.StatusConflict || res.code != operatingSessionSetupPending {
		t.Fatalf("New before setup: %+v; want 409 %s", res, operatingSessionSetupPending)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 0 {
		t.Fatalf("%d session(s) before setup", n)
	}

	if status, code := b.save(); status != http.StatusOK {
		t.Fatalf("the save: %d %s", status, code)
	}
	b.wantComplete(lifeA)

	res := pressNew() // at the generation from before the save
	if res.status != http.StatusConflict || res.code != operatingSessionStale {
		t.Fatalf("New at the generation from before the save: %+v; want 409 %s", res, operatingSessionStale)
	}
	if res.state.State != sessionStateOpen || res.state.Session == nil || !res.state.Session.HeldByYou {
		t.Fatalf("the stale refusal returned %+v; want the session the save opened, held by the app", res.state)
	}
	b.wantComplete(lifeA)
}
