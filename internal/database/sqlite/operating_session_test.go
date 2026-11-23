package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The operating session's transitions (ADR 0100; W-0029 part 1, groups C, D
// and E). Every assertion is on rows read back from the file, never on what
// one call returned alone.

const (
	lifeA = "lifetime-A"
	lifeB = "lifetime-B"
)

var sessT0 = time.Date(2026, 11, 20, 9, 15, 0, 0, time.UTC)

// at is the instant a number of minutes into the test's morning.
func at(minutes int) time.Time { return sessT0.Add(time.Duration(minutes) * time.Minute) }

// sessBed is a file with an identity row and two live logbooks, X and Y. With
// a default, X is the default and the epoch reads 1.
type sessBed struct {
	t            *testing.T
	svc          *Service
	x, y         int64
	xUUID, yUUID string
	qsos         atomic.Int64
}

func newSessBed(t *testing.T, withDefault bool) *sessBed {
	t.Helper()
	svc, _ := testMigratedFileService(t)
	ctx := context.Background()
	b := &sessBed{t: t, svc: svc}
	var err error
	if b.x, err = svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "X", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("logbook X: %v", err)
	}
	if b.y, err = svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Y", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("logbook Y: %v", err)
	}
	if _, err := svc.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatalf("ensure identity: %v", err)
	}
	b.xUUID, b.yUUID = b.text(`SELECT uuid FROM logbook WHERE id = ?`, b.x), b.text(`SELECT uuid FROM logbook WHERE id = ?`, b.y)
	if !utils.IsValidUUIDv7(b.xUUID) || !utils.IsValidUUIDv7(b.yUUID) || b.xUUID == b.yUUID {
		t.Fatalf("the logbooks' uuids are %q and %q", b.xUUID, b.yUUID)
	}
	if withDefault {
		b.setDefault(b.x)
	}
	return b
}

func (b *sessBed) text(q string, args ...any) string {
	b.t.Helper()
	var s sql.NullString
	if err := b.svc.handle.QueryRow(q, args...).Scan(&s); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
	return s.String
}

func (b *sessBed) count(q string, args ...any) int {
	b.t.Helper()
	return countT(b.t, b.svc, q, args...)
}

func (b *sessBed) setDefault(logbook int64) {
	b.t.Helper()
	if err := b.svc.SetArchiveDefaultLogbookWithContext(context.Background(), logbook); err != nil {
		b.t.Fatalf("set the default to %d: %v", logbook, err)
	}
}

// generation and epoch, as stored.
func (b *sessBed) generation() int {
	b.t.Helper()
	return b.count(`SELECT session_generation FROM archive_metadata WHERE singleton = 1`)
}

// records is every session, interval, member and recorded exit, as text, in a
// fixed order. Two equal dumps mean nothing of the record changed.
func (b *sessBed) records() []string {
	b.t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT 'session', id, logbook_uuid, default_epoch, opened_at, closed_at, close_reason FROM operating_session ORDER BY id`,
		`SELECT 'interval', session_id, seq, started_at, ended_at, end_state, app_lifetime FROM operating_session_interval ORDER BY session_id, seq`,
		`SELECT 'member', qso_id, session_id FROM operating_session_member ORDER BY qso_id`,
		`SELECT 'exit', app_lifetime, exited_at FROM operating_app_exit ORDER BY app_lifetime`,
		`SELECT 'counters', session_generation, default_epoch, arrived_activation FROM archive_metadata`,
	} {
		out = append(out, dumpQuery(b.t, b.svc, q)...)
	}
	return out
}

// unchanged runs a call and fails if anything of the record changed.
func (b *sessBed) unchanged(what string, call func()) {
	b.t.Helper()
	before := b.records()
	call()
	if after := b.records(); !reflect.DeepEqual(before, after) {
		b.t.Fatalf("%s changed the record:\n before %v\n after  %v", what, before, after)
	}
}

// invariants are what the schema alone does not hold (codex-184): after every
// completed transition an open session has exactly one open interval, a closed
// session has none, and at most one session is open.
func (b *sessBed) invariants() {
	b.t.Helper()
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`); n > 1 {
		b.t.Fatalf("%d sessions are open", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session s WHERE s.closed_at IS NULL
		AND (SELECT COUNT(*) FROM operating_session_interval i WHERE i.session_id = s.id AND i.end_state = 'open') <> 1`); n != 0 {
		b.t.Fatal("the open session does not have exactly one open interval")
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_interval i JOIN operating_session s ON s.id = i.session_id
		WHERE s.closed_at IS NOT NULL AND i.end_state = 'open'`); n != 0 {
		b.t.Fatalf("%d open interval(s) belong to a closed session", n)
	}
}

type sessionRow struct {
	exists   bool
	logbook  string
	epoch    int
	openedAt time.Time
	closedAt sql.NullTime
	reason   string
}

func (b *sessBed) session(id string) sessionRow {
	b.t.Helper()
	var r sessionRow
	var reason sql.NullString
	err := b.svc.handle.QueryRow(`SELECT logbook_uuid, default_epoch, opened_at, closed_at, close_reason FROM operating_session WHERE id = ?`, id).
		Scan(&r.logbook, &r.epoch, &r.openedAt, &r.closedAt, &reason)
	if err == sql.ErrNoRows {
		return r
	}
	if err != nil {
		b.t.Fatalf("read session %s: %v", id, err)
	}
	r.exists, r.reason = true, reason.String
	return r
}

// intervals are a session's sittings as "seq state holder started ended", the
// times as minutes into the morning and "-" for none.
func (b *sessBed) intervals(id string) []string {
	b.t.Helper()
	rows, err := b.svc.handle.Query(`SELECT seq, end_state, COALESCE(app_lifetime, '-'), started_at, ended_at
		FROM operating_session_interval WHERE session_id = ? ORDER BY seq`, id)
	if err != nil {
		b.t.Fatalf("intervals of %s: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var (
			seq           int
			state, holder string
			started       time.Time
			ended         sql.NullTime
		)
		if err := rows.Scan(&seq, &state, &holder, &started, &ended); err != nil {
			b.t.Fatalf("intervals of %s: %v", id, err)
		}
		end := "-"
		if ended.Valid {
			end = fmt.Sprint(int(ended.Time.Sub(sessT0).Minutes()))
		}
		out = append(out, fmt.Sprintf("%d %s %s %d %s", seq, state, holder, int(started.Sub(sessT0).Minutes()), end))
	}
	if err := rows.Err(); err != nil {
		b.t.Fatalf("intervals of %s: %v", id, err)
	}
	return out
}

func (b *sessBed) wantIntervals(id string, want ...string) {
	b.t.Helper()
	if got := b.intervals(id); !reflect.DeepEqual(got, want) {
		b.t.Fatalf("intervals of %s = %v, want %v", id, got, want)
	}
}

func (b *sessBed) wantClosed(id, reason string, minutes int) {
	b.t.Helper()
	r := b.session(id)
	if !r.exists || !r.closedAt.Valid || r.reason != reason || !r.closedAt.Time.Equal(at(minutes)) {
		b.t.Fatalf("session %s = %+v, want closed with %s at minute %d", id, r, reason, minutes)
	}
}

