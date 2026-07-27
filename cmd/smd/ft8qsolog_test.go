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

// TestResolveQsoDialMHz pins which frequency a completed FT8 contact is logged on.
// The session's pinned dial wins over a live rig read: they differ exactly when the
// operator QSYed between the contact completing and its closing rung, and filing it
// on the band we moved to is worse than not filing it — the wrong-band row is
// forwarded to QRZ and ClubLog (codex P1 on 652821db).
func TestResolveQsoDialMHz(t *testing.T) {
	live := func(mhz float64, ok bool) func() (float64, bool) {
		return func() (float64, bool) { return mhz, ok }
	}
	cases := []struct {
		name   string
		pinned float64
		live   func() (float64, bool)
		want   float64
	}{
		{"pinned wins over a live read that has moved on", 14.074, live(7.074, true), 14.074},
		{"pinned wins even when they agree", 14.074, live(14.074, true), 14.074},
		{"no pin falls back to the live read", 0, live(7.074, true), 7.074},
		{"no pin and no live read logs nothing rather than zero-as-truth", 0, live(0, false), 0},
		{"no pin and no source at all", 0, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveQsoDialMHz(tc.pinned, tc.live); got != tc.want {
				t.Errorf("resolveQsoDialMHz(%v) = %v, want %v", tc.pinned, got, tc.want)
			}
		})
	}
}
