package api

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/testbarrier"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0029 part 5, slice 2, at the daemon's routes: the wait before a leftover
   session is judged empty (E), and what a contact stored by a drain belongs to
   (H). The membership is connected as part 6 will connect it; the unconnected
   server has its own case.

   The bed's completion drain is the one a confirmed exit and an archive switch
   use (SetCompletionDrain). A test puts work into it (inDrain): what runs there
   stands for FT8 completion logging already earned and not yet stored. The
   real jobs, through a real daemon, are cmd/smd's session_membership_test.go.
*/

// earnedContact stores one contact as the FT8 completion sink does: a live
// submit into the default logbook, with no request behind it.
func (b *memberBed) earnedContact() int64 {
	b.t.Helper()
	b.n++
	res, err := b.srv.qso.SubmitLive(context.Background(), b.def, adif.Record{
		ContactedStation: types.ContactedStation{Call: fmt.Sprintf("JA%dXYZ", b.n%10)},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "FT8", Freq: "14.074", QsoDate: "20261120", TimeOn: fmt.Sprintf("11%02d", b.n)},
	}, false)
	if err != nil || res.Status != "stored" {
		b.t.Fatalf("the earned contact: %+v, %v", res, err)
	}
	return res.ID
}

// storedByTheNextDrain makes the next drain store one earned contact before it
// returns, and returns where that contact's id will be.
func (b *memberBed) storedByTheNextDrain() *int64 {
	var qso int64
	b.inDrain = func() {
		b.inDrain = nil
		qso = b.earnedContact()
	}
	return &qso
}

func (b *memberBed) closedAs(session string) string {
	b.t.Helper()
	var reason string
	if err := b.raw.QueryRow(`SELECT COALESCE(close_reason, '') FROM operating_session WHERE id = ?`, session).Scan(&reason); err != nil {
		b.t.Fatalf("session %s: %v", session, err)
	}
	return reason
}

// ---- E: the wait before a leftover session is judged empty ----

