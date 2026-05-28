package sandbox

import (
	"math"
	"math/rand"
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

// TestSoftLLRsN3_NoiselessRoundTrip pins the N=3 path's basic
// correctness: a clean SymbolGrid round-trips through SoftLLRsN3 to
// hard decisions that reconstruct the encoded codeword exactly.
// Exercises all 174 bits across 9 triples × 2 halves + 4 N=1 leftover
// symbols (d=27, d=28, d=56, d=57).
func TestSoftLLRsN3_NoiselessRoundTrip(t *testing.T) {
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = d % 8
	}
	grid := buildNoiselessGrid(trueTones)
	llrs := SoftLLRsN3(grid)

	var expected [FT8CodewordBits]uint8
	for d := 0; d < 58; d++ {
		bits := inverseGrayMap[trueTones[d]]
		expected[3*d] = uint8((bits >> 2) & 1)
		expected[3*d+1] = uint8((bits >> 1) & 1)
		expected[3*d+2] = uint8(bits & 1)
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

// TestSoftLLRsN3_SignConvention pins positive-LLR-favours-bit-0 across
// the N=3 path: an all-tone-0 grid yields all-positive LLRs (all bits
// are 0); an all-tone-7 grid yields all-negative LLRs (all bits are 1).
func TestSoftLLRsN3_SignConvention(t *testing.T) {
	var tones0 [58]int
	g0 := buildNoiselessGrid(tones0)
	l0 := SoftLLRsN3(g0)
	for i, l := range l0 {
		if l <= 0 {
			t.Errorf("tone-0 grid: llr[%d] = %g, expected > 0", i, l)
		}
	}

	var tones7 [58]int
	for d := 0; d < 58; d++ {
		tones7[d] = 7
	}
	g7 := buildNoiselessGrid(tones7)
	l7 := SoftLLRsN3(g7)
	for i, l := range l7 {
		if l >= 0 {
			t.Errorf("tone-7 grid: llr[%d] = %g, expected < 0", i, l)
		}
	}
}

// TestSoftLLRsN3_LeftoverFallback verifies the 4 trailing-symbol N=1
// fallback bits (d=27, d=28, d=56, d=57 — the last two symbols of each
// half-frame, since 29 = 9·3 + 2). Bits owned by these symbols must
// equal the N=1 SoftLLRs values byte-identical: the same code path
// fills them in both functions.
func TestSoftLLRsN3_LeftoverFallback(t *testing.T) {
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = (d * 5) % 8 // some variation, prime stride
	}
	grid := buildNoiselessGrid(trueTones)

	n1 := SoftLLRs(grid)
	n3 := SoftLLRsN3(grid)

	for _, d := range []int{27, 28, 56, 57} {
		for off := 0; off < 3; off++ {
			cbi := 3*d + off
			if n1[cbi] != n3[cbi] {
				t.Errorf("leftover d=%d bit cbi=%d: N=1 llr=%g, N=3 llr=%g (expected equal)",
					d, cbi, n1[cbi], n3[cbi])
			}
		}
	}
}

// TestSoftLLRsN1BitNormalized_NoiselessRoundTrip pins the basic
// correctness of the bit-normalized path: a clean grid round-trips
// through SoftLLRsN1BitNormalized to hard decisions that reconstruct
// the encoded codeword. The per-symbol noise estimator is zero on a
// noiseless grid (only the signal tone is non-zero, the 6 lowest are
// all 0), so scaling is skipped — hard-decision sign comes from the
// raw max-log LLR, which we already know works (TestSoftLLRsN3 family).
func TestSoftLLRsN1BitNormalized_NoiselessRoundTrip(t *testing.T) {
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = d % 8
	}
	grid := buildNoiselessGrid(trueTones)
	llrs := SoftLLRsN1BitNormalized(grid)

	var expected [FT8CodewordBits]uint8
	for d := 0; d < 58; d++ {
		bits := inverseGrayMap[trueTones[d]]
		expected[3*d] = uint8((bits >> 2) & 1)
		expected[3*d+1] = uint8((bits >> 1) & 1)
		expected[3*d+2] = uint8(bits & 1)
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

// TestSoftLLRsN1BitNormalized_SignConvention pins positive-LLR-favours-
// bit-0 across the bit-normalized path: all-tone-0 grid → all-positive
// LLRs; all-tone-7 grid → all-negative LLRs. Scaling by 1/σ̂² preserves
// sign when σ̂² > 0; on the noiseless grid σ̂² = 0 so scaling is skipped
// and the raw max-log signs apply.
func TestSoftLLRsN1BitNormalized_SignConvention(t *testing.T) {
	var tones0 [58]int
	g0 := buildNoiselessGrid(tones0)
	l0 := SoftLLRsN1BitNormalized(g0)
	for i, l := range l0 {
		if l <= 0 {
			t.Errorf("tone-0 grid: llr[%d] = %g, expected > 0", i, l)
		}
	}

	var tones7 [58]int
	for d := 0; d < 58; d++ {
		tones7[d] = 7
	}
	g7 := buildNoiselessGrid(tones7)
	l7 := SoftLLRsN1BitNormalized(g7)
	for i, l := range l7 {
		if l >= 0 {
			t.Errorf("tone-7 grid: llr[%d] = %g, expected < 0", i, l)
		}
	}
}

// TestSoftLLRsN1BitNormalized_ScalesByInverseSigma2 pins the Form-B
// scaling content of the derivation (§ 8.3): for a grid with constant
// non-winner power K per symbol, the bit-normalized LLR must equal
// the raw N=1 LLR divided by K.
//
// Test construction: every data symbol gets signal tone at power
// 100, all 7 non-winner tones at power 4. mean-of-6-lowest σ̂² = 4
// (the 6 lowest non-winners average to 4; the 2 highest are signal
// + next-largest, both excluded). Expected: bit-normalized LLR =
// raw N=1 LLR / 4 for every bit.
func TestSoftLLRsN1BitNormalized_ScalesByInverseSigma2(t *testing.T) {
	const (
		signalPower    = 100.0
		nonWinnerPower = 4.0
		expectedSigma2 = nonWinnerPower
	)
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = d % 8
	}
	// Build a grid with explicit non-winner power on every tone.
	g := &SymbolGrid{}
	for d, tone := range trueTones {
		s := dataSymbolIndices[d]
		for m := 0; m < 8; m++ {
			if m == tone {
				g.Tones[s][m] = signalPower
				g.Amps[s][m] = complex(10, 0) // |10|² = 100
			} else {
				g.Tones[s][m] = nonWinnerPower
				g.Amps[s][m] = complex(2, 0) // |2|² = 4
			}
		}
	}

	rawN1 := SoftLLRs(g)
	normalized := SoftLLRsN1BitNormalized(g)

	const tol = 1e-9
	for i := 0; i < FT8CodewordBits; i++ {
		want := rawN1[i] / expectedSigma2
		if math.Abs(normalized[i]-want) > tol {
			t.Errorf("bit %d: normalized=%g, want %g (raw=%g, σ²=%g)",
				i, normalized[i], want, rawN1[i], expectedSigma2)
		}
	}
}

// TestSoftLLRsN1BitNormalized_RobustToStrongInterferer pins the
// estimator-choice rationale from derivation § 8.4: one off-tone with
// signal-strength power (simulating an adjacent-channel leakage tone)
// must NOT inflate σ̂²_s to the point of suppressing the true signal's
// LLR sign.
//
// Test construction: every data symbol has signal at tone 0 (power
// 100), tone 5 boosted to 50 (interferer-leakage simulation), other
// 6 tones at power 1 (noise floor). mean-of-6-lowest σ̂² should be
// (1×6)/6 = 1 (trimming top-2 drops tone-0 + tone-5). "mean-of-7-
// non-winner" would give (50+1×6)/7 ≈ 8 — much higher, would over-
// suppress LLR magnitudes. The test asserts our estimator
// correctly trims both contaminants.
//
// Sign correctness: all 174 LLRs must remain consistent with the
// true tone (tone 0 → bits 000 → all positive LLRs).
//
// Magnitude check: the bit-normalized LLR magnitudes for this grid
// must exceed what the "mean-of-7-non-winner" alternative would
// produce, by approximately the ratio of the two σ̂² estimates
// (≈ 8 / 1 = 8). The test uses a conservative ≥ 4× lower bound to
// avoid being brittle to exact ratio.
func TestSoftLLRsN1BitNormalized_RobustToStrongInterferer(t *testing.T) {
	const (
		signalPower      = 100.0
		interfererPower  = 50.0
		noisePower       = 1.0
		expectedSigma2   = noisePower // mean of 6 lowest = mean of 6×1 = 1
		altSigma2NonWin  = (interfererPower + 6*noisePower) / 7.0
		minMagRatioBound = 4.0
	)
	g := &SymbolGrid{}
	for d := 0; d < 58; d++ {
		s := dataSymbolIndices[d]
		for m := 0; m < 8; m++ {
			switch m {
			case 0:
				g.Tones[s][m] = signalPower
				g.Amps[s][m] = complex(10, 0)
			case 5:
				g.Tones[s][m] = interfererPower
				g.Amps[s][m] = complex(7.07, 0) // |7.07|² ≈ 50
			default:
				g.Tones[s][m] = noisePower
				g.Amps[s][m] = complex(1, 0)
			}
		}
	}

	llrs := SoftLLRsN1BitNormalized(g)
	rawN1 := SoftLLRs(g)

	// Sign: tone 0 → bits 000 → all LLRs > 0.
	for i, l := range llrs {
		if l <= 0 {
			t.Errorf("bit %d: LLR=%g, expected > 0 (tone-0 grid with interferer at tone 5)", i, l)
		}
	}

	// Estimator-choice check: the chosen σ̂² ≈ 1, the rejected
	// "mean-of-7-non-winner" σ̂² ≈ 8. Bit-normalized LLR magnitudes
	// (which equal raw / σ̂²) should be ≈ 8× larger under our
	// estimator than they would be under the rejected alternative.
	// Lower bound 4× catches the estimator falling through to the
	// rejected behaviour (e.g. if a refactor accidentally averaged 7
	// non-winners instead of 6 lowest).
	for i := 0; i < FT8CodewordBits; i++ {
		// Raw N=1 LLR divided by the rejected σ̂² gives what the
		// alt estimator would produce.
		altLLRMag := math.Abs(rawN1[i] / altSigma2NonWin)
		gotMag := math.Abs(llrs[i])
		if altLLRMag > 0 && gotMag/altLLRMag < minMagRatioBound {
			t.Errorf("bit %d: |LLR|/|altLLR| = %g, want ≥ %g (chosen σ̂² ≈ %g; rejected ≈ %g)",
				i, gotMag/altLLRMag, minMagRatioBound, expectedSigma2, altSigma2NonWin)
		}
	}
}

// TestMeanOfSixLowest pins the noise-estimator helper.
func TestMeanOfSixLowest(t *testing.T) {
	cases := []struct {
		name   string
		powers [8]float64
		want   float64
	}{
		{"all zeros", [8]float64{}, 0},
		{"all equal", [8]float64{2, 2, 2, 2, 2, 2, 2, 2}, 2},
		{"ascending 1..8", [8]float64{1, 2, 3, 4, 5, 6, 7, 8}, (1 + 2 + 3 + 4 + 5 + 6) / 6.0},
		{"descending 8..1", [8]float64{8, 7, 6, 5, 4, 3, 2, 1}, (1 + 2 + 3 + 4 + 5 + 6) / 6.0},
		// Signal at index 0 (100), interferer at index 5 (50), noise (1) elsewhere.
		{"signal + interferer + noise", [8]float64{100, 1, 1, 1, 1, 50, 1, 1}, 1},
		// Tie at top: two tones at maximum value.
		{"tie at maximum", [8]float64{10, 10, 1, 1, 1, 1, 1, 1}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := meanOfSixLowest(tc.powers)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("meanOfSixLowest(%v) = %g, want %g", tc.powers, got, tc.want)
			}
		})
	}
}

