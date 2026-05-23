package dsp

import (
	"math"
	"sort"
)

// Costas-array sync detection per QEX paper §4 ("Channel Symbols and
// Modulation"). Detects FT8 transmissions in a precomputed
// spectrogram and emits candidates (centre frequency + time offset
// + match score) for the downstream demodulator to process.
//
// **Spec-level inputs.** QEX paper §4 specifies:
//
//   - The 7-tone Costas array {3, 1, 4, 0, 6, 5, 2} (== Icos7).
//   - The array appears at three positions in each transmission:
//     channel symbols 0..6 (start), 36..42 (middle), 72..78 (end).
//   - FT8 uses 8-FSK modulation — 8 possible tones per symbol; the
//     Costas array uses 7 of those 8 tones at known positions.
//
// **Detection approach (clean-room — matched filter + SNR ratio).**
// Standard signal-processing concepts: a known signal template
// (Costas array) is correlated against the received spectrogram;
// the score at each candidate (freq, time) compares in-pattern
// power against an out-of-pattern noise reference from the same
// time slots. Both ideas predate FT8 by decades — matched filters
// are 1940s-era, Costas arrays are Costas 1984.
//
// For each candidate (centreBin, dtSteps):
//
//  1. Locate all 21 Costas symbol positions (3 arrays × 7 symbols)
//     in the spectrogram, given the candidate's time offset.
//  2. For each in-bounds Costas symbol: sum spectrogram power at the
//     expected tone over the 4 spectrogram time-steps that span the
//     symbol; do the same for the 6 other tones at the same time
//     slots (the out-of-pattern reference).
//  3. score = mean(in-pattern power) / mean(out-of-pattern power).
//     A score of 1 means signal equals noise; FT8 captures with
//     adequate sync typically produce scores from 1.5 to 50+
//     depending on SNR.
//  4. Reject candidates with score below opts.MinScore.
//
// **What this detector deliberately does NOT do** (vs. richer
// implementations): no separate scoring of the three Costas blocks,
// no percentile-based noise-floor normalisation, no narrow vs. wide
// time-lag search split, no operator-frequency priority placement.
// These are sensitivity tunings; we start simple and revisit if a
// real test corpus shows we need more.

// Costas-detection parameters. All chosen from spec-level reasoning
// — no reference to any specific decoder implementation.
const (
	// NumCostasBlocks is the count of Costas-array repetitions per
	// FT8 transmission (start, middle, end of slot). Three per QEX §4.
	NumCostasBlocks = 3

	// CostasTonesPerBlock is the length of one Costas array — 7
	// tones from Icos7, one per channel symbol position.
	CostasTonesPerBlock = 7

	// CostasBlockStrideSymbols is the channel-symbol offset between
	// successive Costas blocks. 36 symbols separate the start of
	// block N from the start of block N+1 (start at 0, middle at
	// 36, end at 72; the 7-symbol blocks themselves occupy 0..6,
	// 36..42, 72..78).
	CostasBlockStrideSymbols = 36

	// SpectrogramStepsPerSymbol is the spectrogram's temporal
	// oversampling factor — each channel symbol occupies 4
	// spectrogram time-steps (NSPS/NSTEP).
	SpectrogramStepsPerSymbol = NSPS / NSTEP // 4

	// SpectrogramOversampleFreq is the spectrogram's frequency
	// oversampling factor — the FFT bin spacing is half the symbol
	// rate (NFFT1 / NSPS = 2). Two spectrogram bins per FSK tone.
	SpectrogramOversampleFreq = NFFT1 / NSPS // 2

	// TonesPerSymbol is the FT8 modulation tone count: 8-FSK per
	// QEX §4. Indices 0..7. Costas symbols use 7 of these (indices
	// 0..6 per the Icos7 alphabet).
	TonesPerSymbol = 8

	// CostasToneCount is the alphabet size the Costas array draws
	// from (7 distinct tones). One Costas tone is in-pattern per
	// symbol; the remaining 6 are the noise reference.
	CostasToneCount = 7

	// nominalTXStartSeconds is the operational TX start within the
	// 15-second FT8 slot. The 15-s slot accommodates the 12.64-s
	// FT8 waveform (79 symbols × 160 ms) plus 2.36 s of timing
	// slack; convention across FT8 implementations places the TX
	// start 0.5 s into the slot so receivers searching ±2 s of
	// nominal cover both early and late operators. This is a
	// shared operational convention for receiver interop — not
	// strictly mandated by QEX paper §4, but every functional FT8
	// receiver assumes it.
	nominalTXStartSeconds = 0.5

	// SyncDefaultMinScore is the default rejection threshold.
	// Score < 1 means in-pattern power doesn't exceed noise — clear
	// noise. 1.5 is "comfortably above noise"; lower thresholds
	// surface more candidates at the cost of false positives.
	SyncDefaultMinScore = 1.5

	// SyncDefaultMaxCandidates is the default cap on returned
	// candidates. 100 is comfortable headroom for a busy band.
	SyncDefaultMaxCandidates = 100

	// SyncDefaultFreqLow / FreqHigh frame the FT8 working audio
	// passband. 200 Hz keeps us off DC; 2900 Hz keeps us off the
	// 3000 Hz upper edge of the standard audio passband.
	SyncDefaultFreqLow  = 200.0
	SyncDefaultFreqHigh = 2900.0

	// SyncDefaultSearchHalfSpanSec bounds the time-offset search
	// (one side of the symmetric ±range around nominal TX start)
	// when SyncOptions.SearchHalfSpanSec is zero. ±2.0 s covers the
	// usual operator-clock-drift envelope; widen to catch slow
	// transmitters at the cost of ~24%/step extra sync probes per
	// 0.5 s of widening. Tunable per call.
	SyncDefaultSearchHalfSpanSec = 2.0

	// syncDedupFreqBins is the frequency-proximity threshold for
	// near-dupe suppression in spectrogram bins (1 bin = 3.125 Hz).
	// Adjacent freq bins picking up the same actual transmission
	// get clustered; the strongest wins.
	syncDedupFreqBins = 1

	// syncDedupTimeSteps is the time-proximity threshold for
	// near-dupe suppression in spectrogram steps (1 step = 40 ms).
	// 2 steps = 80 ms — slightly more than the spectrogram's
	// natural temporal resolution.
	syncDedupTimeSteps = 2
)

