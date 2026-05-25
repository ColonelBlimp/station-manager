package demod

import (
	"math"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
)

// TestGrayUnmap_IsValidGrayCode confirms the QEX Table 3 mapping is
// a valid Gray code: every adjacent-tone pair (0↔1, 1↔2, ..., 6↔7)
// differs in exactly one bit. This is the structural property a
// Gray code is built to guarantee, and it's the load-bearing reason
// FT8 uses this assignment in the first place — verifying it here
// is the cheapest possible regression guard against the table being
// transcribed wrong.
func TestGrayUnmap_IsValidGrayCode(t *testing.T) {
	for tone := 0; tone < ft8ToneCount-1; tone++ {
		a, b := GrayUnmap[tone], GrayUnmap[tone+1]
		diff := a ^ b
		// popcount over a uint8 — Go has bits.OnesCount8 but the
		// hand-rolled loop is just as clear at width 3.
		ones := 0
		for k := 0; k < bitsPerSymbol; k++ {
			if diff&(1<<uint(k)) != 0 {
				ones++
			}
		}
		if ones != 1 {
			t.Errorf("tones %d (%03b) and %d (%03b) differ by %d bits, want 1",
				tone, a, tone+1, b, ones)
		}
	}
}

// TestGrayUnmap_IsPermutation confirms every 3-bit value (0..7)
// appears exactly once. A duplicate or skipped value would mean some
// LDPC bit pattern cannot be transmitted, breaking both encode and
// decode paths.
func TestGrayUnmap_IsPermutation(t *testing.T) {
	seen := [8]bool{}
	for tone := 0; tone < ft8ToneCount; tone++ {
		v := GrayUnmap[tone]
		if v >= 8 {
			t.Fatalf("GrayUnmap[%d] = %d out of 3-bit range", tone, v)
		}
		if seen[v] {
			t.Fatalf("GrayUnmap[%d] = %d already seen at earlier tone", tone, v)
		}
		seen[v] = true
	}
}

// TestGrayUnmap_Is4_4Partitioned confirms the structural invariant
// the LLRs function relies on: every bit position (MSB, middle, LSB)
// partitions the 8 tones into exactly 4 zeros and 4 ones. The
// [4]float64 buffers in LLRs assume this — a partition skew here
// would corrupt one of the two log-sum-exp inputs.
func TestGrayUnmap_Is4_4Partitioned(t *testing.T) {
	for j := 0; j < bitsPerSymbol; j++ {
		mask := uint8(1) << uint(bitsPerSymbol-1-j)
		n0, n1 := 0, 0
		for tone := 0; tone < ft8ToneCount; tone++ {
			if GrayUnmap[tone]&mask == 0 {
				n0++
			} else {
				n1++
			}
		}
		if n0 != 4 || n1 != 4 {
			t.Errorf("bit position j=%d (mask=%08b): %d zeros / %d ones, want 4/4", j, mask, n0, n1)
		}
	}
}

// TestLLRs_SignConvention pins the positive-LLR-means-bit-0 rule on
// a synthetic case. Energies where tone 0 (= bits 000) dominates by
// a huge margin should produce three strongly-positive LLRs at the
// corresponding output positions.
func TestLLRs_SignConvention(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	// Symbol 0: tone 0 dominates → expected bits 000 → all LLR > 0.
	energies[0][0] = 1e6
	for t := 1; t < ft8ToneCount; t++ {
		energies[0][t] = 1.0
	}
	// Symbol 1: tone 7 dominates → expected bits 111 → all LLR < 0.
	energies[1][7] = 1e6
	for t := 0; t < ft8ToneCount-1; t++ {
		energies[1][t] = 1.0
	}

	llrs := LLRs(energies)

	for j := 0; j < bitsPerSymbol; j++ {
		if llrs[j] <= 0 {
			t.Errorf("symbol 0 (tone 0 dominant) bit %d LLR = %v, want > 0", j, llrs[j])
		}
		if llrs[bitsPerSymbol+j] >= 0 {
			t.Errorf("symbol 1 (tone 7 dominant) bit %d LLR = %v, want < 0", j, llrs[bitsPerSymbol+j])
		}
	}
}

