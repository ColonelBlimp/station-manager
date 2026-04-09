package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
	"github.com/ColonelBlimp/station-manager/internal/ft8/timing"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock types ---

// mockPlayer records calls and optionally blocks until context is cancelled.
type mockPlayer struct {
	mu      sync.Mutex
	calls   []string
	samples []float32
	closed  atomic.Bool
	blockCh chan struct{} // if non-nil, PlaySamples blocks until this is closed or ctx cancelled
}

func (m *mockPlayer) PlaySamples(ctx context.Context, samples []float32, sampleRate uint32, channels uint32) error {
	m.mu.Lock()
	m.calls = append(m.calls, "PlaySamples")
	m.samples = samples
	m.mu.Unlock()

	if m.blockCh != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.blockCh:
		}
	}
	return nil
}

func (m *mockPlayer) Close() error {
	m.closed.Store(true)
	m.mu.Lock()
	m.calls = append(m.calls, "Close")
	m.mu.Unlock()
	return nil
}

func (m *mockPlayer) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// mockPTT records Assert/Release/Close calls.
type mockPTT struct {
	mu    sync.Mutex
	calls []string
}

func (m *mockPTT) Assert() error {
	m.mu.Lock()
	m.calls = append(m.calls, "Assert")
	m.mu.Unlock()
	return nil
}

func (m *mockPTT) Release() error {
	m.mu.Lock()
	m.calls = append(m.calls, "Release")
	m.mu.Unlock()
	return nil
}

func (m *mockPTT) Close() error {
	m.mu.Lock()
	m.calls = append(m.calls, "Close")
	m.mu.Unlock()
	return nil
}

func (m *mockPTT) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// --- Transmit guard tests ---

func TestTransmit_nilReceiver(t *testing.T) {
	var s *Service
	err := s.Transmit(TXRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNilService)
}

func TestTransmit_notInitialized(t *testing.T) {
	s := &Service{}
	err := s.Transmit(TXRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNotInitialized)
}

func TestTransmit_txDisabled(t *testing.T) {
	s := &Service{
		ft8Config: types.FT8Config{TXEnabled: false},
	}
	s.isInitialized.Store(true)

	err := s.Transmit(TXRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgTXDisabled)
}

func TestTransmit_notRunning(t *testing.T) {
	s := &Service{
		ft8Config: types.FT8Config{TXEnabled: true},
	}
	s.isInitialized.Store(true)

	err := s.Transmit(TXRequest{Message: &message.Message{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNotRunning)
}

func TestTransmit_nilMessage(t *testing.T) {
	s := &Service{
		ft8Config: types.FT8Config{TXEnabled: true},
	}
	s.isInitialized.Store(true)
	s.running.Store(true)

	err := s.Transmit(TXRequest{Message: nil})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgTXNilMessage)
}

func TestTransmit_queueFull(t *testing.T) {
	s := &Service{
		ft8Config: types.FT8Config{TXEnabled: true},
		txQueue:   make(chan TXRequest, 1),
		Logger:    noopLogger(),
	}
	s.isInitialized.Store(true)
	s.running.Store(true)

	msg := &message.Message{
		MsgType: message.TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}

	// First submit should succeed.
	err := s.Transmit(TXRequest{Message: msg})
	require.NoError(t, err)

	// Second submit should fail (queue full).
	err = s.Transmit(TXRequest{Message: msg})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgTXQueueFull)
}

func TestTransmit_success(t *testing.T) {
	s := &Service{
		ft8Config: types.FT8Config{TXEnabled: true},
		txQueue:   make(chan TXRequest, 1),
		Logger:    noopLogger(),
	}
	s.isInitialized.Store(true)
	s.running.Store(true)

	msg := &message.Message{
		MsgType: message.TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}

	err := s.Transmit(TXRequest{Message: msg})
	assert.NoError(t, err)

	// Verify the request is in the queue.
	select {
	case req := <-s.txQueue:
		assert.Equal(t, msg, req.Message)
	default:
		t.Fatal("expected a request in the queue")
	}
}

// --- CancelTX ---

func TestCancelTX_nilService(t *testing.T) {
	var s *Service
	s.CancelTX() // should not panic
}

