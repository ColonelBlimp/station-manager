package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Contention between the session's controls, the logging transaction and a
// change of the default pointer (ADR 0100 §2 requires the test; W-0029 part 1,
// group F).
//
// A control takes the writer's position with its first statement
// (takeWriterPosition). The logging transaction does the same by writing its
// QSO row first. In production a transaction that meets a holder waits inside
// SQLite's busy handler, which repeats the attempt until the holder is gone
// or the busy limit passes.
//
// That wait cannot be seen from outside, and a test that sleeps and then looks
// can pass with the second transaction not yet begun (codex-187). So the
// forced cases here stand where the busy handler stands. The contender runs on
// a connection whose busy limit is zero, so its first statement is REFUSED at
// once while the holder holds, and the test sees the refusal. Only then does
// the test let the holder commit, and only after that commit does it have the
// contender repeat the same statement, in the same transaction, as the busy
// handler would. Every step is caused by the one before; nothing depends on
// how long anything takes.

// enough is the longest a test waits for something that must happen.
const enough = 20 * time.Second

type contentionRole int

const (
	holderRole contentionRole = iota + 1
	contenderRole
)

type roleKey struct{}

func asRole(r contentionRole) context.Context {
	return context.WithValue(context.Background(), roleKey{}, r)
}

// contention is the script of one forced case: at most one control as the
// holder and one as the contender, told apart by the context each runs under.
type contention struct {
	t *testing.T
	// locked is closed once the holder control has the writer's position;
	// it stays there until release.
	locked  chan struct{}
	release func()
	// refused receives the result of the contender's first attempt at the
	// writer's position; retry lets it attempt again.
	refused chan error
	retry   func()
}

// script installs the seams for one case and removes them when it ends.
func script(t *testing.T) *contention {
	t.Helper()
	c := &contention{t: t, locked: make(chan struct{}), refused: make(chan error, 1)}
	releaseCh, retryCh := make(chan struct{}), make(chan struct{})
	var releaseOnce, retryOnce sync.Once
	c.release = func() { releaseOnce.Do(func() { close(releaseCh) }) }
	c.retry = func() { retryOnce.Do(func() { close(retryCh) }) }

	realTake := takeSessionWriterPosition
	takeSessionWriterPosition = func(s *sessionTx) error {
		switch s.ctx.Value(roleKey{}) {
		case holderRole:
			if err := realTake(s); err != nil {
				return err
			}
			close(c.locked)
			<-releaseCh
			return nil
		case contenderRole:
			err := realTake(s)
			c.refused <- err
			<-retryCh
			if err == nil {
				return nil // it was not refused; the test has already failed for that
			}
			return realTake(s)
		}
		return realTake(s)
	}
	// A contending control gets a busy limit of zero on the connection it
	// pins, before its transaction begins, and the service's own limit back
	// when it is done.
	realSet := setSynchronous
	setSynchronous = func(ctx context.Context, conn *sql.Conn, level string) error {
		if ctx.Value(roleKey{}) == contenderRole {
			limit := 0
			if level != "FULL" {
				limit = busyTimeoutMS
			}
			if _, err := conn.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf(`PRAGMA busy_timeout = %d`, limit)); err != nil {
				return err
			}
		}
		return realSet(ctx, conn, level)
	}
	t.Cleanup(func() {
		c.release()
		c.retry()
		takeSessionWriterPosition = realTake
		setSynchronous = realSet
	})
	return c
}

