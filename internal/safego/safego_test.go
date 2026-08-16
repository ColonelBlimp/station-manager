package safego

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// setCooldownForTest shortens respawnCooldown for the duration of a
// test and restores it on cleanup. Uses the atomic var so it can race
// safely against goroutines spawned by Go that read the cooldown.
func setCooldownForTest(t *testing.T, d time.Duration) {
	t.Helper()
	old := respawnCooldown.Load()
	respawnCooldown.Store(int64(d))
	t.Cleanup(func() { respawnCooldown.Store(old) })
}

// noopHandler discards recovered panics — used when a test cares
// about respawn mechanics but not the panic payload.
func noopHandler(string, any, []byte) {}

func TestGo_NoPanic_CallsFnOnce(t *testing.T) {
	var counter atomic.Int64
	done := make(chan struct{})

	Go(context.Background(), "test", noopHandler, func() {
		counter.Add(1)
		close(done)
	}, true)

	<-done
	// Give any stray respawn a chance to bump the counter — it shouldn't,
	// since fn didn't panic.
	time.Sleep(50 * time.Millisecond)

	if got := counter.Load(); got != 1 {
		t.Fatalf("counter = %d, want 1 (normal return should not respawn)", got)
	}
}

func TestGo_Panic_RespawnFalse_RunsFnOnce(t *testing.T) {
	setCooldownForTest(t, 10*time.Millisecond)

	var counter atomic.Int64
	panicked := make(chan struct{})

	Go(context.Background(), "test", func(string, any, []byte) {
		close(panicked)
	}, func() {
		counter.Add(1)
		panic("boom")
	}, false)

	<-panicked
	// Wait longer than cooldown; fn must not be respawned.
	time.Sleep(100 * time.Millisecond)

	if got := counter.Load(); got != 1 {
		t.Fatalf("counter = %d, want 1 (respawn=false)", got)
	}
}

func TestGo_Panic_Respawn_ReRunsFn(t *testing.T) {
	setCooldownForTest(t, 10*time.Millisecond)

	var counter atomic.Int64
	succeeded := make(chan struct{})

	Go(context.Background(), "test", noopHandler, func() {
		n := counter.Add(1)
		if n == 1 {
			panic("first try fails")
		}
		close(succeeded)
	}, true)

	select {
	case <-succeeded:
		// fn was respawned and completed successfully on the second attempt.
	case <-time.After(2 * time.Second):
		t.Fatalf("fn never respawned; counter = %d", counter.Load())
	}

	if got := counter.Load(); got != 2 {
		t.Fatalf("counter = %d, want 2 (one panic + one success)", got)
	}
}

func TestGo_PanicHandler_ReceivesNameValueAndStack(t *testing.T) {
	setCooldownForTest(t, 10*time.Millisecond)

	var (
		mu       sync.Mutex
		gotName  string
		gotValue any
		gotStack []byte
	)
	captured := make(chan struct{})

	Go(context.Background(), "worker-qrz", func(name string, v any, stack []byte) {
		mu.Lock()
		gotName = name
		gotValue = v
		gotStack = stack
		mu.Unlock()
		close(captured)
	}, func() {
		panic("deliberate-test-panic")
	}, false)

	<-captured
	mu.Lock()
	defer mu.Unlock()

	if gotName != "worker-qrz" {
		t.Fatalf("name = %q, want worker-qrz", gotName)
	}
	if gotValue != "deliberate-test-panic" {
		t.Fatalf("value = %v, want deliberate-test-panic", gotValue)
	}
	if len(gotStack) == 0 {
		t.Fatal("stack is empty")
	}
	// The stack should mention this test package/function.
	if !strings.Contains(string(gotStack), "safego") {
		t.Fatalf("stack does not reference safego package:\n%s", gotStack)
	}
}

func TestGo_CtxCancelled_SkipsRespawn(t *testing.T) {
	// Cooldown longer than the test-side cancel-then-check delay so we
	// can reliably cancel while the respawn is still waiting.
	setCooldownForTest(t, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	var counter atomic.Int64
	panicked := make(chan struct{}, 1)

	Go(ctx, "test", func(string, any, []byte) {
		panicked <- struct{}{}
	}, func() {
		counter.Add(1)
		panic("always")
	}, true)

	<-panicked
	cancel()

	// Wait comfortably past the cooldown. No respawn should fire.
	time.Sleep(700 * time.Millisecond)

	if got := counter.Load(); got != 1 {
		t.Fatalf("counter = %d, want 1 (ctx cancelled should skip respawn)", got)
	}
}

// TestGoTracked_WaitGroup_NoUnderflowDuringRespawn covers the C2 fix:
// a panicking worker with respawn=true must not let wg.Wait() unblock
// during the cooldown window. Counter starts at 1; through panic,
// cooldown, and respawn, it must stay >= 1 until the goroutine
// permanently exits.
func TestGoTracked_WaitGroup_NoUnderflowDuringRespawn(t *testing.T) {
	setCooldownForTest(t, 50*time.Millisecond)

	var wg sync.WaitGroup
	var attempts atomic.Int64
	finished := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	GoTracked(ctx, "test", noopHandler, func() {
		n := attempts.Add(1)
		if n == 1 {
			panic("first attempt")
		}
		// Second attempt: signal we got here, then return cleanly.
		close(finished)
	}, true, &wg)

	// Watcher: while goroutine is alive (between panic and respawn-success),
	// attempt to drain the wg. If wg.Wait() returns before `finished` is
	// closed, the underflow window is open — bug.
	waited := make(chan struct{})
	go func() {
		wg.Wait()
		close(waited)
	}()

	select {
	case <-waited:
		t.Fatal("wg.Wait returned before fn finished — underflow window open")
	case <-finished:
	}

	// Now the goroutine has returned cleanly; wg.Done has fired; wg.Wait should
	// unblock promptly.
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("wg.Wait did not return after fn finished cleanly")
	}

	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

// TestGoTracked_WaitGroup_DonePromptlyOnNormalReturn — fn returns
// normally (no panic), wg.Done fires immediately.
func TestGoTracked_WaitGroup_DonePromptlyOnNormalReturn(t *testing.T) {
	var wg sync.WaitGroup
	GoTracked(context.Background(), "test", noopHandler, func() {
		// no-op
	}, true, &wg)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wg.Wait did not return within 1s after fn's normal return")
	}
}

