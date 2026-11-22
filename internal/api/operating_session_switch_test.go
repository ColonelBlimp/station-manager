package api

import (
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
)

// The operating session at an archive switch (ADR 0100 §4; W-0029 part 3,
// criteria D to G). The archive manager calls the session's part with the
// session order held; these tests call it the same way. Every case is judged on
// the file's rows and the admission's state together.

// switchClose is the session's part of a switch up to its close, as the archive
// manager runs it: the order held, the drain, then the close.
func (b *exitBed) switchClose() error {
	b.t.Helper()
	release := b.srv.HoldSessionOrder()
	defer release()
	p := b.srv.SwitchSession()
	p.Drain()
	return p.Close()
}

// switchFailed is the end of a switch that failed after its close.
func (b *exitBed) switchFailed() {
	b.t.Helper()
	release := b.srv.HoldSessionOrder()
	defer release()
	b.srv.SwitchSession().Failed()
}

func (b *exitBed) sessions() int {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM operating_session`)
}

func (b *exitBed) openSessions() int {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`)
}

// sessionEvents subscribes to the hub and returns a reader of the generations
// published as operating_session.changed so far.
func (b *exitBed) sessionEvents() (read func() []int64) {
	b.t.Helper()
	ch, cancel := b.srv.hub.Subscribe()
	b.t.Cleanup(cancel)
	return func() []int64 {
		var out []int64
		for {
			select {
			case ev := <-ch:
				if ev.Name == events.NameOperatingSessionChanged {
					out = append(out, ev.Payload.(events.OperatingSessionChangedPayload).Generation)
				}
			default:
				return out
			}
		}
	}
}

func TestSwitchSession_TheCloseEndsTheSessionWhoeverHoldsIt(t *testing.T) {
	t.Run("a session with a contact is closed, its end confirmed, after the drain", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		held := b.view(lifeA)
		b.contact(held.Session.ID)
		read := b.sessionEvents()
		openInDrain := -1
		b.inDrain = func() { openInDrain = b.openSessions() }

		if err := b.switchClose(); err != nil {
			t.Fatalf("close: %v", err)
		}

		if b.drains != 1 || openInDrain != 1 {
			t.Fatalf("drains %d, sessions open during the drain %d; want one drain, with the session still open", b.drains, openInDrain)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = ? AND closed_at IS NOT NULL`,
			held.Session.ID, sqlite.SessionClosedBySwitch); n != 1 {
			t.Fatalf("the session is not closed with %s", sqlite.SessionClosedBySwitch)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session_interval WHERE session_id = ? AND end_state = 'confirmed' AND ended_at IS NOT NULL`, held.Session.ID); n != 1 {
			t.Fatal("the interval's end is not confirmed with its time (operator ruling 2026-11-20)")
		}
		st := b.file(lifeA)
		if st.Open != nil || st.Generation != held.Generation+1 {
			t.Fatalf("file state = %+v; want no session at generation %d", st, held.Generation+1)
		}
		if got := read(); len(got) != 1 || got[0] != st.Generation {
			t.Fatalf("events = %v; want one, at generation %d", got, st.Generation)
		}
		if v := b.view(lifeA); v.State != sessionStateArchiveSwitching || v.Session != nil || v.Generation != st.Generation {
			t.Fatalf("the read says %+v; want %s with no session at generation %d", v, sessionStateArchiveSwitching, st.Generation)
		}
	})
	t.Run("an empty session is removed", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		if err := b.switchClose(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if b.sessions() != 0 {
			t.Fatalf("%d sessions after the close of an empty one, want 0", b.sessions())
		}
	})
	t.Run("a session held by an app that has gone is closed too, with no app current", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		b.contact(b.view(lifeA).Session.ID)
		b.restart() // a daemon restart: nobody is current, the session is A's remnant
		if err := b.switchClose(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if b.openSessions() != 0 {
			t.Fatal("the remnant is still open after a switch by a caller with no lifetime")
		}
	})
	t.Run("with no session open nothing changes, and the phase begins", func(t *testing.T) {
		b := newExitBed(t, false) // setup pending
		read := b.sessionEvents()
		gen := b.file("").Generation
		if err := b.switchClose(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if g := b.file("").Generation; g != gen || len(read()) != 0 {
			t.Fatalf("generation %d to %d, events %v; want nothing changed and nothing published", gen, g, read())
		}
		if v := b.view(lifeA); v.State != sessionStateArchiveSwitching {
			t.Fatalf("the read says %q; want %s", v.State, sessionStateArchiveSwitching)
		}
	})
}

