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

	// SubtractionPasses controls the number of EXTRA iterative
	// decode passes performed after the first (pass 0). Each extra
	// pass: (1) copy-and-subtract every signal decoded so far from a
	// working audio buffer via dsp.SubtractSignal, (2) recompute
	// Spectrogram + Sync + ForwardSpectrum on the residual, (3) run
	// the candidate decode loop again, deduplicating by message
	// body against the running set. Loops break early when a pass
	// adds zero new decodes.
	//
	// Per QEX paper §6: "successful decoders subtract decoded
	// signals from the audio so weaker signals beneath can be
	// decoded" — the canonical way to reveal weak signals that were
	// masked by stronger ones in their frequency neighbourhood.
	//
	// Resolution rules:
	//   - 0 (zero value) → no extra passes — Decode runs as the
	//     pre-subtraction baseline did. **Today's default until
	//     real-capture sensitivity gains justify enabling.**
	//   - 1 → one extra pass after the initial decode (commonly
	//     enough; per WSJT-X experience most reveals happen in the
	//     second pass).
	//   - 2+ → diminishing returns; cost scales with the per-pass
	//     decode budget (~600 ms each at SM's current performance).
	//   - Negative → treated as 0.
	//
	// Cost: each extra pass costs ~600-800 ms (Spectrogram + Sync +
	// ForwardSpectrum + candidate loop + subtraction overhead) plus
	// the subtraction calls themselves (~7 ms × number of decoded
	// signals). At 4.5 s baseline + 1 extra pass, total stays well
	// under the 15-second slot budget.
	//
	// NOTE (2026-05 corpus measurement): against the jt9 3.0.1 oracle,
	// subtraction passes added ZERO oracle-matched decodes and only
	// manufactured false positives. Default stays 0; revisit only if a
	// future demod improvement makes pass-0 strong enough that
	// subtraction reveals genuinely-masked weak signals.
	SubtractionPasses int

	// MinSyncPower is the sync-power acceptance floor (B1) applied to
	// the FINAL decode set: a decoded candidate whose SyncPower is
	// below this is dropped. Low-SyncPower decodes are overwhelmingly
	// CRC14-survivor false positives (an OSD bit-flip search finds a
	// codeword that passes the 1-in-16384 CRC at a low-confidence
	// candidate).
	//
	// Resolution rules:
	//   - 0 (zero value) → DefaultMinSyncPower (3.0).
	//   - Negative → no floor (keep every CRC-valid decode).
	//   - Positive → exact floor.
	//
	// This is a blunt first-stage filter; it cannot separate the false
	// positives that happen to land above the floor from real decodes
	// at the same SyncPower. The OSD soft-distance gate (B2) handles
	// those. See DefaultMinSyncPower for the tuning rationale.
	MinSyncPower float64

	// OSDMaxNormDist is the OSD soft-distance acceptance ceiling (B2):
	// an OSD-produced codeword is accepted only if its reliability-
	// weighted normalised soft distance to the received LLRs is at or
	// below this (see codec.softDistanceNorm). It targets the false
	// positives that survive the MinSyncPower floor — CRC14-lottery
	// codewords from OSD's bit-flip search that disagree with the
	// received signal at high-reliability bit positions. BP-converged
	// decodes are NOT gated (they're trustworthy).
	//
	// Resolution rules:
	//   - 0 (zero value) → DefaultOSDMaxNormDist.
	//   - Negative → no gate (accept any CRC-valid OSD codeword).
	//   - Positive → exact ceiling, in [0,1].
	OSDMaxNormDist float64

	// Stats, when non-nil, is populated by Decode with per-candidate
	// retry diagnostics covering the main pass. Zero-cost when nil.
	// Used by ft8-eval -stats to investigate the candidate-cap /
	// retry-budget tradeoff. Sub-pass (iterative subtraction) work
	// is NOT recorded; subtraction is queued for removal anyway.
	Stats *DecodeStats
}

// DecodeStats holds per-candidate diagnostic counts for one Decode
// call. Populated only when DecodeOptions.Stats is non-nil.
type DecodeStats struct {
	// Sync-stage counts (mirrored from dsp.SyncStats so callers see
	// the whole pipeline through one struct).
	RawSyncCandidates int // pre-dedup, above MinScore
	AfterDedup        int // post near-duplicate suppression
	AfterCap          int // final candidates handed to demod

	// Per-candidate records, length == AfterCap. Records are in the
	// same order as the candidates handed to the decode loop (sync-
	// power descending after dedup).
	Candidates []CandidateStat
}

