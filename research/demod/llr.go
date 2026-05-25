package demod

import "math"

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
// alpha in LLRs: for each of the 58 data symbols, exclude the
// strongest tone and accumulate the remaining 7. The result is the
// mean over all 58 × 7 = 406 collected samples.
//
// Excluding the strongest tone per symbol prevents the signal from
// pumping up its own noise estimate. The plain mean is the first
// cut; a trimmed mean or median over the 406 samples would be more
// robust against second-signal QRM leaking into the candidate's
// 8-tone set — the trade is one sort/partition vs the current O(N).
// Upgrade if real-capture decode parity shows the noise scale
// drifting with co-channel interference.
func estimateNoise(energies [dataSymbolCount][ft8ToneCount]float64) float64 {
	var sum float64
	count := 0
	for i := 0; i < dataSymbolCount; i++ {
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
			sum += energies[i][t]
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
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
