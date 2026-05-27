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