// If the close cannot be written nothing is closed and the phase has not begun:
// the session is as it was and the sitting goes on.
func TestSwitchSession_ACloseThatFailsChangesNothing(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	held := b.view(lifeA)
	b.contact(held.Session.ID)
	restore := b.failSessionWrites()

	err := b.switchClose()
	restore()

	if err == nil {
		t.Fatal("the close answered success while the file could not be written")
	}
	if v := b.view(lifeA); v.State != sessionStateOpen || v.Session == nil || v.Session.ID != held.Session.ID || v.Generation != held.Generation {
		t.Fatalf("after a failed close the read says %+v with %+v; want the session as it was", v, v.Session)
	}
	// Not in the phase: a new lifetime's start step runs, and meets A's session.
	b.mustBeAdmitted(lifeB)
	if v := b.view(lifeB); v.State != sessionStateRecoveryPending {
		t.Fatalf("after a failed close a new app reads %q; want the ordinary rule (%s)", v.State, sessionStateRecoveryPending)
	}
}

// The closing phase: no session is opened, by an app start, a repeat, a
// replacement or setup's first save; an exit is acknowledged without change.
func TestSwitchSession_NoSessionIsOpenedInTheClosingPhase(t *testing.T) {
	phase := func(t *testing.T) *exitBed {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		if err := b.switchClose(); err != nil {
			t.Fatalf("close: %v", err)
		}
		return b
	}
	wantNone := func(t *testing.T, b *exitBed, lifetime, when string) {
		t.Helper()
		if b.sessions() != 0 {
			t.Fatalf("%s: %d sessions in the file, want none opened in the phase", when, b.sessions())
		}
		if v := b.view(lifetime); v.State != sessionStateArchiveSwitching || v.Session != nil {
			t.Fatalf("%s: the read says %+v, want %s with no session", when, v, sessionStateArchiveSwitching)
		}
	}
	t.Run("a second app replacing the first is admitted and given none", func(t *testing.T) {
		b := phase(t)
		gen := b.file("").Generation
		b.mustBeAdmitted(lifeB)
		wantNone(t, b, lifeB, "after the replacement")
		if st := b.readAdmission(t, b.c, lifeB); !st.Current {
			t.Fatalf("the replacing app reads %+v; want current: it is admitted, not refused", st)
		}
		if g := b.file("").Generation; g != gen {
			t.Fatalf("the session generation moved from %d to %d with no session changed", gen, g)
		}
	})
	t.Run("the same app admitting again is given none", func(t *testing.T) {
		b := phase(t)
		b.mustBeAdmitted(lifeA)
		wantNone(t, b, lifeA, "after the repeat")
	})
	t.Run("setup's first save opens none", func(t *testing.T) {
		b := phase(t)
		if err := b.srv.openSessionAtSetup(); err != nil {
			t.Fatalf("setup's session step in the phase: %v", err)
		}
		wantNone(t, b, lifeA, "after setup's save")
	})
	t.Run("an exit report is acknowledged without change, and releases no seal the switch holds", func(t *testing.T) {
		b := phase(t)
		sw := b.gate.SwitchOwner()
		_ = sw.Seal() // as the archive manager holds it through the phase
		gen := b.file("").Generation

		// The app names the generation it last saw, from before the close. In
		// the phase that is not a reason to send it round again.
		a := b.exitAs(lifeA, gen-1, testServedArchive)

		if a.status != http.StatusOK || a.Generation != gen {
			t.Fatalf("the exit in the phase: %d %s at generation %d; want 200 at %d", a.status, a.code, a.Generation, gen)
		}
		if !b.exitRecorded(lifeA) || b.sessions() != 0 {
			t.Fatalf("exit recorded %v, sessions %d; want the exit recorded and no session changed", b.exitRecorded(lifeA), b.sessions())
		}
		if _, refused := b.gate.Admit(true); refused != LiveEntrySwitching {
			t.Fatalf("after the exit a live submit gets %v; want the switch's seal still held", refused)
		}
	})
}

// A switch that fails after its close: the close stands, the phase ends, and one
// new session is opened for the app that is current. With nobody current none
// is opened. Retries do not pile sessions up.
func TestSwitchSession_AFailedSwitchOpensOneNewSessionForTheCurrentApp(t *testing.T) {
	t.Run("the current app gets a new session, published", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		old := b.view(lifeA)
		b.contact(old.Session.ID)
		if err := b.switchClose(); err != nil {
			t.Fatalf("close: %v", err)
		}
		read := b.sessionEvents()

		b.switchFailed()

		v := b.view(lifeA)
		if v.State != sessionStateOpen || v.Session == nil || v.Session.ID == old.Session.ID || !v.Session.HeldByYou {
			t.Fatalf("after the failed switch the read says %+v with %+v; want a new session held by the app", v, v.Session)
		}
		if b.openSessions() != 1 || b.sessions() != 2 {
			t.Fatalf("%d open of %d sessions; want the closed one kept with its contact and one new one", b.openSessions(), b.sessions())
		}
		if got := read(); len(got) != 1 || got[0] != v.Generation {
			t.Fatalf("events = %v; want one for the new session, at generation %d", got, v.Generation)
		}
	})
	t.Run("with no app current none is opened, and the next app start opens it", func(t *testing.T) {
		b := newExitBed(t, true)
		if err := b.switchClose(); err != nil {
			t.Fatalf("close: %v", err)
		}
		b.switchFailed()
		if b.sessions() != 0 {
			t.Fatalf("%d sessions with nobody current, want none", b.sessions())
		}
		if v := b.view(lifeA); v.State != sessionStateNone {
			t.Fatalf("the read says %q after the phase ended; want %s", v.State, sessionStateNone)
		}
		b.mustBeAdmitted(lifeA)
		if v := b.view(lifeA); v.State != sessionStateOpen || !v.Session.HeldByYou {
			t.Fatalf("the next app start reads %+v; want a session of its own", v)
		}
	})
	t.Run("three failed switches leave one open session and no empty closed ones", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		for i := 0; i < 3; i++ {
			if err := b.switchClose(); err != nil {
				t.Fatalf("close %d: %v", i, err)
			}
			b.switchFailed()
		}
		if b.openSessions() != 1 || b.sessions() != 1 {
			t.Fatalf("%d open of %d sessions after three failed switches; want 1 of 1", b.openSessions(), b.sessions())
		}
	})
}

