package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Initialize ---

func TestInitialize_nilReceiver(t *testing.T) {
	var s *Service
	err := s.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNilService)
}

func TestInitialize_nilConfigService(t *testing.T) {
	s := &Service{Logger: noopLogger()}
	err := s.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNilConfigService)
}

func TestInitialize_nilLogger(t *testing.T) {
	s := &Service{ConfigService: &config.Service{}}
	err := s.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNilLogger)
}

// --- Start ---

func TestStart_nilReceiver(t *testing.T) {
	var s *Service
	err := s.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNilService)
}

func TestStart_notInitialized(t *testing.T) {
	s := &Service{}
	err := s.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNotInitialized)
}

func TestStart_disabledConfig(t *testing.T) {
	s := &Service{
		ft8Config: types.FT8Config{Enabled: false},
		Logger:    noopLogger(),
	}
	s.isInitialized.Store(true)

	err := s.Start(context.Background())
	assert.NoError(t, err)
	assert.False(t, s.running.Load(), "should not be running when disabled")
}

func TestStart_alreadyRunning(t *testing.T) {
	s := &Service{
		ft8Config: types.FT8Config{Enabled: true},
	}
	s.isInitialized.Store(true)
	s.running.Store(true)

	err := s.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgAlreadyRunning)
}

// --- Stop ---

func TestStop_nilReceiver(t *testing.T) {
	var s *Service
	err := s.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNilService)
}

func TestStop_notRunning(t *testing.T) {
	s := &Service{}
	err := s.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNotRunning)
}

// --- Close ---

func TestClose_nilReceiver(t *testing.T) {
	var s *Service
	err := s.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errMsgNilService)
}

func TestClose_notRunning_noCapture(t *testing.T) {
	s := &Service{Logger: noopLogger()}
	err := s.Close()
	assert.NoError(t, err)
}

// --- Messages ---

func TestMessages_returnsChannel(t *testing.T) {
	ch := make(chan RXMessage, 1)
	s := &Service{messages: ch}
	assert.Equal(t, (<-chan RXMessage)(ch), s.Messages())
}

func TestMessages_nil(t *testing.T) {
	s := &Service{}
	assert.Nil(t, s.Messages())
}

// --- RXMessage ---

func TestRXMessage_fields(t *testing.T) {
	msg := &message.Message{
		MsgType: message.TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}

	rx := RXMessage{
		Msg77:   [10]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0},
		Message: msg,
		Freq:    1500.0,
		TimeOff: 0.5,
		SNR:     -10.0,
	}

	assert.Equal(t, "CQ W1AW FN31", rx.Message.String())
	assert.InDelta(t, 1500.0, float64(rx.Freq), 0.01)
	assert.InDelta(t, 0.5, float64(rx.TimeOff), 0.01)
	assert.InDelta(t, -10.0, float64(rx.SNR), 0.01)
}

// --- processWindow ---

func TestProcessWindow_silence(t *testing.T) {
	ch := make(chan RXMessage, messageChannelSize)
	s := &Service{
		ft8Config: types.FT8Config{MaxCandidates: 50, MaxIterations: 25},
		messages:  ch,
		Logger:    noopLogger(),
	}

	silence := make([]float32, dsp.WindowSamples)
	s.processWindow(context.Background(), silence)

	select {
	case <-ch:
		t.Fatal("expected no messages from silence")
	default:
	}
}

func TestProcessWindow_roundTrip(t *testing.T) {
	samples, orig := synthFT8Signal(t, 1000.0)

	ch := make(chan RXMessage, messageChannelSize)
	s := &Service{
		ft8Config: types.FT8Config{MaxCandidates: 50, MaxIterations: 50},
		messages:  ch,
		Logger:    noopLogger(),
	}

	s.processWindow(context.Background(), samples)

	select {
	case rxMsg := <-ch:
		require.NotNil(t, rxMsg.Message)
		assert.Equal(t, orig.Call1, rxMsg.Message.Call1)
		assert.Equal(t, orig.Call2, rxMsg.Message.Call2)
		assert.Equal(t, orig.Grid, rxMsg.Message.Grid)
		assert.InDelta(t, 1000.0, float64(rxMsg.Freq), 100.0)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for decoded message")
	}
}

func TestProcessWindow_contextCancelled(t *testing.T) {
	samples, _ := synthFT8Signal(t, 1000.0)

	// Unbuffered channel — send will block unless context is checked.
	ch := make(chan RXMessage)
	s := &Service{
		ft8Config: types.FT8Config{MaxCandidates: 50, MaxIterations: 50},
		messages:  ch,
		Logger:    noopLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		s.processWindow(ctx, samples)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("processWindow did not exit on cancelled context")
	}
}

// --- rxLoop ---