// E1, E5: the admissions that start a session wait for earned logging, after
// the scopes are retired and before the start's transaction: a first
// admission, a replacement, and the held lifetime admitted again with its
// scopes left stopped. Nothing else waits: the current lifetime's ordinary
// repeat, an admission in the closing phase of a switch, and the session a
// failed switch opens. With nothing pending the wait returns at once.
func TestMembership_Admission_WaitsWhereItStartsASession(t *testing.T) {
	b := newMemberBed(t, true)
	// inTheWait records what is true inside each wait. It runs on the
	// admitting goroutine, with the admission's order held.
	type seen struct {
		scopes   []string
		sessions int
		current  string
	}
	var waits []seen
	watch := func() {
		b.inDrain = func() {
			waits = append(waits, seen{b.scopeEvents(), b.sessions(), b.srv.admission.current})
		}
	}
	watch()

	b.mustBeAdmitted(lifeA)
	if len(waits) != 1 || !sameStrings(waits[0].scopes, []string{"retire"}) || waits[0].sessions != 0 || waits[0].current != "" {
		t.Fatalf("a first admission waited as %+v; want once, after the scopes were retired, with no session opened and nobody current yet", waits)
	}

	b.mustBeAdmitted(lifeA)
	if len(waits) != 1 {
		t.Fatalf("the current lifetime's repeat waited: %d waits", len(waits))
	}

	first := b.view(lifeA).Session.ID
	b.mustBeAdmitted(lifeB)
	if len(waits) != 2 || waits[1].current != lifeA || waits[1].sessions != 1 || b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ?`, first) != 0 {
		t.Fatalf("a replacement waited as %+v; want a second wait, with the first app still held and its session not yet judged", waits)
	}
	if last := waits[1].scopes; len(last) == 0 || last[len(last)-1] != "retire" {
		t.Fatalf("at the replacement's wait the scopes had seen %v; want them retired", last)
	}

	// A replacement whose start step fails has waited, and leaves the scopes
	// stopped. The held lifetime's own admission then starts a session, and waits.
	restore := b.failSessionWrites()
	if status, _ := b.admitted(lifeC); status == http.StatusOK || len(waits) != 3 {
		t.Fatalf("a replacement with the file failing: status %d, %d waits; want it refused after its wait", status, len(waits))
	}
	restore()
	if status, code, posted := b.shellAdmits(lifeB); !posted || status != http.StatusOK || len(waits) != 4 {
		t.Fatalf("the held lifetime admitted again with its scopes stopped: %d %s, admit sent %v, %d waits; want an admit that succeeds after a wait", status, code, posted, len(waits))
	}

	// The closing phase of a switch gives no session and waits for nothing;
	// nor does the session a failed switch opens.
	if err := b.switchClose(); err != nil { // the switch's own drain runs here
		t.Fatal(err)
	}
	waits = nil
	b.mustBeAdmitted(lifeD)
	if len(waits) != 0 {
		t.Fatalf("an admission in the closing phase waited %d time(s)", len(waits))
	}
	b.switchFailed()
	if len(waits) != 0 || b.view(lifeD).State != sessionStateOpen {
		t.Fatalf("a failed switch's session: %d waits, state %q; want none and a session open", len(waits), b.view(lifeD).State)
	}
}

// Ruling 1 (2026-11-24): the unconnected server's admissions are as they were:
// none of them waits.
func TestMembership_Admission_Unconnected_NeverWaits(t *testing.T) {
	b := newMemberBed(t, false)
	b.mustBeAdmitted(lifeA)
	b.mustBeAdmitted(lifeB)
	restore := b.failSessionWrites()
	b.admitted(lifeC)
	restore()
	b.shellAdmits(lifeB)
	if b.drains != 0 {
		t.Fatalf("an unconnected server's admissions waited %d time(s)", b.drains)
	}
}

// E4, E7: an app was killed with a contact earned and not yet stored. The next
// app's admission does not answer while that logging is unfinished. A second
// read-and-admit of the same lifetime waits behind the first. (That is more
// than the shell sends: it stops at a read that says current. Its own retry,
// given up on as it gives up, is the next test but one.) When the contact is
// stored the admission succeeds, once; the new app is asked about the earlier
// session, which holds the contact; and no session was opened or discarded.
func TestMembership_Admission_TheLeftoverSessionIsJudgedAfterTheEarnedContact(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	leftover := b.view(lifeA).Session.ID
	before := b.readAdmission(t, b.c, lifeB).Generation

	// The earned contact is stored only when the test lets the wait go.
	entered, gate := make(chan struct{}), make(chan struct{})
	var qso int64
	b.inDrain = func() {
		b.inDrain = nil
		close(entered)
		<-gate
		qso = b.earnedContact()
	}
	released := false
	release := func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(release)

	start := b.admitAsyncAfterRead(lifeB)()
	arrived(t, "the new app's admission to reach its wait", entered)
	notYet(t, "the new app's admission, with a contact earned and not stored", start)
	if b.sessions() != 1 || b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ?`, leftover) != 1 {
		t.Fatal("the leftover session was judged while a contact was still to be stored")
	}

	// The shell gave up on its request and tries the same start again.
	w := orderWatch(t, admissionReadsOrder)
	again := b.admitAsync(lifeB)
	shutOut(w, "the same start tried again", again)

	release()
	if a := finished(t, "the admission", start); a.status != http.StatusOK {
		t.Fatalf("the admission, once the contact was stored: %+v", a)
	}
	if a := finished(t, "the same start tried again", again); a.status != http.StatusOK {
		t.Fatalf("the second try of the same start: %+v", a)
	}
	if st := b.readAdmission(t, b.c, lifeB); !st.Current || st.Generation != before+1 {
		t.Fatalf("after both tries the admission reads %+v; want the new app current after one admission (generation %d)", st, before+1)
	}
	if b.drains != 2 { // A's own admission, and the new app's: the second try started nothing
		t.Fatalf("%d waits, want one for each admission that started a session", b.drains)
	}
	v := b.view(lifeB)
	if v.State != sessionStateRecoveryPending || v.Session == nil || v.Session.ID != leftover || !v.Session.HasContacts {
		t.Fatalf("the new app reads %q with %+v; want to be asked about the earlier session %s, with the contact", v.State, v.Session, leftover)
	}
	if b.memberOf(qso) != leftover || b.sessions() != 1 {
		t.Fatalf("the earned contact is in %q and the file has %d sessions; want it in %s, and no session opened or discarded", b.memberOf(qso), b.sessions(), leftover)
	}
}

