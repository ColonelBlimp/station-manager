package sandbox

import (
	"math"
	"sort"
)

// FT8CodewordBits is the number of soft bits emitted per candidate:
// 58 data symbols × 3 bits/symbol = 174. This is the input width of
// the FT8 LDPC(174, 91) code.
const FT8CodewordBits = 174

// inverseGrayMap converts an FT8 8-FSK tone index into its 3-bit
// payload (MSB-first). This is the Gray-coded tone-to-bits table from
// the QEX 2020 paper § 3 / Table 1: adjacent FT8 tones (in frequency)
// differ by exactly one bit, so a one-tone misclassification flips at
// most one bit of the demodulated symbol.
//
// Tone-to-bits mapping (verified bit-pattern XOR is single-bit for
// every adjacent tone pair, tones 0 through 7):
//
//	tone 0 ↔ 000 (0)        tone 4 ↔ 110 (6)
//	tone 1 ↔ 001 (1)        tone 5 ↔ 100 (4)
//	tone 2 ↔ 011 (3)        tone 6 ↔ 101 (5)
//	tone 3 ↔ 010 (2)        tone 7 ↔ 111 (7)
var inverseGrayMap = [8]int{0, 1, 3, 2, 6, 4, 5, 7}

// dataSymbolIndices lists the 58 non-Costas symbol positions in the
// 79-symbol FT8 message, in temporal order. Costas anchor blocks
// occupy symbols 0-6, 36-42, and 72-78 (per QEX § 2); the rest carry
// the LDPC codeword payload.
//
// codeword bit cbi maps to data symbol dataSymbolIndices[cbi/3], with
// MSB-first ordering within the symbol's 3 bits (cbi%3 == 0 is MSB).
var dataSymbolIndices [58]int

func init() {
	i := 0
	for s := 7; s < 36; s++ {
		dataSymbolIndices[i] = s
		i++
	}
	for s := 43; s < 72; s++ {
		dataSymbolIndices[i] = s
		i++
	}
}

// SoftLLRs computes the 174 max-log-MAP log-likelihood ratios for the
// FT8 codeword from a SymbolGrid's data-symbol tone powers.
//
// Convention:
//
//	LLR > 0  → bit favours 0
//	LLR < 0  → bit favours 1
//	|LLR|    → reliability (larger = more confident)
//
// Computation, per data symbol:
//
//  1. The 3 codeword bits are b2 (MSB), b1, b0 (LSB).
//  2. For each bit position, the 8 tones partition into 4 tones with
//     bit=0 and 4 tones with bit=1 (under the inverseGrayMap).
//  3. Max-log approximation:
//     LLR(bit) = max{power[t] : t with bit=0} - max{power[t] : t with bit=1}.
//
// magnitudeMode selects the demap domain:
//
//   - false: power domain (default for back-compat) — LLR = max0 − max1
//     where the maxes are over |C|² values.
//   - true: magnitude domain (QEX § 6 spec-aligned) — LLR =
//     √max0 − √max1, equivalent to max(|C|, x=0) − max(|C|, x=1)
//     because √ is monotonic over [0, ∞). See qex-derivation.md
//     § 3.1.1 for the math and the rationale.
//
// No noise normalisation is applied here — LLRs are in either |C|²
// units (power mode) or |C| units (magnitude mode). Downstream
// consumers that need an absolute scale (e.g. LDPC belief propagation)
// should normalise by an estimate of the per-symbol noise variance.
//
// Output ordering: codeword bit cbi corresponds to data symbol
// dataSymbolIndices[cbi/3], with cbi%3 == 0 being the MSB of that
// symbol's bit triplet (per FT8 spec).
func SoftLLRs(grid *SymbolGrid, magnitudeMode bool) [FT8CodewordBits]float64 {
	var llrs [FT8CodewordBits]float64
	for d := 0; d < 58; d++ {
		sym := dataSymbolIndices[d]
		powers := grid.Tones[sym]
		for bitPos := 2; bitPos >= 0; bitPos-- {
			max0 := -math.MaxFloat64
			max1 := -math.MaxFloat64
			for tone := 0; tone < 8; tone++ {
				bits := inverseGrayMap[tone]
				if (bits>>bitPos)&1 == 0 {
					if powers[tone] > max0 {
						max0 = powers[tone]
					}
				} else {
					if powers[tone] > max1 {
						max1 = powers[tone]
					}
				}
			}
			// codeword bit: 3*d for MSB, 3*d+1 for middle, 3*d+2 for LSB.
			cbi := 3*d + (2 - bitPos)
			llrs[cbi] = demapDiff(max0, max1, magnitudeMode)
		}
	}
	return llrs
}

// demapDiff returns the LLR for one bit, given the two max-power
// values from the max-log demap (max0 over tones with bit=0, max1
// over tones with bit=1). When magnitudeMode is false the result is
// (max0 − max1) in |C|² units; when true the sqrt is applied first,
// yielding the QEX § 6 spec form (max|C|_{x=0} − max|C|_{x=1}).
//
// Monotonicity: √ over [0, ∞) preserves argmax, so √max(P) = max(√P)
// for non-negative P. Doing the sqrt once at the end is cheaper than
// sqrt-ing every comparison.
func demapDiff(max0, max1 float64, magnitudeMode bool) float64 {
	if magnitudeMode {
		return math.Sqrt(max0) - math.Sqrt(max1)
	}
	return max0 - max1
}

