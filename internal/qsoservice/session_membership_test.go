package qsoservice

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

/*
   W-0029 part 5, slice 1: a contact logged while operating joins the open
   operating session, in the contact's own transaction (ADR 0100 §2). The
   criteria are in the W-0029 dossier, "Part 5: acceptance criteria"; the
   letters here are theirs.

   Every case is judged on rows read back from the file through a second
   connection, after the commit: the QSO, its upload-queue row, the member row
   and the session it names. The service runs over a database FILE opened with
   the configuration's own connection settings, as the daemon opens it.
*/

const (
	memLifeA = "lifetime-A"
	memLifeB = "lifetime-B"
)

var memberMorning = time.Date(2026, 11, 20, 9, 15, 0, 0, time.UTC)

// memberBed is a service with the membership connected, over a file with an
// identity row and two logbooks: X, the file's default, and Y. Each has an
// enabled insert route, so every live contact has an upload-queue row in its
// transaction.
type memberBed struct {
	t    *testing.T
	s    *Service
	raw  *sql.DB
	path string
	x, y int64

	mu          sync.Mutex
	holder      string
	holderReads int
	advanced    []int64
	// membersAtTelling is how many member rows another connection could see
	// each time the membership was told of a change: 0 if it was told before
	// the contact's commit.
	membersAtTelling []int
	// onHolder runs inside the holder's read: between the QSO's insert and the
	// logging step.
	onHolder func()
	minute   int
}

func newMemberBed(t *testing.T) *memberBed {
	t.Helper()
	b := newUnconnectedMemberBed(t)
	b.s.SetSessionMembership(&SessionMembership{
		Holder: func() string {
			b.mu.Lock()
			h, hook := b.holder, b.onHolder
			b.holderReads++
			b.mu.Unlock()
			if hook != nil {
				hook()
			}
			return h
		},
		Advanced: func(generation int64) {
			var visible int
			if err := b.raw.QueryRow(`SELECT COUNT(*) FROM operating_session_member`).Scan(&visible); err != nil {
				visible = -1
			}
			b.mu.Lock()
			b.advanced = append(b.advanced, generation)
			b.membersAtTelling = append(b.membersAtTelling, visible)
			b.mu.Unlock()
		},
		Now: func() time.Time { return memberMorning },
	})
	return b
}

// newUnconnectedMemberBed is the bed as the daemon's ordinary start leaves the
// service until part 6: no membership connected.
func newUnconnectedMemberBed(t *testing.T) *memberBed {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "db", "log.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(dir)
	cfg.Datastore.Path = path
	cfg.Logging.FileLogging = false
	cfg.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}}}
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config: %v", err)
	}
	logSvc := &logging.Service{ConfigService: cfgSvc, WorkingDir: cfgSvc.WorkingDir()}
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging: %v", err)
	}
	db := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := db.Initialize(); err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	raw, err := sql.Open(sqlite.SqliteDriver, "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub()
	t.Cleanup(func() {
		hub.Close()
		_ = raw.Close()
		_ = db.Close()
		_ = logSvc.Close()
	})
	b := &memberBed{t: t, raw: raw, path: path, holder: memLifeA,
		s: &Service{DB: db, Logger: logSvc, Config: cfgSvc, Hub: hub}}
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
	b.setDefault(b.x)
	bindFromConfig(t, b.s)
	// The test's own record of what was true inside each contact's insert
	// (whether its logbook was the file's default then), for
	// everyContactIsWhereItsInsertPutIt; and its check of C1's order on every
	// join: the step must find the contact's upload row already written.
	b.exec(`CREATE TABLE test_at_insert (qso_id INTEGER PRIMARY KEY, was_default INTEGER NOT NULL)`)
	b.exec(`CREATE TRIGGER test_record_at_insert AFTER INSERT ON qso BEGIN
		INSERT INTO test_at_insert SELECT NEW.id, NEW.logbook_id IS m.default_logbook_id
		FROM archive_metadata m WHERE m.singleton = 1;
	END`)
	b.exec(`CREATE TRIGGER test_upload_before_step BEFORE INSERT ON operating_session_member BEGIN
		SELECT RAISE(ABORT, 'the step ran before the upload row')
		WHERE NOT EXISTS (SELECT 1 FROM qso_upload u WHERE u.qso_id = NEW.qso_id);
	END`)
	return b
}

// failTheStep makes the logging step's member row fail, as a file that cannot
// be written would, until the returned func is called. The failure is the
// second thing the trigger does: first it requires the contact's upload row,
// with an error of its own, so "injected failure" is reached only by a
// transaction that had already written that row (C1's order, and F3's "an
// upload-queue row really in the transaction").
func (b *memberBed) failTheStep() (restore func()) {
	b.t.Helper()
	b.exec(`CREATE TRIGGER injected_failure BEFORE INSERT ON operating_session_member BEGIN
		SELECT RAISE(ABORT, 'the step ran before the upload row')
		WHERE NOT EXISTS (SELECT 1 FROM qso_upload u WHERE u.qso_id = NEW.qso_id);
		SELECT RAISE(ABORT, 'injected failure');
	END`)
	return func() { b.t.Helper(); b.exec(`DROP TRIGGER injected_failure`) }
}

