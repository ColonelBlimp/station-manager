package demod

import (
	"math"
	"sort"
)

// GrayUnmap is the canonical FT8 8-FSK tone-to-bit mapping per QEX
// paper §3 Table 3 (Franke, Somerville, Taylor — "The FT4 and FT8
// Communication Protocols," QEX July/August 2020). Each tone index
// 0..7 maps to a 3-bit triad held in the low 3 bits, MSB first:
//
//	tone 0 → 000     tone 4 → 110
//	tone 1 → 001     tone 5 → 100
//	tone 2 → 011     tone 6 → 101
//	tone 3 → 010     tone 7 → 111
//
// Adjacent-tone pairs differ in exactly one bit, so a one-tone slip
// from inter-symbol crosstalk or Doppler spread produces a single
// bit-flip rather than three — this is the point of Gray coding.
// Note this is NOT the textbook "Gray by reflection" sequence: tones
// 4-7 use a different bit assignment than the standard sequence.
// Source-of-truth is QEX paper Table 3 only.
var GrayUnmap = [ft8ToneCount]uint8{0, 1, 3, 2, 6, 4, 5, 7}

const (
	// bitsPerSymbol is log2(ft8ToneCount) — the number of coded bits
	// each 8-FSK channel symbol carries.
	bitsPerSymbol = 3

	// codewordBits is the FT8 LDPC codeword length: 58 data symbols ×
	// 3 bits/symbol = 174 bits.
	codewordBits = dataSymbolCount * bitsPerSymbol // 174

	// llrClamp is the absolute-value cap applied to every output LLR.
	// ±20 is the design-discussion starting point; revisit if
	// real-capture LDPC convergence struggles at the marginal SNR end.
	llrClamp = 20.0

	// llrEps is a numerical-stability floor on the noise estimate.
	// Prevents alpha → infinity if a row is silent (all-zero energies).
	llrEps = 1e-12

	// noiseWinsorFrac is the Winsorize fraction applied at each end
	// when robustly estimating the noise floor from loser-tone
	// energies. 10% replaces the bottom 10% and top 10% of sorted
	// samples with the values at those percentile boundaries, then
	// computes the mean over all samples (including the replaced
	// extremes). This protects against QRM/leakage corrupting the
	// mean while keeping the scale close to a plain-mean estimator —
	// median would underestimate the mean for exponential-ish power
	// noise unless rescaled, risking overconfident LLRs. Operator-
	// chosen default 2026-05-26. Sweep candidates if tuning ever
	// needed: {0.05, 0.10, 0.15, 0.20}.
	noiseWinsorFrac = 0.10
)

// LLRs converts a Demod-produced 58×8 energy matrix into 174 soft
// bit metrics ready for LDPC decoding.
//
// Convention:
//
//	positive LLR → bit 0 more likely
//	negative LLR → bit 1 more likely
//
// Algorithm: for each data symbol i and bit position j (j=0..2, MSB
// first inside the 3-bit triad):
//
//	metric[t]  = alpha * energies[i][t]
//	LLR[i*3+j] = logsumexp(metric[t] : bit_j(GrayUnmap[t]) == 0)
//	           - logsumexp(metric[t] : bit_j(GrayUnmap[t]) == 1)
//
// where alpha = 1 / max(noise, eps) and noise is a per-candidate
// estimate (see estimateNoise — mean of the 7 non-strongest tones
// per symbol, averaged across all 58 data symbols).
//
// Full log-sum-exp (not max-log) is used deliberately: with only 8
// tones the cost difference is negligible, but logsumexp preserves
// ambiguity when several wrong tones are close, weakening the LLR
// rather than ignoring all but the single strongest competitor.
// Near-threshold SNR is where this softening matters for BP/OSD
// ranking.
//
// Output LLRs are clamped to ±llrClamp so a single pathological
// symbol (e.g. a tone bin picking up DC offset or QRM) can't
// dominate the LDPC decoder's parity equations.
func LLRs(energies [dataSymbolCount][ft8ToneCount]float64) [codewordBits]float64 {
	alpha := 1.0 / math.Max(estimateNoise(energies), llrEps)

	var out [codewordBits]float64
	var metric [ft8ToneCount]float64
	for i := 0; i < dataSymbolCount; i++ {
		for t := 0; t < ft8ToneCount; t++ {
			metric[t] = alpha * energies[i][t]
		}
		for j := 0; j < bitsPerSymbol; j++ {
			mask := uint8(1) << uint(bitsPerSymbol-1-j) // MSB first
			// 4-4 partition is structural: every bit column of
			// GrayUnmap has exactly four 0s and four 1s (Gray code
			// over all 8 values requires balanced bit columns). The
			// [4]float64 sizing assumes this invariant — there's a
			// dedicated test (TestGrayUnmap_Is4_4Partitioned) that
			// fails loudly if the table is ever mutated to break it.
			var bit0, bit1 [4]float64
			n0, n1 := 0, 0
			for t := 0; t < ft8ToneCount; t++ {
				if GrayUnmap[t]&mask == 0 {
					bit0[n0] = metric[t]
					n0++
				} else {
					bit1[n1] = metric[t]
					n1++
				}
			}
			llr := logSumExp4(bit0) - logSumExp4(bit1)
			if llr > llrClamp {
				llr = llrClamp
			} else if llr < -llrClamp {
				llr = -llrClamp
			}
			out[i*bitsPerSymbol+j] = llr
		}
	}
	return out
}

