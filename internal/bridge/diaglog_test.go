package bridge

// B4 / B6 / B7 (internal/bridge logging audit) — three advisory paths that
// took an action (or declined one) and left no trace in smd.log:
//
//   - B4  deliverAck dropped a CI-V ACK with no waiter (`ch == nil → return`),
//     so the accepted late-ACK race's real hit-rate stayed unmeasurable.
//   - B6  publishClientCount fanned the subscriber count to every tab but
//     logged nothing, so how many tabs were attached when is unrecoverable.
//   - B7  TriggerBootstrap's safe no-op (pipeline not running) was silent,
//     indistinguishable from a real bootstrap — while its sibling keyed-skip
//     cases already logged at Debug.
//
// ACCEPTANCE (each asserts on the emitted record AND on the confusable state):
//   - B4  -> a Debug "CI-V ACK dropped" line on the no-waiter case ONLY; a
//     delivered ACK (waiter present) logs no drop.
//   - B6  -> an Info line with the count on a fan-out (n>=2); a lone subscriber
//     (below the fan-out threshold) logs nothing.
//   - B7  -> a Debug "bootstrap skipped" line when the pipeline isn't running.

import (
	"context"
	"testing"
)

// TestDeliverAck_NoWaiter_LogsDebugDrop pins B4: the no-waiter drop logs, and a
// delivered ACK does not — the fixture feeds both so the two paths differ.
func TestDeliverAck_NoWaiter_LogsDebugDrop(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")

	// pendingAck is nil by default → no waiter → the drop path.
	s.deliverAck(true)

	recs := matching(t, buf, "CI-V ACK dropped")
	if len(recs) != 1 {
		t.Fatalf("ack-drop debug lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "debug" {
		t.Errorf("level = %q, want debug", lvl)
	}

	// Positive control: a waiter present → the ACK is delivered, no drop line.
	s.mu.Lock()
	s.pendingAck = make(chan bool, 1)
	s.mu.Unlock()
	s.deliverAck(true)
	if got := countLines(buf, "CI-V ACK dropped"); got != 1 {
		t.Fatalf("drop lines after a DELIVERED ack = %d, want still 1 (delivery must not log a drop)", got)
	}
}

// TestPublishClientCount_LogsCountChange pins B6: a fan-out logs the count; a
// lone subscriber (n=1, below the n>=2 fan-out threshold) does not.
func TestPublishClientCount_LogsCountChange(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")

	_, u1 := s.Subscribe()
	defer u1()
	if got := countLines(buf, "subscriber count changed"); got != 0 {
		t.Fatalf("lone-subscriber count lines = %d, want 0 (no fan-out below 2); log:\n%s", got, buf.String())
	}

	_, u2 := s.Subscribe()
	defer u2()
	recs := matching(t, buf, "subscriber count changed")
	if len(recs) != 1 {
		t.Fatalf("count-change lines after 2nd subscribe = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "info" {
		t.Errorf("level = %q, want info", lvl)
	}
	if c, ok := recs[0]["clients"].(float64); !ok || int(c) != 2 {
		t.Errorf("clients = %v, want 2", recs[0]["clients"])
	}
}

// TestTriggerBootstrap_NoPipeline_LogsDebugSkip pins B7: the safe no-op when no
// pipeline is running now leaves a Debug line instead of silence.
func TestTriggerBootstrap_NoPipeline_LogsDebugSkip(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")

	// No Start → activeClient nil, bootstrapBytes nil → the safe no-op path.
	if err := s.TriggerBootstrap(context.Background()); err != nil {
		t.Fatalf("TriggerBootstrap returned err on no-op: %v", err)
	}

	recs := matching(t, buf, "bootstrap skipped")
	if len(recs) != 1 {
		t.Fatalf("bootstrap-skip debug lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "debug" {
		t.Errorf("level = %q, want debug", lvl)
	}
}

// --- codex 1408edb1 review (P2 x2): the two B4/B6 paths that delivered only
// half their stated intent. ---

// TestDeliverAck_BufferFull_LogsDebugDrop pins the codex P2 fix for B4. deliverAck
// has TWO drop paths, but B4 logged only the no-waiter one (ch == nil). The second
// — a waiter IS installed yet its one-slot buffer is already full, so a duplicate
// or second ACK falls to the default branch — stayed silent, indistinguishable
// from a delivered ACK. It must now log at Debug AND be tellable apart from the
// no-waiter drop (distinct message), so the two drop causes can be counted
// separately in smd.log.
func TestDeliverAck_BufferFull_LogsDebugDrop(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")

	// A waiter is installed, but its one slot is already taken: the next ACK
	// cannot be delivered and must fall to the default branch.
	ch := make(chan bool, 1)
	ch <- true // occupy the single buffer slot
	s.mu.Lock()
	s.pendingAck = ch
	s.mu.Unlock()

	s.deliverAck(true)

	recs := matching(t, buf, "command buffer full")
	if len(recs) != 1 {
		t.Fatalf("buffer-full drop lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "debug" {
		t.Errorf("level = %q, want debug", lvl)
	}
	// The nearest confusable state: this is NOT the no-waiter drop.
	if got := countLines(buf, "no command waiting"); got != 0 {
		t.Fatalf("buffer-full drop reused the no-waiter message (%d lines); the two drops must differ", got)
	}
}

// TestUnsubscribe_IdempotentRecall_NoFalseCountChange pins the codex P2 fix for B6.
// The unsubscribe fn is idempotent, but before the fix a repeated call still ran
// publishClientCount at the UNCHANGED count and logged another "subscriber count
// changed" — a transition that never happened. publishClientCount is now
// edge-triggered (fires only when the count actually moved since the last
// broadcast), so the idempotent re-call records nothing new.
func TestUnsubscribe_IdempotentRecall_NoFalseCountChange(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")

	_, u1 := s.Subscribe() // n=1, below the fan-out threshold
	defer u1()
	_, u2 := s.Subscribe() // n=2, a real transition
	_, u3 := s.Subscribe() // n=3, a real transition
	defer u3()

	u2() // leave: count 3 -> 2, a real transition
	base := countLines(buf, "subscriber count changed")
	if base == 0 {
		t.Fatalf("expected count-change lines from the join/leave transitions; got 0\n%s", buf.String())
	}

	// The SAME unsubscribe again is idempotent — the count is still 2, so nothing
	// changed and no new record may appear.
	u2()
	if got := countLines(buf, "subscriber count changed"); got != base {
		t.Fatalf("idempotent unsubscribe re-call logged a false count change: %d lines, want %d", got, base)
	}
}
