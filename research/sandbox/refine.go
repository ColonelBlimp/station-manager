package sandbox

import (
	"math"
)

// refineSymbolSamples is the baseband sample count per FT8 symbol when
// the channelizer is run at refineBandwidthHz bandwidth: 200 Hz complex
// × 0.160 s/symbol = 32 samples/symbol.
const (
	refineSymbolSamples = 32
	refineBandwidthHz   = 200.0
	refineToneSpacingHz = 6.25
)

// RefineOptions tunes RefineCandidate. Zero values fall back to
// DefaultRefineOptions.
type RefineOptions struct {
	// BandwidthHz is the channelizer bandwidth used for baseband
	// extraction. Default 200 Hz → 3200 baseband samples at 200 Hz
	// complex (32 samples per FT8 symbol).
	BandwidthHz float64

	// DTSweepRange / DTSweepStep set the step-2 timing sweep in
	// baseband samples (5 ms each at 200 Hz). Default ±10 samples
	// (±50 ms) in 1-sample steps — well clear of the ±20 ms worst-
	// case error from spectrogram frame-snapping.
	DTSweepRange int
	DTSweepStep  int

	// DFSweepRangeHz / DFSweepStepHz set the step-3 frequency sweep
	// in Hz. Default ±3 Hz in 0.1 Hz steps: half a spectrogram bin
	// (1.5625 Hz) for within-bin refinement plus one full extra bin
	// (3.125 Hz) of headroom to absorb the case where the coarse
	// scanner snapped to an adjacent bin under noise.
	DFSweepRangeHz float64
	DFSweepStepHz  float64

	// FinalDTSweepRange is the step-6 timing sweep on the re-
	// channelized baseband. Default ±5 samples (±25 ms) in 1-sample
	// steps — picks up any residual sub-sample drift the freq shift
	// introduces.
	FinalDTSweepRange int
}

// DefaultRefineOptions returns the baseline tuning. Step-2 timing
// sweep covers ±50 ms, step-3 freq sweep covers ±3 Hz, step-6 timing
// sweep covers ±25 ms.
func DefaultRefineOptions() RefineOptions {
	return RefineOptions{
		BandwidthHz:       200.0,
		DTSweepRange:      10,
		DTSweepStep:       1,
		DFSweepRangeHz:    3.0,
		DFSweepStepHz:     0.1,
		FinalDTSweepRange: 5,
	}
}

// RefineCandidate runs the six-step refinement protocol on a coarse
// candidate from FindCandidates:
//
//  1. Channelize at the coarse freq.
//  2. Sweep dt across ±DTSweepRange baseband samples and pick the
//     highest Costas matched-filter score.
//  3. Sweep df across ±DFSweepRangeHz Hz at the best dt and pick the
//     highest score.
//  4. Update the candidate's freq to coarse + best df.
//  5. Re-channelize at the refined freq.
//  6. Sweep dt across ±FinalDTSweepRange on the re-channelized
//     baseband for the final timing lock.
//
// The supplied Channelizer must already have had Prepare called on
// the source audio — RefineCandidate calls Extract twice (steps 1
// and 5) against that cached spectrum.
//
// On return the Candidate has FreqHz/DtSec updated to the refined
// values, Sync set to the post-refinement baseband matched-filter
// score (different scale from the coarse Sync — not directly
// comparable), Refined=true. Coarse* fields are preserved.
func RefineCandidate(ch *Channelizer, c Candidate, opts RefineOptions) (Candidate, error) {
	opts = applyRefineOptionDefaults(opts)
	bbRate := opts.BandwidthHz

	// Step 1: channelize at coarse freq.
	bb, err := ch.Extract(c.CoarseFreqHz, opts.BandwidthHz)
	if err != nil {
		return c, err
	}

	// Coarse dt as a baseband sample index. The channelizer's baseband
	// shares the audio's time origin (the freq-domain slice doesn't
	// shift time), so buffer-time × baseband rate gives the matching
	// baseband sample index. CoarseDtSec is measured from the WSJT-X
	// nominal slot start (0.5 s into the buffer), so the buffer-time
	// equivalent is CoarseDtSec + nominalStartSec.
	dt0 := int(math.Round((c.CoarseDtSec + nominalStartSec) * bbRate))

	// Step 2: sweep dt around dt0 at df=0.
	zeroDFCarriers := buildToneCarriers(0, bbRate, refineSymbolSamples)
	bestDT := dt0
	bestScore := -1.0
	for delta := -opts.DTSweepRange; delta <= opts.DTSweepRange; delta += opts.DTSweepStep {
		s := costasBasebandScore(bb, dt0+delta, zeroDFCarriers)
		if s > bestScore {
			bestScore = s
			bestDT = dt0 + delta
		}
	}

	// Step 3: sweep df at bestDT.
	bestDF := 0.0
	bestScore = -1.0
	dfSteps := int(opts.DFSweepRangeHz/opts.DFSweepStepHz + 0.5)
	for i := -dfSteps; i <= dfSteps; i++ {
		df := float64(i) * opts.DFSweepStepHz
		c_df := buildToneCarriers(df, bbRate, refineSymbolSamples)
		s := costasBasebandScore(bb, bestDT, c_df)
		if s > bestScore {
			bestScore = s
			bestDF = df
		}
	}

	// Step 4: update freq.
	refinedFreq := c.CoarseFreqHz + bestDF

	// Step 5: re-channelize at the refined freq. Same bandwidth →
	// same baseband sample rate → the dt index from step 2/3 remains
	// valid in the new baseband.
	bb2, err := ch.Extract(refinedFreq, opts.BandwidthHz)
	if err != nil {
		return c, err
	}

	// Step 6: final small dt sweep on the re-channelized baseband.
	// After the freq shift, the residual phase across symbols is gone
	// and the timing lock should be tight — a ±5-sample window is
	// enough to absorb any drift the freq update introduced.
	finalDT := bestDT
	finalScore := costasBasebandScore(bb2, bestDT, zeroDFCarriers)
	for delta := -opts.FinalDTSweepRange; delta <= opts.FinalDTSweepRange; delta++ {
		if delta == 0 {
			continue // already scored
		}
		s := costasBasebandScore(bb2, bestDT+delta, zeroDFCarriers)
		if s > finalScore {
			finalScore = s
			finalDT = bestDT + delta
		}
	}

	c.FreqHz = refinedFreq
	c.DtSec = float64(finalDT)/bbRate - nominalStartSec
	c.Sync = finalScore
	c.Refined = true
	return c, nil
}

