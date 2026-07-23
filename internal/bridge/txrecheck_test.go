package bridge

// Alarm-recovery tests (2026-07-21 stuck-TX incident). The property under test
// is narrow and safety-critical: a standing alarm must be able to obtain fresh
// EVIDENCE, and must never be retirable without it.

import (
	"bytes"
	"context"
	stderr "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// txStatusQuery is what the FTdx10 rigdef encodes read_tx_status to. Asserted
// literally so a probe that ever wrote a KEY command instead would fail loudly
// rather than merely counting one write.
var txStatusQuery = []byte("TX;")

// newAlarmProbeService builds a command-test service wired for the re-probe
// loop: a cancellable runCtx (Start() supplies one in production) and a
// test-fast probe schedule.
//
// Teardown order is why this is ONE helper rather than two: the loop reads the
// schedule vars, so the test must cancel and WAIT for the goroutine before
// restoring them — a plain t.Cleanup on the vars alone races the still-running
// loop (caught by -race). Cancelling here also exercises the loop's ctx.Done
// exit, which is what keeps Stop()'s wg.Wait() from blocking on a probe
// schedule in production.
func newAlarmProbeService(t *testing.T, attempts int) (*Service, *fakeSerial) {
	t.Helper()
	delay, interval, max := txAlarmProbeDelay, txAlarmProbeInterval, txAlarmProbeAttempts
	stopN, stopEvery := txStopRetryAttempts, txStopRetryInterval
	txAlarmProbeDelay = 5 * time.Millisecond
	txAlarmProbeInterval = 5 * time.Millisecond
	txAlarmProbeAttempts = attempts
	// The re-unkey cadence is shortened HERE, not per-test: both background
	// loops read these package vars, and a test that registered its own
	// restore-cleanup would have it run BEFORE this helper's cancel+Wait (LIFO)
	// — restoring the vars while a goroutine still reads them, which -race
	// catches. One helper owning every knob keeps the teardown order correct.
	txStopRetryAttempts = 2
	txStopRetryInterval = 5 * time.Millisecond

	s, fake := newCommandTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()

	t.Cleanup(func() {
		cancel()
		s.wg.Wait() // the probe + re-unkey goroutines are registered on it
		txAlarmProbeDelay, txAlarmProbeInterval, txAlarmProbeAttempts = delay, interval, max
		txStopRetryAttempts, txStopRetryInterval = stopN, stopEvery
	})
	return s, fake
}

// countQueries reports how many of the recorded writes are the TX-status query,
// and whether anything OTHER than that query was written.
func countQueries(fake *fakeSerial) (queries int, foreign [][]byte) {
	for _, w := range fake.recordedWrites() {
		if bytes.Equal(w, txStatusQuery) {
			queries++
			continue
		}
		foreign = append(foreign, w)
	}
	return queries, foreign
}

// TestAlarmProbes_ReAskAutomatically is the core of the fix. Before it, a raised
// alarm was terminal in-process: confirmTxIdle needs an observed TXSTATUS, the
// only issuer of that query was an unkey, and every key/command path refuses
// while txUncertain — which the alarm holds. Raising the alarm must now put the
// query back on the wire by itself.
func TestAlarmProbes_ReAskAutomatically(t *testing.T) {
	s, fake := newAlarmProbeService(t, 3)

	s.raiseTxAlarm(TxAlarmStillKeyed)

	waitFor(t, func() bool {
		n, _ := countQueries(fake)
		return n >= 1
	}, "a raised alarm never re-asked the rig for its TX state")

	// Whatever it wrote, it must be the READ. A probe that keyed anything would
	// be a catastrophic bug in code that runs while the PTT state is unknown.
	if _, foreign := countQueries(fake); len(foreign) != 0 {
		t.Fatalf("alarm re-probe wrote non-query bytes %q — it must write the READ only", foreign)
	}
}

// TestAlarmProbes_StopOnConfirm: the loop is generation-gated, so the rig's
// "in RX" answer both clears the alarm and retires the probing.
func TestAlarmProbes_StopOnConfirm(t *testing.T) {
	s, fake := newAlarmProbeService(t, 100)

	s.raiseTxAlarm(TxAlarmUnconfirmed)
	waitFor(t, func() bool {
		n, _ := countQueries(fake)
		return n >= 1
	}, "no probe was sent")

	// The rig answers "0" = RX — the ONLY thing that may clear the alarm.
	s.observeTxStatus("0")
	if s.TxAlarmActive() {
		t.Fatal("a positive RX answer must clear the alarm")
	}
	if s.TxUncertain() {
		t.Fatal("a positive RX answer must clear tx-uncertainty")
	}

	settled, _ := countQueries(fake)
	time.Sleep(40 * time.Millisecond) // several intervals at the shortened cadence
	after, _ := countQueries(fake)
	if after != settled {
		t.Errorf("probes continued after the alarm cleared: %d → %d", settled, after)
	}
}

// TestAlarmProbes_BoundedNotEndless: probing is capped, so an alarm raised
// because the link died does not write into a dead port forever.
func TestAlarmProbes_BoundedNotEndless(t *testing.T) {
	s, fake := newAlarmProbeService(t, 3)

	s.raiseTxAlarm(TxAlarmUnconfirmed)
	waitFor(t, func() bool {
		n, _ := countQueries(fake)
		return n >= 3
	}, "the probe loop did not run its full bounded schedule")

	time.Sleep(40 * time.Millisecond)
	if n, _ := countQueries(fake); n > 3 {
		t.Errorf("probe count = %d, want no more than the 3-attempt cap", n)
	}
	// The alarm is untouched by the loop's expiry — it stands until evidence.
	if !s.TxAlarmActive() {
		t.Error("the alarm must survive the probe loop expiring")
	}
}

// TestAlarmProbes_SingleLoopPerRaise: a re-raise while already alarmed must not
// stack a second loop (which would double the probe rate for each re-raise).
func TestAlarmProbes_SingleLoopPerRaise(t *testing.T) {
	s, fake := newAlarmProbeService(t, 3)

	s.raiseTxAlarm(TxAlarmUnconfirmed)
	s.raiseTxAlarm(TxAlarmStillKeyed) // already alarmed — no new loop
	s.raiseTxAlarm(TxAlarmStillKeyed)

	time.Sleep(60 * time.Millisecond)
	if n, _ := countQueries(fake); n > 3 {
		t.Errorf("probe count = %d after three raises, want ≤ the 3-attempt cap of ONE loop", n)
	}
}

// TestStillKeyed_ReAssertsTheStop is the 2026-07-23 dogfood incident: a 2-second
// tune on 20m left the carrier up for two minutes because the rig answered "CAT
// TX still on" and the daemon's entire response was to raise a banner. Positive
// evidence that the transmitter is up when it should be down must produce more
// STOP attempts, not just a report.
func TestStillKeyed_ReAssertsTheStop(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)

	// The rig answers the post-unkey query with "still transmitting".
	s.mu.Lock()
	s.txUncertain = true
	s.mu.Unlock()
	s.observeTxStatus("1")

	waitFor(t, func() bool {
		for _, w := range fake.recordedWrites() {
			if bytes.Equal(w, []byte("TX0;")) {
				return true
			}
		}
		return false
	}, "the daemon never re-sent tx_off to a rig reporting itself still keyed")

	// The alarm and the TX block are NOT relaxed by retrying — only the rig's
	// own "in RX" answer may do that.
	if !s.TxAlarmActive() {
		t.Error("re-unkey must not clear the alarm")
	}
	if !s.TxUncertain() {
		t.Error("re-unkey must not clear tx-uncertainty")
	}
}

