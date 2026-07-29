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
	delay, interval, maxAttempts := txAlarmProbeDelay, txAlarmProbeInterval, txAlarmProbeAttempts
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
		txAlarmProbeDelay, txAlarmProbeInterval, txAlarmProbeAttempts = delay, interval, maxAttempts
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

// countUnkeys reports how many recorded writes are the re-unkey (TX0;) — the
// actual stop attempt — as distinct from the status query the retry loop sends
// right after each one. Tests that reason about "did the retry loop stop" must
// count these, not every write, or a single racing iteration (which emits an
// unkey AND a follow-up query) looks like two retries.
func countUnkeys(fake *fakeSerial) int {
	n := 0
	for _, w := range fake.recordedWrites() {
		if bytes.Equal(w, []byte("TX0;")) {
			n++
		}
	}
	return n
}

func countCIVUnkeys(fake *fakeSerial) int {
	n := 0
	for _, w := range fake.recordedWrites() {
		if bytes.Contains(w, []byte{0x1C, 0x00, 0x00}) {
			n++
		}
	}
	return n
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

	// The loop is generation-gated, so clearing the alarm retires it — but a
	// probe goroutine that ALREADY passed the gate still has Lookup+Encode+Write
	// ahead of it and can land one more query afterwards. That straggler is
	// harmless (a read-only status query) and eliminating it would mean adding
	// synchronisation to a safety path purely to satisfy a test.
	//
	// Demanding an exact count therefore raced the straggler and failed ~7% of
	// runs (measured 2026-07-29: 2/30 under -race). The probe loop is a SINGLE
	// goroutine, so at most ONE probe can be in flight — which makes "no more
	// than one further query" a principled allowance rather than a fudge, and
	// still catches a live loop, which at the 5 ms test cadence would add
	// roughly eight in this window.
	//
	// Deliberately not a wait-for-quiescence loop: with txAlarmProbeAttempts
	// capped, an UNRETIRED loop also goes quiet on its own once it exhausts the
	// cap, so "eventually stable" would pass against a broken gate.
	const window = 40 * time.Millisecond // several intervals at the shortened cadence
	settled, _ := countQueries(fake)
	time.Sleep(window)
	after, _ := countQueries(fake)
	if after > settled+1 {
		t.Errorf("probes continued after the alarm cleared: %d → %d in %s (at most one in-flight straggler is allowed)",
			settled, after, window)
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
	settled := countUnkeys(fake)
	time.Sleep(60 * time.Millisecond) // many intervals at the shortened cadence

	// Count re-unkeys (TX0;), NOT every recorded write. Each retry iteration writes
	// the unkey AND a follow-up status query (probeTxStatusOn); counting both makes
	// a single iteration that passed the keyMu re-check just before the confirmation
	// landed look like TWO retries (unkey + its query), which under -race on a loaded
	// CI runner is exactly the intermittent 1→3 flake this test used to have.
	//
	// Tolerance of exactly one UNKEY is right and deterministic: the loop is
	// sequential and re-checks uncertainty under keyMu, so at most one
	// already-committed TX0; slips through — and its follow-up query is a harmless
	// READ, not another stop attempt. Closing even that one would need confirmTxIdle
	// to take keyMu, which it must not. What this asserts is that the loop STOPS
	// rather than running its remaining 20 attempts.
	after := countUnkeys(fake)
	if after > settled+1 {
		t.Errorf("re-unkeys continued after the rig confirmed idle: %d → %d TX0; writes", settled, after)
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
	//
	// Count UNKEYS ONLY, not all writes. Raising the alarm also starts the probe
	// loop, which deliberately re-resolves s.activeClient every attempt — asking
	// a replacement connection what the rig is doing is its whole purpose. A
	// bare write count made a legitimate query look like the defect and failed
	// ~10% of race-enabled runs, which is what had CI intermittently red
	// (2026-07-23). The defect this test exists for is an inherited TX0;.
	var unkeys int
	for _, w := range replacement.recordedWrites() {
		if bytes.Equal(w, []byte("TX0;")) {
			unkeys++
		}
	}
	if unkeys != 0 {
		t.Errorf("the previous connection's retry sequence sent %d unkey(s) to the "+
			"replacement client — it must end when the client changes, not follow it", unkeys)
	}
	// The latch must be free again so the next pipeline can retry.
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.txStopRetrying
	}, "the single-flight latch stayed held after the sequence ended")
}

