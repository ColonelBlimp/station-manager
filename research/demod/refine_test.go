package demod

import (
	"math"
	"math/rand"
	"testing"
)

// TestPhaseRefineFreq_RecoversKnownOffset is the load-bearing
// convergence test: synthesise a signal whose carrier sits Δf away
// from the candidate's stated freq, then refine. The refined freq
// must converge to the actual signal freq within
// phaseRefineStopDfHz tolerance.
//
// IMPORTANT: this test does NOT check the fit's Slope after
// refinement. fitCostasPhase's Slope is 2π·f_signal·T_sym mod 2π,
// intrinsic to the signal's carrier — it doesn't go to zero when
// demod matches signal. The real proof of convergence is
// `refined ≈ signalFreq`, which IS what we check.
func TestPhaseRefineFreq_RecoversKnownOffset(t *testing.T) {
	const baseFreq = 1000.0
	for _, deltaHz := range []float64{0.5, -0.5, 1.0, -1.0, 0.3, -0.3} {
		t.Run(humanFloatLabel(deltaHz), func(t *testing.T) {
			slopeRadPerSym := 2 * math.Pi * deltaHz * float64(nsps) / fs
			samples := synthCostasPhase(baseFreq, 0.0, 0.4, slopeRadPerSym)

			refined, initialFit, finalFit, dfTotal, ok := PhaseRefineFreq(samples, baseFreq, 0.0)
			if !ok {
				t.Fatalf("refinement reported !ok: initial RMS=%g, final RMS=%g",
					initialFit.RMSResid, finalFit.RMSResid)
			}

			expected := baseFreq + deltaHz
			if math.Abs(refined-expected) > 0.1 {
				t.Errorf("refined = %.4f, want %.4f (Δ=%+.4f, expected Δf=%+g, dfTotal=%+.4f)",
					refined, expected, refined-expected, deltaHz, dfTotal)
			}
			t.Logf("Δf=%+g: refined=%.4f (Δ=%+.4f), dfTotal=%+.4f, initial slope=%+.4f, final slope=%+.4f (intrinsic, not a convergence signal)",
				deltaHz, refined, refined-expected, dfTotal, initialFit.Slope, finalFit.Slope)
		})
	}
}

// TestPhaseRefineFreq_SignConvention pins the two assertions that
// matter — the empirical sign of the slope-to-df mapping. Per the
// operator's Session 90 directive: don't trust hand derivation; pin
// the observable behaviour.
//
//	candidate below true signal → correction increases freq (df > 0)
//	candidate above true signal → correction decreases freq (df < 0)
//
// Synth setup: signal at 1001 Hz, candidate at 1000 Hz (candidate
// below true) ⇒ expect refined > 1000. And: signal at 999 Hz,
// candidate at 1000 Hz (candidate above true) ⇒ expect refined < 1000.
func TestPhaseRefineFreq_SignConvention(t *testing.T) {
	twoPiTSym := 2 * math.Pi * float64(nsps) / fs

	t.Run("candidate_below_true", func(t *testing.T) {
		signalFreq := 1001.0
		candidateFreq := 1000.0
		slope := twoPiTSym * (signalFreq - candidateFreq)
		samples := synthCostasPhase(candidateFreq, 0.0, 0.0, slope)

		refined, _, _, dfTotal, ok := PhaseRefineFreq(samples, candidateFreq, 0.0)
		if !ok {
			t.Fatal("!ok on signal=1001 candidate=1000")
		}
		if dfTotal <= 0 {
			t.Errorf("candidate BELOW true: dfTotal = %+g, want > 0 (refine UP toward signal)", dfTotal)
		}
		if math.Abs(refined-signalFreq) > 0.1 {
			t.Errorf("candidate BELOW true: refined = %.4f, want ≈ %.4f", refined, signalFreq)
		}
		t.Logf("candidate=%.1f below signal=%.1f: refined=%.4f, dfTotal=%+.4f ✓ moves UP",
			candidateFreq, signalFreq, refined, dfTotal)
	})

	t.Run("candidate_above_true", func(t *testing.T) {
		signalFreq := 999.0
		candidateFreq := 1000.0
		slope := twoPiTSym * (signalFreq - candidateFreq)
		samples := synthCostasPhase(candidateFreq, 0.0, 0.0, slope)

		refined, _, _, dfTotal, ok := PhaseRefineFreq(samples, candidateFreq, 0.0)
		if !ok {
			t.Fatal("!ok on signal=999 candidate=1000")
		}
		if dfTotal >= 0 {
			t.Errorf("candidate ABOVE true: dfTotal = %+g, want < 0 (refine DOWN toward signal)", dfTotal)
		}
		if math.Abs(refined-signalFreq) > 0.1 {
			t.Errorf("candidate ABOVE true: refined = %.4f, want ≈ %.4f", refined, signalFreq)
		}
		t.Logf("candidate=%.1f above signal=%.1f: refined=%.4f, dfTotal=%+.4f ✓ moves DOWN",
			candidateFreq, signalFreq, refined, dfTotal)
	})
}