// HardBits converts soft LLRs to a hard-decision codeword: bit 0 when
// LLR > 0, bit 1 when LLR < 0. Ties (LLR == 0) resolve to bit 0 by
// convention; in practice ties occur only when two equal tones tie
// per bit position, which is rare on real-world fixtures.
func HardBits(llrs [FT8CodewordBits]float64) [FT8CodewordBits]uint8 {
	var bits [FT8CodewordBits]uint8
	for i, l := range llrs {
		if l < 0 {
			bits[i] = 1
		}
	}
	return bits
}

// SoftLLRsN2 computes a 174-bit LLR vector using N=2 block detection
// per QEX 2020 paper § 6: adjacent pairs of data symbols' complex N=1
// correlations (SymbolGrid.Amps) are coherently combined into 64 N=2
// block correlations, magnitudes-squared are taken, and max-log demap
// produces 6 bit LLRs per pair.
//
// Phase-coherence assumption: each block spans 2 × 0.160 s = 0.32 s.
// The paper notes block detection requires phase stability over the
// block length and "is said to be noncoherent because phase continuity
// between sequences is not assumed". This is a strictly stronger
// channel-coherence demand than N=1; it pays off when met.
//
// Pairing layout: data symbols are split by the middle Costas anchor
// block (positions 36-42) into two contiguous halves of 29 symbols
// each (dataSymbolIndices[0..28] = positions 7..35;
// dataSymbolIndices[29..57] = positions 43..71). Pairs form
// non-overlapping adjacent couples within each half: (0,1), (2,3),
// ..., (26,27) and (29,30), ..., (55,56). Each half's leftover
// symbol (d=28, d=57) falls back to N=1 LLRs — a clean 3 bits per
// leftover for 6 total of the 174.
//
// Pairs are NOT formed across the middle Costas gap: bridging d=28
// (position 35) and d=29 (position 43) would require phase coherence
// over 1.12 s, beyond what the N=2 block-detection assumption can
// reasonably claim.
//
// Output convention matches SoftLLRs: positive LLR favours bit 0.
// Output scale is in |C|² units (power), consistent with the existing
// SoftLLRs; BP's median-LLR normalisation handles the per-set scale
// difference.
//
// Per-pair complexity is 64 complex sums + 64 magnitudes + 6 × 64
// scans = O(384) operations. Across 28 pairs × 2 halves this is well
// below 25 k ops total — negligible against the cost of a candidate's
// channelize + refine + symbol-FFT path.
func SoftLLRsN2(grid *SymbolGrid, magnitudeMode bool) [FT8CodewordBits]float64 {
	var llrs [FT8CodewordBits]float64
	pairHalfLLRs(grid, &llrs, 0, 29, magnitudeMode)
	pairHalfLLRs(grid, &llrs, 29, 58, magnitudeMode)
	return llrs
}

// pairHalfLLRs fills the LLR slots for data-symbol indices in [start, end)
// using non-overlapping N=2 pairs (with the trailing odd symbol, if any,
// falling back to N=1). Operates in place on llrs.
func pairHalfLLRs(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, start, end int, magnitudeMode bool) {
	d := start
	for ; d+1 < end; d += 2 {
		fillPairLLRsN2(grid, llrs, d, d+1, magnitudeMode)
	}
	if d < end {
		fillSymbolLLRsN1(grid, llrs, d, magnitudeMode)
	}
}

// fillPairLLRsN2 computes the 64 N=2 block correlations C_{m1,m2} =
// Amps[s1][m1] + Amps[s2][m2], their magnitudes-squared, and the
// max-log bit LLRs for the 6 codeword bits owned by the pair
// (d1, d2). Writes into codeword bit positions 3·d1..3·d1+2 and
// 3·d2..3·d2+2.
func fillPairLLRsN2(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, d1, d2 int, magnitudeMode bool) {
	s1 := dataSymbolIndices[d1]
	s2 := dataSymbolIndices[d2]

	var mag2 [8][8]float64
	for m1 := 0; m1 < 8; m1++ {
		a1 := grid.Amps[s1][m1]
		for m2 := 0; m2 < 8; m2++ {
			c := a1 + grid.Amps[s2][m2]
			re, im := real(c), imag(c)
			mag2[m1][m2] = re*re + im*im
		}
	}

	// First symbol's 3 bits: bit at position bitPos of inverseGrayMap[m1].
	// codeword bit cbi = 3·d1 + (2 - bitPos) so bitPos=2 → MSB → cbi = 3·d1.
	for bitPos := 2; bitPos >= 0; bitPos-- {
		max0 := -math.MaxFloat64
		max1 := -math.MaxFloat64
		for m1 := 0; m1 < 8; m1++ {
			bitval := (inverseGrayMap[m1] >> bitPos) & 1
			for m2 := 0; m2 < 8; m2++ {
				p := mag2[m1][m2]
				if bitval == 0 {
					if p > max0 {
						max0 = p
					}
				} else {
					if p > max1 {
						max1 = p
					}
				}
			}
		}
		cbi := 3*d1 + (2 - bitPos)
		llrs[cbi] = demapDiff(max0, max1, magnitudeMode)
	}

	// Second symbol's 3 bits.
	for bitPos := 2; bitPos >= 0; bitPos-- {
		max0 := -math.MaxFloat64
		max1 := -math.MaxFloat64
		for m2 := 0; m2 < 8; m2++ {
			bitval := (inverseGrayMap[m2] >> bitPos) & 1
			for m1 := 0; m1 < 8; m1++ {
				p := mag2[m1][m2]
				if bitval == 0 {
					if p > max0 {
						max0 = p
					}
				} else {
					if p > max1 {
						max1 = p
					}
				}
			}
		}
		cbi := 3*d2 + (2 - bitPos)
		llrs[cbi] = demapDiff(max0, max1, magnitudeMode)
	}
}