func await[T any](t *testing.T, what string, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(enough):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

// isBusy is SQLite's refusal of a writer while another holds the writer's
// position (SQLITE_BUSY, 5), and not its stale-snapshot refusal (517).
func isBusy(err error) bool {
	return err != nil && strings.Contains(err.Error(), "database is locked (5)")
}

// wasRefused takes a contender's first attempt and reports whether it was
// refused because the holder held. If it was not, the test is failed and the
// caller lets the contender through first, so that an implementation that does
// not hold the writer's position then meets what it must: a commit behind its
// read.
func (c *contention) wasRefused(what string, refused <-chan error) bool {
	c.t.Helper()
	err := await(c.t, what+" to make its first attempt", refused)
	if isBusy(err) {
		return true
	}
	c.t.Errorf("%s was not refused while the holder held: its first attempt returned %v", what, err)
	return false
}

type controlResult struct {
	st      OperatingSessionState
	outcome SessionOutcome
	err     error
}

// goControl runs a control on its own goroutine, as the holder or the contender.
func goControl(role contentionRole, call func(ctx context.Context) (OperatingSessionState, SessionOutcome, error)) <-chan controlResult {
	return goControlCtx(asRole(role), call)
}

func goControlCtx(ctx context.Context, call func(ctx context.Context) (OperatingSessionState, SessionOutcome, error)) <-chan controlResult {
	ch := make(chan controlResult, 1)
	go func() {
		st, outcome, err := call(ctx)
		ch <- controlResult{st, outcome, err}
	}()
	return ch
}

// wantControl takes a control's result: a database failure fails the test, and
// a refusal is told apart from one by being an outcome with no error.
func wantControl(t *testing.T, what string, ch <-chan controlResult, want SessionOutcome) OperatingSessionState {
	t.Helper()
	r := await(t, what, ch)
	if r.err != nil {
		t.Fatalf("%s failed: %v", what, r.err)
	}
	if r.outcome != want {
		t.Fatalf("%s: outcome %q, want %q", what, r.outcome, want)
	}
	return r.st
}

type logResult struct {
	qso    int64
	joined string
	err    error
}

// heldLogging is a logging transaction with its QSO row, upload row and
// logging step written and not committed: it holds the writer's position.
func (b *sessBed) heldLogging(logbook int64, holder string, minutes int) (*sql.Tx, logResult) {
	b.t.Helper()
	tx, cancel, err := b.svc.BeginTxContext(context.Background())
	if err != nil {
		b.t.Fatalf("begin: %v", err)
	}
	b.t.Cleanup(func() { _ = tx.Rollback(); cancel() })
	qso, joined, err := b.logSteps(context.Background(), tx, logbook, holder, minutes)
	if err != nil {
		b.t.Fatal(err)
	}
	return tx, logResult{qso: qso, joined: joined}
}

// heldPointerChange moves the default pointer with the setters' own statement
// and does not commit: it holds the writer's position.
func (b *sessBed) heldPointerChange(to int64) *sql.Tx {
	b.t.Helper()
	tx, cancel, err := b.svc.BeginTxContext(context.Background())
	if err != nil {
		b.t.Fatalf("begin: %v", err)
	}
	b.t.Cleanup(func() { _ = tx.Rollback(); cancel() })
	if _, err := tx.Exec(fmt.Sprintf(setDefaultPointerSQL, ""), to); err != nil {
		b.t.Fatalf("move the pointer: %v", err)
	}
	return tx
}

// contender is a transaction the test drives on its own connection, whose
// busy limit is zero: its first statement is attempted once, the result sent
// on refused, and attempted again on retry, in the same transaction.
type contender struct {
	refused chan error
	retry   func()
}

// contend runs first on a new transaction of a pinned connection with a busy
// limit of zero. After retry it runs first again if it was refused, then rest,
// and commits.
func (b *sessBed) contend(first func(ctx context.Context, tx *sql.Tx) error, rest func(ctx context.Context, tx *sql.Tx) error) (*contender, <-chan error) {
	c := &contender{refused: make(chan error, 1)}
	retryCh := make(chan struct{})
	var once sync.Once
	c.retry = func() { once.Do(func() { close(retryCh) }) }
	b.t.Cleanup(c.retry)
	done := make(chan error, 1)
	go func() {
		ctx := context.Background()
		conn, err := b.svc.handle.Conn(ctx)
		if err != nil {
			c.refused <- err
			done <- err
			return
		}
		defer func() {
			_, _ = conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout = %d`, busyTimeoutMS))
			_ = conn.Close()
		}()
		if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
			c.refused <- err
			done <- err
			return
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			c.refused <- err
			done <- err
			return
		}
		defer func() { _ = tx.Rollback() }()
		err = first(ctx, tx)
		c.refused <- err
		<-retryCh
		if err != nil {
			err = first(ctx, tx)
		}
		if err == nil && rest != nil {
			err = rest(ctx, tx)
		}
		if err == nil {
			err = tx.Commit()
		}
		done <- err
	}()
	return c, done
}

// contendLogging is a whole logging transaction as a contender: its first
// statement is its QSO row, as on the live paths.
func (b *sessBed) contendLogging(logbook int64, holder string, minutes int) (*contender, <-chan logResult) {
	qso := b.nextQso(logbook)
	var r logResult
	c, done := b.contend(
		func(ctx context.Context, tx *sql.Tx) (err error) {
			r.qso, err = b.svc.InsertQsoTx(ctx, tx, qso)
			return err
		},
		func(ctx context.Context, tx *sql.Tx) (err error) {
			_, r.joined, err = b.logRest(ctx, tx, r.qso, holder, minutes)
			return err
		})
	out := make(chan logResult, 1)
	go func() {
		r.err = <-done
		out <- r
	}()
	return c, out
}

// contendPointerChange moves the default pointer, with the setters' own
// statement, as a contender.
func (b *sessBed) contendPointerChange(to int64) (*contender, <-chan error) {
	return b.contend(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, fmt.Sprintf(setDefaultPointerSQL, ""), to)
		return err
	}, nil)
}

// twoConnections fails the test unless two physical connections are in use:
// the holder's and the contender's.
func (b *sessBed) twoConnections() {
	b.t.Helper()
	if n := b.svc.handle.Stats().InUse; n < 2 {
		b.t.Errorf("%d connection(s) in use; the two transactions are not on separate connections", n)
	}
}

func (b *sessBed) epoch() int {
	b.t.Helper()
	return b.count(`SELECT default_epoch FROM archive_metadata WHERE singleton = 1`)
}

// F1. Logging holds the writer's position when a New starts. The New is
// refused, and when it gets the position after logging's commit it acts on
// what logging stored: the contact is in the session the New closed, which is
// therefore kept.
func TestSessionContention_LoggingFirst_ThenANew(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	s0 := st.Open.ID
	sc := script(t)

	tx, logged := b.heldLogging(b.x, lifeA, 1)
	newDone := goControl(contenderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
		return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
	})
	if sc.wasRefused("the New", sc.refused) {
		b.twoConnections()
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit the logging transaction: %v", err)
	}
	sc.retry()
	after := wantControl(t, "the New", newDone, SessionApplied)

	if logged.joined != s0 || !reflect.DeepEqual(b.members(s0), []int64{logged.qso}) || !b.stored(logged.qso) {
		t.Fatalf("the contact joined %q; members of %s = %v; stored %v", logged.joined, s0, b.members(s0), b.stored(logged.qso))
	}
	b.wantClosed(s0, SessionClosedByNew, 10)
	b.wantIntervals(s0, "1 confirmed "+lifeA+" 0 10")
	id := wantOpen(t, after, b.xUUID, lifeA, 1, 10, 10, false)
	if id == s0 || after.Generation != st.Generation+1 || b.generation() != int(st.Generation)+1 {
		t.Fatalf("new session %s at generation %d (stored %d); want a new one at %d", id, after.Generation, b.generation(), st.Generation+1)
	}
	b.invariants()
}

// F2. A New holds the writer's position when logging starts. Logging is
// refused, and when it gets the position after the New's commit its contact
// joins the session the New opened.
//
// This is also the case that forces the unsafe order on an implementation that
// reads before it writes: the New has read, logging then commits, and the
// New's write comes after that commit.
func TestSessionContention_ANewFirst_ThenLogging(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	s0 := st.Open.ID
	sc := script(t)

	newDone := goControl(holderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
		return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
	})
	await(t, "the New to take the writer's position", sc.locked)
	lg, logDone := b.contendLogging(b.x, lifeA, 11)
	var logged logResult
	if sc.wasRefused("logging", lg.refused) {
		b.twoConnections()
	} else {
		// Not refused: let it commit behind the New's read, then free the New.
		lg.retry()
		logged = await(t, "logging to finish", logDone)
	}
	sc.release()
	after := wantControl(t, "the New", newDone, SessionApplied)
	lg.retry()
	if logged == (logResult{}) {
		logged = await(t, "logging to finish", logDone)
	}
	if logged.err != nil {
		t.Fatalf("logging failed: %v", logged.err)
	}

	id := wantOpen(t, b.state(lifeA), b.xUUID, lifeA, 1, 10, 10, true)
	if id != after.Open.ID || logged.joined != id || !reflect.DeepEqual(b.members(id), []int64{logged.qso}) || !b.stored(logged.qso) {
		t.Fatalf("the New opened %s, the contact joined %q, members %v, stored %v", after.Open.ID, logged.joined, b.members(id), b.stored(logged.qso))
	}
	if b.session(s0).exists {
		t.Fatalf("the session the New closed was empty, and was kept: %+v", b.session(s0))
	}
	b.wantIntervals(id, "1 open "+lifeA+" 10 -")
	if g := b.generation(); g != int(st.Generation)+1 {
		t.Fatalf("generation %d, want %d: the New's advance and none for the contact", g, st.Generation+1)
	}
	b.invariants()
}

// F3. A New refused behind a holder is cancelled before it can try again: it
// reports the cancellation, stores nothing, the holder commits beside it, and
// a third transaction then proceeds.
func TestSessionContention_ACancelledWaiterStoresNothing(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	s0 := st.Open.ID
	sc := script(t)

	tx, logged := b.heldLogging(b.x, lifeA, 1)
	ctx, cancel := context.WithCancel(asRole(contenderRole))
	defer cancel()
	waiter := goControlCtx(ctx, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
		return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
	})
	sc.wasRefused("the New", sc.refused)
	cancel()
	if err := tx.Commit(); err != nil {
		t.Fatalf("the holder could not commit beside a cancelled waiter: %v", err)
	}
	sc.retry()
	r := await(t, "the cancelled New to return", waiter)
	if !stderr.Is(r.err, context.Canceled) {
		t.Fatalf("the cancelled New returned outcome %q and error %v; want the cancellation", r.outcome, r.err)
	}
	if g := b.generation(); g != int(st.Generation) || b.count(`SELECT COUNT(*) FROM operating_session`) != 1 ||
		!reflect.DeepEqual(b.members(s0), []int64{logged.qso}) || b.session(s0).closedAt.Valid {
		t.Fatalf("the cancelled New stored something: generation %d, session %+v, members %v", g, b.session(s0), b.members(s0))
	}

	third := b.newSession(lifeA, st.Generation, 20, SessionApplied)
	b.wantClosed(s0, SessionClosedByNew, 20)
	if third.Generation != st.Generation+1 {
		t.Fatalf("the third transaction committed at generation %d, want %d", third.Generation, st.Generation+1)
	}
}

// F4. A change of the default pointer crossing a session read. The
// default-epoch test sees the change or does not, by the order, never half.
func TestSessionContention_APointerChangeCrossingASessionRead(t *testing.T) {
	t.Run("the pointer change first, then a New: the New sees all of it", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		s0 := st.Open.ID
		b.log(b.x, lifeA, 1)
		sc := script(t)

		ptx := b.heldPointerChange(b.y)
		newDone := goControl(contenderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
			return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
		})
		if sc.wasRefused("the New", sc.refused) {
			b.twoConnections()
		}
		if err := ptx.Commit(); err != nil {
			t.Fatalf("commit the pointer change: %v", err)
		}
		sc.retry()
		after := wantControl(t, "the New", newDone, SessionStale)
		if after.Open != nil || after.Generation != st.Generation+1 || after.DefaultEpoch != 2 {
			t.Fatalf("state = %+v; want no session, generation %d, epoch 2", after, st.Generation+1)
		}
		b.wantClosed(s0, SessionClosedByDefault, 10)
		if n := b.count(`SELECT COUNT(*) FROM operating_session`); n != 1 {
			t.Fatalf("%d sessions in the file; the refused New must have opened none", n)
		}
		b.invariants()
	})
	t.Run("the pointer change first, then logging into the new default", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		s0 := st.Open.ID
		b.log(b.x, lifeA, 1)
		sc := script(t)

		ptx := b.heldPointerChange(b.y)
		lg, logDone := b.contendLogging(b.y, lifeA, 10)
		if sc.wasRefused("logging", lg.refused) {
			b.twoConnections()
		}
		if err := ptx.Commit(); err != nil {
			t.Fatalf("commit the pointer change: %v", err)
		}
		lg.retry()
		logged := await(t, "logging to finish", logDone)
		if logged.err != nil {
			t.Fatalf("logging failed: %v", logged.err)
		}
		b.wantClosed(s0, SessionClosedByDefault, 10)
		now := b.state(lifeA)
		if id := wantOpen(t, now, b.yUUID, lifeA, 2, 10, 10, true); id != logged.joined || !reflect.DeepEqual(b.members(id), []int64{logged.qso}) {
			t.Fatalf("the contact joined %q; open %s; members %v", logged.joined, id, b.members(id))
		}
		if now.Generation != st.Generation+1 || !b.stored(logged.qso) {
			t.Fatalf("generation %d (want %d), stored %v", now.Generation, st.Generation+1, b.stored(logged.qso))
		}
		b.invariants()
	})
	t.Run("a New first, then the pointer change: the New sees none of it", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		sc := script(t)

		newDone := goControl(holderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
			return b.svc.NewOperatingSessionWithContext(ctx, lifeA, st.Generation, at(10))
		})
		await(t, "the New to take the writer's position", sc.locked)
		pc, moved := b.contendPointerChange(b.y)
		if sc.wasRefused("the pointer change", pc.refused) {
			b.twoConnections()
		}
		sc.release()
		after := wantControl(t, "the New", newDone, SessionApplied)
		pc.retry()
		if err := await(t, "the pointer change to finish", moved); err != nil {
			t.Fatalf("the pointer change failed: %v", err)
		}
		// The New committed wholly before the change: its session is of X at epoch 1.
		s1 := wantOpen(t, after, b.xUUID, lifeA, 1, 10, 10, false)
		if after.DefaultEpoch != 1 {
			t.Fatalf("the New returned epoch %d; it committed before the change and must read 1", after.DefaultEpoch)
		}
		if now := b.state(lifeA); now.DefaultEpoch != 2 || now.Open == nil || now.Open.ID != s1 || now.Open.DefaultEpoch != 1 {
			t.Fatalf("after both: %+v with %+v; want the file at epoch 2 and the session still at 1", now, now.Open)
		}
		// The next transition's default-epoch test then closes it.
		next := b.open(lifeA, 20, SessionApplied)
		wantOpen(t, next, b.yUUID, lifeA, 2, 20, 20, false)
		if b.session(s1).exists {
			t.Fatal("the empty session of the earlier default was kept")
		}
	})
	t.Run("logging first, then the pointer change", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		s0 := st.Open.ID
		sc := script(t)

		tx, logged := b.heldLogging(b.x, lifeA, 1)
		pc, moved := b.contendPointerChange(b.y)
		if sc.wasRefused("the pointer change", pc.refused) {
			b.twoConnections()
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit the logging transaction: %v", err)
		}
		pc.retry()
		if err := await(t, "the pointer change to finish", moved); err != nil {
			t.Fatalf("the pointer change failed: %v", err)
		}
		if logged.joined != s0 || !reflect.DeepEqual(b.members(s0), []int64{logged.qso}) || !b.stored(logged.qso) {
			t.Fatalf("the contact joined %q; members %v; stored %v", logged.joined, b.members(s0), b.stored(logged.qso))
		}
		if r := b.session(s0); r.closedAt.Valid || r.epoch != 1 || b.epoch() != 2 || b.generation() != int(st.Generation) {
			t.Fatalf("session %+v, file epoch %d, generation %d; want it open at epoch 1, the file at 2, generation %d",
				r, b.epoch(), b.generation(), st.Generation)
		}
		b.invariants()
	})
}

// F5. An exit of a lifetime against an open for the same lifetime, in both
// orders. Whichever is taken first, the lifetime ends recorded as exited and
// holding no open session, and each answer is an outcome, not a failure.
func TestSessionContention_AnExitAgainstAnOpenOfOneLifetime(t *testing.T) {
	t.Run("the open first: the exit is stale, and its repeat closes what the open made", func(t *testing.T) {
		b := newSessBed(t, true)
		sc := script(t)

		openDone := goControl(holderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
			return b.svc.OpenOperatingSessionWithContext(ctx, lifeA, at(0))
		})
		await(t, "the open to take the writer's position", sc.locked)
		exitDone := goControl(contenderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
			return b.svc.RecordOperatingAppExitWithContext(ctx, lifeA, 0, true, at(1))
		})
		if sc.wasRefused("the exit", sc.refused) {
			b.twoConnections()
		}
		sc.release()
		opened := wantControl(t, "the open", openDone, SessionApplied)
		sc.retry()
		// The exit named the generation from before the open, which the open
		// moved: it is refused as stale and returns the state the open left.
		refused := wantControl(t, "the exit", exitDone, SessionStale)
		if refused.Generation != opened.Generation || refused.Open == nil || refused.Open.ID != opened.Open.ID || b.exitRecorded(lifeA) {
			t.Fatalf("the stale exit returned %+v, exit recorded %v", refused, b.exitRecorded(lifeA))
		}
		b.exit(lifeA, refused.Generation, true, 2, SessionApplied)
		if !b.exitRecorded(lifeA) || b.count(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`) != 0 {
			t.Fatal("the lifetime is not recorded as exited, or still holds a session")
		}
	})
	t.Run("the exit first: the open is retired", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		s0 := st.Open.ID
		b.log(b.x, lifeA, 1)
		sc := script(t)

		exitDone := goControl(holderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
			return b.svc.RecordOperatingAppExitWithContext(ctx, lifeA, st.Generation, true, at(10))
		})
		await(t, "the exit to take the writer's position", sc.locked)
		openDone := goControl(contenderRole, func(ctx context.Context) (OperatingSessionState, SessionOutcome, error) {
			return b.svc.OpenOperatingSessionWithContext(ctx, lifeA, at(11))
		})
		if sc.wasRefused("the open", sc.refused) {
			b.twoConnections()
		}
		sc.release()
		wantControl(t, "the exit", exitDone, SessionApplied)
		sc.retry()
		retired := wantControl(t, "the open", openDone, SessionRetired)
		if retired.Open != nil || !retired.ExitRecorded || !b.exitRecorded(lifeA) {
			t.Fatalf("the retired open returned %+v", retired)
		}
		b.wantClosed(s0, SessionClosedByExit, 10)
		b.invariants()
	})
}

