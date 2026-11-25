package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/testbarrier"
)

// The one order (W-0029 part 2, group G, cases 1 to 4): an app start, an exit
// report and setup's first session each run under the admission's order, one
// at a time. Each case holds one of them at a point the test sees and observes
// the other shut out: the other is seen parked at the admission's lock, in the
// function that takes it for that operation, having answered nothing
// (testbarrier; G7: none sleeps and then looks). The watch is begun before the
// other is started and requires that nothing was waiting there already and
// that exactly one goroutine waits there afterwards: the test's own. The positive half is the
// state each ends in. The cases against a real default change and a real
// activation (G5, G6) are in cmd/smd.

// Where each operation takes the admission's order. An app's start reads the
// admission and then admits (the client's flow, and the bed's), so a start
// arriving behind another operation waits at its read; one whose read was
// answered before the other began waits at the admit itself. Both are forced.
const (
	admissionReadsOrder = "(*appAdmission).read"
	admissionTakesOrder = "(*appAdmission).admit"
	exitTakesOrder      = "(*Server).exitOperatingSession"
	setupTakesOrder     = "(*appAdmission).observe"
)

// heldInDrain blocks the bed's next exit inside its drain, after its seals and
// before its commit, and returns once it is there.
func (b *exitBed) heldInDrain() (entered chan struct{}, release func()) {
	entered, gate := make(chan struct{}), make(chan struct{})
	b.inDrain = func() {
		b.inDrain = nil
		close(entered)
		<-gate
	}
	done := false
	release = func() {
		if !done {
			done = true
			close(gate)
		}
	}
	b.t.Cleanup(release)
	return entered, release
}

func arrived(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		t.Fatalf("%s never happened", what)
	}
}

// orderWatch begins the watch for an operation that will take the admission's
// order in takesOrder. It is taken BEFORE that operation is started, and fails
// if anything is already waiting there.
func orderWatch(t *testing.T, takesOrder string) testbarrier.Watch {
	t.Helper()
	return testbarrier.Expect(t, testbarrier.AtMutex, takesOrder)
}

// shutOut: the operation behind ch, started after w was taken, is the one
// goroutine parked at the admission's lock there, and has answered nothing.
func shutOut[T any](w testbarrier.Watch, what string, ch <-chan T) {
	testbarrier.Waiting(w, what, ch)
}

// notYet: the operation the test itself is holding has answered nothing. It is
// held by construction; this is a look, not the proof.
func notYet[T any](t *testing.T, what string, ch <-chan T) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("%s finished (%+v) while the test was holding it", what, v)
	default:
	}
}

func finished[T any](t *testing.T, what string, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(20 * time.Second):
		t.Fatalf("%s never finished", what)
	}
	var zero T
	return zero
}

type admitAnswer struct {
	status int
	code   string
}

func (b *exitBed) admitAsync(lifetime string) <-chan admitAnswer {
	ch := make(chan admitAnswer, 1)
	go func() {
		status, code := b.admitted(lifetime)
		ch <- admitAnswer{status, code}
	}()
	return ch
}

// admitAsyncAfterRead reads the admission now and returns a func that sends the
// admit built from that read: the start whose read was answered beforehand.
func (b *exitBed) admitAsyncAfterRead(lifetime string) (admit func() <-chan admitAnswer) {
	b.t.Helper()
	read := b.readAdmission(b.t, b.c, lifetime)
	return func() <-chan admitAnswer {
		ch := make(chan admitAnswer, 1)
		go func() {
			status, code := b.admit(b.t, b.c, lifetime, read)
			ch <- admitAnswer{status, code}
		}()
		return ch
	}
}

// arrival is one way an app's start meets an exit under way: both of its
// requests after the exit began, or only its admit.
type arrival struct {
	name  string
	start func(b *exitBed, lifetime string) (begin func() <-chan admitAnswer, waitsIn string)
}

