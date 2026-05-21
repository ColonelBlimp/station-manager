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

	// FineTimingOffsets configures the per-candidate fine-timing
	// retry sequence applied when a candidate's coarse-DT LDPC
	// attempt fails. Values are DT offsets in SECONDS added to the
	// sync detector's coarse DT. The 0.0 coarse value is implicit
	// and always tried first — entries here are the additional
	// retry steps.
	//
	// Resolution rules:
	//   - nil → DefaultFineTimingOffsets (4 retries at ±5/±10 ms).
	//   - non-nil empty slice → no retries (coarse-only behaviour;
	//     useful for benchmarking against a sync-detector baseline).
	//   - non-nil populated slice → exactly the offsets given.
	//
	// Empirical context for the default values: at Fs2 = 200 Hz
	// baseband, the ±5/±10 ms shifts correspond to ±1 and ±2
	// baseband samples — well within the 32-sample demod symbol
	// window, so no cross-symbol leakage. On the three vendored
	// fixtures the default sequence adds +6 decodes over coarse-
	// only (12 → 18). See Session-80 doc-sweep for cost/benefit.
	FineTimingOffsets []float64

	// FineFrequencyOffsets configures the per-candidate fine-
	// frequency retry sequence applied when ALL fine-timing
	// attempts at the coarse frequency fail. Values are Hz offsets
	// added to the sync detector's coarse Freq. The 0.0 coarse
	// frequency is implicit — entries here are the additional
	// retry frequencies.
	//
	// Resolution rules:
	//   - nil → DefaultFineFrequencyOffsets (4 retries at ±0.5/±1 bin).
	//   - non-nil empty slice → no frequency retries (fine-timing-only).
	//   - non-nil populated slice → exactly the offsets given.
	//
	// Each frequency retry triggers a fresh Downsample + full
	// fine-timing sequence at the new frequency. Cost scales as
	// (len(FineFrequencyOffsets) + 1) × (len(FineTimingOffsets) +
	// 1) demod attempts per failed candidate — substantial, but
	// still bounded and only paid on candidates that have already
	// failed every cheaper attempt.
	FineFrequencyOffsets []float64

	// OSDOrder controls the Ordered Statistics Decoding fallback
	// applied when LDPC's belief propagation fails to converge or
	// its CRC doesn't validate. Per Taylor 2020 §6 OSD adds ~1 dB
	// of SNR sensitivity over BP alone.
	//
	//   - DefaultOSDOrder (i.e. 0 in the zero-value case → OSD enabled
	//     at order 1) — single-bit MRB flip search. ~91 trials per
	//     candidate that BP fails. Documented sensitivity floor.
	//   - Explicit 0 → no OSD fallback (BP only).
	//   - 1 → order-1 (same as default).
	//   - 2 → order-2 (pair flips, ~4095 trials, expensive — measure
	//     before committing in batch workloads).
	//
	// The zero-value case (field left unset) maps to DefaultOSDOrder
	// so callers who don't care still get the sensitivity gain. Pass
	// a sentinel like -1 to disable explicitly.
	OSDOrder int

	// LLRScale is the K constant from Taylor 2020 §6's L_j formula
	// — every demodulator output LLR is multiplied by this scale.
	// The paper says "K is adjusted empirically"; SM's default
	// (1.0, via dsp.DefaultLLRScale) returns raw differences of
	// in-pattern vs out-of-pattern tone magnitudes.
	//
	// Resolution rules:
	//   - 0 (zero value) → dsp.DefaultLLRScale (= 1.0).
	//   - Any non-zero value → applied as-is.
	//
	// Per-candidate noise-floor-aware scaling (compute K per
	// candidate from the demodulator's noise estimate) is a
	// documented future sensitivity move. Until that lands, this
	// is a single global knob — set once per Decode call.
	LLRScale float64
}

// DefaultOSDOrder is the OSD search depth applied when
// DecodeOptions.OSDOrder is unset (zero value). Order-1 is the
// canonical sensitivity-vs-cost sweet spot.
const DefaultOSDOrder = 1

// DefaultFineTimingOffsets is the retry sequence applied when
// DecodeOptions.FineTimingOffsets is nil. Tuned empirically on the
// three vendored real-WAV fixtures; expose as a package var (not
// a const) so power-users can override globally if desired.
//
// At Fs2 = 200 Hz baseband: ±0.005 s = ±1 sample, ±0.010 s = ±2
// samples — both within the 32-sample demod symbol window.
var DefaultFineTimingOffsets = []float64{-0.005, 0.005, -0.010, 0.010}

