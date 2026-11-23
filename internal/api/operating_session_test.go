package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
)

// The operating session's lifecycle (ADR 0100; W-0029 part 2, groups A, B, D
// and F): the start step inside the app's admission, the one read route, and
// the event. Every case goes through the real routes of the app's listener,
// and is judged on the admission's state and the file's rows together.

type sessionView struct {
	ArchiveID  string `json:"archive_id"`
	Generation int64  `json:"generation"`
	State      string `json:"state"`
	Session    *struct {
		ID               string    `json:"id"`
		LogbookUUID      string    `json:"logbook_uuid"`
		OpenedAt         time.Time `json:"opened_at"`
		SittingStartedAt time.Time `json:"sitting_started_at"`
		HasContacts      bool      `json:"has_contacts"`
		ContactCount     *int      `json:"contact_count"` // a pointer: a count left out is not a count of 0
		HeldByYou        bool      `json:"held_by_you"`
	} `json:"session"`
}

var sessionMorning = time.Date(2026, 11, 20, 9, 15, 0, 0, time.UTC)

// sessionBed is a checked server as the daemon is when it serves: the archive
// file has its identity row, and with a default, a default logbook the file
// points at. scope records what the admission does to an operating scope.
type sessionBed struct {
	checked
	t       *testing.T
	c       *http.Client
	raw     *sql.DB // a second connection to the server's file, for the test's own rows
	scope   *recordingScope
	logbook string // the default logbook's uuid, "" when the bed has none
}

