// Package service provides the FT8 digital mode service with both RX and TX
// paths.
//
// # RX Pipeline
//
// The service wires the complete receive chain:
//
//	audio.Capture (48 kHz) → channel extraction → Decimate4 (12 kHz) →
//	sample accumulation → dsp.ProcessWindow → message.Unpack → output channel
//
// Audio is captured at 48 kHz (the native rate of USB audio codecs such as
// the TI PCM2903C used in Yaesu FTdx10 / FT-710) and decimated to the
// FT8-standard 12 kHz using a 49-tap FIR anti-aliasing lowpass filter
// (matching WSJT-X's lib/fil4.f90). The decimated samples are accumulated
// into window-sized buffers of [dsp.WindowSamples] samples (180 000 =
// 15 seconds). When a full window is collected, the buffer is handed to
// [dsp.ProcessWindow] which runs the spectrogram → candidate detection →
// soft demodulation → LDPC decode → CRC verify pipeline. Each successfully
// decoded 77-bit message is unpacked via [message.Unpack] and delivered to
// the [Messages] output channel as an [RXMessage].
//
// # TX Pipeline
//
// The TX path is activated when TXEnabled is true in the FT8 configuration.
// Callers submit messages via [Service.Transmit], which queues a [TXRequest].
// The txLoop goroutine waits for the correct window boundary (respecting
// even/odd parity), then executes:
//
//	message.Pack → codec.EncodeMessage → dsp.BitsToSymbols → dsp.InsertSync →
//	synth.Synthesize → PTT assert → audio.PlaySamples → PTT release
//
// PTT control is optional — when no serial port is configured, the
// assert/release steps are skipped (VOX mode).
//
// RX and TX use separate audio devices (capture vs. playback) and operate
// concurrently without interference.
//
// # Lifecycle
//
// The service follows the standard Initialize → Start → Stop lifecycle:
//
//   - [Service.Initialize] validates injected dependencies, loads the
//     [types.FT8Config], creates and initialises the [audio.Capture]
//     and (when TX is enabled) the [audio.Playback] and optional PTT.
//   - [Service.Start] starts audio capture and launches the RX accumulation
//     goroutine and (when TX is enabled) the TX goroutine. Returns immediately.
//   - [Service.Stop] signals both loops to exit, waits for completion, and
//     stops audio capture and playback.
//   - [Service.Close] releases all owned resources (capture device, playback
//     device, PTT port, output channel). The service cannot be reused after Close.
//
// QSO state machine and auto-parity selection are deferred to a subsequent
// milestone.
package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
	"github.com/ColonelBlimp/station-manager/internal/ft8/timing"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/ptt"
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