// CandidateStat records what happened to a single candidate during
// the main decode pass.
type CandidateStat struct {
	Freq      float64 // candidate centre frequency (Hz)
	DT        float64 // candidate time offset (s)
	SyncPower float64 // matched-filter SNR ratio from sync

	// Decoded reports whether the candidate produced a valid
	// (LDPC + CRC + parsable) message. False means every (freq,
	// timing) retry combo was exhausted without success.
	Decoded bool

	// FreqIdxUsed / TimeIdxUsed are the positions in the resolved
	// fine-freq / fine-timing retry sequences where the successful
	// decode landed. Index 0 = the implicit 0.0 coarse value; index
	// ≥ 1 = a fine retry. Both zero ⇒ decoded at coarse-coarse.
	// Meaningful only when Decoded is true.
	FreqIdxUsed int
	TimeIdxUsed int

	// Attempts is the total number of (freq, timing) demod attempts
	// that actually ran for this candidate before exit. For decoded
	// candidates this is (FreqIdxUsed * len(timing) + TimeIdxUsed + 1).
	// For failed candidates it is the full retry budget that ran
	// (some attempts may early-exit if downsample/demod returns nil).
	Attempts int

	// BestMeanAbsLLR is the maximum value of mean(|LLR|) seen across
	// all this candidate's demod attempts (across the freq×timing
	// retry grid). Higher = LLR distribution was "more confident" at
	// the best attempt. Used to calibrate the LLR-quality early-exit
	// gate: plot distributions of Decoded=true vs Decoded=false
	// candidates and find where they separate.
	BestMeanAbsLLR float64

	// SuccessMeanAbsLLR is the mean(|LLR|) for the attempt that
	// actually decoded, populated only when Decoded is true. May be
	// lower than BestMeanAbsLLR if a different retry combo produced
	// stronger LLRs but didn't decode (e.g. due to BP convergence
	// quirks). Diagnostic-only.
	SuccessMeanAbsLLR float64
}

// DefaultOSDOrder is the OSD search depth applied when
// DecodeOptions.OSDOrder is unset (zero value). Order-1 is the
// canonical sensitivity-vs-cost sweet spot.
const DefaultOSDOrder = 1

// DefaultSubtractionPasses is the number of EXTRA iterative
// subtraction passes applied when DecodeOptions.SubtractionPasses
// is unset (zero value). Currently 0 — subtraction is opt-in until
// real-capture sensitivity gains are measured. To enable, the
// caller explicitly sets DecodeOptions.SubtractionPasses ≥ 1.
const DefaultSubtractionPasses = 0

// DefaultMinSyncPower is the default sync-power acceptance floor (B1).
// Decoded candidates with SyncPower below this are dropped as likely
// CRC14-survivor false positives. dsp.SyncDefaultMinScore (1.5) is the
// candidate-generation threshold; this is a stricter floor applied to
// the FINAL decode set.
//
// Tuned on the 2026-05 live + vendored corpus (jt9 3.0.1 oracle): at
// 3.0, false positives dropped with ZERO loss of oracle-matched
// decodes; above ~3.5, real decodes start to be lost. The bulk of
// remaining false positives sit above this floor (sync-overlapping
// real decodes) and need the OSD soft-distance gate (B2) instead.
const DefaultMinSyncPower = 3.0

// DefaultOSDMaxNormDist is the default OSD soft-distance acceptance
// ceiling (B2), applied when DecodeOptions.OSDMaxNormDist is unset. An
// OSD codeword is rejected when its normalised soft distance to the
// received LLRs exceeds this (see codec.softDistanceNorm).
//
// Tuned on the 2026-05 live + vendored corpus (jt9 3.0.1 oracle): OSD
// recovers ~20 real decodes but produces the bulk of SM's false
// positives (CRC14-lottery survivors from its bit-flip search). The
// soft-distance band that separates them is compressed near zero —
// because OSD builds its codeword from the most-reliable bits, its
// output always agrees with high-confidence positions, so the
// discriminating signal lives in the low-reliability tail. At 0.06 the
// gate cut corpus-wide false positives 44→18 with ZERO loss of
// oracle-matched decodes; below ~0.05 real decodes start to be lost.
const DefaultOSDMaxNormDist = 0.06