// SoftLLRsN3 computes a 174-bit LLR vector using N=3 block detection
// per QEX 2020 paper § 6 paragraph 6 (the paper explicitly enumerates
// N=1, N=2, N=3 as the block-detection family used by jt9). Per-triple
// complex N=1 correlations from `SymbolGrid.Amps` are coherently
// combined into 512 N=3 block correlations, magnitudes-squared are
// taken, and max-log demap produces 9 bit LLRs per triple.
//
// Phase-coherence assumption: each block spans 3 × 0.160 s = 0.48 s,
// strictly more demanding than N=2's 0.32 s. On clean HF this is
// usually satisfied; on fast-fading or polar paths it may not be —
// that's the empirical question the surrounding cascade experiment
// (Session 103+) answers.
//
// Triple layout: data symbols are split by the middle Costas anchor
// block (positions 36-42) into two contiguous halves of 29 symbols
// each (dataSymbolIndices[0..28] = positions 7..35;
// dataSymbolIndices[29..57] = positions 43..71). Triples are formed
// non-overlapping within each half: (0,1,2), (3,4,5), …, (24,25,26)
// in the first half and (29,30,31), …, (53,54,55) in the second.
// 9 triples × 3 symbols = 27 symbols per half; the 2 trailing symbols
// per half (d=27, d=28 and d=56, d=57) fall back to N=1 LLRs — a
// clean 3 bits per leftover, 12 of the 174 bits total.
//
// Triples are NOT formed across the middle Costas gap: bridging
// d=26..d=29 (or any spanning combination) would require ~1.6 s
// phase coherence, far beyond what N=3 can claim. The leftover
// symbols at the half-boundary (d=27, d=28, d=56, d=57) are processed
// independently via the N=1 fallback rather than rolled into a
// boundary-spanning block.
//
// Output convention matches SoftLLRs / SoftLLRsN2: positive LLR
// favours bit 0. Power scale is in |C|² units (same as N=2); BP's
// median-LLR normalisation absorbs the per-set scale difference.
//
// Per-triple complexity: 512 complex sums for the C array, 512
// magnitudes, 9 × 512 scans = ~5,120 ops per triple. Across 9 triples
// × 2 halves = 18 triples this is ~92 k ops total per call — well
// below the cost of a candidate's channelize + refine + symbol-FFT
// path, and only ~3× the N=2 cost.
//
// Provenance: the N=3 algorithm family is QEX § 6 explicit content;
// the implementation extends `SoftLLRsN2` straightforwardly; the
// 9-triples + 2-leftover layout is the engineering choice derived in
// research/sandbox/qex-derivation.md § 7.3 from first principles
// (the rejected 8 triples + 2 pairs + 1 leftover layout is recorded
// there).
func SoftLLRsN3(grid *SymbolGrid, magnitudeMode bool) [FT8CodewordBits]float64 {
	var llrs [FT8CodewordBits]float64
	tripleHalfLLRs(grid, &llrs, 0, 29, magnitudeMode)
	tripleHalfLLRs(grid, &llrs, 29, 58, magnitudeMode)
	return llrs
}

// tripleHalfLLRs fills the LLR slots for data-symbol indices in
// [start, end) using non-overlapping N=3 triples (with trailing
// leftover symbols falling back to N=1). Operates in place on llrs.
//
// For a half of 29 symbols this produces 9 triples covering 27
// symbols and 2 trailing N=1 fallback symbols. Mirrors the shape of
// pairHalfLLRs for N=2.
func tripleHalfLLRs(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, start, end int, magnitudeMode bool) {
	d := start
	for ; d+2 < end; d += 3 {
		fillTripleLLRsN3(grid, llrs, d, d+1, d+2, magnitudeMode)
	}
	for ; d < end; d++ {
		fillSymbolLLRsN1(grid, llrs, d, magnitudeMode)
	}
}

// fillTripleLLRsN3 computes the 512 N=3 block correlations
// C_{m1,m2,m3} = Amps[s1][m1] + Amps[s2][m2] + Amps[s3][m3], their
// magnitudes-squared, and the max-log bit LLRs for the 9 codeword
// bits owned by the triple (d1, d2, d3). Writes into codeword bit
// positions 3·d1..3·d1+2, 3·d2..3·d2+2, and 3·d3..3·d3+2.
//
// Loop structure: outer m1, middle m2, inner m3. Per-bit marginalization
// scans the full 512-entry mag2 array once per bit position per symbol;
// the m_owner switch (m1 for d1's bits, m2 for d2's, m3 for d3's) is
// the only structural difference between the three symbol blocks.
func fillTripleLLRsN3(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, d1, d2, d3 int, magnitudeMode bool) {
	s1 := dataSymbolIndices[d1]
	s2 := dataSymbolIndices[d2]
	s3 := dataSymbolIndices[d3]

	var mag2 [8][8][8]float64
	for m1 := 0; m1 < 8; m1++ {
		a1 := grid.Amps[s1][m1]
		for m2 := 0; m2 < 8; m2++ {
			a12 := a1 + grid.Amps[s2][m2]
			for m3 := 0; m3 < 8; m3++ {
				c := a12 + grid.Amps[s3][m3]
				re, im := real(c), imag(c)
				mag2[m1][m2][m3] = re*re + im*im
			}
		}
	}

	fillTripleBits(llrs, &mag2, d1, 0, magnitudeMode)
	fillTripleBits(llrs, &mag2, d2, 1, magnitudeMode)
	fillTripleBits(llrs, &mag2, d3, 2, magnitudeMode)
}

