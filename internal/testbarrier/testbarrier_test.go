package testbarrier

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // the driver whose busy handler CountSQLiteBusy looks for
)

type order struct {
	mu   sync.Mutex
	jobs sync.WaitGroup
}

func (o *order) take() func() { o.mu.Lock(); return o.mu.Unlock }
func (o *order) drain()       { o.jobs.Wait() }

// viaTake parks in take, one call further down: take is the lock's caller, this is not.
func (o *order) viaTake() { o.take()() }

// refusal records Fatalf instead of ending the test, so the helper's own refusals can be
// tested. Fatalf must not return to its caller, as testing.T's does not.
type refusal struct {
	testing.TB
	msg string
}

func (r *refusal) Helper() {}
func (r *refusal) Fatalf(format string, _ ...any) {
	r.msg = format
	panic(r)
}

// refused runs fn and returns what it failed with, or "" if it did not fail.
func refused(t *testing.T, fn func(tb testing.TB)) string {
	t.Helper()
	r := &refusal{TB: t}
	func() {
		defer func() {
			if p := recover(); p != nil && p != any(r) {
				panic(p)
			}
		}()
		fn(r)
	}()
	return r.msg
}

func quick(t *testing.T) {
	t.Helper()
	old := patience
	patience = 100 * time.Millisecond
	t.Cleanup(func() { patience = old })
}

// parkedIn starts fn and returns once it is parked for reason in where.
func parkedIn(t *testing.T, reason, where string, fn func()) <-chan struct{} {
	t.Helper()
	before := Count(reason, where)
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	deadline := time.Now().Add(5 * time.Second)
	for Count(reason, where) != before+1 {
		if time.Now().After(deadline) {
			t.Fatalf("the fixture: nothing parked (%s in %s)", reason, where)
		}
		time.Sleep(time.Millisecond)
	}
	return done
}

// The helper sees a goroutine parked at a mutex and at a wait group, by the function that
// took the lock; it does not take another reason, another function, or a function further
// up the stack for it.
func TestWaiting_SeesTheWaitAndWhereItIs(t *testing.T) {
	o := &order{}
	o.mu.Lock()
	o.jobs.Add(1)
	took, drained := make(chan struct{}), make(chan struct{})

	w := Expect(t, AtMutex, "(*order).take")
	go func() { defer close(took); o.viaTake() }()
	Waiting(w, "the take", took)

	w = Expect(t, AtWaitGroup, "(*order).drain")
	go func() { defer close(drained); o.drain() }()
	Waiting(w, "the drain", drained)

	for _, wrong := range []struct{ reason, fn string }{
		{AtWaitGroup, "(*order).take"}, // another reason
		{AtMutex, "(*order).drain"},    // another reason
		{AtMutex, "(*other).take"},     // another function
		{AtMutex, "(*order).viaTake"},  // on the stack, but not the lock's caller
		{AtMutex, "(*Mutex).Lock"},     // the sync package's own frame
	} {
		if n := Count(wrong.reason, wrong.fn); n != 0 {
			t.Errorf("%d counted for %s in %s; want none", n, wrong.reason, wrong.fn)
		}
	}

	o.mu.Unlock()
	o.jobs.Done()
	for _, ch := range []chan struct{}{took, drained} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("a waiter never finished after it was let go")
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for Count(AtMutex, "(*order).take")+Count(AtWaitGroup, "(*order).drain") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("a finished waiter still reads as parked")
		}
		time.Sleep(time.Millisecond)
	}
}

// An operation that answers is a failure, not a wait.
func TestWaiting_RefusesAnOperationThatAnswers(t *testing.T) {
	answered := make(chan int, 1)
	answered <- 7
	msg := refused(t, func(tb testing.TB) {
		Waiting(Expect(tb, AtMutex, "(*order).take"), "the operation", answered)
	})
	if !strings.Contains(msg, "answered") {
		t.Fatalf("Waiting said %q of an operation that had answered", msg)
	}
}

// The wrong waiter (codex-204): another goroutine is already parked at the place when the
// watch begins, and the operation under test is waiting somewhere else. Seeing "a
// goroutine parked there" would say nothing about the operation, so the watch is refused.
func TestExpect_RefusesAPlaceWhereSomethingIsAlreadyParked(t *testing.T) {
	o := &order{}
	o.mu.Lock()
	unrelated := parkedIn(t, AtMutex, "(*order).take", func() { o.take()() })
	defer func() { o.mu.Unlock(); <-unrelated }()

	elsewhere := make(chan struct{}) // the operation under test: it never answers, and is not at the lock
	msg := refused(t, func(tb testing.TB) {
		Waiting(Expect(tb, AtMutex, "(*order).take"), "the operation", elsewhere)
	})
	if !strings.Contains(msg, "already parked") {
		t.Fatalf("the watch said %q with an unrelated goroutine already parked there", msg)
	}
}

