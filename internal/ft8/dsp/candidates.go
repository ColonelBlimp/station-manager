// candidates.go — FT8 signal candidate detection via Costas sync correlation.
//
// An FT8 message occupies 79 symbol periods in the spectrogram. Three
// 7-symbol Costas sync blocks (positions 0–6, 36–42, 72–78) carry a known
// tone pattern {3, 1, 4, 0, 6, 5, 2}. A candidate detector scans all
// plausible (time-offset, base-frequency) positions and scores each by how
// much power the Costas sync tones carry relative to the average across all
// tone positions.
//
// FFT bin spacing vs. FT8 tone spacing: with a 2048-point FFT at 12 kHz,
// the bin width is 12000/2048 = 5.859375 Hz, while FT8 tone spacing is
// 6.25 Hz. The ratio is 16/15 ≈ 1.067 bins per tone. For tone indices 0–7
// the maximum fractional offset is 7×(1/15) ≈ 0.47 bins, which rounds to
// zero — so baseBin + toneIndex is a valid nearest-bin approximation for
// all 8 tones. Sub-bin frequency refinement can be added later if needed.
//
// Reference: Franke, S. & Taylor, J., "The FT4 and FT8 Communication
// Protocols", QEX July/August 2020.

package dsp

import (
	"cmp"
	"math"
	"slices"
)

// Candidate represents a detected FT8 signal candidate in the spectrogram.
type Candidate struct {
	Freq    float32 // base audio frequency (Hz)
	TimeOff float32 // time offset within the window (seconds)
	Score   float32 // sync correlation strength (higher = better)
}

// Search bounds for the FT8 audio passband.
const (
	minSearchFreqHz = 200.0  // lower edge of the search range (Hz)
	maxSearchFreqHz = 3000.0 // upper edge of the search range (Hz)

	// minSyncScoreLog2 is the coarse threshold for candidate acceptance
	// when the spectrogram uses log2-power representation (as in
	// [SpectrogramFT8]). A score of ~1.5 in log2 domain means the sync
	// tones are ~2^1.5 ≈ 2.83× stronger than the average — matching
	// ft8_lib's FT8_SYNC_MIN_SCORE.
	minSyncScoreLog2 = 1.5

	// minSyncScoreLinear is the threshold for linear-power spectrograms,
	// kept for backward compatibility with existing tests.
	minSyncScoreLinear = 1.0
)

