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
	txAlarmProbeDelay = 5 * time.Millisecond
	txAlarmProbeInterval = 5 * time.Millisecond
	txAlarmProbeAttempts = attempts

	s, fake := newCommandTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()

	t.Cleanup(func() {
		cancel()
		s.wg.Wait() // the probe goroutine is registered on it
		txAlarmProbeDelay, txAlarmProbeInterval, txAlarmProbeAttempts = delay, interval, max
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