// members of a session, as QSO ids.
func (b *sessBed) members(id string) []int64 {
	b.t.Helper()
	rows, err := b.svc.handle.Query(`SELECT qso_id FROM operating_session_member WHERE session_id = ? ORDER BY qso_id`, id)
	if err != nil {
		b.t.Fatalf("members of %s: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var q int64
		if err := rows.Scan(&q); err != nil {
			b.t.Fatalf("members of %s: %v", id, err)
		}
		out = append(out, q)
	}
	return out
}

func (b *sessBed) exitRecorded(lifetime string) bool {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM operating_app_exit WHERE app_lifetime = ?`, lifetime) == 1
}

// The transitions, each followed by the invariants.

func (b *sessBed) state(lifetime string) OperatingSessionState {
	b.t.Helper()
	st, err := b.svc.OperatingSessionStateWithContext(context.Background(), lifetime)
	if err != nil {
		b.t.Fatalf("read the state: %v", err)
	}
	return st
}

func (b *sessBed) done(what string, st OperatingSessionState, got SessionOutcome, err error, want SessionOutcome) OperatingSessionState {
	b.t.Helper()
	if err != nil {
		b.t.Fatalf("%s: %v", what, err)
	}
	if got != want {
		b.t.Fatalf("%s: outcome %q, want %q", what, got, want)
	}
	b.invariants()
	if g := b.generation(); st.Generation != int64(g) {
		b.t.Fatalf("%s returned generation %d; the file holds %d", what, st.Generation, g)
	}
	return st
}

func (b *sessBed) open(lifetime string, minutes int, want SessionOutcome) OperatingSessionState {
	b.t.Helper()
	st, got, err := b.svc.OpenOperatingSessionWithContext(context.Background(), lifetime, at(minutes))
	return b.done("open for "+lifetime, st, got, err, want)
}

func (b *sessBed) newSession(lifetime string, generation int64, minutes int, want SessionOutcome) OperatingSessionState {
	b.t.Helper()
	st, got, err := b.svc.NewOperatingSessionWithContext(context.Background(), lifetime, generation, at(minutes))
	return b.done("New by "+lifetime, st, got, err, want)
}

func (b *sessBed) continueSession(lifetime string, generation int64, id string, minutes int, want SessionOutcome) OperatingSessionState {
	b.t.Helper()
	st, got, err := b.svc.ContinueOperatingSessionWithContext(context.Background(), lifetime, generation, id, at(minutes))
	return b.done("Continue by "+lifetime, st, got, err, want)
}

func (b *sessBed) closeForSwitch(arrival bool, minutes int, want SessionOutcome) OperatingSessionState {
	b.t.Helper()
	st, got, err := b.svc.CloseOperatingSessionForSwitchWithContext(context.Background(), arrival, at(minutes))
	return b.done("close for a switch", st, got, err, want)
}

func (b *sessBed) exit(lifetime string, generation int64, current bool, minutes int, want SessionOutcome) OperatingSessionState {
	b.t.Helper()
	st, got, err := b.svc.RecordOperatingAppExitWithContext(context.Background(), lifetime, generation, current, at(minutes))
	return b.done("exit of "+lifetime, st, got, err, want)
}

// log stores a QSO as the live paths do: the QSO row, an upload row and the
// logging step, in one transaction. It returns the QSO's id and the session
// the step says it joined ("" for none).
func (b *sessBed) log(logbook int64, holder string, minutes int) (int64, string) {
	b.t.Helper()
	ctx := context.Background()
	tx, cancel, err := b.svc.BeginTxContext(ctx)
	if err != nil {
		b.t.Fatalf("begin: %v", err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()
	qso, joined := b.logIn(ctx, tx, logbook, holder, minutes)
	if err := tx.Commit(); err != nil {
		b.t.Fatalf("commit the QSO: %v", err)
	}
	b.invariants()
	return qso, joined
}

func (b *sessBed) logIn(ctx context.Context, tx *sql.Tx, logbook int64, holder string, minutes int) (int64, string) {
	b.t.Helper()
	qso, joined, err := b.logSteps(ctx, tx, logbook, holder, minutes)
	if err != nil {
		b.t.Fatal(err)
	}
	return qso, joined
}

// logSteps is the logging transaction's three writes, in the order the live
// paths make them. It reports an error and never fails the test itself, so a
// goroutine can call it.
func (b *sessBed) logSteps(ctx context.Context, tx *sql.Tx, logbook int64, holder string, minutes int) (int64, string, error) {
	qso, err := b.svc.InsertQsoTx(ctx, tx, b.nextQso(logbook))
	if err != nil {
		return 0, "", fmt.Errorf("insert the QSO: %w", err)
	}
	return b.logRest(ctx, tx, qso, holder, minutes)
}

// nextQso is a QSO no other call of the bed has made.
func (b *sessBed) nextQso(logbook int64) types.Qso {
	return validTestQso(logbook, fmt.Sprintf("M%dABC", b.qsos.Add(1)), "40m", "SSB", "20261120", "0915")
}

// logRest is what follows the QSO row: its upload row and the logging step.
func (b *sessBed) logRest(ctx context.Context, tx *sql.Tx, qso int64, holder string, minutes int) (int64, string, error) {
	if err := b.svc.InsertQsoUploadTx(ctx, tx, qso, action.Insert, "qrz", "qrz", origin.Live); err != nil {
		return 0, "", fmt.Errorf("insert the upload row: %w", err)
	}
	joined, err := b.svc.JoinOperatingSessionTx(ctx, tx, qso, holder, at(minutes))
	if err != nil {
		return 0, "", fmt.Errorf("the logging step: %w", err)
	}
	return qso, joined, nil
}

// stored is whether a QSO and its upload row are in the file.
func (b *sessBed) stored(qso int64) bool {
	b.t.Helper()
	return b.count(`SELECT COUNT(*) FROM qso WHERE id = ?`, qso) == 1 &&
		b.count(`SELECT COUNT(*) FROM qso_upload WHERE qso_id = ?`, qso) == 1
}

func wantOpen(t *testing.T, st OperatingSessionState, logbook, holder string, epoch int64, openedMin, intervalMin int, members bool) string {
	t.Helper()
	o := st.Open
	if o == nil {
		t.Fatalf("no session is open; want one held by %q", holder)
	}
	if !utils.IsValidUUIDv7(o.ID) || o.LogbookUUID != logbook || o.Holder != holder || o.DefaultEpoch != epoch ||
		!o.OpenedAt.Equal(at(openedMin)) || !o.IntervalStartedAt.Equal(at(intervalMin)) || o.HasMembers != members {
		t.Fatalf("open session = %+v; want logbook %s, holder %q, epoch %d, opened at minute %d, interval from minute %d, members %v",
			*o, logbook, holder, epoch, openedMin, intervalMin, members)
	}
	return o.ID
}

// ---- Open, for a lifetime (proposal A's table) ----

func TestOperatingSession_Open_NoSessionOpensOne_AndIsIdempotent(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	id := wantOpen(t, st, b.xUUID, lifeA, 1, 0, 0, false)
	if st.Generation != 1 || st.DefaultEpoch != 1 {
		t.Fatalf("generation %d, epoch %d; want 1 and 1", st.Generation, st.DefaultEpoch)
	}
	b.wantIntervals(id, "1 open "+lifeA+" 0 -")

	b.unchanged("a second open for the same lifetime", func() {
		again := b.open(lifeA, 5, SessionUnchanged)
		if again.Open == nil || again.Open.ID != id || again.Generation != 1 {
			t.Fatalf("the second open returned %+v; want the same session at generation 1", again)
		}
	})
	if read := b.state(lifeA); read.Open == nil || read.Open.ID != id || read.Generation != 1 || read.ExitRecorded {
		t.Fatalf("the state read = %+v", read)
	}
}

func TestOperatingSession_Open_SetupPendingAndUnreadableAreToldApart(t *testing.T) {
	t.Run("no default logbook", func(t *testing.T) {
		b := newSessBed(t, false)
		b.unchanged("an open with no default logbook", func() {
			st := b.open(lifeA, 0, SessionSetupPending)
			if st.Open != nil || st.Generation != 0 {
				t.Fatalf("state = %+v; want no session at generation 0", st)
			}
		})
	})
	t.Run("the default names a deleted logbook", func(t *testing.T) {
		b := newSessBed(t, true)
		execT(t, b.svc, `UPDATE logbook SET deleted_at = datetime('now') WHERE id = ?`, b.x)
		b.unchanged("an open with a deleted default", func() { b.open(lifeA, 0, SessionSetupPending) })
	})
	t.Run("no identity row is an error, not setup pending", func(t *testing.T) {
		svc, _ := testMigratedFileService(t)
		_, got, err := svc.OpenOperatingSessionWithContext(context.Background(), lifeA, at(0))
		if err == nil {
			t.Fatalf("a file with no identity row answered %q; want an error", got)
		}
		if n := countT(t, svc, `SELECT COUNT(*) FROM operating_session`); n != 0 {
			t.Fatalf("%d session(s) were opened in a file with no identity", n)
		}
	})
}

func TestOperatingSession_Open_AnotherLifetimesSession(t *testing.T) {
	t.Run("with a member: recovery pending, nothing changes", func(t *testing.T) {
		b := newSessBed(t, true)
		id := b.open(lifeA, 0, SessionApplied).Open.ID
		b.log(b.x, lifeA, 1)
		b.unchanged("an open by another lifetime", func() {
			st := b.open(lifeB, 10, SessionRecoveryPending)
			if st.Open == nil || st.Open.ID != id || st.Open.Holder != lifeA || !st.Open.HasMembers {
				t.Fatalf("state = %+v; want the earlier lifetime's session, still its own", st.Open)
			}
		})
	})
	t.Run("whose only member is a deleted QSO: not empty", func(t *testing.T) {
		b := newSessBed(t, true)
		b.open(lifeA, 0, SessionApplied)
		qso, _ := b.log(b.x, lifeA, 1)
		execT(t, b.svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, qso)
		b.unchanged("an open by another lifetime", func() { b.open(lifeB, 10, SessionRecoveryPending) })
	})
	t.Run("with no member: discarded, and a new one opened, in one commit", func(t *testing.T) {
		b := newSessBed(t, true)
		old := b.open(lifeA, 0, SessionApplied).Open.ID
		st := b.open(lifeB, 10, SessionApplied)
		id := wantOpen(t, st, b.xUUID, lifeB, 1, 10, 10, false)
		if id == old || b.session(old).exists || len(b.intervals(old)) != 0 {
			t.Fatalf("the empty remnant %s was kept (new session %s)", old, id)
		}
		if st.Generation != 2 {
			t.Fatalf("generation %d after a discard and an open in one transaction, want 2", st.Generation)
		}
	})
}

func TestOperatingSession_Open_RetiredComesFirst(t *testing.T) {
	b := newSessBed(t, true)
	b.exit(lifeA, 0, false, 0, SessionExitRecorded)
	b.unchanged("an open by a retired lifetime", func() {
		if st := b.open(lifeA, 1, SessionRetired); st.Open != nil {
			t.Fatalf("a retired lifetime was given %+v", st.Open)
		}
	})

	// A later lifetime's session at an earlier default is not touched by a
	// retired caller: the default-epoch test runs only for a lifetime that
	// is not retired.
	id := b.open(lifeB, 2, SessionApplied).Open.ID
	b.log(b.x, lifeB, 3)
	b.setDefault(b.y)
	b.unchanged("an open by a retired lifetime after the default moved", func() { b.open(lifeA, 4, SessionRetired) })
	if r := b.session(id); r.closedAt.Valid {
		t.Fatalf("the retired caller closed the other lifetime's session: %+v", r)
	}
}

func TestOperatingSession_Open_TheDefaultEpochTestRunsBeforeTheTable(t *testing.T) {
	t.Run("held by the same lifetime, with a member", func(t *testing.T) {
		b := newSessBed(t, true)
		old := b.open(lifeA, 0, SessionApplied).Open.ID
		qso, _ := b.log(b.x, lifeA, 1)
		b.setDefault(b.y)
		st := b.open(lifeA, 10, SessionApplied)
		id := wantOpen(t, st, b.yUUID, lifeA, 2, 10, 10, false)
		if id == old || st.Generation != 2 {
			t.Fatalf("session %s at generation %d; want a new session at generation 2", id, st.Generation)
		}
		b.wantClosed(old, SessionClosedByDefault, 10)
		b.wantIntervals(old, "1 confirmed "+lifeA+" 0 10")
		if m := b.members(old); !reflect.DeepEqual(m, []int64{qso}) {
			t.Fatalf("the closed session's members = %v, want [%d]", m, qso)
		}
	})
	t.Run("held by an earlier lifetime's remnant: confirmed at the closing time, as ruled", func(t *testing.T) {
		b := newSessBed(t, true)
		old := b.open(lifeA, 0, SessionApplied).Open.ID
		b.log(b.x, lifeA, 1)
		b.setDefault(b.y)
		st := b.open(lifeB, 30, SessionApplied)
		wantOpen(t, st, b.yUUID, lifeB, 2, 30, 30, false)
		b.wantClosed(old, SessionClosedByDefault, 30)
		b.wantIntervals(old, "1 confirmed "+lifeA+" 0 30")
	})
}

// ---- New ----

func TestOperatingSession_New_ByTheHolder(t *testing.T) {
	t.Run("a session with a member is kept", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		old := st.Open.ID
		qso, joined := b.log(b.x, lifeA, 1)
		if joined != old {
			t.Fatalf("the contact joined %q, want %s", joined, old)
		}
		st = b.newSession(lifeA, st.Generation, 10, SessionApplied)
		id := wantOpen(t, st, b.xUUID, lifeA, 1, 10, 10, false)
		if id == old || st.Generation != 2 {
			t.Fatalf("session %s at generation %d; want a new session at generation 2", id, st.Generation)
		}
		b.wantClosed(old, SessionClosedByNew, 10)
		b.wantIntervals(old, "1 confirmed "+lifeA+" 0 10")
		b.wantIntervals(id, "1 open "+lifeA+" 10 -")
		if m := b.members(old); !reflect.DeepEqual(m, []int64{qso}) || len(b.members(id)) != 0 {
			t.Fatalf("members moved: old %v, new %v", m, b.members(id))
		}
	})
	t.Run("an empty session is discarded", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		old := st.Open.ID
		st = b.newSession(lifeA, st.Generation, 10, SessionApplied)
		if b.session(old).exists || len(b.intervals(old)) != 0 || st.Generation != 2 {
			t.Fatalf("the empty session was kept, or the generation is %d (want 2)", st.Generation)
		}
		if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 1 {
			t.Fatalf("%d sessions in the file, want 1", n)
		}
	})
	t.Run("a session whose only member is deleted is kept", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		old := st.Open.ID
		qso, _ := b.log(b.x, lifeA, 1)
		execT(t, b.svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, qso)
		b.newSession(lifeA, st.Generation, 10, SessionApplied)
		b.wantClosed(old, SessionClosedByNew, 10)
	})
}

func TestOperatingSession_New_AsTheRecoveryAnswer(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	old := st.Open.ID
	b.log(b.x, lifeA, 1)
	st = b.newSession(lifeB, st.Generation, 10, SessionApplied)
	wantOpen(t, st, b.xUUID, lifeB, 1, 10, 10, false)
	b.wantClosed(old, SessionClosedUnconfirmed, 10)
	b.wantIntervals(old, "1 unconfirmed "+lifeA+" 0 -")
}

func TestOperatingSession_New_StaleAndRetried(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	b.log(b.x, lifeA, 1)
	b.unchanged("a New at an old generation", func() {
		got := b.newSession(lifeA, st.Generation-1, 5, SessionStale)
		if got.Generation != st.Generation || got.Open == nil || got.Open.ID != st.Open.ID {
			t.Fatalf("the refusal returned %+v; want the current state", got)
		}
	})
	first := b.newSession(lifeA, st.Generation, 10, SessionApplied)
	// The same press again, its answer having been lost: refused, and no
	// second session is opened.
	b.unchanged("the retried New", func() {
		got := b.newSession(lifeA, st.Generation, 11, SessionStale)
		if got.Open == nil || got.Open.ID != first.Open.ID {
			t.Fatalf("the retry returned %+v; want the session the first press opened", got.Open)
		}
	})
}

// A refused control whose default-epoch test closed a session: the close is
// stored and counted once, and the control contributes nothing (codex-184).
func TestOperatingSession_ARefusedControlKeepsTheDefaultEpochTestsClose(t *testing.T) {
	for _, withMember := range []bool{true, false} {
		t.Run(fmt.Sprintf("member %v", withMember), func(t *testing.T) {
			b := newSessBed(t, true)
			st := b.open(lifeA, 0, SessionApplied)
			old := st.Open.ID
			if withMember {
				b.log(b.x, lifeA, 1)
			}
			b.setDefault(b.y)

			got := b.newSession(lifeA, st.Generation, 10, SessionStale)
			if got.Open != nil || got.Generation != st.Generation+1 {
				t.Fatalf("after the refusal: %+v; want no session open at generation %d", got, st.Generation+1)
			}
			if withMember {
				b.wantClosed(old, SessionClosedByDefault, 10)
				b.wantIntervals(old, "1 confirmed "+lifeA+" 0 10")
			} else if b.session(old).exists {
				t.Fatal("the empty session was kept")
			}
			if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`); n != 0 {
				t.Fatalf("the refused New opened %d session(s)", n)
			}
			b.unchanged("the same press repeated", func() { b.newSession(lifeA, st.Generation, 11, SessionStale) })

			// At the generation the refusal returned, with no session open,
			// New opens one for the new default.
			next := b.newSession(lifeA, got.Generation, 12, SessionApplied)
			wantOpen(t, next, b.yUUID, lifeA, 2, 12, 12, false)
		})
	}
}

