package ft8

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// TestStartTransmission_PanicClearsInFlight guards review 2026-06-19 H1: a panic
// in the transmit fn (recovered by safego) must still run the cleanup defer —
// clearing txInFlight/txCancel, publishing a final state, and calling
// onDone(false) — so the TX path isn't wedged in-flight until a daemon restart.
func TestStartTransmission_PanicClearsInFlight(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	done := make(chan bool, 1)
	err := s.startTransmission("CQ G0XYZ IO91", 1500, 0, nil,
		func(context.Context, *TxController) error { panic("boom in fn") },
		func(ok bool) { done <- ok })
	require.NoError(t, err, "launch succeeds; the panic happens in the tracked goroutine")

	select {
	case ok := <-done:
		require.False(t, ok, "onDone(false) after a panicked transmission")
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup defer / onDone did not run after a recovered panic")
	}

	s.txMu.Lock()
	inFlight, cancelSet := s.txInFlight, s.txCancel != nil
	s.txMu.Unlock()
	require.False(t, inFlight, "txInFlight cleared after a recovered panic")
	require.False(t, cancelSet, "txCancel cleared after a recovered panic")

	// The TX path is not wedged: a later transmit is accepted, not ErrTxInFlight.
	require.NotErrorIs(t, s.TransmitNext("CQ G0XYZ IO91", 1600), ErrTxInFlight)
}

// TestStartTransmission_DisarmRaceClearsState guards review 2026-06-19 M1:
// GoTracked's wg.Add runs under txMu, so a disarm racing a transmit launch
// cannot pass txWg.Wait before the goroutine is counted — by the time disarm
// returns, any launched TX has cleared its in-flight state. Run under -race a
// regression also surfaces as a data race on the txCancel/txInFlight fields.
func TestStartTransmission_DisarmRaceClearsState(t *testing.T) {
	for i := 0; i < 50; i++ {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		go func() { _ = s.TransmitNext("CQ G0XYZ IO91", 1500) }()
		_ = s.ArmTx(false) // disarm racing the launch

		s.txMu.Lock()
		armed, inFlight := s.txArmed, s.txInFlight
		s.txMu.Unlock()
		require.False(t, armed)
		require.False(t, inFlight, "disarm must drain any launched TX goroutine before returning")
	}
}

// TestValidateTxOffset_RejectsOutOfPassband guards review 2026-06-19 M1: the
// daemon refuses a TX audio offset outside the usable passband on every keying
// path, regardless of the SPA. The gate runs before the arm check, so a disarmed
// service exercises it; offset 0 stays ErrNoOffset ("pick one"), a finite
// out-of-range or non-finite value is ErrTxBadOffset, and a valid offset passes
// through to the arm gate.
func TestValidateTxOffset_RejectsOutOfPassband(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	const msg = "CQ G0XYZ IO91"

	require.ErrorIs(t, s.TransmitNext(msg, 0), ErrNoOffset)
	require.ErrorIs(t, s.TransmitNext(msg, 50), ErrTxBadOffset)          // below the 200 Hz low edge
	require.ErrorIs(t, s.TransmitNext(msg, 4000), ErrTxBadOffset)        // above the 3000 Hz high edge
	require.ErrorIs(t, s.TransmitNext(msg, 2990), ErrTxBadOffset)        // 2990+50 spills past the high edge
	require.ErrorIs(t, s.TransmitNext(msg, math.Inf(1)), ErrTxBadOffset) // non-finite
	require.ErrorIs(t, s.TransmitNext(msg, math.NaN()), ErrTxBadOffset)  // non-finite
	require.ErrorIs(t, s.TransmitNext(msg, 1500), ErrTxNotArmed)         // valid → reaches the arm gate

	// The sequenced start paths share the gate.
	require.ErrorIs(t,
		s.StartQso("7Q5MLV", "IO91", "K1ABC", "FN42", "2026-06-10T14:30:00Z", 4000, 14.074, 1),
		ErrTxBadOffset)
}

// fakeTxPlayer is a txPlayer (slotPlayer + Init/Close) recording its lifecycle.
type fakeTxPlayer struct {
	mu      sync.Mutex
	initN   int
	closeN  int
	playN   int
	stopN   int
	initErr error
	done    chan struct{}
}

func newFakeTxPlayer() *fakeTxPlayer { return &fakeTxPlayer{done: make(chan struct{})} }