// TestPhaseRefineFreq_OffGridStartingCandidate pins convergence
// when the STARTING candidate freq isn't a multiple of baud — i.e.,
// not on the FT8 bin grid. This is the case where the bug hid:
// for on-grid f_demod (multiple of 6.25 Hz), the demod-term
// 2π·f_demod·T_sym ≡ 0 mod 2π, making the absolute-frame slope
// numerically equal to the residual slope. As soon as the loop
// steps off-grid, the bridge becomes necessary; if it's missing
// or wrong, the test fails.
//
// Setup: signal at 1001.0 Hz, starting candidate at 1000.3 Hz
// (off-grid). One-iteration convergence expected: residual at
// 1000.3 ≈ 2π·0.7·T_sym, df ≈ 0.7 Hz, → 1001.0.
func TestPhaseRefineFreq_OffGridStartingCandidate(t *testing.T) {
	twoPiTSym := 2 * math.Pi * float64(nsps) / fs

	signalFreq := 1001.0
	candidateFreq := 1000.3 // off-grid
	slope := twoPiTSym * (signalFreq - 1000.0)
	samples := synthCostasPhase(1000.0, 0.0, 0.0, slope)

	refined, _, _, dfTotal, ok := PhaseRefineFreq(samples, candidateFreq, 0.0)
	if !ok {
		t.Fatal("!ok on off-grid candidate=1000.3 signal=1001.0")
	}
	if math.Abs(refined-signalFreq) > 0.1 {
		t.Errorf("off-grid: refined=%.4f want ≈ %.4f (dfTotal=%+.4f)",
			refined, signalFreq, dfTotal)
	}
	t.Logf("off-grid: candidate=%.1f signal=%.1f → refined=%.4f, dfTotal=%+.4f",
		candidateFreq, signalFreq, refined, dfTotal)
}

// TestPhaseRefineFreq_ResidualShrinksAfterCorrection pins the
// "one correction shrinks residualSlope" property the operator
// asked for. Run a single iteration of the bridge manually and
// verify |residualSlope| at the new freq is smaller than at the
// original.
func TestPhaseRefineFreq_ResidualShrinksAfterCorrection(t *testing.T) {
	twoPiTSym := 2 * math.Pi * float64(nsps) / fs
	signalFreq := 1001.0
	candidateFreq := 1000.0
	slope := twoPiTSym * (signalFreq - candidateFreq)
	samples := synthCostasPhase(candidateFreq, 0.0, 0.0, slope)

	initialFit := fitCostasPhase(samples, candidateFreq, 0.0)
	if math.IsInf(initialFit.RMSResid, 1) {
		t.Fatal("initial fit invalid")
	}
	residualBefore := wrapPi(initialFit.Slope - twoPiTSym*candidateFreq)
	df := residualBefore / twoPiTSym

	newFreq := candidateFreq + df
	newFit := fitCostasPhase(samples, newFreq, 0.0)
	if math.IsInf(newFit.RMSResid, 1) {
		t.Fatal("new fit invalid")
	}
	residualAfter := wrapPi(newFit.Slope - twoPiTSym*newFreq)

	if math.Abs(residualAfter) >= math.Abs(residualBefore) {
		t.Errorf("residualSlope did not shrink: |before|=%.4f → |after|=%.4f",
			math.Abs(residualBefore), math.Abs(residualAfter))
	}
	if math.Abs(residualAfter) > 0.1 {
		t.Errorf("|residualSlope after one correction| = %.4f, want ≤ 0.1 rad for clean synth",
			math.Abs(residualAfter))
	}
	t.Logf("residual: |before|=%.4f → |after|=%.4f (df=%+.4f, new freq=%.4f)",
		math.Abs(residualBefore), math.Abs(residualAfter), df, newFreq)
}

