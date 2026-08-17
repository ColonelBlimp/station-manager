package refresher

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// newTestService builds a Service that's already past Initialize and
// Start. Tests that need to exercise the lifecycle paths construct
// the Service manually.
func newTestService(t *testing.T, maxInFlight int) *Service {
	t.Helper()
	s := &Service{
		LoggerService: &logging.Service{},
		MaxInFlight:   maxInFlight,
	}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s
}

// ---- Initialize ----

func TestInitialize_MissingLogger(t *testing.T) {
	s := &Service{}
	if err := s.Initialize(); err == nil {
		t.Fatal("expected error when logger is nil")
	}
}

func TestInitialize_DefaultsZeroMaxInFlight(t *testing.T) {
	s := &Service{LoggerService: &logging.Service{}}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if s.MaxInFlight != DefaultMaxInFlight {
		t.Errorf("MaxInFlight = %d, want default %d", s.MaxInFlight, DefaultMaxInFlight)
	}
}

func TestInitialize_PreservesPositiveMaxInFlight(t *testing.T) {
	s := &Service{LoggerService: &logging.Service{}, MaxInFlight: 16}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if s.MaxInFlight != 16 {
		t.Errorf("MaxInFlight = %d, want 16 (operator-set)", s.MaxInFlight)
	}
}

// ---- Schedule happy path ----