var arrivals = []arrival{
	{"its read and its admit both arrive during the exit", func(b *exitBed, lifetime string) (func() <-chan admitAnswer, string) {
		return func() <-chan admitAnswer { return b.admitAsync(lifetime) }, admissionReadsOrder
	}},
	{"its read was answered before the exit; its admit arrives during it", func(b *exitBed, lifetime string) (func() <-chan admitAnswer, string) {
		return b.admitAsyncAfterRead(lifetime), admissionTakesOrder
	}},
}

func (b *exitBed) exitAsync(lifetime string, generation int64) <-chan exitAnswer {
	ch := make(chan exitAnswer, 1)
	go func() { ch <- b.exitAs(lifetime, generation, testServedArchive) }()
	return ch
}

// G1: an admission of a lifetime against its own exit. Whichever is first, it
// ends retired and holding no session.
func TestSessionOrder_G1_AnAdmissionAgainstItsOwnExit(t *testing.T) {
	ended := func(t *testing.T, b *exitBed) {
		t.Helper()
		if !b.exitRecorded(lifeA) {
			t.Fatal("the exit is not recorded")
		}
		if st := b.file(lifeA); st.Open != nil {
			t.Fatalf("a session is left open: %+v", st.Open)
		}
		if st := b.readAdmission(t, b.c, lifeA); st.Current {
			t.Fatal("the lifetime still reads as current")
		}
		if status, code := b.admitted(lifeA); status != http.StatusConflict || code != "app_lifetime_retired" {
			t.Fatalf("its admission afterwards: %d %s; want retired", status, code)
		}
	}
	for _, arr := range arrivals {
		t.Run("the exit is under way when the admission arrives: "+arr.name, func(t *testing.T) {
			b := newExitBed(t, true)
			b.mustBeAdmitted(lifeA)
			gen := b.view(lifeA).Generation
			begin, waitsIn := arr.start(b, lifeA) // the same lifetime admits again
			entered, release := b.heldInDrain()
			exit := b.exitAsync(lifeA, gen)
			arrived(t, "the exit reaching its drain", entered)

			w := orderWatch(t, waitsIn)
			admission := begin()
			shutOut(w, "the admission, with the lifetime's exit under way", admission)
			release()
			if a := finished(t, "the exit", exit); a.status != http.StatusOK {
				t.Fatalf("the exit: %+v", a)
			}
			if a := finished(t, "the admission", admission); a.status != http.StatusConflict || a.code != "app_lifetime_retired" {
				t.Fatalf("the admission after the exit: %+v; want retired", a)
			}
			ended(t, b)
		})
	}
	t.Run("the admission is under way when the exit arrives", func(t *testing.T) {
		b := newExitBed(t, true)
		// The admission is held at the file: another connection has the
		// writer's position, and the admission's session step waits for it.
		hold, err := b.raw.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = hold.Close() }()
		if _, err := hold.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
			t.Fatal(err)
		}
		letGo := func() { _, _ = hold.ExecContext(context.Background(), `ROLLBACK`) }
		defer letGo()
		admission := b.admitAsync(lifeA)
		deadline := time.Now().Add(20 * time.Second)
		for b.srv.admission.mu.TryLock() { // held once the admission is inside
			b.srv.admission.mu.Unlock()
			if time.Now().After(deadline) {
				t.Fatal("the admission never took the order")
			}
			time.Sleep(time.Millisecond)
		}
		// Its session will be the file's first: generation 1.
		w := orderWatch(t, exitTakesOrder)
		exit := b.exitAsync(lifeA, 1)
		shutOut(w, "the exit, with the lifetime's admission under way", exit)
		notYet(t, "the admission, held at the file", admission)
		letGo()
		if a := finished(t, "the admission", admission); a.status != http.StatusOK {
			t.Fatalf("the admission: %+v", a)
		}
		if a := finished(t, "the exit", exit); a.status != http.StatusOK || a.AlreadyRecorded {
			t.Fatalf("the exit after the admission: %+v; want 200", a)
		}
		ended(t, b)
	})
}

