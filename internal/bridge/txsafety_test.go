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

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// waitFor polls cond up to a second — the async defensive-recovery goroutine
// (ADR 0051) makes several fixtures eventually-consistent.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

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
	t.Cleanup(answerTxStatusQueries(s, fake)) // healthy rig: confirm-gate passes
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
	t.Cleanup(answerTxStatusQueries(s, fake)) // healthy rig: confirm-gate passes
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
	t.Cleanup(answerTxStatusQueries(s, fake)) // healthy rig: confirm-gate passes
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

// a1d031cf review residual: the auto-off callbacks' cheap generation
// pre-check runs OUTSIDE keyMu — a stop + new key completing in that gap must
// still be caught by the release-level recheck UNDER the lock.
func TestReleaseChecked_StaleGenerationUnderKeyMuNoOp(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake)) // healthy rig: confirm-gate passes
	s.tuneRestoreSettle = 0
	s.lastMode = "USB"
	s.lastPower = 35

	// Tune 1 (its generation is what the stale callback would carry).
	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("first tune: %v", err)
	}
	s.mu.Lock()
	staleGen := s.tuneGen
	s.mu.Unlock()
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	s.observeTxStatus("0")
	// Tune 2 is live; the stale callback's release arrives late.
	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("second tune: %v", err)
	}
	before := len(fake.recordedWrites())

	if err := s.releaseTuneChecked(context.Background(), "stale-test", &staleGen); err != nil {
		t.Fatalf("stale release must no-op cleanly, got %v", err)
	}
	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if !active {
		t.Fatal("stale-generation release unkeyed the NEWER tune under keyMu")
	}
	if n := len(fake.recordedWrites()); n != before {
		t.Fatalf("stale release wrote %d frame(s), want none", n-before)
	}

	// The ft8 twin.
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("cleanup stop: %v", err)
	}
	s.observeTxStatus("0")
	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("ft8 key 1: %v", err)
	}
	s.mu.Lock()
	staleFt8 := s.ft8TxGen
	s.mu.Unlock()
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("ft8 unkey: %v", err)
	}
	s.observeTxStatus("0")
	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("ft8 key 2: %v", err)
	}
	before = len(fake.recordedWrites())
	if err := s.releaseFt8TxChecked(context.Background(), "stale-test", &staleFt8); err != nil {
		t.Fatalf("stale ft8 release must no-op cleanly, got %v", err)
	}
	s.mu.Lock()
	active = s.ft8TxActive
	s.mu.Unlock()
	if !active {
		t.Fatal("stale-generation release unkeyed the NEWER ft8 TX under keyMu")
	}
	if n := len(fake.recordedWrites()); n != before {
		t.Fatalf("stale ft8 release wrote %d frame(s), want none", n-before)
	}
}

// a1d031cf review low: a cached identity_unrecognised bridge-error must retire
// once identity later confirms — a new tab must not be toasted a stale warning
// after the recovery finding 7 enabled.
func TestIdentityCacheClearsOnConfirmation(t *testing.T) {
	s, fake := newPipelineTestService(t) // FT-710
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	fake.feedLine([]byte("ID9999")) // unrecognised → error published + cached
	time.Sleep(50 * time.Millisecond)
	fake.feedLine([]byte("ID0800")) // recovery: identity confirms
	deadline := time.Now().Add(time.Second)
	for !s.identityOK() {
		if time.Now().After(deadline) {
			t.Fatal("identity did not confirm")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A NEW subscriber must not receive the stale cached identity error.
	ch, unsub := s.Subscribe()
	defer unsub()
	timeout := time.After(200 * time.Millisecond)
	for {
		select {
		case evt := <-ch:
			if evt.Name == EventBridgeError {
				if p, ok := evt.Payload.(BridgeErrorPayload); ok && p.Code == BridgeErrCodeIdentityUnrecognised {
					t.Fatal("stale identity_unrecognised replayed to a new subscriber after confirmation")
				}
			}
		case <-timeout:
			return // no stale replay — pass
		}
	}
}

// TestDefensiveRecovery_FreshProcessModelsRestart is the 8bd88c1b review's
// core demand: a FRESH Service — no seeded alarm, no uncertainty, exactly what
// a restarted daemon looks like — must still run the full defensive recovery
// on its first confirmed connection: TX0 + status query out, TX blocked until
// the rig positively answers RX. (The earlier recovery test seeded the alarm
// in-process, which validated the alarm-carry path but not the restart.)
func TestDefensiveRecovery_FreshProcessModelsRestart(t *testing.T) {
	s, fake := newPipelineTestService(t) // FT-710 — completely fresh state
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	fake.feedLine([]byte("ID0800")) // identity confirms → unconditional recovery
	waitFor(t, func() bool {
		var sawTxOff, sawQuery bool
		for _, w := range fake.recordedWrites() {
			switch string(w) {
			case "TX0;":
				sawTxOff = true
			case "TX;":
				sawQuery = true
			}
		}
		return sawTxOff && sawQuery
	}, "fresh-process recovery did not write TX0 + the status query")

	if !s.TxUncertain() {
		t.Fatal("fresh-process recovery must hold TX blocked until the rig confirms")
	}
	// A key attempted mid-recovery is refused — the ordering guarantee.
	s.lastMode = "USB"
	s.lastPower = 35
	if err := s.StartTune(context.Background()); !stderr.Is(err, ErrTxUncertain) {
		t.Fatalf("StartTune during fresh recovery = %v, want ErrTxUncertain", err)
	}

	fake.feedLine([]byte("TX0")) // the rig answers RX
	waitFor(t, func() bool { return !s.TxUncertain() },
		"fresh-process recovery did not confirm on the RX answer")
}

// TestDefensiveRecovery_SilentRigAlarms: the restart shape where the defensive
// TX0/query vanish into a dead write path — the confirm timeout must alarm and
// TX must stay blocked (before the 8bd88c1b fix this produced nothing at all).
func TestDefensiveRecovery_SilentRigAlarms(t *testing.T) {
	prev := txConfirmTimeout
	txConfirmTimeout = 50 * time.Millisecond
	defer func() { txConfirmTimeout = prev }()

	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	fake.feedLine([]byte("ID0800")) // recovery fires; the rig never answers
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.txAlarmActive
	}, "unanswered fresh-process recovery must raise the tx-alarm")
	if !s.TxUncertain() {
		t.Fatal("TX must stay blocked while the recovery is unconfirmed")
	}
}

