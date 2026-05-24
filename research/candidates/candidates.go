// Package candidates is the research-stage candidate finder. Find
// takes a 15-second 12 kHz mono FT8 audio buffer and returns detected
// sync candidates. The implementation is the subject of the new
// approach we're exploring; the CLI at research/cmd/find-candidates
// wires the harness around it.
//
// Imports stdlib + internal/audio only — by rule the research tree
// must not depend on internal/ft8/*. FT8 protocol constants are
// re-declared in this file.
package candidates

import (
	"math"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// FT8 protocol parameters per the QEX 2020 paper §4. Replicated here
// because the research tree is firewalled from internal/ft8/*.
const (
	fs    = 12000.0   // sample rate
	nsps  = 1920      // samples per symbol (160 ms at fs)
	nn    = 79        // total channel symbols per transmission
	baud  = fs / nsps // 6.25 Hz — symbol rate and 8-FSK tone spacing
	nfft1 = 2 * nsps  // 3840-point spectrogram FFT → 3.125 Hz bins (freq oversampled 2×)
	nstep = nsps / 4  // 480-sample column step → 40 ms (time oversampled 4×)

	// Costas-array layout per QEX paper §4 — three blocks of seven
	// known-tone symbols at fixed channel-symbol positions 0..6,
	// 36..42, 72..78.
	numCostasBlocks       = 3
	costasSymbolsPerBlock = 7  // symbols per Costas block (== len(icos7))
	costasBlockStride     = 36 // channel-symbol stride between block starts

	// ft8ToneCount is the 8-FSK alphabet size (tones 0..7). Costas
	// anchors use only tones 0..6 (the icos7 alphabet); tone 7
	// appears only in data symbols, so at any Costas anchor position
	// tone 7's bin is guaranteed-silent of in-pattern energy — useful
	// for the matched filter's noise reference.
	ft8ToneCount = 8

	// Derived from the spectrogram shape: how many time-steps span
	// one symbol, and how many freq bins span one tone.
	stepsPerSymbol = nsps / nstep // 4
	freqOversample = nfft1 / nsps // 2

	// maxToneIdx is the largest FT8 tone index the matched filter
	// touches. Used by Find to keep centreBin + freqOversample*maxToneIdx
	// inside the spectrogram and by costasScore's bounds guard.
	maxToneIdx = ft8ToneCount - 1 // 7

	// expectedInSamples is the total in-pattern (centre, time-step)
	// cell count when every Costas anchor sits inside the spectrogram:
	// 3 blocks × 7 anchors × 4 time-steps each.
	expectedInSamples = numCostasBlocks * costasSymbolsPerBlock * stepsPerSymbol // 84

	// minInSamples is the floor under which costasScore refuses to
	// trust the in-pattern / out-of-pattern ratio — partial-block
	// matches near the slot edges can pump out high scores from very
	// few in-bounds cells. 50% of the full anchor grid is a
	// conservative cut that still admits this package's worst-case
	// in-search-range coverage (dtSteps = ±50 → 56 samples = 67%).
	minInSamples = expectedInSamples / 2 // 42
)

// icos7 is the FT8 7-tone Costas synchronisation pattern. The same
// sequence appears three times in each transmission: channel symbols
// 0-6, 36-42, 72-78.
var icos7 = [7]uint8{3, 1, 4, 0, 6, 5, 2}

// Candidate is one detected FT8 sync hit: where in the slot (Freq,
// DT), how strong (Score, the stage-1 matched-filter ratio), and
// — when stage-2 verification has run — the Costas-pattern
// verification record.
type Candidate struct {
	// Freq is the candidate centre frequency in Hz. Quantised to the
	// spectrogram bin spacing (fs / nfft1 = 3.125 Hz).
	Freq float64

	// DT is the time offset in seconds relative to the nominal 0.5 s
	// TX start, in the physical TX frame — i.e. the stage-1 raw DT
	// has had dtPhysicalOffsetSec added so a physically-on-time
	// signal reports DT = 0. Negative = early arrival, positive = late.
	DT float64

	// Score is the stage-1 matched-filter SNR ratio: mean in-pattern
	// Costas tone power / mean out-of-pattern tone power. Used as
	// the final tie-break in the stage-2 ranking.
	Score float64

	// Verify, when non-nil, carries the stage-2 Costas-pattern
	// verification record for this candidate. Find populates this
	// for every returned candidate.
	Verify *CostasVerify
}

// Stage-1 generator settings: a very low sanity floor and a generous
// topK cap. The threshold is just above the matched-filter noise
// floor (ratio = 1.0 means signal energy equals out-of-pattern
// energy); anything below is uncorrelated with the Costas pattern by
// construction. stage1TopK caps how many candidates feed into the
// stage-2 verifier — sized loose enough that all in-band signals
// down to the FT8 sensitivity floor pass through.
//
// Why 10000: at -22 dB on a 10-CQ fixture the weakest real has
// s1=1.406; the surrounding noise produces several thousand other
// candidates with s1 >= 1.0. A topK of 500 (the earlier value)
// silently cuts marginal reals out of the verifier queue. 10000 is
// effectively "verify everything above the sanity floor" for our
// test corpus; the verifier itself is the precision lever.
const (
	stage1ScoreThreshold = 1.0   // matched-filter ratio floor (sanity only)
	stage1TopK           = 10000 // verifier accepts at most this many per slot
)

// Stage-2 gate: categorical-only. WinsTotal + WinsPerBlock catch
// pattern-incoherent junk; the GeoContrast metric is retained as a
// RANKING signal (see the sort key below) rather than as a hard
// floor. The previous hard floor at GeoContrast >= 0.85 was
// non-monotonic with SNR: it rejected real -22 dB signals (geo
// ~0.81) while admitting strong random aliases at clean (geo
// 1.24). A floor that drifts the wrong way across operating
// conditions is worse than no floor — rely on ranking + topK to
// surface real signals and let downstream demod/LDPC reject any
// FPs that survive into the output tail.
const (
	sanityWinsTotal    = 8 // minimum WinsTotal out of 21 anchors
	sanityWinsPerBlock = 1 // minimum WinsBlock[b] out of 7 anchors each
)

// Final cap after ranking + NMS.
const stage2MaxResults = 100

// Find returns verified sync candidates detected in a 15-second FT8
// audio slot. Input must be 12 kHz mono float32 samples covering
// the full slot (180,000 samples). Every returned candidate has a
// non-nil Verify field.
//
// Pipeline:
//
//  1. Spectrogram (rectangular window, 3840-point real FFT, 480-sample step).
//  2. Stage-1 sweep over (centreBin, dtSteps): pooled-power Costas
//     matched-filter score from costasScore. NO non-max suppression
//     at this stage — sympathetic peaks survive into stage 2.
//  3. DT correction: each stage-1 DT is shifted by dtPhysicalOffsetSec
//     so downstream coordinates are in the physical TX frame.
//  4. Top-stage1TopK by stage-1 score cap going into stage 2.
//  5. Stage-2 verification: per-symbol DTFT (Goertzel-batched 8-tone)
//     at each Costas anchor → win counts + log contrast.
//  6. Categorical gate: WinsTotal ≥ sanityWinsTotal AND every
//     WinsBlock[b] ≥ sanityWinsPerBlock. GeoContrast steers the
//     ranking, not the gate.
//  7. Ranked tie-break: GeoContrast desc → WinsTotal desc →
//     MinBlockContrast desc → Stage1Score desc.
//  8. Final non-max suppression on physical (Freq, DT).
//  9. Cap to stage2MaxResults.
func Find(samples []float32) []Candidate {
	if len(samples) < nn*nsps {
		return nil
	}
	spec := spectrogram(samples)
	if len(spec) == 0 {
		return nil
	}

	const (
		freqLowHz       = 200.0
		freqHighHz      = 2900.0
		searchHalfSpanS = 2.0

		df    = fs / nfft1          // 3.125 Hz per freq bin
		tstep = float64(nstep) / fs // 0.04 s per time column
	)

	nominalStartStep := int(math.Floor(0.5 / tstep))

	// Compute raw dt-step search bounds so the PHYSICAL DT range is
	// symmetric at ±searchHalfSpanS. The matched-filter peak sits
	// dtPhysicalOffsetSteps steps below physical zero (see
	// verify.go for the structural reason), so the raw loop bounds
	// must shift by that amount or the physical range becomes
	// asymmetric (e.g. [-1.88, +2.12] s for the previous symmetric
	// raw [-50, +50] loop).
	halfSpanSteps := int(math.Round(searchHalfSpanS / tstep))
	dtStepsMin := -halfSpanSteps - dtPhysicalOffsetSteps // raw step at physical -searchHalfSpanS
	dtStepsMax := halfSpanSteps - dtPhysicalOffsetSteps  // raw step at physical +searchHalfSpanS

	halfFFT := len(spec[0])
	binLow := int(math.Round(freqLowHz / df))
	if binLow < 0 {
		binLow = 0
	}
	binHigh := int(math.Round(freqHighHz / df))
	if binHigh+freqOversample*maxToneIdx >= halfFFT {
		binHigh = halfFFT - freqOversample*maxToneIdx - 1
	}
	if binHigh < binLow {
		return nil
	}

	// ---- Stage 1: spectrogram sweep, no NMS. ----
	var stage1 []Candidate
	for centreBin := binLow; centreBin <= binHigh; centreBin++ {
		for dtSteps := dtStepsMin; dtSteps <= dtStepsMax; dtSteps++ {
			score := costasScore(spec, centreBin, dtSteps, nominalStartStep)
			if score >= stage1ScoreThreshold {
				stage1 = append(stage1, Candidate{
					Freq:  float64(centreBin) * df,
					DT:    float64(dtSteps)*tstep + dtPhysicalOffsetSec,
					Score: score,
				})
			}
		}
	}
	if len(stage1) == 0 {
		return nil
	}

	// Cap stage-1 set going into verification, descending by stage-1 score.
	sort.Slice(stage1, func(i, j int) bool {
		return stage1[i].Score > stage1[j].Score
	})
	if len(stage1) > stage1TopK {
		stage1 = stage1[:stage1TopK]
	}

	// ---- Stage 2: per-anchor Costas verification. ----
	verified := make([]Candidate, 0, len(stage1))
	for _, c := range stage1 {
		v := verifyCostas(samples, c.Freq, c.DT, c.Score)
		// Categorical gate only. Pattern-incoherent junk gets cut
		// here; GeoContrast steers the ranking (sort key below)
		// rather than acting as a hard floor.
		if v.WinsTotal < sanityWinsTotal {
			continue
		}
		blockOk := true
		for b := 0; b < numCostasBlocks; b++ {
			if v.WinsBlock[b] < sanityWinsPerBlock {
				blockOk = false
				break
			}
		}
		if !blockOk {
			continue
		}
		vCopy := v
		c.Verify = &vCopy
		verified = append(verified, c)
	}
	if len(verified) == 0 {
		return nil
	}

	// ---- Rank by verifier metrics. ----
	sort.Slice(verified, func(i, j int) bool {
		a, b := verified[i].Verify, verified[j].Verify
		if a.GeoContrast != b.GeoContrast {
			return a.GeoContrast > b.GeoContrast
		}
		if a.WinsTotal != b.WinsTotal {
			return a.WinsTotal > b.WinsTotal
		}
		if a.MinBlockContrast != b.MinBlockContrast {
			return a.MinBlockContrast > b.MinBlockContrast
		}
		return a.Stage1Score > b.Stage1Score
	})

	// ---- Final NMS on physical (Freq, DT). ----
	//
	// Time suppression at 3 spectrogram steps (= 120 ms) rather than
	// 2 — the half-symbol structural alias of a real signal sits at
	// exactly 2 steps offset and would otherwise survive NMS via
	// float-precision noise at the ≤ 2·tstep boundary. 3 steps
	// cleanly catches it without affecting legitimate signals
	// (operator clock drift between FT8 stations is ±5-10 ms, never
	// approaching 120 ms).
	const (
		freqSuppressHz = 2 * df    // ±2 bins (= 6.25 Hz)
		timeSuppressS  = 3 * tstep // ±3 steps (= 120 ms)
	)
	keep := make([]bool, len(verified))
	for i := range keep {
		keep[i] = true
	}
	for i := 0; i < len(verified); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(verified); j++ {
			if !keep[j] {
				continue
			}
			if math.Abs(verified[i].Freq-verified[j].Freq) <= freqSuppressHz &&
				math.Abs(verified[i].DT-verified[j].DT) <= timeSuppressS {
				keep[j] = false
			}
		}
	}

	out := make([]Candidate, 0, stage2MaxResults)
	for i, c := range verified {
		if !keep[i] {
			continue
		}
		out = append(out, c)
		if len(out) >= stage2MaxResults {
			break
		}
	}
	return out
}