// fillTripleBits writes the 3 LLRs owned by the symbol at position
// `ownerPos` within a triple (0 = first/d1, 1 = middle/d2, 2 = last/d3)
// into the codeword-bit slots starting at 3·dataIdx. Marginalizes over
// the other two tones by scanning the full 512-entry mag2 array per
// (bitPos, owner) combination.
func fillTripleBits(
	llrs *[FT8CodewordBits]float64,
	mag2 *[8][8][8]float64,
	dataIdx, ownerPos int,
	magnitudeMode bool,
) {
	for bitPos := 2; bitPos >= 0; bitPos-- {
		max0 := -math.MaxFloat64
		max1 := -math.MaxFloat64
		for m1 := 0; m1 < 8; m1++ {
			for m2 := 0; m2 < 8; m2++ {
				for m3 := 0; m3 < 8; m3++ {
					var m int
					switch ownerPos {
					case 0:
						m = m1
					case 1:
						m = m2
					default:
						m = m3
					}
					p := mag2[m1][m2][m3]
					if (inverseGrayMap[m]>>bitPos)&1 == 0 {
						if p > max0 {
							max0 = p
						}
					} else {
						if p > max1 {
							max1 = p
						}
					}
				}
			}
		}
		cbi := 3*dataIdx + (2 - bitPos)
		llrs[cbi] = demapDiff(max0, max1, magnitudeMode)
	}
}

// SoftLLRsN1BitNormalized computes a 174-bit LLR vector using N=1
// block detection (single-symbol max-log demap, identical to SoftLLRs)
// with a per-symbol noise-variance normalization applied to the
// resulting LLRs.
//
// Motivation: belief propagation on the LDPC factor graph weights each
// incoming LLR by its magnitude. When the channel SNR varies across
// the 79 symbols of an FT8 frame (fading, frequency-selective
// interference, impulse noise), raw |Amps|² powers vary too, and
// high-power symbols' LLRs dominate BP iteration even when low-power
// symbols' bits are equally reliable for their own SNR. Bit-
// normalization divides each symbol's bit LLRs by an estimate of that
// symbol's noise variance σ̂²_s, putting per-bit confidence on a common
// scale across symbols.
//
// Per-symbol noise estimator: mean of the 6 lowest of 8 tone powers
// (drop the signal tone + the next-largest, which is the most likely
// adjacent-channel-leakage contaminant per the Session 84 finding).
// 6 samples is sufficient for a noise estimate; trimming top-2 is
// robust to one strong interferer that mimics signal-tone power.
//
// Formulation (Form B from derivation § 8.3): max-log demap on raw
// powers exactly as SoftLLRs does, THEN scale each symbol's 3 LLRs by
// 1/σ̂²_s as a single multiplicative step. Mathematically identical to
// scaling powers before max-log; chosen for code symmetry with
// SoftLLRs and easier testability.
//
// Sign convention matches SoftLLRs / SoftLLRsN2 / SoftLLRsN3: positive
// LLR favours bit 0. σ̂²_s > 0 preserves sign; on the degenerate all-
// zero-tones case (σ̂²_s = 0) the unscaled max-log differences are
// returned as-is.
//
// Provenance: bit-normalization is NOT QEX-prescribed; the principle
// is textbook info-theory (Richardson & Urbanke, Modern Coding Theory,
// MIT 2008, ch. 4–5). The specific estimator + scaling choice is
// derived from first principles + Session 84 adjacent-interference
// findings; see research/sandbox/qex-derivation.md § 8 for the full
// derivation including the rejected estimator alternatives.
//
// Per-symbol complexity: identical to SoftLLRs (24 power scans) + 8
// power-summing + 2 max-tracking + 3 multiplications. Negligible
// overhead vs N=1 baseline.
func SoftLLRsN1BitNormalized(grid *SymbolGrid, magnitudeMode bool) [FT8CodewordBits]float64 {
	var llrs [FT8CodewordBits]float64
	for d := 0; d < 58; d++ {
		sym := dataSymbolIndices[d]
		powers := grid.Tones[sym]

		// Max-log demap (identical to SoftLLRs).
		for bitPos := 2; bitPos >= 0; bitPos-- {
			max0 := -math.MaxFloat64
			max1 := -math.MaxFloat64
			for tone := 0; tone < 8; tone++ {
				if (inverseGrayMap[tone]>>bitPos)&1 == 0 {
					if powers[tone] > max0 {
						max0 = powers[tone]
					}
				} else {
					if powers[tone] > max1 {
						max1 = powers[tone]
					}
				}
			}
			cbi := 3*d + (2 - bitPos)
			llrs[cbi] = demapDiff(max0, max1, magnitudeMode)
		}

		// Per-symbol noise estimate + bit-LLR scaling.
		sigma2 := meanOfSixLowest(powers)
		if sigma2 > 0 {
			inv := 1.0 / sigma2
			llrs[3*d] *= inv
			llrs[3*d+1] *= inv
			llrs[3*d+2] *= inv
		}
	}
	return llrs
}