// SendCommands must treat an UNCONFIRMED transmission like an active one
// (8bd88c1b review): the active flags cleared but the PTT may still be up.
func TestSendCommands_RefusedWhileUncertain(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.mu.Lock()
	s.txUncertain = true
	s.mu.Unlock()
	if err := s.SendCommand(context.Background(), "set_freq", "14074000"); !stderr.Is(err, ErrTxUncertain) {
		t.Fatalf("SendCommand while uncertain = %v, want ErrTxUncertain", err)
	}
	if n := len(fake.recordedWrites()); n != 0 {
		t.Fatalf("%d command writes reached a possibly-transmitting rig, want 0", n)
	}
}

// The any-rig-data fallback may only confirm on frames decoded AFTER the
// confirmation cycle began — a pre-unkey frame proves nothing (8bd88c1b
// review, ordering finding).
func TestObserveRigData_WatermarkGuard(t *testing.T) {
	s, _ := newCommandTestService(t)
	def, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("rigdef missing")
	}
	s.rxFrameCount.Store(7) // frames seen BEFORE the unkey
	s.beginTxConfirm(def, nil)

	s.observeRigData() // same watermark — must NOT confirm
	if !s.TxUncertain() {
		t.Fatal("a pre-cycle frame count must not confirm the unkey")
	}
	s.rxFrameCount.Add(1) // a frame decoded AFTER the cycle began
	s.observeRigData()
	if s.TxUncertain() {
		t.Fatal("a post-cycle frame must confirm (no-query fallback)")
	}
}

// The any-rig-data fallback must stay DISARMED while the confirmation watermark
// holds the disarm sentinel — the state beginDefensiveRecovery uses for its
// committed-but-unwritten window. Without it, a frame queued behind the one that
// triggered recovery (the CI-V initial multi-frame READ) would confirm the stop
// through observeRigData before the defensive tx_off had gone out, exposing
// TxReady with the unkey still in flight (50e35d review P1). The recovery
// goroutine re-arms the watermark to a real count once the unkey is on the wire.
func TestObserveRigData_SentinelDisarmsFallback(t *testing.T) {
	s, _ := newCommandTestService(t)
	s.mu.Lock()
	s.txUncertain = true
	s.hasTxStatusQuery = false // no-query def → the any-rig-data fallback is live
	s.txConfirmAfterFrame = confirmFallbackDisarmed
	s.mu.Unlock()

	s.rxFrameCount.Store(100) // frames keep streaming during the pre-write window
	s.observeRigData()
	if !s.TxUncertain() {
		t.Fatal("observeRigData confirmed while the watermark held the disarm sentinel — " +
			"a pre-write frame must not confirm an unkey that has not left the host")
	}

	// Re-armed to the current count, exactly as the recovery goroutine does once
	// the unkey has been written: a genuinely-later frame may now confirm.
	s.mu.Lock()
	s.txConfirmAfterFrame = 100
	s.mu.Unlock()
	s.rxFrameCount.Store(101)
	s.observeRigData()
	if s.TxUncertain() {
		t.Fatal("observeRigData failed to confirm after the fallback was re-armed post-write")
	}
}