// FindCandidates searches the spectrogram for FT8 signals by correlating
// against the known Costas synchronisation pattern.
//
// The spectrogram is a [nFrames][nBins]float32 power matrix as produced by
// [Spectrogram] or [SpectrogramFT8]. All rows must have the same length
// (uniform matrix); passing a jagged slice causes undefined behaviour.
//
// stepsPerSymbol indicates how many spectrogram rows span one FT8 symbol
// period:
//   - 1 for non-overlapping spectrograms (step = symbol period)
//   - 2 for half-symbol overlap (step = symbol period / 2), matching ft8_lib
//
// Returns candidates sorted by descending score, up to maxCandidates.
// Returns nil if the spectrogram is too small to contain a full FT8 message,
// or if maxCandidates ≤ 0.
func FindCandidates(spectrogram [][]float32, maxCandidates int, stepsPerSymbol int) []Candidate {
	if len(spectrogram) == 0 || maxCandidates <= 0 || stepsPerSymbol <= 0 {
		return nil
	}

	nFrames := len(spectrogram)
	nBins := len(spectrogram[0])

	// Need enough frames for 79 symbols at the given step resolution.
	minFrames := (NumSymbols-1)*stepsPerSymbol + 1
	if nFrames < minFrames {
		return nil
	}
	// Need at least 8 frequency bins for the 8-FSK tone grid.
	if nBins < NumTones {
		return nil
	}

	// Derive bin width from the spectrogram dimensions.
	// nBins = fftSize/2 + 1, so fftSize = 2*(nBins-1).
	fftSize := 2 * (nBins - 1)
	binWidth := float32(SampleRate) / float32(fftSize)

	// Convert time offset from frame index to seconds.
	stepDuration := float32(SamplesPerSymbol) / float32(stepsPerSymbol) / float32(SampleRate)

	// Use the appropriate sync score threshold.
	threshold := float32(minSyncScoreLinear)
	if stepsPerSymbol > 1 {
		// Half-symbol stepping implies log2-power spectrogram.
		threshold = minSyncScoreLog2
	}

	// Convert frequency search bounds to bin indices.
	minBin := int(minSearchFreqHz / float64(binWidth))
	maxBin := int(maxSearchFreqHz/float64(binWidth)) + 1
	// The tone grid spans baseBin..baseBin+7, so the highest valid baseBin
	// is nBins − NumTones.
	if maxBin > nBins-NumTones {
		maxBin = nBins - NumTones
	}
	if minBin > maxBin {
		return nil
	}

	// Maximum time offset: the message needs (NumSymbols-1)*stepsPerSymbol+1
	// consecutive frames from the start position.
	maxTimeOff := nFrames - minFrames

	var candidates []Candidate

	for t := 0; t <= maxTimeOff; t++ {
		for b := minBin; b <= maxBin; b++ {
			score := syncScoreSteps(spectrogram, t, b, stepsPerSymbol)
			if score > threshold {
				candidates = append(candidates, Candidate{
					Freq:    float32(b) * binWidth,
					TimeOff: float32(t) * stepDuration,
					Score:   score,
				})
			}
		}
	}

	// Sort by descending score.
	slices.SortFunc(candidates, func(a, b Candidate) int {
		return cmp.Compare(b.Score, a.Score)
	})

	// Truncate to the requested maximum.
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	return candidates
}

// syncScoreSteps computes the Costas sync correlation score for a candidate
// at the given time offset and base frequency bin, accounting for the number
// of spectrogram rows per symbol.
//
// When stepsPerSymbol=1, each row is one symbol (no overlap); when
// stepsPerSymbol=2, each row is a half-symbol (50% overlap). Symbol k is
// at row timeOff + k*stepsPerSymbol.
//
// The score is the mean power at the 21 sync tone positions minus the mean
// power across all 79 × 8 tone positions.
func syncScoreSteps(sg [][]float32, timeOff, baseBin, stepsPerSymbol int) float32 {
	// Sum power at the 21 sync positions (3 blocks × 7 Costas symbols).
	var syncPower float32
	for _, start := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			row := timeOff + (start+j)*stepsPerSymbol
			syncPower += sg[row][baseBin+int(CostasSync[j])]
		}
	}

	// Sum total power across all 79 symbols × 8 tones.
	var totalPower float32
	for s := range NumSymbols {
		row := sg[timeOff+s*stepsPerSymbol]
		for tone := range NumTones {
			totalPower += row[baseBin+tone]
		}
	}

	// Score = mean sync power − mean total power.
	return syncPower/float32(NumSyncSyms) - totalPower/float32(NumSymbols*NumTones)
}

// syncScore is a convenience wrapper for syncScoreSteps with stepsPerSymbol=1
// (non-overlapping spectrogram).
func syncScore(sg [][]float32, timeOff, baseBin int) float32 {
	return syncScoreSteps(sg, timeOff, baseBin, 1)
}

// Refinement search parameters for [RefineCandidateAudio].
const (
	// refineTimeSteps is the number of sub-symbol time offsets to evaluate.
	// The symbol period is divided into this many steps, giving a time
	// resolution of SamplesPerSymbol / refineTimeSteps samples (120 samples
	// = 10 ms for the default value of 16).
	refineTimeSteps = 16

	// refineFreqRange is the half-width of the frequency search window (Hz).
	// Widened from 3.0 to accommodate the bin mismatch between 2048-point
	// FFT (5.86 Hz bins) and 6.25 Hz tone spacing — coarse frequency
	// estimates can be off by up to half a bin (≈3 Hz) or more.
	refineFreqRange = 6.0

	// refineFreqStep is the frequency search step size (Hz).
	refineFreqStep = 0.25
)