// TestGoTracked_WaitGroup_DoneOnCtxCancelDuringCooldown — fn panics,
// ctx cancels during cooldown, wg.Done fires when the goroutine
// permanently exits.
func TestGoTracked_WaitGroup_DoneOnCtxCancelDuringCooldown(t *testing.T) {
	setCooldownForTest(t, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	panicked := make(chan struct{})

	GoTracked(ctx, "test", func(string, any, []byte) {
		close(panicked)
	}, func() {
		panic("always")
	}, true, &wg)

	<-panicked
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wg.Wait did not return after ctx cancel")
	}
}

func TestGo_NilHandler_IsTolerated(t *testing.T) {
	setCooldownForTest(t, 10*time.Millisecond)

	done := make(chan struct{})
	Go(context.Background(), "test", nil, func() {
		defer close(done)
		panic("recovered silently")
	}, false)

	select {
	case <-done:
		// Recover ran (fn's own defer ran), process didn't crash.
	case <-time.After(time.Second):
		t.Fatal("goroutine did not complete within 1s")
	}
}

// TestGo_PanickingHandler_DoesNotEscape — review m1. An onPanic that
// itself panics must degrade to a silent skip, not bubble past
// runWithRespawn's outer recover and crash the process. The goroutine
// completes; the WaitGroup-tracked variant's wg.Done still fires.
func TestGo_PanickingHandler_DoesNotEscape(t *testing.T) {
	var wg sync.WaitGroup
	GoTracked(context.Background(), "test",
		func(string, any, []byte) { panic("handler-itself-panics") },
		func() { panic("worker-panic") },
		false, &wg)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// Both panics absorbed; goroutine exited cleanly; wg.Done fired.
	case <-time.After(time.Second):
		t.Fatal("wg.Wait did not return within 1s — onPanic crash escaped runWithRespawn")
	}
}

// LC-1 — GoTrackedPreAdded: caller pre-Adds; one tracked attempt; onPanic + per-worker
// panicPolicy run on a panic (policy even if onPanic faults); no respawn; wg always balanced.
func TestGoTrackedPreAdded(t *testing.T) {
	t.Run("clean return runs no policy and balances wg", func(t *testing.T) {
		var wg sync.WaitGroup
		policyRan := false
		wg.Add(1)
		GoTrackedPreAdded("clean", nil, func() {}, func() { policyRan = true }, &wg)
		wg.Wait() // returns iff Done was called exactly once
		if policyRan {
			t.Error("panicPolicy ran on a clean return")
		}
	})

	t.Run("panic runs onPanic then policy, balances wg", func(t *testing.T) {
		var wg sync.WaitGroup
		var onPanicN, policyN int
		wg.Add(1)
		GoTrackedPreAdded("boom",
			func(string, any, []byte) { onPanicN++ },
			func() { panic("worker") },
			func() { policyN++ },
			&wg)
		wg.Wait()
		if onPanicN != 1 {
			t.Errorf("onPanic ran %d times, want 1", onPanicN)
		}
		if policyN != 1 {
			t.Errorf("panicPolicy ran %d times, want 1", policyN)
		}
	})

	t.Run("policy runs even if onPanic itself panics", func(t *testing.T) {
		var wg sync.WaitGroup
		policyRan := false
		wg.Add(1)
		GoTrackedPreAdded("boom",
			func(string, any, []byte) { panic("handler faulted") },
			func() { panic("worker") },
			func() { policyRan = true },
			&wg)
		wg.Wait() // must still return — Done called despite the handler fault
		if !policyRan {
			t.Error("panicPolicy did not run when onPanic panicked")
		}
	})

	t.Run("a panic in the policy is swallowed and wg still balances", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)
		GoTrackedPreAdded("boom",
			func(string, any, []byte) {},
			func() { panic("worker") },
			func() { panic("policy faulted") },
			&wg)
		wg.Wait() // must return — a policy fault must not bypass Done
	})
}