// Service is the FT8 digital mode service with RX and TX paths.
//
// Dependencies are injected via DI struct tags. Call [Initialize] before
// [Start], and [Stop] before [Close]. The service is safe for concurrent
// use by multiple goroutines.
type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	Logger        *logging.Service `di.inject:"loggingservice"`

	ft8Config  types.FT8Config
	capture    *audio.Capture
	decimator  *dsp.Decimator // 48 kHz → 12 kHz anti-aliasing FIR + decimation
	messages   chan RXMessage
	windowDone chan struct{} // signals after each decode window completes

	// capChannels is the number of channels the capture device delivers.
	// When > 1 the rxLoop extracts a single channel before accumulation.
	capChannels uint32
	// capChanIdx is the 0-based channel index to extract (0 = left, 1 = right).
	capChanIdx int

	// --- TX ---

	playback      txPlayer                                                    // audio playback device (nil when TX disabled)
	pttCtl        pttController                                               // PTT control (nil when no PTT port configured)
	txQueue       chan TXRequest                                              // buffered channel for pending TX requests (cap 1)
	txActive      atomic.Bool                                                 // true while audio is being played
	txMu          sync.Mutex                                                  // protects txPlayCancel
	txPlayCancel  context.CancelFunc                                          // cancels the current playback
	waitForWindow func(ctx context.Context, m timing.Mode) (time.Time, error) // injectable for tests

	// --- Lifecycle state ---

	isInitialized atomic.Bool
	running       atomic.Bool
	initOnce      sync.Once
	initErr       error
	closeOnce     sync.Once          // ensures messages channel is closed only once
	mu            sync.Mutex         // protects cancel, txCancel
	cancel        context.CancelFunc // cancels the RX loop context
	wg            sync.WaitGroup     // tracks the RX loop goroutine
	txCancel      context.CancelFunc // cancels the TX loop context
	txWg          sync.WaitGroup     // tracks the TX loop goroutine
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

		// Apply sensible defaults for zero-valued config fields.
		if s.ft8Config.MaxCandidates <= 0 {
			s.ft8Config.MaxCandidates = 50
		}
		if s.ft8Config.MaxIterations <= 0 {
			s.ft8Config.MaxIterations = 25
		}
		if s.ft8Config.BufferSize == 0 {
			s.ft8Config.BufferSize = 512
		}

		// Resolve capture channel count: default to 2 (stereo) so that USB
		// audio codecs that carry signal on a single channel (e.g., Yaesu
		// FTdx10 / FT-710) are not degraded by the automatic (L+R)/2 downmix.
		capChannels := cfg.CaptureChannels
		if capChannels == 0 {
			capChannels = 2
		}

		// Resolve which channel to extract when stereo.
		capChanIdx := 0 // left
		if capChannels >= 2 && cfg.CaptureChannel == "right" {
			capChanIdx = 1
		}
		s.capChannels = capChannels
		s.capChanIdx = capChanIdx

		// Create audio capture with FT8-specific settings.
		// Capture at 48 kHz (the native rate of USB audio codecs) and
		// decimate to 12 kHz with a proper anti-aliasing FIR filter.
		// This matches WSJT-X's approach (lib/fil4.f90) and avoids
		// relying on miniaudio's internal resampler.
		capCfg := audio.Config{
			DeviceIndex: s.ft8Config.DeviceIndex,
			SampleRate:  dsp.CaptureSampleRate, // 48000
			Channels:    capChannels,
			BufferSize:  s.ft8Config.BufferSize,
			Logger:      s.Logger,
		}
		s.capture = audio.New(capCfg)

		if err := s.capture.Init(); err != nil {
			s.initErr = errors.New(op).Err(err).Msg("failed to initialize audio capture")
			return
		}

		// Create the decimator for 48 kHz → 12 kHz conversion.
		s.decimator = dsp.NewDecimator()

		s.messages = make(chan RXMessage, messageChannelSize)
		s.windowDone = make(chan struct{}, 4)
		s.txQueue = make(chan TXRequest, 1)
		s.waitForWindow = timing.WaitForNext

		// --- TX setup (when enabled) ---
		if cfg.TXEnabled {
			pbCfg := audio.Config{
				DeviceIndex: cfg.TXDeviceIndex,
				SampleRate:  dsp.SampleRate,
				Channels:    1,
				BufferSize:  cfg.TXBufferSize,
				Logger:      s.Logger,
			}
			pb := audio.NewPlayback(pbCfg)
			if err := pb.Init(); err != nil {
				s.initErr = errors.New(op).Err(err).Msg("failed to initialize TX playback")
				return
			}
			s.playback = pb

			// PTT is optional — only opened when a port name is configured.
			if cfg.PTTPortName != "" {
				pttLine := ptt.LineRTS
				if cfg.PTTLine == "DTR" {
					pttLine = ptt.LineDTR
				}
				p, err := ptt.Open(ptt.Config{
					PortName: cfg.PTTPortName,
					Line:     pttLine,
					Logger:   s.Logger,
				})
				if err != nil {
					// PTT failure is not fatal — log a warning and continue
					// without PTT (VOX mode).
					s.Logger.WarnWith().Err(err).
						Str("port", cfg.PTTPortName).
						Msg("FT8 TX: PTT open failed; continuing without PTT (VOX mode)")
				} else {
					s.pttCtl = p
				}
			}
		}

		s.isInitialized.Store(true)

		s.Logger.InfoWith().
			Int("device_index", cfg.DeviceIndex).
			Uint32("buffer_size", cfg.BufferSize).
			Int("max_candidates", cfg.MaxCandidates).
			Int("max_iterations", cfg.MaxIterations).
			Bool("enabled", cfg.Enabled).
			Bool("tx_enabled", cfg.TXEnabled).
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
// is cancelled, the loop exits, but callers must still call [Stop] to reset
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

	// Prepare the TX loop context if TX is enabled. Both cancel funcs are
	// stored under a single mutex acquisition, so a concurrent Stop() sees
	// either both or neither.
	var txCtx context.Context
	var txCancel context.CancelFunc
	if s.ft8Config.TXEnabled && s.playback != nil {
		txCtx, txCancel = context.WithCancel(ctx)
	}

	s.mu.Lock()
	s.cancel = cancel
	s.txCancel = txCancel
	s.mu.Unlock()

	s.wg.Add(1)
	go s.rxLoop(rxCtx, s.capture.Samples())

	// Launch the TX loop if TX is enabled.
	if txCtx != nil {
		s.txWg.Add(1)
		go s.txLoop(txCtx)
	}

	s.Logger.InfoWith().
		Bool("tx_enabled", s.ft8Config.TXEnabled).
		Msg("FT8 service started")
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
	// Cancel the TX loop context.
	if s.txCancel != nil {
		s.txCancel()
		s.txCancel = nil
	}
	s.mu.Unlock()

	// Wait for both loop goroutines to exit.
	s.wg.Wait()
	s.txWg.Wait()

	// Stop audio capture.
	if s.capture != nil {
		if err := s.capture.Stop(); err != nil && s.Logger != nil {
			s.Logger.WarnWith().Err(err).Msg("error stopping audio capture")
		}
	}

	if s.Logger != nil {
		s.Logger.InfoWith().Msg("FT8 service stopped")
	}
	return nil
}