func (p *fakeTxPlayer) Init() error  { p.mu.Lock(); defer p.mu.Unlock(); p.initN++; return p.initErr }
func (p *fakeTxPlayer) Close() error { p.mu.Lock(); defer p.mu.Unlock(); p.closeN++; return nil }
func (p *fakeTxPlayer) Play(s []int16) (<-chan struct{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.playN++
	return p.done, nil
}
func (p *fakeTxPlayer) Stop() error { p.mu.Lock(); defer p.mu.Unlock(); p.stopN++; return nil }
func (p *fakeTxPlayer) inits() int  { p.mu.Lock(); defer p.mu.Unlock(); return p.initN }
func (p *fakeTxPlayer) closes() int { p.mu.Lock(); defer p.mu.Unlock(); return p.closeN }

// newTxTestService builds an enabled Service with an injected keyer and a player
// factory returning the given player (or playerErr). Not Started — these tests
// exercise the TX gate + lifecycle, not capture, so base() falls back to
// context.Background() and the transmit goroutine simply waits for the slot
// boundary until disarm cancels it (no 15 s wait is ever awaited).
func newTxTestService(keyer TxKeyer, player txPlayer, playerErr error) *Service {
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}}, logging.Noop(), nil)
	s.newPlayer = func(string, int) (txPlayer, error) {
		if playerErr != nil {
			return nil, playerErr
		}
		return player, nil
	}
	s.SetTxKeyer(keyer)
	return s
}

// txInFlightNow reads the single-flight flag under txMu — race-safe for assertions
// (the transmit goroutine mutates it under the same lock).
func (s *Service) txInFlightNow() bool {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	return s.txInFlight
}

// drainTxState reads ft8-tx events from a subscriber until one matches want or
// the deadline passes, returning the last TxState seen.
func drainTxState(t *testing.T, ch <-chan hubEvent, want func(TxState) bool) TxState {
	t.Helper()
	deadline := time.After(2 * time.Second)
	var last TxState
	for {
		select {
		case evt := <-ch:
			if evt.name != EventTx {
				continue
			}
			st, ok := evt.payload.(TxState)
			if !ok {
				continue
			}
			last = st
			if want == nil || want(st) {
				return last
			}
		case <-deadline:
			t.Fatalf("timed out waiting for ft8-tx state; last=%+v", last)
			return last
		}
	}
}

func TestArmTx_RequiresReadyKeyer(t *testing.T) {
	t.Run("no keyer wired", func(t *testing.T) {
		s := newTxTestService(nil, newFakeTxPlayer(), nil)
		err := s.ArmTx(true)
		require.ErrorIs(t, err, ErrTxUnavailable)
		require.False(t, s.txArmed)
	})

	t.Run("rig not ready", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{notReady: true}, newFakeTxPlayer(), nil)
		err := s.ArmTx(true)
		require.ErrorIs(t, err, ErrTxNotReady)
		require.False(t, s.txArmed)
	})

	t.Run("player unavailable (CGO-free build shape)", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, nil, ErrTxUnavailable)
		err := s.ArmTx(true)
		require.ErrorIs(t, err, ErrTxUnavailable)
		require.False(t, s.txArmed)
	})
}

// review M1: once the rig becomes unready after arming, a sequenced session start
// must refuse with ErrTxNotReady — the live keyer readiness is re-checked, not just
// the sticky armed flag, so the API can't return 202 and spin against an unready rig.
func TestStartSession_RefusesWhenRigBecomesUnready(t *testing.T) {
	k := &fakeKeyer{}
	s := newTxTestService(k, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	k.setNotReady(true) // rig disconnects / loses identity after arming
	now := time.Now().UTC().Format(time.RFC3339)
	require.ErrorIs(t, s.StartQso("7Q5MLV", "KH78", "K1ABC", "FN42", now, 1500, 14.074, 1), ErrTxNotReady)
	require.ErrorIs(t, s.StartCallCq("7Q5MLV", "KH78", 1500, 14.074, "", 1), ErrTxNotReady)
	require.ErrorIs(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1500, 14.074, 1), ErrTxNotReady)
}

// TestStartWorkCaller_Gating: the work-a-caller entry point shares the arm gate with
// StartQso/StartCallCq — refused when disarmed, committed (session active) when armed.
func TestStartWorkCaller_Gating(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	t.Run("refused when disarmed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.ErrorIs(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1500, 14.074, 1), ErrTxNotArmed)
	})
	t.Run("commits when armed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		require.NoError(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1500, 14.074, 1))
		require.True(t, s.seq.Active(), "an armed work-a-caller start commits a session")
		s.AbandonQso()
	})
}