// meanOfSixLowest returns the mean of the 6 lowest values in an 8-power
// array — used as the per-symbol noise-variance estimator for
// SoftLLRsN1BitNormalized. Drops the top 2 (signal + likely-interferer
// contaminant) and averages the remaining 6.
//
// Implementation: single-pass scan tracking sum + top-2; returns
// (sum − top1 − top2) / 6. O(8) with no allocation. Stable under
// ties: when multiple tones share the maximum, top1/top2 collapse to
// the shared value, the formula still returns the average of the
// remaining 6 (correct for noise-estimate purposes).
//
// Edge cases:
//   - All zeros: returns 0; caller (SoftLLRsN1BitNormalized) checks
//     for this and skips scaling rather than dividing by zero.
//   - All equal to v: returns v (since top1+top2 = 2v, sum = 8v,
//     result = 6v/6 = v). Correct when there's no signal tone — all
//     tones are noise.
func meanOfSixLowest(powers [8]float64) float64 {
	var top1, top2, sum float64
	for _, p := range powers {
		sum += p
		if p >= top1 {
			top2 = top1
			top1 = p
		} else if p > top2 {
			top2 = p
		}
	}
	return (sum - top1 - top2) / 6.0
}

// SoftLLRsBestOfN computes a 174-bit LLR vector by per-bit selection
// across the three block-detection variants {N=1, N=2, N=3}: for each
// codeword bit i, picks the variant whose |LLR| is largest, defaulting
// to the lower-N variant on ties.
//
// Motivation: real HF channels have varying within-frame phase
// coherence. Different symbols are better-served by different N
// (N=1 robust when phase wanders; N=2 / N=3 stronger when phase holds
// over 0.32 / 0.48 s). The cascade as built (§§ 7-8) tries each
// metric whole; best-of-N is the per-bit version where the resulting
// LLR vector cannot be produced by any single source. The selection
// is well-justified under the (approximate) independence assumption
// of the three sources.
//
// Selection rule (§ 9.2):
//
//	source(i) = argmax_{k ∈ {N=1, N=2, N=3}} |LLR_k[i]|
//	LLR_best[i] = LLR_source(i)[i]
//
// Tiebreak prefers lower-N (N=1 > N=2 > N=3) — lower N has weaker
// phase-coherence demands and is the safer default when no metric
// dominates.
//
// Scale comparability (§ 9.3 open question): the three sources are
// NOT on the same magnitude scale because |sum of N tones|² grows
// roughly as N² for coherent signals. Raw max-|LLR| selection may
// therefore bias toward N=3. The companion SoftLLRsBestOfNWithSource
// returns the per-bit selection array so the cmd-tool corpus runs
// can detect collapse to "always N=3" — if observed, the planned fix
// is to normalize each source's |LLR| scale before selection.
//
// Sign convention: positive LLR favours bit 0 across all three input
// sources; selection preserves the chosen source's value as-is.
//
// Per-call cost: 3 metric generations (≈18× single-symbol N=1 cost)
// plus a 174-element O(1) selection loop. Called only at cascade
// pass 5 — when N=1, N=2, N=3, and N1Norm have all failed — so the
// invocation rate is low.
//
// Provenance: §9 of research/sandbox/qex-derivation.md. Not QEX-
// prescribed; first-principles derivation from log-likelihood
// max-selection algebra.
func SoftLLRsBestOfN(grid *SymbolGrid, magnitudeMode bool) [FT8CodewordBits]float64 {
	llrs, _ := SoftLLRsBestOfNWithSource(grid, magnitudeMode)
	return llrs
}

// SoftLLRsBestOfNWithSource computes the best-of-N LLR vector and
// returns it together with a per-bit source array recording which of
// {N=1, N=2, N=3} won each bit (encoded as 1, 2, 3 respectively).
// Used by the corpus harness to detect the scale-bias failure mode
// flagged in qex-derivation.md § 9.3.
//
// Implementation applies the § 9.3 (a) normalization fix that the
// initial raw-max-|LLR| variant required: each source's LLRs are
// scaled by the source's median |LLR| before selection, so the
// |LLR| magnitudes used for the max comparison are on a rank-
// equivalent scale across sources. The OUTPUT LLR uses the scaled
// value too (not the original) so the resulting vector is internally
// consistent for BP — the tanh(LLR/2) step inside BP is not scale-
// invariant, so mixing sources' native scales would saturate the
// smaller-magnitude source's bits to near-zero confidence even when
// they should dominate.
//
// Empirical justification for normalization: on a synthetic noisy
// grid (TestSoftLLRsBestOfN_SourceAttributionNotDegenerate), the
// pre-normalization variant collapsed to 93% N=3 — a degenerate
// selection driven by coherent-sum magnitude growth (|sum of N
// tones|² ∝ N² when in phase) rather than per-bit confidence.
// Normalizing by median |LLR| restores meaningful per-bit
// selection across sources.
//
// Degenerate case: when a source's median |LLR| is zero (e.g., the
// trivial all-zero grid), that source's LLRs pass through unscaled —
// avoids divide-by-zero while leaving the selection well-defined.
func SoftLLRsBestOfNWithSource(grid *SymbolGrid, magnitudeMode bool) (llrs [FT8CodewordBits]float64, source [FT8CodewordBits]uint8) {
	n1 := SoftLLRs(grid, magnitudeMode)
	n2 := SoftLLRsN2(grid, magnitudeMode)
	n3 := SoftLLRsN3(grid, magnitudeMode)

	// Scale each source by its median |LLR| so the selection
	// magnitudes are rank-equivalent across N=1, N=2, N=3.
	s1 := scaleByMedianAbs(n1)
	s2 := scaleByMedianAbs(n2)
	s3 := scaleByMedianAbs(n3)

	for i := 0; i < FT8CodewordBits; i++ {
		a1 := math.Abs(s1[i])
		a2 := math.Abs(s2[i])
		a3 := math.Abs(s3[i])
		// Tiebreak preference: N=1 first, then N=2, then N=3.
		switch {
		case a1 >= a2 && a1 >= a3:
			llrs[i] = s1[i]
			source[i] = 1
		case a2 >= a3:
			llrs[i] = s2[i]
			source[i] = 2
		default:
			llrs[i] = s3[i]
			source[i] = 3
		}
	}
	return llrs, source
}