// spectrogram returns spec[t][f] = |X[t][f]|² for time columns at
// nstep-sample intervals and frequency bins [0, nfft1/2). The
// Nyquist bin (X[nfft1/2]) is intentionally dropped — for our FT8
// use case the candidate sweep tops out at 2900 Hz (bin ~928 of
// 1920) so the Nyquist bin at 6000 Hz carries no signal of interest.
// Matches the NH1 = NFFT1/2 convention used by SM's dsp.Sync.
//
// Input contract: callers must supply at least nfft1 samples.
// `candidates.Find` already pre-validates len(samples) >= nn*nsps
// before calling, so nCols ≥ 1 is guaranteed in production use; the
// nil return below is a defensive fallback for direct callers.
//
// Windowing: rectangular (no window). Sidelobe leakage at -13 dB
// from the strongest bin is acceptable for FT8 Costas matched-filter
// scoring because the other six Costas tones live two bins away and
// don't sit inside the first sidelobe. If false positives from
// sidelobe leakage ever become an issue, swap in a Hann/Hamming
// window here.
//
// Allocation shape: spec rows back onto one contiguous slice, so the
// 367 × 1920 = ~706k float64 cells live in a single heap object
// instead of 367 separate ones — better cache locality, fewer GC
// roots. The per-call plan.FFT(chunk) still allocates internally
// (two slices per call; see audio.RealPlan); we don't fix that here.
func spectrogram(samples []float32) [][]float64 {
	plan := audio.NewRealPlan(nfft1)
	halfFFT := nfft1 / 2

	nCols := 0
	for s := 0; s+nfft1 <= len(samples); s += nstep {
		nCols++
	}
	if nCols == 0 {
		return nil
	}

	backing := make([]float64, nCols*halfFFT)
	spec := make([][]float64, nCols)
	chunk := make([]float32, nfft1)
	// chunk is reused across all nCols calls. Safe because each
	// iteration's copy() fills exactly nfft1 samples — no stale
	// tail leaks into a later FFT. If we ever allow partial
	// zero-padded final frames, clear chunk's tail before that call.
	for t := 0; t < nCols; t++ {
		copy(chunk, samples[t*nstep:t*nstep+nfft1])
		X := plan.FFT(chunk)
		row := backing[t*halfFFT : (t+1)*halfFFT]
		for f := 0; f < halfFFT; f++ {
			re, im := real(X[f]), imag(X[f])
			row[f] = re*re + im*im
		}
		spec[t] = row
	}
	return spec
}

