package demod

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// TestEstimateNoise_SkipsZeroRows pins the contract that data-symbol
// rows with zero total energy (Demod's slot-edge handling for symbols
// whose audio window falls outside the buffer) are excluded from the
// noise estimate. Without this skip, those zeros would pull the
// Winsorized mean toward zero and produce overconfident LLRs.
func TestEstimateNoise_SkipsZeroRows(t *testing.T) {
	// Build a baseline energy matrix with uniform noise of value 1.0
	// in all non-strongest tones; strongest tone (tone 0) at value 10.
	var energies [dataSymbolCount][ft8ToneCount]float64
	for i := 0; i < dataSymbolCount; i++ {
		energies[i][0] = 10.0
		for t := 1; t < ft8ToneCount; t++ {
			energies[i][t] = 1.0
		}
	}
	baseline := estimateNoise(energies)
	if math.Abs(baseline-1.0) > 1e-9 {
		t.Fatalf("baseline noise = %.6f, want 1.0", baseline)
	}

	// Zero out half the rows (simulate slot-edge handling). Noise
	// estimate must STILL be 1.0 — zero rows are skipped, not averaged.
	for i := 0; i < dataSymbolCount/2; i++ {
		for t := 0; t < ft8ToneCount; t++ {
			energies[i][t] = 0
		}
	}
	got := estimateNoise(energies)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("with %d zeroed rows: noise = %.6f, want 1.0 (zero rows must be skipped)",
			dataSymbolCount/2, got)
	}
}

// TestEstimateNoise_RobustToOutliers pins that the Winsorized mean
// caps the influence of extreme outliers — a small number of
// contaminated loser-tone samples (QRM, adjacent-signal leakage)
// should NOT spike the noise estimate the way a plain mean would.
//
// CONTAMINATION BUDGET MUST STAY BELOW THE WINSORIZE FRACTION. At
// 10% Winsorize each end, contamination > ~10% of total loser
// samples (406) starts leaking through. We test at 5 rows × 6
// contaminated losers = 30 / 406 = 7.4%, well below the budget.
func TestEstimateNoise_RobustToOutliers(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	for i := 0; i < dataSymbolCount; i++ {
		energies[i][0] = 10.0
		for t := 1; t < ft8ToneCount; t++ {
			energies[i][t] = 1.0
		}
	}

	// Contaminate the loser tones in the first 5 rows with extreme
	// values (1000× background). After Winsorize the contamination
	// should be capped well below its raw magnitude.
	for i := 0; i < 5; i++ {
		for t := 1; t < ft8ToneCount; t++ {
			energies[i][t] = 1000.0
		}
	}
	robust := estimateNoise(energies)

	// 5 rows × 6 contaminated losers (each contaminated row's
	// strongest tone is now one of the 1000-valued tones, so it's
	// EXCLUDED; the 6 remaining losers per row are: 1 sample of 10
	// + 5 samples of 1000) + 1 sample of 10 each ≈ 30 outliers of
	// 1000 out of 406 total = 7.4%. Winsorize at 10% caps these.
	if robust > 3.0 {
		t.Errorf("Winsorized noise = %.3f with 7.4%% outliers; expected ≤3.0 (plain mean would be ~75)", robust)
	}
	t.Logf("robust noise with 7.4%% outliers = %.3f", robust)
}

// TestEstimateNoise_ContaminationExceedsBudgetFailsGracefully pins
// the failure mode when contamination exceeds the Winsorize budget
// (>10%). The Winsorize can't help; the noise estimate inflates.
// Documenting this boundary so future operators don't expect
// Winsorize at 10% to handle arbitrary contamination.
func TestEstimateNoise_ContaminationExceedsBudgetFailsGracefully(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	for i := 0; i < dataSymbolCount; i++ {
		energies[i][0] = 10.0
		for t := 1; t < ft8ToneCount; t++ {
			energies[i][t] = 1.0
		}
	}
	for i := 0; i < 15; i++ {
		for t := 1; t < ft8ToneCount; t++ {
			energies[i][t] = 1000.0
		}
	}
	got := estimateNoise(energies)
	// At 15 rows × ~6 contaminated = 90 / 406 = 22% — exceeds 10%
	// Winsorize budget. Estimate is inflated. Documentary only: no
	// strict bound here, just verify it's finite/positive.
	if !(got > 0 && !math.IsInf(got, 0) && !math.IsNaN(got)) {
		t.Errorf("estimateNoise with 22%% contamination = %v, expected finite positive", got)
	}
	t.Logf("at 22%% contamination (above Winsorize budget): noise = %.3f (informational)", got)
}

// TestEstimateNoise_HandlesEmptyEnergies pins the safe-return-on-empty
// contract. estimateNoise must not panic or return NaN/Inf if all
// rows are zero.
func TestEstimateNoise_HandlesEmptyEnergies(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	got := estimateNoise(energies)
	if got != 0 {
		t.Errorf("estimateNoise on all-zero energies = %v, want 0", got)
	}
}