// ---- H: what a contact stored by a drain belongs to ----

// A contact stored by a switch's drain is a member of the session the switch
// closes, which is kept and not discarded.
func TestMembership_Drain_ASwitchClosesTheSessionWithTheContactInIt(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	session := b.view(lifeA).Session.ID
	qso := b.storedByTheNextDrain()

	if err := b.switchClose(); err != nil {
		t.Fatal(err)
	}

	if b.memberOf(*qso) != session || b.closedAs(session) != sqlite.SessionClosedBySwitch || b.openSessions() != 0 {
		t.Fatalf("the contact is in %q; the session closed as %q; %d sessions open; want the contact in the session the switch closed and kept",
			b.memberOf(*qso), b.closedAs(session), b.openSessions())
	}
}

// A contact stored by the exit's drain, with the exiting app holding the
// session: a member of the session then closed as app_closed.
func TestMembership_Drain_AnExitClosesTheSessionWithTheContactInIt(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	v := b.view(lifeA)
	qso := b.storedByTheNextDrain()

	if a := b.exitAs(lifeA, v.Generation, testServedArchive); a.status != http.StatusOK {
		t.Fatalf("the exit: %+v; a contact joining the open session moves no generation, so it is not stale", a)
	}

	if b.memberOf(*qso) != v.Session.ID || b.closedAs(v.Session.ID) != sqlite.SessionClosedByExit || !b.exitRecorded(lifeA) {
		t.Fatalf("the contact is in %q; the session closed as %q; exit recorded %v; want the contact in the session the exit closed",
			b.memberOf(*qso), b.closedAs(v.Session.ID), b.exitRecorded(lifeA))
	}
}

// An exit while a recovery is pending: the contact stored by its drain joins
// the other holder's session, which the exit leaves open.
func TestMembership_Drain_AnExitWhileAQuestionIsPendingLeavesTheOtherSessionOpen(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	b.live(lifeA)
	earlier := b.view(lifeA).Session.ID
	b.restart() // the first app is gone without a confirmed exit
	b.mustBeAdmitted(lifeB)
	v := b.view(lifeB)
	if v.State != sessionStateRecoveryPending {
		t.Fatalf("the fixture: the new app reads %q", v.State)
	}
	qso := b.storedByTheNextDrain()

	if a := b.exitAs(lifeB, v.Generation, testServedArchive); a.status != http.StatusOK {
		t.Fatalf("the exit: %+v", a)
	}

	if b.memberOf(*qso) != earlier || b.closedAs(earlier) != "" || b.holderOf(earlier) != lifeA || !b.exitRecorded(lifeB) {
		t.Fatalf("the contact is in %q; the earlier session closed as %q, held by %q; exit recorded %v; want the contact in the earlier app's session, still open and still its own",
			b.memberOf(*qso), b.closedAs(earlier), b.holderOf(earlier), b.exitRecorded(lifeB))
	}
}

// When the drained work advances the generation, the exit is answered as
// stale and repeated, as built. Here the exiting app holds no session (a
// switch failed to reopen one), so the contact its drain stores opens one, for
// that app. The repeat, at the generation given, closes that session with the
// contact in it.
func TestMembership_Drain_AnExitMadeStaleByItsOwnDrainIsRepeated(t *testing.T) {
	b := newMemberBed(t, true)
	b.failedReopen()
	gen := b.file(lifeA).Generation
	qso := b.storedByTheNextDrain()

	stale := b.exitAs(lifeA, gen, testServedArchive)

	session := b.memberOf(*qso)
	if stale.status != http.StatusConflict || stale.code != operatingSessionStale || stale.Generation != gen+1 {
		t.Fatalf("the exit: %+v; want it stale, with the generation the contact's session produced (%d)", stale, gen+1)
	}
	if session == "" || b.holderOf(session) != lifeA || b.closedAs(session) != "" || b.exitRecorded(lifeA) {
		t.Fatalf("after the stale answer the contact is in %q held by %q, closed as %q, exit recorded %v; want a session of the app's own, open, and no exit recorded",
			session, b.holderOf(session), b.closedAs(session), b.exitRecorded(lifeA))
	}

	if a := b.exitAs(lifeA, stale.Generation, testServedArchive); a.status != http.StatusOK {
		t.Fatalf("the exit repeated at the generation given: %+v", a)
	}
	if b.memberOf(*qso) != session || b.closedAs(session) != sqlite.SessionClosedByExit || !b.exitRecorded(lifeA) || b.sessions() != 1 {
		// One session: the switch discarded the empty one it closed.
		t.Fatalf("once the exit is acknowledged the contact is in %q, closed as %q, exit recorded %v, %d sessions; want it in the session the exit closed",
			b.memberOf(*qso), b.closedAs(session), b.exitRecorded(lifeA), b.sessions())
	}
}

