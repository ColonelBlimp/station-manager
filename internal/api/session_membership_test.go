package api

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/events"
)

/*
   W-0029 part 5, slice 1, at the daemon's routes: the submit route's own
   classification reaches the contact's transaction (A1, B3, B4); the session a
   contact opens is held by the lifetime the admission has published (D); a
   change is published once, with the served archive and the generation the
   contact's transaction produced (F); and logging never waits for the
   admission's order (C5).

   Each case goes through the app listener's real routes with the membership
   connected as part 6 will connect it (ConnectSessionMembership), and is
   judged on the file's rows and the read route together. The unconnected
   server, which is what the daemon's ordinary start is until part 6, has its
   own case at the end.
*/

// memberBed is an exit bed with the membership connected and a second logbook.
type memberBed struct {
	*exitBed
	def, other int64  // the default logbook and another
	otherUUID  string // the other logbook's uuid
	n          int
}

func newMemberBed(t *testing.T, connect bool) *memberBed {
	t.Helper()
	b := &memberBed{exitBed: newExitBed(t, true)}
	id, _, err := b.srv.effectiveDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b.def = id
	b.other = createTestLogbook(t, b.srv, "Other", "G4ABC")
	b.otherUUID = logbookUUID(t, b.srv, b.other)
	if connect {
		b.srv.ConnectSessionMembership()
	}
	return b
}

type loggedAnswer struct {
	status int
	code   string
	id     int64
	stored string
}

// log sends one contact to the submit route as a page does: the logbook it
// names, and the default it asserts. Each call is a contact no other made.
func (b *memberBed) log(lifetime string, names int64, asserts string) loggedAnswer {
	b.t.Helper()
	b.n++
	adif := fmt.Sprintf("<CALL:6>M%dABCD<BAND:3>20m<MODE:3>SSB<FREQ:6>14.210<QSO_DATE:8>20261120<TIME_ON:4>10%02d<EOR>", b.n%10, b.n)
	r := b.acceptable(b.t, true, http.MethodPost, fmt.Sprintf("/v1/qso?logbook=%d", names), b.cred)
	if lifetime != "" {
		r.Header.Set(appLifetimeHeader, lifetime)
	}
	r.Header.Set(defaultLogbookHeader, asserts)
	r.Header.Set("Content-Type", "application/x-adif")
	r.Body = io.NopCloser(strings.NewReader(adif))
	r.ContentLength = int64(len(adif))
	resp, err := b.c.Do(r)
	if err != nil {
		b.t.Fatalf("submit: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	a := loggedAnswer{status: resp.StatusCode}
	if resp.StatusCode >= 400 {
		a.code = errorCode(b.t, raw)
		return a
	}
	var body struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		b.t.Fatalf("submit: %v in %s", err, raw)
	}
	a.id, a.stored = body.ID, body.Status
	return a
}

// live is a contact the route classifies as live: it names the default and
// asserts it. It must be stored.
func (b *memberBed) live(lifetime string) int64 {
	b.t.Helper()
	a := b.log(lifetime, b.def, b.logbook)
	if a.status != http.StatusCreated || a.stored != "stored" {
		b.t.Fatalf("a live submit: %+v", a)
	}
	return a.id
}

// memberOf is the session a QSO is a member of, "" for none.
func (b *memberBed) memberOf(qso int64) string {
	b.t.Helper()
	var id string
	rows, err := b.raw.Query(`SELECT session_id FROM operating_session_member WHERE qso_id = ?`, qso)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			b.t.Fatal(err)
		}
	}
	return id
}

// holderOf is the lifetime that holds a session's open interval, "" for none.
func (b *memberBed) holderOf(session string) string {
	b.t.Helper()
	if session == "" {
		return "(no session)"
	}
	var h string
	err := b.raw.QueryRow(`SELECT COALESCE(app_lifetime, '') FROM operating_session_interval
		WHERE session_id = ? AND end_state = 'open'`, session).Scan(&h)
	if err != nil {
		b.t.Fatalf("the open interval of %s: %v", session, err)
	}
	return h
}

// changes subscribes to the hub and returns a reader of the session changes
// published since the last read, each checked to name the served archive.
func (b *memberBed) changes() (read func() []int64) {
	b.t.Helper()
	ch, cancel := b.srv.hub.Subscribe()
	b.t.Cleanup(cancel)
	return func() []int64 {
		b.t.Helper()
		var out []int64
		for {
			select {
			case ev := <-ch:
				if ev.Name != events.NameOperatingSessionChanged {
					continue
				}
				p := ev.Payload.(events.OperatingSessionChangedPayload)
				if p.ArchiveID != testServedArchive {
					b.t.Fatalf("a session change names archive %q, want the served one", p.ArchiveID)
				}
				out = append(out, p.Generation)
			default:
				return out
			}
		}
	}
}