// TestTransmitNext_RefusedWhileSessionActive: a manual send and a sequenced
// session are mutually exclusive. StartCallCq makes the sequencer active WITHOUT
// keying immediately (the caller's CQ goes out on the next slot), so txInFlight is
// false — the manual send must still be refused (ErrQsoInProgress) by the Active()
// gate, not admitted to key mid-session on the strength of an idle single-flight.
func TestTransmitNext_RefusedWhileSessionActive(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	require.NoError(t, s.StartCallCq("7Q5MLV", "IO91", 1500, 14.074, "", 1))
	require.True(t, s.seq.Active(), "the Call-CQ session is active")
	require.False(t, s.txInFlightNow(), "the caller's CQ has not keyed yet (next slot)")

	require.ErrorIs(t, s.TransmitNext("CQ G0XYZ IO91", 1600), ErrQsoInProgress)
	s.AbandonQso()
}

// TestStartSession_RefusedWhileManualSendInFlight: the reverse exclusion. A manual
// send sets txInFlight synchronously (the fake player's done never closes, so it
// stays in flight), and every session start must then be refused (ErrTxInFlight)
// rather than committing a session whose opening rung would collide and drop while
// the manual message still keys.
func TestStartSession_RefusedWhileManualSendInFlight(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }() // disarm cancels the in-flight manual send

	require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500))
	require.True(t, s.txInFlightNow(), "the manual send is in flight")

	now := time.Now().UTC().Format(time.RFC3339)
	require.ErrorIs(t, s.StartQso("7Q5MLV", "IO91", "K1ABC", "FN42", now, 1600, 14.074, 1), ErrTxInFlight)
	require.ErrorIs(t, s.StartCallCq("7Q5MLV", "IO91", 1600, 14.074, "", 1), ErrTxInFlight)
	require.ErrorIs(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1600, 14.074, 1), ErrTxInFlight)
	require.False(t, s.seq.Active(), "no session may commit while a manual send is in flight")
}

// TestStartSession_DuplicateDuringRung_ReportsQsoInProgress: txInFlight is shared
// by manual sends AND sequencer rungs. While a session is active its rung keys for
// most of each slot (txInFlight true), so a duplicate start must still classify as
// ErrQsoInProgress ("a QSO is in progress") — NOT ErrTxInFlight ("a transmission is
// in flight"), which would misreport the session's own rung as a foreign send. Same
// code the between-rungs path returns. (Regression guard for the sessionTxGate
// classification introduced with mutual exclusion.)
func TestStartSession_DuplicateDuringRung_ReportsQsoInProgress(t *testing.T) {
	k := &fakeKeyer{}
	s := newTxTestService(k, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	// An active session (its CQ has not keyed yet — the caller CQ goes out next slot).
	require.NoError(t, s.StartCallCq("7Q5MLV", "IO91", 1500, 14.074, "", 1))
	require.True(t, s.seq.Active())

	// Model the session's rung KEYING: txInFlight true AND — crucially, like the real
	// bridge (TxReady() folds in ft8TxActive) — the keyer reports NOT ready during the
	// keyed portion. A ready-first gate would mask this as ErrTxNotReady; the classify-
	// before-ready order must still return ErrQsoInProgress.
	s.txMu.Lock()
	s.txInFlight = true
	s.txMu.Unlock()
	k.setNotReady(true)

	err := s.StartQso("7Q5MLV", "IO91", "K1ABC", "FN42",
		time.Now().UTC().Format(time.RFC3339), 1600, 14.074, 1)
	require.ErrorIs(t, err, ErrQsoInProgress, "a duplicate start atop an active session is a QSO conflict")
	require.NotErrorIs(t, err, ErrTxInFlight, "the session's own rung must not read as a manual transmission")
	require.NotErrorIs(t, err, ErrTxNotReady, "a keyed rung reports not-ready; must not leak as rig-not-ready")

	// Restore so the deferred disarm sees consistent state (no real goroutine here).
	k.setNotReady(false)
	s.txMu.Lock()
	s.txInFlight = false
	s.txMu.Unlock()
}

func TestArmTx_AcquiresAndReleasesDevice(t *testing.T) {
	p := newFakeTxPlayer()
	s := newTxTestService(&fakeKeyer{}, p, nil)
	ch, unsub := s.hub.subscribe()
	defer unsub()

	require.NoError(t, s.ArmTx(true))
	require.True(t, s.txArmed)
	require.Equal(t, 1, p.inits(), "arming Init's the output device")
	st := drainTxState(t, ch, func(st TxState) bool { return st.Armed })
	require.True(t, st.Armed)

	// Idempotent: arming again is a no-op, not a second device.
	require.NoError(t, s.ArmTx(true))
	require.Equal(t, 1, p.inits())

	require.NoError(t, s.ArmTx(false))
	require.False(t, s.txArmed)
	require.Equal(t, 1, p.closes(), "disarming Close's the output device")
	st = drainTxState(t, ch, func(st TxState) bool { return !st.Armed })
	require.False(t, st.Armed)
}

func TestTransmitNext_Gating(t *testing.T) {
	t.Run("refused when disarmed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		err := s.TransmitNext("CQ G0XYZ IO91", 1500)
		require.ErrorIs(t, err, ErrTxNotArmed)
	})

	t.Run("bad message rejected before arming check", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		defer func() { _ = s.ArmTx(false) }()
		err := s.TransmitNext("this is not a standard ft8 message", 1500)
		require.ErrorIs(t, err, ErrTxBadMessage)
	})

	t.Run("refused while a transmission is in flight", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		defer func() { _ = s.ArmTx(false) }() // cancels the in-flight wait

		require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500))
		require.True(t, s.txInFlight)

		err := s.TransmitNext("CQ G0XYZ IO91", 1600)
		require.ErrorIs(t, err, ErrTxInFlight)
	})
}