// scaleByMedianAbs returns a copy of llrs scaled so the median |LLR|
// is 1.0. Preserves signs. When all LLRs are zero (median == 0), the
// function returns the input unchanged. Used by SoftLLRsBestOfN to
// put the three input sources on a rank-equivalent magnitude scale
// before per-bit max-|LLR| selection.
func scaleByMedianAbs(llrs [FT8CodewordBits]float64) [FT8CodewordBits]float64 {
	absVals := make([]float64, FT8CodewordBits)
	for i, l := range llrs {
		absVals[i] = math.Abs(l)
	}
	sort.Float64s(absVals)
	median := absVals[FT8CodewordBits/2]
	if median <= 0 {
		return llrs
	}
	inv := 1.0 / median
	var out [FT8CodewordBits]float64
	for i, l := range llrs {
		out[i] = l * inv
	}
	return out
}

// fillSymbolLLRsN1 writes the 3 N=1 LLRs for data-symbol index d into
// llrs. Used for the trailing leftover symbol in each half-frame when
// the half-symbol count is odd. Replicates SoftLLRs's per-symbol logic
// in place, scoped to one symbol.
func fillSymbolLLRsN1(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, d int, magnitudeMode bool) {
	sym := dataSymbolIndices[d]
	powers := grid.Tones[sym]
	for bitPos := 2; bitPos >= 0; bitPos-- {
		max0 := -math.MaxFloat64
		max1 := -math.MaxFloat64
		for tone := 0; tone < 8; tone++ {
			if (inverseGrayMap[tone]>>bitPos)&1 == 0 {
				if powers[tone] > max0 {
					max0 = powers[tone]
				}
			} else {
				if powers[tone] > max1 {
					max1 = powers[tone]
				}
			}
		}
		cbi := 3*d + (2 - bitPos)
		llrs[cbi] = demapDiff(max0, max1, magnitudeMode)
	}
}

// ── A priori (AP) decoding — QEX § 7 specialisation ─────────────────
//
// The QEX paper § 7 ("A Priori Information") prescribes injecting
// known-bit priors into the LLR vector before BP. The paper defines
// three families (AP1, AP2, AP3) that vary by how much of the 77-bit
// payload is hypothesised as known. AP-CQ is the specialisation of
// AP2 where the c28_1 callsign-1 slot is hypothesised to carry the
// special "CQ" token — the format every fixture with truth text
// starting with "CQ " conforms to.
//
// Knowable bits under the AP-CQ hypothesis (34 of 174 codeword bits,
// all in the systematic-payload prefix; bit positions traced to the
// PackType1 layout documented in pack.go and QEX § 4):
//
//	bits  0..27  c28_1 = 2  (PackCallsign28("CQ") = 2; MSB-first in 28 bits)
//	bit  28      p1    = 0  (rover-suffix flag for call 1; CQ is not a rover)
//	bit  57      p2    = 0  (rover-suffix flag for call 2)
//	bit  58      r1    = 0  (roger flag; CQ messages do not carry a roger)
//	bits 74..76  i3    = 1  (Type 1; MSB-first in 3 bits)
//
// Bits 29..56 (c28_2) and 59..73 (g15) carry the unknown callsign-2 and
// grid; CRC14 (77..90) and parity (91..173) depend on those unknown
// bits and so are NOT pinned. The 43 unknown payload bits + 14 CRC +
// 83 parity remain entirely channel-driven; BP recovers them given
// the 33-bit AP anchor.
//
// Provenance: bit positions cross-checked against PackType1 (pack.go,
// derived from QEX § 4); CQ token value 2 read from PackCallsign28
// (pack.go, also QEX § 4). WSJT-X source NOT consulted, per
// feedback_ft8_spec_implementation_oracle_policy.

// apCQMagnitude is the pinning magnitude added to channel LLRs at the
// 33 AP-CQ-known positions. Chosen so it dominates typical channel
// LLR magnitudes (|chann|≤5 on the working fixtures) while staying
// inside BP's tanh/atanh numerical sweet spot. ±10 is well below the
// regions where BP's message passing saturates; ±30+ starts producing
// NaNs in tight-coupling runs.
const apCQMagnitude = 10.0