// A1, B3, B4: the route's classification is what decides. A submit that names
// the default the page asserts is live and joins; one that names the default
// while asserting another logbook, and one that names another logbook, are
// deliberate: stored, and in no session.
func TestMembership_Route_TheClassificationReachesTheTransaction(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	session := b.view(lifeA).Session.ID

	live := b.live(lifeA)
	if got := b.memberOf(live); got != session {
		t.Fatalf("the live contact is a member of %q, want the open session %s", got, session)
	}
	before := b.file(lifeA).Generation

	deliberate := b.log(lifeA, b.def, b.otherUUID) // names A, asserts B
	if deliberate.status != http.StatusCreated || deliberate.stored != "stored" {
		t.Fatalf("a submit naming the default while asserting another logbook: %+v; want it stored, as today", deliberate)
	}
	elsewhere := b.log(lifeA, b.other, b.logbook) // names B, asserts A
	if elsewhere.status != http.StatusCreated || elsewhere.stored != "stored" {
		t.Fatalf("a submit naming another logbook: %+v; want it stored", elsewhere)
	}
	if b.memberOf(deliberate.id) != "" || b.memberOf(elsewhere.id) != "" {
		t.Fatalf("a deliberate submit joined a session: %q and %q", b.memberOf(deliberate.id), b.memberOf(elsewhere.id))
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_member`); n != 1 {
		t.Fatalf("%d member rows, want the live contact's alone", n)
	}
	if now := b.file(lifeA); now.Generation != before || now.Open == nil || now.Open.ID != session {
		t.Fatalf("the deliberate submits moved the session state: generation %d to %d, open %+v", before, now.Generation, now.Open)
	}
}

// F1, F2, F4, and D's last ordering: a live contact before any app has been
// admitted opens a session held by nobody, and that is published once, with
// the generation the contact produced. A second contact joins it and nothing
// is published; the read route counts both at the same generation. The next
// app to start is asked.
func TestMembership_Route_AContactWithNoAppOpensASessionHeldByNobody(t *testing.T) {
	b := newMemberBed(t, true)
	changes := b.changes()
	start := b.file("").Generation

	first := b.live("")
	session := b.memberOf(first)
	if session == "" || b.holderOf(session) != "" {
		t.Fatalf("the contact joined %q; want a new session held by nobody", session)
	}
	if got := changes(); len(got) != 1 || got[0] != start+1 || b.file("").Generation != start+1 {
		t.Fatalf("the session a contact opened was published as %v with the file at generation %d; want [%d]", got, b.file("").Generation, start+1)
	}

	second := b.live("")
	if b.memberOf(second) != session {
		t.Fatalf("the second contact joined %q, want %s", b.memberOf(second), session)
	}
	if got := changes(); len(got) != 0 {
		t.Fatalf("a plain join published %v", got)
	}
	v := b.view("")
	if v.Generation != start+1 || v.Session == nil || !v.Session.HasContacts || v.Session.ContactCount == nil || *v.Session.ContactCount != 2 {
		t.Fatalf("the read says generation %d and %+v; want generation %d and two contacts", v.Generation, v.Session, start+1)
	}

	b.mustBeAdmitted(lifeA)
	if v := b.view(lifeA); v.State != sessionStateRecoveryPending || v.Session == nil || v.Session.ID != session {
		t.Fatalf("the next app reads %q with %+v; want to be asked about %s", v.State, v.Session, session)
	}
}

// D1: after a confirmed exit the admission publishes nobody as holding, so a
// contact logged then opens a session held by nobody, and the next start asks.
func TestMembership_Route_AfterAConfirmedExitAContactsSessionIsHeldByNobody(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	if a := b.exitAs(lifeA, b.file(lifeA).Generation, testServedArchive); a.status != http.StatusOK {
		t.Fatalf("the exit: %+v", a)
	}
	if b.openSessions() != 0 {
		t.Fatalf("%d sessions open after the exit, want none", b.openSessions())
	}

	qso := b.live("") // a tool, or a page left open: no app is admitted
	session := b.memberOf(qso)
	if session == "" || b.holderOf(session) != "" {
		t.Fatalf("the contact joined %q held by %q; want a new session held by nobody", session, b.holderOf(session))
	}
	b.mustBeAdmitted(lifeB)
	if v := b.view(lifeB); v.State != sessionStateRecoveryPending {
		t.Fatalf("the next app reads %q; want to be asked", v.State)
	}
}

// D1: who the admission publishes as holding, read where the logging step
// reads it. Nobody before any admission; the lifetime admitted; still that
// lifetime after a replacement that failed, although its scopes are stopped;
// the new lifetime after one that succeeded; nobody after a confirmed exit.
// (After an exit the file's own record of it would keep a session from being
// given to that lifetime anyway, so no row shows whether this value was
// cleared: it is read here.)
func TestMembership_TheAdmissionPublishesWhoHolds(t *testing.T) {
	b := newMemberBed(t, true)
	want := func(when, lifetime string) {
		t.Helper()
		if got := b.srv.admission.holding(); got != lifetime {
			t.Fatalf("%s the admission publishes %q as holding, want %q", when, got, lifetime)
		}
	}
	want("before any admission", "")
	b.mustBeAdmitted(lifeA)
	want("after A's admission", lifeA)

	restore := b.failSessionWrites()
	if status, _ := b.admitted(lifeB); status == http.StatusOK {
		t.Fatal("the replacement succeeded with the file failing")
	}
	restore()
	wantScopes(t, b.sessionBed, "after the failed replacement", "retire", "publish "+lifeA, "retire")
	want("after a replacement that failed", lifeA)

	b.mustBeAdmitted(lifeB)
	want("after a replacement that succeeded", lifeB)
	if a := b.exitAs(lifeB, b.file(lifeB).Generation, testServedArchive); a.status != http.StatusOK {
		t.Fatalf("the exit: %+v", a)
	}
	want("after the confirmed exit", "")
}

// failedReopen leaves app A as a switch that failed to reopen leaves it
// (TestSwitchSession_AFailedReopenIsRepairedByTheAppsOwnFlow): still the
// lifetime the admission holds, its scopes stopped, no session open, and the
// file writable again.
func (b *memberBed) failedReopen() {
	b.t.Helper()
	b.mustBeAdmitted(lifeA)
	if err := b.switchClose(); err != nil {
		b.t.Fatalf("close: %v", err)
	}
	restore := b.failSessionWrites()
	b.switchFailed()
	restore()
	if st := b.readAdmission(b.t, b.c, lifeA); st.Current || b.openSessions() != 0 {
		b.t.Fatalf("the fixture: the app reads %+v with %d sessions open; want not current and none", st, b.openSessions())
	}
}

// D4, first ordering: a running app with no session and its scopes stopped,
// and a live contact that takes the writer's position before the app is
// admitted again. The session is the app's; its next admission keeps it and
// asks nothing.
func TestMembership_Route_AContactBeforeTheAppsRepairOpensTheAppsSession(t *testing.T) {
	b := newMemberBed(t, true)
	b.failedReopen()

	qso := b.live(lifeA)
	session := b.memberOf(qso)
	if session == "" || b.holderOf(session) != lifeA {
		t.Fatalf("the contact joined %q held by %q; want a new session held by the running app", session, b.holderOf(session))
	}
	if status, code, posted := b.shellAdmits(lifeA); !posted || status != http.StatusOK {
		t.Fatalf("the app's own admission: %d %s, admit sent %v", status, code, posted)
	}
	v := b.view(lifeA)
	if v.State != sessionStateOpen || v.Session == nil || v.Session.ID != session || !v.Session.HeldByYou || !v.Session.HasContacts {
		t.Fatalf("after its admission the app reads %q with %+v; want the same session, its own, with the contact, and no question", v.State, v.Session)
	}
	if b.sessions() != 1 {
		t.Fatalf("%d sessions, want the one the contact opened", b.sessions())
	}
}

// D4, second ordering: the same, and another app replaces the first. It finds
// the first app's session with the contact in it, and is asked.
func TestMembership_Route_AContactBeforeAReplacementLeavesTheNewAppAQuestion(t *testing.T) {
	b := newMemberBed(t, true)
	b.failedReopen()

	qso := b.live(lifeA)
	session := b.memberOf(qso)
	if session == "" || b.holderOf(session) != lifeA {
		t.Fatalf("the contact joined %q held by %q; want a new session held by the running app", session, b.holderOf(session))
	}
	b.mustBeAdmitted(lifeB)
	v := b.view(lifeB)
	if v.State != sessionStateRecoveryPending || v.Session == nil || v.Session.ID != session || v.Session.HeldByYou {
		t.Fatalf("the new app reads %q with %+v; want to be asked about the first app's session", v.State, v.Session)
	}
	if b.memberOf(qso) != session || b.holderOf(session) != lifeA {
		t.Fatal("the replacement moved the contact or took the session")
	}
}

// C5: logging never takes the admission's order. With that order held, as an
// app start, an exit or an archive operation holds it, a live contact that
// opens a session is stored and answered, and its change published.
func TestMembership_Route_LoggingNeverWaitsForTheAdmissionsOrder(t *testing.T) {
	b := newMemberBed(t, true)
	changes := b.changes()
	release := b.srv.HoldSessionOrder()
	released := false
	letGo := func() {
		if !released {
			released = true
			release()
		}
	}
	defer letGo()

	answered := make(chan int64, 1)
	go func() {
		a := b.log("", b.def, b.logbook)
		if a.status != http.StatusCreated {
			answered <- 0
			return
		}
		answered <- a.id
	}()
	select {
	case qso := <-answered:
		if qso == 0 || b.memberOf(qso) == "" {
			t.Fatalf("with the order held the contact was answered as %d and joined %q; want it stored and a member", qso, b.memberOf(qso))
		}
		if got := changes(); len(got) != 1 {
			t.Fatalf("with the order held the contact's session was published as %v, want once", got)
		}
	case <-time.After(10 * time.Second):
		letGo()
		t.Fatal("a live contact waited for the admission's order")
	}
}

// C3 at the route: a failure in the logging step is the answer a failed
// insert gives, and nothing is stored.
func TestMembership_Route_AFailedStepIsAFailedSubmit(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	b.exec(`CREATE TRIGGER injected_failure BEFORE INSERT ON operating_session_member
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	before := b.count(`SELECT COUNT(*) FROM qso`)
	a := b.log(lifeA, b.def, b.logbook)
	if a.status != http.StatusInternalServerError || a.code != "submit_failed" {
		t.Fatalf("the submit answered %+v; want 500 submit_failed", a)
	}
	if n := b.count(`SELECT COUNT(*) FROM qso`); n != before {
		t.Fatalf("%d contacts stored by a failed submit", n-before)
	}
}

// Ruling 1 (2026-11-24): the server as the daemon's ordinary start leaves it
// until part 6. A live contact is stored, no member row is written, no session
// is opened by it, and no start asks about it.
func TestMembership_Route_Unconnected_NothingJoinsAndNoStartAsks(t *testing.T) {
	b := newMemberBed(t, false)
	b.mustBeAdmitted(lifeA)
	session := b.view(lifeA).Session.ID
	qso := b.live(lifeA)
	if n := b.count(`SELECT COUNT(*) FROM operating_session_member`); n != 0 || b.memberOf(qso) != "" {
		t.Fatalf("an unconnected server wrote %d member rows", n)
	}
	if v := b.view(lifeA); v.Session == nil || v.Session.ID != session || v.Session.HasContacts {
		t.Fatalf("the session is %+v; want the same one, without contacts", v.Session)
	}
	// The app is killed, and the next one starts: nothing to ask about.
	b.restart()
	b.mustBeAdmitted(lifeB)
	if v := b.view(lifeB); v.State != sessionStateOpen || v.Session == nil || !v.Session.HeldByYou {
		t.Fatalf("the next app reads %q with %+v; want a session of its own and no question", v.State, v.Session)
	}
}

// heldLifetimeWrites is every way a node writes the lifetime the admission
// holds or the value it publishes: an assignment, an increment, taking either
// field's address, a literal of the admission that sets one, and any call on
// the published value but a load.
func heldLifetimeWrites(n ast.Node) (what []string) {
	field := func(e ast.Expr, names ...string) string {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		for _, name := range names {
			if sel.Sel.Name == name {
				return name
			}
		}
		return ""
	}
	add := func(name string) {
		if name != "" {
			what = append(what, name)
		}
	}
	switch v := n.(type) {
	case *ast.AssignStmt:
		for _, lhs := range v.Lhs {
			add(field(lhs, "current", "holder"))
		}
	case *ast.IncDecStmt:
		add(field(v.X, "current", "holder"))
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			add(field(v.X, "current", "holder"))
		}
	case *ast.CompositeLit:
		if id, ok := v.Type.(*ast.Ident); ok && id.Name == "appAdmission" {
			for _, el := range v.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok && (key.Name == "current" || key.Name == "holder") {
						add(key.Name)
					}
				}
			}
		}
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name != "Load" {
			add(field(sel.X, "holder"))
		}
	}
	return what
}

// D2, as a guard on the source: the lifetime the admission holds, and the
// value it publishes for the logging step, are written by setCurrent alone, so
// the two cannot come apart. Every way of writing either (heldLifetimeWrites)
// is refused anywhere else in the package's production code.
func TestSource_TheHeldLifetimeIsWrittenOnlyBySetCurrent(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	inSetter := map[string]int{}
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
			ast.Inspect(decl, func(n ast.Node) bool {
				for _, what := range heldLifetimeWrites(n) {
					if where == "setCurrent" {
						inSetter[what]++
						continue
					}
					t.Errorf("%s: %s writes %s; only setCurrent may", fset.Position(n.Pos()), where, what)
				}
				return true
			})
		}
	}
	if inSetter["current"] != 1 || inSetter["holder"] != 1 || len(inSetter) != 2 {
		t.Fatalf("setCurrent writes %v; want current once and holder once, together", inSetter)
	}
}
