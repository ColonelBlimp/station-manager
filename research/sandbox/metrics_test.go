package sandbox

import (
	"testing"
)

// TestSoftLLRsN2_NoiselessRoundTrip verifies that a SymbolGrid built
// from a known codeword (each data symbol's true tone set to a large
// complex amplitude, others zero) produces N=2 LLRs whose hard decisions
// reconstruct the codeword exactly. Exercises every codeword bit position
// (174 bits) across 28 N=2 pairs + 2 N=1 leftovers.
func TestSoftLLRsN2_NoiselessRoundTrip(t *testing.T) {
	// Build a deterministic 58-tone sequence: tone d = d % 8.
	// This exercises all 8 tone values across both halves and gives
	// every codeword bit a chance to be flipped on at least once.
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = d % 8
	}

	grid := buildNoiselessGrid(trueTones)
	llrs := SoftLLRsN2(grid)

	// Reconstruct expected codeword from trueTones using the same Gray
	// map and bit-ordering convention SoftLLRs(N1) uses.
	var expected [FT8CodewordBits]uint8
	for d := 0; d < 58; d++ {
		bits := inverseGrayMap[trueTones[d]]
		expected[3*d] = uint8((bits >> 2) & 1)   // MSB
		expected[3*d+1] = uint8((bits >> 1) & 1) // middle
		expected[3*d+2] = uint8(bits & 1)        // LSB
	}

	got := HardBits(llrs)
	for i := 0; i < FT8CodewordBits; i++ {
		if got[i] != expected[i] {
			d := i / 3
			t.Errorf("bit %d (data-sym d=%d, true tone=%d): got %d, want %d (llr=%g)",
				i, d, trueTones[d], got[i], expected[i], llrs[i])
		}
	}
}

// TestSoftLLRsN2_SignConvention verifies the positive-LLR-favours-bit-0
// convention is preserved across the N=2 path. A symbol carrying tone 0
// (inverseGrayMap[0] = 0b000) should produce three positive LLRs;
// tone 7 (inverseGrayMap[7] = 0b111) should produce three negative LLRs.
func TestSoftLLRsN2_SignConvention(t *testing.T) {
	// All data symbols carry tone 0 → all 174 bits should be 0 →
	// all 174 LLRs should be > 0.
	var tones0 [58]int
	g0 := buildNoiselessGrid(tones0)
	l0 := SoftLLRsN2(g0)
	for i, l := range l0 {
		if l <= 0 {
			t.Errorf("tone-0 grid: llr[%d] = %g, expected > 0", i, l)
		}
	}

	// All data symbols carry tone 7 → all 174 bits should be 1 →
	// all 174 LLRs should be < 0.
	var tones7 [58]int
	for d := 0; d < 58; d++ {
		tones7[d] = 7
	}
	g7 := buildNoiselessGrid(tones7)
	l7 := SoftLLRsN2(g7)
	for i, l := range l7 {
		if l >= 0 {
			t.Errorf("tone-7 grid: llr[%d] = %g, expected < 0", i, l)
		}
	}
}

// TestSoftLLRsN2_LeftoverFallback verifies the trailing-odd-symbol
// fallback path: data symbols d=28 (last of half 1) and d=57 (last of
// half 2) should be demodulated via the N=1 SoftLLRs path. The bits at
// those positions should match what SoftLLRs produces on the same grid.
func TestSoftLLRsN2_LeftoverFallback(t *testing.T) {
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = (d * 3) % 8 // some variation
	}
	grid := buildNoiselessGrid(trueTones)

	n1 := SoftLLRs(grid)
	n2 := SoftLLRsN2(grid)

	// Bits 84..86 belong to d=28 (leftover of half 1).
	// Bits 171..173 belong to d=57 (leftover of half 2).
	for _, d := range []int{28, 57} {
		for off := 0; off < 3; off++ {
			cbi := 3*d + off
			if n1[cbi] != n2[cbi] {
				t.Errorf("leftover d=%d bit cbi=%d: N=1 llr=%g, N=2 llr=%g (expected equal)",
					d, cbi, n1[cbi], n2[cbi])
			}
		}
	}
}

// buildNoiselessGrid constructs a SymbolGrid where each data symbol's
// true tone has amplitude 1+0j and all other tones are zero. Costas
// symbols are also populated with their expected tone (anchor[k]) at
// amplitude 1; this isn't read by SoftLLRsN2 but matches what an
// idealised ExtractSymbols would produce on a clean fixture.
func buildNoiselessGrid(trueTones [58]int) *SymbolGrid {
	g := &SymbolGrid{}
	for d, tone := range trueTones {
		s := dataSymbolIndices[d]
		g.Amps[s][tone] = complex(1, 0)
		g.Tones[s][tone] = 1.0
	}
	// Costas symbols: anchor[k] tone in each of the 3 blocks (0..6,
	// 36..42, 72..78). Populated for completeness; not consulted by
	// SoftLLRs / SoftLLRsN2.
	for _, blockStart := range costasBlockStarts {
		for k := 0; k < 7; k++ {
			s := blockStart + k
			tone := costasArray[k]
			g.Amps[s][tone] = complex(1, 0)
			g.Tones[s][tone] = 1.0
		}
	}
	return g
}