func TestOperatingSession_New_WithNoDefaultLogbook(t *testing.T) {
	b := newSessBed(t, false)
	b.unchanged("a New with no default logbook", func() { b.newSession(lifeA, 0, 0, SessionSetupPending) })
}

// ---- Continue ----

func TestOperatingSession_Continue_AsTheRecoveryAnswer(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	id := st.Open.ID
	qso, _ := b.log(b.x, lifeA, 1)
	st = b.continueSession(lifeB, st.Generation, id, 10, SessionApplied)
	if got := wantOpen(t, st, b.xUUID, lifeB, 1, 0, 10, true); got != id || st.Generation != 2 {
		t.Fatalf("session %s at generation %d; want the same session %s at generation 2", got, st.Generation, id)
	}
	b.wantIntervals(id, "1 unconfirmed "+lifeA+" 0 -", "2 open "+lifeB+" 10 -")
	if m := b.members(id); !reflect.DeepEqual(m, []int64{qso}) {
		t.Fatalf("members = %v, want [%d]", m, qso)
	}
}

// continueBed leaves S0 closed with one contact and S1 open, both of
// logbook X and both held by lifeA. It returns their ids and the state.
func continueBed(t *testing.T) (b *sessBed, s0, s1 string, q0 int64, st OperatingSessionState) {
	t.Helper()
	b = newSessBed(t, true)
	st = b.open(lifeA, 0, SessionApplied)
	s0 = st.Open.ID
	q0, _ = b.log(b.x, lifeA, 1)
	st = b.newSession(lifeA, st.Generation, 10, SessionApplied)
	return b, s0, st.Open.ID, q0, st
}