// meanAbsLLR returns the mean of |x| over the slice. Used by the
// per-candidate LLR-quality measurement (diagnostic only at present;
// the early-exit gate consumes the same statistic). Returns 0 for an
// empty slice. Single pass, no allocations.
func meanAbsLLR(llrs []float64) float64 {
	if len(llrs) == 0 {
		return 0
	}
	var sum float64
	for _, v := range llrs {
		if v < 0 {
			sum -= v
		} else {
			sum += v
		}
	}
	return sum / float64(len(llrs))
}

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

	// Sync-power acceptance floor (B1). Decoded candidates below this
	// SyncPower are dropped from the output — they are overwhelmingly
	// CRC14-survivor false positives (OSD bit-flip search produces them
	// at low-confidence candidates). Zero value → DefaultMinSyncPower;
	// negative → no floor. Tuned on the 2026-05 live corpus: a floor of
	// 3.0 removed false positives with zero loss of oracle-matched
	// decodes. See DecodeOptions.MinSyncPower.
	minSync := opts.MinSyncPower
	if minSync == 0 {
		minSync = DefaultMinSyncPower
	}
	if minSync < 0 {
		minSync = 0
	}

	// OSD soft-distance acceptance ceiling (B2). Zero value →
	// DefaultOSDMaxNormDist; negative → no gate. See
	// DecodeOptions.OSDMaxNormDist and codec.softDistanceNorm.
	osdMaxNormDist := opts.OSDMaxNormDist
	if osdMaxNormDist == 0 {
		osdMaxNormDist = DefaultOSDMaxNormDist
	}
	if osdMaxNormDist < 0 {
		osdMaxNormDist = 0
	}

	// If the caller asked for diagnostics, attach a transient stats
	// sink to the sync options so dsp.Sync populates the pre-dedup,
	// post-dedup and post-cap counts. The original caller's opts is
	// passed by value, so this assignment is scoped to Decode.
	var syncStats *dsp.SyncStats
	if opts.Stats != nil {
		syncStats = &dsp.SyncStats{}
		opts.Sync.Stats = syncStats
	}

	spec := dsp.Spectrogram(samples)
	cands := dsp.Sync(spec, opts.Sync)
	if opts.Stats != nil && syncStats != nil {
		opts.Stats.RawSyncCandidates = syncStats.RawAboveThreshold
		opts.Stats.AfterDedup = syncStats.AfterDedup
		opts.Stats.AfterCap = syncStats.AfterCap
	}
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
	//
	// msgBits is retained alongside the parsed Message for two
	// reasons: (1) the iterative-subtraction loop below needs them
	// to call dsp.SubtractSignal, (2) they form the dedup key when
	// later passes might re-decode the same signal off the residual.
	type pending struct {
		cand    dsp.Candidate
		msg     codec.Message
		msgBits []byte
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
		var foundBits []byte
		decoded := false
		// Diagnostic counters per candidate. attempts counts demod
		// runs (entries into the inner-loop body); freqIdxUsed and
		// timeIdxUsed capture the position in the resolved retry
		// sequences where success landed; bestMeanAbsLLR tracks the
		// largest mean(|LLR|) observed across all attempts (for the
		// LLR-quality early-exit calibration).
		var attempts, freqIdxUsed, timeIdxUsed int
		var bestMeanAbsLLR, successMeanAbsLLR float64

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
		for fIdx, fOffset := range fineFreqOffsets {
			baseband := dsp.DownsampleFromSpectrumWithPlan(spectrum, c.Freq+fOffset, inversePlan)
			if baseband == nil {
				continue
			}
			for tIdx, dOffset := range fineOffsets {
				llrs := dsp.DemodulateWithPlan(baseband, c.DT+dOffset, symPlan, llrScale)
				if llrs == nil {
					continue
				}
				attempts++
				// Diagnostic-only LLR-quality measurement (Phase 1
				// of the early-exit work): mean of |LLR| across the
				// 174 codeword bits. Single pass; negligible cost.
				// Used to calibrate the future early-exit gate.
				meanAbs := meanAbsLLR(llrs)
				if opts.Stats != nil && meanAbs > bestMeanAbsLLR {
					bestMeanAbsLLR = meanAbs
				}
				msgBits, ok := codec.LDPCDecodeWithOSD(llrs, maxIters, osdOrder, osdMaxNormDist)
				if !ok {
					continue
				}
				msg, err := codec.DecodeMessage(msgBits)
				if err != nil {
					continue
				}
				foundMsg = msg
				foundBits = msgBits
				decoded = true
				freqIdxUsed = fIdx
				timeIdxUsed = tIdx
				if opts.Stats != nil {
					successMeanAbsLLR = meanAbs
				}
				break freqRetry // first (freq, dt) combination that works wins
			}
		}
		if decoded {
			raw = append(raw, pending{c, foundMsg, foundBits})
		}
		if opts.Stats != nil {
			opts.Stats.Candidates = append(opts.Stats.Candidates, CandidateStat{
				Freq:              c.Freq,
				DT:                c.DT,
				SyncPower:         c.SyncPower,
				Decoded:           decoded,
				FreqIdxUsed:       freqIdxUsed,
				TimeIdxUsed:       timeIdxUsed,
				Attempts:          attempts,
				BestMeanAbsLLR:    bestMeanAbsLLR,
				SuccessMeanAbsLLR: successMeanAbsLLR,
			})
		}
	}

	// **Iterative-subtraction passes (optional).** Per QEX §6, after
	// a successful decode, subtracting the synthesized waveform from
	// the audio can reveal weaker signals that were masked by the
	// stronger one's spectral footprint. Each extra pass: (1)
	// subtract every pass-(N-1) decode from a working audio buffer,
	// (2) recompute Spectrogram + Sync + ForwardSpectrum on the
	// residual, (3) re-run the candidate decode loop, deduplicating
	// by msgBits-as-key against the running raw set.
	//
	// Loops break early when a pass adds zero new decodes (no point
	// in further passes if the previous one didn't reveal anything).
	subPasses := opts.SubtractionPasses
	if subPasses < 0 {
		subPasses = 0
	}
	if subPasses > 0 && len(raw) > 0 {
		// Copy samples so caller's buffer is preserved.
		working := make([]float32, len(samples))
		copy(working, samples)

		// Seen-set keyed by 77-bit message body. Populated from pass 0
		// before the first subtraction; updated per pass.
		seen := make(map[string]bool, len(raw))
		for _, p := range raw {
			seen[string(p.msgBits)] = true
		}

		// Track what to subtract next iteration. Starts as everything
		// from pass 0.
		toSubtract := make([]pending, len(raw))
		copy(toSubtract, raw)

		for pass := 1; pass <= subPasses; pass++ {
			// Subtract every pass-(N-1) decode from the working audio.
			for _, p := range toSubtract {
				dsp.SubtractSignal(working, p.msgBits, p.cand.Freq, p.cand.DT)
			}

			// Recompute the pipeline on the residual.
			passSpec := dsp.Spectrogram(working)
			passCands := dsp.Sync(passSpec, opts.Sync)
			if len(passCands) == 0 {
				break
			}
			passSpectrum := dsp.ForwardSpectrumWithPlan(working, forwardPlan)

			var passRaw []pending
			for _, c := range passCands {
				var foundMsg codec.Message
				var foundBits []byte
				decoded := false

			freqRetryPass:
				for _, fOffset := range fineFreqOffsets {
					baseband := dsp.DownsampleFromSpectrumWithPlan(passSpectrum, c.Freq+fOffset, inversePlan)
					if baseband == nil {
						continue
					}
					for _, dOffset := range fineOffsets {
						llrs := dsp.DemodulateWithPlan(baseband, c.DT+dOffset, symPlan, llrScale)
						if llrs == nil {
							continue
						}
						msgBits, ok := codec.LDPCDecodeWithOSD(llrs, maxIters, osdOrder, osdMaxNormDist)
						if !ok {
							continue
						}
						msg, err := codec.DecodeMessage(msgBits)
						if err != nil {
							continue
						}
						foundMsg = msg
						foundBits = msgBits
						decoded = true
						break freqRetryPass
					}
				}
				if !decoded {
					continue
				}
				// Dedup against everything seen so far (raw + earlier
				// candidates in this pass).
				key := string(foundBits)
				if seen[key] {
					continue
				}
				seen[key] = true
				passRaw = append(passRaw, pending{c, foundMsg, foundBits})
			}

			if len(passRaw) == 0 {
				// No new decodes — further passes won't help.
				break
			}
			raw = append(raw, passRaw...)
			toSubtract = passRaw
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
		if p.cand.SyncPower < minSync {
			continue // B1: sync-power floor — drop low-confidence false positives.
		}
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