// A queued transmission marks the subsystem transmitting and publishes the
// state; disarm then cancels the pending slot wait and the in-flight flag
// clears (no RF — the controller never reaches the slot boundary).
func TestTransmitNext_PublishesAndCancelsOnDisarm(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	ch, unsub := s.hub.subscribe()
	defer unsub()

	require.NoError(t, s.ArmTx(true))
	require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500))

	st := drainTxState(t, ch, func(st TxState) bool { return st.Transmitting })
	require.True(t, st.Transmitting)
	require.Equal(t, "CQ G0XYZ IO91", st.Message)

	// Disarm aborts the pending transmission and drains its goroutine.
	require.NoError(t, s.ArmTx(false))
	require.False(t, s.txInFlight)
	require.False(t, s.txArmed)
}

// TestAbandonQso_CancelsInFlightTransmission covers the operator off-ramp: hitting
// Abandon during a transmission (e.g. a Call-CQ that's mid-cycle) must cut TX
// immediately — drop PTT, stop audio — not let the ~13 s slot finish. Unlike
// disarm, Abandon leaves TX ARMED so the operator can call CQ or answer again.
func TestAbandonQso_CancelsInFlightTransmission(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	ch, unsub := s.hub.subscribe()
	defer unsub()

	require.NoError(t, s.ArmTx(true))
	require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500))

	st := drainTxState(t, ch, func(st TxState) bool { return st.Transmitting })
	require.True(t, st.Transmitting)

	s.AbandonQso() // operator hits Abandon mid-transmission

	inFlight := func() bool { s.txMu.Lock(); defer s.txMu.Unlock(); return s.txInFlight }
	require.Eventually(t, func() bool { return !inFlight() }, time.Second, 5*time.Millisecond,
		"Abandon must cancel the in-flight transmission immediately, not wait for the slot")

	armed := func() bool { s.txMu.Lock(); defer s.txMu.Unlock(); return s.txArmed }
	require.True(t, armed(), "Abandon must leave TX armed (only the contact ends, not the TX path)")
}

// Stop disarms TX (drops PTT / closes the device) and latches the subsystem so
// it can't be re-armed afterwards.
func TestStop_DisarmsAndLatchesTx(t *testing.T) {
	p := newFakeTxPlayer()
	s := newTxTestService(&fakeKeyer{}, p, nil)
	require.NoError(t, s.ArmTx(true))

	require.NoError(t, s.Stop())
	require.False(t, s.txArmed)
	require.GreaterOrEqual(t, p.closes(), 1, "Stop closes the output device")

	err := s.ArmTx(true)
	require.ErrorIs(t, err, ErrTxUnavailable, "no arming after Stop")
}

