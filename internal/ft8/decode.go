package ft8

import (
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// DecodedMessage is one successfully-decoded FT8 transmission from
// a 15-second audio slot — the per-message output of Decode.
type DecodedMessage struct {
	// Freq is the candidate centre frequency in Hz, from the sync
	// detector. Quantised to the spectrogram bin spacing (3.125 Hz).
	Freq float64

	// DT is the time offset in seconds from the nominal TX start.
	// Negative = arrived early; positive = arrived late.
	DT float64

	// SyncPower is the matched-filter SNR score from the sync
	// detector that flagged this candidate. Higher = stronger
	// signal relative to noise floor.
	SyncPower float64

	// Message is the parsed FT8 Message struct (call signs, grid,
	// report, etc.). Use codec.FormatMessage(Message) to render
	// the operator-facing text form.
	Message codec.Message

	// Text is the formatted operator-facing rendering of Message
	// (e.g. "K1JT W9XYZ FN20"). Pre-computed during decode so
	// callers don't have to re-run FormatMessage themselves.
	Text string
}

// DecodeOptions configures the FT8 decode pipeline. Zero-valued
// fields use the documented per-stage defaults.
type DecodeOptions struct {
	// Sync configures the Costas-sync detector — frequency search
	// range, threshold, max candidates. See dsp.SyncOptions for
	// per-field defaults.
	Sync dsp.SyncOptions

	// LDPCMaxIterations bounds the belief-propagation decoder.
	// Default (when zero): codec.LDPCMaxIterationsDefault (50).
	LDPCMaxIterations int
}

// Decode runs the full FT8 audio → messages pipeline on a 15-second
// audio slot:
//
//  1. Spectrogram (sliding 3840-point FFT across 12 kHz audio).
//  2. Costas sync detection → candidate (freq, time, score) list.
//  3. For each candidate:
//     a. Baseband downsample (FFT-based mix + decimate, 12 kHz → 200 Hz).
//     b. Soft demodulation (per-symbol 32-point FFT, L_j formula → 174 LLRs).
//     c. LDPC + CRC14 decode → 77-bit message body.
//     d. ParseMessage on the bits → codec.Message struct.
//     e. FormatMessage → operator-facing text.
//  4. Collect successful decodes; return.
//
// Candidates that fail at any stage (LDPC non-convergence, CRC
// mismatch, message parse error) are silently dropped — the
// upstream sync detector is permissive by design (typically 100
// candidates per slot for a busy band), and the LDPC + CRC chain
// is the structural validator that separates genuine signals from
// noise.
//
// Returns a fresh slice; nil for malformed audio (passed through
// from Spectrogram's contract).
func Decode(audio []float32, opts DecodeOptions) []DecodedMessage {
	maxIters := opts.LDPCMaxIterations
	if maxIters <= 0 {
		maxIters = codec.LDPCMaxIterationsDefault
	}

	spec := dsp.Spectrogram(audio)
	cands := dsp.Sync(spec, opts.Sync)
	if len(cands) == 0 {
		return nil
	}

	var out []DecodedMessage
	for _, c := range cands {
		// Mix the candidate down to baseband and demodulate.
		baseband := dsp.Downsample(audio, c.Freq)
		if baseband == nil {
			continue
		}
		llrs := dsp.Demodulate(baseband, c.DT)
		if llrs == nil {
			continue
		}

		// LDPC + CRC14 → 77-bit message body.
		msgBits, ok := codec.LDPCDecode(llrs, maxIters)
		if !ok {
			continue
		}

		// Parse the bits into a Message struct.
		msg, err := codec.DecodeMessage(msgBits)
		if err != nil {
			continue
		}

		// Render to operator-facing text. Failures here would be
		// strange (we just decoded the message from its own bits)
		// but skip rather than crash.
		text, err := codec.FormatMessage(msg)
		if err != nil {
			continue
		}

		out = append(out, DecodedMessage{
			Freq:      c.Freq,
			DT:        c.DT,
			SyncPower: c.SyncPower,
			Message:   msg,
			Text:      text,
		})
	}

	return out
}
