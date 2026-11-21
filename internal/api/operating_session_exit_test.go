package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
)

// The exit report (ADR 0100 §3; W-0029 part 2, group E; the brief's exit
// protocol). A confirmed exit of the desktop app closes its session and
// retires its lifetime. Every case goes through the app listener's real route,
// and is judged on the admission, the scopes, the live-entry gate and the
// file's rows together.

const otherArchive = "01a16a00-0000-7000-8000-0000000000ff"

type exitAnswer struct {
	status          int
	code            string
	Generation      int64 `json:"generation"`
	AlreadyRecorded bool  `json:"already_recorded"`
}

// exitBed is a session bed with a live-entry gate and a completion drain the
// test can put work into.
type exitBed struct {
	*sessionBed
	gate   *LiveEntryGate
	drains int
	// inDrain, when set, runs inside the exit's drain: after the seals, before
	// the commit.
	inDrain func()
}

func newExitBed(t *testing.T, withDefault bool) *exitBed {
	t.Helper()
	b := &exitBed{sessionBed: newSessionBed(t, withDefault), gate: NewLiveEntryGate()}
	b.srv.SetLiveEntryGate(b.gate)
	b.srv.SetCompletionDrain(func() {
		b.drains++
		if b.inDrain != nil {
			b.inDrain()
		}
	})
	return b
}