// TestEstimateCostasCalibration_FindsHighSNRSignal sweeps the clean
// fixture's frequency band for high-SNR positions. The 10 CQ signals
// from gen10cq have substantial signal-to-noise headroom; at LEAST
// ONE of the swept positions should produce SNR ≥ 5.
//
// We avoid hardcoding signal frequencies (gen10cq's exact positions
// depend on its seed and may change). Sweeping is more robust.
func TestEstimateCostasCalibration_FindsHighSNRSignal(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}

	// 3.125 Hz bin spacing × every-32-bin sweep across the FT8 band.
	// At least one of these should land near a synthetic CQ signal.
	bestSNR := 0.0
	bestFreq := 0.0
	for freq := 300.0; freq < 2800.0; freq += 100.0 {
		signal, noise := EstimateCostasCalibration(data.Samples, freq, 0.0)
		if noise <= 0 {
			continue
		}
		snr := signal / noise
		if snr > bestSNR {
			bestSNR = snr
			bestFreq = freq
		}
	}
	if bestSNR < 5.0 {
		t.Errorf("swept band 300-2800 Hz: best SNR = %.2f (at %.0f Hz), expected ≥5 somewhere", bestSNR, bestFreq)
	}
	t.Logf("clean fixture: best Costas SNR = %.2f at %.0f Hz", bestSNR, bestFreq)
}

// TestEstimateCostasCalibration_HandlesEdgeDT pins that extreme-dt
// calibration calls don't panic and produce finite, non-negative
// values. The (0, 0) early-return path triggers only when fewer than
// 3 anchors are accessible — which requires |dt| close to ±13s
// (TX window of 12.6s sliding entirely out of the 15s slot). At
// more moderate edge-DT (e.g., ±2 to ±5s) most or all anchors may
// still be accessible since the Costas blocks span the full TX
// window, and the function should return finite output.
func TestEstimateCostasCalibration_HandlesEdgeDT(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}
	for _, dt := range []float64{-2.0, -5.0, +2.0, +5.0} {
		signal, noise := EstimateCostasCalibration(data.Samples, 1500.0, dt)
		if math.IsNaN(signal) || math.IsInf(signal, 0) || math.IsNaN(noise) || math.IsInf(noise, 0) {
			t.Errorf("at dt=%g s: non-finite signal=%g noise=%g", dt, signal, noise)
		}
		if signal < 0 || noise < 0 {
			t.Errorf("at dt=%g s: negative signal=%g noise=%g", dt, signal, noise)
		}
	}
}

// TestLLRsSoftened_CleanFixtureMatchesLLRs pins that LLRsSoftened
// produces output identical to LLRs on a clean-SNR fixture: the
// softening must NOT touch non-pathological rows, only clamp the
// pathological ones. With the clean fixture's high SNR, no rows
// should be flagged pathological.
func TestLLRsSoftened_CleanFixtureMatchesLLRs(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}
	// Energies at a known-good position from the clean fixture (any
	// strong CQ signal). Use (900 Hz, 0 dt) which works for the
	// existing high-confidence test.
	energies := Demod(data.Samples, 900.0, 0.0)
	baseline := LLRs(energies)
	softened := LLRsSoftened(energies, 1.0, 2.0, 5.0)

	for i, want := range baseline {
		got := softened[i]
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("clean row %d: LLRsSoftened=%g, LLRs=%g (softened should not touch clean rows)", i, got, want)
		}
	}
}

// TestLLRsSoftened_PathologicalRowsClamped pins that a synthetic
// pathological row (all 8 tones equal — ambiguous and weak) has its
// LLRs capped at ±softClamp while a baseline-style row (one strong
// tone) keeps full ±llrClamp confidence.
func TestLLRsSoftened_PathologicalRowsClamped(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	// Row 0: pathological (all tones equal at noise level 1.0).
	for t2 := 0; t2 < ft8ToneCount; t2++ {
		energies[0][t2] = 1.0
	}
	// Rows 1..57: clean (one strong tone at value 100, others at 1).
	for i := 1; i < dataSymbolCount; i++ {
		energies[i][0] = 100.0
		for t2 := 1; t2 < ft8ToneCount; t2++ {
			energies[i][t2] = 1.0
		}
	}
	const softClamp = 5.0
	llrs := LLRsSoftened(energies, 1.0, 2.0, softClamp)

	// Row 0's 3 LLRs (indices 0..2) must be within ±softClamp.
	for j := 0; j < bitsPerSymbol; j++ {
		if math.Abs(llrs[j]) > softClamp+1e-9 {
			t.Errorf("pathological row 0 bit %d: |LLR|=%g exceeds soft clamp %g", j, llrs[j], softClamp)
		}
	}
	// Rows 1+ should saturate at ±llrClamp (=20) for strong-tone-dominated rows.
	// Just sample a few middle indices to verify.
	for j := 0; j < bitsPerSymbol; j++ {
		idx := 10*bitsPerSymbol + j // row 10
		if math.Abs(llrs[idx]) < softClamp+1e-9 {
			t.Errorf("clean row 10 bit %d: |LLR|=%g is within softClamp — clean row should NOT be softened", j, llrs[idx])
		}
	}
}

