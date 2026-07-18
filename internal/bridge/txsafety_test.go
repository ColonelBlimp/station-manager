package bridge

// TX-safety companion batch (2026-07-18 review, findings 3/5/6/7): the
// strike-aware write predicate, the generation-gated auto-off backstops (the
// previously-uncovered callbacks), the CI-V keyed-snapshot skip, and
// unrecognised-then-valid identity recovery.

import (
	"context"
	stderr "errors"
	"testing"
	"time"
)

// Finding 3: every mutating entry point refuses a rig the liveness strikes
// already read as non-responsive — before this, a 2-strike "dead" rig was
// still keyable and commandable.
func TestWriteGates_RefuseAtStrikeLimit(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.lastMode = "USB"
	s.lastPower = 35
	s.noDataStrikes.Store(noDataStrikeLimit)

	if s.RigConnected() {
		t.Fatal("RigConnected must be false at the strike limit")
	}
	if s.TxReady() {
		t.Fatal("TxReady must be false at the strike limit")
	}
	if err := s.SendCommand(context.Background(), "set_freq", "14074000"); !stderr.Is(err, ErrRigNotConnected) {
		t.Fatalf("SendCommand at strike limit = %v, want ErrRigNotConnected", err)
	}
	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); !stderr.Is(err, ErrRigNotConnected) {
		t.Fatalf("KeyFt8Tx at strike limit = %v, want ErrRigNotConnected", err)
	}
	if err := s.StartTune(context.Background()); !stderr.Is(err, ErrRigNotConnected) {
		t.Fatalf("StartTune at strike limit = %v, want ErrRigNotConnected", err)
	}
	if n := len(fake.recordedWrites()); n != 0 {
		t.Fatalf("%d writes reached a struck-out rig, want 0", n)
	}

	// A recovered rig (strikes reset by a successful read) is writable again.
	s.noDataStrikes.Store(0)
	if !s.TxReady() {
		t.Fatal("TxReady must recover once the strikes reset")
	}
}

// Finding 6: an auto-off callback armed for an OLD transmission must not
// release (or re-arm against) a NEWER one. Timer.Stop can't cancel an
// already-executing callback; the generation gate is what makes that safe.
func TestFt8TxAutoOff_StaleGenerationNoOp(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.tuneRestoreSettle = 0

	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("first key: %v", err)
	}
	s.mu.Lock()
	oldGen := s.ft8TxGen
	s.mu.Unlock()

	// The first transmission ends; the rig answers the status query with RX
	// (ADR 0051 — a new key is refused while the unkey is unconfirmed); then
	// a second transmission begins (a new generation).
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("unkey: %v", err)
	}
	s.observeTxStatus("0")
	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("second key: %v", err)
	}
	before := len(fake.recordedWrites())

	// The stale callback (armed for the first transmission) fires late.
	s.ft8TxAutoOff(oldGen)

	s.mu.Lock()
	active := s.ft8TxActive
	s.mu.Unlock()
	if !active {
		t.Fatal("stale auto-off callback unkeyed the NEWER transmission")
	}
	if n := len(fake.recordedWrites()); n != before {
		t.Fatalf("stale callback wrote %d frame(s) to the wire, want none", n-before)
	}
}

// The current-generation callback IS the backstop — it must release. (The
// review measured ft8TxAutoOff at 0% coverage; this is its direct test.)
func TestFt8TxAutoOff_CurrentGenerationReleases(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.tuneRestoreSettle = 0

	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("key: %v", err)
	}
	s.mu.Lock()
	gen := s.ft8TxGen
	s.mu.Unlock()

	s.ft8TxAutoOff(gen)

	s.mu.Lock()
	active := s.ft8TxActive
	s.mu.Unlock()
	if active {
		t.Fatal("current-generation auto-off did not release PTT")
	}
	writes := fake.recordedWrites()
	if len(writes) < 2 || string(writes[1]) != "TX0;" {
		t.Fatalf("auto-off writes = %q, want a TX0; unkey", writes)
	}
}

