package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/testbarrier"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0029 part 5, C4: contention between a live contact and the session's
   controls, through the REAL submit (qsoservice.SubmitLive), with the order of
   the two writers forced both ways. The storage tests beside this file
   (operating_session_contention_test.go) force the same orders on the logging
   step alone; these show them for the transaction the daemon really runs.

   Nothing here depends on how long anything takes or on how goroutines are
   scheduled (Codex's review of the first form of this evidence, codex-259):

     - THE CONTACT FIRST. The membership asks who holds from inside the
       contact's transaction, after its QSO row has taken the writer's
       position. The test starts the control there and goes on only once the
       control is SEEN waiting inside SQLite's busy handler
       (testbarrier.ExpectSQLiteBusy). The contact then commits, and the
       control acts on what it committed.
     - THE CONTROL FIRST. The control is held once it has the writer's
       position (the storage tests' seam). The test starts the submit and goes
       on only once the submit is seen waiting, at its QSO insert. The control
       then commits, and the contact acts on what it committed.

   Each case is judged on rows, and on every contact in the file, not on a
   count: a trigger of the test's own records, inside each contact's insert,
   whether its logbook was the file's default then and at which default epoch.
   A contact whose logbook was the default must be in exactly one session, of
   that logbook and that epoch; any other contact must be in none.

   The file is opened with the configuration's own connection settings.
*/

const (
	orderLifeA = "lifetime-A"
	orderWait  = 20 * time.Second
)

var orderMorning = time.Date(2026, 11, 20, 9, 15, 0, 0, time.UTC)

type submitBed struct {
	t            *testing.T
	db           *sqlite.Service
	q            *qsoservice.Service
	raw          *sql.DB
	x, y         int64
	xUUID, yUUID string
	// earlier is a closed session with a contact; current is the open one,
	// held by lifetime A, with a contact.
	earlier, current string

	mu sync.Mutex
	// inContact runs where the membership asks who holds: inside the contact's
	// transaction, with the writer's position held.
	inContact func()
	told      []int64
	n         int

	// workers is every goroutine a case starts (start). The bed waits for them
	// before the database is closed, and a case that installs the storage
	// tests' seam waits for them before the seam is taken out (heldUntilDone),
	// whichever way the case ends.
	workers sync.WaitGroup
}

// start runs fn on a goroutine the bed waits for.
func (b *submitBed) start(fn func()) {
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		fn()
	}()
}

// heldUntilDone registers, for a case that holds the writer's position, what
// must happen when the case ends, however it ends: the holder lets go, and
// every goroutine the case started has finished. It is registered AFTER the
// holder was taken, so it runs BEFORE the holder's own cleanup. That matters
// for the seam (HoldAControlForTest): its cleanup puts back package-level
// functions that a released control still reads on its way out, so a case
// that failed while the control was held would otherwise report a data race
// of the test's own making beside its real failure (Codex's review,
// codex-260).
func (b *submitBed) heldUntilDone(letGo func()) {
	b.t.Helper()
	b.t.Cleanup(func() {
		letGo()
		b.workers.Wait()
	})
}

func newSubmitBed(t *testing.T) *submitBed {
	t.Helper()
	db, path := sqlite.MigratedFileServiceForTest(t)
	raw, err := sql.Open(sqlite.SqliteDriver, "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub()
	b := &submitBed{t: t, db: db, raw: raw,
		q: &qsoservice.Service{DB: db, Logger: db.LoggerService, Config: db.ConfigService, Hub: hub}}
	// Registered after the service's own cleanup, so before the database is
	// closed: no goroutine of a case outlives the file it writes to.
	t.Cleanup(func() { b.workers.Wait(); hub.Close(); _ = raw.Close() })
	ctx := context.Background()
	if b.x, err = db.InsertLogbookWithContext(ctx, types.Logbook{Name: "X", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("logbook X: %v", err)
	}
	if b.y, err = db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Y", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("logbook Y: %v", err)
	}
	if _, err := db.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := db.SetArchiveDefaultLogbookWithContext(ctx, b.x); err != nil {
		t.Fatalf("the default: %v", err)
	}
	b.xUUID, b.yUUID = b.text(`SELECT uuid FROM logbook WHERE id = ?`, b.x), b.text(`SELECT uuid FROM logbook WHERE id = ?`, b.y)
	route := types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"}}
	other := route
	other.Name = "qrz." + b.yUUID
	b.q.SetDestinationRoutes([]forwarding.BoundForwarder{{LogbookID: b.x, Config: route}, {LogbookID: b.y, Config: other}})
	b.q.SetSessionMembership(&qsoservice.SessionMembership{
		Holder: func() string {
			b.mu.Lock()
			hook := b.inContact
			b.inContact = nil // once: for the contact the case is about
			b.mu.Unlock()
			if hook != nil {
				hook()
			}
			return orderLifeA
		},
		Advanced: func(generation int64) {
			b.mu.Lock()
			b.told = append(b.told, generation)
			b.mu.Unlock()
		},
		Now: func() time.Time { return orderMorning },
	})
	// The test's own record of what was true inside each contact's insert, and
	// its check that the step finds the contact's upload row already written.
	b.exec(`CREATE TABLE test_at_insert (qso_id INTEGER PRIMARY KEY, was_default INTEGER NOT NULL, epoch INTEGER NOT NULL)`)
	b.exec(`CREATE TRIGGER test_record_at_insert AFTER INSERT ON qso BEGIN
		INSERT INTO test_at_insert SELECT NEW.id, NEW.logbook_id IS m.default_logbook_id, m.default_epoch
		FROM archive_metadata m WHERE m.singleton = 1;
	END`)
	b.exec(`CREATE TRIGGER test_upload_before_step BEFORE INSERT ON operating_session_member BEGIN
		SELECT RAISE(ABORT, 'the step ran before the contact''s upload row was written')
		WHERE NOT EXISTS (SELECT 1 FROM qso_upload u WHERE u.qso_id = NEW.qso_id);
	END`)

	// An earlier session, closed with a contact; and the open one, with a contact.
	st := b.control("the first session", func(ctx context.Context) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
		return db.OpenOperatingSessionWithContext(ctx, orderLifeA, orderMorning)
	})
	b.earlier = st.Open.ID
	b.live(b.x)
	st = b.control("the second session", func(ctx context.Context) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
		return db.NewOperatingSessionWithContext(ctx, orderLifeA, st.Generation, orderMorning)
	})
	b.current = st.Open.ID
	b.live(b.x)
	if b.current == b.earlier || b.memberCount(b.earlier) != 1 || b.memberCount(b.current) != 1 {
		t.Fatalf("the fixture: sessions %s and %s with %d and %d contacts", b.earlier, b.current, b.memberCount(b.earlier), b.memberCount(b.current))
	}
	b.told = nil
	return b
}

