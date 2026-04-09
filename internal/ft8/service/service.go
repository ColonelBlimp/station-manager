// Package service provides the RX-only FT8 digital mode service.
//
// The service wires the complete receive chain:
//
//	audio.Capture → sample accumulation → dsp.ProcessWindow →
//	message.Unpack → output channel
//
// Audio samples are captured at 12 kHz mono (the standard WSJT sample rate)
// and accumulated into window-sized buffers of [dsp.WindowSamples] samples
// (180 000 = 15 seconds). When a full window is collected, the buffer is
// handed to [dsp.ProcessWindow] which runs the spectrogram → candidate
// detection → soft demodulation → LDPC decode → CRC verify pipeline. Each
// successfully decoded 77-bit message is unpacked via [message.Unpack] and
// delivered to the [Messages] output channel as an [RXMessage].
//
// The service follows the standard Initialize → Start → Stop lifecycle:
//
//   - [Service.Initialize] validates injected dependencies, loads the
//     [types.FT8Config], creates and initialises the [audio.Capture].
//   - [Service.Start] starts audio capture and launches the RX accumulation
//     goroutine. Returns immediately.
//   - [Service.Stop] signals the RX loop to exit, waits for completion, and
//     stops audio capture.
//   - [Service.Close] releases all owned resources (capture device, output
//     channel). The service cannot be reused after Close.
//
// TX synthesis, PTT control, QSO state machine, and timing alignment are
// deferred to a subsequent milestone.
package service

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const (
	// ServiceName is the DI bean ID for this service.
	ServiceName = types.FT8ServiceName

	// messageChannelSize is the capacity of the Messages output channel.
	// FT8 windows are 15 s long; even at a high decode rate this provides
	// ample buffering for bursty consumers.
	messageChannelSize = 64
)

// Error message constants.
const (
	errMsgNilService       = "nil service receiver"
	errMsgNotInitialized   = "service not initialized"
	errMsgAlreadyRunning   = "service already running"
	errMsgNotRunning       = "service not running"
	errMsgNilConfigService = "config service is nil"
	errMsgNilLogger        = "logger service is nil"
)

// RXMessage represents a decoded and unpacked FT8 message from the RX
// pipeline. Only messages that pass both LDPC CRC-14 verification and
// [message.Unpack] are emitted.
type RXMessage struct {
	// Msg77 is the raw 77-bit packed message (10 bytes, trailing 3 bits
	// masked to zero). Useful for forwarding, deduplication, or re-packing.
	Msg77 [10]byte

	// Message is the unpacked human-readable FT8 message.
	Message *message.Message

	// Freq is the estimated audio frequency of the signal (Hz).
	Freq float32

	// TimeOff is the estimated time offset within the capture window (s).
	TimeOff float32

	// SNR is a rough signal-to-noise ratio estimate (dB).
	SNR float32
}

// Service is the RX-only FT8 digital mode service.
//
// Dependencies are injected via DI struct tags. Call [Initialize] before
// [Start], and [Stop] before [Close]. The service is safe for concurrent
// use by multiple goroutines.
type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	Logger        *logging.Service `di.inject:"loggingservice"`

	ft8Config types.FT8Config
	capture   *audio.Capture
	messages  chan RXMessage

	// Lifecycle state.
	isInitialized atomic.Bool
	running       atomic.Bool
	initOnce      sync.Once
	initErr       error
	mu            sync.Mutex         // protects cancel
	cancel        context.CancelFunc // cancels the RX loop context
	wg            sync.WaitGroup     // tracks the RX loop goroutine
}

// Initialize validates injected dependencies, loads the FT8 configuration,
// and creates the audio capture device.
//
// Idempotent: subsequent calls after successful initialisation are no-ops.
func (s *Service) Initialize() error {
	const op errors.Op = "ft8.Service.Initialize"

	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	s.initOnce.Do(func() {
		if s.ConfigService == nil {
			s.initErr = errors.New(op).Msg(errMsgNilConfigService)
			return
		}
		if s.Logger == nil {
			s.initErr = errors.New(op).Msg(errMsgNilLogger)
			return
		}

		cfg, err := s.ConfigService.FT8Config()
		if err != nil {
			s.initErr = errors.New(op).Err(err).Msg("failed to load FT8 configuration")
			return
		}
		s.ft8Config = cfg

		// Create audio capture with FT8-specific settings: 12 kHz mono.
		capCfg := audio.Config{
			DeviceIndex: cfg.DeviceIndex,
			SampleRate:  dsp.SampleRate,
			Channels:    1,
			BufferSize:  cfg.BufferSize,
			Logger:      s.Logger,
		}
		s.capture = audio.New(capCfg)

		if err := s.capture.Init(); err != nil {
			s.initErr = errors.New(op).Err(err).Msg("failed to initialize audio capture")
			return
		}

		s.messages = make(chan RXMessage, messageChannelSize)
		s.isInitialized.Store(true)

		s.Logger.InfoWith().
			Int("device_index", cfg.DeviceIndex).
			Uint32("buffer_size", cfg.BufferSize).
			Int("max_candidates", cfg.MaxCandidates).
			Int("max_iterations", cfg.MaxIterations).
			Bool("enabled", cfg.Enabled).
			Msg("FT8 service initialized")
	})

	return s.initErr
}

