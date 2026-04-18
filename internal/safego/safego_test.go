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