// Candidate is a single FT8 sync detection.
type Candidate struct {
	// Freq is the candidate centre frequency in Hz. Quantised to
	// the spectrogram bin spacing (Fs/NFFT1 = 3.125 Hz).
	Freq float64

	// DT is the candidate's time offset in seconds relative to
	// the nominal TX start. Negative = early arrival, positive =
	// late arrival.
	DT float64

	// SyncPower is the matched-filter SNR score: mean in-pattern
	// Costas tone power divided by mean out-of-pattern tone power
	// over the same time slots. Values > 1 indicate signal above
	// noise; the rejection threshold is opts.MinScore.
	SyncPower float64
}

// ScoreVariant selects the per-candidate matched-filter scoring
// formula used by Sync. Different formulas have different sensitivity
// properties — see scoreCandidate (ratio) and scoreCandidateLocalPeak
// (local-peakness) for the per-variant rationale.
type ScoreVariant int

const (
	// ScoreVariantRatio (default) compares in-pattern Costas-tone
	// power to the mean of the other 6 Costas-alphabet tones at the
	// same time slots: score = mean(in) / mean(out). Self-normalises
	// against broadband noise.
	ScoreVariantRatio ScoreVariant = 0

	// ScoreVariantNormalised runs the same ratio scorer as
	// ScoreVariantRatio but then divides each cell's score by the
	// MEDIAN ratio score in its frequency-band neighbourhood (per-
	// freq-bin median across all time offsets in the search range).
	// Candidates rank by "how much do I stand out from my local
	// frequency band," not by absolute ratio. Addresses the precision
	// problem visible in the cap-displacement diagnostic: real weak
	// signals in clean parts of the band were being outranked by
	// noise candidates in louder parts of the band. The post-
	// normalisation score is dimensionless (still self-normalising
	// against broadband noise via the underlying ratio) AND adapted
	// to local band conditions.
	ScoreVariantNormalised ScoreVariant = 1
)