// TestLLRs_Clamp pins the ±llrClamp ceiling on the output magnitudes.
// A tone with overwhelming energy must not produce an infinite or
// numerically-uncontrolled LLR — the clamp protects downstream LDPC
// from a single bad symbol hijacking the parity equations.
func TestLLRs_Clamp(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	for i := 0; i < dataSymbolCount; i++ {
		energies[i][0] = 1e12
		for t := 1; t < ft8ToneCount; t++ {
			energies[i][t] = 1e-6
		}
	}
	llrs := LLRs(energies)
	for i, v := range llrs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("LLR[%d] = %v (NaN or Inf — numerical-stability bug)", i, v)
		}
		if math.Abs(v) > llrClamp+1e-9 {
			t.Errorf("LLR[%d] = %v exceeds clamp ±%v", i, v, llrClamp)
		}
	}
}

// TestLLRs_AllZeroEnergies confirms the all-zero input edge case
// doesn't NaN. Per-tone metrics are all zero, each logsumexp is
// log(4), and every LLR is exactly zero.
func TestLLRs_AllZeroEnergies(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	llrs := LLRs(energies)
	for i, v := range llrs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("LLR[%d] = %v on all-zero input", i, v)
		}
		if v != 0 {
			t.Errorf("LLR[%d] = %v on all-zero input, want 0", i, v)
		}
	}
}

// TestLogSumExp4_Stability covers the numerical core in isolation:
// large positive inputs without max-shift would overflow exp(),
// large negative inputs would underflow to zero (giving log(0) =
// -Inf). The max-shift keeps both ends finite.
func TestLogSumExp4_Stability(t *testing.T) {
	// Large positives: result should be ~ max + log(4).
	got := logSumExp4([4]float64{1000, 1000, 1000, 1000})
	want := 1000 + math.Log(4)
	if math.Abs(got-want) > 1e-9 || math.IsNaN(got) || math.IsInf(got, 0) {
		t.Errorf("logSumExp4(1000×4) = %v, want ~%v", got, want)
	}
	// Large negatives: not -Inf.
	got = logSumExp4([4]float64{-1000, -1000, -1000, -1000})
	want = -1000 + math.Log(4)
	if math.Abs(got-want) > 1e-9 || math.IsNaN(got) || math.IsInf(got, 0) {
		t.Errorf("logSumExp4(-1000×4) = %v, want ~%v", got, want)
	}
	// Single-spike: result dominated by the max.
	got = logSumExp4([4]float64{100, 0, 0, 0})
	if math.Abs(got-100) > 1e-9 {
		t.Errorf("logSumExp4 with spike 100 should equal ~100, got %v", got)
	}
}

// TestLLRs_CleanFixtureProducesHighConfidence runs the full
// candidate → Demod → LLRs chain on the clean synthetic fixture and
// verifies the bulk of the 174 output LLRs are at high confidence
// (|LLR| ≥ 10). On a clean signal with zero added noise the per-tone
// SNR is enormous and the metric differences should saturate the
// clamp — a low high-confidence count would point at a clamp/scale
// bug, a graymap transcription error, or a sign-convention slip.
//
// Budget is calibrated lightly: ≥ 90% of 174 LLRs at |LLR| ≥ 10 on
// the clean fixture. Tightens easily; loosen only with evidence.
func TestLLRs_CleanFixtureProducesHighConfidence(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}
	cands := candidates.Find(data.Samples)
	if len(cands) == 0 {
		t.Fatal("no candidates found in clean fixture")
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	top := cands[0]

	energies := Demod(data.Samples, top.Freq, top.DT)
	llrs := LLRs(energies)

	highConf := 0
	for _, v := range llrs {
		if math.Abs(v) >= 10 {
			highConf++
		}
	}
	minHigh := int(math.Floor(0.9 * float64(codewordBits)))
	if highConf < minHigh {
		t.Errorf("clean fixture LLR confidence: %d/%d ≥ 10, want ≥ %d (top candidate freq=%.2f dt=%.3f)",
			highConf, codewordBits, minHigh, top.Freq, top.DT)
	}
	t.Logf("clean fixture, top candidate freq=%.2f dt=%.3f: %d/%d LLRs at |LLR|≥10",
		top.Freq, top.DT, highConf, codewordBits)
}