// A reconnecting subscriber gets the current arm state replayed immediately
// from the hub cache (the ADR 0009 late-subscriber pattern).
func TestTxState_ReplayedToLateSubscriber(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	ch, unsub := s.hub.subscribe()
	defer unsub()
	st := drainTxState(t, ch, nil) // first event is the replayed cache
	require.True(t, st.Armed)
}

// TestTxSlotTracking covers the occupancy self-TX fix: markTxSlot (wired to the TX
// controller's onTransmit) records the keyed slot, and wasTxSlot matches the SAME
// slot's capture ref — so decodeLoop skips occupancy for a slot we transmitted in.
// The boundary (TX) and the capture StartUTC must reduce to the same RFC3339 string.
func TestTxSlotTracking(t *testing.T) {
	s := &Service{}
	b1 := time.Date(2026, 7, 4, 15, 5, 30, 0, time.UTC)
	s.markTxSlot(b1)

	// The captured slot's ref for the same boundary must match.
	if ref := SlotRefFromTime(b1); !s.wasTxSlot(ref.StartUTC) {
		t.Fatalf("wasTxSlot(%q) = false, want true (the slot we keyed)", ref.StartUTC)
	}

	// A second TX slot keyed close behind is ALSO remembered — the ring means marking
	// b2 doesn't evict b1 before decodeLoop reads it (the overwrite race the fix closes).
	b2 := b1.Add(SlotDuration)
	s.markTxSlot(b2)
	r1, r2 := SlotRefFromTime(b1), SlotRefFromTime(b2)
	if !s.wasTxSlot(r1.StartUTC) || !s.wasTxSlot(r2.StartUTC) {
		t.Fatal("both recent TX slots must match after two marks (ring, not a single slot)")
	}

	// A slot we never transmitted in must NOT match — its occupancy is real and published.
	if other := SlotRefFromTime(b2.Add(SlotDuration)); s.wasTxSlot(other.StartUTC) {
		t.Fatalf("wasTxSlot(%q) = true, want false (a slot we never keyed)", other.StartUTC)
	}
	// Empty never matches (defensive; before any TX).
	if s.wasTxSlot("") {
		t.Fatal(`wasTxSlot("") should be false`)
	}
}

// TestStartTransmission_SupersededCommitRefused pins the review 2026-07-20 #1
// commit gate: a rung whose session generation went stale between the sequencer
// dropping its lock and the commit taking txMu (an Abandon in that gap finds no
// txCancel to cancel) must be REFUSED at commit — no goroutine, no in-flight
// state, no RF — rather than keying a transmission for a dead session.
func TestStartTransmission_SupersededCommitRefused(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	ran := false
	err := s.startTransmission("CQ G0XYZ IO91", 1500, 0,
		func() bool { return false }, // stale generation — Abandon won the race
		func(context.Context, *TxController) error { ran = true; return nil },
		nil)
	require.ErrorIs(t, err, ErrTxSuperseded)
	require.False(t, ran, "fn must never run for a refused commit")

	s.txMu.Lock()
	inFlight, cancelSet := s.txInFlight, s.txCancel != nil
	s.txMu.Unlock()
	require.False(t, inFlight, "refused commit leaves nothing in flight")
	require.False(t, cancelSet, "refused commit registers no txCancel")
}

// TestStartQso_RejectedStartKeepsExchangePath pins review 2026-07-20 #5: the
// antenna-path reset happens only on an ACCEPTED start — a rejected duplicate
// StartQso must not flip the active exchange's long-path choice back to short.
func TestStartQso_RejectedStartKeepsExchangePath(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	// theirSlot = the CURRENT wall-clock slot, so its parity matches "now" and
	// fireOpening declines to fire (no TX goroutine — deterministic test).
	theirSlot := slotStart(time.Now().UTC()).Format(time.RFC3339)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", theirSlot, 1500, 14.074, 1))
	s.SetExchangePath("L") // operator picks long path for the ACTIVE exchange

	err := s.StartQso("G0XYZ", "IO91", "W1AW", "FN31", theirSlot, 1500, 14.074, 1)
	require.Error(t, err, "second start while a QSO is active is rejected")
	require.Equal(t, antPathLong, s.exchangePath(),
		"a rejected start must not reset the active exchange's path")

	s.AbandonQso()
}