// TestSoftLLRsBestOfN_NoiselessRoundTrip pins basic correctness: a
// clean grid round-trips through SoftLLRsBestOfN to hard decisions
// reconstructing the codeword. On a noiseless grid all three sources
// (N=1, N=2, N=3) produce the same sign for every bit; selection
// returns the chosen source's LLR but the sign is invariant.
func TestSoftLLRsBestOfN_NoiselessRoundTrip(t *testing.T) {
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = d % 8
	}
	grid := buildNoiselessGrid(trueTones)
	llrs := SoftLLRsBestOfN(grid)

	var expected [FT8CodewordBits]uint8
	for d := 0; d < 58; d++ {
		bits := inverseGrayMap[trueTones[d]]
		expected[3*d] = uint8((bits >> 2) & 1)
		expected[3*d+1] = uint8((bits >> 1) & 1)
		expected[3*d+2] = uint8(bits & 1)
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

// TestSoftLLRsBestOfN_SignConvention pins positive-LLR-favours-bit-0
// across the best-of path. Selection preserves the chosen source's
// sign; all sources share the same sign convention.
func TestSoftLLRsBestOfN_SignConvention(t *testing.T) {
	var tones0 [58]int
	g0 := buildNoiselessGrid(tones0)
	l0 := SoftLLRsBestOfN(g0)
	for i, l := range l0 {
		if l <= 0 {
			t.Errorf("tone-0 grid: llr[%d] = %g, expected > 0", i, l)
		}
	}

	var tones7 [58]int
	for d := 0; d < 58; d++ {
		tones7[d] = 7
	}
	g7 := buildNoiselessGrid(tones7)
	l7 := SoftLLRsBestOfN(g7)
	for i, l := range l7 {
		if l >= 0 {
			t.Errorf("tone-7 grid: llr[%d] = %g, expected < 0", i, l)
		}
	}
}

// TestSoftLLRsBestOfN_LowerNTiebreak pins the §9.2 tiebreak rule:
// when multiple sources produce equal |LLR| for a bit, the lower-N
// source wins (N=1 > N=2 > N=3). On a perfectly noiseless grid all
// three sources produce strictly-positive (or strictly-negative)
// LLRs whose magnitudes happen to differ — but the *source* picked
// should be N=1 byte-identical for any bit where N=1's |LLR|
// equals or exceeds N=2's and N=3's. The strongest pin is the
// source-array check: SoftLLRsBestOfNWithSource should never
// return source=3 for any bit in the noiseless grid case where
// N=1's |LLR| is at least N=3's.
//
// In practice on the noiseless grid, N=2 and N=3 produce LARGER
// |LLR| values than N=1 (because the coherent block-sums add
// amplitudes that scale by N for in-phase signals), so the tiebreak
// rule doesn't fire here. Instead this test directly exercises the
// selection function with hand-crafted LLR vectors via a private
// helper bestOfNSelect — see TestBestOfNSelect for the unit-level
// tiebreak verification.
//
// The current test simply verifies the source array is populated
// (all values are 1, 2, or 3) and that the chosen LLRs match the
// indicated source's LLRs exactly.
func TestSoftLLRsBestOfN_LowerNTiebreak(t *testing.T) {
	var trueTones [58]int
	for d := 0; d < 58; d++ {
		trueTones[d] = (d * 5) % 8
	}
	grid := buildNoiselessGrid(trueTones)

	llrs, src := SoftLLRsBestOfNWithSource(grid)
	n1 := SoftLLRs(grid)
	n2 := SoftLLRsN2(grid)
	n3 := SoftLLRsN3(grid)

	for i := 0; i < FT8CodewordBits; i++ {
		var want float64
		switch src[i] {
		case 1:
			want = n1[i]
		case 2:
			want = n2[i]
		case 3:
			want = n3[i]
		default:
			t.Errorf("bit %d: invalid source value %d (want 1, 2, or 3)", i, src[i])
			continue
		}
		if llrs[i] != want {
			t.Errorf("bit %d: source=%d, llr=%g, want %g (n1=%g n2=%g n3=%g)",
				i, src[i], llrs[i], want, n1[i], n2[i], n3[i])
		}
	}
}

// TestSoftLLRsBestOfN_SourceAttributionNotDegenerate pins the §9.7
// source-attribution sanity check: on a non-clean (mixed-tone) grid,
// the selection histogram should NOT collapse to "all-N=3". The
// lower bound — at least 5% of bits from N=1 or N=2 — catches the
// scale-bias failure mode flagged in §9.3 (raw max-|LLR| collapsing
// to always-N=3 because the higher-N source has systematically
// larger magnitude).
//
// On the clean noiseless grid, all three sources produce identical
// signs but different magnitudes — and N=2/N=3 are systematically
// larger because coherent-sum |LLR| scales with N. So on a clean
// grid the histogram WILL be dominated by N=3 (correctly — there's
// no noise, the higher-N estimate is genuinely more confident).
//
// To test the non-degenerate case we need *noise* — i.e., per-symbol
// LLR magnitudes from the three sources that disagree about which
// is most confident. Constructed here as: for each symbol, place
// signal at the true tone and noise (random-magnitude complex)
// at 1 randomly chosen non-signal tone. The randomness breaks the
// coherent-add advantage N=2/N=3 has on clean signals.
func TestSoftLLRsBestOfN_SourceAttributionNotDegenerate(t *testing.T) {
	rng := newDeterministicRand(20260528)

	// Build a "messy" grid: signal tone has amp 1+0j, one random
	// non-signal tone has a moderate amplitude with random phase,
	// other tones have small random noise.
	g := &SymbolGrid{}
	for d := 0; d < 58; d++ {
		s := dataSymbolIndices[d]
		signalTone := d % 8
		altTone := (signalTone + 1 + (d % 7)) % 8
		for m := 0; m < 8; m++ {
			switch m {
			case signalTone:
				g.Amps[s][m] = complex(1, 0)
				g.Tones[s][m] = 1.0
			case altTone:
				phase := rng.Float64() * 2.0 * math.Pi
				amp := 0.4 + 0.3*rng.Float64()
				g.Amps[s][m] = complex(amp*math.Cos(phase), amp*math.Sin(phase))
				g.Tones[s][m] = amp * amp
			default:
				phase := rng.Float64() * 2.0 * math.Pi
				amp := 0.05 + 0.05*rng.Float64()
				g.Amps[s][m] = complex(amp*math.Cos(phase), amp*math.Sin(phase))
				g.Tones[s][m] = amp * amp
			}
		}
	}

	_, src := SoftLLRsBestOfNWithSource(g)

	counts := [4]int{}
	for _, s := range src {
		counts[s]++
	}
	total := FT8CodewordBits
	pct1 := float64(counts[1]) / float64(total)
	pct2 := float64(counts[2]) / float64(total)
	pct3 := float64(counts[3]) / float64(total)

	const minPct = 0.05
	if pct1 < minPct && pct2 < minPct {
		t.Errorf("source histogram collapsed to all-N=3: pct1=%.2f%% pct2=%.2f%% pct3=%.2f%% (need at least 5%% from N=1 or N=2)",
			pct1*100, pct2*100, pct3*100)
	}
}

// newDeterministicRand returns a deterministic random source for tests
// that need controlled randomness. Uses math/rand's Source seeded with
// the supplied value. Returns a *rand.Rand for clarity of intent.
func newDeterministicRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
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