// estimateNoise returns the per-candidate noise floor used to set
// alpha in LLRs. Algorithm:
//
//  1. Skip data-symbol rows whose total energy is zero — these are
//     symbols whose audio window fell outside the input buffer in
//     Demod (slot-edge handling). Including them would bias the
//     noise estimate toward zero.
//  2. For each remaining row, exclude the strongest tone (the
//     signal-aligned one) and collect the other 7 tone energies.
//  3. Sort the collected samples.
//  4. Winsorize: replace the lowest noiseWinsorFrac of values with
//     the value at that boundary, and the highest noiseWinsorFrac
//     with the value at THAT boundary. This caps extremes from QRM
//     or adjacent-signal leakage without dropping the count.
//  5. Return the mean of the Winsorized samples.
//
// Winsorized over trimmed because trimming reduces the effective
// sample count and biases low; Winsorized keeps every contribution
// and caps the outliers in place. Over median because for
// exponential-ish power noise the median underestimates the mean
// without rescaling, which risks overconfident LLRs (saturating to
// the ±llrClamp ceiling on noise alone).
//
// Replaces the prior plain-mean estimator (Session 94+, 2026-05-26).
// Performance: O(N log N) for the sort vs O(N) before; N=406, so the
// added cost is ~12k extra comparisons per LLR call — negligible.
func estimateNoise(energies [dataSymbolCount][ft8ToneCount]float64) float64 {
	samples := make([]float64, 0, dataSymbolCount*(ft8ToneCount-1))
	for i := 0; i < dataSymbolCount; i++ {
		var rowSum float64
		for t := 0; t < ft8ToneCount; t++ {
			rowSum += energies[i][t]
		}
		if rowSum <= 0 {
			// Symbol window fell outside the audio buffer in Demod.
			// Don't let zeroed rows pull the noise estimate down.
			continue
		}
		maxIdx := 0
		for t := 1; t < ft8ToneCount; t++ {
			if energies[i][t] > energies[i][maxIdx] {
				maxIdx = t
			}
		}
		for t := 0; t < ft8ToneCount; t++ {
			if t == maxIdx {
				continue
			}
			samples = append(samples, energies[i][t])
		}
	}
	if len(samples) == 0 {
		return 0
	}
	sort.Float64s(samples)
	trim := int(float64(len(samples)) * noiseWinsorFrac)
	if trim > 0 && trim*2 < len(samples) {
		loCap := samples[trim]
		hiCap := samples[len(samples)-1-trim]
		for i := 0; i < trim; i++ {
			samples[i] = loCap
		}
		for i := len(samples) - trim; i < len(samples); i++ {
			samples[i] = hiCap
		}
	}
	var sum float64
	for _, v := range samples {
		sum += v
	}
	return sum / float64(len(samples))
}