func TestOperatingSession_Continue_AClosedSession(t *testing.T) {
	t.Run("the current session has a contact and is kept", func(t *testing.T) {
		b, s0, s1, q0, st := continueBed(t)
		q1, _ := b.log(b.x, lifeA, 11)
		st = b.continueSession(lifeA, st.Generation, s0, 20, SessionApplied)
		if got := wantOpen(t, st, b.xUUID, lifeA, 1, 0, 20, true); got != s0 || st.Generation != 3 {
			t.Fatalf("session %s at generation %d; want %s at generation 3", got, st.Generation, s0)
		}
		b.wantIntervals(s0, "1 confirmed "+lifeA+" 0 10", "2 open "+lifeA+" 20 -")
		b.wantClosed(s1, SessionClosedByContinue, 20)
		b.wantIntervals(s1, "1 confirmed "+lifeA+" 10 20")
		if m0, m1 := b.members(s0), b.members(s1); !reflect.DeepEqual(m0, []int64{q0}) || !reflect.DeepEqual(m1, []int64{q1}) {
			t.Fatalf("contacts moved: S0 %v, S1 %v", m0, m1)
		}
	})
	t.Run("the current session is empty and is discarded", func(t *testing.T) {
		b, s0, s1, _, st := continueBed(t)
		st = b.continueSession(lifeA, st.Generation, s0, 20, SessionApplied)
		if b.session(s1).exists || st.Open.ID != s0 {
			t.Fatalf("the empty current session was kept, or %s is open", st.Open.ID)
		}
	})
}

func TestOperatingSession_Continue_Refusals(t *testing.T) {
	t.Run("a session that does not exist", func(t *testing.T) {
		b, _, _, _, st := continueBed(t)
		b.unchanged("the Continue", func() {
			b.continueSession(lifeA, st.Generation, "01920000-0000-7000-8000-0000000000ff", 20, SessionIneligible)
		})
	})
	t.Run("the current session itself", func(t *testing.T) {
		b, _, s1, _, st := continueBed(t)
		b.unchanged("the Continue", func() { b.continueSession(lifeA, st.Generation, s1, 20, SessionIneligible) })
	})
	t.Run("a session of another logbook", func(t *testing.T) {
		b, s0, _, _, _ := continueBed(t)
		b.setDefault(b.y)
		st := b.open(lifeA, 15, SessionApplied) // the session for Y
		b.unchanged("the Continue", func() { b.continueSession(lifeA, st.Generation, s0, 20, SessionIneligible) })
	})
	t.Run("an old generation", func(t *testing.T) {
		b, s0, _, _, st := continueBed(t)
		b.unchanged("the Continue", func() { b.continueSession(lifeA, st.Generation-1, s0, 20, SessionStale) })
	})
	t.Run("a closed session while a remnant awaits its answer", func(t *testing.T) {
		b, s0, _, _, st := continueBed(t)
		b.log(b.x, lifeA, 11) // the open session now has a contact and is another lifetime's
		b.unchanged("the Continue", func() { b.continueSession(lifeB, st.Generation, s0, 20, SessionRecoveryPending) })
	})
}

// ADR 0100 §8's acceptance: continue S0 after the default went X to Y to X,
// then log a contact: S0 stays open and receives it.
func TestOperatingSession_Continue_RebindsTheDefaultEpoch(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	s0 := st.Open.ID
	b.log(b.x, lifeA, 1)
	b.setDefault(b.y)
	b.open(lifeA, 10, SessionApplied) // S0 closes; a session for Y opens
	b.setDefault(b.x)
	st = b.open(lifeA, 20, SessionApplied) // that one is empty and goes; a session for X opens
	if r := b.session(s0); r.epoch != 1 || r.reason != SessionClosedByDefault {
		t.Fatalf("S0 before the Continue = %+v; want closed by the default change at epoch 1", r)
	}

	st = b.continueSession(lifeA, st.Generation, s0, 30, SessionApplied)
	if st.Open.ID != s0 || st.Open.DefaultEpoch != 3 || b.session(s0).epoch != 3 {
		t.Fatalf("S0 after the Continue: %+v, stored epoch %d; want epoch 3", st.Open, b.session(s0).epoch)
	}
	qso, joined := b.log(b.x, lifeA, 31)
	if joined != s0 || b.session(s0).closedAt.Valid {
		t.Fatalf("the contact joined %q and S0 is %+v; want S0, still open", joined, b.session(s0))
	}
	if m := b.members(s0); len(m) != 2 || m[1] != qso {
		t.Fatalf("S0's members = %v", m)
	}
}

// ---- The close for an archive switch ----

func TestOperatingSession_CloseForSwitch(t *testing.T) {
	t.Run("at the request: confirmed, whoever holds it", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		id := st.Open.ID
		b.log(b.x, lifeA, 1)
		st = b.closeForSwitch(false, 10, SessionApplied)
		if st.Open != nil || st.Generation != 2 {
			t.Fatalf("state = %+v; want no session at generation 2", st)
		}
		b.wantClosed(id, SessionClosedBySwitch, 10)
		b.wantIntervals(id, "1 confirmed "+lifeA+" 0 10")
	})
	t.Run("at an arrival: unconfirmed, no time", func(t *testing.T) {
		b := newSessBed(t, true)
		id := b.open(lifeA, 0, SessionApplied).Open.ID
		b.log(b.x, lifeA, 1)
		b.closeForSwitch(true, 10, SessionApplied)
		b.wantClosed(id, SessionClosedBySwitch, 10)
		b.wantIntervals(id, "1 unconfirmed "+lifeA+" 0 -")
	})
	t.Run("an empty session is discarded", func(t *testing.T) {
		b := newSessBed(t, true)
		id := b.open(lifeA, 0, SessionApplied).Open.ID
		st := b.closeForSwitch(false, 10, SessionApplied)
		if b.session(id).exists || st.Generation != 2 {
			t.Fatalf("the empty session was kept, or the generation is %d (want 2)", st.Generation)
		}
	})
	t.Run("with none open nothing changes", func(t *testing.T) {
		b := newSessBed(t, true)
		b.unchanged("the close", func() { b.closeForSwitch(false, 10, SessionUnchanged) })
	})
}