func TestSchedule_RunsFn(t *testing.T) {
	s := newTestService(t, 4)
	done := make(chan struct{})
	s.Schedule(func(_ context.Context) { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduled fn did not run within 1s")
	}
}

func TestSchedule_FnReceivesParentContext(t *testing.T) {
	s := &Service{LoggerService: &logging.Service{}, MaxInFlight: 4}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	if err := s.Start(parentCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	gotCtx := make(chan context.Context, 1)
	s.Schedule(func(ctx context.Context) {
		gotCtx <- ctx
		<-ctx.Done()
	})

	parentCancel() // cancel the parent — the fn's ctx should follow

	select {
	case ctx := <-gotCtx:
		// Wait briefly for the cancellation to propagate (it should be immediate).
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("fn's ctx did not cancel when parent ctx was cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("fn never ran")
	}
}

// ---- Bounding ----

func TestSchedule_DropsAtCapacity(t *testing.T) {
	s := newTestService(t, 2)

	// Block all slots.
	gate := make(chan struct{})
	var ran atomic.Int64
	s.Schedule(func(_ context.Context) {
		ran.Add(1)
		<-gate
	})
	s.Schedule(func(_ context.Context) {
		ran.Add(1)
		<-gate
	})
	// Wait for both to be in-flight before scheduling more.
	for s.InFlight() < 2 {
		time.Sleep(time.Millisecond)
	}

	// Third schedule must be dropped.
	s.Schedule(func(_ context.Context) { ran.Add(1) })

	if s.Dropped() != 1 {
		t.Errorf("Dropped() = %d, want 1", s.Dropped())
	}
	// Release the two in-flight fns.
	close(gate)

	// Drain. Stop waits for in-flight to complete.
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := ran.Load(); got != 2 {
		t.Errorf("ran = %d, want 2 (third was dropped)", got)
	}
}

func TestSchedule_ReleasesSlotAfterFn(t *testing.T) {
	// Schedule MaxInFlight fns, wait for them to complete, then
	// schedule another batch — none should be dropped because the
	// slots released.
	//
	// Done in two explicit batches rather than scheduling 4 in a
	// loop because scheduling 4 in a tight loop can race the first
	// pair completing — the 3rd / 4th may both see "at capacity"
	// and drop, which is correct bound behaviour but not what this
	// test is exercising.
	//
	// Synchronisation note (closes the historical -race flake): the
	// fn body sends `done` BEFORE the goroutine's deferred slot
	// release runs (the defer that does `<-s.sem` + `inFlight--`
	// fires after fn returns). So receiving on `done` only proves
	// the fn body finished, not that the slot is released. If the
	// next batch's Schedule beats the deferred release, it sees a
	// full semaphore and drops — that's the race. waitInFlight(0)
	// blocks until the deferred release has run.
	s := newTestService(t, 2)

	var ran atomic.Int64
	done := make(chan struct{}, 4)

	for range 2 {
		s.Schedule(func(_ context.Context) {
			ran.Add(1)
			done <- struct{}{}
		})
	}
	for range 2 {
		<-done
	}
	waitInFlight(t, s, 0)
	if s.Dropped() != 0 {
		t.Errorf("after first batch: Dropped() = %d, want 0", s.Dropped())
	}

	for range 2 {
		s.Schedule(func(_ context.Context) {
			ran.Add(1)
			done <- struct{}{}
		})
	}
	for range 2 {
		<-done
	}
	waitInFlight(t, s, 0)
	if s.Dropped() != 0 {
		t.Errorf("after second batch: Dropped() = %d, want 0 (slots must release)", s.Dropped())
	}
	if got := ran.Load(); got != 4 {
		t.Errorf("ran = %d, want 4", got)
	}
}

// waitInFlight polls Service.InFlight() until it equals want or the
// deadline expires. Used between batches in tests that need the
// goroutine's deferred slot release (which decrements inFlight) to
// observably complete before the next Schedule, since the fn body's
// completion signal alone doesn't synchronise that.
//
// 1s deadline is generous for a CI host — the deferred release is
// O(microseconds) on a healthy runtime; only an OS-level scheduler
// stall would push past 1s.
func waitInFlight(t *testing.T, s *Service, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.InFlight() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("InFlight() did not reach %d within 1s; got %d", want, s.InFlight())
}

// ---- Lifecycle ----

func TestSchedule_BeforeStart_Drops(t *testing.T) {
	s := &Service{LoggerService: &logging.Service{}, MaxInFlight: 4}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// Skip Start.
	s.Schedule(func(_ context.Context) { t.Error("fn should not run before Start") })
	if s.Dropped() != 1 {
		t.Errorf("Dropped() = %d, want 1", s.Dropped())
	}
}

func TestSchedule_AfterStop_Drops(t *testing.T) {
	s := &Service{LoggerService: &logging.Service{}, MaxInFlight: 4}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	s.Schedule(func(_ context.Context) { t.Error("fn should not run after Stop") })
	if s.Dropped() != 1 {
		t.Errorf("Dropped() = %d, want 1 after Stop", s.Dropped())
	}
}

func TestStop_WaitsForInFlight(t *testing.T) {
	s := newTestService(t, 4)
	done := make(chan struct{})
	s.Schedule(func(_ context.Context) {
		time.Sleep(50 * time.Millisecond)
		close(done)
	})

	start := time.Now()
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Error("Stop returned before in-flight fn completed")
	}
	select {
	case <-done:
	default:
		t.Error("done channel not closed — fn didn't run to completion")
	}
}

func TestStop_CancelsInFlightContext(t *testing.T) {
	s := newTestService(t, 4)
	noticed := make(chan struct{})
	s.Schedule(func(ctx context.Context) {
		<-ctx.Done()
		close(noticed)
	})

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-noticed:
	case <-time.After(time.Second):
		t.Fatal("fn's ctx was not cancelled by Stop")
	}
}

func TestStop_Idempotent(t *testing.T) {
	s := newTestService(t, 4)
	if err := s.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestStart_Idempotent(t *testing.T) {
	s := &Service{LoggerService: &logging.Service{}, MaxInFlight: 4}
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
}

// ---- Panic recovery ----

// TestSchedule_StopRace exercises the M1 race the code-review flagged: concurrent Schedule + Stop
// where a Schedule's admission could pass just before Stop, then its worker-count Add land after
// Stop's Wait saw zero. Post-migration the refresh lane's Admit does the Running check + count in one
// atomic step against the supervisor's seal, so once Stop seals, Admit refuses and the lane-wait sees
// a complete counter — the WaitGroup-misuse panic cannot arise. (This test also constructs Service
// via a struct literal, exercising the ensureLifecycle lazy-init path.)
//
// The test runs many iterations (each a fresh Service + interleaved Schedule and Stop) and is most
// useful with -race; pre-fix, some iterations would race-detect or panic outright.
func TestSchedule_StopRace_NoWaitGroupMisuse(t *testing.T) {
	const iterations = 100
	const schedulesPerIter = 16

	for i := 0; i < iterations; i++ {
		s := &Service{
			LoggerService: &logging.Service{},
			MaxInFlight:   8,
		}
		if err := s.Initialize(); err != nil {
			t.Fatalf("iter %d: Initialize: %v", i, err)
		}
		if err := s.Start(context.Background()); err != nil {
			t.Fatalf("iter %d: Start: %v", i, err)
		}

		// Spray Schedule calls from several goroutines while Stop fires
		// from another. The interleaving is what we're trying to
		// stress-test; the exact ordering doesn't matter — only that no
		// goroutine panics with a WaitGroup-misuse error.
		var sprayWg sync.WaitGroup
		for k := 0; k < schedulesPerIter; k++ {
			sprayWg.Add(1)
			go func() {
				defer sprayWg.Done()
				s.Schedule(func(ctx context.Context) {
					// Bail immediately on ctx cancel — we want fns to
					// be cheap so Stop drains quickly.
					select {
					case <-ctx.Done():
					default:
					}
				})
			}()
		}

		stopErr := make(chan error, 1)
		go func() {
			stopErr <- s.Stop()
		}()

		sprayWg.Wait()
		if err := <-stopErr; err != nil {
			t.Fatalf("iter %d: Stop returned err: %v", i, err)
		}
	}
}

func TestSchedule_RecoversFnPanic(t *testing.T) {
	s := newTestService(t, 4)

	// Panicking fn shouldn't crash the test runner. After it
	// "completes" (via panic recovery), the slot is released and
	// subsequent schedules work.
	s.Schedule(func(_ context.Context) { panic("synthetic refresh panic") })

	// Schedule another fn and confirm it runs — proves the slot
	// got released after the panic.
	done := make(chan struct{})
	for {
		// May briefly be at capacity until the panicking goroutine
		// finishes and releases the slot.
		dropped := s.Dropped()
		s.Schedule(func(_ context.Context) { close(done) })
		if s.Dropped() == dropped {
			break // didn't drop — schedule succeeded
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("post-panic schedule did not run — slot leak after panic")
	}
}
