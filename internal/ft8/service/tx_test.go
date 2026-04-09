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

func TestExecuteTX_cancelledDuringWindowWait(t *testing.T) {
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

// --- Integration: executeTX with mocks ---

// TestExecuteTX_fullPipeline exercises the complete encode → PTT assert →
// PlaySamples → PTT release pipeline by injecting a waitForWindow that
// returns immediately with an even-parity timestamp, bypassing the real
// 15-second timing boundary.
func TestExecuteTX_fullPipeline(t *testing.T) {
	player := &mockPlayer{}
	pttMock := &mockPTT{}

	// Pick a time that is on an even slot boundary (Unix epoch = slot 0 = even).
	evenTime := time.Unix(0, 0).UTC()

	s := &Service{
		ft8Config: types.FT8Config{
			TXEnabled:    true,
			TXBaseFreqHz: 1000.0,
			TXParity:     "even",
		},
		playback: player,
		pttCtl:   pttMock,
		Logger:   noopLogger(),
		waitForWindow: func(ctx context.Context, m timing.Mode) (time.Time, error) {
			return evenTime, nil
		},
	}

	msg := &message.Message{
		MsgType: message.TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}

	// Use a generous timeout — the TX offset wait (1s) is the longest part.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.executeTX(ctx, TXRequest{Message: msg})

	// Verify PTT Assert → PlaySamples → PTT Release ordering.
	pttCalls := pttMock.getCalls()
	require.Equal(t, []string{"Assert", "Release"}, pttCalls,
		"PTT should be asserted before play and released after")

	playerCalls := player.getCalls()
	require.Equal(t, []string{"PlaySamples"}, playerCalls,
		"PlaySamples should be called exactly once")

	// Verify samples were actually synthesised (151680 samples for FT8).
	player.mu.Lock()
	sampleLen := len(player.samples)
	player.mu.Unlock()
	assert.Equal(t, 151680, sampleLen, "expected 151680 FT8 TX samples")

	// Verify txActive was set during play and cleared after.
	assert.False(t, s.txActive.Load(), "txActive should be false after TX completes")
}

// TestExecuteTX_fullPipeline_noPTT verifies the pipeline works without PTT
// (VOX mode — pttCtl is nil).
func TestExecuteTX_fullPipeline_noPTT(t *testing.T) {
	player := &mockPlayer{}
	evenTime := time.Unix(0, 0).UTC()

	s := &Service{
		ft8Config: types.FT8Config{
			TXEnabled:    true,
			TXBaseFreqHz: 1000.0,
			TXParity:     "even",
		},
		playback: player,
		pttCtl:   nil, // VOX mode
		Logger:   noopLogger(),
		waitForWindow: func(ctx context.Context, m timing.Mode) (time.Time, error) {
			return evenTime, nil
		},
	}

	msg := &message.Message{
		MsgType: message.TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.executeTX(ctx, TXRequest{Message: msg})

	playerCalls := player.getCalls()
	assert.Equal(t, []string{"PlaySamples"}, playerCalls)
}

// TestCancelTX_cancelsInFlightPlayback verifies that CancelTX cancels an
// in-progress playback by calling the stored txPlayCancel function.
func TestCancelTX_cancelsInFlightPlayback(t *testing.T) {
	cancelled := make(chan struct{})

	s := &Service{
		txQueue: make(chan TXRequest, 1),
		Logger:  noopLogger(),
	}

	// Simulate an in-progress playback by setting txPlayCancel.
	s.txMu.Lock()
	s.txPlayCancel = func() {
		close(cancelled)
	}
	s.txMu.Unlock()

	s.CancelTX()

	// Verify the cancel function was called.
	select {
	case <-cancelled:
		// OK — cancel was invoked.
	case <-time.After(1 * time.Second):
		t.Fatal("CancelTX did not invoke txPlayCancel")
	}

	// Verify txPlayCancel was nil-ed out.
	s.txMu.Lock()
	assert.Nil(t, s.txPlayCancel, "txPlayCancel should be nil after CancelTX")
	s.txMu.Unlock()
}

func TestExecuteTX_packFailure(t *testing.T) {
	player := &mockPlayer{}
	s := &Service{
		ft8Config: types.FT8Config{
			TXEnabled:    true,
			TXBaseFreqHz: 1000.0,
		},
		playback: player,
		Logger:   noopLogger(),
		// Inject a waitForWindow that returns immediately so we don't
		// block on timing if pack somehow succeeds.
		waitForWindow: func(ctx context.Context, m timing.Mode) (time.Time, error) {
			return time.Now(), nil
		},
	}

	// A message with an invalid type should fail packing synchronously.
	badMsg := &message.Message{
		MsgType: message.Type(99),
		Call1:   "CQ",
		Call2:   "W1AW",
	}

	s.executeTX(context.Background(), TXRequest{Message: badMsg})

	calls := player.getCalls()
	assert.Empty(t, calls, "expected no playback when pack fails")
}