// Close releases all resources owned by the service. The service cannot be
// reused after Close is called — initOnce is permanently spent, so a
// subsequent Initialize call would be a no-op returning stale state.
//
// Close stops the service if it is running, closes the audio capture device,
// and closes the Messages output channel. Idempotent with respect to the
// messages channel (safe to call twice).
func (s *Service) Close() error {
	const op errors.Op = "ft8.Service.Close"

	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	// Stop unconditionally; ignore "not running" — avoids the TOCTOU
	// between running.Load() and Stop() that would log a spurious warning
	// if a concurrent goroutine calls Stop() in between.
	if err := s.Stop(); err != nil {
		// Only log genuine errors, not the expected "not running" case.
		if s.Logger != nil && err.Error() != errMsgNotRunning {
			s.Logger.WarnWith().Err(err).Msg("error during stop on close")
		}
	}

	// Close audio capture device.
	if s.capture != nil {
		if err := s.capture.Close(); err != nil {
			return errors.New(op).Err(err).Msg("failed to close audio capture")
		}
	}

	// Close TX playback device.
	if s.playback != nil {
		if err := s.playback.Close(); err != nil && s.Logger != nil {
			s.Logger.WarnWith().Err(err).Msg("error closing TX playback")
		}
	}

	// Close PTT.
	if s.pttCtl != nil {
		if err := s.pttCtl.Close(); err != nil && s.Logger != nil {
			s.Logger.WarnWith().Err(err).Msg("error closing PTT")
		}
	}

	// Close messages output channel — once only to prevent double-close panic.
	s.closeOnce.Do(func() {
		if s.messages != nil {
			close(s.messages)
		}
		if s.windowDone != nil {
			close(s.windowDone)
		}
	})

	s.isInitialized.Store(false)
	if s.Logger != nil {
		s.Logger.InfoWith().Msg("FT8 service closed")
	}
	return nil
}

// Messages returns a receive-only channel that delivers decoded FT8
// messages. The channel is closed when [Close] is called.
func (s *Service) Messages() <-chan RXMessage {
	return s.messages
}

// WindowDone returns a receive-only channel that fires each time a
// decode window completes, regardless of whether any messages were decoded.
// Consumers can use this to count actual decode cycles instead of relying
// on a wall-clock timer.
func (s *Service) WindowDone() <-chan struct{} {
	return s.windowDone
}

// rxLoop is the main RX processing goroutine. It reads audio sample chunks
// from the sample's channel, accumulates them into a window-sized buffer,
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

	channels := int(s.capChannels)
	chanIdx := s.capChanIdx

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-samples:
			if !ok {
				return // capture channel closed
			}

			// When capturing in stereo (channels > 1), extract the
			// selected channel from the interleaved sample buffer.
			if channels > 1 {
				mono := make([]float32, 0, len(chunk)/channels)
				for i := chanIdx; i < len(chunk); i += channels {
					mono = append(mono, chunk[i])
				}
				chunk = mono
			}

			// Decimate from 48 kHz to 12 kHz using the WSJT-X
			// anti-aliasing FIR filter (fil4). This is the critical
			// step that ensures USB audio codec signals are properly
			// filtered before FT8 DSP processing.
			if s.decimator != nil {
				chunk = s.decimator.Decimate(chunk)
				if len(chunk) == 0 {
					continue
				}
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

	// Notify window-done observers after decoding completes (or finds nothing).
	defer func() {
		select {
		case s.windowDone <- struct{}{}:
		default: // non-blocking; drop if nobody is listening
		}
	}()

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