// Two goroutines parked at the place after the watch began: the wait seen does not say
// which of them is the operation.
func TestWaiting_RefusesTwoParkedAtThePlace(t *testing.T) {
	o := &order{}
	o.mu.Lock()
	w := Watch{} // filled below: Expect must run before either is started
	msg := refused(t, func(tb testing.TB) {
		w = Expect(tb, AtMutex, "(*order).take")
		a := parkedIn(t, AtMutex, "(*order).take", func() { o.take()() })
		b := parkedIn(t, AtMutex, "(*order).take", func() { o.take()() })
		defer func() { o.mu.Unlock(); <-a; <-b }()
		Waiting(w, "the operation", make(chan struct{}))
	})
	if !strings.Contains(msg, "goroutines are parked") {
		t.Fatalf("Waiting said %q with two goroutines parked at the place", msg)
	}
}

// Nothing parks at the place and the operation does not answer: neither a wait nor an
// answer was seen, and that is a failure, never a pass.
func TestWaiting_RefusesWhenNothingIsSeen(t *testing.T) {
	quick(t)
	msg := refused(t, func(tb testing.TB) {
		Waiting(Expect(tb, AtMutex, "(*order).take"), "the operation", make(chan struct{}))
	})
	if !strings.Contains(msg, "neither answered nor was seen waiting") {
		t.Fatalf("Waiting said %q when nothing was seen", msg)
	}
}

// writer asks a database for its writer position and gives it back.
type writer struct{ db *sql.DB }

func (w *writer) take() error {
	conn, err := w.db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	_, err = conn.ExecContext(context.Background(), `ROLLBACK`)
	return err
}

// viaTake asks through take: both are on the waiting goroutine's stack.
func (w *writer) viaTake() error { return w.take() }

// The helper sees a writer that SQLite is making wait, by any function on its stack: a
// connection that asked for the writer's position while another held it. It sees none
// before the wait begins, none through a function that is not on the stack, and none
// once the holder has let go.
func TestWaiting_SeesAWriterSQLiteMakesWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "barrier.db")
	open := func(busyMS string) *sql.DB {
		t.Helper()
		db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout("+busyMS+")&_pragma=journal_mode(WAL)")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	holder, waiter := open("0"), &writer{db: open("20000")}
	held, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	if _, err := held.ExecContext(context.Background(), `CREATE TABLE t (n INTEGER); BEGIN IMMEDIATE; INSERT INTO t VALUES (1)`); err != nil {
		t.Fatal(err)
	}

	w := ExpectSQLiteBusy(t, "(*writer).take")
	took := make(chan error, 1)
	go func() { took <- waiter.viaTake() }()
	Waiting(w, "the second writer", took)
	if n := CountSQLiteBusy("(*writer).viaTake"); n != 1 {
		// Not a race: Waiting returned with the writer seen, and it stays refused
		// while the holder holds, but it is in the handler only between attempts.
		deadline := time.Now().Add(5 * time.Second)
		for CountSQLiteBusy("(*writer).viaTake") != 1 {
			if time.Now().After(deadline) {
				t.Fatal("the waiting writer is not seen through a function further up its stack")
			}
			time.Sleep(time.Millisecond)
		}
	}
	for i := 0; i < 200; i++ {
		if n := CountSQLiteBusy("(*order).take"); n != 0 {
			t.Fatalf("%d counted through a function that is not on the waiter's stack", n)
		}
	}

	if _, err := held.ExecContext(context.Background(), `COMMIT`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-took:
		if err != nil {
			t.Fatalf("the writer that waited failed once the holder let go: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the writer that waited never finished after the holder let go")
	}
	if n := CountSQLiteBusy("(*writer).take"); n != 0 {
		t.Fatalf("%d still counted as waiting after the writer finished", n)
	}
}

// A writer that is not made to wait is an answer, not a wait: the watch refuses it, in
// the words it keeps for an answer, and not in those for a wait that was never seen
// (both contain "answered"). The writer itself took the position and gave it back: the
// answer was a success, not a database error that would also have counted as one.
func TestWaiting_RefusesAWriterThatWasNotMadeToWait(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "free.db")+"?_pragma=busy_timeout(20000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	free := &writer{db: db}
	var (
		took    error
		done    = make(chan struct{}) // its own signal: Waiting consumes the answer it refuses
		answers = make(chan error, 1)
	)
	msg := refused(t, func(tb testing.TB) {
		w := ExpectSQLiteBusy(tb, "(*writer).take")
		go func() {
			defer close(done)
			took = free.take()
			answers <- took
		}()
		Waiting(w, "a writer nobody is in the way of", answers)
	})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the free writer never finished")
	}
	if took != nil {
		t.Fatalf("the free writer failed (%v): the case is about a writer that succeeds without waiting", took)
	}
	if !strings.Contains(msg, "while it should have been waiting") {
		t.Fatalf("the watch said %q of a writer that did not wait; want it refused as an answer", msg)
	}
}
