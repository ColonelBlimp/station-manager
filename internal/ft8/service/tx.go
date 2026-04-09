// tx.go — FT8 TX orchestration.
//
// This file adds transmit capabilities to the FT8 service. The TX path:
//
//	TXRequest → timing.WaitForNext → PTT assert → synth.Synthesize →
//	audio.PlaySamples → PTT release
//
// PTT control is optional — when no serial port is configured (VOX mode)
// the assert/release steps are skipped.
//
// The txLoop goroutine runs alongside the existing rxLoop. RX and TX use
// separate audio devices (capture vs. playback), so both operate concurrently
// without interference.

package service

import (
	"context"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
	"github.com/ColonelBlimp/station-manager/internal/ft8/synth"
	"github.com/ColonelBlimp/station-manager/internal/ft8/timing"
)

// TX error message constants.
const (
	errMsgTXDisabled   = "TX is not enabled in configuration"
	errMsgTXQueueFull  = "TX already queued; wait for current transmission to complete"
	errMsgTXNilMessage = "TX request message is nil"
	errMsgTXPackFailed = "failed to pack TX message"
)

// txPlayer is the subset of audio.Playback used by the TX loop.
// Defined as an interface so tests can inject a mock without real hardware.
type txPlayer interface {
	PlaySamples(ctx context.Context, samples []float32, sampleRate uint32, channels uint32) error
	Close() error
}

// pttController is the subset of ptt.PTT used by the TX loop.
// Nil-safe: when no PTT device is configured, the service stores nil.
type pttController interface {
	Assert() error
	Release() error
	Close() error
}

// TXRequest describes a message to be transmitted in the next available TX
// window.
type TXRequest struct {
	// Message is the FT8 message to encode and transmit.
	// Must be non-nil and packable via message.Pack.
	Message *message.Message

	// BaseFreqHz overrides the configured TX base frequency for this
	// transmission. If ≤ 0, the service's ft8Config.TXBaseFreqHz is used.
	BaseFreqHz float64

	// Parity overrides the configured TX parity for this transmission.
	// Empty string uses the configured default.
	Parity string
}

// Transmit submits a message for transmission in the next available TX
// window.
//
// Only one TX request may be queued at a time. A second call while a
// request is pending returns an error. Use [CancelTX] to clear the queue.
//
// Returns an error if the service is not running, TX is disabled, or the
// request is invalid (nil message).
func (s *Service) Transmit(req TXRequest) error {
	const op errors.Op = "ft8.Service.Transmit"

	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}
	if !s.isInitialized.Load() {
		return errors.New(op).Msg(errMsgNotInitialized)
	}
	if !s.ft8Config.TXEnabled {
		return errors.New(op).Msg(errMsgTXDisabled)
	}
	if !s.running.Load() {
		return errors.New(op).Msg(errMsgNotRunning)
	}
	if req.Message == nil {
		return errors.New(op).Msg(errMsgTXNilMessage)
	}

	select {
	case s.txQueue <- req:
		s.Logger.InfoWith().
			Str("message", req.Message.String()).
			Msg("FT8 TX request queued")
		return nil
	default:
		return errors.New(op).Msg(errMsgTXQueueFull)
	}
}

// CancelTX discards any pending TX request. If a transmission is currently
// in progress (audio playing), it is interrupted.
func (s *Service) CancelTX() {
	if s == nil || s.txQueue == nil {
		return
	}

	// Drain the queue.
	select {
	case <-s.txQueue:
		s.Logger.InfoWith().Msg("FT8 TX request cancelled (drained from queue)")
	default:
	}

	// Cancel in-progress playback.
	s.txMu.Lock()
	if s.txPlayCancel != nil {
		s.txPlayCancel()
		s.txPlayCancel = nil
		s.Logger.InfoWith().Msg("FT8 TX cancelled (in-progress playback stopped)")
	}
	s.txMu.Unlock()
}

// IsTXActive returns true when the service is actively transmitting audio.
func (s *Service) IsTXActive() bool {
	if s == nil {
		return false
	}
	return s.txActive.Load()
}

// txLoop is the TX processing goroutine. It reads TXRequests from the queue,
// waits for the correct window boundary, and executes the encode → synth →
// PTT → play pipeline.
func (s *Service) txLoop(ctx context.Context) {
	defer s.txWg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-s.txQueue:
			if !ok {
				return // channel closed
			}
			s.executeTX(ctx, req)
		}
	}
}