// logSumExp4 is the numerically-stable log-sum-exp over a fixed
// 4-input slice (every LLR partition is exactly 4 tones × 4 tones by
// the Gray-code balanced-column invariant). Max-shift the inputs
// before exponentiating so the largest argument to exp() is always
// zero — keeps the result finite even when metric magnitudes are
// large enough that raw exp() would overflow.
func logSumExp4(x [4]float64) float64 {
	maximum := x[0]
	for i := 1; i < 4; i++ {
		if x[i] > maximum {
			maximum = x[i]
		}
	}
	sum := 0.0
	for i := 0; i < 4; i++ {
		sum += math.Exp(x[i] - maximum)
	}
	return maximum + math.Log(sum)
}

// LLRsCalibrated is the Costas-anchor-calibrated variant of LLRs:
// instead of estimating the noise scale from data-symbol loser tones
// (which can be contaminated by adjacent-signal leakage, QRM, or
// partial-coverage rows), it takes the noise level as a parameter —
// typically the output of EstimateCostasCalibration evaluated at the
// candidate's (samples, freqHz, dtSec).
//
// Costas anchors are known-tone reference points, so their measurement
// of expected-vs-other-tone power is a cleaner calibration source than
// data-symbol energies where the signal-aligned tone is unknown a priori.
//
// alpha = 1 / max(noiseLevel, llrEps). The signal level returned by
// EstimateCostasCalibration is NOT used in the metric scale here — it
// is available for diagnostics and potential future per-symbol
// reliability weighting (deferred per operator directive 2026-05-26).
//
// Sign convention and clamp are identical to LLRs. The logsumexp
// partition over Gray-code bit positions is unchanged. Only the
// noise-scale source differs.
func LLRsCalibrated(energies [dataSymbolCount][ft8ToneCount]float64, noiseLevel float64) [codewordBits]float64 {
	alpha := 1.0 / math.Max(noiseLevel, llrEps)

	var out [codewordBits]float64
	var metric [ft8ToneCount]float64
	for i := 0; i < dataSymbolCount; i++ {
		for t := 0; t < ft8ToneCount; t++ {
			metric[t] = alpha * energies[i][t]
		}
		for j := 0; j < bitsPerSymbol; j++ {
			mask := uint8(1) << uint(bitsPerSymbol-1-j) // MSB first
			var bit0, bit1 [4]float64
			n0, n1 := 0, 0
			for t := 0; t < ft8ToneCount; t++ {
				if GrayUnmap[t]&mask == 0 {
					bit0[n0] = metric[t]
					n0++
				} else {
					bit1[n1] = metric[t]
					n1++
				}
			}
			llr := logSumExp4(bit0) - logSumExp4(bit1)
			if llr > llrClamp {
				llr = llrClamp
			} else if llr < -llrClamp {
				llr = -llrClamp
			}
			out[i*bitsPerSymbol+j] = llr
		}
	}
	return out
}

// LLRsCoherent converts pre-scaled coherent metrics from DemodCoherent
// into 174 bit LLRs. Differs from LLRs only in that it skips the
// per-candidate alpha estimation — the metrics fed in are already
// scaled by Ahat/σ² inside DemodCoherent, so this function just
// runs the same logsumexp partition + ±llrClamp as the incoherent
// path.
//
// Sign convention is the same as LLRs: positive output LLR ⇒ bit 0
// more likely, negative ⇒ bit 1.
//
// The input metrics are signed (unlike LLRs's unsigned |X|² energies),
// but logsumexp handles signed inputs without modification — large
// negative metrics fold into the partition's log-sum-exp at their
// natural weight.
func LLRsCoherent(metrics [dataSymbolCount][ft8ToneCount]float64) [codewordBits]float64 {
	var out [codewordBits]float64
	for i := 0; i < dataSymbolCount; i++ {
		for j := 0; j < bitsPerSymbol; j++ {
			mask := uint8(1) << uint(bitsPerSymbol-1-j) // MSB first
			var bit0, bit1 [4]float64
			n0, n1 := 0, 0
			for t := 0; t < ft8ToneCount; t++ {
				if GrayUnmap[t]&mask == 0 {
					bit0[n0] = metrics[i][t]
					n0++
				} else {
					bit1[n1] = metrics[i][t]
					n1++
				}
			}
			llr := logSumExp4(bit0) - logSumExp4(bit1)
			if llr > llrClamp {
				llr = llrClamp
			} else if llr < -llrClamp {
				llr = -llrClamp
			}
			out[i*bitsPerSymbol+j] = llr
		}
	}
	return out
}