// SyncOptions configures the Costas-sync detector. Zero-valued
// fields use the SyncDefault* constants documented in this file.
type SyncOptions struct {
	FreqLow, FreqHigh float64
	MinScore          float64
	MaxCand           int

	// SearchHalfSpanSec bounds the symmetric time-offset search
	// around nominal TX start, in seconds. Zero → use
	// SyncDefaultSearchHalfSpanSec. Internally quantised to the
	// nearest spectrogram step (40 ms).
	SearchHalfSpanSec float64

	// ScoreVariant selects the matched-filter scoring formula. Zero
	// value = ScoreVariantRatio (current default, preserves existing
	// behaviour). See ScoreVariant constants.
	ScoreVariant ScoreVariant

	// Stats, when non-nil, is populated by Sync with stage-level
	// counts (raw above-threshold, post-dedup, post-cap). Useful for
	// the candidate-cap / retry-budget diagnostic work; zero-cost
	// when nil.
	Stats *SyncStats
}

// SyncStats holds per-call counts for the sync stage. All fields
// are populated by Sync when SyncOptions.Stats is non-nil.
type SyncStats struct {
	// RawAboveThreshold is the count of (freq, time) cells whose
	// matched-filter score met opts.MinScore — i.e. the raw count
	// BEFORE near-duplicate suppression.
	RawAboveThreshold int

	// AfterDedup is the count remaining after near-duplicate
	// suppression but BEFORE the MaxCand truncation.
	AfterDedup int

	// AfterCap is the final candidate count returned, equal to
	// min(AfterDedup, MaxCand).
	AfterCap int
}

// Sync detects FT8 sync candidates in a precomputed spectrogram.
// Returns candidates with SyncPower >= opts.MinScore, sorted by
// descending sync power, after near-duplicate suppression.
//
// Returns nil for malformed spectrograms or when no candidates
// exceed the threshold.
func Sync(spec [][]float64, opts SyncOptions) []Candidate {
	if !validSpec(spec) {
		return nil
	}

	if opts.FreqLow == 0 {
		opts.FreqLow = SyncDefaultFreqLow
	}
	if opts.FreqHigh == 0 {
		opts.FreqHigh = SyncDefaultFreqHigh
	}
	if opts.MinScore == 0 {
		opts.MinScore = SyncDefaultMinScore
	}
	if opts.MaxCand <= 0 {
		opts.MaxCand = SyncDefaultMaxCandidates
	}

	const (
		df    = Fs / float64(NFFT1) // 3.125 Hz per freq bin
		tstep = float64(NSTEP) / Fs // 0.04 s per spectrogram step
	)
	nominalStartStep := int(math.Floor(nominalTXStartSeconds / tstep))

	// Resolve the time-offset search half-span (seconds) → integer
	// spectrogram steps. Zero option value → default.
	halfSpanSec := opts.SearchHalfSpanSec
	if halfSpanSec <= 0 {
		halfSpanSec = SyncDefaultSearchHalfSpanSec
	}
	searchHalfSteps := int(math.Round(halfSpanSec / tstep))
	if searchHalfSteps < 1 {
		searchHalfSteps = 1
	}

	// Frequency bin range.
	binLow := int(math.Round(opts.FreqLow / df))
	if binLow < 0 {
		binLow = 0
	}
	// Costas tones land at centreBin + nfos * tone (tone in 0..6),
	// so the highest accessed bin is centreBin + nfos*6 = centreBin
	// + 12. Cap binHigh so the access stays in [0, NH1).
	binHigh := int(math.Round(opts.FreqHigh / df))
	if binHigh+SpectrogramOversampleFreq*(CostasToneCount-1) >= NH1 {
		binHigh = NH1 - SpectrogramOversampleFreq*(CostasToneCount-1) - 1
	}
	if binHigh < binLow {
		return nil
	}

	var rawCands []intCand
	switch opts.ScoreVariant {
	case ScoreVariantNormalised:
		rawCands = collectNormalisedCandidates(spec, binLow, binHigh, searchHalfSteps, nominalStartStep, opts.MinScore)
	default:
		for centreBin := binLow; centreBin <= binHigh; centreBin++ {
			for dtSteps := -searchHalfSteps; dtSteps <= searchHalfSteps; dtSteps++ {
				score := scoreCandidate(spec, centreBin, dtSteps, nominalStartStep)
				if score >= opts.MinScore {
					rawCands = append(rawCands, intCand{centreBin, dtSteps, score})
				}
			}
		}
	}

	if opts.Stats != nil {
		opts.Stats.RawAboveThreshold = len(rawCands)
	}

	if len(rawCands) == 0 {
		return nil
	}

	// Descending sort by score.
	sort.Slice(rawCands, func(i, j int) bool {
		return rawCands[i].score > rawCands[j].score
	})

	// Near-duplicate suppression in integer coordinate space. Walk
	// in descending-score order; for each survivor, mark all later
	// candidates within the dedup window as suppressed. Comparing
	// `abs(binA - binB) <= syncDedupFreqBins` is exact — no float
	// rounding can let boundary-case dupes through.
	keep := make([]bool, len(rawCands))
	for i := range keep {
		keep[i] = true
	}
	for i := 0; i < len(rawCands); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(rawCands); j++ {
			if !keep[j] {
				continue
			}
			if abs(rawCands[i].centreBin-rawCands[j].centreBin) <= syncDedupFreqBins &&
				abs(rawCands[i].dtSteps-rawCands[j].dtSteps) <= syncDedupTimeSteps {
				keep[j] = false
			}
		}
	}

	if opts.Stats != nil {
		dedupedCount := 0
		for _, k := range keep {
			if k {
				dedupedCount++
			}
		}
		opts.Stats.AfterDedup = dedupedCount
	}

	out := make([]Candidate, 0, opts.MaxCand)
	for i, rc := range rawCands {
		if !keep[i] {
			continue
		}
		out = append(out, Candidate{
			Freq:      float64(rc.centreBin) * df,
			DT:        float64(rc.dtSteps) * tstep,
			SyncPower: rc.score,
		})
		if len(out) >= opts.MaxCand {
			break
		}
	}
	if opts.Stats != nil {
		opts.Stats.AfterCap = len(out)
	}
	return out
}