// TestPhaseRefineFreq_FinalResidualBelowStopThreshold confirms the
// loop converges so that the final iteration's |df| is below the
// stop tolerance. Uses PhaseRefineFreq directly (not the manual
// bridge) so this exercises the full iteration logic.
func TestPhaseRefineFreq_FinalResidualBelowStopThreshold(t *testing.T) {
	twoPiTSym := 2 * math.Pi * float64(nsps) / fs
	signalFreq := 1001.2
	candidateFreq := 1000.0
	slope := twoPiTSym * (signalFreq - candidateFreq)
	samples := synthCostasPhase(candidateFreq, 0.0, 0.0, slope)

	refined, _, finalFit, _, ok := PhaseRefineFreq(samples, candidateFreq, 0.0)
	if !ok {
		t.Fatal("!ok")
	}
	finalResidual := wrapPi(finalFit.Slope - twoPiTSym*refined)
	finalDf := finalResidual / twoPiTSym
	if math.Abs(finalDf) > phaseRefineStopDfHz {
		t.Errorf("final |df| = %.4f Hz, want ≤ %g (stop threshold)",
			math.Abs(finalDf), phaseRefineStopDfHz)
	}
	t.Logf("converged: refined=%.4f signal=%.4f, |final df|=%.5f Hz (stop ≤ %g)",
		refined, signalFreq, math.Abs(finalDf), phaseRefineStopDfHz)
}

// TestPhaseRefineFreq_NoChangeWhenAlreadyConverged confirms the
// stop-tolerance gate: a signal already at the candidate's stated
// freq should produce dfTotal ≈ 0 and converge in zero or one
// iteration.
func TestPhaseRefineFreq_NoChangeWhenAlreadyConverged(t *testing.T) {
	const baseFreq = 1000.0
	samples := synthCostasPhase(baseFreq, 0.0, 0.4, 0.0) // zero slope
	refined, initialFit, finalFit, dfTotal, ok := PhaseRefineFreq(samples, baseFreq, 0.0)
	if !ok {
		t.Fatalf("refinement reported !ok on already-converged signal")
	}
	if math.Abs(dfTotal) > phaseRefineStopDfHz {
		t.Errorf("dfTotal = %+.4f Hz on already-converged signal, want < %g",
			dfTotal, phaseRefineStopDfHz)
	}
	if math.Abs(refined-baseFreq) > 0.05 {
		t.Errorf("refined = %.4f, want %.4f", refined, baseFreq)
	}
	t.Logf("converged signal: initial slope=%+.4f, final slope=%+.4f, refined=%.4f, dfTotal=%+.4f",
		initialFit.Slope, finalFit.Slope, refined, dfTotal)
}

// TestPhaseRefineFreq_NoiseReturnsNotOk confirms the guardrail
// against drifting on signal-free input. Pure noise has no
// coherent Costas anchors; fitCostasPhase returns +Inf RMSResid;
// refinement must report !ok and not drift the candidate freq.
func TestPhaseRefineFreq_NoiseReturnsNotOk(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	txStart := int(math.Round(synthSlotStartSec * fs))
	bufLen := txStart + nn*nsps + 1
	samples := make([]float32, bufLen)
	for i := range samples {
		samples[i] = float32(rng.NormFloat64() * 0.05)
	}

	refined, _, _, dfTotal, ok := PhaseRefineFreq(samples, 1000.0, 0.0)
	if ok {
		t.Errorf("PhaseRefineFreq returned ok=true on pure noise (refined=%.3f dfTotal=%+.3f)",
			refined, dfTotal)
	}
	if refined != 1000.0 {
		t.Errorf("on !ok, refined should equal originalFreq; got %.4f want 1000.0", refined)
	}
}

// humanFloatLabel formats a float as a stable subtest name.
func humanFloatLabel(v float64) string {
	sign := "+"
	if v < 0 {
		sign = "-"
	}
	abs := math.Abs(v)
	return sign + formatFloat3(abs)
}

func formatFloat3(v float64) string {
	if v == math.Trunc(v) {
		return formatInt(int(v)) + "Hz"
	}
	return formatFloatGeneric(v) + "Hz"
}

func formatInt(n int) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func formatFloatGeneric(v float64) string {
	// Quick and dirty for test labels — math.Round to 3 decimals.
	scaled := math.Round(v * 1000)
	intPart := int(scaled) / 1000
	fracPart := int(scaled) % 1000
	if fracPart == 0 {
		return formatInt(intPart)
	}
	return formatInt(intPart) + "p" + formatInt(fracPart)
}