// Start begins audio capture and launches the RX accumulation/decode loop.
//
// If the FT8 service is disabled in configuration, Start logs a message and
// returns nil without starting anything. This allows callers to unconditionally
// call Start without checking Enabled first.
//
// The provided context controls the lifetime of the RX loop: when the context
// is cancelled the loop exits, but callers must still call [Stop] to reset
// the service state.
func (s *Service) Start(ctx context.Context) error {
	const op errors.Op = "ft8.Service.Start"

	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	if !s.isInitialized.Load() {
		return errors.New(op).Msg(errMsgNotInitialized)
	}

	if !s.ft8Config.Enabled {
		s.Logger.InfoWith().Msg("FT8 service disabled in configuration; skipping start")
		return nil
	}

	if !s.running.CompareAndSwap(false, true) {
		return errors.New(op).Msg(errMsgAlreadyRunning)
	}

	// Start audio capture.
	if err := s.capture.Start(ctx); err != nil {
		s.running.Store(false)
		return errors.New(op).Err(err).Msg("failed to start audio capture")
	}

	// Internal cancellation context for the RX loop. Derived from the
	// caller's context, so either external cancellation or Stop() will
	// terminate the loop.
	rxCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go s.rxLoop(rxCtx, s.capture.Samples())

	s.Logger.InfoWith().Msg("FT8 RX service started")
	return nil
}

// Stop signals the RX loop to exit, waits for it to finish, and stops
// audio capture. Idempotent with respect to the running state.
func (s *Service) Stop() error {
	const op errors.Op = "ft8.Service.Stop"

	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	if !s.running.CompareAndSwap(true, false) {
		return errors.New(op).Msg(errMsgNotRunning)
	}

	// Cancel the RX loop context.
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()

	// Wait for the RX loop goroutine to exit.
	s.wg.Wait()

	// Stop audio capture.
	if err := s.capture.Stop(); err != nil {
		s.Logger.WarnWith().Err(err).Msg("error stopping audio capture")
	}

	s.Logger.InfoWith().Msg("FT8 RX service stopped")
	return nil
}

// Close releases all resources owned by the service. The service cannot be
// reused after Close is called.
//
// Close stops the service if it is running, closes the audio capture device,
// and closes the Messages output channel.
func (s *Service) Close() error {
	const op errors.Op = "ft8.Service.Close"

	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	// Stop if still running (ignore "not running" errors).
	if s.running.Load() {
		if err := s.Stop(); err != nil {
			s.Logger.WarnWith().Err(err).Msg("error during stop on close")
		}
	}

	// Close audio capture device.
	if s.capture != nil {
		if err := s.capture.Close(); err != nil {
			return errors.New(op).Err(err).Msg("failed to close audio capture")
		}
	}

	// Close messages output channel.
	if s.messages != nil {
		close(s.messages)
	}

	s.isInitialized.Store(false)
	s.Logger.InfoWith().Msg("FT8 service closed")
	return nil
}

// Messages returns a receive-only channel that delivers decoded FT8
// messages. The channel is closed when [Close] is called.
func (s *Service) Messages() <-chan RXMessage {
	return s.messages
}

// rxLoop is the main RX processing goroutine. It reads audio sample chunks
// from the samples channel, accumulates them into a window-sized buffer,
// and runs the DSP decode pipeline each time a full window is collected.
//
// Sample overflow from one window is carried into the next, ensuring no
// audio data is lost at window boundaries.
//
// The samples channel is passed explicitly (rather than read from
// s.capture internally) to enable testing without audio hardware.
func (s *Service) rxLoop(ctx context.Context, samples <-chan []float32) {
	defer s.wg.Done()

	buf := make([]float32, 0, dsp.WindowSamples+dsp.SamplesPerSymbol)

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-samples:
			if !ok {
				return // capture channel closed
			}

			buf = append(buf, chunk...)

			if len(buf) >= dsp.WindowSamples {
				// Snapshot the window buffer for processing.
				window := make([]float32, dsp.WindowSamples)
				copy(window, buf[:dsp.WindowSamples])

				// Carry overflow samples into the next window.
				overflow := len(buf) - dsp.WindowSamples
				if overflow > 0 {
					copy(buf[:overflow], buf[dsp.WindowSamples:])
				}
				buf = buf[:overflow]

				s.processWindow(ctx, window)
			}
		}
	}
}

// processWindow runs the DSP decode pipeline on a complete capture window
// and delivers successfully decoded messages to the output channel.
func (s *Service) processWindow(ctx context.Context, samples []float32) {
	// Early exit if the context was cancelled during accumulation.
	select {
	case <-ctx.Done():
		return
	default:
	}

	decoded := dsp.ProcessWindow(samples, s.ft8Config.MaxCandidates, s.ft8Config.MaxIterations)
	if len(decoded) == 0 {
		s.Logger.DebugWith().Msg("FT8 window: no messages decoded")
		return
	}

	s.Logger.InfoWith().Int("count", len(decoded)).Msg("FT8 window: messages decoded")

	for i := range decoded {
		dm := &decoded[i]

		msg, err := message.Unpack(dm.Msg77)
		if err != nil {
			s.Logger.DebugWith().
				Err(err).
				Float32("freq", dm.Freq).
				Float32("snr", dm.SNR).
				Msg("failed to unpack decoded FT8 message")
			continue
		}

		rxMsg := RXMessage{
			Msg77:   dm.Msg77,
			Message: msg,
			Freq:    dm.Freq,
			TimeOff: dm.TimeOff,
			SNR:     dm.SNR,
		}

		s.Logger.InfoWith().
			Str("message", msg.String()).
			Float32("freq", dm.Freq).
			Float32("snr", dm.SNR).
			Msg("FT8 RX")

		// Non-blocking send: drop if the consumer is too slow. The channel
		// buffer (64 slots) provides ample room for normal operation.
		select {
		case <-ctx.Done():
			return
		case s.messages <- rxMsg:
		default:
			s.Logger.WarnWith().
				Str("message", msg.String()).
				Msg("FT8 message channel full; dropping message")
		}
	}
}