// Ruling 1, as a guard on the source: the admission's wait is given to it in
// one place, the function that connects the membership, and is called from
// one place, awaitBeforeStart, which the two admissions that start a session
// call and nothing else does. A second assignment could connect the wait
// without the membership, and a call from the shared start step would put it
// where a failed switch meets it.
func TestSource_TheAdmissionsWaitIsConnectedAndMadeInOnePlace(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	set, called, made := map[string]int{}, map[string]int{}, map[string]int{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			where := "(package level)"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				where = fn.Name.Name
			}
			named := func(e ast.Expr) bool {
				sel, ok := e.(*ast.SelectorExpr)
				return ok && sel.Sel.Name == "awaitEarned"
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.AssignStmt:
					for _, lhs := range v.Lhs {
						if named(lhs) {
							set[where]++
						}
					}
				case *ast.CallExpr:
					if named(v.Fun) {
						called[where]++
					}
					if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "awaitBeforeStart" {
						made[where]++
					}
				case *ast.KeyValueExpr:
					if key, ok := v.Key.(*ast.Ident); ok && key.Name == "awaitEarned" {
						set[where+" (in a literal)"]++
					}
				case *ast.UnaryExpr:
					if v.Op == token.AND && named(v.X) {
						set[where+" (by its address)"]++
					}
				}
				return true
			})
		}
	}
	if len(set) != 1 || set["ConnectSessionMembership"] != 1 {
		t.Errorf("the admission's wait is set in %v; want once, in ConnectSessionMembership", set)
	}
	if len(called) != 1 || called["awaitBeforeStart"] != 1 {
		t.Errorf("the admission's wait is called in %v; want once, in awaitBeforeStart", called)
	}
	// The two admissions that start a session, and not the start step they
	// share with a failed switch.
	if len(made) != 2 || made["admit"] != 1 || made["complete"] != 1 {
		t.Errorf("the wait before a start is made in %v; want once in admit and once in complete", made)
	}
}

