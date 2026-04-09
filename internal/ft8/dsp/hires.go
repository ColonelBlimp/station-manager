// hires.go — high-resolution candidate detection and refinement.
//
// This file provides frequency-oversampled equivalents of the functions in
// candidates.go. The standard [FindCandidates] and [RefineCandidateAudio]
// work with spectrograms at the native FFT resolution (~5.86 Hz/bin). The
// functions here operate on the wider spectrograms produced by
// [SpectrogramFT8HiRes] (~2.93 Hz/bin with freqOSR=2) and use ft8_lib's
// neighbor-comparison sync scoring for more robust candidate ranking.
//
// The coarse-fine two-pass refinement grid ([RefineCandidateAudioFast])
// replaces the flat 33×49 grid of [RefineCandidateAudio] with a 96+25
// point search, reducing Goertzel evaluations ~12× per candidate.
//
// These functions are used by [ProcessWindowMultiPass]; the original
// functions are retained for backward compatibility and tests.
//
// Reference: ft8_lib decode.c ft8_sync_score (lines 61–125),
// ftx_find_candidates (lines 190–250).

package dsp

import (
	"cmp"
	"math"
	"slices"
)

// FreqOSR is the default frequency oversampling rate used by
// [SpectrogramFT8HiRes] and [FindCandidatesHiRes]. ft8_lib uses 2.
const FreqOSR = 2