// buildToneCarriers precomputes the eight conjugated-carrier tables
// used by the Costas matched filter:
//
//	carrier[m][s] = exp(-i·2π·(m·6.25 + dfHz)·s / bbRate)
//
// The conjugate is baked in so the per-symbol correlation in
// costasBasebandScore is a plain complex multiply (no Conj() per
// inner iteration). Length per table is symbolSamples; m runs the
// eight FT8 tones 0..7.
func buildToneCarriers(dfHz, bbRate float64, symbolSamples int) [8][]complex128 {
	var carriers [8][]complex128
	for m := 0; m < 8; m++ {
		tab := make([]complex128, symbolSamples)
		toneHz := float64(m)*refineToneSpacingHz + dfHz
		for s := 0; s < symbolSamples; s++ {
			phase := -2 * math.Pi * toneHz * float64(s) / bbRate
			tab[s] = complex(math.Cos(phase), math.Sin(phase))
		}
		carriers[m] = tab
	}
	return carriers
}

// costasBasebandScore evaluates the incoherent Costas matched-filter
// score at the given baseband dt offset, using pre-built per-tone
// carrier tables (which already encode the freq offset for this
// trial). Returns Σ |Σ_s bb[symStart+s] · carrier[tone(k)][s]|²
// across the 21 Costas anchor symbols.
//
// Coherent within a symbol (the 32 samples accumulate as a complex
// inner product); incoherent across symbols (per-symbol magnitudes
// sum), so the result is invariant to a global phase that varies
// across symbols — which is exactly what we want, since each Costas
// symbol's phase relative to the previous is unconstrained.
func costasBasebandScore(bb []complex128, dtSample int, carriers [8][]complex128) float64 {
	score := 0.0
	for _, blockStart := range costasBlockStarts {
		for k := 0; k < 7; k++ {
			symStart := dtSample + (blockStart+k)*refineSymbolSamples
			if symStart < 0 || symStart+refineSymbolSamples > len(bb) {
				continue
			}
			tab := carriers[costasArray[k]]
			var sumRe, sumIm float64
			for s := 0; s < refineSymbolSamples; s++ {
				v := bb[symStart+s]
				c := tab[s]
				sumRe += real(v)*real(c) - imag(v)*imag(c)
				sumIm += real(v)*imag(c) + imag(v)*real(c)
			}
			score += sumRe*sumRe + sumIm*sumIm
		}
	}
	return score
}

func applyRefineOptionDefaults(opts RefineOptions) RefineOptions {
	d := DefaultRefineOptions()
	if opts.BandwidthHz == 0 {
		opts.BandwidthHz = d.BandwidthHz
	}
	if opts.DTSweepRange == 0 {
		opts.DTSweepRange = d.DTSweepRange
	}
	if opts.DTSweepStep == 0 {
		opts.DTSweepStep = d.DTSweepStep
	}
	if opts.DFSweepRangeHz == 0 {
		opts.DFSweepRangeHz = d.DFSweepRangeHz
	}
	if opts.DFSweepStepHz == 0 {
		opts.DFSweepStepHz = d.DFSweepStepHz
	}
	if opts.FinalDTSweepRange == 0 {
		opts.FinalDTSweepRange = d.FinalDTSweepRange
	}
	return opts
}