// exitAs sends an exit report: the lifetime, the generation it expects, and
// the archive it names ("" for no archive header).
func (b *exitBed) exitAs(lifetime string, generation int64, archive string) exitAnswer {
	b.t.Helper()
	resp, err := b.c.Do(b.exitRequest(lifetime, generation, archive))
	if err != nil {
		b.t.Fatalf("exit as %q: %v", lifetime, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	a := exitAnswer{status: resp.StatusCode}
	_ = json.Unmarshal(raw, &a)
	if resp.StatusCode != http.StatusOK {
		a.code = errorCode(b.t, raw)
	}
	return a
}

// exitRequest is the exit report as the app listener accepts it.
func (b *exitBed) exitRequest(lifetime string, generation int64, archive string) *http.Request {
	b.t.Helper()
	r := b.acceptable(b.t, true, http.MethodPost, "/v1/operating-session/exit", b.cred)
	if lifetime != "" {
		r.Header.Set(appLifetimeHeader, lifetime)
	}
	r.Header.Del(archiveIDHeader) // the request helper names the served archive by default
	if archive != "" {
		r.Header.Set(archiveIDHeader, archive)
	}
	body := fmt.Sprintf(`{"generation":%d}`, generation)
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func (b *exitBed) exitRecorded(lifetime string) bool {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM operating_app_exit WHERE app_lifetime = ?`, lifetime) == 1
}

// untouched fails the test unless the scopes, the gate, the file's sessions
// and the recorded exits are as before call.
func (b *exitBed) untouched(what string, call func()) {
	b.t.Helper()
	scopes, drains := b.scopeEvents(), b.drains
	sessions := b.count(`SELECT COUNT(*) FROM operating_session`)
	exits := b.count(`SELECT COUNT(*) FROM operating_app_exit`)
	gen := b.file("").Generation
	call()
	if after := b.scopeEvents(); !sameStrings(scopes, after) {
		b.t.Fatalf("%s reached the scopes: %v became %v", what, scopes, after)
	}
	if b.gate.Closed() || b.drains != drains {
		b.t.Fatalf("%s sealed live entry or drained (closed %v, drains %d to %d)", what, b.gate.Closed(), drains, b.drains)
	}
	if n, e, g := b.count(`SELECT COUNT(*) FROM operating_session`), b.count(`SELECT COUNT(*) FROM operating_app_exit`), b.file("").Generation; n != sessions || e != exits || g != gen {
		b.t.Fatalf("%s changed the file: sessions %d to %d, exits %d to %d, generation %d to %d", what, sessions, n, exits, e, gen, g)
	}
}

// ---- E2: the exit of the current lifetime ----

func TestExit_TheCurrentLifetimeClosesItsSessionAndIsRetired(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	b.contact(before.Session.ID) // a session with a contact is kept, closed
	ch, cancel := b.srv.hub.Subscribe()
	defer cancel()

	sealedInDrain, stoppedInDrain := false, false
	b.inDrain = func() {
		sealedInDrain = b.gate.Closed()
		ev := b.scopeEvents()
		stoppedInDrain = len(ev) > 0 && ev[len(ev)-1] == "retire"
	}
	a := b.exitAs(lifeA, before.Generation, testServedArchive)
	if a.status != http.StatusOK || a.AlreadyRecorded || a.Generation != before.Generation+1 {
		t.Fatalf("the exit: %+v; want 200 at generation %d", a, before.Generation+1)
	}
	if b.drains != 1 || !sealedInDrain || !stoppedInDrain {
		t.Fatalf("drains %d, live entry sealed during the drain %v, scopes stopped during it %v; want one drain after both seals", b.drains, sealedInDrain, stoppedInDrain)
	}
	if b.gate.Closed() {
		t.Fatal("live entry is still closed after the exit")
	}
	wantScopes(t, b.sessionBed, "after the exit", "retire", "publish "+lifeA, "retire")
	if !b.exitRecorded(lifeA) {
		t.Fatal("the exit is not recorded")
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = 'app_closed'`, before.Session.ID); n != 1 {
		t.Fatal("the session is not closed with app_closed")
	}
	if v := b.view(lifeA); v.State != "none" || v.Session != nil || v.Generation != before.Generation+1 {
		t.Fatalf("after the exit the route says %+v", v)
	}
	// Retired: its read is not current, and its admission is refused for good.
	if st := b.readAdmission(t, b.c, lifeA); st.Current {
		t.Fatal("the exited lifetime still reads as current")
	}
	if status, code := b.admitted(lifeA); status != http.StatusConflict || code != "app_lifetime_retired" {
		t.Fatalf("the exited lifetime's admission: %d %s; want retired", status, code)
	}
	changes := 0
	for {
		select {
		case ev := <-ch:
			if ev.Name == "operating_session.changed" {
				changes++
			}
			continue
		default:
		}
		break
	}
	if changes != 1 {
		t.Fatalf("the exit published %d events, want 1", changes)
	}
	// A new start of the app is admitted and gets a new session.
	b.mustBeAdmitted(lifeB)
	if v := b.view(lifeB); v.State != "open" || !v.Session.HeldByYou || v.Session.ID == before.Session.ID {
		t.Fatalf("after a new start the route says %+v with %+v", v, v.Session)
	}
}

func TestExit_AnEmptySessionIsDiscarded(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	if a := b.exitAs(lifeA, before.Generation, testServedArchive); a.status != http.StatusOK {
		t.Fatalf("the exit: %+v", a)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 0 || !b.exitRecorded(lifeA) {
		t.Fatalf("%d session(s) left, exit recorded %v; want none and recorded", n, b.exitRecorded(lifeA))
	}
}

// A refusal at the first check has sealed nothing.
func TestExit_ARefusalAtTheFirstCheckSealsNothing(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	for name, tc := range map[string]struct {
		generation int64
		archive    string
		status     int
		code       string
	}{
		"another archive":     {before.Generation, otherArchive, http.StatusConflict, "archive_mismatch"},
		"no archive named":    {before.Generation, "", http.StatusBadRequest, "missing_required_field"},
		"an older generation": {before.Generation - 1, testServedArchive, http.StatusConflict, "operating_session_stale"},
		"a later generation":  {before.Generation + 5, testServedArchive, http.StatusConflict, "operating_session_stale"},
	} {
		b.untouched(name, func() {
			a := b.exitAs(lifeA, tc.generation, tc.archive)
			if a.status != tc.status || a.code != tc.code {
				t.Fatalf("%s: %+v; want %d %s", name, a, tc.status, tc.code)
			}
			if tc.code == "operating_session_stale" && a.Generation != before.Generation {
				t.Fatalf("%s: the stale answer carries generation %d, want the current %d", name, a.Generation, before.Generation)
			}
		})
	}
	if st := b.readAdmission(t, b.c, lifeA); !st.Current || b.exitRecorded(lifeA) {
		t.Fatalf("after the refusals: %+v, exit recorded %v; want still current, not recorded", st, b.exitRecorded(lifeA))
	}
}

// A live Phone/CW submit admitted before the seal finishes; one arriving after
// the seal began is refused; the exit waits for the first.
func TestExit_TheLiveEntrySealWaitsForAdmittedSubmits(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	done, refused := b.gate.Admit(true) // a submit already admitted
	if refused != LiveEntryAdmitted {
		t.Fatal("the gate refused a submit before any seal")
	}
	answer := make(chan exitAnswer, 1)
	go func() { answer <- b.exitAs(lifeA, before.Generation, testServedArchive) }()
	// The exit reaches the gate: admission closes at once and it waits there.
	// The test waits for that condition, however long the request takes to
	// arrive; it does not wait a fixed time and then look.
	deadline := time.Now().Add(20 * time.Second)
	for !b.gate.Closed() {
		select {
		case a := <-answer:
			t.Fatalf("the exit finished without sealing live entry: %+v", a)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the exit never sealed live entry")
		}
		time.Sleep(time.Millisecond)
	}
	if _, refused := b.gate.Admit(true); refused != LiveEntryClosing {
		t.Fatalf("a submit arriving after the exit's seal got %v; want it refused as closing", refused)
	}
	select {
	case a := <-answer:
		t.Fatalf("the exit did not wait for the admitted submit: %+v", a)
	default:
	}
	if b.exitRecorded(lifeA) || b.drains != 0 {
		t.Fatalf("the exit committed or drained while a submit was still running (recorded %v, drains %d)", b.exitRecorded(lifeA), b.drains)
	}
	done() // the admitted submit finishes
	if a := <-answer; a.status != http.StatusOK {
		t.Fatalf("the exit after the submit finished: %+v", a)
	}
	if b.gate.Closed() || !b.exitRecorded(lifeA) {
		t.Fatal("after the exit live entry is closed, or the exit is not recorded")
	}
}

// ---- E3, E4: stale or failing at the commit; nothing stays sealed ----

func TestExit_StaleAtTheCommit_RestoresAndMayBeRepeated(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	b.contact(before.Session.ID)
	// The default moves after the first check: the commit's default-epoch test
	// closes the session, and the exit is stale.
	b.exec(`INSERT INTO logbook (name, callsign, uuid) VALUES ('Other', 'G0TST', '01920000-0000-7000-8000-0000000000e2')`)
	b.inDrain = func() {
		b.inDrain = nil
		b.exec(`UPDATE archive_metadata SET default_epoch = default_epoch + 1,
			default_logbook_id = (SELECT id FROM logbook WHERE name = 'Other') WHERE singleton = 1`)
	}
	a := b.exitAs(lifeA, before.Generation, testServedArchive)
	if a.status != http.StatusConflict || a.code != "operating_session_stale" || a.Generation != before.Generation+1 {
		t.Fatalf("the exit: %+v; want 409 operating_session_stale carrying generation %d", a, before.Generation+1)
	}
	if b.gate.Closed() {
		t.Fatal("live entry stayed sealed after a stale exit")
	}
	wantScopes(t, b.sessionBed, "after the stale exit", "retire", "publish "+lifeA, "retire", "publish "+lifeA)
	if st := b.readAdmission(t, b.c, lifeA); !st.Current || b.exitRecorded(lifeA) {
		t.Fatalf("after the stale exit: %+v, recorded %v; want still current, not recorded", st, b.exitRecorded(lifeA))
	}
	// The same lifetime repeats at the generation returned, without admitting again.
	if again := b.exitAs(lifeA, a.Generation, testServedArchive); again.status != http.StatusOK || !b.exitRecorded(lifeA) {
		t.Fatalf("the repeated exit: %+v, recorded %v", again, b.exitRecorded(lifeA))
	}
}

func TestExit_AFailureBeforeTheCommit_RestoresAndConfirmsNothing(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	var restore func()
	b.inDrain = func() { b.inDrain = nil; restore = b.failSessionWrites() }
	a := b.exitAs(lifeA, before.Generation, testServedArchive)
	if a.status != http.StatusServiceUnavailable || a.code != "operating_session_unavailable" {
		t.Fatalf("the exit: %+v; want 503 operating_session_unavailable", a)
	}
	restore()
	if b.gate.Closed() {
		t.Fatal("live entry stayed sealed after a failed exit")
	}
	wantScopes(t, b.sessionBed, "after the failed exit", "retire", "publish "+lifeA, "retire", "publish "+lifeA)
	if st := b.readAdmission(t, b.c, lifeA); !st.Current || b.exitRecorded(lifeA) {
		t.Fatalf("after the failed exit: %+v, recorded %v; want still current, not recorded", st, b.exitRecorded(lifeA))
	}
	if v := b.view(lifeA); v.Session == nil || v.Session.ID != before.Session.ID || v.Generation != before.Generation {
		t.Fatalf("the failed exit changed the session: %+v with %+v", v, v.Session)
	}
}

// ---- E1, E5: an exit already recorded ----

func TestExit_AlreadyRecordedIsAcknowledgedAndChangesNothing(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	first := b.view(lifeA)
	if a := b.exitAs(lifeA, first.Generation, testServedArchive); a.status != http.StatusOK {
		t.Fatalf("the exit: %+v", a)
	}
	b.mustBeAdmitted(lifeB) // a later start is current, with its session
	for _, tc := range []struct {
		generation int64
		archive    string
	}{{first.Generation, testServedArchive}, {0, testServedArchive}, {99, otherArchive}, {first.Generation, ""}} {
		b.untouched("a repeated exit report", func() {
			a := b.exitAs(lifeA, tc.generation, tc.archive)
			if a.status != http.StatusOK || !a.AlreadyRecorded {
				t.Fatalf("a repeat naming generation %d, archive %q: %+v; want 200, already recorded", tc.generation, tc.archive, a)
			}
		})
	}
	if v := b.view(lifeB); v.State != "open" || !v.Session.HeldByYou {
		t.Fatalf("the current lifetime's session changed: %+v", v)
	}
}

// A client that gives up while its exit is under way does not interrupt it: the
// exit does not follow its request (E4 names a cancellation BEFORE the commit
// as unconfirmed; this exit is past its seals and is not cancelled by its
// caller going away). The client gets no answer, the exit goes on to its
// commit, and the repeat, which waits for it, is acknowledged (E1, E5).
func TestExit_AClientThatGivesUpDoesNotInterruptTheExit(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	gen := b.view(lifeA).Generation
	entered, release := b.heldInDrain()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gaveUp := make(chan error, 1)
	go func() {
		resp, err := b.c.Do(b.exitRequest(lifeA, gen, testServedArchive).WithContext(ctx))
		if err == nil {
			_ = resp.Body.Close()
		}
		gaveUp <- err
	}()
	arrived(t, "the exit reaching its drain", entered)
	cancel()
	if err := finished(t, "the client giving up", gaveUp); err == nil {
		t.Fatal("the client that gave up was answered")
	}
	if b.exitRecorded(lifeA) || !b.gate.Closed() {
		t.Fatalf("with the exit still held in its drain: recorded %v, live entry closed %v; want not recorded, closed", b.exitRecorded(lifeA), b.gate.Closed())
	}

	w := orderWatch(t, exitTakesOrder)
	repeat := b.exitAsync(lifeA, gen)
	shutOut(w, "the repeat, with the first exit still under way", repeat)
	release()
	if a := finished(t, "the repeat", repeat); a.status != http.StatusOK || !a.AlreadyRecorded {
		t.Fatalf("the repeat: %+v; want 200, already recorded", a)
	}
	if !b.exitRecorded(lifeA) {
		t.Fatal("the exit the client gave up on was not stored")
	}
	if st := b.file(lifeA); st.Open != nil {
		t.Fatalf("a session is left open: %+v", st.Open)
	}
	if st := b.readAdmission(t, b.c, lifeA); st.Current {
		t.Fatal("the lifetime still reads as current")
	}
	if b.gate.Closed() || b.drains != 1 {
		t.Fatalf("live entry closed %v, drains %d; want reopened and one drain", b.gate.Closed(), b.drains)
	}
	wantScopes(t, b.sessionBed, "after the exit", "retire", "publish "+lifeA, "retire")
}

// ---- E6 to E9: a lifetime that is not current ----

func TestExit_NotCurrentAndHoldsTheOpenSession_IsRefusedForGood(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	b.contact(before.Session.ID)
	b.restart() // the daemon restarted before the exit was stored; nobody is admitted
	b.untouched("the exit", func() {
		a := b.exitAs(lifeA, before.Generation, testServedArchive)
		if a.status != http.StatusConflict || a.code != "operating_session_exit_not_current" {
			t.Fatalf("the exit: %+v; want 409 operating_session_exit_not_current", a)
		}
	})
	if b.exitRecorded(lifeA) {
		t.Fatal("a refused exit was recorded")
	}
}

func TestExit_NotCurrentAndHoldsNone_IsRecordedAndTouchesNothingElse(t *testing.T) {
	t.Run("a late report from a replaced lifetime", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		b.mustBeAdmitted(lifeB) // A is replaced; its empty session is gone, B has its own
		held := b.view(lifeB)
		scopes := b.scopeEvents()
		a := b.exitAs(lifeA, 0, testServedArchive)
		if a.status != http.StatusOK || a.AlreadyRecorded || !b.exitRecorded(lifeA) {
			t.Fatalf("the exit: %+v, recorded %v; want 200 and recorded", a, b.exitRecorded(lifeA))
		}
		if after := b.scopeEvents(); !sameStrings(scopes, after) || b.gate.Closed() || b.drains != 0 {
			t.Fatalf("the exit reached the scopes, the gate or the drain (%v to %v, closed %v, drains %d)", scopes, after, b.gate.Closed(), b.drains)
		}
		if v := b.view(lifeB); v.Session == nil || v.Session.ID != held.Session.ID || !v.Session.HeldByYou || v.Generation != held.Generation {
			t.Fatalf("the current lifetime's session changed: %+v with %+v", v, v.Session)
		}
		if st := b.readAdmission(t, b.c, lifeB); !st.Current {
			t.Fatal("the current lifetime is no longer current")
		}
	})
	// The exit whose answer was lost, after the archive was switched under the
	// closing app (the brief, C): it names the archive it closed in, which is
	// not the one served now. It must still be acknowledged.
	t.Run("naming another archive than the one served", func(t *testing.T) {
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeB)
		held := b.view(lifeB)
		a := b.exitAs(lifeA, 7, otherArchive)
		if a.status != http.StatusOK || !b.exitRecorded(lifeA) {
			t.Fatalf("the exit naming another archive: %+v, recorded %v; want 200 and recorded here", a, b.exitRecorded(lifeA))
		}
		if v := b.view(lifeB); v.Session == nil || v.Session.ID != held.Session.ID || v.Generation != held.Generation {
			t.Fatalf("the served archive's session changed: %+v", v)
		}
		if status, code := b.admitted(lifeA); status != http.StatusConflict || code != "app_lifetime_retired" {
			t.Fatalf("that lifetime's admission afterwards: %d %s; want retired", status, code)
		}
	})
	t.Run("the current lifetime holds none: setup pending", func(t *testing.T) {
		b := newExitBed(t, false)
		b.mustBeAdmitted(lifeA)
		a := b.exitAs(lifeA, 0, testServedArchive)
		if a.status != http.StatusOK || !b.exitRecorded(lifeA) || a.Generation != 0 {
			t.Fatalf("the exit: %+v, recorded %v; want 200 at generation 0", a, b.exitRecorded(lifeA))
		}
		if st := b.readAdmission(t, b.c, lifeA); st.Current {
			t.Fatal("the exited lifetime still reads as current")
		}
	})
}

func TestExit_NeedsALifetime(t *testing.T) {
	b := newExitBed(t, true)
	b.untouched("an exit with no lifetime", func() {
		if a := b.exitAs("", 0, testServedArchive); a.status != http.StatusBadRequest || a.code != "app_lifetime_required" {
			t.Fatalf("the exit: %+v; want 400 app_lifetime_required", a)
		}
	})
}

// The file decides: an exit recorded in the file is acknowledged by a process
// that has never heard of that lifetime.
func TestExit_AfterADaemonRestartARecordedExitIsStillAcknowledged(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	if _, outcome, err := b.srv.db.RecordOperatingAppExitWithContext(context.Background(), lifeA, b.file(lifeA).Generation, true, sessionMorning); err != nil || outcome != sqlite.SessionApplied {
		t.Fatalf("record the exit: %q, %v", outcome, err)
	}
	b.restart()
	b.untouched("the repeated exit", func() {
		if a := b.exitAs(lifeA, 0, testServedArchive); a.status != http.StatusOK || !a.AlreadyRecorded {
			t.Fatalf("the exit: %+v; want 200, already recorded", a)
		}
	})
}

// An exit never reopens live entry under a seal another operation holds (G5;
// codex-192): a default change keeps the gate sealed until the daemon
// restarts, and the exit's own seal, release and failure paths must leave that
// seal as it was. The other operation's seal is the gate's own, as the archive
// manager takes it.
func TestExit_NeverReleasesAnotherOperationsLiveEntrySeal(t *testing.T) {
	held := func(t *testing.T) (*exitBed, sessionView) {
		t.Helper()
		b := newExitBed(t, true)
		b.mustBeAdmitted(lifeA)
		if err := b.gate.Seal(); err != nil { // the default change, still waiting for its restart
			t.Fatal(err)
		}
		return b, b.view(lifeA)
	}
	stillSealed := func(t *testing.T, b *exitBed, when string) {
		t.Helper()
		if _, refused := b.gate.Admit(true); refused == LiveEntryAdmitted || !b.gate.Closed() {
			t.Fatalf("%s live entry is open under the other operation's seal", when)
		}
	}
	t.Run("an exit that succeeds", func(t *testing.T) {
		b, before := held(t)
		if a := b.exitAs(lifeA, before.Generation, testServedArchive); a.status != http.StatusOK {
			t.Fatalf("the exit: %+v", a)
		}
		stillSealed(t, b, "after the exit")
		b.gate.Release()
		if b.gate.Closed() {
			t.Fatal("the exit left a seal of its own behind")
		}
	})
	t.Run("an exit that fails before its commit", func(t *testing.T) {
		b, before := held(t)
		var restore func()
		b.inDrain = func() { b.inDrain = nil; restore = b.failSessionWrites() }
		if a := b.exitAs(lifeA, before.Generation, testServedArchive); a.status != http.StatusServiceUnavailable {
			t.Fatalf("the exit: %+v; want 503", a)
		}
		restore()
		stillSealed(t, b, "after the failed exit")
		b.gate.Release()
		if b.gate.Closed() {
			t.Fatal("the failed exit left a seal of its own behind")
		}
	})
	t.Run("an exit refused at its first check", func(t *testing.T) {
		b, before := held(t)
		if a := b.exitAs(lifeA, before.Generation+3, testServedArchive); a.code != "operating_session_stale" {
			t.Fatalf("the exit: %+v; want stale", a)
		}
		stillSealed(t, b, "after the refused exit")
	})
}

// "Already recorded" comes before everything else, for a lifetime the
// admission still holds too. The file can record an exit the admission's
// memory does not know of; the answer is then the acknowledgement, with
// nothing stopped, sealed or drained, whatever generation and archive are
// named. Without the first check this lifetime would be taken down the current
// lifetime's road: refused for its archive or its generation, or sealed and
// drained for an exit that is already stored.
func TestExit_AlreadyRecordedComesFirst_EvenForTheHeldLifetime(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	gen := b.file(lifeA).Generation
	// Recorded in the file as a lifetime that is not current and holds none
	// would be; the admission still holds it. The session is closed first so
	// the record is one the file accepts.
	if _, outcome, err := b.srv.db.RecordOperatingAppExitWithContext(context.Background(), lifeA, gen, true, sessionMorning); err != nil || outcome != sqlite.SessionApplied {
		t.Fatalf("record the exit in the file: %q, %v", outcome, err)
	}
	if st := b.srv.admission.currentForTest(); st != lifeA {
		t.Fatalf("the fixture: the admission holds %q, want it still to hold the lifetime", st)
	}
	for _, tc := range []struct {
		generation int64
		archive    string
	}{{gen + 1, testServedArchive}, {gen, testServedArchive}, {0, otherArchive}, {gen + 1, ""}} {
		b.untouched("an exit already recorded", func() {
			a := b.exitAs(lifeA, tc.generation, tc.archive)
			if a.status != http.StatusOK || !a.AlreadyRecorded {
				t.Fatalf("naming generation %d, archive %q: %+v; want 200, already recorded", tc.generation, tc.archive, a)
			}
		})
	}
}

// currentForTest is the lifetime the admission holds.
func (a *appAdmission) currentForTest() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current
}

