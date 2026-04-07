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

import "cmp"
import "slices"

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

	// minSyncScore is the coarse threshold for candidate acceptance.
	// Candidates with a score below this are discarded. The score
	// represents the excess mean power at sync positions above the
	// mean power across all tone positions — a dimensionless ratio in
	// the power-spectrum domain.
	//
	// This value may need empirical tuning against real FT8 recordings.
	minSyncScore = 1.0
)

// FindCandidates searches the spectrogram for FT8 signals by correlating
// against the known Costas synchronisation pattern.
//
// The spectrogram is a [nFrames][nBins]float32 power matrix as produced by
// [Spectrogram]. Each row corresponds to one symbol period; each column is
// a frequency bin.
//
// Returns candidates sorted by descending score, up to maxCandidates.
// Returns nil if the spectrogram is too small to contain a full FT8 message
// (needs at least [NumSymbols] time frames and [NumTones] frequency bins),
// or if maxCandidates ≤ 0.
func FindCandidates(spectrogram [][]float32, maxCandidates int) []Candidate {
	if len(spectrogram) == 0 || maxCandidates <= 0 {
		return nil
	}

	nFrames := len(spectrogram)
	nBins := len(spectrogram[0])

	// Need at least 79 frames for a complete FT8 message.
	if nFrames < NumSymbols {
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

	// Convert frequency search bounds to bin indices.
	minBin := int(minSearchFreqHz / float64(binWidth))
	maxBin := int(maxSearchFreqHz/float64(binWidth)) + 1
	if minBin < 0 {
		minBin = 0
	}
	// The tone grid spans baseBin..baseBin+7, so the highest valid baseBin
	// is nBins − NumTones.
	if maxBin > nBins-NumTones {
		maxBin = nBins - NumTones
	}
	if minBin > maxBin {
		return nil
	}

	// Maximum time offset: the message needs NumSymbols consecutive frames.
	maxTimeOff := nFrames - NumSymbols

	var candidates []Candidate

	for t := 0; t <= maxTimeOff; t++ {
		for b := minBin; b <= maxBin; b++ {
			score := syncScore(spectrogram, t, b)
			if score > minSyncScore {
				candidates = append(candidates, Candidate{
					Freq:    float32(b) * binWidth,
					TimeOff: float32(t) * SymbolPeriod,
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

// syncScore computes the Costas sync correlation score for a candidate at
// the given time offset and base frequency bin.
//
// The score is the mean power at the 21 sync tone positions minus the mean
// power across all 79 × 8 tone positions. A strong FT8 signal produces a
// positive score because the sync tones carry concentrated power at
// predictable frequency/time positions, while data symbols spread their
// energy across the tone grid quasi-randomly.
func syncScore(sg [][]float32, timeOff, baseBin int) float32 {
	// Sum power at the 21 sync positions (3 blocks × 7 Costas symbols).
	var syncPower float32
	for _, start := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			syncPower += sg[timeOff+start+j][baseBin+int(CostasSync[j])]
		}
	}

	// Sum total power across all 79 symbols × 8 tones.
	var totalPower float32
	for s := range NumSymbols {
		row := sg[timeOff+s]
		for tone := range NumTones {
			totalPower += row[baseBin+tone]
		}
	}

	// Score = mean sync power − mean total power.
	return syncPower/float32(NumSyncSyms) - totalPower/float32(NumSymbols*NumTones)
}