func TestRxLoop_accumulates(t *testing.T) {
	samples, orig := synthFT8Signal(t, 1000.0)

	// Feed the samples in small chunks via a channel.
	feedCh := make(chan []float32, 512)
	const chunkSize = 512
	for i := 0; i < len(samples); i += chunkSize {
		end := i + chunkSize
		if end > len(samples) {
			end = len(samples)
		}
		chunk := make([]float32, end-i)
		copy(chunk, samples[i:end])
		feedCh <- chunk
	}
	close(feedCh)

	outCh := make(chan RXMessage, messageChannelSize)
	s := &Service{
		ft8Config: types.FT8Config{MaxCandidates: 50, MaxIterations: 50},
		messages:  outCh,
		Logger:    noopLogger(),
	}

	s.wg.Add(1)
	go s.rxLoop(context.Background(), feedCh)
	s.wg.Wait()

	select {
	case rxMsg := <-outCh:
		require.NotNil(t, rxMsg.Message)
		assert.Equal(t, orig.Call1, rxMsg.Message.Call1)
		assert.Equal(t, orig.Call2, rxMsg.Message.Call2)
		assert.Equal(t, orig.Grid, rxMsg.Message.Grid)
	default:
		t.Fatal("expected a decoded message from rxLoop")
	}
}

func TestRxLoop_contextCancel(t *testing.T) {
	// Use an open (not closed) channel so rxLoop blocks on receive.
	feedCh := make(chan []float32)

	s := &Service{
		ft8Config: types.FT8Config{MaxCandidates: 50, MaxIterations: 50},
		messages:  make(chan RXMessage, messageChannelSize),
		Logger:    noopLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.wg.Add(1)
	go s.rxLoop(ctx, feedCh)

	cancel()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("rxLoop did not exit on context cancel")
	}
}

func TestRxLoop_channelClosed(t *testing.T) {
	feedCh := make(chan []float32)
	close(feedCh)

	s := &Service{
		ft8Config: types.FT8Config{MaxCandidates: 50, MaxIterations: 50},
		messages:  make(chan RXMessage, messageChannelSize),
		Logger:    noopLogger(),
	}

	s.wg.Add(1)
	go s.rxLoop(context.Background(), feedCh)

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("rxLoop did not exit when channel was closed")
	}
}

func TestRxLoop_overflowCarried(t *testing.T) {
	// Verify that samples exceeding one window are carried to the next.
	// Feed WindowSamples + extra silence. rxLoop should process one window
	// and carry the extra into the next buffer (no panic, no lost data).
	feedCh := make(chan []float32, 512)

	// Feed all silence plus a small overflow.
	total := dsp.WindowSamples + 1000
	const chunkSize = 512
	for i := 0; i < total; i += chunkSize {
		end := i + chunkSize
		if end > total {
			end = total
		}
		feedCh <- make([]float32, end-i)
	}
	close(feedCh)

	s := &Service{
		ft8Config: types.FT8Config{MaxCandidates: 50, MaxIterations: 25},
		messages:  make(chan RXMessage, messageChannelSize),
		Logger:    noopLogger(),
	}

	s.wg.Add(1)
	go s.rxLoop(context.Background(), feedCh)
	s.wg.Wait()
	// No panic or hang = success.
}

// --- ServiceName ---

func TestServiceName(t *testing.T) {
	assert.Equal(t, types.FT8ServiceName, ServiceName)
}

// --- helpers ---

// noopLogger returns a *logging.Service that discards all output.
// The zero-value Service has no initialised logger, so all event builders
// are no-ops — safe for unit tests.
func noopLogger() *logging.Service {
	return &logging.Service{}
}

// synthFT8Signal encodes a known FT8 message and synthesises its audio
// waveform in a full WindowSamples-length buffer. Returns the buffer and
// the original message for verification.
func synthFT8Signal(t *testing.T, baseFreqHz float64) ([]float32, *message.Message) {
	t.Helper()

	orig := &message.Message{
		MsgType: message.TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	packed, err := message.Pack(orig)
	require.NoError(t, err)

	// TX chain: message → LDPC encode → symbols → channel sequence.
	cw := codec.EncodeMessage(packed)
	var cwDSP [dsp.CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := dsp.BitsToSymbols(cwDSP)
	chanSyms := dsp.InsertSync(dataSyms)

	samples := make([]float32, dsp.WindowSamples)
	for sym := range dsp.NumSymbols {
		toneIdx := chanSyms[sym]
		toneFreq := baseFreqHz + float64(toneIdx)*dsp.ToneSpacing
		sampleStart := sym * dsp.SamplesPerSymbol
		for n := range dsp.SamplesPerSymbol {
			globalN := sampleStart + n
			samples[globalN] = float32(
				math.Cos(2 * math.Pi * toneFreq * float64(globalN) / float64(dsp.SampleRate)))
		}
	}

	return samples, orig
}
