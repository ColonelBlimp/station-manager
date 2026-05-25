// Phase-coherent frequency refinement for FT8 candidates.
//
// **Status: research artifact, NOT on the default decode path.**
// Session 92 (2026-05-25) wired PhaseRefineFreq into decode-eval as
// `-refine-freq`, measured against the 144-signal real-capture
// corpus, and ruled it out as a parity lever:
//
//	mode                       matched  text-extra  CRC-pass
//	------------------------   -------  ----------  --------
//	incoherent baseline             96           2        98
//	-refine-freq                    96           4       100
//	-coherent                       96           2        98
//	-coherent -refine-freq          96           3        99
//
// Diagnostics over 315 refined-OK candidates (of 584 total):
//   - |dfTotal| median 0.692 Hz; 40% hit the 3 Hz maxCorrection cap.
//   - Phase-RMS median 1.325 (init) → 1.292 (final) — ~2.5% reduction.
//     If freq offset were the cause of the high slope, RMS would
//     collapse toward zero after refinement. It doesn't.
//   - Final RMS median 1.292 is still 3× the coherent threshold (0.4).
//     Refined candidates are NOT becoming coherent-eligible.
//
// What this falsifies: Session 90's working hypothesis that real-signal
// candidates were sitting off-bin and refinement would re-center them.
// The high slope/RMS in those candidates is not dominated by frequency
// offset — it's the same adjacent-signal cross-contamination Session 84
// identified for matched-filter subtraction, which corrupts the Costas
// anchor phases such that the linear-model slope estimate is noise
// rather than signal carrier. Refinement chases the noise and lands
// somewhere worse, admitting +1 to +2 text-extras.
//
// The primitive is retained because (a) it is correct on synthetic
// AWGN-only signals (TestPhaseRefineFreq_RecoversKnownOffset), which
// makes it useful for controlled experiments, and (b) the
// slope-to-freq-offset bridge (slopeToFreqOffsetHz) is a reusable
// piece of the DTFT-vs-mix-and-integrate convention work and
// documents that derivation in one place.
//
// Do not re-wire into decode-eval without new evidence — the
// corpus measurement is documented above and the slope/RMS
// diagnostic (Session 90) explains why the hypothesis fails.
package demod

import "math"

// Phase-coherent frequency refinement constants. Operator-set in
// Session 90 per the slope-vs-RMS diagnostic finding (84.1% of
// real-signal candidates have |slope| ≥ 0.3 rad/sym → equivalent
// freq offset ≥ ~0.3 Hz, larger than the candidate finder's 1 Hz
// fine-refinement grid spacing).
const (
	// phaseRefineMaxIter caps the inner loop. Three iterations is
	// enough for typical convergence on real captures (the second
	// iteration usually gets us under stopDfHz once the first has
	// removed the gross offset); more iterations rarely shave off
	// meaningful slope and risk oscillating on marginal candidates.
	phaseRefineMaxIter = 3

	// phaseRefineMaxStepHz clamps a single iteration's step. Anchors
	// far from the true frequency can produce a very large slope
	// estimate; clamping prevents one bad iteration from flinging
	// the refined freq into a wrong attractor.
	phaseRefineMaxStepHz = 1.5

	// phaseRefineStopDfHz is the convergence tolerance. Below this
	// the residual slope is small enough not to materially affect
	// coherent demod or LLR alignment.
	phaseRefineStopDfHz = 0.05

	// phaseRefineMaxCorrectionHz is the validity bound. A candidate
	// that needs more than ±3 Hz of correction probably wasn't a
	// real signal at the original freq; refining past this risks
	// locking onto a different signal. Refinement bails (returns
	// the original freq) if the cumulative drift exceeds this.
	phaseRefineMaxCorrectionHz = 3.0
)