// G2: a second lifetime's admission while the first's exit is between its
// seals and its commit. The exit finishes first: the first lifetime's session
// is closed by its own exit, not by the arrival. The second is then admitted
// and gets a new session.
func TestSessionOrder_G2_ASecondStartWaitsForTheFirstsExit(t *testing.T) {
	for _, arr := range arrivals {
		t.Run(arr.name, func(t *testing.T) {
			b := newExitBed(t, true)
			b.mustBeAdmitted(lifeA)
			first := b.view(lifeA)
			b.contact(first.Session.ID)
			begin, waitsIn := arr.start(b, lifeB)
			entered, release := b.heldInDrain()
			exit := b.exitAsync(lifeA, first.Generation)
			arrived(t, "the exit reaching its drain", entered)

			w := orderWatch(t, waitsIn)
			second := begin()
			shutOut(w, "the second start, with the first's exit under way", second)
			if st := b.file(lifeB); st.Open == nil || st.Open.ID != first.Session.ID {
				t.Fatalf("the session changed while the exit was held: %+v", st.Open)
			}
			release()
			if a := finished(t, "the exit", exit); a.status != http.StatusOK {
				t.Fatalf("the exit: %+v", a)
			}
			if a := finished(t, "the second start", second); a.status != http.StatusOK {
				t.Fatalf("the second start: %+v", a)
			}
			if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = 'app_closed'`, first.Session.ID); n != 1 {
				t.Fatal("the first lifetime's session was not closed by its own exit (app_closed)")
			}
			if v := b.view(lifeB); v.State != "open" || !v.Session.HeldByYou || v.Session.ID == first.Session.ID {
				t.Fatalf("the second start's session: %+v with %+v; want a new one, its own", v, v.Session)
			}
			wantScopes(t, b.sessionBed, "after both", "retire", "publish "+lifeA, "retire", "retire", "publish "+lifeB)
		})
	}
}

// exitDirect is an exit report given to the server without a listener, for the
// setup bed, which has none: the request's routing is tested elsewhere.
func (b *setupBed) exitDirect(lifetime string, generation int64) exitResult {
	b.t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/operating-session/exit", nil)
	r.Header.Set(archiveIDHeader, b.srv.servedArchiveID)
	return b.srv.exitOperatingSession(r, lifetime, generation)
}

// G3: setup's first session against a replacement. The step waits for the
// order, and the session is for the lifetime the admission holds when the step
// runs: here the order is held, as a replacement under way holds it, and the
// replacement completes before the step is let in.
func TestSessionOrder_G3_SetupsSessionWaitsForAReplacementUnderWay(t *testing.T) {
	b := newSetupBed(t, 1)
	b.admit(lifeA)
	a := &b.srv.admission
	a.mu.Lock() // a replacement is under way
	held := true
	defer func() {
		if held {
			a.mu.Unlock()
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
	shutOut(w, "setup's save, with a replacement under way", save)
	if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 0 {
		t.Fatalf("%d session(s) opened while the order was held", n)
	}
	// The replacement completes: B is the lifetime held.
	a.retire(lifeA)
	a.setCurrent(lifeB)
	a.generation++
	held = false
	a.mu.Unlock()
	if s := finished(t, "setup's save", save); s.status != http.StatusOK {
		t.Fatalf("the save: %+v", s)
	}
	b.wantComplete(lifeB)
}

// G4: setup's first session against an exit of the same lifetime.
func TestSessionOrder_G4_SetupsSessionAgainstAnExit(t *testing.T) {
	t.Run("the save first: the exit closes the session it opened", func(t *testing.T) {
		b := newSetupBed(t, 1)
		b.admit(lifeA)
		if status, code := b.save(); status != http.StatusOK {
			t.Fatalf("the save: %d %s", status, code)
		}
		b.wantComplete(lifeA)
		st, err := b.srv.db.OperatingSessionStateWithContext(context.Background(), lifeA)
		if err != nil {
			t.Fatal(err)
		}
		if res := b.exitDirect(lifeA, st.Generation); res.status != 0 {
			t.Fatalf("the exit: %+v", res)
		}
		if got := b.stored(); got.sessions != 0 || !got.complete {
			t.Fatalf("after the exit: %+v; want no session left (it had no contacts) and setup complete", got)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_app_exit WHERE app_lifetime = ?`, lifeA); n != 1 {
			t.Fatal("the exit is not recorded")
		}
	})
	t.Run("the save is under way when the exit arrives: the exit waits, then closes the session it opened", func(t *testing.T) {
		b := newSetupBed(t, 1)
		b.admit(lifeA)
		// The save is held inside its session step, with the order taken and the
		// session not yet opened: the step asks for the time there, once.
		entered, gate := make(chan struct{}), make(chan struct{})
		first := true
		b.srv.sessionClock = func() time.Time {
			if first {
				first = false
				close(entered)
				<-gate
			}
			return sessionMorning
		}
		released := false
		letGo := func() {
			if !released {
				released = true
				close(gate)
			}
		}
		t.Cleanup(letGo)
		type saved struct {
			status int
			code   string
		}
		save := make(chan saved, 1)
		go func() {
			status, code := b.save()
			save <- saved{status, code}
		}()
		arrived(t, "the save reaching its session step", entered)

		// The session the save is about to open will be the file's first.
		exit := make(chan exitResult, 1)
		w := orderWatch(t, exitTakesOrder)
		go func() { exit <- b.exitDirect(lifeA, 1) }()
		shutOut(w, "the exit, with setup's save under way", exit)
		notYet(t, "setup's save, held in its session step", save)
		if n := b.count(`SELECT COUNT(*) FROM operating_app_exit`); n != 0 {
			t.Fatalf("%d exit(s) recorded while the save was under way", n)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 0 {
			t.Fatalf("%d session(s) exist while the save is held before opening one", n)
		}
		letGo()
		if s := finished(t, "setup's save", save); s.status != http.StatusOK {
			t.Fatalf("the save: %+v", s)
		}
		if res := finished(t, "the exit", exit); res.status != 0 {
			t.Fatalf("the exit after the save: %+v; want it stored at the generation of the session the save opened", res)
		}
		// The save opened a session for the lifetime (one was opened and closed:
		// the file's generation moved twice) and the exit that followed closed it.
		st, err := b.srv.db.OperatingSessionStateWithContext(context.Background(), lifeA)
		if err != nil {
			t.Fatal(err)
		}
		if st.Open != nil || !st.ExitRecorded || st.Generation != 2 {
			t.Fatalf("after both: open %+v, exit recorded %v, generation %d; want none open, recorded, generation 2", st.Open, st.ExitRecorded, st.Generation)
		}
		if got := b.stored(); got.sessions != 0 || !got.complete || got.pointer == 0 {
			t.Fatalf("after both: %+v; want setup complete and the empty session gone", got)
		}
	})
	t.Run("the exit is under way when the save arrives: the save opens nothing for it", func(t *testing.T) {
		b := newSetupBed(t, 1)
		b.admit(lifeA)
		entered, gate := make(chan struct{}), make(chan struct{})
		b.srv.SetCompletionDrain(func() { close(entered); <-gate })
		released := false
		letGo := func() {
			if !released {
				released = true
				close(gate)
			}
		}
		t.Cleanup(letGo)
		exit := make(chan exitResult, 1)
		go func() { exit <- b.exitDirect(lifeA, 0) }()
		arrived(t, "the exit reaching its drain", entered)

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
		shutOut(w, "setup's save, with the lifetime's exit under way", save)
		if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 0 {
			t.Fatalf("%d session(s) opened while the exit was under way", n)
		}
		letGo()
		if res := finished(t, "the exit", exit); res.status != 0 && res.code != operatingSessionStale {
			t.Fatalf("the exit: %+v", res)
		} else if res.code == operatingSessionStale {
			// The save recorded the default while the exit was held, which
			// moved nothing the exit named; a stale answer here would mean the
			// order is not what this test says it is.
			t.Fatalf("the exit was refused as stale: %+v", res)
		}
		if s := finished(t, "setup's save", save); s.status != http.StatusOK {
			t.Fatalf("the save: %+v", s)
		}
		if got := b.stored(); got.sessions != 0 || !got.complete || got.pointer == 0 {
			t.Fatalf("after both: %+v; want setup complete and no session for the exited lifetime", got)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_app_exit WHERE app_lifetime = ?`, lifeA); n != 1 {
			t.Fatal("the exit is not recorded")
		}
	})
}