// SoftLLRsAPCQ builds an LLR vector for the AP-CQ hypothesis: the
// candidate decodes to a Type-1 "CQ <call> <grid>" message. The
// vector starts from the N=1 channel LLRs and ADDs a ±apCQMagnitude
// prior at each of the 34 codeword positions pinned by the
// hypothesis. Adding (rather than replacing) keeps the channel
// information active: strong channel disagreement at a known
// position can still flip the bit if the channel LLR overpowers the
// prior, so a wrongly-hypothesised AP-CQ on a non-CQ signal degrades
// gracefully to a CRC failure rather than locking BP into a wrong
// codeword.
//
// Output: same shape as SoftLLRs (positive favours bit 0).
//
// Consumed by the multipass cascade as the last (opt-in) pass; gated
// behind MultiPassOptions.EnableAPCQ to avoid CRC-lottery extras
// when the AP hypothesis is wrong.
func SoftLLRsAPCQ(grid *SymbolGrid, magnitudeMode bool) [FT8CodewordBits]float64 {
	return softLLRsAPCQWithMag(grid, 0, apCQValueBare, magnitudeMode)
}

// softLLRsAPCQWithMag is the magnitude-tunable form. mag<=0 falls back
// to apCQMagnitude; c28Value selects the c28_1 hypothesis (apCQValueBare,
// apCQValueDX, apCQValuePOTA, apCQValueCOTA, or any caller-supplied
// value in the [0, ntokens) special-token range). Used by the cascade
// so the operator can sweep both pinning strength and CQ-form
// hypothesis via MultiPassOptions.APCQMag without re-deriving the bit
// layout.
func softLLRsAPCQWithMag(grid *SymbolGrid, mag float64, c28Value uint32, magnitudeMode bool) [FT8CodewordBits]float64 {
	if mag <= 0 {
		mag = apCQMagnitude
	}
	llrs := SoftLLRs(grid, magnitudeMode)
	applyAPCQPriorsForC28(&llrs, mag, c28Value)
	return llrs
}

// applyAPCQPriorsForC28 mutates llrs at the 34 codeword positions
// pinned by an AP-CQ-family hypothesis. mag is the magnitude
// added/subtracted from the channel LLR at each known position
// (positive for bit 0, negative for bit 1, per the LDPC sign
// convention used throughout the sandbox). c28Value selects which
// specific c28_1 hypothesis to pin:
//
//	apCQValueBare    = 2      → "CQ" alone (bare CQ)
//	apCQValueDX      = 69279  → "CQ DX  " (DXAA padding)
//	apCQValuePOTA    = 274601 → "CQ POTA"
//	apCQValueCOTA    = 46113  → "CQ COTA"
//	any [0, ntokens) → caller-supplied c28_1 hypothesis
//
// The remaining 5 known bits (p1, p2, r1, i3) stay pinned to the
// Type-1-CQ shape regardless of c28Value — every supported variant
// is still a Type-1 message with no rover/roger flags.
//
// Exposed (lowercase, package-internal) so tests can drive
// magnitude-zero / magnitude-large variants without re-deriving the
// known-bit layout.
func applyAPCQPriorsForC28(llrs *[FT8CodewordBits]float64, mag float64, c28Value uint32) {
	// Bits 0..27 = c28_1 = c28Value, MSB-first in 28 bits.
	const cqBits = 28
	for i := 0; i < cqBits; i++ {
		bit := uint8((c28Value >> (cqBits - 1 - i)) & 1)
		llrs[i] += signedPrior(bit, mag)
	}
	// bit 28 = p1 = 0 (CQ is not a rover; the "/" suffix never
	// applies to the bare token nor to "CQ AAAA" forms).
	llrs[28] += signedPrior(0, mag)
	// bit 57 = p2 = 0, bit 58 = r1 = 0 (Type-1 CQ has no rover-suffix
	// for the called callsign and no roger flag).
	llrs[57] += signedPrior(0, mag)
	llrs[58] += signedPrior(0, mag)
	// bits 74..76 = i3 = 1, MSB-first in 3 bits → 0, 0, 1.
	const i3Value uint32 = 1
	const i3Bits = 3
	for i := 0; i < i3Bits; i++ {
		bit := uint8((i3Value >> (i3Bits - 1 - i)) & 1)
		llrs[74+i] += signedPrior(bit, mag)
	}
}

// applyAPCQPriors retains the original entry point (c28_1=apCQValueBare)
// for tests that asserted the historical AP-CQ shape pre-Session-104
// CQ_nnnn extension.
func applyAPCQPriors(llrs *[FT8CodewordBits]float64, mag float64) {
	applyAPCQPriorsForC28(llrs, mag, apCQValueBare)
}

// c28_1 hypothesis values for AP-CQ-family cascade. Derived from the
// FT8 token-range layout in unpack.go: bare CQ is value 2; "CQ AAAA"
// forms occupy [1003, 1003+26⁴) under the formula
// 1003 + 26³·c[0] + 26²·c[1] + 26·c[2] + c[3], where c[k] = ord(ch)-'A'
// and the modifier is right-padded with 'A' (=0) for shorter
// modifiers like "DX". Values cross-checked by running them through
// Unpack77's decodeCQAbcd in a probe (one-off shell run, not in
// tree).
const (
	apCQValueBare uint32 = 2
	apCQValueDX   uint32 = 69279  // "CQ DX  " padded to "DXAA"
	apCQValueCOTA uint32 = 46113  // "CQ COTA"
	apCQValuePOTA uint32 = 274601 // "CQ POTA"
)

// apCQValueOrder is the cascade try-order for the AP-CQ-family
// hypotheses. Bare CQ is most common (~60% of CQ-format truths in
// the working corpus); DX next; POTA / COTA tail. The cascade tries
// each in turn on a given candidate; first BP-OK wins.
var apCQValueOrder = [...]uint32{
	apCQValueBare, apCQValueDX, apCQValueCOTA, apCQValuePOTA,
}