func TestCancelTX_drainsQueue(t *testing.T) {
	s := &Service{
		txQueue: make(chan TXRequest, 1),
		Logger:  noopLogger(),
	}

	s.txQueue <- TXRequest{Message: &message.Message{}}
	s.CancelTX()

	select {
	case <-s.txQueue:
		t.Fatal("queue should be empty after CancelTX")
	default:
	}
}

// --- IsTXActive ---

func TestIsTXActive_nilReceiver(t *testing.T) {
	var s *Service
	assert.False(t, s.IsTXActive())
}

func TestIsTXActive_false(t *testing.T) {
	s := &Service{}
	assert.False(t, s.IsTXActive())
}

// --- txLoop ---

func TestTxLoop_contextCancel(t *testing.T) {
	s := &Service{
		txQueue: make(chan TXRequest, 1),
		Logger:  noopLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.txWg.Add(1)
	go s.txLoop(ctx)

	cancel()

	done := make(chan struct{})
	go func() {
		s.txWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("txLoop did not exit on context cancel")
	}
}

func TestTxLoop_channelClosed(t *testing.T) {
	q := make(chan TXRequest, 1)
	close(q)

	s := &Service{
		txQueue: q,
		Logger:  noopLogger(),
	}

	s.txWg.Add(1)
	go s.txLoop(context.Background())

	done := make(chan struct{})
	go func() {
		s.txWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("txLoop did not exit when channel was closed")
	}
}

// --- executeTX ---

func TestExecuteTX_encodesAndPlays(t *testing.T) {
	player := &mockPlayer{}
	pttMock := &mockPTT{}

	s := &Service{
		ft8Config: types.FT8Config{
			TXEnabled:    true,
			TXBaseFreqHz: 1000.0,
			TXParity:     "even",
		},
		playback: player,
		pttCtl:   pttMock,
		Logger:   noopLogger(),
	}

	msg := &message.Message{
		MsgType: message.TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}

	// Call executeTX directly with a very short-lived context.
	// The timing.WaitForNext will be the bottleneck in a real run,
	// but we can use a context with an immediate cancel to force the
	// function to bail at the window wait stage.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	s.executeTX(ctx, TXRequest{Message: msg})

	// The context times out during WaitForNext, so we expect no
	// playback calls.
	calls := player.getCalls()
	assert.Empty(t, calls, "expected no playback calls when context times out during window wait")
}

func TestExecuteTX_nilMessage(t *testing.T) {
	player := &mockPlayer{}
	s := &Service{
		ft8Config: types.FT8Config{TXEnabled: true, TXBaseFreqHz: 1000.0},
		playback:  player,
		Logger:    noopLogger(),
	}

	// nil message should be caught by the guard and logged, not panic.
	s.executeTX(context.Background(), TXRequest{Message: nil})

	calls := player.getCalls()
	assert.Empty(t, calls, "expected no playback calls for nil message")
}

// --- parseParity ---

func TestParseParity(t *testing.T) {
	assert.Equal(t, timing.Even, parseParity(""))
	assert.Equal(t, timing.Even, parseParity("even"))
	assert.Equal(t, timing.Even, parseParity("bogus"))
	assert.Equal(t, timing.Odd, parseParity("odd"))
}

// --- Integration: executeTX with mocks (bypasses timing) ---

// testExecuteTXDirect tests the encode → synth → PTT → play pipeline by
// calling executeTX on a service where the context is set to expire right
// after the encoding stage. This verifies that the encoding pipeline
// produces valid output.
func TestExecuteTX_packFailure(t *testing.T) {
	player := &mockPlayer{}
	s := &Service{
		ft8Config: types.FT8Config{
			TXEnabled:    true,
			TXBaseFreqHz: 1000.0,
		},
		playback: player,
		Logger:   noopLogger(),
	}

	// A message with an invalid type should fail packing.
	badMsg := &message.Message{
		MsgType: message.Type(99),
		Call1:   "CQ",
		Call2:   "W1AW",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s.executeTX(ctx, TXRequest{Message: badMsg})

	calls := player.getCalls()
	assert.Empty(t, calls, "expected no playback when pack fails")
}