// shellRequest is a request as the shell's main process sends it, bound to ctx
// so that the test can give up on it as the shell does at its own time limit.
func (b *memberBed) shellRequest(ctx context.Context, lifetime, method, path, body string) *http.Request {
	b.t.Helper()
	r := b.acceptable(b.t, true, method, path, b.cred).WithContext(ctx)
	r.Header.Set(appLifetimeHeader, lifetime)
	if body != "" {
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

// sent sends a request in the background and reports the client's own error:
// what the shell sees of a request it gave up on.
func (b *memberBed) sent(r *http.Request) <-chan error {
	ch := make(chan error, 1)
	go func() {
		resp, err := b.c.Do(r)
		if resp != nil {
			_ = resp.Body.Close()
		}
		ch <- err
	}()
	return ch
}

// E7, the shell's own time limit (Codex's review, codex-261). The shell gives
// up on its admission after five seconds and tries again with a read, and
// gives up on that too, while the daemon's start is still waiting for the
// earned contact. The start goes on without its caller, as it must. A read
// that was given up on must not go on holding one of the daemon's request
// slots until the wait ends: a long wait would fill them with abandoned reads,
// and a request that has nothing to do with the start, a contact being logged
// included, would be refused as server_busy.
//
// So: the admission's slot stays taken while it works; the abandoned read's
// slot is free again while the wait is still held; and when the wait ends the
// same app reads as current after one admission, with the contact in the
// earlier session.
func TestMembership_Admission_AnAbandonedRetryHoldsNoRequestSlot(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	leftover := b.view(lifeA).Session.ID
	read := b.readAdmission(t, b.c, lifeB)
	slots := func() int { return len(b.srv.limits.concurrent) }
	settles := func(what string, want int) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for slots() != want {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d request slots are taken, want %d", what, slots(), want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	settles("before the start", 0)
	// waitingForTheOrder counts the goroutines parked for the admission's
	// order, wherever a request takes it, with the admission that holds it and
	// is itself parked in the test's wait.
	waitingForTheOrder := func() int {
		n := 0
		for _, fn := range []string{admissionReadsOrder, admissionTakesOrder, setupTakesOrder, exitTakesOrder,
			"(*appAdmission).read", "(*appAdmission).observeUnlessGone", "(*Server).handleAppLifetimeRead", "(*Server).handleOperatingSessionRead"} {
			n += testbarrier.Count(testbarrier.AtMutex, fn) + testbarrier.Count(testbarrier.AtSelect, fn)
		}
		return n + 1 // the admission, inside the bed's drain
	}

	entered, gate := make(chan struct{}), make(chan struct{})
	var qso int64
	b.inDrain = func() {
		b.inDrain = nil
		close(entered)
		<-gate
		qso = b.earnedContact()
	}
	released := false
	release := func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(release)

	// The shell's admit reaches the wait, and the shell gives up on it.
	first, giveUpFirst := context.WithCancel(context.Background())
	defer giveUpFirst()
	admit := b.sent(b.shellRequest(first, lifeB, http.MethodPost, "/v1/app/lifetime",
		fmt.Sprintf(`{"daemon_instance":%q,"generation":%d}`, read.Instance, read.Generation)))
	arrived(t, "the admission to reach its wait", entered)
	settles("with the admission in its wait", 1)
	giveUpFirst()
	if err := finished(t, "the shell's own end of the admit it gave up on", admit); err == nil {
		t.Fatal("the admit was answered; the test wants the shell to have given up on it")
	}
	settles("with the admission abandoned by its caller and still working", 1)

	// It tries again, as it does a second later: a read of the admission,
	// which waits for the admission's order, and which it gives up on too.
	// Then the operator closes the waiting window: the shell's exit begins with
	// a read of the session (desktop/src/closing.mjs, reportExit), which waits
	// for the same order and is given up on in its turn. Each read, once given
	// up on, leaves nothing behind: its request slot is free and no goroutine
	// of it still waits for the order, while the admission's wait goes on.
	for _, given := range []struct{ what, path string }{
		{"the shell's second try, a read of the admission", "/v1/app/lifetime"},
		{"the closing shell's read of the session", "/v1/operating-session"},
	} {
		ctx, giveUp := context.WithCancel(context.Background())
		defer giveUp()
		w := orderWatch(t, admissionReadsOrder)
		retry := b.sent(b.shellRequest(ctx, lifeB, http.MethodGet, given.path, ""))
		shutOut(w, given.what, retry)
		settles("with "+given.what+" waiting", 2)
		giveUp()
		if err := finished(t, "the shell's own end of "+given.what, retry); err == nil {
			t.Fatalf("%s was answered while the admission held the order", given.what)
		}
		settles("with "+given.what+" given up on, and the admission still in its wait", 1)
		if n := waitingForTheOrder(); n != 1 {
			t.Fatalf("after %s was given up on, %d goroutines wait for or hold the admission's order; want the admission alone", given.what, n)
		}
	}
	if b.sessions() != 1 || b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ?`, leftover) != 1 {
		t.Fatal("the leftover session was judged while a contact was still to be stored")
	}

	// The contact is stored. The start completes without anyone waiting for
	// its answer, and the shell's next read finds itself current.
	release()
	settles("once the start has completed", 0)
	if st := b.readAdmission(t, b.c, lifeB); !st.Current || st.Generation != read.Generation+1 {
		t.Fatalf("the app's next read says %+v; want it current after one admission (generation %d)", st, read.Generation+1)
	}
	v := b.view(lifeB)
	if v.State != sessionStateRecoveryPending || v.Session == nil || v.Session.ID != leftover || b.memberOf(qso) != leftover || b.sessions() != 1 {
		t.Fatalf("the app reads %q with %+v; the contact is in %q; %d sessions; want to be asked about %s, with the contact in it, and no session opened or discarded",
			v.State, v.Session, b.memberOf(qso), b.sessions(), leftover)
	}
	if b.drains != 2 {
		t.Fatalf("%d waits; want one for each admission that started a session", b.drains)
	}
}

// A read whose caller has already gone is not made, even when the order is
// free to take (Codex's review, codex-263). Taking a free order for a request
// that is over would run the read with that request's cancelled context: the
// session's read would then fail in the database for no fault of the file's,
// log it as one, and try to answer 503 to nobody. The order is left free: a
// reader who is still there gets it at once.
func TestAdmission_AReadWhoseCallerHasGoneIsNotMade(t *testing.T) {
	a := &appAdmission{}
	a.setCurrent(lifeA)
	gone, cancel := context.WithCancel(context.Background())
	cancel()

	ran := false
	if a.observeUnlessGone(gone, func(string) { ran = true }) || ran {
		t.Fatalf("a read for a caller that had gone was made (ran %v) with the order free", ran)
	}
	if _, _, answered := a.read(gone, lifeA); answered {
		t.Fatal("the admission was read for a caller that had gone")
	}
	if !a.mu.TryLock() {
		t.Fatal("a read that was not made left the order held")
	}
	a.mu.Unlock()

	var seen string
	if !a.observeUnlessGone(context.Background(), func(current string) { seen = current }) || seen != lifeA {
		t.Fatalf("a reader who is there saw %q; want the read made, with the lifetime held", seen)
	}
	if _, current, answered := a.read(context.Background(), lifeA); !answered || !current {
		t.Fatalf("a reader who is there: answered %v, current %v; want both", answered, current)
	}
}

// A guard on the source: giving up the wait for the admission's order is for
// the two reads only. Everything that changes the admission or the session
// takes the order with Lock and is not abandoned with its caller: an admission
// or an exit that stopped halfway because a request was cancelled would leave
// the scopes stopped, or a seal taken, with nobody to finish.
//
// The guard is on the admission's own mutex and on the path to it: a TryLock
// of that mutex is made in tryForRead alone; tryForRead is called by
// lockUnlessGone alone; that by the two reads; and the session's read by its
// handler. Another mutex in the package, or one acquisition written once and
// called twice, is no business of this guard's.
func TestSource_OnlyTheTwoReadsGiveUpTheOrder(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// ofTheAdmission is whether e names the admission's mutex: mu of a method's
	// own appAdmission receiver, or mu of a field named admission.
	ofTheAdmission := func(e ast.Expr, receiver string) bool {
		mu, ok := e.(*ast.SelectorExpr)
		if !ok || mu.Sel.Name != "mu" {
			return false
		}
		switch x := mu.X.(type) {
		case *ast.Ident:
			return receiver != "" && x.Name == receiver
		case *ast.SelectorExpr:
			return x.Sel.Name == "admission"
		}
		return false
	}
	fset := token.NewFileSet()
	callers := map[string]map[string]bool{} // the function called -> the functions that call it
	note := func(called, from string) {
		if callers[called] == nil {
			callers[called] = map[string]bool{}
		}
		callers[called][from] = true
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			receiver := ""
			if fn.Recv != nil && len(fn.Recv.List) == 1 && len(fn.Recv.List[0].Names) == 1 {
				if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
					if id, ok := star.X.(*ast.Ident); ok && id.Name == "appAdmission" {
						receiver = fn.Recv.List[0].Names[0].Name
					}
				}
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "TryLock":
					if ofTheAdmission(sel.X, receiver) {
						note("TryLock of the admission's order", fn.Name.Name)
					}
				case "tryForRead", "lockUnlessGone", "observeUnlessGone":
					note(sel.Sel.Name, fn.Name.Name)
				}
				return true
			})
		}
	}
	for called, want := range map[string][]string{
		"TryLock of the admission's order": {"tryForRead"},
		"tryForRead":                       {"lockUnlessGone"},
		"lockUnlessGone":                   {"observeUnlessGone", "read"},
		"observeUnlessGone":                {"handleOperatingSessionRead"},
	} {
		var got []string
		for from := range callers[called] {
			got = append(got, from)
		}
		sort.Strings(got)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s is called from %v; want exactly %v", called, got, want)
		}
	}
}