// ---- The exit of a lifetime (brief, D) ----

func TestOperatingSession_Exit_OfTheCurrentLifetime(t *testing.T) {
	t.Run("it holds the open session, with a member", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		id := st.Open.ID
		b.log(b.x, lifeA, 1)
		st = b.exit(lifeA, st.Generation, true, 10, SessionApplied)
		if st.Open != nil || st.Generation != 2 || !b.exitRecorded(lifeA) {
			t.Fatalf("state = %+v, exit recorded %v; want no session, generation 2, the exit recorded", st, b.exitRecorded(lifeA))
		}
		b.wantClosed(id, SessionClosedByExit, 10)
		b.wantIntervals(id, "1 confirmed "+lifeA+" 0 10")
		if read := b.state(lifeA); !read.ExitRecorded {
			t.Fatal("the state read does not say the exit is recorded")
		}
	})
	t.Run("it holds the open session, empty", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		id := st.Open.ID
		st = b.exit(lifeA, st.Generation, true, 10, SessionApplied)
		if b.session(id).exists || st.Generation != 2 || !b.exitRecorded(lifeA) {
			t.Fatalf("the empty session was kept, the generation is %d, or the exit is not recorded", st.Generation)
		}
	})
	t.Run("a stale generation", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.unchanged("the exit", func() { b.exit(lifeA, st.Generation+7, true, 10, SessionStale) })
		if b.exitRecorded(lifeA) {
			t.Fatal("a stale exit was recorded")
		}
	})
	t.Run("it holds none: another lifetime's session awaits its answer", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		id := st.Open.ID
		b.log(b.x, lifeA, 1)
		before := b.intervals(id)
		st = b.exit(lifeB, st.Generation, true, 10, SessionExitRecorded)
		if !b.exitRecorded(lifeB) || st.Generation != 1 || st.Open == nil || st.Open.ID != id {
			t.Fatalf("state = %+v; want the exit recorded, generation 1, the remnant still open", st)
		}
		if after := b.intervals(id); !reflect.DeepEqual(before, after) || b.session(id).closedAt.Valid {
			t.Fatalf("the remnant changed: %v to %v", before, after)
		}
	})
	t.Run("it holds none: setup pending", func(t *testing.T) {
		b := newSessBed(t, false)
		st := b.exit(lifeA, 0, true, 10, SessionExitRecorded)
		if !b.exitRecorded(lifeA) || st.Generation != 0 {
			t.Fatalf("exit recorded %v at generation %d; want recorded at 0", b.exitRecorded(lifeA), st.Generation)
		}
	})
	t.Run("the default-epoch test closed its own session: refused as stale", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		id := st.Open.ID
		b.log(b.x, lifeA, 1)
		b.setDefault(b.y)
		got := b.exit(lifeA, st.Generation, true, 10, SessionStale)
		if b.exitRecorded(lifeA) || got.Generation != st.Generation+1 || got.Open != nil {
			t.Fatalf("exit recorded %v, state %+v; want not recorded, generation %d, no session", b.exitRecorded(lifeA), got, st.Generation+1)
		}
		b.wantClosed(id, SessionClosedByDefault, 10)
		// At the generation returned it holds none, and the exit is recorded.
		b.exit(lifeA, got.Generation, true, 11, SessionExitRecorded)
		if !b.exitRecorded(lifeA) {
			t.Fatal("the second report was not recorded")
		}
	})
}

func TestOperatingSession_Exit_AlreadyRecorded(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	b.log(b.x, lifeA, 1)
	b.exit(lifeA, st.Generation, true, 10, SessionApplied)
	other := b.open(lifeB, 20, SessionApplied)
	b.log(b.x, lifeB, 21)
	b.setDefault(b.y) // a default-epoch test would now close the other lifetime's session
	for _, current := range []bool{true, false} {
		for _, generation := range []int64{0, st.Generation, other.Generation, 99} {
			b.unchanged("a repeated exit report", func() {
				b.exit(lifeA, generation, current, 30, SessionExitAlreadyRecorded)
			})
		}
	}
}

func TestOperatingSession_Exit_OfALifetimeThatIsNotCurrent(t *testing.T) {
	t.Run("it holds the open session: refused, terminal", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.unchanged("the exit", func() { b.exit(lifeA, st.Generation, false, 10, SessionNotCurrent) })
	})
	t.Run("it holds none: recorded, whatever generation, and no session is touched", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		id := st.Open.ID
		b.log(b.x, lifeA, 1)
		b.setDefault(b.y) // lifeA's session is now of an earlier default; this exit must not close it
		before := b.records()
		got := b.exit(lifeB, 99, false, 10, SessionExitRecorded)
		if !b.exitRecorded(lifeB) || got.Generation != st.Generation || b.session(id).closedAt.Valid {
			t.Fatalf("exit recorded %v, generation %d, session %+v; want recorded, generation %d, the session untouched",
				b.exitRecorded(lifeB), got.Generation, b.session(id), st.Generation)
		}
		after := b.records()
		if len(after) != len(before)+1 {
			t.Fatalf("the exit changed more than its own row:\n before %v\n after  %v", before, after)
		}
	})
}