func newSessionBed(t *testing.T, withDefault bool) *sessionBed {
	t.Helper()
	l := startChecked(t)
	_, c := l.clients(t)
	b := &sessionBed{checked: l, t: t, c: c, scope: &recordingScope{}}
	b.adopt(l.srv)
	raw, err := sql.Open(sqlite.SqliteDriver, "file:"+l.srv.db.DatabaseConfig.Path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	b.raw = raw
	ctx := context.Background()
	if _, err := l.srv.db.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatalf("ensure identity: %v", err)
	}
	if withDefault {
		b.logbook = ensureDefaultLogbook(t, l.srv)
		id, _, err := l.srv.effectiveDefault(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.srv.db.SetArchiveDefaultLogbookWithContext(ctx, id); err != nil {
			t.Fatalf("record the default in the file: %v", err)
		}
	}
	return b
}

// adopt gives a server the bed's recording scope and a clock the test reads.
func (b *sessionBed) adopt(srv *Server) {
	srv.admission.scopes = append(srv.admission.scopes, b.scope)
	srv.sessionClock = func() time.Time { return sessionMorning }
}

// restart is what a daemon restart does to the admission: the archive file is
// the same, and the new process has no lifetime current and nothing retired in
// memory.
func (b *sessionBed) restart() {
	b.t.Helper()
	a := &b.srv.admission
	a.mu.Lock()
	a.current, a.generation, a.retired, a.stopped, a.closing = "", 0, nil, false, false
	a.published.Store(0)
	a.mu.Unlock()
	b.scope.mu.Lock()
	b.scope.events = nil
	b.scope.mu.Unlock()
}

func (b *sessionBed) exec(q string, args ...any) {
	b.t.Helper()
	if _, err := b.raw.Exec(q, args...); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
}

func (b *sessionBed) count(q string, args ...any) int {
	b.t.Helper()
	var n int
	if err := b.raw.QueryRow(q, args...).Scan(&n); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// file is the session state as the file holds it, read for a lifetime.
func (b *sessionBed) file(lifetime string) sqlite.OperatingSessionState {
	b.t.Helper()
	st, err := b.srv.db.OperatingSessionStateWithContext(context.Background(), lifetime)
	if err != nil {
		b.t.Fatalf("read the file's session state: %v", err)
	}
	return st
}

// view is the read route's answer for a lifetime.
func (b *sessionBed) view(lifetime string) sessionView {
	b.t.Helper()
	status, body, _ := b.appDo(b.t, b.c, lifetime, http.MethodGet, "/v1/operating-session", "")
	if status != http.StatusOK {
		b.t.Fatalf("read the session as %q: %d %s", lifetime, status, body)
	}
	var v sessionView
	if err := json.Unmarshal(body, &v); err != nil {
		b.t.Fatalf("read the session as %q: %v in %s", lifetime, err, body)
	}
	return v
}

// admitted reads and admits a lifetime and returns the status and the code.
func (b *sessionBed) admitted(lifetime string) (int, string) {
	b.t.Helper()
	return b.admit(b.t, b.c, lifetime, b.readAdmission(b.t, b.c, lifetime))
}

func (b *sessionBed) mustBeAdmitted(lifetime string) {
	b.t.Helper()
	if status, code := b.admitted(lifetime); status != http.StatusOK {
		b.t.Fatalf("admit %q: %d %s", lifetime, status, code)
	}
}

// contact puts a QSO of the default logbook into a session: the member row
// part 5 will write, put here by the test.
func (b *sessionBed) contact(session string) {
	b.t.Helper()
	b.exec(`INSERT INTO qso (uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		SELECT '01920000-0000-7000-8000-00000000c001', 'G4ABC', '40m', 'SSB', 7050000, '20261120', '0915', '0915', '59', '59', 'Test',
			printf('%064d', 1), id FROM logbook WHERE uuid = ?`, b.logbook)
	b.exec(`INSERT INTO operating_session_member (qso_id, session_id) SELECT MAX(id), ? FROM qso`, session)
}

// failSessionWrites makes every control on the session record fail, as a
// database that cannot be written would, until the returned func is called.
func (b *sessionBed) failSessionWrites() (restore func()) {
	b.t.Helper()
	b.exec(`CREATE TRIGGER injected_failure BEFORE UPDATE OF session_generation ON archive_metadata
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	return func() { b.t.Helper(); b.exec(`DROP TRIGGER injected_failure`) }
}

func (b *sessionBed) scopeEvents() []string {
	b.scope.mu.Lock()
	defer b.scope.mu.Unlock()
	return append([]string(nil), b.scope.events...)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- B: the start step, inside the admission ----

func TestOperatingSession_AnAdmissionOpensOne_AndTwiceIsOnce(t *testing.T) {
	b := newSessionBed(t, true)
	b.mustBeAdmitted(lifeA)

	v := b.view(lifeA)
	if v.State != "open" || v.Session == nil || !v.Session.HeldByYou || v.Session.HasContacts || v.Generation != 1 ||
		v.ArchiveID != testServedArchive || v.Session.LogbookUUID != b.logbook ||
		!v.Session.OpenedAt.Equal(sessionMorning) || !v.Session.SittingStartedAt.Equal(sessionMorning) {
		t.Fatalf("after the admission the route says %+v with %+v", v, v.Session)
	}
	if st := b.file(lifeA); st.Open == nil || st.Open.ID != v.Session.ID || st.Open.Holder != lifeA || st.Generation != 1 {
		t.Fatalf("the file holds %+v with %+v", st, st.Open)
	}
	if other := b.view(lifeB); other.Session == nil || other.Session.HeldByYou || other.Session.ID != v.Session.ID {
		t.Fatalf("another lifetime reads %+v with %+v; want the same session, not its own", other, other.Session)
	}

	// The same admission again, and a retry after a lost answer: never a second session.
	for i := 0; i < 2; i++ {
		b.mustBeAdmitted(lifeA)
	}
	if again := b.view(lifeA); again.Session == nil || again.Session.ID != v.Session.ID || again.Generation != 1 {
		t.Fatalf("after two more admissions: %+v with %+v", again, again.Session)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 1 {
		t.Fatalf("%d sessions in the file, want 1", n)
	}
}

func TestOperatingSession_AnAdmissionWithNoDefaultLogbookIsSetupPending(t *testing.T) {
	b := newSessionBed(t, false)
	b.mustBeAdmitted(lifeA)
	if v := b.view(lifeA); v.State != "setup_pending" || v.Session != nil || v.Generation != 0 {
		t.Fatalf("the route says %+v", v)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 0 {
		t.Fatalf("%d session(s) opened with no default logbook", n)
	}
	if st := b.readAdmission(t, b.c, lifeA); !st.Current {
		t.Fatal("the lifetime was not admitted; it must be, to reach setup")
	}
}

func TestOperatingSession_AnAdmissionMeetsAnotherLifetimesSession(t *testing.T) {
	t.Run("with a contact: recovery pending, and nothing changes", func(t *testing.T) {
		b := newSessionBed(t, true)
		b.mustBeAdmitted(lifeA)
		first := b.view(lifeA)
		b.contact(first.Session.ID)

		b.mustBeAdmitted(lifeB)
		v := b.view(lifeB)
		if v.State != "recovery_pending" || v.Session == nil || v.Session.ID != first.Session.ID || v.Session.HeldByYou || !v.Session.HasContacts {
			t.Fatalf("the route says %+v with %+v", v, v.Session)
		}
		if st := b.file(lifeB); st.Open == nil || st.Open.Holder != lifeA || st.Generation != first.Generation {
			t.Fatalf("the file holds %+v with %+v; want the earlier lifetime's session, untouched", st, st.Open)
		}
		if st := b.readAdmission(t, b.c, lifeB); !st.Current {
			t.Fatal("the second lifetime was not admitted")
		}
	})
	t.Run("empty: it is discarded and a new one opened", func(t *testing.T) {
		b := newSessionBed(t, true)
		b.mustBeAdmitted(lifeA)
		first := b.view(lifeA)

		b.mustBeAdmitted(lifeB)
		v := b.view(lifeB)
		if v.State != "open" || v.Session == nil || v.Session.ID == first.Session.ID || !v.Session.HeldByYou || v.Generation != first.Generation+1 {
			t.Fatalf("the route says %+v with %+v", v, v.Session)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 1 {
			t.Fatalf("%d sessions in the file, want only the new one", n)
		}
	})
}

// A recorded exit comes first: before any scope is stopped, and across a
// daemon restart, which forgets the in-memory retirement.
func TestOperatingSession_ARecordedExitIsRetiredBeforeAnyScopeIsStopped(t *testing.T) {
	b := newSessionBed(t, true)
	b.mustBeAdmitted(lifeA)
	if _, outcome, err := b.srv.db.RecordOperatingAppExitWithContext(context.Background(), lifeA, b.file(lifeA).Generation, true, sessionMorning); err != nil || outcome != sqlite.SessionApplied {
		t.Fatalf("record the exit: %q, %v", outcome, err)
	}

	b.restart()
	b.mustBeAdmitted(lifeB)
	during := b.view(lifeB)
	before := b.scopeEvents()

	status, code := b.admitted(lifeA)
	if status != http.StatusConflict || code != "app_lifetime_retired" {
		t.Fatalf("the exited lifetime's admission: %d %s; want 409 app_lifetime_retired", status, code)
	}
	if after := b.scopeEvents(); !sameStrings(before, after) {
		t.Fatalf("the retired caller reached the scopes: %v became %v", before, after)
	}
	if v := b.view(lifeB); v.Session == nil || v.Session.ID != during.Session.ID || !v.Session.HeldByYou || v.Generation != during.Generation {
		t.Fatalf("the current lifetime's session changed: %+v with %+v", v, v.Session)
	}
	if st := b.readAdmission(t, b.c, lifeB); !st.Current {
		t.Fatal("the current lifetime is no longer current")
	}
}

// ---- A: a daemon restart ----

func TestOperatingSession_ADaemonRestartChangesNoSession(t *testing.T) {
	b := newSessionBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)

	b.restart()
	if st := b.readAdmission(t, b.c, lifeA); st.Current || st.Generation != 0 {
		t.Fatalf("a new process has %+v; want no lifetime current", st)
	}
	if st := b.file(lifeA); st.Open == nil || st.Open.ID != before.Session.ID || st.Generation != before.Generation {
		t.Fatalf("the restart changed the file: %+v with %+v", st, st.Open)
	}
	b.mustBeAdmitted(lifeA)
	after := b.view(lifeA)
	if after.Session == nil || after.Session.ID != before.Session.ID || !after.Session.HeldByYou || after.Generation != before.Generation {
		t.Fatalf("after the same app was admitted again: %+v with %+v; want the session it held", after, after.Session)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 1 {
		t.Fatalf("%d sessions in the file, want 1", n)
	}
}

// ---- B4: a step that fails does not admit, and how it recovers ----

func wantScopes(t *testing.T, b *sessionBed, what string, want ...string) {
	t.Helper()
	if got := b.scopeEvents(); !sameStrings(got, want) {
		t.Fatalf("%s: the scope saw %v, want %v", what, got, want)
	}
}

func TestOperatingSession_AFailedStepDoesNotAdmit(t *testing.T) {
	t.Run("the first admission in a process", func(t *testing.T) {
		b := newSessionBed(t, true)
		restore := b.failSessionWrites()
		status, code := b.admitted(lifeA)
		if status != http.StatusServiceUnavailable || code != "operating_session_unavailable" {
			t.Fatalf("the admission: %d %s; want 503 operating_session_unavailable", status, code)
		}
		if st := b.readAdmission(t, b.c, lifeA); st.Current || st.Generation != 0 {
			t.Fatalf("after the failure the admission is %+v; want nobody current at generation 0", st)
		}
		if st := b.file(lifeA); st.Open != nil || st.Generation != 0 {
			t.Fatalf("the failure changed the file: %+v", st)
		}
		wantScopes(t, b, "after the failure", "retire")

		restore()
		b.mustBeAdmitted(lifeA)
		wantScopes(t, b, "after the recovery", "retire", "retire", "publish "+lifeA)
		if v := b.view(lifeA); v.State != "open" || !v.Session.HeldByYou {
			t.Fatalf("after the recovery the route says %+v", v)
		}
	})
	t.Run("a replacement: the earlier lifetime stays current and the scopes authorize nobody", func(t *testing.T) {
		b := newSessionBed(t, true)
		b.mustBeAdmitted(lifeA)
		held := b.view(lifeA)
		restore := b.failSessionWrites()

		if status, code := b.admitted(lifeB); status != http.StatusServiceUnavailable || code != "operating_session_unavailable" {
			t.Fatalf("the replacement: %d %s; want 503 operating_session_unavailable", status, code)
		}
		// The admission still holds A, at generation 1, and has retired nobody;
		// but with its scopes stopped A does not read as current, so its app
		// sends the admit that repairs it.
		if a, bb := b.readAdmission(t, b.c, lifeA), b.readAdmission(t, b.c, lifeB); a.Current || bb.Current || a.Generation != 1 {
			t.Fatalf("after the failure: A %+v, B %+v; want neither reading as current, at generation 1", a, bb)
		}
		if st := b.file(lifeA); st.Open == nil || st.Open.ID != held.Session.ID || st.Generation != held.Generation {
			t.Fatalf("the failure changed the file: %+v with %+v", st, st.Open)
		}
		wantScopes(t, b, "after the failure", "retire", "publish "+lifeA, "retire")

		// The current lifetime's own admission, while the file still fails: it
		// must not answer success and leave the scopes authorizing nobody.
		if status, code := b.admitted(lifeA); status != http.StatusServiceUnavailable || code != "operating_session_unavailable" {
			t.Fatalf("the current lifetime's repeat while the file fails: %d %s; want 503", status, code)
		}
		wantScopes(t, b, "after that repeat", "retire", "publish "+lifeA, "retire")

		restore()
		b.mustBeAdmitted(lifeA) // it completes what the failure left undone
		wantScopes(t, b, "after the current lifetime's recovery", "retire", "publish "+lifeA, "retire", "publish "+lifeA)
		if v := b.view(lifeA); v.Session == nil || v.Session.ID != held.Session.ID || v.Generation != held.Generation {
			t.Fatalf("the recovery changed the session: %+v with %+v", v, v.Session)
		}
		// With nothing left undone, a further repeat changes nothing.
		b.mustBeAdmitted(lifeA)
		wantScopes(t, b, "after a further repeat", "retire", "publish "+lifeA, "retire", "publish "+lifeA)
	})
	t.Run("a replacement that fails, then succeeds", func(t *testing.T) {
		b := newSessionBed(t, true)
		b.mustBeAdmitted(lifeA)
		restore := b.failSessionWrites()
		if status, _ := b.admitted(lifeB); status != http.StatusServiceUnavailable {
			t.Fatalf("the replacement: %d; want 503", status)
		}
		restore()
		b.mustBeAdmitted(lifeB)
		if status, code := b.admitted(lifeA); status != http.StatusConflict || code != "app_lifetime_retired" {
			t.Fatalf("the replaced lifetime: %d %s; want retired", status, code)
		}
		wantScopes(t, b, "after the replacement", "retire", "publish "+lifeA, "retire", "retire", "publish "+lifeB)
		if v := b.view(lifeB); v.State != "open" || !v.Session.HeldByYou {
			t.Fatalf("the route says %+v", v)
		}
	})
}

// ---- D: the read route ----

func TestOperatingSession_TheReadRouteChangesNothing_AndAFailureIsAnError(t *testing.T) {
	b := newSessionBed(t, true)
	if v := b.view(""); v.State != "none" || v.Session != nil || v.Generation != 0 {
		t.Fatalf("before any admission the route says %+v", v)
	}
	b.mustBeAdmitted(lifeA)
	before := b.file(lifeA)
	for _, lifetime := range []string{lifeA, lifeB, ""} {
		b.view(lifetime)
	}
	if after := b.file(lifeA); after.Generation != before.Generation || after.Open == nil || after.Open.ID != before.Open.ID || after.Open.Holder != lifeA ||
		b.count(`SELECT COUNT(*) FROM operating_session`) != 1 || b.count(`SELECT COUNT(*) FROM operating_app_exit`) != 0 {
		t.Fatalf("reading changed the file: %+v with %+v", after, after.Open)
	}
	if st := b.readAdmission(t, b.c, lifeB); st.Current {
		t.Fatal("reading made another lifetime current")
	}

	// A file that cannot be read is an error, never an empty session.
	b.exec(`DELETE FROM operating_session_interval`) // the open session now has no open interval: the record is broken
	status, body, _ := b.appDo(t, b.c, lifeA, http.MethodGet, "/v1/operating-session", "")
	if status < 500 {
		t.Fatalf("a broken record answered %d %s; want an error", status, body)
	}
}

// ---- F: the event ----

func TestOperatingSession_OneEventForEachAdvanceOfTheGeneration(t *testing.T) {
	b := newSessionBed(t, true)
	ch, cancel := b.srv.hub.Subscribe()
	defer cancel()
	changes := func() []int64 {
		t.Helper()
		var out []int64
		for {
			select {
			case ev := <-ch:
				if ev.Name != "operating_session.changed" {
					continue
				}
				raw, err := json.Marshal(ev.Payload)
				if err != nil {
					t.Fatal(err)
				}
				var p struct {
					ArchiveID  string `json:"archive_id"`
					Generation int64  `json:"generation"`
				}
				if err := json.Unmarshal(raw, &p); err != nil || p.ArchiveID != testServedArchive {
					t.Fatalf("the event carries %s (%v); want this archive and a generation", raw, err)
				}
				out = append(out, p.Generation)
			default:
				return out
			}
		}
	}

	b.mustBeAdmitted(lifeA)
	if got := changes(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("after the first admission the events are %v, want [1]", got)
	}
	b.mustBeAdmitted(lifeA) // changes nothing
	b.view(lifeA)
	if got := changes(); len(got) != 0 {
		t.Fatalf("a repeat and a read published %v", got)
	}
	b.mustBeAdmitted(lifeB) // the empty session is discarded and a new one opened: one advance
	if got := changes(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("after the replacement the events are %v, want [2]", got)
	}
	restore := b.failSessionWrites()
	if status, _ := b.admitted(lifeC); status == http.StatusOK {
		t.Fatal("the admission succeeded with the file failing")
	}
	restore()
	if got := changes(); len(got) != 0 {
		t.Fatalf("a rolled-back step published %v", got)
	}

	// A step that runs and changes nothing publishes nothing: the same app
	// admitted again after a daemon restart, and an admission that finds
	// another lifetime's session with a contact.
	b.restart()
	b.mustBeAdmitted(lifeB)
	if got := changes(); len(got) != 0 {
		t.Fatalf("an admission that found its own session published %v", got)
	}
	b.contact(b.view(lifeB).Session.ID)
	b.mustBeAdmitted(lifeD)
	if v := b.view(lifeD); v.State != "recovery_pending" {
		t.Fatalf("the fixture is %q, want recovery_pending", v.State)
	}
	if got := changes(); len(got) != 0 {
		t.Fatalf("an admission that left another lifetime's session alone published %v", got)
	}
}

func TestOperatingSession_TheEventNameIsTheDeclaredOne(t *testing.T) {
	if events.NameOperatingSessionChanged != "operating_session.changed" {
		t.Fatalf("the event is named %q", events.NameOperatingSessionChanged)
	}
}

// The route's answer is one snapshot of the file and the admission together
// (codex-195). Between its read of the file and its naming of the state, no
// close and retirement can land: the admission's order is held across both.
// If it were not, the gap below performs exactly that close, and the answer
// would be a combination that never existed: the old session, held by the
// caller, yet "recovery pending".
func TestOperatingSession_TheReadIsOneSnapshotOfTheFileAndTheAdmission(t *testing.T) {
	b := newSessionBed(t, true)
	b.mustBeAdmitted(lifeA)
	before := b.view(lifeA)
	b.contact(before.Session.ID)
	before = b.view(lifeA)
	if before.State != "open" || !before.Session.HasContacts || !before.Session.HeldByYou {
		t.Fatalf("the fixture is %+v with %+v", before, before.Session)
	}

	a := &b.srv.admission
	gaps := 0
	b.srv.sessionReadGap = func() {
		gaps++
		if !a.mu.TryLock() {
			return // held by the read: nothing can come between
		}
		// Not held: do what a confirmed exit does, here, between the two halves.
		defer a.mu.Unlock()
		t.Error("the read does not hold the admission's order between the file and the state's name")
		if _, _, err := b.srv.db.RecordOperatingAppExitWithContext(context.Background(), lifeA, before.Generation, true, sessionMorning); err != nil {
			t.Errorf("close in the gap: %v", err)
		}
		a.retired = map[string]struct{}{lifeA: {}}
		a.current = ""
	}
	got := b.view(lifeA)
	b.srv.sessionReadGap = nil
	if gaps != 1 {
		t.Fatalf("the read passed its gap %d time(s); the test proved nothing", gaps)
	}
	if got.State != before.State || got.Generation != before.Generation || got.Session == nil ||
		got.Session.ID != before.Session.ID || got.Session.HeldByYou != before.Session.HeldByYou {
		t.Fatalf("the read returned %+v with %+v; want the whole state from before the close: %+v with %+v",
			got, got.Session, before, before.Session)
	}
}

// shellAdmits is the desktop app's own admission (desktop/src/admission.mjs,
// admit): it reads, and if the read says it is current it stops there and
// sends no admit. Only otherwise does it admit against what it read.
func (b *sessionBed) shellAdmits(lifetime string) (status int, code string, posted bool) {
	b.t.Helper()
	read := b.readAdmission(b.t, b.c, lifetime)
	if read.Current {
		return http.StatusOK, "", false
	}
	status, code = b.admit(b.t, b.c, lifetime, read)
	return status, code, true
}

// After a replacement failed, the earlier lifetime is still the one the
// admission holds, but its scopes authorize nobody. Its read must not say
// "current" then: the app stops at a read that says so and never sends the
// admit that would repair it, and would believe itself admitted while FT8
// refused its commands (the clean-room review of a8317035).
func TestOperatingSession_AfterAFailedReplacementTheAppsOwnFlowRepairsIt(t *testing.T) {
	b := newSessionBed(t, true)
	b.mustBeAdmitted(lifeA)
	held := b.view(lifeA)
	restore := b.failSessionWrites()
	if status, _ := b.admitted(lifeB); status != http.StatusServiceUnavailable {
		t.Fatalf("the replacement: %d; want 503", status)
	}
	wantScopes(t, b, "after the failure", "retire", "publish "+lifeA, "retire")
	if st := b.readAdmission(t, b.c, lifeA); st.Current || st.Generation != 1 {
		t.Fatalf("the earlier lifetime reads %+v while its scopes are stopped; want not current, generation 1", st)
	}

	// While the file still fails, the app's flow does send its admit, and is told so.
	if status, code, posted := b.shellAdmits(lifeA); !posted || status != http.StatusServiceUnavailable || code != "operating_session_unavailable" {
		t.Fatalf("the app's flow while the file fails: %d %s, admit sent %v; want a 503 from an admit", status, code, posted)
	}

	restore()
	if status, code, posted := b.shellAdmits(lifeA); !posted || status != http.StatusOK {
		t.Fatalf("the app's flow after the fault is removed: %d %s, admit sent %v; want an admit that succeeds", status, code, posted)
	}
	wantScopes(t, b, "after the app's own flow", "retire", "publish "+lifeA, "retire", "publish "+lifeA)
	if st := b.readAdmission(t, b.c, lifeA); !st.Current || st.Generation != 1 {
		t.Fatalf("after the repair the lifetime reads %+v; want current at generation 1", st)
	}
	if v := b.view(lifeA); v.Session == nil || v.Session.ID != held.Session.ID || !v.Session.HeldByYou {
		t.Fatalf("the repair changed the session: %+v with %+v", v, v.Session)
	}
	// Repaired, the flow stops at its read again and nothing more is published.
	if _, _, posted := b.shellAdmits(lifeA); posted {
		t.Fatal("the app's flow sent an admit though the lifetime reads current")
	}
	wantScopes(t, b, "after a further flow", "retire", "publish "+lifeA, "retire", "publish "+lifeA)
	if status, code := b.admitted(lifeB); status != http.StatusOK {
		t.Fatalf("a later replacement: %d %s", status, code)
	}
}