// TestOnComplete_UsesStampThenResetsPath pins review 2026-07-20 #5 (caller-mode
// half): the completed contact carries the path stamped for its final-rung
// attempt, and onComplete resets the live selection so a Call-CQ run's NEXT
// answerer does not inherit the previous contact's choice.
func TestOnComplete_UsesStampThenResetsPath(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	logged := make(chan CompletedQso, 1)
	s.SetQsoLogger(func(_ context.Context, c CompletedQso) { logged <- c })

	s.SetExchangePath("L")
	c := CompletedQso{TheirCall: "K1ABC"}
	s.stampCompletionPath(&c) // taken at the successful final-rung boundary
	s.seq.onComplete(c)

	select {
	case c := <-logged:
		require.Equal(t, antPathLong, c.AntPath, "the completed contact carries the snapshotted choice")
	case <-time.After(time.Second):
		t.Fatal("qsoLogger not invoked")
	}
	require.Equal(t, antPathShort, s.exchangePath(),
		"the live selection is reset — the next contact starts from the short-path default")
}

// TestOnComplete_StampSurvivesLivePathReset is the race guard: a concurrent new
// session start (or an OnSlot idle-out) can reset the LIVE exchPath between the final
// rung's success and onComplete. The completed QSO must still log the path captured at
// success, not the reset live value. Reading exchPath in onComplete logged the default
// and stole the operator's choice.
func TestOnComplete_StampSurvivesLivePathReset(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	logged := make(chan CompletedQso, 1)
	s.SetQsoLogger(func(_ context.Context, c CompletedQso) { logged <- c })

	s.SetExchangePath("L")
	c := CompletedQso{TheirCall: "K1ABC"}
	s.stampCompletionPath(&c) // final rung succeeds with the operator's long-path choice

	// A concurrent start / OnSlot idle-out resets the live selection BEFORE onComplete.
	s.consumeExchangePath()
	require.Equal(t, antPathShort, s.exchangePath(), "live selection was reset")

	s.seq.onComplete(c)
	select {
	case c := <-logged:
		require.Equal(t, antPathLong, c.AntPath,
			"the completed QSO logs its stamp, not the reset live value")
	case <-time.After(time.Second):
		t.Fatal("qsoLogger not invoked")
	}
}

// TestOnComplete_DoesNotClearNewerPathSelection guards the inverse completion
// race. Once a new session has started, its explicit path selection must survive
// the previous QSO's delayed onComplete reset. The generation check makes the
// newer selection win while the old QSO still logs its own stamped value.
func TestOnComplete_DoesNotClearNewerPathSelection(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	logged := make(chan CompletedQso, 1)
	s.SetQsoLogger(func(_ context.Context, c CompletedQso) { logged <- c })

	old := CompletedQso{TheirCall: "K1ABC"}
	s.stampCompletionPath(&old) // old contact used the default short path

	s.consumeExchangePath() // accepted new-session start resets the live value
	s.SetExchangePath("L")  // operator selects long path for the new contact

	s.seq.onComplete(old)
	select {
	case c := <-logged:
		require.Equal(t, antPathShort, c.AntPath, "old QSO keeps its own path")
	case <-time.After(time.Second):
		t.Fatal("qsoLogger not invoked")
	}
	require.Equal(t, antPathLong, s.exchangePath(),
		"delayed old completion must not clear the new session's selection")
}

// TestOnComplete_PerQsoStampsDoNotOverwrite proves completion metadata is not a
// Service singleton: two prepared completions retain their own path even when
// their callbacks are delivered in the same window.
func TestOnComplete_PerQsoStampsDoNotOverwrite(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	logged := make(chan CompletedQso, 2)
	s.SetQsoLogger(func(_ context.Context, c CompletedQso) { logged <- c })

	s.SetExchangePath("L")
	first := CompletedQso{TheirCall: "K1ABC"}
	s.stampCompletionPath(&first)

	s.SetExchangePath("S")
	second := CompletedQso{TheirCall: "W1AW"}
	s.stampCompletionPath(&second)

	s.seq.onComplete(first)
	s.seq.onComplete(second)

	require.Equal(t, antPathLong, (<-logged).AntPath)
	require.Equal(t, antPathShort, (<-logged).AntPath)
	require.Equal(t, antPathShort, s.exchangePath(), "latest completed selection is reset")
}

