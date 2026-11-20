package testbarrier

import (
	"strings"
	"sync"
	"testing"
	"time"
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