// TestLLRsSoftened_WeakRowGetsSoftened pins the T2 (weak-row)
// criterion: a row whose strongest tone barely beats the noise
// floor gets softened even if its top1-top2 contrast is decent.
func TestLLRsSoftened_WeakRowGetsSoftened(t *testing.T) {
	var energies [dataSymbolCount][ft8ToneCount]float64
	// Make a noise floor: most rows at level 1.0 across all tones.
	// Row 0: weak — top1 just above noise, others below. top1/noise
	// ratio is 1.5, which is < T2=2.0 so should trigger weak-row
	// pathological flag. top1-top2 = 1.5 - 0.1 = 1.4 > T1=1.0 so
	// would NOT trigger ambiguous-winner alone — confirms T2 path
	// is independent.
	energies[0][0] = 1.5
	for t2 := 1; t2 < ft8ToneCount; t2++ {
		energies[0][t2] = 0.1
	}
	// Filler rows: uniform 1.0 to set the noise estimate to ~1.0.
	for i := 1; i < dataSymbolCount; i++ {
		for t2 := 0; t2 < ft8ToneCount; t2++ {
			energies[i][t2] = 1.0
		}
	}
	const softClamp = 5.0
	llrs := LLRsSoftened(energies, 1.0, 2.0, softClamp)
	for j := 0; j < bitsPerSymbol; j++ {
		if math.Abs(llrs[j]) > softClamp+1e-9 {
			t.Errorf("weak row 0 bit %d: |LLR|=%g exceeds soft clamp %g — T2 weak-row gate should have fired", j, llrs[j], softClamp)
		}
	}
}

// TestLLRsSoftened_ParameterValidation pins the input-validation
// added 2026-05-26 per code review: arbitrary user-supplied
// parameters (NaN, negative, oversized) must not produce garbage
// output. Function silently clamps to safe range rather than
// returning an error, so callers don't need to validate themselves.
func TestLLRsSoftened_ParameterValidation(t *testing.T) {
	// Energy matrix with rows that should all pass the gate cleanly
	// at any sane parameter setting — one tone dominates, others at
	// noise floor. With clean rows, LLRsSoftened should never apply
	// the soft clamp.
	var energies [dataSymbolCount][ft8ToneCount]float64
	for i := 0; i < dataSymbolCount; i++ {
		energies[i][0] = 100.0
		for tn := 1; tn < ft8ToneCount; tn++ {
			energies[i][tn] = 1.0
		}
	}

	// Each of these inputs would, if not validated, produce garbage:
	cases := []struct {
		name              string
		t1, t2, softClamp float64
	}{
		{"NaN T1", math.NaN(), 2.0, 5.0},
		{"NaN T2", 1.0, math.NaN(), 5.0},
		{"NaN softClamp", 1.0, 2.0, math.NaN()},
		{"negative softClamp", 1.0, 2.0, -5.0},
		{"negative T1", -1.0, 2.0, 5.0},
		{"negative T2", 1.0, -2.0, 5.0},
		{"oversized softClamp", 1.0, 2.0, 100.0},
		{"all zero", 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			llrs := LLRsSoftened(energies, c.t1, c.t2, c.softClamp)
			for i, v := range llrs {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Errorf("bit %d: LLR=%v (NaN/Inf — validation should have caught the bad input)", i, v)
					return
				}
				if math.Abs(v) > llrClamp+1e-9 {
					t.Errorf("bit %d: |LLR|=%g exceeds llrClamp=%g — softClamp should be silently capped at llrClamp",
						i, v, llrClamp)
					return
				}
			}
		})
	}
}

// TestEstimateCostasCalibration_ReturnsZeroForExtremeDT pins the
// (0, 0) early-return when fewer than 3 anchors are accessible.
// Requires |dt| beyond the TX window's reach.
func TestEstimateCostasCalibration_ReturnsZeroForExtremeDT(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}
	// Extreme negative dt: TX would start at sample -15 × 12000 + 6000
	// = -174000. Anchor at sym=78 has symStart = -174000 + 78×1920 = -24240,
	// still negative → inaccessible. At dt=-14.5, anchor sym=78 has
	// symStart = (0.5 - 14.5)*12000 + 78*1920 = -168000 + 149760 = -18240,
	// still negative. So at dt=-14.5 ALL anchors are inaccessible.
	signal, noise := EstimateCostasCalibration(data.Samples, 1500.0, -14.5)
	if signal != 0 || noise != 0 {
		t.Errorf("at dt=-14.5s (all anchors inaccessible): signal=%g noise=%g, want (0, 0)",
			signal, noise)
	}
}
