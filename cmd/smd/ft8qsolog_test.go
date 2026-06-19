package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func noopPanic(string, any, []byte) {}

// launchFt8QsoLog must track the goroutine on the WaitGroup so shutdown can
// drain it (review 2026-06-19 M2).
func TestLaunchFt8QsoLog_TrackedAndDrains(t *testing.T) {
	var wg sync.WaitGroup
	ran := make(chan struct{})

	launchFt8QsoLog(&wg, noopPanic, func(context.Context) {
		close(ran)
	})

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("work never ran")
	}

	// wg.Wait must return once the (one-shot) goroutine has exited — i.e. it was
	// Add(1)'d, so a shutdown drain blocks on it.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait did not return; goroutine was not tracked")
	}
}

// The work's context must be decoupled from the caller's (decode-loop) context,
// which ft8Svc.Stop() cancels: a QSO that already completed on the air has to be
// persisted during the shutdown drain, so a cancelled input context must not
// abort it. The work context is its own bounded context (review M2).
func TestLaunchFt8QsoLog_ContextDecoupledFromDecodeLoop(t *testing.T) {
	var wg sync.WaitGroup
	type ctxState struct {
		err         error
		hasDeadline bool
	}
	got := make(chan ctxState, 1)

	// Inspect the context's state INSIDE work — after work returns the deferred
	// cancel() fires, so a post-hoc check would always see "canceled".
	launchFt8QsoLog(&wg, noopPanic, func(ctx context.Context) {
		_, hasDeadline := ctx.Deadline()
		got <- ctxState{err: ctx.Err(), hasDeadline: hasDeadline}
	})

	var st ctxState
	select {
	case st = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("work never ran")
	}

	// Not already cancelled — the QSO can be stored even though the caller's
	// decode-loop context may be cancelled by ft8Svc.Stop().
	if st.err != nil {
		t.Errorf("work context already errored: %v", st.err)
	}
	// Bounded — a hung enrich/submit can't run forever.
	if !st.hasDeadline {
		t.Error("work context has no deadline; a hung log job could outlive shutdown")
	}
	wg.Wait()
}
