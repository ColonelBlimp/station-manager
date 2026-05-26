package demod

import "math"

// EstimateCostasCalibration measures per-candidate signal and noise
// levels directly from the 21 Costas anchors at (freqHz, dtSec).
// Provides a cleaner LLR-scale source than estimating noise from
// data-symbol loser tones, since the Costas anchors are KNOWN-tone
// reference points — uncontaminated by the candidate's own unknown
// data-symbol modulation.
//
// Returns:
//   - signalLevel: mean of |X(expected_tone)|² across all accessible
//     anchors. Each anchor's expected tone is icos7[i mod 7].
//   - noiseLevel: mean of |X(non_expected_tone)|² across the same
//     anchors. 7 non-expected tones × (≤21) accessible anchors → up
//     to 147 samples in the average.
//
// Both are in the same units as Demod's output (squared-magnitude
// power). Inaccessible anchors (audio window falls outside the buffer
// for slot-edge candidates) are silently skipped — same convention as
// fitCostasPhase in this package.
//
// When fewer than 3 anchors are accessible, returns (0, 0) — caller
// should fall back to the data-symbol-derived noise estimate
// (estimateNoise via LLRs) for slot-edge candidates.
//
// The signal level is returned but not consumed by LLRsCalibrated;
// it is exposed for diagnostic logging and potential future per-
// symbol reliability weighting (operator directive 2026-05-26:
// deferred to follow-up patch).
func EstimateCostasCalibration(samples []float32, freqHz, dtSec float64) (signalLevel, noiseLevel float64) {
	var coeffs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		fk := freqHz + float64(k)*baud
		coeffs[k] = 2 * math.Cos(2*math.Pi*fk/fs)
	}

	txStart := int(math.Round((synthSlotStartSec + dtSec) * fs))

	var sigSum, noiseSum float64
	sigCount, noiseCount := 0, 0

	for i := 0; i < costasAnchors; i++ {
		sym := costasSym[i]
		expectedTone := int(costasExpectedTone[i])
		symStart := txStart + sym*nsps
		if symStart < 0 || symStart+nsps > len(samples) {
			continue
		}
		// Real-valued Goertzel — same kernel demod uses for incoherent
		// energy extraction. Returns [8]float64 of |X|².
		energies := goertzelMulti(samples, symStart, nsps, coeffs)
		sigSum += energies[expectedTone]
		sigCount++
		for t := 0; t < ft8ToneCount; t++ {
			if t == expectedTone {
				continue
			}
			noiseSum += energies[t]
			noiseCount++
		}
	}

	if sigCount < 3 || noiseCount == 0 {
		return 0, 0
	}
	return sigSum / float64(sigCount), noiseSum / float64(noiseCount)
}