// everyContactIsWhereItsInsertPutIt checks every contact in the file against
// what was true inside its own insert: one whose logbook was the file's
// default then is in exactly one session, of that logbook; any other is in
// none. It is for a test whose contacts all come through a live submit.
func (b *memberBed) everyContactIsWhereItsInsertPutIt() {
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
	if n := b.count(`SELECT COUNT(*) FROM operating_session_member m JOIN qso q ON q.id = m.qso_id
		JOIN logbook l ON l.id = q.logbook_id JOIN operating_session s ON s.id = m.session_id
		WHERE s.logbook_uuid <> l.uuid`); n != 0 {
		b.t.Fatalf("%d contacts are members of a session of another logbook", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`); n > 1 {
		b.t.Fatalf("%d sessions are open", n)
	}
}

func (b *memberBed) setDefault(logbook int64) {
	b.t.Helper()
	if err := b.s.DB.SetArchiveDefaultLogbookWithContext(context.Background(), logbook); err != nil {
		b.t.Fatalf("set the default to %d: %v", logbook, err)
	}
}

func (b *memberBed) setHolder(lifetime string) {
	b.mu.Lock()
	b.holder = lifetime
	b.mu.Unlock()
}

// told is the generations the membership was told of since the last call.
func (b *memberBed) told() []int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.advanced
	b.advanced = nil
	return out
}

func (b *memberBed) count(q string, args ...any) int {
	b.t.Helper()
	var n int
	if err := b.raw.QueryRow(q, args...).Scan(&n); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (b *memberBed) text(q string, args ...any) string {
	b.t.Helper()
	var s sql.NullString
	if err := b.raw.QueryRow(q, args...).Scan(&s); err != nil && err != sql.ErrNoRows {
		b.t.Fatalf("%s: %v", q, err)
	}
	return s.String
}

func (b *memberBed) exec(q string, args ...any) {
	b.t.Helper()
	if _, err := b.raw.Exec(q, args...); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
}

func (b *memberBed) generation() int {
	b.t.Helper()
	return b.count(`SELECT session_generation FROM archive_metadata WHERE singleton = 1`)
}

// memberOf is the session a QSO is a member of, "" for none.
func (b *memberBed) memberOf(qso int64) string {
	b.t.Helper()
	return b.text(`SELECT session_id FROM operating_session_member WHERE qso_id = ?`, qso)
}

func (b *memberBed) uploads(qso int64) int {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM qso_upload WHERE qso_id = ?`, qso)
}

// record is every session, interval, member and recorded exit, with the
// generation, as text. Two equal records mean no session state changed.
func (b *memberBed) record() string {
	b.t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT 'generation ' || session_generation FROM archive_metadata`,
		`SELECT 'session ' || id || ' ' || logbook_uuid || ' ' || default_epoch || ' ' || COALESCE(close_reason, 'open') FROM operating_session ORDER BY id`,
		`SELECT 'interval ' || session_id || ' ' || seq || ' ' || end_state || ' ' || COALESCE(app_lifetime, '-') FROM operating_session_interval ORDER BY session_id, seq`,
		`SELECT 'member ' || qso_id || ' ' || session_id FROM operating_session_member ORDER BY qso_id`,
		`SELECT 'exit ' || app_lifetime FROM operating_app_exit ORDER BY app_lifetime`,
	} {
		rows, err := b.raw.Query(q)
		if err != nil {
			b.t.Fatalf("%s: %v", q, err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				b.t.Fatal(err)
			}
			out = append(out, line)
		}
		if err := rows.Close(); err != nil {
			b.t.Fatal(err)
		}
	}
	return strings.Join(out, "\n")
}

func (b *memberBed) state(lifetime string) sqlite.OperatingSessionState {
	b.t.Helper()
	st, err := b.s.DB.OperatingSessionStateWithContext(context.Background(), lifetime)
	if err != nil {
		b.t.Fatalf("read the session state: %v", err)
	}
	return st
}

// open runs the start step for a lifetime, as an app's admission does.
func (b *memberBed) open(lifetime string) sqlite.OperatingSessionState {
	b.t.Helper()
	st, _, err := b.s.DB.OpenOperatingSessionWithContext(context.Background(), lifetime, memberMorning)
	if err != nil {
		b.t.Fatalf("open a session for %s: %v", lifetime, err)
	}
	return st
}

// nextRec is a contact no other call of the bed has made.
func (b *memberBed) nextRec() adif.Record {
	b.mu.Lock()
	b.minute++
	n := b.minute
	b.mu.Unlock()
	return adif.Record{
		ContactedStation: types.ContactedStation{Call: fmt.Sprintf("M%dABC", n)},
		QsoDetails: types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.250", QsoDate: "20261120",
			TimeOn: fmt.Sprintf("%02d%02d", 9+n/60, n%60)},
	}
}

// live logs one contact as a live submit and returns its id.
func (b *memberBed) live(logbook int64) int64 {
	b.t.Helper()
	res, err := b.s.SubmitLive(context.Background(), logbook, b.nextRec(), false)
	if err != nil || res.Status != "stored" {
		b.t.Fatalf("a live submit: %+v, %v", res, err)
	}
	return res.ID
}

// stored drains the hub of qso.stored events and returns how many there were.
func drainStored(ch <-chan events.Event) int {
	n := 0
	for {
		select {
		case ev := <-ch:
			if ev.Name == events.NameQsoStored {
				n++
			}
		default:
			return n
		}
	}
}

// ---- A: who joins ----

// A1, A3, C1, F2, F4: with a session open, a live contact is stored with its
// upload row and one member row for that session; the generation does not
// move and nobody is told of a change; qso.stored is published as before.
func TestMembership_ALiveContactJoinsTheOpenSession(t *testing.T) {
	b := newMemberBed(t)
	st := b.open(memLifeA)
	ch, cancel := b.s.Hub.Subscribe()
	defer cancel()

	qso := b.live(b.x)

	if got := b.memberOf(qso); got != st.Open.ID || b.uploads(qso) != 1 {
		t.Fatalf("the contact is a member of %q with %d upload rows; want session %s and one upload row", got, b.uploads(qso), st.Open.ID)
	}
	if lb := b.text(`SELECT l.uuid FROM qso q JOIN logbook l ON l.id = q.logbook_id WHERE q.id = ?`, qso); lb != st.Open.LogbookUUID {
		t.Fatalf("the contact's logbook is %s and its session's is %s", lb, st.Open.LogbookUUID)
	}
	if g, told := b.generation(), b.told(); int64(g) != st.Generation || len(told) != 0 {
		t.Fatalf("a plain join moved the generation from %d to %d and told of %v; want neither", st.Generation, g, told)
	}
	if n := drainStored(ch); n != 1 {
		t.Fatalf("%d qso.stored events for one contact", n)
	}
	now := b.state(memLifeA)
	if now.Open == nil || !now.Open.HasMembers || now.Open.ContactCount != 1 || now.Generation != st.Generation {
		t.Fatalf("the state after the contact is %+v at generation %d; want one contact, generation %d", now.Open, now.Generation, st.Generation)
	}
}

// A4: a forced duplicate that is stored joins like any other.
func TestMembership_AForcedDuplicateJoins(t *testing.T) {
	b := newMemberBed(t)
	st := b.open(memLifeA)
	rec := b.nextRec()
	ctx := context.Background()
	first, err := b.s.SubmitLive(ctx, b.x, rec, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.s.SubmitLive(ctx, b.x, rec, true)
	if err != nil || second.Status != "stored" || second.ID == first.ID {
		t.Fatalf("the forced duplicate: %+v, %v", second, err)
	}
	if b.memberOf(first.ID) != st.Open.ID || b.memberOf(second.ID) != st.Open.ID {
		t.Fatalf("members: %q and %q; want both in %s", b.memberOf(first.ID), b.memberOf(second.ID), st.Open.ID)
	}
}

// A5: a submit that stores nothing changes no session state and opens no
// session. Each is tried with no session open, where a contact would open one.
func TestMembership_ASubmitThatStoresNothingTouchesNoSession(t *testing.T) {
	ctx := context.Background()
	t.Run("a refused duplicate", func(t *testing.T) {
		b := newMemberBed(t)
		rec := b.nextRec()
		if _, err := b.s.Submit(ctx, b.x, rec, false); err != nil { // the original, stored as a deliberate submit: no session
			t.Fatal(err)
		}
		before := b.record()
		res, err := b.s.SubmitLive(ctx, b.x, rec, false)
		if err != nil || res.Status != "duplicate" {
			t.Fatalf("the duplicate: %+v, %v", res, err)
		}
		if after := b.record(); after != before || len(b.told()) != 0 {
			t.Fatalf("a refused duplicate changed the session record:\n%s\nwas\n%s", after, before)
		}
	})
	t.Run("a validation failure", func(t *testing.T) {
		b := newMemberBed(t)
		before := b.record()
		rec := b.nextRec()
		rec.Freq = "1.0" // in no band
		if _, err := b.s.SubmitLive(ctx, b.x, rec, false); IsSubmitError(err) == nil {
			t.Fatalf("the submit answered %v; want a validation refusal", err)
		}
		if after := b.record(); after != before {
			t.Fatalf("a refused submit changed the session record:\n%s\nwas\n%s", after, before)
		}
	})
	t.Run("a logbook that does not exist", func(t *testing.T) {
		b := newMemberBed(t)
		before := b.record()
		if _, err := b.s.SubmitLive(ctx, 987654, b.nextRec(), false); IsSubmitError(err) == nil {
			t.Fatalf("the submit answered %v; want logbook_not_found", err)
		}
		if after := b.record(); after != before {
			t.Fatalf("a refused submit changed the session record:\n%s\nwas\n%s", after, before)
		}
	})
	t.Run("a logbook that has been deleted", func(t *testing.T) {
		b := newMemberBed(t)
		b.exec(`UPDATE logbook SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?`, b.x)
		before := b.record()
		if _, err := b.s.SubmitLive(ctx, b.x, b.nextRec(), false); IsSubmitError(err) == nil {
			t.Fatalf("the submit answered %v; want logbook_not_found", err)
		}
		if after := b.record(); after != before {
			t.Fatalf("a refused submit changed the session record:\n%s\nwas\n%s", after, before)
		}
	})
}

// ---- B: who never joins ----

// B1 to B4, each with a session open and into the default logbook where the
// path allows it: the contact is stored, no member row is written, and the
// session's record is as it was.
func TestMembership_WhatNeverJoins(t *testing.T) {
	ctx := context.Background()
	imported := func(b *memberBed) adif.Record {
		rec := b.nextRec()
		rec.StationCallsign = "G4ABC"
		return rec
	}
	cases := []struct {
		name string
		// store stores contacts and returns how many it stored.
		store func(t *testing.T, b *memberBed) int
	}{
		{"B4: a submit to the default logbook that is not live", func(t *testing.T, b *memberBed) int {
			res, err := b.s.Submit(ctx, b.x, b.nextRec(), false)
			if err != nil || res.Status != "stored" {
				t.Fatalf("%+v, %v", res, err)
			}
			return 1
		}},
		{"B3: a live submit that names another logbook", func(t *testing.T, b *memberBed) int {
			b.live(b.y)
			return 1
		}},
		{"B1: SubmitImport", func(t *testing.T, b *memberBed) int {
			res, err := b.s.SubmitImport(ctx, b.x, imported(b), false, []string{"qrz"})
			if err != nil || res.Status != "stored" {
				t.Fatalf("%+v, %v", res, err)
			}
			return 1
		}},
		{"B1: SubmitImportBatch", func(t *testing.T, b *memberBed) int {
			res, err := b.s.SubmitImportBatch(ctx, b.x, []adif.Record{imported(b), imported(b)}, nil, 10, nil)
			if err != nil || res.Stored != 2 {
				t.Fatalf("%+v, %v", res, err)
			}
			return 2
		}},
		{"B1: SubmitImportBatch, record by record after a failed batch", func(t *testing.T, b *memberBed) int {
			// A UUID already in the file makes the batch's insert fail, and the
			// run drops to one submit per record.
			const uuid = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
			held := imported(b)
			held.AppSmQsoID = uuid
			if _, err := b.s.SubmitImport(ctx, b.x, held, false, nil); err != nil {
				t.Fatal(err)
			}
			clash := imported(b)
			clash.AppSmQsoID = uuid
			res, err := b.s.SubmitImportBatch(ctx, b.x, []adif.Record{imported(b), clash}, nil, 2, nil)
			if err != nil || res.Stored != 1 || len(res.Errors) != 1 {
				t.Fatalf("%+v, %v; want one stored by the fallback and one refused", res, err)
			}
			return 2
		}},
		{"B2: Restore", func(t *testing.T, b *memberBed) int {
			if _, err := b.s.Restore(ctx, b.x, restorableQso(utils.NewUUIDv7(), memberMorning)); err != nil {
				t.Fatal(err)
			}
			return 1
		}},
		{"B2: RestoreInto", func(t *testing.T, b *memberBed) int {
			if _, err := b.s.RestoreInto(ctx, b.s.DB, b.x, restorableQso(utils.NewUUIDv7(), memberMorning)); err != nil {
				t.Fatal(err)
			}
			return 1
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newMemberBed(t)
			b.open(memLifeA)
			member := b.live(b.x) // the session has a contact, so nothing here is about an empty one
			before, qsos := b.record(), b.count(`SELECT COUNT(*) FROM qso`)

			stored := c.store(t, b)

			if got := b.count(`SELECT COUNT(*) FROM qso`) - qsos; got != stored {
				t.Fatalf("%d contacts were stored, want %d: the case proves nothing unless they are", got, stored)
			}
			if after := b.record(); after != before {
				t.Fatalf("the session record changed:\n%s\nwas\n%s", after, before)
			}
			if n := b.count(`SELECT COUNT(*) FROM operating_session_member WHERE qso_id <> ?`, member); n != 0 || len(b.told()) != 0 {
				t.Fatalf("%d member rows were written", n)
			}
		})
	}
}

// Ruling 1 (2026-11-24): until part 6 connects it, the service has no
// membership, and a live submit is stored exactly as before: no member row and
// no session opened.
func TestMembership_Unconnected_ALiveSubmitJoinsNothing(t *testing.T) {
	b := newUnconnectedMemberBed(t)
	before := b.record()
	qso := b.live(b.x)
	if b.uploads(qso) != 1 {
		t.Fatalf("the contact has %d upload rows, want 1", b.uploads(qso))
	}
	if after := b.record(); after != before {
		t.Fatalf("an unconnected service changed the session record:\n%s\nwas\n%s", after, before)
	}
}

// ---- C: order, and what cannot stop a contact ----

// C2: no session state refuses or delays a live contact.
func TestMembership_NoSessionStateRefusesAContact(t *testing.T) {
	t.Run("a remnant awaiting its answer: the contact joins the remnant", func(t *testing.T) {
		b := newMemberBed(t)
		st := b.open(memLifeA)
		b.live(b.x)
		b.setHolder(memLifeB) // another app has started and has not answered
		if _, outcome, err := b.s.DB.OpenOperatingSessionWithContext(context.Background(), memLifeB, memberMorning); err != nil || outcome != sqlite.SessionRecoveryPending {
			t.Fatalf("the fixture: %s, %v; want recovery pending", outcome, err)
		}
		before := b.generation()
		qso := b.live(b.x)
		now := b.state(memLifeB)
		if b.memberOf(qso) != st.Open.ID || now.Open == nil || now.Open.ID != st.Open.ID || now.Open.Holder != memLifeA || b.generation() != before {
			t.Fatalf("the contact joined %q; the open session is %+v at generation %d; want the remnant %s, still held by %s, generation %d",
				b.memberOf(qso), now.Open, b.generation(), st.Open.ID, memLifeA, before)
		}
	})
	t.Run("after a change of the default: the old session closes and the contact joins a new one", func(t *testing.T) {
		b := newMemberBed(t)
		st := b.open(memLifeA)
		b.live(b.x)
		b.setDefault(b.y)
		before := b.generation()
		b.told()
		qso := b.live(b.y)
		joined := b.memberOf(qso)
		if joined == "" || joined == st.Open.ID {
			t.Fatalf("the contact joined %q; want a new session", joined)
		}
		if reason := b.text(`SELECT close_reason FROM operating_session WHERE id = ?`, st.Open.ID); reason != sqlite.SessionClosedByDefault {
			t.Fatalf("the old session's close reason is %q, want %s", reason, sqlite.SessionClosedByDefault)
		}
		if told := b.told(); len(told) != 1 || told[0] != int64(before+1) || b.generation() != before+1 {
			t.Fatalf("a close and an open in one contact told of %v, generation %d to %d; want one advance to %d", told, before, b.generation(), before+1)
		}
	})
}

// C1, C3, F3: a database failure in the logging step rolls back the QSO and
// its upload row. That row really was in the transaction, before the step:
// the failure injected is reached only after the trigger has found it
// (failTheStep). The caller gets an error that is not a refusal, and nothing
// reports the contact as logged. The same record, sent again once the file
// can be written, is stored and joins: the failure left nothing of it behind.
func TestMembership_AFailedStepRollsBackTheContact(t *testing.T) {
	b := newMemberBed(t)
	st := b.open(memLifeA)
	b.live(b.x)
	notified := 0
	b.s.SetCommitNotifier(func() { notified++ })
	ch, cancel := b.s.Hub.Subscribe()
	defer cancel()
	restore := b.failTheStep()
	before := b.record()
	qsos, uploads := b.count(`SELECT COUNT(*) FROM qso`), b.count(`SELECT COUNT(*) FROM qso_upload`)
	rec := b.nextRec()

	res, err := b.s.SubmitLive(context.Background(), b.x, rec, false)

	if err == nil || IsSubmitError(err) != nil || res.Status != "" {
		t.Fatalf("the submit answered %+v, %v; want a failure that is not a refusal", res, err)
	}
	if !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("the submit failed for another reason, so the case proves nothing: %v", err)
	}
	if q, u := b.count(`SELECT COUNT(*) FROM qso`), b.count(`SELECT COUNT(*) FROM qso_upload`); q != qsos || u != uploads {
		t.Fatalf("after the failure the file holds %d contacts and %d upload rows; want %d and %d", q, u, qsos, uploads)
	}
	if after := b.record(); after != before {
		t.Fatalf("the session record changed:\n%s\nwas\n%s", after, before)
	}
	if n, told := drainStored(ch), b.told(); n != 0 || notified != 0 || len(told) != 0 {
		t.Fatalf("a contact that was not stored was reported: %d qso.stored, %d commit notices, changes %v", n, notified, told)
	}

	// The same record again, once the file can be written. Stored, not a
	// duplicate of anything the failure left; a member; and reported once.
	restore()
	again, err := b.s.SubmitLive(context.Background(), b.x, rec, false)
	if err != nil || again.Status != "stored" {
		t.Fatalf("the same record, sent again: %+v, %v; want it stored", again, err)
	}
	if b.memberOf(again.ID) != st.Open.ID || b.uploads(again.ID) != 1 {
		t.Fatalf("sent again, the contact is in %q with %d upload rows; want the open session %s and one", b.memberOf(again.ID), b.uploads(again.ID), st.Open.ID)
	}
	if n := drainStored(ch); n != 1 || notified != 1 {
		t.Fatalf("the stored contact was reported by %d qso.stored and %d commit notices; want one of each", n, notified)
	}
}

// The same when the step's own transaction would have opened a session: the
// session goes with the contact, and nobody is told of a change.
func TestMembership_AFailedStepRollsBackTheSessionItOpened(t *testing.T) {
	b := newMemberBed(t)
	b.failTheStep()
	before := b.record()
	_, err := b.s.SubmitLive(context.Background(), b.x, b.nextRec(), false)
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("the submit answered %v; want the injected failure", err)
	}
	if after := b.record(); after != before || len(b.told()) != 0 || b.count(`SELECT COUNT(*) FROM qso`) != 0 {
		t.Fatalf("a rolled-back contact left something behind:\n%s\nwas\n%s", after, before)
	}
}

// A1 for contacts logged at once: several submitters, each logging contacts
// into the default logbook and into another, with no session open at the
// start. No submit fails; the contacts of the default logbook share one
// session; every contact is where its own insert put it.
//
// This forces no order and makes no claim about one. C4's evidence, a live
// submit against New, Continue, an exit and a change of the default pointer
// with each writer first in turn, is internal/database/sqlite's
// operating_session_submit_order_test.go. An earlier form of this test ran
// those controls beside the submits and judged the mix by counts: it depended
// on how the goroutines were scheduled, and could pass with a contact's
// membership lost (Codex's review, codex-259).
func TestMembership_ContactsLoggedAtOnceEachJoin(t *testing.T) {
	const submitters, each = 4, 15
	b := newMemberBed(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, submitters*each)
	for w := 0; w < submitters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				logbook := b.x
				if (w+i)%3 == 0 {
					logbook = b.y
				}
				res, err := b.s.SubmitLive(ctx, logbook, b.nextRec(), false)
				if err != nil || res.Status != "stored" {
					errs <- fmt.Errorf("a live submit into logbook %d: %+v, %w", logbook, res, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := b.count(`SELECT COUNT(*) FROM qso`); n != submitters*each {
		t.Fatalf("%d contacts stored, want %d", n, submitters*each)
	}
	b.everyContactIsWhereItsInsertPutIt()
	if sessions, told := b.count(`SELECT COUNT(*) FROM operating_session`), b.told(); sessions != 1 || len(told) != 1 {
		t.Fatalf("%d sessions and changes told %v; want the one session the first contact opened, told once", sessions, told)
	}
}

// ---- D: the session a contact opens ----

// D1, D3: with no session open the contact opens a new one, held by the
// lifetime the membership names; by nobody when it names none, or names one
// whose exit the file records.
func TestMembership_TheSafetyNet(t *testing.T) {
	ctx := context.Background()
	t.Run("held by the lifetime named", func(t *testing.T) {
		b := newMemberBed(t)
		qso := b.live(b.x)
		st := b.state(memLifeA)
		if st.Open == nil || st.Open.ID != b.memberOf(qso) || st.Open.Holder != memLifeA {
			t.Fatalf("the open session is %+v and the contact joined %q; want one held by %s", st.Open, b.memberOf(qso), memLifeA)
		}
		if told := b.told(); len(told) != 1 || told[0] != st.Generation {
			t.Fatalf("told of %v; want one advance, to generation %d", told, st.Generation)
		}
		if seen := b.membersAtTelling; len(seen) != 1 || seen[0] != 1 {
			t.Fatalf("when the change was told another connection saw %v member rows; want the contact's, committed", seen)
		}
	})
	t.Run("held by nobody when no lifetime is named", func(t *testing.T) {
		b := newMemberBed(t)
		b.setHolder("")
		qso := b.live(b.x)
		if st := b.state(""); st.Open == nil || st.Open.ID != b.memberOf(qso) || st.Open.Holder != "" {
			t.Fatalf("the open session is %+v; want one held by nobody", st.Open)
		}
	})
	t.Run("held by nobody when the lifetime named has exited", func(t *testing.T) {
		b := newMemberBed(t)
		st := b.open(memLifeA)
		if _, _, err := b.s.DB.RecordOperatingAppExitWithContext(ctx, memLifeA, st.Generation, true, memberMorning); err != nil {
			t.Fatal(err)
		}
		qso := b.live(b.x) // the membership still names A
		if now := b.state(""); now.Open == nil || now.Open.ID != b.memberOf(qso) || now.Open.Holder != "" {
			t.Fatalf("the open session is %+v; want one held by nobody", now.Open)
		}
	})
	t.Run("a closed session is never continued by a contact", func(t *testing.T) {
		b := newMemberBed(t)
		st := b.open(memLifeA)
		first := b.live(b.x)
		if _, _, err := b.s.DB.RecordOperatingAppExitWithContext(ctx, memLifeA, st.Generation, true, memberMorning); err != nil {
			t.Fatal(err)
		}
		b.setHolder(memLifeB)
		second := b.live(b.x)
		if b.memberOf(second) == "" || b.memberOf(second) == b.memberOf(first) {
			t.Fatalf("the second contact joined %q and the first %q; want a new session", b.memberOf(second), b.memberOf(first))
		}
	})
}

// D2: the holder is read once for a contact, and after the QSO's insert has
// taken the writer's position: while it is read, no other connection can
// become a writer.
func TestMembership_TheHolderIsReadOnceWithTheWritersPositionHeld(t *testing.T) {
	b := newMemberBed(t)
	other, err := sql.Open(sqlite.SqliteDriver, "file:"+b.path+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	var attempt error
	b.onHolder = func() {
		conn, err := other.Conn(context.Background())
		if err != nil {
			attempt = fmt.Errorf("no connection: %w", err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
			attempt = err
			return
		}
		_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
	}
	b.live(b.x)
	b.mu.Lock()
	reads := b.holderReads
	b.mu.Unlock()
	if reads != 1 {
		t.Fatalf("the holder was read %d times for one contact, want once", reads)
	}
	if attempt == nil || !strings.Contains(attempt.Error(), "locked") {
		t.Fatalf("another connection's attempt to write while the holder was read answered %v; want it refused, the contact's transaction holding the writer's position", attempt)
	}
}

// ---- F: what is told, and when ----

// F1: the membership is told the generation the contact's own transaction
// produced, after that transaction has committed and after the contact's own
// notices. Another writer's advance in between is not reported in its place.
func TestMembership_TheChangeToldIsTheContactsOwn(t *testing.T) {
	b := newMemberBed(t)
	ctx := context.Background()
	var inNotice error
	b.s.SetCommitNotifier(func() {
		// The contact is committed: a New here is another writer's advance.
		st := b.state(memLifeA)
		if st.Open == nil || !st.Open.HasMembers {
			inNotice = fmt.Errorf("at the commit notice the session is %+v; want the contact's session, committed", st.Open)
			return
		}
		if _, outcome, err := b.s.DB.NewOperatingSessionWithContext(ctx, memLifeA, st.Generation, memberMorning); err != nil || outcome != sqlite.SessionApplied {
			inNotice = fmt.Errorf("the New in the notice: %s, %v", outcome, err)
		}
	})
	before := b.generation()
	b.live(b.x) // no session open: the contact opens one
	if inNotice != nil {
		t.Fatal(inNotice)
	}
	if b.generation() != before+2 {
		t.Fatalf("the generation went from %d to %d; the fixture wants the contact's advance and the New's", before, b.generation())
	}
	if told := b.told(); len(told) != 1 || told[0] != int64(before+1) {
		t.Fatalf("told of %v; want only the contact's own advance, to generation %d", told, before+1)
	}
}

// ---- G: later changes ----

// G: an edit of a member keeps its row; a delete keeps its row, and the
// session still has contacts while its count drops; the count comes back with
// the contact. (No route restores a deleted contact; the row is restored here
// as the file would hold it.)
func TestMembership_LaterChangesKeepTheMemberRow(t *testing.T) {
	b := newMemberBed(t)
	st := b.open(memLifeA)
	qso := b.live(b.x)
	b.live(b.x)
	ctx := context.Background()
	fetch := func() types.Qso {
		t.Helper()
		q, err := b.s.DB.FetchQsoByIdWithContext(ctx, qso)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	for _, patch := range []string{
		`{"time_on":"0800"}`,
		`{"freq":"7.150","band":"40m"}`,
		`{"mode":"CW"}`,
		`{"call":"K9ZZZ"}`,
	} {
		if _, err := b.s.Update(ctx, fetch(), []byte(patch), source.API); err != nil {
			t.Fatalf("edit %s: %v", patch, err)
		}
		if got := b.memberOf(qso); got != st.Open.ID {
			t.Fatalf("after the edit %s the contact is a member of %q, want %s", patch, got, st.Open.ID)
		}
	}
	if got := fetch(); got.Call != "K9ZZZ" || got.Mode != "CW" || got.Band != "40m" {
		t.Fatalf("the edits did not land (%s %s %s), so the case proves nothing", got.Call, got.Mode, got.Band)
	}

	if err := b.s.Delete(ctx, fetch(), source.API); err != nil {
		t.Fatalf("delete: %v", err)
	}
	now := b.state(memLifeA)
	if b.memberOf(qso) != st.Open.ID || now.Open == nil || !now.Open.HasMembers || now.Open.ContactCount != 1 {
		t.Fatalf("after the delete the contact is a member of %q and the session is %+v; want the row kept and one contact counted", b.memberOf(qso), now.Open)
	}
	b.exec(`UPDATE qso SET deleted_at = NULL WHERE id = ?`, qso)
	if now := b.state(memLifeA); now.Open == nil || now.Open.ContactCount != 2 {
		t.Fatalf("with the contact back the session is %+v; want two contacts", now.Open)
	}
	if b.generation() != int(st.Generation) || len(b.told()) != 0 {
		t.Fatalf("an edit or a delete moved the generation from %d to %d", st.Generation, b.generation())
	}
}

// ---- Part 6, slice 1: qso.stored names the session the contact joined ----

// storedEvents drains the hub of qso.stored events and returns their payloads.
func storedEvents(ch <-chan events.Event) []events.QsoStoredPayload {
	var out []events.QsoStoredPayload
	for {
		select {
		case ev := <-ch:
			if ev.Name == events.NameQsoStored {
				out = append(out, ev.Payload.(events.QsoStoredPayload))
			}
		default:
			return out
		}
	}
}

// A window learns that a stored contact is a member from the contact's own
// qso.stored (W-0029 part 6). It names the session the contact joined: the
// open one for a plain join, the new one for the contact that opened it. It
// names none for a contact that joined none: a deliberate submit, a live
// submit to another logbook, an import, and any contact while the membership
// is not connected.
func TestMembership_QsoStoredNamesTheSessionJoined(t *testing.T) {
	ctx := context.Background()
	t.Run("a plain join, and the contact that opens a session", func(t *testing.T) {
		b := newMemberBed(t)
		ch, cancel := b.s.Hub.Subscribe()
		defer cancel()

		opener := b.live(b.x) // no session open: it opens one
		joiner := b.live(b.x)

		got := storedEvents(ch)
		session := b.memberOf(opener)
		if len(got) != 2 || got[0].QsoID != opener || got[1].QsoID != joiner {
			t.Fatalf("the events are %+v; want one for each contact, in order", got)
		}
		if session == "" || got[0].OperatingSessionID != session || got[1].OperatingSessionID != session {
			t.Fatalf("the events name sessions %q and %q; want %s, which both contacts joined", got[0].OperatingSessionID, got[1].OperatingSessionID, session)
		}
	})
	t.Run("a contact that joined none names none", func(t *testing.T) {
		b := newMemberBed(t)
		b.open(memLifeA)
		ch, cancel := b.s.Hub.Subscribe()
		defer cancel()

		if _, err := b.s.Submit(ctx, b.x, b.nextRec(), false); err != nil { // deliberate
			t.Fatal(err)
		}
		b.live(b.y) // another logbook
		rec := b.nextRec()
		rec.StationCallsign = "G4ABC"
		if _, err := b.s.SubmitImport(ctx, b.x, rec, false, nil); err != nil {
			t.Fatal(err)
		}

		got := storedEvents(ch)
		if len(got) != 3 {
			t.Fatalf("%d events, want one for each of the three contacts", len(got))
		}
		for _, p := range got {
			if p.OperatingSessionID != "" {
				t.Fatalf("the event for contact %d names session %q; it joined none", p.QsoID, p.OperatingSessionID)
			}
		}
	})
	t.Run("unconnected, a live contact names none", func(t *testing.T) {
		b := newUnconnectedMemberBed(t)
		ch, cancel := b.s.Hub.Subscribe()
		defer cancel()
		b.live(b.x)
		if got := storedEvents(ch); len(got) != 1 || got[0].OperatingSessionID != "" {
			t.Fatalf("the event is %+v; want it to name no session", got)
		}
	})
}

// The first contact's qso.stored is published before the telling of the
// session it opened. A window therefore meets a contact of a session it does
// not show yet; the order is pinned here so that the page's handling of it is
// tested against what the daemon really does.
func TestMembership_QsoStoredComesBeforeTheSessionsChange(t *testing.T) {
	b := newUnconnectedMemberBed(t)
	ch, cancel := b.s.Hub.Subscribe()
	defer cancel()
	var order []string
	b.s.SetSessionMembership(&SessionMembership{
		Holder: func() string { return memLifeA },
		Advanced: func(int64) {
			if got := storedEvents(ch); len(got) == 1 {
				order = append(order, "qso.stored for session "+got[0].OperatingSessionID)
			}
			order = append(order, "the session's change")
		},
		Now: func() time.Time { return memberMorning },
	})
	qso := b.live(b.x)
	want := []string{"qso.stored for session " + b.memberOf(qso), "the session's change"}
	if len(order) != 2 || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("the order was %v; want %v", order, want)
	}
}

// runAtLine is a log writer that runs fn once, when a line holding marker is
// written. It is this fixture's own: the test logger holds its lock while it
// writes, so whatever fn does must not log through the same writer.
type runAtLine struct {
	marker string
	fn     func()
	ran    bool
}

func (w *runAtLine) Write(p []byte) (int, error) {
	if !w.ran && strings.Contains(string(p), w.marker) {
		w.ran = true
		w.fn()
	}
	return len(p), nil
}

// The session named is the one the contact's own transaction put it in, not
// one looked up afterwards. Here New is run after the contact has committed
// and before its event is published (at the "QSO stored" log line, which is
// written between the two): the event names the session the contact is in,
// which New has just closed, and not the session then open.
func TestMembership_QsoStoredNamesTheContactsSessionNotTheOneOpenLater(t *testing.T) {
	b := newMemberBed(t)
	st := b.open(memLifeA)
	b.live(b.x) // so that New keeps the session
	ch, cancel := b.s.Hub.Subscribe()
	defer cancel()
	var newErr error
	b.s.Logger = logging.NewForWriter(&runAtLine{marker: "QSO stored", fn: func() {
		now := b.state(memLifeA)
		if _, outcome, err := b.s.DB.NewOperatingSessionWithContext(context.Background(), memLifeA, now.Generation, memberMorning); err != nil || outcome != sqlite.SessionApplied {
			newErr = fmt.Errorf("the New at the log line: %s, %v", outcome, err)
		}
	}})

	qso := b.live(b.x)

	if newErr != nil {
		t.Fatal(newErr)
	}
	open := b.state(memLifeA).Open
	if b.memberOf(qso) != st.Open.ID || open == nil || open.ID == st.Open.ID {
		t.Fatalf("the fixture: the contact is in %q and %+v is open; want it in %s, with another session open", b.memberOf(qso), open, st.Open.ID)
	}
	got := storedEvents(ch)
	if len(got) != 1 || got[0].QsoID != qso || got[0].OperatingSessionID != st.Open.ID {
		t.Fatalf("the event is %+v; want it to name %s, the session the contact joined, and not %s, the one open when it was published",
			got, st.Open.ID, open.ID)
	}
}