// DefaultFineFrequencyOffsets is the retry sequence applied to a
// candidate's frequency when ALL fine-timing attempts at the
// coarse frequency fail. Values are frequency offsets in HZ added
// to the sync detector's coarse Freq. Tuned empirically on the
// three vendored real-WAV fixtures.
//
// The Sync detector quantises frequency to spectrogram-bin spacing
// (df = Fs / NFFT1 = 12000 / 3840 = 3.125 Hz). A transmitter that
// sits half a bin off (~1.56 Hz) gets bucketed to whichever side
// it falls on, and the demod's per-symbol FFT then sees the tone
// straddling two bins instead of landing cleanly in one. The
// ±1.5625 Hz (= df/2) and ±3.125 Hz (= df) retries pull the
// candidate's reference frequency to the correct side of the bin.
var DefaultFineFrequencyOffsets = []float64{-1.5625, 1.5625, -3.125, 3.125}

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

	// OSD order resolution. Zero value → default (order 1). Negative
	// → disabled (BP-only). Clamp positive values at OSDMaxOrder=2.
	osdOrder := opts.OSDOrder
	if osdOrder == 0 {
		osdOrder = DefaultOSDOrder
	}
	if osdOrder < 0 {
		osdOrder = 0
	}
	if osdOrder > codec.OSDMaxOrder {
		osdOrder = codec.OSDMaxOrder
	}

	// LLR scale resolution. Zero value → dsp.DefaultLLRScale (= 1.0,
	// the spec-baseline K from Taylor 2020 §6). Any non-zero value
	// is applied as-is, including negatives (which flip every LLR's
	// sign — not useful but technically valid).
	llrScale := opts.LLRScale
	if llrScale == 0 {
		llrScale = dsp.DefaultLLRScale
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
	// Fine-timing retry sequence resolution: nil → defaults;
	// non-nil → caller's exact list (possibly empty for coarse
	// only). The 0.0 coarse value is prepended in either case so
	// candidates whose coarse alignment already works pay no
	// extra cost.
	tweaks := opts.FineTimingOffsets
	if tweaks == nil {
		tweaks = DefaultFineTimingOffsets
	}
	fineOffsets := make([]float64, 0, len(tweaks)+1)
	fineOffsets = append(fineOffsets, 0)
	fineOffsets = append(fineOffsets, tweaks...)

	// Fine-frequency retry sequence — same resolution rules as
	// FineTimingOffsets. The 0.0 coarse Freq is implicit and tried
	// first; entries here are additional retry frequencies in Hz.
	// Each frequency retry triggers a fresh Downsample (different
	// f0 → different baseband samples), then re-runs the full
	// fine-timing sequence at that frequency.
	freqTweaks := opts.FineFrequencyOffsets
	if freqTweaks == nil {
		freqTweaks = DefaultFineFrequencyOffsets
	}
	fineFreqOffsets := make([]float64, 0, len(freqTweaks)+1)
	fineFreqOffsets = append(fineFreqOffsets, 0)
	fineFreqOffsets = append(fineFreqOffsets, freqTweaks...)

	var raw []pending
	for _, c := range cands {
		var foundMsg codec.Message
		decoded := false

		// Outer loop: fine-frequency retries. Inner loop: fine-
		// timing retries at the current frequency. Each combination
		// runs BP, falling back to OSD on BP failure. Both loops
		// short-circuit on first successful decode — easy candidates
		// pay only the coarse-coarse cost.
		//
		// Empirical note (Session 80): OSD-at-every-variant catches
		// ~16 more decodes across the 3 fixtures than OSD-only-at-
		// coarse would, because the LLRs differ per variant and OSD
		// recovery is variant-specific. The cost is per-variant
		// osdMRBSetup allocation; we optimise that via the codec
		// package's reusable OSDScratch instead of by running OSD
		// less often.
	freqRetry:
		for _, fOffset := range fineFreqOffsets {
			baseband := dsp.DownsampleFromSpectrumWithPlan(spectrum, c.Freq+fOffset, inversePlan)
			if baseband == nil {
				continue
			}
			for _, dOffset := range fineOffsets {
				llrs := dsp.DemodulateWithPlan(baseband, c.DT+dOffset, symPlan, llrScale)
				if llrs == nil {
					continue
				}
				msgBits, ok := codec.LDPCDecodeWithOSD(llrs, maxIters, osdOrder)
				if !ok {
					continue
				}
				msg, err := codec.DecodeMessage(msgBits)
				if err != nil {
					continue
				}
				foundMsg = msg
				decoded = true
				break freqRetry // first (freq, dt) combination that works wins
			}
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
