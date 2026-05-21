package ft8

import (
	"github.com/ColonelBlimp/station-manager/internal/audio"
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
	// (e.g. "K1JT W9XYZ FN20"). Pre-computed during decoding, so
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

	// HashTable, if non-nil, is used to Observe each decoded
	// Message (populating the table with newly-seen plaintext
	// callsigns from this slot) and then Resolve any sentinel-
	// bearing Messages (swapping in real callsigns from the
	// table). Callers should reuse the same table across Decode
	// invocations on consecutive slots — FT8 protocol design
	// relies on cross-slot state: one Type 4 transmission of
	// "PJ4/K1ABC" seeds resolution of many later Type 1/2/3
	// references that carry just the 22-bit hash.
	//
	// Without a HashTable, hash-bearing Type 1/2/3 messages still
	// decode (the codec produces Call1/Call2 = "<...>" sentinel +
	// Hash22Call1/Hash22Call2) but FormatMessage rejects the
	// sentinel and the message drops out of the returned slice.
	// Provide a HashTable to retain these messages.
	HashTable *codec.HashTable

	// FineTimingDisabled turns off the within-symbol fine-timing
	// retry path. When false (default), candidates whose coarse-DT
	// LDPC attempt fails get retried at ±5 ms and ±10 ms shifts —
	// recovers borderline decodes where the sync detector's 40 ms
	// quantisation puts the symbol-extraction window a few baseband
	// samples off the true TX alignment. The retry is cheap (only
	// runs on failed candidates) and bounded (5 attempts max). Set
	// true for strict-coarse-only behaviour, e.g. when benchmarking
	// against a sync-detector baseline.
	FineTimingDisabled bool
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
// Returns a fresh slice; returns nil for nil or empty audio
// (rather than panicking — finding #6 from the post-Session-78
// review fixed the contract mismatch where the doc claimed nil
// behaviour but dsp.Spectrogram(nil) panicked).
func Decode(samples []float32, opts DecodeOptions) []DecodedMessage {
	if len(samples) == 0 {
		return nil
	}

	maxIters := opts.LDPCMaxIterations
	if maxIters <= 0 {
		maxIters = codec.LDPCMaxIterationsDefault
	}

	spec := dsp.Spectrogram(samples)
	cands := dsp.Sync(spec, opts.Sync)
	if len(cands) == 0 {
		return nil
	}

	// Build the two FFT plans this slot needs ONCE up-front:
	//
	//   - forwardPlan (NFFT1DS = 192000): for the one-shot audio →
	//     spectrum FFT used by ForwardSpectrum.
	//   - inversePlan (NFFT2 = 3200): reused across every candidate's
	//     IFFT in DownsampleFromSpectrumWithPlan. ~100 candidates ×
	//     one IFFT each in a typical slot.
	//
	// Plan construction is non-trivial (twiddle table + 2× workspace
	// allocation, plus N math.Sincos calls). Hoisting both out of
	// the per-candidate loop is the Phase-2 follow-on win after
	// caching the forward FFT itself.
	forwardPlan := audio.NewPlan(dsp.NFFT1DS)
	inversePlan := audio.NewPlan(dsp.NFFT2)
	// Per-symbol FFT plan for the demodulator's 58 small (size=32)
	// FFTs per call. With fine-timing retries enabled, we may call
	// Demodulate up to 5× per failed candidate; without plan reuse
	// that's 58 × 5 × 100 = 29,000 Plan constructions per slot.
	symPlan := audio.NewPlan(dsp.SymbolFFTSize)

	// Compute the audio's 192k forward FFT ONCE for this slot. Every
	// candidate downsamples the same audio at a different centre
	// frequency, so the forward FFT is identical across them — only
	// the bin extraction + IFFT vary per candidate.
	spectrum := dsp.ForwardSpectrumWithPlan(samples, forwardPlan)

	// **Pass 1:** decode every candidate that survives the structural
	// validators (LDPC + CRC14 + DecodeMessage). Accumulate raw
	// Messages alongside their sync metadata for the post-decode
	// Resolve pass. Messages may carry sentinel call slots ("<...>")
	// + Hash22Call1/Hash22Call2 fields when c28 landed in the hash
	// partition.
	type pending struct {
		cand dsp.Candidate
		msg  codec.Message
	}
	// Fine-timing retry offsets in seconds. Tried in order; the
	// first one that produces a successful LDPC+CRC+parse wins.
	// 0.0 (coarse DT) goes first, so cases where coarse alignment
	// already works pay no extra cost. At Fs2=200 Hz baseband the
	// shifts correspond to ±1 and ±2 baseband samples — within
	// the 32-sample demod symbol window, no cross-symbol leakage.
	//
	// Empirically (3 fixtures, Session 80 measurement):
	//   - ±5ms: cap1=1, cap2=6, cap3=9  (+5 decodes vs no fine-timing)
	//   - ±5+±10ms: cap1=1, cap2=6, cap3=11 (+6 decodes total)
	// The ±10ms retries DO matter — cap3 picked up two extra
	// decodes (`5Z4VJ YB1RUS OI33` at sync 8.03 and `CQ SP4MSY
	// KO13` at sync 4.19) that ±5ms alone didn't recover.
	fineOffsets := []float64{0, -0.005, 0.005, -0.010, 0.010}
	if opts.FineTimingDisabled {
		fineOffsets = fineOffsets[:1] // coarse only
	}

	var raw []pending
	for _, c := range cands {
		baseband := dsp.DownsampleFromSpectrumWithPlan(spectrum, c.Freq, inversePlan)
		if baseband == nil {
			continue
		}

		var foundMsg codec.Message
		decoded := false
		for _, dOffset := range fineOffsets {
			llrs := dsp.DemodulateWithPlan(baseband, c.DT+dOffset, symPlan)
			if llrs == nil {
				continue
			}
			msgBits, ok := codec.LDPCDecode(llrs, maxIters)
			if !ok {
				continue
			}
			msg, err := codec.DecodeMessage(msgBits)
			if err != nil {
				continue
			}
			foundMsg = msg
			decoded = true
			break // first offset that works wins
		}
		if decoded {
			raw = append(raw, pending{c, foundMsg})
		}
	}

	// **Pass 2 (optional):** if the caller supplied a HashTable,
	// Observe every decoded Message first (populating the table
	// with all plaintext callsigns we just saw this slot), then
	// Resolve each Message (swapping sentinels for real callsigns
	// using the now-populated table + any cross-slot state the
	// caller has accumulated). The two-pass shape ensures a Type 4
	// transmission in this slot can resolve a Type 1 hash reference
	// also in this slot, regardless of the order they appeared in
	// the sync candidate list.
	if opts.HashTable != nil {
		for _, p := range raw {
			opts.HashTable.Observe(p.msg)
		}
		for i := range raw {
			raw[i].msg = opts.HashTable.Resolve(raw[i].msg)
		}
	}

	// **Pass 3:** format each Message to its operator-facing text.
	// Messages with unresolved sentinel slots (no HashTable, or the
	// table didn't know the call yet) fail FormatMessage and drop
	// out here — fixing that requires either a richer "render with
	// sentinel inline" formatter or eventual table population.
	out := make([]DecodedMessage, 0, len(raw))
	for _, p := range raw {
		text, err := codec.FormatMessage(p.msg)
		if err != nil {
			continue
		}
		out = append(out, DecodedMessage{
			Freq:      p.cand.Freq,
			DT:        p.cand.DT,
			SyncPower: p.cand.SyncPower,
			Message:   p.msg,
			Text:      text,
		})
	}

	return out
}