// A retry result belongs to the pipeline that wrote it. Pipeline teardown also
// takes keyMu, so it must not be able to acquire that lifecycle lock before the
// retry has armed its confirmation state; otherwise a replacement pipeline can
// begin defensive recovery and then be overwritten by the old result.
func TestStillKeyed_AppliesConfirmationBeforeReleasingConnection(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	writeStarted := make(chan struct{})
	allowWrite := make(chan struct{})
	fake.onWrite = func(w []byte) []byte {
		if !bytes.Equal(w, []byte("TX0;")) {
			return nil
		}
		select {
		case <-writeStarted:
			return nil // later attempts are not part of this ordering test
		default:
			close(writeStarted)
			<-allowWrite
			return nil
		}
	}

	s.mu.Lock()
	s.txUncertain = true
	s.mu.Unlock()
	s.retryUnkeyStillKeyed()

	select {
	case <-writeStarted:
	case <-time.After(time.Second):
		t.Fatal("retry never reached tx_off")
	}

	// Model pipeline teardown waiting to take ownership of the connection.
	// When it acquires keyMu, the retry's confirmation cycle must already exist.
	observedGen := make(chan uint64, 1)
	go func() {
		s.keyMu.Lock()
		s.mu.Lock()
		gen := s.txConfirmGen
		s.mu.Unlock()
		s.keyMu.Unlock()
		observedGen <- gen
	}()
	close(allowWrite)

	select {
	case gen := <-observedGen:
		if gen == 0 {
			t.Fatal("pipeline lifecycle lock was released before retry confirmation was armed")
		}
	case <-time.After(time.Second):
		t.Fatal("pipeline lifecycle waiter never acquired keyMu")
	}
	s.confirmTxIdle("test cleanup")
}

// CI-V has no read_tx_status command. Its automatic alarm recovery therefore
// re-asserts the intrinsically safe tx_off and requires the addressed rig's FB
// ACK before clearing uncertainty.
func TestAlarmProbes_CIVRecoversThroughAckedUnkey(t *testing.T) {
	delay, interval, attempts := txAlarmProbeDelay, txAlarmProbeInterval, txAlarmProbeAttempts
	txAlarmProbeDelay = 5 * time.Millisecond
	txAlarmProbeInterval = 5 * time.Millisecond
	txAlarmProbeAttempts = 3
	defer func() {
		txAlarmProbeDelay, txAlarmProbeInterval, txAlarmProbeAttempts = delay, interval, attempts
	}()

	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup() // runs before the schedule restore above (LIFO)
	before := countCIVUnkeys(fake)

	s.raiseTxAlarm(TxAlarmUnconfirmed)
	waitFor(t, func() bool {
		return countCIVUnkeys(fake) > before && !s.TxAlarmActive() && !s.TxUncertain()
	}, "CI-V alarm recovery did not obtain a tx_off ACK")
}

// The bounded automatic attempts may expire while a radio remains powered off.
// RecheckTx must still provide a safe later recovery on the same connection
// instead of returning ErrTxRecheckUnsupported forever.
func TestRecheckTx_CIVRecoversAfterAutomaticWindow(t *testing.T) {
	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()
	before := countCIVUnkeys(fake)

	// Model an alarm whose bounded automatic loop has already ended.
	s.mu.Lock()
	s.txAlarmActive = true
	s.txUncertain = true
	s.txAlarmProbeGen++
	s.mu.Unlock()

	if err := s.RecheckTx(); err != nil {
		t.Fatalf("RecheckTx on CI-V alarm: %v", err)
	}
	if countCIVUnkeys(fake) <= before {
		t.Fatal("CI-V RecheckTx did not send tx_off")
	}
	if s.TxAlarmActive() || s.TxUncertain() {
		t.Fatalf("ACKed CI-V recheck did not clear alarm: alarm=%v uncertain=%v",
			s.TxAlarmActive(), s.TxUncertain())
	}
}