// executeTX performs a single TX cycle: wait for window → assert PTT →
// play synthesised audio → release PTT.
func (s *Service) executeTX(ctx context.Context, req TXRequest) {
	// Guard against nil message (shouldn't happen if Transmit validates,
	// but be defensive).
	if req.Message == nil {
		s.Logger.WarnWith().Msg("FT8 TX: nil message in request; skipping")
		return
	}

	// Resolve the base frequency.
	baseFreq := req.BaseFreqHz
	if baseFreq <= 0 {
		baseFreq = s.ft8Config.TXBaseFreqHz
	}

	// Resolve the slot parity.
	parityStr := req.Parity
	if parityStr == "" {
		parityStr = s.ft8Config.TXParity
	}
	parity := parseParity(parityStr)

	// --- 1. Encode the message ---
	packed, err := message.Pack(req.Message)
	if err != nil {
		s.Logger.WarnWith().Err(err).
			Str("message", req.Message.String()).
			Msg(errMsgTXPackFailed)
		return
	}

	cw := codec.EncodeMessage(packed)
	var cwDSP [dsp.CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := dsp.BitsToSymbols(cwDSP)
	chanSyms := dsp.InsertSync(dataSyms)

	s.Logger.InfoWith().
		Str("message", req.Message.String()).
		Float64("base_freq_hz", baseFreq).
		Str("parity", parity.String()).
		Msg("FT8 TX: message encoded, waiting for window")

	// --- 2. Wait for the correct window boundary ---
	waitFn := s.waitForWindow
	if waitFn == nil {
		waitFn = timing.WaitForNext
	}
	for {
		windowStart, err := waitFn(ctx, timing.FT8)
		if err != nil {
			s.Logger.DebugWith().Err(err).Msg("FT8 TX: window wait cancelled")
			return
		}

		slotParity := timing.SlotParity(timing.FT8, windowStart)
		if slotParity == parity {
			break // correct parity window
		}

		s.Logger.DebugWith().
			Str("got", slotParity.String()).
			Str("want", parity.String()).
			Msg("FT8 TX: skipping wrong-parity window")

		// Check context between iterations so cancellation arriving after
		// WaitForNext returns doesn't block for another full window period.
		select {
		case <-ctx.Done():
			return
		default:
		}
	}

	// --- 3. Wait for the TX offset (1 s into the window) ---
	txOffsetTimer := time.NewTimer(timing.FT8.TXOffset())
	defer txOffsetTimer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-txOffsetTimer.C:
	}

	// --- 4. Synthesise audio ---
	samples := synth.Synthesize(chanSyms, baseFreq)
	if samples == nil {
		s.Logger.WarnWith().Msg("FT8 TX: synthesis returned nil")
		return
	}

	// --- 5. Assert PTT (if configured) ---
	if s.pttCtl != nil {
		if err := s.pttCtl.Assert(); err != nil {
			s.Logger.WarnWith().Err(err).Msg("FT8 TX: PTT assert failed")
			return
		}
		defer func() {
			if err := s.pttCtl.Release(); err != nil {
				s.Logger.WarnWith().Err(err).Msg("FT8 TX: PTT release failed")
			}
		}()
		s.Logger.DebugWith().Msg("FT8 TX: PTT asserted")
	}

	// --- 6. Play audio ---
	s.txActive.Store(true)
	defer s.txActive.Store(false)

	// Create a cancellable context for this playback so CancelTX can
	// interrupt it.
	playCtx, playCancel := context.WithCancel(ctx)
	s.txMu.Lock()
	s.txPlayCancel = playCancel
	s.txMu.Unlock()

	defer func() {
		playCancel()
		s.txMu.Lock()
		s.txPlayCancel = nil
		s.txMu.Unlock()
	}()

	s.Logger.InfoWith().
		Str("message", req.Message.String()).
		Float64("base_freq_hz", baseFreq).
		Msg("FT8 TX: playing audio")

	if err := s.playback.PlaySamples(playCtx, samples, dsp.SampleRate, 1); err != nil {
		s.Logger.WarnWith().Err(err).Msg("FT8 TX: playback error")
		return
	}

	s.Logger.InfoWith().
		Str("message", req.Message.String()).
		Msg("FT8 TX: transmission complete")
}

// parseParity converts a config string ("even"/"odd") to a timing.Parity.
// Defaults to Even for empty or unrecognised values.
func parseParity(s string) timing.Parity {
	switch s {
	case "odd":
		return timing.Odd
	case "even":
		return timing.Even
	default:
		return timing.Even
	}
}