// intCand is the internal candidate carrier used during sync's
// scoring + dedup pipeline. Integer bin / step coordinates let the
// dedup pass compare with exact thresholds (the public Candidate
// uses float Hz/s; conversion happens at the API edge).
type intCand struct {
	centreBin int
	dtSteps   int
	score     float64
}

// abs returns |x| for an int (no math.Abs for ints in stdlib).
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// scoreCandidate returns the matched-filter SNR ratio for a
// candidate at the given centre frequency bin and time offset
// (in spectrogram steps from the nominal TX start).
//
// Score = mean in-pattern Costas tone power / mean out-of-pattern
// tone power. The mean is over all in-bounds Costas symbols and
// their 4 spectrogram time-steps. Symbols whose time slot falls
// outside the spectrogram contribute zero to both sums (graceful
// degradation at the slot edges).
//
// Returns 0 if no in-pattern samples are in bounds (candidate is
// entirely outside the spectrogram).
func scoreCandidate(spec [][]float64, centreBin, dtSteps, nominalStartStep int) float64 {
	var inSum, noiseSum float64
	var inCount, noiseCount int

	// 21 Costas symbol positions: 3 blocks × 7 symbols.
	for block := 0; block < NumCostasBlocks; block++ {
		blockStartSym := block * CostasBlockStrideSymbols
		for symInBlock := 0; symInBlock < CostasTonesPerBlock; symInBlock++ {
			// Channel symbol index across the full transmission.
			channelSym := blockStartSym + symInBlock
			// Spectrogram time slot for this symbol (4 consecutive
			// steps).
			tStart := dtSteps + nominalStartStep + channelSym*SpectrogramStepsPerSymbol
			expectedTone := int(Icos7[symInBlock])
			inBin := centreBin + SpectrogramOversampleFreq*expectedTone

			// Accumulate over the 4 time-steps that cover this symbol.
			for step := 0; step < SpectrogramStepsPerSymbol; step++ {
				t := tStart + step
				if t < 0 || t >= NHSYM {
					continue
				}
				// In-pattern: power at the expected tone.
				inSum += spec[t][inBin]
				inCount++
				// Out-of-pattern: power at the other 6 Costas tones.
				for k := 0; k < CostasToneCount; k++ {
					if k == expectedTone {
						continue
					}
					otherBin := centreBin + SpectrogramOversampleFreq*k
					noiseSum += spec[t][otherBin]
					noiseCount++
				}
			}
		}
	}

	if inCount == 0 || noiseCount == 0 {
		return 0
	}
	inMean := inSum / float64(inCount)
	noiseMean := noiseSum / float64(noiseCount)
	if noiseMean <= 0 {
		return 0
	}
	return inMean / noiseMean
}