// A failed exit gives back what it took, and no more. After a failed
// replacement the held lifetime's scopes are stopped, and stay stopped until
// its own admission repairs it (the read says "not current" until then). An
// exit of that lifetime that fails must leave them stopped: publishing the
// lifetime again would authorize FT8 without the admission's repair.
func TestExit_AFailedExitDoesNotAuthorizeScopesAFailedReplacementStopped(t *testing.T) {
	b := newExitBed(t, true)
	b.mustBeAdmitted(lifeA)
	gen := b.view(lifeA).Generation
	restore := b.failSessionWrites()
	if status, _ := b.admitted(lifeB); status != http.StatusServiceUnavailable {
		t.Fatalf("the fixture: the replacement: %d; want 503", status)
	}
	wantScopes(t, b.sessionBed, "the fixture: after the failed replacement", "retire", "publish "+lifeA, "retire")

	if a := b.exitAs(lifeA, gen, testServedArchive); a.status != http.StatusServiceUnavailable {
		t.Fatalf("the exit while the file fails: %+v; want 503", a)
	}
	wantScopes(t, b.sessionBed, "after the failed exit", "retire", "publish "+lifeA, "retire", "retire")
	if st := b.readAdmission(t, b.c, lifeA); st.Current {
		t.Fatal("after the failed exit the lifetime reads as current while its scopes are stopped")
	}
	if b.gate.Closed() || b.exitRecorded(lifeA) {
		t.Fatalf("after the failed exit: live entry closed %v, exit recorded %v", b.gate.Closed(), b.exitRecorded(lifeA))
	}
	restore()
	// Its own admission repairs it, as after the failed replacement alone.
	if status, code, posted := b.shellAdmits(lifeA); !posted || status != http.StatusOK {
		t.Fatalf("the app's flow afterwards: %d %s, admit sent %v; want an admit that succeeds", status, code, posted)
	}
	wantScopes(t, b.sessionBed, "after the repair", "retire", "publish "+lifeA, "retire", "retire", "publish "+lifeA)
}