// costasScore returns the matched-filter SNR ratio for one candidate
// at (centreBin, dtSteps): power averaged across the 21 in-bounds
// Costas anchor (symbol, tone) positions, divided by power averaged
// across the corresponding out-of-pattern (symbol, other-tone)
// positions at the same time slots. The "other" tones span all 8
// FT8 8-FSK alphabet tones except the anchor's expected one — at
// Costas anchor positions tones outside the icos7 set (in particular
// tone 7) are guaranteed no in-pattern energy, giving a cleaner
// noise reference than restricting to icos7 alone.
//
// **Input contract.** spec must have at least one row; every row
// must have len ≥ centreBin + freqOversample*maxToneIdx + 1. centreBin
// must be ≥ 0. Find enforces these via its binLow/binHigh bracketing;
// the bounds guard below is defensive for any other caller.
//
// **Slot-edge handling.** Anchor cells whose time slot falls outside
// the spectrogram are skipped — graceful degradation at the slot
// edges. Below the minInSamples coverage floor the function returns
// 0 to prevent a tiny number of in-bounds cells from producing a
// high-but-meaningless ratio.
func costasScore(spec [][]float64, centreBin, dtSteps, nominalStartStep int) float64 {
	if len(spec) == 0 || len(spec[0]) == 0 {
		return 0
	}
	if centreBin < 0 {
		return 0
	}
	if centreBin+freqOversample*maxToneIdx >= len(spec[0]) {
		return 0
	}

	nCols := len(spec)
	var inSum, noiseSum float64
	var inCount, noiseCount int

	for block := 0; block < numCostasBlocks; block++ {
		blockStartSym := block * costasBlockStride
		for sym := 0; sym < costasSymbolsPerBlock; sym++ {
			channelSym := blockStartSym + sym
			tStart := dtSteps + nominalStartStep + channelSym*stepsPerSymbol
			expectedTone := int(icos7[sym])
			inBin := centreBin + freqOversample*expectedTone

			for step := 0; step < stepsPerSymbol; step++ {
				t := tStart + step
				if t < 0 || t >= nCols {
					continue
				}
				row := spec[t]
				inSum += row[inBin]
				inCount++
				for k := 0; k < ft8ToneCount; k++ {
					if k == expectedTone {
						continue
					}
					otherBin := centreBin + freqOversample*k
					noiseSum += row[otherBin]
					noiseCount++
				}
			}
		}
	}

	if inCount < minInSamples || noiseCount == 0 {
		return 0
	}
	inMean := inSum / float64(inCount)
	noiseMean := noiseSum / float64(noiseCount)
	if noiseMean <= 0 {
		return 0
	}
	return inMean / noiseMean
}