// The endpoint is always registered, not only while an alarm banner is visible.
// A direct call during a healthy IC-7300 FT8 transmission must be a no-op:
// sending tx_off would truncate the slot while ft8TxActive and its timer stayed
// armed.
func TestRecheckTx_CIVDoesNotInterruptHealthyFT8(t *testing.T) {
	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()
	before := countCIVUnkeys(fake)

	s.mu.Lock()
	s.ft8TxActive = true
	s.txUncertain = false
	s.txAlarmActive = false
	s.mu.Unlock()

	if err := s.RecheckTx(); err != nil {
		t.Fatalf("RecheckTx during healthy CI-V FT8 TX: %v", err)
	}
	if got := countCIVUnkeys(fake); got != before {
		t.Fatalf("healthy CI-V FT8 TX was interrupted: tx_off count %d → %d", before, got)
	}
	s.mu.Lock()
	active := s.ft8TxActive
	s.ft8TxActive = false // keep fixture cleanup from treating this as a real TX
	s.mu.Unlock()
	if !active {
		t.Fatal("healthy FT8 controller state was unexpectedly cleared")
	}
}

// An automatic probe validates its generation before calling the protocol
// recovery, but it can then wait behind keyMu while the alarm clears and a new
// transmission starts. The protocol operation must revalidate after acquiring
// keyMu rather than acting on the stale earlier decision.
func TestCIVRecovery_RevalidatesAfterWaitingForKeyMu(t *testing.T) {
	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()
	before := countCIVUnkeys(fake)

	s.mu.Lock()
	s.txAlarmActive = true
	s.txUncertain = true
	s.mu.Unlock()

	s.keyMu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- s.probeTxRecovery("stale automatic probe")
	}()

	// The old alarm is resolved and a new, healthy controller owns PTT before
	// the queued recovery operation gets the lifecycle lock.
	s.mu.Lock()
	s.txAlarmActive = false
	s.txUncertain = false
	s.ft8TxActive = true
	s.mu.Unlock()
	s.keyMu.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stale CI-V recovery: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale CI-V recovery did not finish")
	}
	if got := countCIVUnkeys(fake); got != before {
		t.Fatalf("stale recovery interrupted new FT8 TX: tx_off count %d → %d", before, got)
	}
	s.mu.Lock()
	active := s.ft8TxActive
	s.ft8TxActive = false
	s.mu.Unlock()
	if !active {
		t.Fatal("new FT8 controller state was unexpectedly cleared")
	}
}

// Pressing Re-check is not an operator override. If CI-V never ACKs the safe
// tx_off, the request fails and both the warning and TX gate remain latched.
func TestRecheckTx_CIVMissingAckKeepsAlarm(t *testing.T) {
	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()
	s.civAckTimeout = 20 * time.Millisecond
	fake.mu.Lock()
	fake.onWrite = func([]byte) []byte { return nil }
	fake.mu.Unlock()

	s.mu.Lock()
	s.txAlarmActive = true
	s.txUncertain = true
	s.mu.Unlock()

	err := s.RecheckTx()
	if !stderr.Is(err, ErrCommandNoAck) {
		t.Fatalf("RecheckTx without CI-V ACK = %v, want ErrCommandNoAck", err)
	}
	if !s.TxAlarmActive() || !s.TxUncertain() {
		t.Fatalf("missing ACK cleared safety state: alarm=%v uncertain=%v",
			s.TxAlarmActive(), s.TxUncertain())
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