// PhaseRefineFreq iteratively refines a candidate's freq by zeroing
// out the demod misalignment (`f_signal - f_demod`) inferred from
// the Costas-anchor phase slope. Returns refined freq, the initial
// and final phase fits, the cumulative correction dfTotal, and an
// ok flag.
//
// **Frame note.** fit.Slope is the ABSOLUTE phase advance between
// symbol windows — 2π·f_signal·T_sym (mod 2π), reflecting the
// signal's carrier in the windowed-DTFT's own reference. Refinement
// needs the signal-minus-demod residual, which requires subtracting
// the demodulator's expected phase advance 2π·f_demod·T_sym (mod 2π)
// first. That bridge is slopeToFreqOffsetHz; without it the loop
// diverges because the absolute slope doesn't respond to demod
// movement.
//
// **Slope convention bridge (load-bearing).** fitCostasPhase returns
// Slope = 2π·f_signal·T_sym (mod 2π) — the DTFT-arg-per-sym rate of
// phase advance INTRINSIC to the signal's carrier. This Slope is
// the right quantity for coherent-demod phase prediction (DemodCoherent
// uses Phi0 + Slope·s and the data-symbol Goertzel outputs follow the
// same DTFT convention). But it is NOT the quantity to drive freq
// refinement: it doesn't change as f_demod varies, so a naive
// `df = Slope/(2π·T_sym)` loop diverges (each iter re-applies the
// same step). The misalignment slope wanted for refinement is
//
//	slope_user = wrapPi(Slope - 2π·f_demod·T_sym)
//
// which IS responsive to f_demod (the second term tracks the demod
// position). Then df = slope_user/(2π·T_sym) is the right per-iter
// correction. For on-grid f_demod (multiple of baud), the second
// term is 0 mod 2π and slope_user == Slope — but as soon as the loop
// steps off-grid, the bridge matters. See slopeToFreqOffsetHz.
//
// Algorithm:
//
//	refinedFreq = freqHz
//	loop maxIter times:
//	  fit at refinedFreq
//	  df = slopeToFreqOffsetHz(fit.Slope, refinedFreq)
//	  if |df| < stopDfHz: break (converged)
//	  clamp |df| to maxStepHz
//	  bail if |refinedFreq + df - originalFreq| > maxCorrectionHz
//	  refinedFreq += df
//	finalFit = fit at refinedFreq
//	roll back if finalFit accessible-anchor count < initialFit
//
// **Sign:** slope_user > 0 ⇒ signal above demod ⇒ df > 0 (move demod
// UP). The operator's Session 90 spec wrote df with a leading
// minus; my derivation + the known-offset synth test
// (TestPhaseRefineFreq_RecoversKnownOffset) confirm the positive
// sign. The pseudocode minus was a sign typo; the algorithm shape
// is otherwise as specified.
//
// **Guardrails.**
//
//   - Invalid initial fit (AccessibleAnchors < 3 or RMSResid = +Inf)
//     → return original freq with ok=false. Caller uses the
//     unrefined candidate.
//   - Step that would carry refinedFreq beyond ±maxCorrectionHz of
//     the original → break the loop, keep what we have so far.
//   - Final fit's count of accessible-with-positive-weight anchors
//     less than initial → refinement broke something; roll back to
//     original. (Refinement should never lose anchors; if it does,
//     we've moved into noise.)
//
// **Diagnostics.** The caller can compare initial vs final fit
// (PhaseFit.Slope, RMSResid, weights) to summarise the refinement
// impact. dfTotal = refinedFreq - originalFreq.
func PhaseRefineFreq(samples []float32, freqHz, dtSec float64) (refinedFreq float64, initialFit, finalFit PhaseFit, dfTotal float64, ok bool) {
	originalFreq := freqHz
	refinedFreq = freqHz

	initialFit = fitCostasPhase(samples, originalFreq, dtSec)
	if initialFit.AccessibleAnchors < 3 || math.IsInf(initialFit.RMSResid, 1) {
		finalFit = initialFit
		return originalFreq, initialFit, finalFit, 0, false
	}

	initialAccessibleWithWeight := 0
	for _, w := range initialFit.weights {
		if w > 0 {
			initialAccessibleWithWeight++
		}
	}

	currFit := initialFit

	for iter := 0; iter < phaseRefineMaxIter; iter++ {
		// Convert fit's DTFT-convention Slope into the
		// (f_signal - f_demod) misalignment via the bridge described
		// in the function doc. df > 0 ⇒ signal above current demod
		// ⇒ refinedFreq should move up to converge.
		df := slopeToFreqOffsetHz(currFit.Slope, refinedFreq)
		if math.Abs(df) < phaseRefineStopDfHz {
			break
		}
		if df > phaseRefineMaxStepHz {
			df = phaseRefineMaxStepHz
		} else if df < -phaseRefineMaxStepHz {
			df = -phaseRefineMaxStepHz
		}

		candidate := refinedFreq + df
		if math.Abs(candidate-originalFreq) > phaseRefineMaxCorrectionHz {
			break
		}

		nextFit := fitCostasPhase(samples, candidate, dtSec)
		if nextFit.AccessibleAnchors < 3 || math.IsInf(nextFit.RMSResid, 1) {
			break
		}

		refinedFreq = candidate
		currFit = nextFit
	}

	// Worsening guard: did refinement destroy signal coherence at
	// any anchor? Count positive-weight anchors before vs after.
	finalAccessibleWithWeight := 0
	for _, w := range currFit.weights {
		if w > 0 {
			finalAccessibleWithWeight++
		}
	}
	if finalAccessibleWithWeight < initialAccessibleWithWeight {
		// Refinement made things worse — roll back.
		finalFit = initialFit
		return originalFreq, initialFit, finalFit, 0, false
	}

	finalFit = currFit
	dfTotal = refinedFreq - originalFreq
	ok = true
	return refinedFreq, initialFit, finalFit, dfTotal, ok
}

// slopeToFreqOffsetHz converts fitCostasPhase's DTFT-arg Slope (which
// is 2π·f_signal·T_sym mod 2π — invariant under changes in f_demod)
// into the misalignment frequency (f_signal - f_demod) in Hz wanted
// for freq refinement. The bridge subtracts the demod-term
// contribution 2π·f_demod·T_sym (mod 2π) and wraps to (-π, π] so the
// result is in the (-baud/2, baud/2] = (-3.125, 3.125] Hz range —
// the unambiguous half-bin around the candidate.
//
// **Empirical sign convention** (pinned by TestPhaseRefineFreq_SignConvention,
// per operator's Session 90 directive — don't trust hand derivation;
// trust the test):
//
//	candidate BELOW true signal → output > 0 → refinement moves UP
//	candidate ABOVE true signal → output < 0 → refinement moves DOWN
//
// I.e., `refinedFreq += slopeToFreqOffsetHz(...)` always moves
// toward the true signal. The operator's Session 90 pseudocode
// wrote `df = -slope/(2π·T_sym)` (with a leading minus); the
// empirical sign here is positive after the demod-term bridge.
func slopeToFreqOffsetHz(slope, demodFreqHz float64) float64 {
	tSym := float64(nsps) / fs
	twoPiTSym := 2 * math.Pi * tSym
	demodTerm := twoPiTSym * demodFreqHz
	misalign := wrapPi(slope - demodTerm)
	return misalign / twoPiTSym
}
