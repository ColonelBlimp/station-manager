package ft8

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

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
	err := s.startTransmission("CQ G0XYZ IO91", 1500, 0,
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
		s.StartQso("7Q5MLV", "IO91", "K1ABC", "FN42", "2026-06-10T14:30:00Z", 4000, 14.074),
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
	require.ErrorIs(t, s.StartQso("7Q5MLV", "KH78", "K1ABC", "FN42", now, 1500, 14.074), ErrTxNotReady)
	require.ErrorIs(t, s.StartCallCq("7Q5MLV", "KH78", 1500, 14.074), ErrTxNotReady)
	require.ErrorIs(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1500, 14.074), ErrTxNotReady)
}

// TestStartWorkCaller_Gating: the work-a-caller entry point shares the arm gate with
// StartQso/StartCallCq — refused when disarmed, committed (session active) when armed.
func TestStartWorkCaller_Gating(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	t.Run("refused when disarmed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.ErrorIs(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1500, 14.074), ErrTxNotArmed)
	})
	t.Run("commits when armed", func(t *testing.T) {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		require.NoError(t, s.ArmTx(true))
		require.NoError(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1500, 14.074))
		require.True(t, s.seq.Active(), "an armed work-a-caller start commits a session")
		s.AbandonQso()
	})
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