// If the new session cannot be opened when a switch fails, the admission is left
// as a failed start step leaves it (part 2, B4): the scopes stopped, the app's
// read saying it is not current. The same app's own flow then repairs it.
func TestSwitchSession_AFailedReopenIsRepairedByTheAppsOwnFlow(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	if err := b.switchClose(); err != nil {
		t.Fatalf("close: %v", err)
	}
	restore := b.failSessionWrites()

	b.switchFailed()

	wantScopes(t, b.sessionBed, "after the failed re-open", "retire", "publish "+lifeA, "retire")
	if st := b.readAdmission(t, b.c, lifeA); st.Current {
		t.Fatalf("the app reads %+v after a failed re-open; want not current, so that its flow admits again", st)
	}
	if b.sessions() != 0 {
		t.Fatalf("%d sessions after a failed re-open, want none", b.sessions())
	}
	if status, code, posted := b.shellAdmits(lifeA); !posted || status != http.StatusServiceUnavailable {
		t.Fatalf("the app's flow while the file fails: %d %s, admit sent %v; want a 503 from an admit", status, code, posted)
	}

	restore()
	if status, code, posted := b.shellAdmits(lifeA); !posted || status != http.StatusOK {
		t.Fatalf("the app's flow once the file can be written: %d %s, admit sent %v; want an admit that succeeds", status, code, posted)
	}
	if v := b.view(lifeA); v.State != sessionStateOpen || v.Session == nil || !v.Session.HeldByYou {
		t.Fatalf("after the repair the read says %+v with %+v; want a session held by the app", v, v.Session)
	}
	wantScopes(t, b.sessionBed, "after the repair", "retire", "publish "+lifeA, "retire", "publish "+lifeA)
}

// I3: setup's first save against a switch, both orders. The save's session
// step and the switch's close each run with the admission's order held, so one
// is wholly before the other.
func TestSwitchOrder_I3_SetupsFirstSaveAgainstASwitch(t *testing.T) {
	t.Run("the save first: the switch that follows closes the session it opened", func(t *testing.T) {
		b := newSetupBed(t, 1)
		b.admit(lifeA)
		if status, code := b.save(); status != http.StatusOK {
			t.Fatalf("the save: %d %s", status, code)
		}
		b.wantComplete(lifeA)

		release := b.srv.HoldSessionOrder()
		err := b.srv.SwitchSession().Close()
		release()

		if err != nil {
			t.Fatalf("the switch's close: %v", err)
		}
		if got := b.stored(); got.sessions != 0 || !got.complete {
			t.Fatalf("after the switch: %+v; want the save's session closed (it had no contacts, so removed) and setup complete", got)
		}
	})
	t.Run("the switch first: the save waits, then succeeds and opens nothing", func(t *testing.T) {
		b := newSetupBed(t, 1)
		b.admit(lifeA)
		release := b.srv.HoldSessionOrder() // a switch is under way
		held := true
		defer func() {
			if held {
				release()
			}
		}()
		type saved struct {
			status int
			code   string
		}
		save := make(chan saved, 1)
		w := orderWatch(t, setupTakesOrder)
		go func() {
			status, code := b.save()
			save <- saved{status, code}
		}()
		shutOut(w, "setup's save, with a switch under way", save)
		if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 0 {
			t.Fatalf("%d session(s) opened while the switch held the order", n)
		}

		// The switch closes: nothing is open, and the closing phase begins.
		if err := b.srv.SwitchSession().Close(); err != nil {
			t.Fatalf("the switch's close: %v", err)
		}
		held = false
		release()

		if s := finished(t, "setup's save", save); s.status != http.StatusOK {
			t.Fatalf("the save in the closing phase: %+v; want it to succeed", s)
		}
		if got := b.stored(); got.sessions != 0 || !got.complete || got.logbooks != 1 || got.pointer == 0 {
			t.Fatalf("after the save: %+v; want setup complete with its default logbook, and no session opened in the phase", got)
		}
	})
}