// collectNormalisedCandidates implements the ScoreVariantNormalised
// pipeline. Two-pass:
//
//  1. Score every (centreBin, dtSteps) cell with the ratio scorer
//     (same formula as ScoreVariantRatio).
//  2. For each freq bin, compute the MEDIAN raw score across all
//     time offsets in the search range — that's the "local baseline"
//     for that frequency. Then divide each cell's raw score by its
//     freq bin's baseline. Cells with normalised score >= MinScore
//     become candidates. The stored score IS the normalised value.
//
// Median (not mean) is used so a single strong real signal at a freq
// bin doesn't pump the baseline up and suppress weaker signals at
// the same freq. The median across ~100 time offsets is dominated by
// noise-floor cells regardless of a few signal-bearing peaks.
//
// Cost: scoring every cell is the same O(N) work the single-pass
// path does. Per-freq median is sort + middle pick (~log of search-
// span entries per bin). Total overhead is modest compared to BP/OSD
// downstream.
func collectNormalisedCandidates(spec [][]float64, binLow, binHigh, searchHalfSteps, nominalStartStep int, minScore float64) []intCand {
	width := binHigh - binLow + 1
	if width <= 0 {
		return nil
	}
	height := 2*searchHalfSteps + 1

	// Pass 1: raw scores indexed [centreBin-binLow][dtSteps+searchHalfSteps].
	rawScores := make([][]float64, width)
	for i := range rawScores {
		rawScores[i] = make([]float64, height)
	}
	for centreBin := binLow; centreBin <= binHigh; centreBin++ {
		row := rawScores[centreBin-binLow]
		for dtSteps := -searchHalfSteps; dtSteps <= searchHalfSteps; dtSteps++ {
			row[dtSteps+searchHalfSteps] = scoreCandidate(spec, centreBin, dtSteps, nominalStartStep)
		}
	}

	// Pass 2: per-freq-bin median (the local baseline). Sort a copy
	// of each row and take the middle element. Robust to a few
	// signal-bearing high scores within the bin's own time offsets.
	perBinMedian := make([]float64, width)
	scratch := make([]float64, height)
	for i, row := range rawScores {
		copy(scratch, row)
		sort.Float64s(scratch)
		perBinMedian[i] = scratch[height/2]
	}

	// Pass 2b: smooth the per-bin baseline over a ±baselineSmoothBins
	// frequency window (excluding the centre bin). A single bin's
	// median is itself a noisy estimator; the local-band noise floor
	// is more stable when averaged over the neighbourhood. Excluding
	// the centre means the bin's own (potentially signal-pumped)
	// median doesn't dominate its own baseline.
	const baselineSmoothBins = 16 // ±16 bins = ±50 Hz at 3.125 Hz/bin
	baseline := make([]float64, width)
	for i := 0; i < width; i++ {
		var sum float64
		var count int
		lo := i - baselineSmoothBins
		if lo < 0 {
			lo = 0
		}
		hi := i + baselineSmoothBins
		if hi >= width {
			hi = width - 1
		}
		for j := lo; j <= hi; j++ {
			if j == i {
				continue
			}
			sum += perBinMedian[j]
			count++
		}
		if count > 0 {
			baseline[i] = sum / float64(count)
		}
	}

	// Pass 3: normalise and threshold.
	var cands []intCand
	for binIdx := 0; binIdx < width; binIdx++ {
		b := baseline[binIdx]
		if b <= 0 {
			// Degenerate baseline (all-zero or negative scores in
			// this freq bin). Skip — normalisation isn't meaningful.
			continue
		}
		row := rawScores[binIdx]
		centreBin := binLow + binIdx
		for j, raw := range row {
			score := raw / b
			if score >= minScore {
				dtSteps := j - searchHalfSteps
				cands = append(cands, intCand{centreBin, dtSteps, score})
			}
		}
	}
	return cands
}

// validSpec checks the spectrogram has the expected dimensions
// [NHSYM][NH1]. Any irregularity returns false; Sync then returns nil.
func validSpec(spec [][]float64) bool {
	if len(spec) != NHSYM {
		return false
	}
	for _, row := range spec {
		if len(row) != NH1 {
			return false
		}
	}
	return true
}
