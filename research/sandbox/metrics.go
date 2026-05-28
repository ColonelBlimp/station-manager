package sandbox

import "math"

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
// No noise normalisation is applied here — LLRs are in the same units
// as SymbolGrid.Tones (power). Downstream consumers that need an
// absolute scale (e.g. LDPC belief propagation) should normalise by
// an estimate of the per-symbol noise variance. The raw LLRs are
// sufficient for hard-decision conversion and BER measurement.
//
// Output ordering: codeword bit cbi corresponds to data symbol
// dataSymbolIndices[cbi/3], with cbi%3 == 0 being the MSB of that
// symbol's bit triplet (per FT8 spec).
func SoftLLRs(grid *SymbolGrid) [FT8CodewordBits]float64 {
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
			llrs[cbi] = max0 - max1
		}
	}
	return llrs
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
func SoftLLRsN2(grid *SymbolGrid) [FT8CodewordBits]float64 {
	var llrs [FT8CodewordBits]float64
	pairHalfLLRs(grid, &llrs, 0, 29)
	pairHalfLLRs(grid, &llrs, 29, 58)
	return llrs
}

// pairHalfLLRs fills the LLR slots for data-symbol indices in [start, end)
// using non-overlapping N=2 pairs (with the trailing odd symbol, if any,
// falling back to N=1). Operates in place on llrs.
func pairHalfLLRs(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, start, end int) {
	d := start
	for ; d+1 < end; d += 2 {
		fillPairLLRsN2(grid, llrs, d, d+1)
	}
	if d < end {
		fillSymbolLLRsN1(grid, llrs, d)
	}
}

// fillPairLLRsN2 computes the 64 N=2 block correlations C_{m1,m2} =
// Amps[s1][m1] + Amps[s2][m2], their magnitudes-squared, and the
// max-log bit LLRs for the 6 codeword bits owned by the pair
// (d1, d2). Writes into codeword bit positions 3·d1..3·d1+2 and
// 3·d2..3·d2+2.
func fillPairLLRsN2(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, d1, d2 int) {
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
		llrs[cbi] = max0 - max1
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
		llrs[cbi] = max0 - max1
	}
}

// fillSymbolLLRsN1 writes the 3 N=1 LLRs for data-symbol index d into
// llrs. Used for the trailing leftover symbol in each half-frame when
// the half-symbol count is odd. Replicates SoftLLRs's per-symbol logic
// in place, scoped to one symbol.
func fillSymbolLLRsN1(grid *SymbolGrid, llrs *[FT8CodewordBits]float64, d int) {
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
		llrs[cbi] = max0 - max1
	}
}