// ── AP3 — QEX § 7 AP3 (both callsigns hypothesised) ─────────────────
//
// AP3 is the strongest of the QEX § 7 AP forms. Where AP2 / AP-CQ
// hypothesises only c28_1 (34 pinned bits), AP3 additionally
// hypothesises c28_2 — the called callsign. The hypothesis comes
// from the running CallsignHashTable: any callsign the decoder has
// recently seen is a plausible call2 for a failed candidate. Total
// pinned bits: 28 (c28_1) + 28 (c28_2) + 6 (p1, p2, r1, i3=3) = 62
// out of 174 — roughly double AP-CQ's leverage. Only 15 g15 + 14
// CRC + 83 parity bits remain channel-driven.
//
// AP3 enumerates hypothesis pairs (c1, c2) and tries each via BP+OSD
// with the 61-bit pin mask. First success wins. Total cost per
// failed candidate is O(K²) BP runs where K is the hash-table-snapshot
// cap; bounded via MultiPassOptions.AP3MaxCallsigns.

// applyAP3PriorsForC28s pins both c28_1 and c28_2 plus the AP-CQ
// auxiliary bits (p1, p2, r1, i3) to a Type-1 message hypothesis.
// Magnitude is added to channel LLRs at the 61 known positions; sign
// reflects the hypothesised bit value (positive favours bit 0).
func applyAP3PriorsForC28s(llrs *[FT8CodewordBits]float64, mag float64, c28_1, c28_2 uint32) {
	// Bits 0..27 = c28_1 (28 bits MSB-first)
	const c28Bits = 28
	for i := 0; i < c28Bits; i++ {
		bit := uint8((c28_1 >> (c28Bits - 1 - i)) & 1)
		llrs[i] += signedPrior(bit, mag)
	}
	// bit 28 = p1 = 0
	llrs[28] += signedPrior(0, mag)
	// bits 29..56 = c28_2 (28 bits MSB-first)
	for i := 0; i < c28Bits; i++ {
		bit := uint8((c28_2 >> (c28Bits - 1 - i)) & 1)
		llrs[29+i] += signedPrior(bit, mag)
	}
	// bit 57 = p2 = 0, bit 58 = r1 = 0
	llrs[57] += signedPrior(0, mag)
	llrs[58] += signedPrior(0, mag)
	// bits 74..76 = i3 = 1 (Type 1)
	const i3Value uint32 = 1
	const i3Bits = 3
	for i := 0; i < i3Bits; i++ {
		bit := uint8((i3Value >> (i3Bits - 1 - i)) & 1)
		llrs[74+i] += signedPrior(bit, mag)
	}
}

// softLLRsAP3WithMag generates the AP3-augmented LLR vector for a
// given (c28_1, c28_2) hypothesis pair. mag<=0 falls back to
// apCQMagnitude (same default as AP-CQ — the pin strength is the
// same per-bit; AP3 just pins more bits).
func softLLRsAP3WithMag(grid *SymbolGrid, mag float64, c28_1, c28_2 uint32, magnitudeMode bool) [FT8CodewordBits]float64 {
	if mag <= 0 {
		mag = apCQMagnitude
	}
	llrs := SoftLLRs(grid, magnitudeMode)
	applyAP3PriorsForC28s(&llrs, mag, c28_1, c28_2)
	return llrs
}

// ap3PinMask returns the 62-position pin mask: c28_1 (0..27) +
// p1 (28) + c28_2 (29..56) + p2 (57) + r1 (58) + i3 (74..76).
//
// Passed to BPDecodeWithPin so OSD's MRB bit-flip search will not
// undo AP3 priors — same mechanism as apCQPinMask, just covering
// more bits.
func ap3PinMask() [FT8CodewordBits]bool {
	var m [FT8CodewordBits]bool
	for i := 0; i < 28; i++ { // c28_1
		m[i] = true
	}
	m[28] = true               // p1
	for i := 29; i < 57; i++ { // c28_2
		m[i] = true
	}
	m[57] = true // p2
	m[58] = true // r1
	m[74] = true // i3 MSB
	m[75] = true // i3 middle
	m[76] = true // i3 LSB
	return m
}

// apCQPinMask returns the [LDPCCodewordBits]bool mask of codeword
// positions pinned by any AP-CQ-family hypothesis. The same 33
// positions are pinned regardless of the specific c28_1 value
// (bare CQ vs CQ DX vs CQ POTA etc.) — only the *values* at those
// positions differ across hypotheses, not the positions themselves.
//
// Passed to BPDecodeWithPin so OSD's MRB bit-flip search will not
// undo the AP priors during its CRC-search. Without the pin,
// OSD-2's 2-bit flip can land on the AP-pinned positions and
// produce non-CQ-shape codewords that pass CRC but disagree with
// the hypothesis (the Session 104 failure mode).
func apCQPinMask() [FT8CodewordBits]bool {
	var m [FT8CodewordBits]bool
	for i := 0; i < 28; i++ { // c28_1 (28 bits)
		m[i] = true
	}
	m[28] = true // p1 = 0
	m[57] = true // p2 = 0
	m[58] = true // r1 = 0
	m[74] = true // i3 MSB
	m[75] = true // i3 middle
	m[76] = true // i3 LSB
	return m
}

// signedPrior returns +mag when bit==0, −mag when bit==1, following
// the LDPC sign convention (positive LLR favours bit 0).
func signedPrior(bit uint8, mag float64) float64 {
	if bit == 0 {
		return +mag
	}
	return -mag
}
