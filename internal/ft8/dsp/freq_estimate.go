package dsp

import "math"

// anchorDtftSkipSamples is the half-width (in samples) of the
// Gaussian transition zone at each symbol boundary that is EXCLUDED
// from the per-anchor DTFT in EstimateFreqOffsetCostas. The GFSK
// transition at the boundaries carries intermediate frequencies that
// would bias the estimate; skipping ~3σ on each side (3·sqrt(ln 2)/
// (2π·BT)·NSPS = ~127 samples · 3 ≈ 380 samples) keeps the
// integration to the symbol's steady-state plateau. ~1160 samples
// per anchor × 21 anchors is plenty of integration length even at
// threshold SNR.
const anchorDtftSkipSamples = 380

// EstimateFreqOffsetCostas estimates the residual frequency offset
// (in Hz) of an FT8 signal whose nominal carrier is at candFreq and
// whose nominal slot-relative TX start is at dt seconds. The estimate
// is derived from the 21 Costas anchor symbols — known tones at known
// positions — and is independent of the data payload.
//
// **Why this exists.** The sync detector quantises candidate
// frequencies to the spectrogram's 3.125 Hz bin grid (Fs/NFFT1); a
// real signal sits at candFreq ± up to half the bin spacing
// (~1.56 Hz residual). For matched-filter signal SUBTRACTION
// (dsp.SubtractSignal) that residual offset accumulates ~6 complete
// phase rotations across the 12.64-second TX window, breaking the
// constant-phase assumption the matched filter relies on. Refining
// the candidate's frequency before synthesising the template fixes
// that.
//
// **Algorithm — non-coherent brute-force energy peak.** Sweep a
// candidate offset Δf over ±SearchHalfSpanHz at SearchStepHz
// resolution. At each step, compute the total spectral energy at the
// 21 Costas-anchor tone bins under the hypothesis that the actual
// signal sits at candFreq + Δf:
//
//	E(Δf) = Σ_{p=0..20} |Σ_n audio[txStart+rel_p+n]·exp(-i·2π·f_target_p·rel_p+n/Fs)|²
//
// where f_target_p = candFreq + Δf + Icos7[p%7]·Baud and rel_p+n
// ranges over the symbol-p anchor's steady-state plateau (skipping
// the Gaussian transition zones at each end). Find the Δf that
// maximises E; parabolic-interpolate across the peak's three samples
// for sub-step precision.
//
// **Why non-coherent + brute-force, not phase-coherent.** A previous
// pair-product implementation that summed y_{p+1}·conj(y_p) across
// adjacent Costas symbols (NSPS samples apart) worked perfectly on
// clean synthetic signals (<0.001 Hz error) but failed catastrophically
// on real HF captures, biasing the estimate to the ±π wrap boundary
// (~±2.8 Hz on signals whose true offset was a fraction of a Hz).
// Per-block diagnostics showed pair-sum magnitudes 50-100× weaker
// than the corresponding clean-synth case, with only ~3-4× of that
// reduction explained by signal-amplitude attenuation. The remaining
// 5-10× extra reduction is structural: multipath fading + Doppler
// + adjacent-signal contamination destroys phase coherence between
// Costas symbols 160 ms apart, so the pair-products from different
// adjacencies don't align into a coherent sum and the residual is
// dominated by whichever few pairs happen to have the largest
// magnitude — producing wrap-boundary noise. A non-coherent energy
// peak finder makes no phase-coherence assumption and works directly
// from amplitude, which is robust to those effects.
//
// Returns 0 if the audio is too short to cover the TX window, if
// txStart would land before sample 0, or if no measurable energy is
// present at any tested Δf (all-zero audio, off-band candidate).
func EstimateFreqOffsetCostas(audio []float32, candFreq, dt float64) float64 {
	txStart := int(math.Round((SynthSlotStartSeconds + dt) * Fs))
	if txStart < 0 {
		return 0
	}
	if txStart+NN*NSPS > len(audio) {
		return 0
	}

	// Search range covers the sync-grid's worst-case ±1.56 Hz residual
	// plus headroom. SearchStepHz=0.1 Hz with parabolic interpolation
	// gives ~0.01 Hz precision at the peak.
	const (
		searchHalfSpanHz = 2.0
		searchStepHz     = 0.1
	)
	nSteps := int(math.Round(2*searchHalfSpanHz/searchStepHz)) + 1

	energies := make([]float64, nSteps)
	for step := 0; step < nSteps; step++ {
		deltaF := -searchHalfSpanHz + float64(step)*searchStepHz
		energies[step] = costasAnchorEnergy(audio, txStart, candFreq+deltaF)
	}

	// Find the discrete peak.
	peakStep := 0
	peakEnergy := energies[0]
	for i, e := range energies {
		if e > peakEnergy {
			peakStep = i
			peakEnergy = e
		}
	}
	if peakEnergy < 1e-12 {
		return 0
	}

	deltaF := -searchHalfSpanHz + float64(peakStep)*searchStepHz

	// Parabolic interpolation around the peak for sub-step precision.
	// Skip if peak landed at an endpoint (no left or right neighbour).
	if peakStep > 0 && peakStep < nSteps-1 {
		y0 := energies[peakStep-1]
		y1 := energies[peakStep]
		y2 := energies[peakStep+1]
		denom := y0 - 2*y1 + y2
		if denom < -1e-12 { // peak is a true maximum (denom < 0)
			offset := 0.5 * (y0 - y2) / denom
			// Clamp the parabolic offset to ±0.5 step — beyond that
			// the parabolic model has broken down (peak isn't between
			// the three samples we picked).
			if offset > 0.5 {
				offset = 0.5
			} else if offset < -0.5 {
				offset = -0.5
			}
			deltaF += offset * searchStepHz
		}
	}

	return deltaF
}

// costasAnchorEnergy sums |y_p|² across the 21 Costas anchors at the
// supplied baseband centre frequency. Each y_p is the DTFT of the
// anchor's steady-state samples evaluated at the anchor's expected
// tone position (refinedCandFreq + Icos7[symInBlock]·Baud).
//
// Demod kernel is referenced to TX start (rel = symStartRel + n, NOT
// absolute sample index). See EstimateFreqOffsetCostas's doc comment
// for the derivation — referencing to absolute m would inject a
// tone-change-dependent phase bias that fails to vanish modulo 2π
// when txStart isn't an integer multiple of NSPS.
func costasAnchorEnergy(audio []float32, txStart int, refinedCandFreq float64) float64 {
	const twoPiOverFs = 2.0 * math.Pi / Fs

	var totalEnergy float64
	for block := 0; block < NumCostasBlocks; block++ {
		blockStartSym := block * CostasBlockStrideSymbols
		for symInBlock := 0; symInBlock < CostasTonesPerBlock; symInBlock++ {
			channelSym := blockStartSym + symInBlock
			symStartRel := channelSym * NSPS
			fTarget := refinedCandFreq + float64(Icos7[symInBlock])*Baud

			var re, im float64
			for n := anchorDtftSkipSamples; n < NSPS-anchorDtftSkipSamples; n++ {
				rel := symStartRel + n
				x := float64(audio[txStart+rel])
				phase := twoPiOverFs * fTarget * float64(rel)
				s, c := math.Sincos(phase)
				re += x * c
				im -= x * s
			}
			totalEnergy += re*re + im*im
		}
	}
	return totalEnergy
}