// An exit of L and an open for L, in either order, end with L recorded as
// exited and holding no open session.
func TestOperatingSession_ExitAndOpenOfOneLifetime_EitherOrder(t *testing.T) {
	t.Run("open, then exit", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.exit(lifeA, st.Generation, true, 1, SessionApplied)
		if !b.exitRecorded(lifeA) || b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`) != 0 {
			t.Fatal("the lifetime is not recorded as exited, or a session is still open")
		}
	})
	t.Run("exit, then open", func(t *testing.T) {
		b := newSessBed(t, true)
		b.exit(lifeA, 0, true, 0, SessionExitRecorded)
		b.open(lifeA, 1, SessionRetired)
		if !b.exitRecorded(lifeA) || b.count(`SELECT COUNT(*) FROM operating_session`) != 0 {
			t.Fatal("the lifetime is not recorded as exited, or a session exists")
		}
	})
}

// ---- The logging step ----

func TestOperatingSession_LoggingStep_JoinsTheOpenSession(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	b.unchanged("nothing but a member row and the QSO", func() {}) // the bed is quiet before the contact
	qso, joined := b.log(b.x, lifeB, 1)                            // a contact while another lifetime is asking: it joins the open session
	if joined != st.Open.ID || !reflect.DeepEqual(b.members(st.Open.ID), []int64{qso}) || !b.stored(qso) {
		t.Fatalf("joined %q, members %v, stored %v", joined, b.members(st.Open.ID), b.stored(qso))
	}
	if g := b.generation(); g != 1 {
		t.Fatalf("a contact joining an open session moved the generation to %d", g)
	}
	if lb := b.text(`SELECT l.uuid FROM qso q JOIN logbook l ON l.id = q.logbook_id WHERE q.id = ?`, qso); lb != b.session(joined).logbook {
		t.Fatalf("the contact's logbook is %s and its session's is %s", lb, b.session(joined).logbook)
	}
	if read := b.state(lifeA); read.Open == nil || !read.Open.HasMembers || read.Open.Holder != lifeA {
		t.Fatalf("the state after the contact = %+v", read.Open)
	}
}

func TestOperatingSession_LoggingStep_TheSafetyNet(t *testing.T) {
	for _, holder := range []string{lifeA, ""} {
		t.Run(fmt.Sprintf("holder %q", holder), func(t *testing.T) {
			b := newSessBed(t, true)
			qso, joined := b.log(b.x, holder, 3)
			st := b.state(holder)
			if id := wantOpen(t, st, b.xUUID, holder, 1, 3, 3, true); id != joined || st.Generation != 1 {
				t.Fatalf("joined %q, open %s, generation %d", joined, id, st.Generation)
			}
			if !reflect.DeepEqual(b.members(joined), []int64{qso}) || !b.stored(qso) {
				t.Fatalf("members %v, stored %v", b.members(joined), b.stored(qso))
			}
		})
	}
}

func TestOperatingSession_LoggingStep_RunsTheDefaultEpochTest(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	old := st.Open.ID
	b.log(b.x, lifeA, 1)
	b.setDefault(b.y)
	qso, joined := b.log(b.y, lifeA, 10)
	b.wantClosed(old, SessionClosedByDefault, 10)
	now := b.state(lifeA)
	if id := wantOpen(t, now, b.yUUID, lifeA, 2, 10, 10, true); id != joined || id == old {
		t.Fatalf("joined %q, open %s", joined, id)
	}
	if now.Generation != st.Generation+1 {
		t.Fatalf("a close and an open in one logging transaction moved the generation from %d to %d, want one advance", st.Generation, now.Generation)
	}
	if !reflect.DeepEqual(b.members(joined), []int64{qso}) {
		t.Fatalf("members = %v", b.members(joined))
	}
}

func TestOperatingSession_LoggingStep_RollsBackWithItsQSO(t *testing.T) {
	b := newSessBed(t, true)
	ctx := context.Background()
	before := b.records()
	tx, cancel, err := b.svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer cancel()
	qso, joined := b.logIn(ctx, tx, b.x, lifeA, 1) // the safety net opens a session inside the transaction
	if joined == "" {
		t.Fatal("the step joined nothing; the rollback would prove nothing")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if after := b.records(); !reflect.DeepEqual(before, after) {
		t.Fatalf("the rolled-back step left something:\n before %v\n after  %v", before, after)
	}
	if b.count(`SELECT COUNT(*) FROM qso WHERE id = ?`, qso) != 0 {
		t.Fatal("the QSO outlived its rollback")
	}
}

func TestOperatingSession_LoggingStep_AFailureIsReported_AndTheRollbackTakesTheQSO(t *testing.T) {
	b := newSessBed(t, true)
	b.open(lifeA, 0, SessionApplied)
	ctx := context.Background()
	tx, cancel, err := b.svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer cancel()
	qso, _ := b.logIn(ctx, tx, b.x, lifeA, 1)
	// The step again for the same QSO: its member row cannot be written twice.
	if _, err := b.svc.JoinOperatingSessionTx(ctx, tx, qso, lifeA, at(1)); err == nil {
		t.Fatal("a second member row for one QSO was not reported")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if b.count(`SELECT COUNT(*) FROM qso WHERE id = ?`, qso)+b.count(`SELECT COUNT(*) FROM operating_session_member`) != 0 {
		t.Fatal("the QSO or a member row outlived the rollback")
	}

	tx2, cancel2, err := b.svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer cancel2()
	defer func() { _ = tx2.Rollback() }()
	if _, err := b.svc.JoinOperatingSessionTx(ctx, tx2, 987654, lifeA, at(2)); err == nil {
		t.Fatal("the step accepted a QSO that is not in the file")
	}
}

// A QSO whose logbook is not the file's default never joins, and the step
// changes no session for it (ADR 0100 §2; codex-185). The QSO itself commits.
func TestOperatingSession_LoggingStep_AQSOOutsideTheDefaultLogbook(t *testing.T) {
	t.Run("with no session open, none is opened", func(t *testing.T) {
		b := newSessBed(t, true)
		var qso int64
		var joined string
		b.unchanged("the step", func() { qso, joined = b.log(b.y, lifeA, 1) })
		if joined != "" || !b.stored(qso) {
			t.Fatalf("joined %q, stored %v; want no session and the QSO stored", joined, b.stored(qso))
		}
	})
	t.Run("with a session open, it is left alone", func(t *testing.T) {
		b := newSessBed(t, true)
		b.open(lifeA, 0, SessionApplied)
		var qso int64
		var joined string
		b.unchanged("the step", func() { qso, joined = b.log(b.y, lifeA, 1) })
		if joined != "" || !b.stored(qso) {
			t.Fatalf("joined %q, stored %v", joined, b.stored(qso))
		}
	})
	t.Run("with a session that the default-epoch test would close, the test is not run", func(t *testing.T) {
		b := newSessBed(t, true)
		id := b.open(lifeA, 0, SessionApplied).Open.ID
		b.log(b.x, lifeA, 1)
		b.setDefault(b.y) // X is no longer the default; the session of X awaits the test
		var qso int64
		var joined string
		b.unchanged("the step", func() { qso, joined = b.log(b.x, lifeA, 5) })
		if joined != "" || !b.stored(qso) || b.session(id).closedAt.Valid {
			t.Fatalf("joined %q, stored %v, session %+v", joined, b.stored(qso), b.session(id))
		}
	})
	t.Run("with no default logbook", func(t *testing.T) {
		b := newSessBed(t, false)
		var qso int64
		var joined string
		b.unchanged("the step", func() { qso, joined = b.log(b.x, lifeA, 1) })
		if joined != "" || !b.stored(qso) {
			t.Fatalf("joined %q, stored %v", joined, b.stored(qso))
		}
	})
}

// ---- The generation (group C) ----

func TestOperatingSession_TheGenerationOnlyClimbs(t *testing.T) {
	b := newSessBed(t, true)
	seen := []int{b.generation()}
	step := func(what string, moves bool, call func()) {
		t.Helper()
		call()
		g, last := b.generation(), seen[len(seen)-1]
		if moves && g != last+1 {
			t.Fatalf("%s: generation %d to %d, want one advance", what, last, g)
		}
		if !moves && g != last {
			t.Fatalf("%s: generation %d to %d, want it unchanged", what, last, g)
		}
		seen = append(seen, g)
	}
	var st OperatingSessionState
	step("an open", true, func() { st = b.open(lifeA, 0, SessionApplied) })
	step("a read", false, func() { b.state(lifeA) })
	step("a second open", false, func() { b.open(lifeA, 1, SessionUnchanged) })
	step("a New that discards", true, func() { st = b.newSession(lifeA, st.Generation, 2, SessionApplied) })
	step("a contact joining", false, func() { b.log(b.x, lifeA, 3) })
	step("a stale New", false, func() { b.newSession(lifeA, st.Generation-1, 4, SessionStale) })
	step("a New that keeps", true, func() { st = b.newSession(lifeA, st.Generation, 5, SessionApplied) })
	step("an exit of a lifetime holding none", false, func() { b.exit(lifeB, 0, false, 6, SessionExitRecorded) })
	step("a close for a switch that discards", true, func() { st = b.closeForSwitch(false, 7, SessionApplied) })
	step("a close with none open", false, func() { b.closeForSwitch(false, 8, SessionUnchanged) })
	step("the safety net", true, func() { b.log(b.x, lifeA, 9) })
	if !sort.IntsAreSorted(seen) {
		t.Fatalf("the generation went back: %v", seen)
	}
}

// ---- The state is one snapshot (group E) ----

// While News commit on another connection, every state read must belong to
// one generation: the session open at generation g was opened at minute g.
// The writer goes on until the reader has read enough and the reader until the
// writer has committed enough, so neither count depends on how the two happen
// to be scheduled.
func TestOperatingSession_TheStateReadIsOneSnapshot(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 1, SessionApplied) // generation 1, opened at minute 1
	const wantReads, wantCommits = 200, 60
	var commits, reads atomic.Int64
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		g := st.Generation
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			next, outcome, err := b.svc.NewOperatingSessionWithContext(context.Background(), lifeA, g, at(int(g)+1))
			if err != nil || outcome != SessionApplied {
				done <- fmt.Errorf("New at generation %d: outcome %q, err %v", g, outcome, err)
				return
			}
			g = next.Generation
			commits.Add(1)
		}
	}()
	deadline := time.Now().Add(enough)
	for reads.Load() < wantReads || commits.Load() < wantCommits {
		select {
		case err := <-done:
			t.Fatalf("the writer stopped early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			close(stop)
			t.Fatalf("timed out after %d reads and %d commits", reads.Load(), commits.Load())
		}
		read, err := b.svc.OperatingSessionStateWithContext(context.Background(), lifeA)
		if err != nil {
			close(stop)
			t.Fatalf("read: %v", err)
		}
		reads.Add(1)
		if read.Open == nil || !read.Open.OpenedAt.Equal(at(int(read.Generation))) || !read.Open.IntervalStartedAt.Equal(at(int(read.Generation))) {
			close(stop)
			t.Fatalf("a read mixed two generations: generation %d with %+v", read.Generation, read.Open)
		}
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// ---- A database failure part-way through a control (codex-187) ----

// failAt makes one kind of statement fail from now on, as a database failure
// would: a trigger on the test's own file raises on it. The control under test
// has by then already made its earlier writes in the same transaction.
func (b *sessBed) failAt(when string) {
	b.t.Helper()
	execT(b.t, b.svc, `CREATE TRIGGER injected_failure `+when+` BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
}

const (
	failReopen        = `BEFORE UPDATE OF closed_at ON operating_session WHEN NEW.closed_at IS NULL`
	failClose         = `BEFORE UPDATE OF closed_at ON operating_session WHEN NEW.closed_at IS NOT NULL`
	failDiscard       = `BEFORE DELETE ON operating_session`
	failNewSession    = `BEFORE INSERT ON operating_session`
	failLaterInterval = `BEFORE INSERT ON operating_session_interval WHEN NEW.seq > 1`
	failAdvance       = `BEFORE UPDATE OF session_generation ON archive_metadata WHEN NEW.session_generation <> OLD.session_generation`
)

// failed runs a control that must fail as a database failure, not a refusal,
// and store nothing: every session, interval, member, recorded exit and both
// counters read as they did before it.
func (b *sessBed) failed(what string, call func() (OperatingSessionState, SessionOutcome, error)) {
	b.t.Helper()
	b.unchanged(what, func() {
		st, outcome, err := call()
		if err == nil {
			b.t.Fatalf("%s: outcome %q and no error; want the injected database failure", what, outcome)
		}
		if !strings.Contains(err.Error(), "injected failure") {
			b.t.Fatalf("%s failed with %v; want the injected failure", what, err)
		}
		if outcome != "" || st.Open != nil || st.Generation != 0 {
			b.t.Fatalf("%s returned outcome %q and state %+v beside its error", what, outcome, st)
		}
	})
	b.invariants()
}

func TestOperatingSession_AFailedControlStoresNothing(t *testing.T) {
	ctx := context.Background()

	t.Run("Continue: the reopen fails after the current session was closed", func(t *testing.T) {
		b, s0, s1, _, st := continueBed(t)
		b.log(b.x, lifeA, 11) // the current session has a contact: it would be closed and kept
		b.failAt(failReopen)
		b.failed("the Continue", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.ContinueOperatingSessionWithContext(ctx, lifeA, st.Generation, s0, at(20))
		})
		if cur, target := b.session(s1), b.session(s0); cur.closedAt.Valid || !target.closedAt.Valid {
			t.Fatalf("current %+v, target %+v; want the current still open and the target still closed", cur, target)
		}
	})
	t.Run("Continue: the reopen fails after the empty current session was discarded", func(t *testing.T) {
		b, s0, s1, _, st := continueBed(t)
		b.failAt(failReopen)
		b.failed("the Continue", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.ContinueOperatingSessionWithContext(ctx, lifeA, st.Generation, s0, at(20))
		})
		if cur := b.session(s1); !cur.exists || cur.closedAt.Valid {
			t.Fatalf("the current session = %+v; want it still there and open", cur)
		}
	})
	t.Run("Continue: the new interval fails after the close and the reopen", func(t *testing.T) {
		b, s0, s1, _, st := continueBed(t)
		b.log(b.x, lifeA, 11)
		b.failAt(failLaterInterval)
		b.failed("the Continue", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.ContinueOperatingSessionWithContext(ctx, lifeA, st.Generation, s0, at(20))
		})
		if cur, target := b.session(s1), b.session(s0); cur.closedAt.Valid || !target.closedAt.Valid {
			t.Fatalf("current %+v, target %+v; want the current still open and the target still closed", cur, target)
		}
	})
	t.Run("the recovery Continue: the new interval fails after the interrupted one was ended", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.failAt(failLaterInterval)
		b.failed("the Continue", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.ContinueOperatingSessionWithContext(ctx, lifeB, st.Generation, st.Open.ID, at(10))
		})
		b.wantIntervals(st.Open.ID, "1 open "+lifeA+" 0 -")
	})
	t.Run("the exit: the close fails after the exit was written", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.failAt(failClose)
		b.failed("the exit", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.RecordOperatingAppExitWithContext(ctx, lifeA, st.Generation, true, at(10))
		})
		if b.exitRecorded(lifeA) || b.session(st.Open.ID).closedAt.Valid {
			t.Fatal("the exit's record or its close survived the failure")
		}
		b.wantIntervals(st.Open.ID, "1 open "+lifeA+" 0 -")
	})
	t.Run("the exit: the discard fails after the exit was written", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.failAt(failDiscard)
		b.failed("the exit", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.RecordOperatingAppExitWithContext(ctx, lifeA, st.Generation, true, at(10))
		})
		if b.exitRecorded(lifeA) || !b.session(st.Open.ID).exists {
			t.Fatal("the exit's record survived the failure, or the session is gone")
		}
	})
	t.Run("New: the new session fails after the old one was closed", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.failAt(failNewSession)
		b.failed("the New", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
		})
		if b.session(st.Open.ID).closedAt.Valid {
			t.Fatal("the old session stayed closed after the failure")
		}
	})
	t.Run("New: the generation's advance fails after everything else", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.failAt(failAdvance)
		b.failed("the New", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
		})
	})
	t.Run("Open: the new session fails after an empty remnant was discarded", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.failAt(failNewSession)
		b.failed("the open", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.OpenOperatingSessionWithContext(ctx, lifeB, at(10))
		})
		if !b.session(st.Open.ID).exists {
			t.Fatal("the empty remnant is gone though the open failed")
		}
	})
	t.Run("the close for a switch fails", func(t *testing.T) {
		b := newSessBed(t, true)
		b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.failAt(failClose)
		b.failed("the close", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.CloseOperatingSessionForSwitchWithContext(ctx, false, at(10))
		})
	})
	t.Run("a stale New whose default-epoch close fails", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.setDefault(b.y)
		b.failAt(failClose)
		b.failed("the New", func() (OperatingSessionState, SessionOutcome, error) {
			return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
		})
	})
}

