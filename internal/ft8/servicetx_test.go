package ft8

import (
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

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
	s.newPlayer = func(int) (txPlayer, error) {
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