// TestStillKeyed_StopsOnceTheRigObeys: the retry loop is evidence-driven — once
// uncertainty clears (the rig confirmed RX, or the operator unkeyed at the
// radio) there is nothing left to stop, so it must not keep writing.
func TestStillKeyed_StopsOnceTheRigObeys(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	// A generous retry budget so "it stopped" is a real observation rather than
	// the loop simply running out. Safe to set here: the goroutine has not
	// started yet, and the helper's cleanup restores the original AFTER waiting
	// for it — which is precisely the ordering a per-test cleanup would break.
	txStopRetryAttempts = 20

	s.mu.Lock()
	s.txUncertain = true
	s.mu.Unlock()
	s.observeTxStatus("1")

	waitFor(t, func() bool {
		for _, w := range fake.recordedWrites() {
			if bytes.Equal(w, []byte("TX0;")) {
				return true
			}
		}
		return false
	}, "no re-unkey was sent")

	s.confirmTxIdle("test: rig obeyed")
	settled := len(fake.recordedWrites())
	time.Sleep(60 * time.Millisecond) // many intervals at the shortened cadence

	// Tolerance of exactly one: the loop re-checks uncertainty under keyMu, but a
	// confirmation landing between that check and the write still lets a single
	// already-committed unkey through. That cannot be closed without confirmTxIdle
	// taking keyMu, and it does not need to be — an extra tx_off to a rig already
	// in RX is a no-op. What matters, and what this asserts, is that the loop
	// STOPS rather than running its remaining 20 attempts.
	after := len(fake.recordedWrites())
	if after > settled+1 {
		t.Errorf("retries continued after the rig confirmed idle: %d → %d writes", settled, after)
	}
}

