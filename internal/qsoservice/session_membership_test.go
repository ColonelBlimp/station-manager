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
	return b
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

// C3, F3: a database failure in the logging step rolls back the QSO and its
// upload row, which really is in the transaction; the caller gets an error
// that is not a refusal; and nothing reports the contact as logged.
func TestMembership_AFailedStepRollsBackTheContact(t *testing.T) {
	b := newMemberBed(t)
	b.open(memLifeA)
	b.live(b.x)
	notified := 0
	b.s.SetCommitNotifier(func() { notified++ })
	ch, cancel := b.s.Hub.Subscribe()
	defer cancel()
	b.exec(`CREATE TRIGGER injected_failure BEFORE INSERT ON operating_session_member
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	before := b.record()
	qsos, uploads := b.count(`SELECT COUNT(*) FROM qso`), b.count(`SELECT COUNT(*) FROM qso_upload`)

	res, err := b.s.SubmitLive(context.Background(), b.x, b.nextRec(), false)

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

	// The same contact, once the file can be written: stored, and a member.
	b.exec(`DROP TRIGGER injected_failure`)
	if qso := b.live(b.x); b.memberOf(qso) == "" {
		t.Fatal("the contact did not join once the failure was removed")
	}
}

// The same when the step's own transaction would have opened a session: the
// session goes with the contact, and nobody is told of a change.
func TestMembership_AFailedStepRollsBackTheSessionItOpened(t *testing.T) {
	b := newMemberBed(t)
	b.exec(`CREATE TRIGGER injected_failure BEFORE INSERT ON operating_session_member
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	before := b.record()
	if _, err := b.s.SubmitLive(context.Background(), b.x, b.nextRec(), false); err == nil {
		t.Fatal("the submit succeeded with the member row refused")
	}
	if after := b.record(); after != before || len(b.told()) != 0 || b.count(`SELECT COUNT(*) FROM qso`) != 0 {
		t.Fatalf("a rolled-back contact left something behind:\n%s\nwas\n%s", after, before)
	}
}

const contendSubmitters, contendEach = 4, 15

// contendSubmits logs submitters*each live contacts from several goroutines
// while control runs, one round begun with each submit, and fails the test
// if any call fails or the file's invariants do not hold afterwards.
func contendSubmits(t *testing.T, b *memberBed, control func(round int) error) {
	const submitters, each = contendSubmitters, contendEach
	t.Helper()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, submitters*each+1)
	rounds := make(chan struct{}, submitters*each)
	for w := 0; w < submitters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				logbook := b.x
				if (w+i)%3 == 0 {
					logbook = b.y
				}
				rounds <- struct{}{}
				res, err := b.s.SubmitLive(ctx, logbook, b.nextRec(), false)
				if err != nil || res.Status != "stored" {
					errs <- fmt.Errorf("a live submit into logbook %d: %+v, %w", logbook, res, err)
					return
				}
			}
		}(w)
	}
	controls := make(chan error, 1)
	go func() {
		round := 0
		for range rounds {
			if err := control(round); err != nil {
				controls <- err
				for range rounds { // let the submitters finish
				}
				return
			}
			round++
		}
		controls <- nil
	}()
	wg.Wait()
	close(rounds)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if err := <-controls; err != nil {
		t.Errorf("a control failed: %v", err)
	}
	if n := b.count(`SELECT COUNT(*) FROM qso`); n != submitters*each {
		t.Fatalf("%d contacts stored, want %d", n, submitters*each)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_member m JOIN qso q ON q.id = m.qso_id
		JOIN logbook l ON l.id = q.logbook_id JOIN operating_session s ON s.id = m.session_id
		WHERE s.logbook_uuid <> l.uuid`); n != 0 {
		t.Fatalf("%d contacts are members of a session of another logbook", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`); n > 1 {
		t.Fatalf("%d sessions are open", n)
	}
}

// C4: contention is not a failure, shown through the real submit. Live
// submits run against the session's controls and against changes of the
// default pointer, on the file with the configuration's own connection
// settings. No call fails, and every contact is in at most one session, which
// is always its own logbook's.
//
// The controls are paced by the submits: one round begins as each submit
// does, so every round overlaps contacts being logged. They are not run in a
// loop of their own. A loop of durable transactions with no pause holds the
// writer's position nearly all the time, SQLite's busy handler gives a waiting
// writer no turn, and a submit then fails at its QSO insert when the busy
// limit passes: seen here under the race detector, 2026-11-25. That is
// saturation of the file, with or without a session, and not what an operator
// pressing New does.
func TestMembership_ContentionIsNotAFailure(t *testing.T) {
	t.Run("against New, Continue and an exit", func(t *testing.T) {
		b := newMemberBed(t)
		ctx := context.Background()
		contendSubmits(t, b, func(round int) error {
			// Each round is a new app: it answers New for whatever session it
			// finds, continues the one it closed, and exits.
			life := fmt.Sprintf("lifetime-%d", round)
			b.setHolder(life)
			st, _, err := b.s.DB.OpenOperatingSessionWithContext(ctx, life, memberMorning)
			if err != nil {
				return err
			}
			earlier := ""
			if st.Open != nil {
				earlier = st.Open.ID
			}
			if st, _, err = b.s.DB.NewOperatingSessionWithContext(ctx, life, st.Generation, memberMorning); err != nil {
				return err
			}
			if earlier != "" {
				if st, _, err = b.s.DB.ContinueOperatingSessionWithContext(ctx, life, st.Generation, earlier, memberMorning); err != nil {
					return err
				}
			}
			_, _, err = b.s.DB.RecordOperatingAppExitWithContext(ctx, life, st.Generation, true, memberMorning)
			return err
		})
		if sessions, exits := b.count(`SELECT COUNT(*) FROM operating_session`), b.count(`SELECT COUNT(*) FROM operating_app_exit`); sessions < 2 || exits < 1 {
			t.Fatalf("%d sessions and %d exits: the controls did not run against the submits, so the case proves nothing", sessions, exits)
		}
		// The default never moved: every contact of X is in exactly one session
		// and no contact of Y is in any.
		if n := b.count(`SELECT COUNT(*) FROM qso q WHERE q.logbook_id = ? AND
			(SELECT COUNT(*) FROM operating_session_member m WHERE m.qso_id = q.id) <> 1`, b.x); n != 0 {
			t.Fatalf("%d contacts of the default logbook are not in exactly one session", n)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session_member m JOIN qso q ON q.id = m.qso_id WHERE q.logbook_id = ?`, b.y); n != 0 {
			t.Fatalf("%d contacts of another logbook joined a session", n)
		}
	})
	t.Run("against changes of the default pointer", func(t *testing.T) {
		b := newMemberBed(t)
		contendSubmits(t, b, func(round int) error {
			to := b.y
			if round%2 == 1 {
				to = b.x
			}
			return b.s.DB.SetArchiveDefaultLogbookWithContext(context.Background(), to)
		})
		if epoch := b.count(`SELECT default_epoch FROM archive_metadata WHERE singleton = 1`); epoch < 3 {
			t.Fatalf("the default epoch is %d: the pointer did not move against the submits, so the case proves nothing", epoch)
		}
		if n := b.count(`SELECT COUNT(DISTINCT s.logbook_uuid) FROM operating_session_member m JOIN operating_session s ON s.id = m.session_id`); n != 2 {
			t.Fatalf("contacts joined sessions of %d logbooks; the case wants both, each while it was the default", n)
		}
	})
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