func (b *submitBed) exec(q string, args ...any) {
	b.t.Helper()
	if _, err := b.raw.Exec(q, args...); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
}

func (b *submitBed) count(q string, args ...any) int {
	b.t.Helper()
	var n int
	if err := b.raw.QueryRow(q, args...).Scan(&n); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (b *submitBed) text(q string, args ...any) string {
	b.t.Helper()
	var s sql.NullString
	if err := b.raw.QueryRow(q, args...).Scan(&s); err != nil && err != sql.ErrNoRows {
		b.t.Fatalf("%s: %v", q, err)
	}
	return s.String
}

func (b *submitBed) memberCount(session string) int {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM operating_session_member WHERE session_id = ?`, session)
}

// memberOf is the session a contact is in, "" for none.
func (b *submitBed) memberOf(qso int64) string {
	b.t.Helper()
	return b.text(`SELECT session_id FROM operating_session_member WHERE qso_id = ?`, qso)
}

// closedAs is a session's close reason, "" while it is open.
func (b *submitBed) closedAs(session string) string {
	b.t.Helper()
	return b.text(`SELECT close_reason FROM operating_session WHERE id = ?`, session)
}

// open is the open session's id, "" when none is open.
func (b *submitBed) open() string {
	b.t.Helper()
	return b.text(`SELECT id FROM operating_session WHERE closed_at IS NULL`)
}

func (b *submitBed) state() sqlite.OperatingSessionState {
	b.t.Helper()
	st, err := b.db.OperatingSessionStateWithContext(context.Background(), orderLifeA)
	if err != nil {
		b.t.Fatalf("read the session state: %v", err)
	}
	return st
}

type controlAnswer struct {
	st      sqlite.OperatingSessionState
	outcome sqlite.SessionOutcome
	err     error
}

type sessionControl func(ctx context.Context) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error)

// control runs a control now and requires it applied.
func (b *submitBed) control(what string, call sessionControl) sqlite.OperatingSessionState {
	b.t.Helper()
	st, outcome, err := call(context.Background())
	if err != nil || outcome != sqlite.SessionApplied {
		b.t.Fatalf("%s: %s, %v; want it applied", what, outcome, err)
	}
	return st
}

func (b *submitBed) nextRec() adif.Record {
	b.mu.Lock()
	b.n++
	n := b.n
	b.mu.Unlock()
	return adif.Record{
		ContactedStation: types.ContactedStation{Call: fmt.Sprintf("M%dABC", n)},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.250", QsoDate: "20261120", TimeOn: fmt.Sprintf("09%02d", n)},
	}
}

// live logs one contact through the real submit and requires it stored.
func (b *submitBed) live(logbook int64) int64 {
	b.t.Helper()
	res, err := b.q.SubmitLive(context.Background(), logbook, b.nextRec(), false)
	if err != nil || res.Status != "stored" {
		b.t.Fatalf("a live submit: %+v, %v", res, err)
	}
	return res.ID
}

func orderAwait[T any](t *testing.T, what string, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(orderWait):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

// everyContactIsWhereItsInsertPutIt is the check on every contact in the file,
// against what was true inside its own insert.
func (b *submitBed) everyContactIsWhereItsInsertPutIt() {
	b.t.Helper()
	if n := b.count(`SELECT COUNT(*) FROM qso q WHERE NOT EXISTS (SELECT 1 FROM test_at_insert a WHERE a.qso_id = q.id)`); n != 0 {
		b.t.Fatalf("%d contacts have no record of their insert: the test's trigger did not run", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM test_at_insert a WHERE a.was_default = 1 AND
		(SELECT COUNT(*) FROM operating_session_member m WHERE m.qso_id = a.qso_id) <> 1`); n != 0 {
		b.t.Fatalf("%d contacts whose logbook was the default at their insert are not in exactly one session", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM test_at_insert a WHERE a.was_default = 0 AND
		EXISTS (SELECT 1 FROM operating_session_member m WHERE m.qso_id = a.qso_id)`); n != 0 {
		b.t.Fatalf("%d contacts whose logbook was not the default at their insert are in a session", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM test_at_insert a
		JOIN operating_session_member m ON m.qso_id = a.qso_id
		JOIN operating_session s ON s.id = m.session_id
		JOIN qso q ON q.id = a.qso_id JOIN logbook l ON l.id = q.logbook_id
		WHERE s.logbook_uuid <> l.uuid OR s.default_epoch <> a.epoch`); n != 0 {
		b.t.Fatalf("%d contacts are in a session of another logbook, or of another default epoch than their insert's", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`); n > 1 {
		b.t.Fatalf("%d sessions are open", n)
	}
}

// orderCase is one control, and what the file must hold in each order.
type orderCase struct {
	name string
	// waitsIn is the function the control is seen waiting in when the contact
	// has the writer's position first.
	waitsIn string
	// run is the control, named at the generation the test read before either
	// writer began.
	run func(b *submitBed, generation int64) sessionControl
	// contactFirst and controlFirst judge the file once both have committed:
	// qso is the contact, logged into the default logbook X.
	contactFirst, controlFirst func(t *testing.T, b *submitBed, qso int64)
}

func orderCases() []orderCase {
	return []orderCase{
		{
			name: "New", waitsIn: "(*Service).NewOperatingSessionWithContext",
			run: func(b *submitBed, generation int64) sessionControl {
				return func(ctx context.Context) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
					return b.db.NewOperatingSessionWithContext(ctx, orderLifeA, generation, orderMorning)
				}
			},
			contactFirst: func(t *testing.T, b *submitBed, qso int64) {
				if b.memberOf(qso) != b.current || b.closedAs(b.current) != sqlite.SessionClosedByNew || b.open() == "" || b.memberCount(b.open()) != 0 {
					t.Fatalf("the contact is in %q; the session it met closed as %q; the open session %q has %d contacts; want the contact in the session New closed, and a new empty one open",
						b.memberOf(qso), b.closedAs(b.current), b.open(), b.memberCount(b.open()))
				}
			},
			controlFirst: func(t *testing.T, b *submitBed, qso int64) {
				if now := b.open(); now == "" || now == b.current || b.memberOf(qso) != now || b.closedAs(b.current) != sqlite.SessionClosedByNew || b.memberCount(b.current) != 1 {
					t.Fatalf("the contact is in %q; the open session is %q; the earlier one closed as %q with %d contacts; want the contact in the session New opened",
						b.memberOf(qso), now, b.closedAs(b.current), b.memberCount(b.current))
				}
			},
		},
		{
			name: "Continue", waitsIn: "(*Service).ContinueOperatingSessionWithContext",
			run: func(b *submitBed, generation int64) sessionControl {
				return func(ctx context.Context) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
					return b.db.ContinueOperatingSessionWithContext(ctx, orderLifeA, generation, b.earlier, orderMorning)
				}
			},
			contactFirst: func(t *testing.T, b *submitBed, qso int64) {
				if b.memberOf(qso) != b.current || b.closedAs(b.current) != sqlite.SessionClosedByContinue || b.open() != b.earlier || b.memberCount(b.earlier) != 1 {
					t.Fatalf("the contact is in %q; the session it met closed as %q; the open session is %q; want the contact in the session Continue closed, and the earlier one open",
						b.memberOf(qso), b.closedAs(b.current), b.open())
				}
			},
			controlFirst: func(t *testing.T, b *submitBed, qso int64) {
				if b.memberOf(qso) != b.earlier || b.open() != b.earlier || b.closedAs(b.current) != sqlite.SessionClosedByContinue || b.memberCount(b.current) != 1 {
					t.Fatalf("the contact is in %q; the open session is %q; the other closed as %q; want the contact in the session Continue reopened",
						b.memberOf(qso), b.open(), b.closedAs(b.current))
				}
			},
		},
		{
			name: "an exit", waitsIn: "(*Service).RecordOperatingAppExitWithContext",
			run: func(b *submitBed, generation int64) sessionControl {
				return func(ctx context.Context) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
					return b.db.RecordOperatingAppExitWithContext(ctx, orderLifeA, generation, true, orderMorning)
				}
			},
			contactFirst: func(t *testing.T, b *submitBed, qso int64) {
				if b.memberOf(qso) != b.current || b.closedAs(b.current) != sqlite.SessionClosedByExit || b.open() != "" {
					t.Fatalf("the contact is in %q; the session it met closed as %q; the open session is %q; want the contact in the session the exit closed, and none open",
						b.memberOf(qso), b.closedAs(b.current), b.open())
				}
			},
			controlFirst: func(t *testing.T, b *submitBed, qso int64) {
				// The lifetime the membership names has exited: the contact opens
				// a session held by nobody, and that advance is told once.
				now := b.open()
				holder := b.text(`SELECT COALESCE(app_lifetime, '') FROM operating_session_interval WHERE session_id = ? AND end_state = 'open'`, now)
				if now == "" || now == b.current || b.memberOf(qso) != now || holder != "" || b.closedAs(b.current) != sqlite.SessionClosedByExit {
					t.Fatalf("the contact is in %q; the open session is %q held by %q; the earlier one closed as %q; want the contact in a new session held by nobody",
						b.memberOf(qso), now, holder, b.closedAs(b.current))
				}
				if g := b.state().Generation; len(b.told) != 1 || b.told[0] != g {
					t.Fatalf("the session the contact opened was told as %v with the file at generation %d; want it told once", b.told, g)
				}
			},
		},
	}
}

// The contact has the writer's position first. The control is seen waiting,
// and acts on what the contact committed.
func TestSubmitOrder_TheContactFirst(t *testing.T) {
	for _, c := range orderCases() {
		t.Run(c.name, func(t *testing.T) {
			b := newSubmitBed(t)
			run := c.run(b, b.state().Generation)
			answered := make(chan controlAnswer, 1)
			b.inContact = func() {
				w := testbarrier.ExpectSQLiteBusy(t, c.waitsIn)
				b.start(func() {
					st, outcome, err := run(context.Background())
					answered <- controlAnswer{st, outcome, err}
				})
				testbarrier.Waiting(w, c.name+", with a contact's transaction holding the writer's position", answered)
			}

			qso := b.live(b.x)

			if a := orderAwait(t, c.name, answered); a.err != nil || a.outcome != sqlite.SessionApplied {
				t.Fatalf("%s, after the contact: %s, %v; want it applied", c.name, a.outcome, a.err)
			}
			if len(b.told) != 0 {
				t.Fatalf("a contact that joined the open session told of %v", b.told)
			}
			c.contactFirst(t, b, qso)
			b.everyContactIsWhereItsInsertPutIt()
		})
	}
	t.Run("a change of the default pointer", func(t *testing.T) {
		b := newSubmitBed(t)
		moved := make(chan error, 1)
		b.inContact = func() {
			w := testbarrier.ExpectSQLiteBusy(t, "(*Service).SetArchiveDefaultLogbookWithContext")
			b.start(func() { moved <- b.db.SetArchiveDefaultLogbookWithContext(context.Background(), b.y) })
			testbarrier.Waiting(w, "the pointer change, with a contact's transaction holding the writer's position", moved)
		}

		qso := b.live(b.x)

		if err := orderAwait(t, "the pointer change", moved); err != nil {
			t.Fatalf("the pointer change, after the contact: %v", err)
		}
		// X was the default at the contact's insert: it is in the session it
		// met, which the next contact's epoch test will close.
		if b.memberOf(qso) != b.current || b.open() != b.current || b.state().DefaultLogbookUUID != b.yUUID {
			t.Fatalf("the contact is in %q; the open session is %q; the default is %s; want the contact in the session it met, and the default moved to Y",
				b.memberOf(qso), b.open(), b.state().DefaultLogbookUUID)
		}
		next := b.live(b.y)
		if b.closedAs(b.current) != sqlite.SessionClosedByDefault || b.memberOf(next) == "" || b.memberOf(next) == b.current {
			t.Fatalf("after the next contact the earlier session closed as %q and that contact is in %q; want it closed for the changed logbook and the contact in a new session",
				b.closedAs(b.current), b.memberOf(next))
		}
		b.everyContactIsWhereItsInsertPutIt()
	})
}

// The control has the writer's position first. The contact is seen waiting at
// its QSO insert, and acts on what the control committed.
func TestSubmitOrder_TheControlFirst(t *testing.T) {
	type logged struct {
		res qsoservice.SubmitResult
		err error
	}
	// waitingContact starts a live submit into a logbook and returns once it
	// is seen waiting for the writer's position at its QSO insert.
	waitingContact := func(t *testing.T, b *submitBed, logbook int64) <-chan logged {
		t.Helper()
		w := testbarrier.ExpectSQLiteBusy(t, "(*Service).InsertQsoTx")
		ch := make(chan logged, 1)
		rec := b.nextRec()
		b.start(func() {
			res, err := b.q.SubmitLive(context.Background(), logbook, rec, false)
			ch <- logged{res, err}
		})
		testbarrier.Waiting(w, "the contact, with the writer's position held", ch)
		return ch
	}
	stored := func(t *testing.T, ch <-chan logged) int64 {
		t.Helper()
		l := orderAwait(t, "the contact", ch)
		if l.err != nil || l.res.Status != "stored" {
			t.Fatalf("the contact that waited: %+v, %v; want it stored", l.res, l.err)
		}
		return l.res.ID
	}

	for _, c := range orderCases() {
		t.Run(c.name, func(t *testing.T) {
			b := newSubmitBed(t)
			run := c.run(b, b.state().Generation)
			ctx, locked, release := sqlite.HoldAControlForTest(t)
			b.heldUntilDone(release)
			answered := make(chan controlAnswer, 1)
			b.start(func() {
				st, outcome, err := run(ctx)
				answered <- controlAnswer{st, outcome, err}
			})
			orderAwait(t, c.name+" to have the writer's position", locked)

			contact := waitingContact(t, b, b.x)
			release()

			if a := orderAwait(t, c.name, answered); a.err != nil || a.outcome != sqlite.SessionApplied {
				t.Fatalf("%s, holding the writer's position: %s, %v; want it applied", c.name, a.outcome, a.err)
			}
			c.controlFirst(t, b, stored(t, contact))
			b.everyContactIsWhereItsInsertPutIt()
		})
	}

	// The pointer moves from X to Y while a contact waits. One for X, which is
	// no longer the default at its insert; one for Y, which now is.
	t.Run("a change of the default pointer, and a contact for the logbook it left", func(t *testing.T) {
		b := newSubmitBed(t)
		before := b.state().Generation
		move := sqlite.HeldDefaultPointerChangeForTest(t, b.db, b.y)
		b.heldUntilDone(func() { _ = move.Rollback() }) // nothing to undo once the case has committed it
		contact := waitingContact(t, b, b.x)
		if err := move.Commit(); err != nil {
			t.Fatalf("commit the pointer change: %v", err)
		}
		qso := stored(t, contact)
		if b.memberOf(qso) != "" || b.open() != b.current || b.state().Generation != before || len(b.told) != 0 {
			t.Fatalf("the contact is in %q; the open session is %q; the generation went from %d to %d; want the contact in no session and no session touched",
				b.memberOf(qso), b.open(), before, b.state().Generation)
		}
		b.everyContactIsWhereItsInsertPutIt()
	})
	t.Run("a change of the default pointer, and a contact for the logbook it moved to", func(t *testing.T) {
		b := newSubmitBed(t)
		before := b.state().Generation
		move := sqlite.HeldDefaultPointerChangeForTest(t, b.db, b.y)
		b.heldUntilDone(func() { _ = move.Rollback() })
		contact := waitingContact(t, b, b.y)
		if err := move.Commit(); err != nil {
			t.Fatalf("commit the pointer change: %v", err)
		}
		qso := stored(t, contact)
		now := b.open()
		if now == "" || now == b.current || b.memberOf(qso) != now || b.closedAs(b.current) != sqlite.SessionClosedByDefault {
			t.Fatalf("the contact is in %q; the open session is %q; the earlier one closed as %q; want it closed for the changed logbook and the contact in a new session",
				b.memberOf(qso), now, b.closedAs(b.current))
		}
		if g := b.state().Generation; g != before+1 || len(b.told) != 1 || b.told[0] != g {
			t.Fatalf("a close and an open in one contact: generation %d to %d, told %v; want one advance, told once", before, g, b.told)
		}
		b.everyContactIsWhereItsInsertPutIt()
	})
}