// F9, a supplement to the forced cases: many logging transactions and News at
// once, with nothing held and SQLite's own busy handler doing the waiting. No
// transaction fails; every QSO has exactly one member row; a reader never
// finds two open sessions; and the generation ends at its start plus the
// number of state-changing commits. It does not show that any two of them
// overlapped; the forced cases are the evidence for that.
func TestSessionContention_AMixedRun(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	const (
		loggers, each = 4, 40
		newers, news  = 2, 30
	)
	var (
		wg      sync.WaitGroup
		applied atomic.Int64
		stale   atomic.Int64
		errs    = make(chan error, loggers*each+newers*news*20)
	)
	started := time.Now()
	for l := 0; l < loggers; l++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				ctx := context.Background()
				tx, cancel, err := b.svc.BeginTxContext(ctx)
				if err == nil {
					if _, _, err = b.logSteps(ctx, tx, b.x, lifeA, i); err == nil {
						err = tx.Commit()
					} else {
						_ = tx.Rollback()
					}
					cancel()
				}
				if err != nil {
					errs <- fmt.Errorf("logging: %w", err)
				}
			}
		}()
	}
	for n := 0; n < newers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := st.Generation
			for mine := 0; mine < news; {
				got, outcome, err := b.svc.NewOperatingSessionWithContext(context.Background(), lifeA, g, at(60))
				switch {
				case err != nil:
					errs <- fmt.Errorf("New: %w", err)
					return
				case outcome == SessionApplied:
					applied.Add(1)
					mine++
				case outcome == SessionStale:
					stale.Add(1)
				default:
					errs <- fmt.Errorf("New: outcome %q", outcome)
					return
				}
				g = got.Generation
			}
		}()
	}
	stop := make(chan struct{})
	readerDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				readerDone <- nil
				return
			default:
			}
			var open int
			if err := b.svc.handle.QueryRow(`SELECT COUNT(*) FROM operating_session WHERE closed_at IS NULL`).Scan(&open); err != nil || open > 1 {
				readerDone <- fmt.Errorf("a reader found %d open sessions (err %v)", open, err)
				return
			}
		}
	}()
	wg.Wait()
	close(stop)
	if err := <-readerDone; err != nil {
		t.Error(err)
	}
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}

	qsos := loggers * each
	if n := b.count(`SELECT COUNT(*) FROM qso`); n != qsos {
		t.Fatalf("%d QSOs stored, want %d", n, qsos)
	}
	if n := b.count(`SELECT COUNT(*) FROM qso q WHERE (SELECT COUNT(*) FROM operating_session_member m WHERE m.qso_id = q.id) <> 1`); n != 0 {
		t.Fatalf("%d QSO(s) do not have exactly one member row", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM operating_session_member m WHERE NOT EXISTS (SELECT 1 FROM operating_session s WHERE s.id = m.session_id AND s.logbook_uuid = ?)`, b.xUUID); n != 0 {
		t.Fatalf("%d member row(s) point at no session of the contact's logbook", n)
	}
	if n := b.count(`SELECT COUNT(*) FROM qso_upload`); n != qsos {
		t.Fatalf("%d upload rows stored, want %d", n, qsos)
	}
	if g, want := b.generation(), int(st.Generation+applied.Load()); g != want || applied.Load() != newers*news {
		t.Fatalf("generation %d after %d applied News, want %d", g, applied.Load(), want)
	}
	b.invariants()
	t.Logf("mixed run: %d logging transactions and %d News (%d more refused as stale) in %s, no failure",
		qsos, applied.Load(), stale.Load(), time.Since(started).Round(time.Millisecond))
}

// The measurement behind takeWriterPosition (F7). On two connections of one
// file, every step following from the one before:
//   - a transaction that READS the state it will update, and then writes after
//     another connection has committed, is refused with SQLite's
//     stale-snapshot error, however long it would wait;
//   - a transaction that takes the writer's position first shuts the other
//     connection's write out (refused at once with a busy limit of zero, and
//     after the limit with a limit set), and once it has committed the other's
//     write goes through. Both are counted.
func TestSessionContention_ReadingFirstIsRefused_WritingFirstExcludes(t *testing.T) {
	const bump = `UPDATE archive_metadata SET session_generation = session_generation + 1 WHERE singleton = 1`
	ctx := context.Background()
	twoConns := func(t *testing.T, b *sessBed) (*sql.Conn, *sql.Conn) {
		t.Helper()
		a, err := b.svc.handle.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		c, err := b.svc.handle.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = a.Close(); _ = c.Close() })
		return a, c
	}
	t.Run("reading first", func(t *testing.T) {
		b := newSessBed(t, true)
		connA, connB := twoConns(t, b)
		txA, err := connA.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = txA.Rollback() }()
		var g int
		if err := txA.QueryRowContext(ctx, `SELECT session_generation FROM archive_metadata WHERE singleton = 1`).Scan(&g); err != nil {
			t.Fatal(err)
		}
		if _, err := connB.ExecContext(ctx, bump); err != nil {
			t.Fatalf("the competing write: %v", err)
		}
		_, err = txA.ExecContext(ctx, bump)
		if err == nil {
			t.Fatal("a write after a stale read was accepted")
		}
		if !strings.Contains(err.Error(), "(517)") {
			t.Fatalf("the write failed with %q; want SQLite's stale-snapshot refusal (SQLITE_BUSY_SNAPSHOT, 517)", err)
		}
		t.Logf("reading first: refused with %q", err)
		if got := b.generation(); got != 1 {
			t.Fatalf("generation %d; only the competing write should have counted", got)
		}
	})
	t.Run("writing first", func(t *testing.T) {
		b := newSessBed(t, true)
		connA, connB := twoConns(t, b)
		txA, err := connA.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = txA.Rollback() }()
		if err := (&sessionTx{ctx: ctx, tx: txA}).takeWriterPosition(); err != nil {
			t.Fatalf("take the writer's position: %v", err)
		}
		limit := func(ms int) {
			t.Helper()
			if _, err := connB.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout = %d`, ms)); err != nil {
				t.Fatal(err)
			}
		}
		limit(0)
		if _, err := connB.ExecContext(ctx, bump); !isBusy(err) {
			t.Fatalf("with no busy limit the competing write returned %v; want it refused as busy", err)
		}
		const waitMS = 300
		limit(waitMS)
		started := time.Now()
		_, err = connB.ExecContext(ctx, bump)
		waited := time.Since(started)
		if !isBusy(err) {
			t.Fatalf("with a busy limit of %d ms the competing write returned %v; want it refused as busy", waitMS, err)
		}
		// A lower bound only: the handler waited about its limit before it gave up.
		if waited < waitMS*time.Millisecond*2/3 {
			t.Fatalf("the competing write was refused after %s; the busy handler should have waited about %d ms", waited, waitMS)
		}
		var g int
		if err := txA.QueryRowContext(ctx, `SELECT session_generation FROM archive_metadata WHERE singleton = 1`).Scan(&g); err != nil {
			t.Fatal(err)
		}
		if _, err := txA.ExecContext(ctx, bump); err != nil {
			t.Fatalf("the holder's write: %v", err)
		}
		if err := txA.Commit(); err != nil {
			t.Fatalf("the holder's commit: %v", err)
		}
		limit(busyTimeoutMS)
		if _, err := connB.ExecContext(ctx, bump); err != nil {
			t.Fatalf("the competing write after the holder's commit: %v", err)
		}
		t.Logf("writing first: the competing write was refused at once with no limit, after %s with a %d ms limit, and went through after the holder's commit",
			waited.Round(time.Millisecond), waitMS)
		if got := b.generation(); got != 2 {
			t.Fatalf("generation %d, want 2: both writes counted", got)
		}
	})
}
