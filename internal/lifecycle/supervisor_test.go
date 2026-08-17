package lifecycle

// ADR 0070 phase 1 — the Supervisor primitive. These are the operator-observable invariants it
// exists to make structural (docs/v2-design/lifecycle.md §2); each was ratified across the ADR +
// design review. They are the LC-2/LC-3/LC-4 guarantees, once:
//
//   AC-1  Admission is open ONLY while Running — Lane.Admit is refused before Start (admission not
//         open) and once sealed at Stop, accepted in between.
//   AC-2  Start is one transition w.r.t. Stop AND producers: public Admit stays closed until the
//         commit point (after launch); an acquire failure leaves the supervisor Idle/retryable with
//         the context cancelled and admission still closed.
//   AC-3  Stop waits every lane's admitted work before teardown; it cancels the context BEFORE
//         waiting (so cancellable work releases its locks), then runs teardown.
//   AC-4  Stop is a completion barrier: concurrent callers all return the SAME error and teardown
//         runs exactly once.
//   AC-5  Terminal: Start-after-Stop is a no-op that never re-acquires; Stop-before-Start runs no
//         teardown and is terminal.
//   AC-6  A teardown panic folds (with its stack) into the shared error, is never re-panicked, and
//         strands no caller.
//   AC-7  A Cancellable lane's work is interrupted at Stop; a MustDrain lane's is only awaited
//         (never cancelled) — the LC-4 writer-vs-sync distinction.
//
// Reversion proofs (revert one mechanism, one test goes RED for its own reason) are exercised
// during development; the mechanisms and their guarding tests are noted at each test.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// AC-1: admission tracks Running. Reversion: drop the `phase != Running` guard in Admit → accepted
// before Start / after Stop.
func TestAdmit_OpenOnlyWhileRunning(t *testing.T) {
	s := New()
	lane := s.RegisterLane("w", MustDrain)

	if _, ok := lane.Admit(); ok {
		// Fail-fast: a wrongly-admitted unit would leak a lane count and hang the Stop below.
		t.Fatal("Admit accepted before Start; admission must be closed until Running")
	}
	if err := s.Start(context.Background(), nil, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done, ok := lane.Admit()
	if !ok {
		t.Fatal("Admit refused while Running")
	}
	done()
	_ = s.Stop(func() error { return nil })
	if _, ok := lane.Admit(); ok {
		t.Error("Admit accepted after Stop; admission must be sealed")
	}
}

// AC-2: an acquire failure stays Idle/retryable, cancels the context, and does not open admission.
// Reversion: publish Running before acquire → phase Running after a failed acquire.
func TestStart_AcquireFailureStaysIdleRetryable(t *testing.T) {
	s := New()
	lane := s.RegisterLane("w", MustDrain)
	boom := errors.New("acquire failed")

	var acquiredCtx context.Context
	err := s.Start(context.Background(),
		func(ctx context.Context) error { acquiredCtx = ctx; return boom }, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Start err = %v, want the acquire error", err)
	}
	if s.Phase() != Idle {
		t.Errorf("phase = %s after acquire failure, want idle (retryable)", s.Phase())
	}
	if _, ok := lane.Admit(); ok {
		t.Error("Admit accepted after a failed Start; admission must stay closed")
	}
	if acquiredCtx.Err() == nil {
		t.Error("service context not cancelled after acquire failure")
	}
	// Retryable:
	if err := s.Start(context.Background(), nil, nil); err != nil {
		t.Errorf("retry Start err = %v", err)
	}
	if s.Phase() != Running {
		t.Errorf("phase = %s after retry, want running", s.Phase())
	}
}

// AC-2: public Admit is closed during launch (before the commit point), while Track works.
// Reversion: publish Running before launch → Admit succeeds inside launch.
func TestStart_PublicAdmitClosedDuringLaunch(t *testing.T) {
	s := New()
	lane := s.RegisterLane("w", MustDrain)

	var okInLaunch bool
	err := s.Start(context.Background(), nil, func(ctx context.Context, sc *StartScope) {
		done, ok := lane.Admit() // admission must still be closed here
		okInLaunch = ok
		if ok {
			done() // defensive: if it wrongly admits, don't leak a lane count into a hanging Stop
		}
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if okInLaunch {
		t.Error("public Admit succeeded during launch — before the commit point")
	}
	if done, ok := lane.Admit(); !ok {
		t.Error("Admit refused after Start committed")
	} else {
		done()
	}
	_ = s.Stop(func() error { return nil })
}

// AC-3 / AC-7 (must-drain half): Stop waits an outstanding MustDrain unit before teardown, and the
// context cancel does NOT release it. Reversion: skip the lane wg.Wait in Stop → Stop returns while
// the unit is outstanding.
func TestStop_WaitsMustDrainWorkBeforeTeardown(t *testing.T) {
	s := New()
	lane := s.RegisterLane("w", MustDrain)
	if err := s.Start(context.Background(), nil, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done, ok := lane.Admit()
	if !ok {
		t.Fatal("Admit refused while Running")
	}

	teardownRan := make(chan struct{})
	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop(func() error { close(teardownRan); return nil }) }()

	select {
	case <-stopped:
		t.Fatal("Stop returned before the outstanding must-drain unit finished")
	case <-teardownRan:
		t.Fatal("teardown ran before the must-drain unit finished")
	case <-time.After(200 * time.Millisecond):
	}
	done() // release the unit
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("Stop err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after the must-drain unit finished")
	}
}

// AC-3 / AC-7 (cancellable half): a Cancellable worker bound to Context() is interrupted by Stop,
// then awaited, then teardown runs. Reversion: don't cancel in Stop → the worker blocks forever and
// Stop hangs (the watchdog fires).
func TestStop_CancelsCancellableThenTearsDown(t *testing.T) {
	s := New()
	lane := s.RegisterLane("sync", Cancellable)

	workerExited := make(chan struct{})
	err := s.Start(context.Background(), nil, func(ctx context.Context, sc *StartScope) {
		release := sc.Track(lane)
		go func() { defer release(); <-ctx.Done(); close(workerExited) }()
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	teardownRan := make(chan struct{})
	stopped := make(chan struct{})
	go func() { _ = s.Stop(func() error { close(teardownRan); return nil }); close(stopped) }()

	select {
	case <-workerExited:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellable worker was not interrupted by Stop's context cancel")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after the cancellable worker exited")
	}
	select {
	case <-teardownRan:
	default:
		t.Error("teardown did not run")
	}
}

// AC-4: concurrent Stop callers all return the same error; teardown runs once.
// Reversion: run teardown outside stopOnce → teardown runs N times / callers diverge.
func TestStop_ConcurrentCallersShareOneResult(t *testing.T) {
	s := New()
	if err := s.Start(context.Background(), nil, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	sentinel := errors.New("teardown boom")
	var calls int32
	teardown := func() error { atomic.AddInt32(&calls, 1); return sentinel }

	const n = 6
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Stop(teardown) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, sentinel) {
			t.Errorf("a caller returned %v, want the shared sentinel", err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("teardown ran %d times, want exactly 1", got)
	}
}

// AC-5: Start-after-Stop is a terminal no-op that never re-acquires.
// Reversion: allow Start from Stopped → acquire runs again.
func TestStart_AfterStopIsTerminalNoOp(t *testing.T) {
	s := New()
	var acquireCount int32
	acquire := func(ctx context.Context) error { atomic.AddInt32(&acquireCount, 1); return nil }

	if err := s.Start(context.Background(), acquire, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = s.Stop(func() error { return nil })
	if err := s.Start(context.Background(), acquire, nil); err != nil {
		t.Errorf("Start-after-Stop err = %v, want nil (no-op)", err)
	}
	if got := atomic.LoadInt32(&acquireCount); got != 1 {
		t.Errorf("acquire ran %d times; Start-after-Stop must not re-acquire", got)
	}
	if s.Phase() != Stopped {
		t.Errorf("phase = %s, want stopped (terminal)", s.Phase())
	}
}

// AC-5: Stop-before-Start runs no teardown and is terminal (a later Start is refused).
func TestStop_BeforeStartIsTerminal(t *testing.T) {
	s := New()
	var teardownRan bool
	_ = s.Stop(func() error { teardownRan = true; return nil })
	if teardownRan {
		t.Error("teardown ran on Stop-before-Start; nothing was opened to tear down")
	}
	if s.Phase() != Stopped {
		t.Errorf("phase = %s after Stop-before-Start, want stopped", s.Phase())
	}
	err := s.Start(context.Background(),
		func(ctx context.Context) error { t.Error("acquire ran after a terminal Stop"); return nil }, nil)
	if err != nil {
		t.Errorf("terminal Start err = %v, want nil", err)
	}
	if s.Phase() != Stopped {
		t.Errorf("phase = %s after terminal Start, want stopped", s.Phase())
	}
}

// AC-6: a teardown panic folds (with stack) into the shared error, is not re-panicked, and strands
// no caller. Reversion: call teardown() directly instead of runTeardown → the panic escapes and
// stopDone never closes (callers hang / the process crashes).
func TestStop_TeardownPanicFoldsIntoError(t *testing.T) {
	s := New()
	if err := s.Start(context.Background(), nil, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := s.Stop(func() error { panic("kaboom") })
	if err == nil || !strings.Contains(err.Error(), "kaboom") || !strings.Contains(err.Error(), "panicked") {
		t.Errorf("Stop err = %v, want a folded panic error mentioning the value", err)
	}
	if s.Phase() != Stopped {
		t.Errorf("phase = %s after a teardown panic, want stopped", s.Phase())
	}
	// The barrier held despite the panic: a second caller returns the same folded error.
	if err2 := s.Stop(func() error { return nil }); err2 == nil || !strings.Contains(err2.Error(), "kaboom") {
		t.Errorf("second caller err = %v, want the same folded panic error", err2)
	}
}