// RefineCandidateAudio fine-tunes a candidate's time offset and frequency
// by maximising the Costas sync correlation computed via the Goertzel
// algorithm on raw audio samples.
//
// The coarse candidate (from [FindCandidates]) has its frequency quantised
// to FFT bin boundaries and its time offset quantised to the spectrogram
// step size. This function searches a small grid around the coarse position:
//   - Time: ±1 symbol period in [refineTimeSteps] steps
//   - Frequency: ±[refineFreqRange] Hz in [refineFreqStep] Hz steps
//
// The candidate with the highest sync score is returned. If the input
// samples are too short for any search position, the original candidate is
// returned unchanged.
func RefineCandidateAudio(samples []float32, hann []float32, cand Candidate) Candidate {
	if len(hann) < SamplesPerSymbol {
		return cand
	}

	// Convert coarse candidate position to sample offset.
	coarseStart := int(math.Round(float64(cand.TimeOff) * SampleRate))
	coarseFreq := float64(cand.Freq)

	best := cand
	bestScore := float64(math.Inf(-1))

	// Time search: sweep sub-symbol offsets around the coarse position.
	timeStep := SamplesPerSymbol / refineTimeSteps
	halfRange := SamplesPerSymbol // ±1 full symbol period

	for dt := -halfRange; dt <= halfRange; dt += timeStep {
		startSample := coarseStart + dt
		if startSample < 0 {
			continue
		}
		// Check that the full 79-symbol span fits.
		endSample := startSample + NumSymbols*SamplesPerSymbol
		if endSample > len(samples) {
			continue
		}

		// Frequency search: sweep sub-Hz offsets around the coarse frequency.
		for df := -refineFreqRange; df <= refineFreqRange; df += refineFreqStep {
			freq := coarseFreq + df
			if freq < minSearchFreqHz || freq > maxSearchFreqHz {
				continue
			}

			score := syncScoreAudio(samples, hann, startSample, freq)
			if score > bestScore {
				bestScore = score
				best = Candidate{
					Freq:    float32(freq),
					TimeOff: float32(startSample) / float32(SampleRate),
					Score:   float32(score),
				}
			}
		}
	}

	return best
}

// syncScoreAudio computes the Costas sync correlation score using the
// Goertzel algorithm on raw audio samples.
//
// The score is computed the same way as [syncScoreSteps]:
//
//	meanSyncPower − meanTotalPower
//
// where meanSyncPower is the average Goertzel power at the 21 known Costas
// sync tone positions, and meanTotalPower is the average across all 8 tones
// at those same 21 symbol positions (168 Goertzel evaluations total).
//
// Subtracting the mean total power removes the noise/spectral-shape bias,
// making the score comparable across frequencies and suitable for both
// refinement and SNR estimation.
func syncScoreAudio(samples []float32, hann []float32, startSample int, baseFreq float64) float64 {
	var syncPower float64
	var totalPower float64
	for _, blockStart := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			symStart := startSample + (blockStart+j)*SamplesPerSymbol
			if symStart < 0 || symStart+SamplesPerSymbol > len(samples) {
				return math.Inf(-1)
			}
			frame := samples[symStart : symStart+SamplesPerSymbol]

			// Compute power at all 8 tones for this sync symbol.
			syncTone := int(CostasSync[j])
			for tone := range NumTones {
				toneFreq := baseFreq + float64(tone)*ToneSpacing
				power := Goertzel(frame, hann, toneFreq)
				totalPower += power
				if tone == syncTone {
					syncPower += power
				}
			}
		}
	}

	// Score = mean sync power − mean total power (matches syncScoreSteps).
	return syncPower/float64(NumSyncSyms) - totalPower/float64(NumSyncSyms*NumTones)
}
