package evidence

// LC-4 (docs/reviews/internal-lifecycle-concurrency-audit.md) — the evidence honesty endpoint
// GET /v1/evidence/status ran ~17 sequential archive reads with NO shared deadline and no way
// for a client disconnect to cancel the remaining work (worst case ~17×2s under lock
// contention). StatusContext threads the request context through the four fill* aggregates and
// bounds the whole snapshot by one deadline (statusAggregateTimeout, 3s — operator ruling
// 2026-08-17). A cancelled/timed-out read folds into EH-4's existing shape: the DB-derived
// groups report UNKNOWN (nil), degraded=true — never a plausible zero.
//
// Acceptance criteria (operator-observable):
//
//   AC-1  A cancelled/disconnected status request returns promptly with its DB-derived groups
//         reported unknown (nil) and degraded=true — never zero/partial counts — while non-DB
//         fields (capture state, dropped count) stay honest. Confusable state broken: a
//         cancelled read serializing zeros as if the archive were empty (the EH-4 failure mode).
//   AC-2  One aggregate deadline bounds the whole snapshot: a ctx-interruptible slow read
//         returns within the deadline, degraded — not after all reads complete. Confusable
//         state broken: an unbounded snapshot that runs every read to completion (~17×2s).
//
// Reversion proofs (each reverts one mechanism; verified 2026-08-17):
//   AC-1: StatusContext derives its deadline from context.Background() instead of the caller's
//         ctx → the cancelled request is ignored, the reads succeed, counts serialize as 0 and
//         degraded=false.
//   AC-2: drop the WithTimeout wrapper → the ctx-interruptible read blocks forever; the
//         watchdog fires because the snapshot never bounds itself.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// AC-1: a cancelled request reports every DB-derived group as unknown (nil) + degraded, never
// zero, while the non-DB capture state survives (EH-4 honest-partial shape).
func TestStatusContext_CancelledRequestReportsUnknownNotZero(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the client is already gone before the aggregates run

	st := s.StatusContext(ctx)

	if !st.Degraded {
		t.Error("Degraded = false on a cancelled status request; want true")
	}
	if !strings.Contains(st.StatusError, "context canceled") {
		t.Errorf("StatusError = %q, want it to mention context cancellation", st.StatusError)
	}
	if st.Observations != nil || st.UnprofiledObservations != nil {
		t.Errorf("observation counts served as %v/%v on a cancelled read; want nil (unknown)",
			st.Observations, st.UnprofiledObservations)
	}
	if st.Profiles == nil || st.Profiles.Lineages != nil {
		t.Error("profile lineage count served as a value on a cancelled read; want unknown (nil)")
	}
	if st.Retention == nil || st.Retention.PurgedObservations != nil {
		t.Error("retention counts served as a value on a cancelled read; want unknown (nil)")
	}
	// A non-DB field must survive the cancellation — the operator still learns capture is live.
	if st.State != StateCapturing {
		t.Errorf("State = %q on a cancelled read, want %q (a non-DB field must survive)", st.State, StateCapturing)
	}
}

// AC-2: the whole snapshot is bounded by one aggregate deadline. The seam models a
// ctx-interruptible slow archive read; the dialled-down timeout must cut it short.
func TestStatusContext_BoundedByAggregateDeadline(t *testing.T) {
	old := statusAggregateTimeout
	statusAggregateTimeout = 100 * time.Millisecond
	t.Cleanup(func() { statusAggregateTimeout = old })

	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	statusBlockForTest = func(ctx context.Context) { <-ctx.Done() } // blocks until the deadline fires
	defer func() { statusBlockForTest = nil }()

	done := make(chan Status, 1)
	go func() { done <- s.StatusContext(context.Background()) }()
	select {
	case st := <-done:
		if !st.Degraded {
			t.Error("Degraded = false after the aggregate deadline fired; want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StatusContext did not return within its aggregate deadline — the snapshot is unbounded")
	}
}