// FindCandidatesHiRes searches a frequency-oversampled spectrogram for FT8
// signals using ft8_lib's neighbor-comparison sync scoring.
//
// Unlike [FindCandidates], which assumes 1 FFT bin per tone, this function
// computes the bins-per-tone ratio from the spectrogram dimensions and the
// known tone spacing, then uses [syncScoreNeighbor] for robust local-contrast
// scoring.
//
// Parameters:
//   - sg: spectrogram from [SpectrogramFT8HiRes] (or [SpectrogramFT8] with
//     freqOSR=1). All rows must have the same length.
//   - maxCandidates: upper limit on returned candidates.
//   - stepsPerSymbol: spectrogram rows per FT8 symbol (2 for half-symbol step).
//
// Returns candidates sorted by descending score, up to maxCandidates.
func FindCandidatesHiRes(sg [][]float32, maxCandidates int, stepsPerSymbol int) []Candidate {
	if len(sg) == 0 || maxCandidates <= 0 || stepsPerSymbol <= 0 {
		return nil
	}

	nFrames := len(sg)
	nBins := len(sg[0])

	minFrames := (NumSymbols-1)*stepsPerSymbol + 1
	if nFrames < minFrames {
		return nil
	}
	if nBins < NumTones {
		return nil
	}

	// Derive bin width from the spectrogram dimensions.
	fftSize := 2 * (nBins - 1) // e.g., 4096 for 2049 bins
	binWidth := float64(SampleRate) / float64(fftSize)

	// Bins per tone: ToneSpacing / binWidth. For a 4096-point FFT at 12 kHz:
	// 6.25 / 2.929688 ≈ 2.133.
	binsPerTone := ToneSpacing / binWidth

	// Maximum bin offset from baseBin for tone 7.
	maxToneOffset := int(math.Round(float64(NumTones-1) * binsPerTone))

	// Convert time offset from frame index to seconds.
	stepDuration := float32(SamplesPerSymbol) / float32(stepsPerSymbol) / float32(SampleRate)

	// Frequency search bounds in bin indices.
	minBin := int(minSearchFreqHz / binWidth)
	maxBin := int(maxSearchFreqHz/binWidth) + 1
	if maxBin > nBins-maxToneOffset-1 {
		maxBin = nBins - maxToneOffset - 1
	}
	if minBin > maxBin {
		return nil
	}

	maxTimeOff := nFrames - minFrames

	// Use the log2 threshold (the hi-res path always uses log2-power spectrograms).
	threshold := float32(minSyncScoreLog2)

	var candidates []Candidate

	for t := 0; t <= maxTimeOff; t++ {
		for b := minBin; b <= maxBin; b++ {
			score := syncScoreNeighbor(sg, t, b, stepsPerSymbol, binsPerTone)
			if score > threshold {
				candidates = append(candidates, Candidate{
					Freq:    float32(float64(b) * binWidth),
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

	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	return candidates
}

// syncScoreNeighbor computes the Costas sync score using ft8_lib's
// neighbor-comparison method (decode.c lines 61–125).
//
// Instead of comparing sync-tone power against the mean of all 8 tones
// (as [syncScoreSteps] does), this function compares each sync tone's power
// against its immediate neighbors in both frequency and time:
//
//   - Frequency neighbors: one tone below and above (when not at edge tones)
//   - Time neighbors: previous and next symbol within the sync block
//
// This local-contrast method is more robust against spectral slope and
// broadband interference, and better discriminates weak signals from noise.
//
// The binsPerTone parameter maps tone indices to spectrogram bin offsets
// (e.g., ~2.133 for a 4096-point FFT at 12 kHz).
func syncScoreNeighbor(sg [][]float32, timeOff, baseBin, stepsPerSymbol int, binsPerTone float64) float32 {
	var score float32
	var numAvg int

	syncStarts := [3]int{Sync1Start, Sync2Start, Sync3Start}

	for _, start := range syncStarts {
		for k := range SyncLen {
			symIdx := start + k
			row := timeOff + symIdx*stepsPerSymbol

			// Bounds check.
			if row < 0 || row >= len(sg) {
				continue
			}

			sm := int(CostasSync[k])
			smBin := baseBin + int(math.Round(float64(sm)*binsPerTone))

			// Bounds check on bin.
			if smBin < 0 || smBin >= len(sg[row]) {
				continue
			}

			syncVal := sg[row][smBin]

			// Frequency neighbor below.
			if sm > 0 {
				lowerBin := baseBin + int(math.Round(float64(sm-1)*binsPerTone))
				if lowerBin >= 0 && lowerBin < len(sg[row]) {
					score += syncVal - sg[row][lowerBin]
					numAvg++
				}
			}

			// Frequency neighbor above.
			if sm < NumTones-1 {
				upperBin := baseBin + int(math.Round(float64(sm+1)*binsPerTone))
				if upperBin >= 0 && upperBin < len(sg[row]) {
					score += syncVal - sg[row][upperBin]
					numAvg++
				}
			}

			// Time neighbor before (previous symbol in this sync block).
			if k > 0 {
				prevRow := row - stepsPerSymbol
				if prevRow >= 0 && prevRow < len(sg) {
					score += syncVal - sg[prevRow][smBin]
					numAvg++
				}
			}

			// Time neighbor after (next symbol in this sync block).
			if k+1 < SyncLen {
				nextRow := row + stepsPerSymbol
				if nextRow >= 0 && nextRow < len(sg) {
					score += syncVal - sg[nextRow][smBin]
					numAvg++
				}
			}
		}
	}

	if numAvg > 0 {
		score /= float32(numAvg)
	}
	return score
}

// Coarse-fine refinement grid parameters for [RefineCandidateAudioFast].
const (
	// coarseTimePoints and coarseFreqPoints define the coarse grid.
	// 8 time × 12 freq ≈ 96 evaluation points.
	coarseTimePoints = 8
	coarseFreqPoints = 12

	// fineGridSize defines the fine grid around the coarse best: 5×5 = 25
	// evaluation points.
	fineGridSize = 5
)

// RefineCandidateAudioFast fine-tunes a candidate's time offset and frequency
// using a coarse-to-fine two-pass grid search, replacing the flat 33×49 grid
// of [RefineCandidateAudio].
//
// Pass 1 (coarse): 8 time × 12 freq ≈ 96 points with wide steps.
// Pass 2 (fine):   5 × 5 = 25 points centered on the coarse optimum.
//
// Total evaluations: ~121 per candidate vs. ~1,617 for the flat grid — a
// ~13× reduction in Goertzel calls. Each evaluation uses [refineSyncScore]
// (21 Goertzel calls), so total Goertzel calls drop from ~34k to ~2.5k
// per candidate.
//
// After finding the best grid position, the full normalised score is
// computed once via [syncScoreAudio] for SNR estimation.
func RefineCandidateAudioFast(samples []float32, hann []float32, cand Candidate) Candidate {
	if len(hann) < SamplesPerSymbol {
		return cand
	}

	coarseStart := int(math.Round(float64(cand.TimeOff) * SampleRate))
	coarseFreq := float64(cand.Freq)

	halfRange := SamplesPerSymbol // ±1 full symbol period
	timeRange := 2 * halfRange    // total range in samples
	freqRange := 2 * refineFreqRange

	// Coarse step sizes.
	coarseTimeStep := timeRange / coarseTimePoints
	if coarseTimeStep < 1 {
		coarseTimeStep = 1
	}
	coarseFreqStep := freqRange / float64(coarseFreqPoints)
	if coarseFreqStep < refineFreqStep {
		coarseFreqStep = refineFreqStep
	}

	bestStart := coarseStart
	bestFreq := coarseFreq
	bestScore := math.Inf(-1)
	found := false

	// --- Coarse pass ---
	for dt := -halfRange; dt <= halfRange; dt += coarseTimeStep {
		startSample := coarseStart + dt
		if startSample < 0 {
			continue
		}
		if startSample+NumSymbols*SamplesPerSymbol > len(samples) {
			continue
		}

		for df := -refineFreqRange; df <= refineFreqRange; df += coarseFreqStep {
			freq := coarseFreq + df
			if freq < minSearchFreqHz || freq > maxSearchFreqHz {
				continue
			}

			score := refineSyncScore(samples, hann, startSample, freq)
			if score > bestScore {
				bestScore = score
				bestStart = startSample
				bestFreq = freq
				found = true
			}
		}
	}

	if !found {
		return cand
	}

	// --- Fine pass around the coarse optimum ---
	fineTimeStep := coarseTimeStep / (fineGridSize - 1)
	if fineTimeStep < 1 {
		fineTimeStep = 1
	}
	fineFreqStep := coarseFreqStep / float64(fineGridSize-1)
	if fineFreqStep < refineFreqStep/2 {
		fineFreqStep = refineFreqStep / 2
	}

	fineTimeRange := coarseTimeStep // ±1 coarse step
	fineFreqRange := coarseFreqStep

	coarseBestStart := bestStart
	coarseBestFreq := bestFreq

	for dt := -fineTimeRange; dt <= fineTimeRange; dt += fineTimeStep {
		startSample := coarseBestStart + dt
		if startSample < 0 {
			continue
		}
		if startSample+NumSymbols*SamplesPerSymbol > len(samples) {
			continue
		}

		for df := -fineFreqRange; df <= fineFreqRange; df += fineFreqStep {
			freq := coarseBestFreq + df
			if freq < minSearchFreqHz || freq > maxSearchFreqHz {
				continue
			}

			score := refineSyncScore(samples, hann, startSample, freq)
			if score > bestScore {
				bestScore = score
				bestStart = startSample
				bestFreq = freq
			}
		}
	}

	// Compute the full normalised sync score for SNR estimation.
	finalScore := syncScoreAudio(samples, hann, bestStart, bestFreq)

	return Candidate{
		Freq:    float32(bestFreq),
		TimeOff: float32(bestStart) / float32(SampleRate),
		Score:   float32(finalScore),
	}
}