// TestStillKeyed_StopsWhenTheClientGoesAway: the retry sequence resolves the
// serial client per attempt, so a pipeline reconnect (or teardown) inside its
// ~1.6 s window ends it instead of writing into a closed port — and, because it
// ends, the single-flight latch frees for the replacement pipeline to start a
// clean sequence (2026-07-23 review).
func TestStillKeyed_StopsWhenTheClientGoesAway(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	txStopRetryAttempts = 20 // see StopsOnceTheRigObeys for why this is safe here

	s.mu.Lock()
	s.txUncertain = true
	s.mu.Unlock()
	s.observeTxStatus("1")

	waitFor(t, func() bool {
		for _, w := range fake.recordedWrites() {
			if bytes.Equal(w, []byte("TX0;")) {
				return true
			}
		}
		return false
	}, "no re-unkey was sent")

	// The pipeline REPLACES the client, as a reconnect does. Nil would be the
	// easy case; a live replacement is the one that slipped through when the
	// check was nil-only (review round 6) — the old sequence carried on writing
	// to the new connection while holding the single-flight latch.
	replacement := newFakeSerial()
	s.mu.Lock()
	s.activeClient = replacement
	s.mu.Unlock()

	time.Sleep(60 * time.Millisecond) // many intervals at the shortened cadence

	// Assert on the REPLACEMENT, not the old client. Writes to the old one stop
	// under either implementation — it is no longer s.activeClient — so watching
	// it proves nothing. The regression is the old sequence spending its budget
	// on the NEW connection.
	if n := len(replacement.recordedWrites()); n != 0 {
		t.Errorf("the previous connection's retry sequence wrote %d time(s) to the "+
			"replacement client — it must end when the client changes, not follow it", n)
	}
	// The latch must be free again so the next pipeline can retry.
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.txStopRetrying
	}, "the single-flight latch stayed held after the sequence ended")
}

// TestRecheckTx_AsksButCannotClear pins the safety contract: the manual
// re-check may ask, and may ONLY ask. It must work while the alarm holds (every
// other write path is gated shut at that moment) and must leave both the alarm
// and the uncertainty flag exactly as it found them.
func TestRecheckTx_AsksButCannotClear(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)

	s.raiseTxAlarm(TxAlarmStillKeyed)
	waitFor(t, func() bool { n, _ := countQueries(fake); return n >= 1 }, "no initial probe")
	before, _ := countQueries(fake)

	if err := s.RecheckTx(); err != nil {
		t.Fatalf("RecheckTx while alarmed must be permitted: %v", err)
	}

	after, _ := countQueries(fake)
	if after <= before {
		t.Errorf("RecheckTx wrote no query (%d → %d)", before, after)
	}
	if _, foreign := countQueries(fake); len(foreign) != 0 {
		t.Fatalf("RecheckTx wrote non-query bytes %q — READ only", foreign)
	}
	// The whole point: asking is not evidence.
	if !s.TxAlarmActive() {
		t.Error("RecheckTx cleared the alarm — only the rig's answer may do that")
	}
	if !s.TxUncertain() {
		t.Error("RecheckTx cleared tx-uncertainty — that would re-enable keying over a live PTT")
	}
}