// A status answer arriving while nothing is uncertain must stay INERT — it may
// not key, unkey, alarm, or confirm anything. It is only recorded, so the
// transition can be logged: with txUncertain false the value was previously
// read and dropped, which hid a rig reporting "2" (transmitting by other
// means) at rest — the 2026-07-23 stuck-tune signature, where an asserted RTS
// line held data-mode PTT down for the whole connection.
func TestObserveTxStatus_IdleObservationIsInert(t *testing.T) {
	s, fake := newCommandTestService(t)

	s.observeTxStatus("2") // rig claims TX by other means; we believe we are idle

	if s.TxUncertain() {
		t.Fatal("an unsolicited status answer must not make the TX state uncertain")
	}
	if s.TxAlarmActive() {
		t.Fatal("an unsolicited status answer must not raise the operator alarm")
	}
	if n := len(fake.recordedWrites()); n != 0 {
		t.Fatalf("observation wrote %d frame(s) to the rig, want 0", n)
	}

	// Recorded, so a repeat is a non-transition and the log stays readable
	// across a long FT8 session (TXSTATUS also arrives on AUTO-mode pushes).
	s.mu.Lock()
	last := s.lastTxStatus
	s.mu.Unlock()
	if last != "2" {
		t.Fatalf("lastTxStatus = %q, want %q — transitions cannot be detected", last, "2")
	}
}

// "2" (transmitting by OTHER means) must not satisfy the confirmation gate.
// The tune release only restores the operator's mode + power after positive RX
// confirmation, because that restore raises power from the clamped tune level:
// treating "2" as idle let it write full power into a rig whose PTT was being
// held down by a control line (2026-07-23 review P1). It must not alarm either
// — "2" is also the ordinary TX→RX tail — so the correct state is UNCHANGED.
func TestObserveTxStatus_TxByOtherMeansDoesNotConfirm(t *testing.T) {
	s, _ := newCommandTestService(t)
	def, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("rigdef missing")
	}
	s.beginTxConfirm(def, nil)

	s.observeTxStatus("2")

	if !s.TxUncertain() {
		t.Fatal("\"2\" confirmed the unkey — it reports the rig is TRANSMITTING, not idle")
	}
	if s.TxAlarmActive() {
		t.Fatal("\"2\" alarmed; it is also the normal TX→RX tail, so it must only stay inconclusive")
	}

	// The rig settling into RX is what resolves it.
	s.observeTxStatus("0")
	if s.TxUncertain() {
		t.Fatal("\"0\" after \"2\" must confirm — otherwise a clean tune never restores")
	}
}

// A skipped tune restore must not leave the mode/power snapshot describing a
// state the rig is no longer in. captureTuneSnapshot is frozen for the whole
// tune, so nothing else corrects it — and CurrentPowerW feeds TX_PWR on logged
// QSOs, which would then record the operator's normal power while the rig sat
// at clamped tune power (2026-07-23 review P2).
func TestReleaseTune_UnconfirmedUnkeyInvalidatesSnapshot(t *testing.T) {
	prev := txConfirmTimeout
	txConfirmTimeout = 30 * time.Millisecond // rig stays silent; skip path taken
	defer func() { txConfirmTimeout = prev }()

	s, _ := newCommandTestService(t)
	s.tuneRestoreSettle = 0

	s.mu.Lock()
	s.lastMode, s.lastPower = "USB", 100 // the pre-tune state StartTune restores to
	s.mu.Unlock()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	// No TXSTATUS answer, so waitTxConfirm fails and the restore is skipped —
	// leaving the rig at RTTY / clamped tune power.
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune: %v", err)
	}

	if got := s.CurrentPowerW(); got != 0 {
		t.Errorf("CurrentPowerW = %d after a skipped restore, want 0 (unknown) — "+
			"a stale value gets stamped on QSOs as TX_PWR", got)
	}
	s.mu.Lock()
	mode := s.lastMode
	s.mu.Unlock()
	if mode != "" {
		t.Errorf("lastMode = %q after a skipped restore, want empty — the rig is still in tune mode", mode)
	}
}

// An effective backoff initial must never exceed the effective maximum. Config
// validation only compares the two RAW settings, so a max set with the initial
// omitted left the built-in 1s default above a 50ms cap (2026-07-23 review P2).
func TestNewService_BackoffInitialClampedToEffectiveMax(t *testing.T) {
	cfg := types.BridgeConfig{Enabled: false}
	cfg.Timeouts.BackoffMaxMs = 50 // initial deliberately left unset

	s := New(cfg, &logging.Service{})

	if s.supervisorInitialBackoff > s.supervisorMaxBackoff {
		t.Errorf("initial backoff %v exceeds configured max %v — the configured ceiling is not honoured",
			s.supervisorInitialBackoff, s.supervisorMaxBackoff)
	}
}