// TestCompletionRace_PreservesNewPathAndStatus drives the production ordering:
// the old final-rung callback has committed idle but is paused before onComplete,
// a replacement session starts and selects long path, then the old callback
// resumes. The old completion must neither clear the new choice nor publish stale
// idle after the replacement's active status.
func TestCompletionRace_PreservesNewPathAndStatus(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	var (
		statusMu sync.Mutex
		statuses []QsoStatus
		pending  func(bool)
	)
	s.seq.publish = func(st QsoStatus) {
		statusMu.Lock()
		statuses = append(statuses, st)
		statusMu.Unlock()
	}
	// Match the real asynchronous contract: the terminal onDone is delivered
	// after the rung-driving call has returned.
	s.seq.transmit = func(_ string, _, _ float64, _ uint64, onDone func(bool)) error {
		if onDone != nil {
			pending = onDone
		}
		return nil
	}

	logged := make(chan CompletedQso, 1)
	s.SetQsoLogger(func(_ context.Context, c CompletedQso) { logged <- c })
	entered := make(chan struct{})
	release := make(chan struct{})
	originalOnComplete := s.seq.onComplete
	s.seq.onComplete = func(c CompletedQso) {
		close(entered)
		<-release
		originalOnComplete(c)
	}

	epoch := time.Unix(0, 0).UTC()
	require.NoError(t, s.seq.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		epoch.Format(time.RFC3339), 1500, 14.074, epoch))
	s.SetExchangePath("L")
	driveTheir(s.seq, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s.seq, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s.seq, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
	require.NotNil(t, pending, "final rung must register an asynchronous completion")

	callbackDone := make(chan struct{})
	go func() {
		pending(true)
		close(callbackDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("completion callback did not reach the race window")
	}

	// Match the current wall-clock parity so the replacement does not immediately
	// fire an opening rung; only its state/path commit matters to this test.
	newSlot := slotStart(time.Now().UTC()).Format(time.RFC3339)
	startErr := s.StartQso("G0XYZ", "IO91", "W1AW", "FN31", newSlot, 1600, 14.074, 2)
	if startErr == nil {
		s.SetExchangePath("L")
	}
	close(release)
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("completion callback did not finish")
	}

	require.NoError(t, startErr, "replacement session should start after idle commit")
	select {
	case c := <-logged:
		require.Equal(t, "K1ABC", c.TheirCall)
		require.Equal(t, antPathLong, c.AntPath, "old QSO logs its own stamped path")
	case <-time.After(time.Second):
		t.Fatal("qsoLogger not invoked")
	}
	require.Equal(t, antPathLong, s.exchangePath(),
		"old completion must not clear the replacement session's selection")

	statusMu.Lock()
	gotStatuses := append([]QsoStatus(nil), statuses...)
	statusMu.Unlock()
	require.NotEmpty(t, gotStatuses)
	last := gotStatuses[len(gotStatuses)-1]
	require.True(t, last.Active, "old completion must not overwrite the replacement with idle")
	require.Equal(t, "W1AW", last.TheirCall)
}

// TestExchangePath_ConsumeAndRestore pins the round-12 #3 helpers: consume is
// an atomic read+clear (a second consume yields the default), and restore puts
// back only the non-default — so a selection that landed after a rejected
// start's consume beats a short-path restore.
func TestExchangePath_ConsumeAndRestore(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)

	s.SetExchangePath("L")
	p, gen := s.consumeExchangePath()
	require.Equal(t, antPathLong, p, "consume returns the choice")
	require.Equal(t, antPathShort, s.exchangePath(), "consume clears back to the default")
	p2, _ := s.consumeExchangePath()
	require.Equal(t, antPathShort, p2, "second consume yields the default")

	s.restoreExchangePath(antPathLong, gen)
	require.Equal(t, antPathLong, s.exchangePath(), "quiet-window long-path restore reinstates the choice")

	// The lost-update case the codex review caught in the first shape: a
	// selection landing between consume and restore must WIN over the restore.
	p3, gen3 := s.consumeExchangePath()
	require.Equal(t, antPathLong, p3)
	s.SetExchangePath("S") // operator's newer selection, mid-window
	s.restoreExchangePath(antPathLong, gen3)
	require.Equal(t, antPathShort, s.exchangePath(),
		"a stale long-path restore must not overwrite a newer selection")

	s.SetExchangePath("L") // and a short-path restore is always a no-op
	_, gen4 := s.consumeExchangePath()
	s.SetExchangePath("L")
	s.restoreExchangePath(antPathShort, gen4)
	require.Equal(t, antPathLong, s.exchangePath(),
		"a short-path restore never clobbers a selection")
}