// The logging step's close-and-open, rolled back with its QSO: the default
// moved, so the step closed the session of the earlier default and opened one
// for the new default. After the rollback the earlier session is open as it
// was, and the QSO, its upload row and its member row are gone (codex-187).
func TestOperatingSession_LoggingStep_ACloseAndOpenRollBackWithTheQSO(t *testing.T) {
	b := newSessBed(t, true)
	s0 := b.open(lifeA, 0, SessionApplied).Open.ID
	b.log(b.x, lifeA, 1)
	b.setDefault(b.y)
	before := b.records()

	ctx := context.Background()
	tx, cancel, err := b.svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer cancel()
	qso, joined := b.logIn(ctx, tx, b.y, lifeA, 10)
	var closedInTx int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM operating_session WHERE id = ? AND close_reason = ?`, s0, SessionClosedByDefault).Scan(&closedInTx); err != nil {
		t.Fatal(err)
	}
	if joined == "" || joined == s0 || closedInTx != 1 {
		t.Fatalf("inside the transaction: joined %q, the earlier session closed %d; the rollback would prove nothing", joined, closedInTx)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if after := b.records(); !reflect.DeepEqual(before, after) {
		t.Fatalf("the rolled-back step left something:\n before %v\n after  %v", before, after)
	}
	if r := b.session(s0); r.closedAt.Valid {
		t.Fatalf("the earlier session stayed closed: %+v", r)
	}
	if n := b.count(`SELECT COUNT(*) FROM qso WHERE id = ?`, qso) + b.count(`SELECT COUNT(*) FROM qso_upload WHERE qso_id = ?`, qso) +
		b.count(`SELECT COUNT(*) FROM operating_session_member WHERE qso_id = ?`, qso); n != 0 {
		t.Fatalf("%d row(s) of the rolled-back QSO remain", n)
	}
	b.invariants()
}

// ---- A lifetime whose exit is recorded acts no more ----

// A lifetime at the recovery question exits: it holds no session, so its exit
// is recorded and the generation does not move. A New or a Continue it sent
// before, arriving after, names a generation that is still current. It must be
// refused as retired and touch nothing: otherwise a session would be opened
// for, or handed to, a lifetime that has gone, and no later exit report could
// close it (the clean-room review of 6f2b4e27).
func TestOperatingSession_ARecordedExitFencesNewAndContinue(t *testing.T) {
	recoveryThenExit := func(t *testing.T) (*sessBed, OperatingSessionState) {
		t.Helper()
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		b.open(lifeB, 10, SessionRecoveryPending)
		after := b.exit(lifeB, st.Generation, true, 11, SessionExitRecorded)
		if after.Generation != st.Generation {
			t.Fatalf("the exit moved the generation to %d; the delayed control would be stale anyway and the test would prove nothing", after.Generation)
		}
		return b, st
	}
	t.Run("a delayed New", func(t *testing.T) {
		b, st := recoveryThenExit(t)
		b.unchanged("the New", func() {
			got := b.newSession(lifeB, st.Generation, 12, SessionRetired)
			if got.Open == nil || got.Open.ID != st.Open.ID || got.Open.Holder != lifeA || !got.ExitRecorded {
				t.Fatalf("the refusal returned %+v with %+v; want the other lifetime's session, and the exit recorded", got, got.Open)
			}
		})
	})
	t.Run("a delayed Continue of the remnant", func(t *testing.T) {
		b, st := recoveryThenExit(t)
		b.unchanged("the Continue", func() { b.continueSession(lifeB, st.Generation, st.Open.ID, 12, SessionRetired) })
	})
	t.Run("retired comes before the default-epoch test", func(t *testing.T) {
		b, st := recoveryThenExit(t)
		b.setDefault(b.y) // the other lifetime's session is now of an earlier default
		b.unchanged("the New", func() { b.newSession(lifeB, st.Generation, 12, SessionRetired) })
		b.unchanged("the Continue", func() { b.continueSession(lifeB, st.Generation, st.Open.ID, 12, SessionRetired) })
		if b.session(st.Open.ID).closedAt.Valid {
			t.Fatal("a retired caller closed the other lifetime's session")
		}
	})
	t.Run("after its own session was closed by its exit", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		after := b.exit(lifeA, st.Generation, true, 10, SessionApplied)
		b.unchanged("the New", func() { b.newSession(lifeA, after.Generation, 11, SessionRetired) })
		b.unchanged("the Continue", func() { b.continueSession(lifeA, after.Generation, st.Open.ID, 11, SessionRetired) })
	})
}

// The logging step's holder (codex-189). When the step must open a session, a
// holder whose exit is recorded holds nothing: the session is opened with no
// holder. The QSO, its upload row and its member row are stored as always; the
// step refuses no contact for it. A session that is already open keeps the
// holder it has.
func TestOperatingSession_LoggingStep_AnExitedLifetimeHoldsNothing(t *testing.T) {
	t.Run("no session is open", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		after := b.exit(lifeA, st.Generation, true, 10, SessionApplied)

		qso, joined := b.log(b.x, lifeA, 20)
		now := b.state(lifeA)
		if id := wantOpen(t, now, b.xUUID, "", 1, 20, 20, true); id != joined || now.Generation != after.Generation+1 {
			t.Fatalf("joined %q, open %s, generation %d (want %d)", joined, id, now.Generation, after.Generation+1)
		}
		b.wantIntervals(joined, "1 open - 20 -")
		if !reflect.DeepEqual(b.members(joined), []int64{qso}) || !b.stored(qso) {
			t.Fatalf("members %v, stored %v", b.members(joined), b.stored(qso))
		}
	})
	t.Run("the default-epoch test closed the session that was open", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeB, 0, SessionApplied)
		s0 := st.Open.ID
		b.log(b.x, lifeB, 1)
		b.exit(lifeA, 0, false, 2, SessionExitRecorded) // lifeA holds none; its exit is recorded
		b.setDefault(b.y)

		qso, joined := b.log(b.y, lifeA, 20)
		b.wantClosed(s0, SessionClosedByDefault, 20)
		b.wantIntervals(s0, "1 confirmed "+lifeB+" 0 20")
		now := b.state(lifeA)
		if id := wantOpen(t, now, b.yUUID, "", 2, 20, 20, true); id != joined || id == s0 || now.Generation != st.Generation+1 {
			t.Fatalf("joined %q, open %s, generation %d (want %d)", joined, id, now.Generation, st.Generation+1)
		}
		b.wantIntervals(joined, "1 open - 20 -")
		if !reflect.DeepEqual(b.members(joined), []int64{qso}) || !b.stored(qso) {
			t.Fatalf("members %v, stored %v", b.members(joined), b.stored(qso))
		}
	})
	t.Run("a session is open, held by another lifetime: it keeps its holder", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeB, 0, SessionApplied)
		s0 := st.Open.ID
		b.exit(lifeA, 0, false, 1, SessionExitRecorded)
		before := b.intervals(s0)

		qso, joined := b.log(b.x, lifeA, 20)
		if joined != s0 || !reflect.DeepEqual(b.members(s0), []int64{qso}) || !b.stored(qso) {
			t.Fatalf("joined %q, members %v, stored %v; want the open session %s", joined, b.members(s0), b.stored(qso), s0)
		}
		if after := b.intervals(s0); !reflect.DeepEqual(before, after) || b.generation() != int(st.Generation) {
			t.Fatalf("the open session's intervals %v became %v, generation %d (want %d)", before, after, b.generation(), st.Generation)
		}
	})
	t.Run("a holder with no recorded exit holds the session the step opens", func(t *testing.T) {
		b := newSessBed(t, true)
		b.exit(lifeB, 0, false, 1, SessionExitRecorded) // another lifetime's exit must not matter
		_, joined := b.log(b.x, lifeA, 20)
		b.wantIntervals(joined, "1 open "+lifeA+" 20 -")
	})
}

// ---- The contact count (W-0029 part 4, D) ----

// The recovery question shows how many contacts the session has. The count
// leaves out a contact that has since been deleted; whether the session has
// members at all, which decides whether the question is asked, does not (ruled
// 2026-11-10). A read and a control's returned state give the same count, each
// from its own transaction.
func TestOperatingSession_ContactCount_LeavesOutDeletedContacts(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	if st.Open.ContactCount != 0 || st.Open.HasMembers {
		t.Fatalf("a new session = %+v; want no contacts", st.Open)
	}
	q1, _ := b.log(b.x, lifeA, 1)
	b.log(b.x, lifeA, 2)
	b.log(b.x, lifeA, 3)
	if read := b.state(lifeA); read.Open.ContactCount != 3 || !read.Open.HasMembers {
		t.Fatalf("after three contacts the read = %+v; want a count of 3", read.Open)
	}

	execT(t, b.svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, q1)
	if read := b.state(lifeA); read.Open.ContactCount != 2 || !read.Open.HasMembers {
		t.Fatalf("with one of three deleted the read = %+v; want a count of 2, still with members", read.Open)
	}
	// A control returns the count in its own state: here the recovery answer
	// Continue, by another lifetime.
	g := b.open(lifeB, 10, SessionRecoveryPending).Generation
	if after := b.continueSession(lifeB, g, st.Open.ID, 11, SessionApplied); after.Open.ContactCount != 2 || after.Open.Holder != lifeB {
		t.Fatalf("the state Continue returned = %+v; want a count of 2, held by %s", after.Open, lifeB)
	}

	// Every contact deleted: the count is 0 and the session still has members,
	// so it is still asked about and never discarded.
	execT(t, b.svc, `UPDATE qso SET deleted_at = datetime('now')`)
	if read := b.state(lifeA); read.Open == nil || read.Open.ContactCount != 0 || !read.Open.HasMembers {
		t.Fatalf("with every contact deleted the read = %+v; want a count of 0, still with members", read.Open)
	}
}