// TestRecheckTx_GatedOnConnectionAndIdentity: a query is harmless in itself, but
// its ANSWER is read as this rig's TX state — so an unidentified rig must not be
// asked, exactly as the write paths refuse it.
func TestRecheckTx_GatedOnConnectionAndIdentity(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)

	s.mu.Lock()
	s.identityConfirmed = false
	s.mu.Unlock()
	if err := s.RecheckTx(); !stderr.Is(err, ErrRigIdentityUnverified) {
		t.Errorf("RecheckTx on an unverified rig = %v, want ErrRigIdentityUnverified", err)
	}

	s.mu.Lock()
	s.identityConfirmed = true
	s.activeClient = nil
	s.mu.Unlock()
	if err := s.RecheckTx(); !stderr.Is(err, ErrRigNotConnected) {
		t.Errorf("RecheckTx with no client = %v, want ErrRigNotConnected", err)
	}

	if n := len(fake.recordedWrites()); n != 0 {
		t.Errorf("%d writes reached a rig that should not have been asked, want 0", n)
	}
}

// TestAlarmProbes_ReplyStillConfirmsDuringAlarm is the end-to-end recovery this
// whole feature exists for: an alarm stands, the loop re-asks, the rig answers
// "in RX", the alarm retires. It is also the guard that killed the reverted
// reply-counting design — that scheme discarded exactly this answer after a
// reconnect, leaving TX blocked on a healthy rig (see the KNOWN LIMITATION note
// in observeTxStatus).
func TestAlarmProbes_ReplyStillConfirmsDuringAlarm(t *testing.T) {
	s, fake := newAlarmProbeService(t, 5)
	def, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("rigdef lookup failed")
	}

	s.beginTxConfirm(def, fake) // query 1 — goes unanswered
	s.raiseTxAlarm(TxAlarmUnconfirmed)
	waitFor(t, func() bool {
		n, _ := countQueries(fake)
		return n >= 2 // the cycle's query plus at least one re-probe
	}, "the alarm did not re-probe")

	s.observeTxStatus("0")
	if s.TxAlarmActive() || s.TxUncertain() {
		t.Fatalf("a probe answer must clear the alarm: alarm=%v uncertain=%v",
			s.TxAlarmActive(), s.TxUncertain())
	}
}

// TestAlarmProbes_StopDoesNotRaceWaitGroup exercises the review's P2 shape:
// alarms raised concurrently with Stop, where registering the probe goroutine
// after releasing the lock that observed s.stopped allowed wg.Add to run once
// Stop had already begun wg.Wait() — WaitGroup misuse (panic, or a Stop that
// returns with the goroutine still live).
//
// Honest about its power: this is a SMOKE test, not a deterministic guard. The
// bad interleaving needs Stop's Wait to observe a zero counter in the exact
// window between the unlock and the Add, and re-running it against the broken
// ordering did NOT reliably fail. The actual guarantee is structural — the Add
// now happens under the same mutex Stop uses to publish `stopped`, so the two
// orderings are mutually exclusive by construction (see startAlarmProbes). Keep
// this for the churn coverage under -race; do not rely on it to catch a revert.
func TestAlarmProbes_StopDoesNotRaceWaitGroup(t *testing.T) {
	s, _ := newAlarmProbeService(t, 100)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			s.raiseTxAlarm(TxAlarmUnconfirmed)
			s.confirmTxIdle("test churn") // clear so the next raise is an edge
		}
	}()

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-done

	// Post-Stop raises must not register anything new on the WaitGroup.
	s.raiseTxAlarm(TxAlarmUnconfirmed)
}

// TestRecheckTx_ClearsWhenTheRigAnswersIdle is the end-to-end recovery the
// incident wanted: operator re-checks, the rig answers RX, the banner retires.
func TestRecheckTx_ClearsWhenTheRigAnswersIdle(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)

	s.raiseTxAlarm(TxAlarmStillKeyed)
	if err := s.RecheckTx(); err != nil {
		t.Fatalf("RecheckTx: %v", err)
	}
	// The readLoop would deliver this from the rig's answer frame.
	s.observeTxStatus("0")

	if s.TxAlarmActive() || s.TxUncertain() {
		t.Fatalf("alarm=%v uncertain=%v after an RX answer, want both false",
			s.TxAlarmActive(), s.TxUncertain())
	}
}