// Finding 6, tune flavour: stale no-op + current releases.
func TestTuneAutoOff_GenerationGate(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.tuneRestoreSettle = 0
	s.lastMode = "USB"
	s.lastPower = 35

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("first tune: %v", err)
	}
	s.mu.Lock()
	oldGen := s.tuneGen
	s.mu.Unlock()
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	s.observeTxStatus("0") // rig confirms RX — unblocks the next key (ADR 0051)
	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("second tune: %v", err)
	}
	before := len(fake.recordedWrites())

	s.tuneAutoOff(oldGen) // stale — must not touch the new carrier

	s.mu.Lock()
	active := s.tuneActive
	gen := s.tuneGen
	s.mu.Unlock()
	if !active {
		t.Fatal("stale tune auto-off unkeyed the NEWER carrier")
	}
	if n := len(fake.recordedWrites()); n != before {
		t.Fatalf("stale callback wrote %d frame(s), want none", n-before)
	}

	s.tuneAutoOff(gen) // current — the backstop must fire
	s.mu.Lock()
	active = s.tuneActive
	s.mu.Unlock()
	if active {
		t.Fatal("current-generation tune auto-off did not drop the carrier")
	}
}

// Finding 5: a CI-V bootstrap snapshot is skipped while TX is keyed — the
// multi-frame sequence would hold cmdMu for seconds directly ahead of an
// emergency tx_off on the same mutex.
func TestTriggerBootstrap_SkipsCIVWhileKeyed(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.mu.Lock()
	s.bootstrapBytes = []byte{0xFE, 0xFE, 0x94, 0xE0, 0x03, 0xFD}
	s.bootstrapCIV = true
	s.ft8TxActive = true
	s.mu.Unlock()

	if err := s.TriggerBootstrap(context.Background()); err != nil {
		t.Fatalf("TriggerBootstrap: %v", err)
	}
	if n := len(fake.recordedWrites()); n != 0 {
		t.Fatalf("bootstrap wrote %d frame(s) while keyed, want 0 (deferred)", n)
	}

	// Unkeyed, the snapshot flows again.
	s.mu.Lock()
	s.ft8TxActive = false
	s.mu.Unlock()
	_ = s.TriggerBootstrap(context.Background())
	if n := len(fake.recordedWrites()); n == 0 {
		t.Fatal("bootstrap wrote nothing once unkeyed")
	}
}

// Finding 7: a garbled/unmapped first IDENTITY must not permanently
// write-block the instance — a later exact-match frame re-classifies and
// confirms.
func TestReadLoop_UnrecognisedIdentityThenValidConfirms(t *testing.T) {
	s, fake := newPipelineTestService(t) // FT-710
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	fake.feedLine([]byte("ID9999")) // unmapped → unrecognised, NOT latched
	time.Sleep(50 * time.Millisecond)
	if s.identityOK() {
		t.Fatal("identity confirmed from an unrecognised ID")
	}

	fake.feedLine([]byte("ID0800")) // the real rig answers — must confirm now
	deadline := time.Now().Add(time.Second)
	for !s.identityOK() {
		if time.Now().After(deadline) {
			t.Fatal("a valid IDENTITY after an unrecognised one did not confirm (instance poisoned)")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ADR 0051 end-to-end recovery: a standing tx-alarm from a prior life clears
// through the REAL pipeline — identity confirms, the unconditional defensive
// unkey fires, the TX-status query goes out, the rig answers RX, the alarm
// drops. This exercises the rigdef's read_tx_status command + TXSTATUS decode.
func TestTxAlarm_RecoveryCycleThroughPipeline(t *testing.T) {
	s, fake := newPipelineTestService(t)       // FT-710 (has read_tx_status)
	s.raiseTxAlarm(TxAlarmTeardownUnconfirmed) // prior life left the rig possibly keyed

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	fake.feedLine([]byte("ID0800")) // identity confirms → defensive unkey + query
	deadline := time.Now().Add(time.Second)
	for {
		writes := fake.recordedWrites()
		var sawTxOff, sawQuery bool
		for _, w := range writes {
			switch string(w) {
			case "TX0;":
				sawTxOff = true
			case "TX;":
				sawQuery = true
			}
		}
		if sawTxOff && sawQuery {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("defensive unkey + status query not written within 1s (writes: %q)", writes)
		}
		time.Sleep(5 * time.Millisecond)
	}

	fake.feedLine([]byte("TX0")) // the rig answers: RX — the alarm must clear
	deadline = time.Now().Add(time.Second)
	for s.TxUncertain() {
		if time.Now().After(deadline) {
			t.Fatal("alarm/uncertainty did not clear on a positive RX answer")
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	alarmed := s.txAlarmActive
	s.mu.Unlock()
	if alarmed {
		t.Fatal("txAlarmActive still set after RX confirmation")
	}
}
