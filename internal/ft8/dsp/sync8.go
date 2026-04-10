// sync8.go — WSJT-X-faithful sync8 candidate detection for FT8.
//
// Faithful port of WSJT-X lib/ft8/sync8.f90. This replaces the
// neighbor-comparison scoring in hires.go with the ratio-metric approach
// used by WSJT-X for more robust candidate detection of weak signals.
//
// Key differences from the previous [FindCandidatesHiRes] approach:
//
//   - Linear-power spectrogram (not log2-power) — used only internally
//     for sync scoring.
//   - Rectangular window (scale by 1/300) — no Hann window, matching WSJT-X.
//   - 1920-sample analysis window, zero-padded to 3840 FFT — giving exactly
//     2 bins per FT8 tone (nfos=2), matching WSJT-X NFFT1.
//   - Ratio-metric sync scoring: syncPower / meanNonSyncPower, summing
//     only 7 tones (0–6), not all 8.
//   - Dual sync modes: sync_abc (all 3 Costas blocks) and sync_bc (blocks
//     2+3 only, for late-arriving signals), taking the maximum.
//   - 40th-percentile baseline normalization.
//   - Near-dupe suppression: |Δf| < 4 Hz and |Δt| < 40 ms.
//   - Both narrow (±10 lags ≈ ±0.4 s) and wide (±62 lags ≈ ±2.5 s)
//     time search ranges per frequency bin.
//
// Reference: WSJT-X lib/ft8/sync8.f90, lib/ft8/ft8_params.f90.

package dsp

import (
	"cmp"
	"math"
	"slices"
)

// sync8 algorithm constants matching WSJT-X ft8_params.f90 and sync8.f90.
const (
	sync8NFFT       = 2 * SamplesPerSymbol         // 3840 — FFT size for sync spectrogram
	sync8NH1        = sync8NFFT / 2                // 1920 — number of positive-frequency bins (excl. DC)
	sync8Step       = SamplesPerSymbol / 4         // 480 — spectrogram step size (NSTEP)
	sync8Nfos       = sync8NFFT / SamplesPerSymbol // 2 — bins per FT8 tone
	sync8Nssy       = SamplesPerSymbol / sync8Step // 4 — spectrogram steps per symbol
	sync8JZ         = 62                           // time search half-range: ±2.5 seconds
	sync8MLag       = 10                           // narrow time search half-range: ±10 lags
	sync8MaxPreCand = 1000                         // max pre-dedup candidates

	// DefaultSyncMin is the default sync score threshold after normalization.
	// Matches WSJT-X ft8_decode.f90 syncmin=1.3 for ndepth=3.
	DefaultSyncMin = 1.3
)

// Sync8Result holds the output of [Sync8FindCandidates].
type Sync8Result struct {
	Candidates []Candidate
	NoiseFloor float64 // mean linear power across the search band, for SNR estimation
}

// Sync8FindCandidates detects FT8 signal candidates using WSJT-X's sync8
// algorithm: ratio-metric sync scoring on linear-power spectrograms with
// 40th-percentile normalization and near-dupe suppression.
//
// This function computes its own spectrogram internally (1920-sample
// rectangular window, 3840-point FFT, 480-sample step, linear power)
// matching WSJT-X sync8.f90 exactly.
//
// Parameters:
//   - samples: audio capture buffer (one FT8 window, typically 180000 samples)
//   - syncMin: minimum normalized sync score (typically [DefaultSyncMin] = 1.3)
//   - maxCandidates: upper limit on returned candidates
//
// Returns candidates sorted by descending score, deduplicated.
func Sync8FindCandidates(samples []float32, syncMin float64, maxCandidates int) Sync8Result {
	if len(samples) < SamplesPerSymbol || maxCandidates <= 0 {
		return Sync8Result{}
	}

	// Number of spectrogram frames: NHSYM = NMAX/NSTEP - 3.
	// Equivalent to (len - NSPS) / NSTEP + 1 for standard buffer sizes.
	nhsym := (len(samples)-SamplesPerSymbol)/sync8Step + 1
	if nhsym < sync8Nssy*NumSymbols {
		return Sync8Result{}
	}

	tstep := float64(sync8Step) / float64(SampleRate) // 0.04 s
	df := float64(SampleRate) / float64(sync8NFFT)    // 3.125 Hz

	// jstrt: starting frame offset for xdt=0 signal (0.5 s into capture).
	// Fortran implicit integer: int(0.5/0.04) = 12.
	jstrt := int(0.5 / tstep) // 12

	// --- Step 1: Compute linear-power spectrogram ---
	// Matching sync8.f90 lines 28–43: no window function, scale by 1/300,
	// zero-pad to NFFT1, real-to-complex FFT, linear power |cx|².
	//
	// s[frame][bin] — linear power, bins 0..NH1 (bin 0 = DC).
	// Frame j uses audio samples [j*NSTEP .. j*NSTEP + NSPS - 1].
	fac := 1.0 / 300.0
	s := make([][]float64, nhsym)
	for j := range nhsym {
		ia := j * sync8Step
		// Build complex input: scale samples, zero-pad.
		x := make([]complex128, sync8NFFT)
		end := ia + SamplesPerSymbol
		if end > len(samples) {
			end = len(samples)
		}
		for i := ia; i < end; i++ {
			x[i-ia] = complex(fac*float64(samples[i]), 0)
		}

		generalDFT(x)

		row := make([]float64, sync8NH1+1)
		for i := range sync8NH1 + 1 {
			r, im := real(x[i]), imag(x[i])
			row[i] = r*r + im*im
		}
		s[j] = row
	}

	// --- Step 2: Compute sync scores ---
	// Frequency search bounds in bin indices.
	// Bins are 1-indexed in Fortran; we use 0-indexed but start from bin 1
	// (skipping DC at bin 0), matching WSJT-X's ia=max(1, nint(nfa/df)).
	iaFreq := int(math.Max(1, math.Round(minSearchFreqHz/df)))
	ibFreq := int(math.Round(maxSearchFreqHz / df))
	// Ensure the highest tone (tone 6 at bin + 6*nfos) is within bounds.
	if ibFreq+sync8Nfos*6 > sync8NH1 {
		ibFreq = sync8NH1 - sync8Nfos*6
	}
	if iaFreq > ibFreq {
		return Sync8Result{}
	}

	// Compute noise floor (mean linear power in the search band).
	var sumPower float64
	var countPower int
	for j := range nhsym {
		for i := iaFreq; i <= ibFreq; i++ {
			sumPower += s[j][i]
			countPower++
		}
	}
	noiseFloor := 0.0
	if countPower > 0 {
		noiseFloor = sumPower / float64(countPower)
	}

	// Per-frequency results: best time lag (narrow and wide).
	type freqPeak struct {
		jpeak, jpeak2 int
		red, red2     float64
	}
	peaks := make([]freqPeak, sync8NH1+1)

	for i := iaFreq; i <= ibFreq; i++ {
		var bestNarrow, bestWide float64
		var jBestNarrow, jBestWide int

		for j := -sync8JZ; j <= sync8JZ; j++ {
			var ta, tb, tc, t0a, t0b, t0c float64

			for n := range 7 {
				// Fortran 1-indexed frame: m = j + jstrt + nssy*n
				// Go 0-indexed frame:      f = m - 1
				m := j + jstrt + sync8Nssy*n
				f := m - 1 // Go 0-indexed

				syncBin := i + sync8Nfos*int(CostasSync[n])

				// Block 1 (Costas sync block at symbols 0–6)
				if f >= 0 && f < nhsym && syncBin <= sync8NH1 {
					ta += s[f][syncBin]
					for t := range 7 { // tones 0–6
						b := i + sync8Nfos*t
						if b <= sync8NH1 {
							t0a += s[f][b]
						}
					}
				}

				// Block 2 (Costas sync block at symbols 36–42)
				f2 := f + sync8Nssy*36
				if f2 >= 0 && f2 < nhsym && syncBin <= sync8NH1 {
					tb += s[f2][syncBin]
					for t := range 7 {
						b := i + sync8Nfos*t
						if b <= sync8NH1 {
							t0b += s[f2][b]
						}
					}
				}

				// Block 3 (Costas sync block at symbols 72–78)
				f3 := f + sync8Nssy*72
				if f3 >= 0 && f3 < nhsym && syncBin <= sync8NH1 {
					tc += s[f3][syncBin]
					for t := range 7 {
						b := i + sync8Nfos*t
						if b <= sync8NH1 {
							t0c += s[f3][b]
						}
					}
				}
			}

			// sync_abc: all three blocks.
			// sync = syncPower / meanNonSyncPower
			// where meanNonSyncPower = (totalPower - syncPower) / 6
			tABC := ta + tb + tc
			t0ABC := t0a + t0b + t0c
			denomABC := (t0ABC - tABC) / 6.0
			var syncABC float64
			if denomABC > 0 {
				syncABC = tABC / denomABC
			}

			// sync_bc: blocks 2+3 only (catches late-arriving signals
			// where block 1 may be partially missing).
			tBC := tb + tc
			t0BC := t0b + t0c
			denomBC := (t0BC - tBC) / 6.0
			var syncBC float64
			if denomBC > 0 {
				syncBC = tBC / denomBC
			}

			syncVal := max(syncABC, syncBC)

			// Update narrow peak (±MLag)
			if j >= -sync8MLag && j <= sync8MLag {
				if syncVal > bestNarrow {
					bestNarrow = syncVal
					jBestNarrow = j
				}
			}

			// Update wide peak (±JZ)
			if syncVal > bestWide {
				bestWide = syncVal
				jBestWide = j
			}
		}

		peaks[i] = freqPeak{
			jpeak: jBestNarrow, red: bestNarrow,
			jpeak2: jBestWide, red2: bestWide,
		}
	}

	// --- Step 3: 40th-percentile baseline normalization ---
	// Matching sync8.f90 lines 87–115.
	iz := ibFreq - iaFreq + 1
	if iz <= 0 {
		return Sync8Result{NoiseFloor: noiseFloor}
	}

	// Extract red[] and red2[] for percentile computation.
	redSlice := make([]float64, iz)
	red2Slice := make([]float64, iz)
	for idx, bin := 0, iaFreq; bin <= ibFreq; idx, bin = idx+1, bin+1 {
		redSlice[idx] = peaks[bin].red
		red2Slice[idx] = peaks[bin].red2
	}

	pctIdx := int(math.Round(0.40 * float64(iz)))
	if pctIdx < 1 {
		pctIdx = 1
	}

	// Find 40th percentile of red values.
	sorted := make([]float64, iz)
	copy(sorted, redSlice)
	slices.Sort(sorted)
	base := sorted[pctIdx-1]
	if base <= 0 {
		base = 1e-10
	}

	// Find 40th percentile of red2 values.
	sorted2 := make([]float64, iz)
	copy(sorted2, red2Slice)
	slices.Sort(sorted2)
	base2 := sorted2[pctIdx-1]
	if base2 <= 0 {
		base2 = 1e-10
	}

	// Normalize.
	for bin := iaFreq; bin <= ibFreq; bin++ {
		peaks[bin].red /= base
		peaks[bin].red2 /= base2
	}

	// --- Step 4: Emit candidates sorted by descending score ---
	// Matching sync8.f90 lines 117–134.
	type binScore struct {
		bin   int
		score float64
	}
	sortedBins := make([]binScore, iz)
	for idx, bin := 0, iaFreq; bin <= ibFreq; idx, bin = idx+1, bin+1 {
		sortedBins[idx] = binScore{bin, peaks[bin].red}
	}
	slices.SortFunc(sortedBins, func(a, b binScore) int {
		return cmp.Compare(b.score, a.score)
	})

	var precands []Candidate
	for _, bs := range sortedBins {
		if len(precands) >= sync8MaxPreCand {
			break
		}
		bin := bs.bin
		p := peaks[bin]

		// Add narrow-range peak.
		if p.red >= syncMin && !math.IsNaN(p.red) {
			precands = append(precands, Candidate{
				Freq:    float32(float64(bin) * df),
				TimeOff: sync8LagToTimeOff(p.jpeak, tstep),
				Score:   float32(p.red),
			})
		}

		// Also add wide-range peak if it's at a different time position.
		if p.jpeak2 == p.jpeak {
			continue
		}
		if len(precands) >= sync8MaxPreCand {
			break
		}
		if p.red2 >= syncMin && !math.IsNaN(p.red2) {
			precands = append(precands, Candidate{
				Freq:    float32(float64(bin) * df),
				TimeOff: sync8LagToTimeOff(p.jpeak2, tstep),
				Score:   float32(p.red2),
			})
		}
	}

	// --- Step 5: Near-dupe suppression ---
	// Matching sync8.f90 lines 138–149: suppress if |Δf| < 4 Hz and |Δt| < 40 ms.
	for i := 1; i < len(precands); i++ {
		if precands[i].Score <= 0 {
			continue
		}
		for j := range i {
			if precands[j].Score <= 0 {
				continue
			}
			fdiff := math.Abs(float64(precands[i].Freq) - float64(precands[j].Freq))
			tdiff := math.Abs(float64(precands[i].TimeOff) - float64(precands[j].TimeOff))
			if fdiff < 4.0 && tdiff < 0.04 {
				if precands[i].Score >= precands[j].Score {
					precands[j].Score = 0
				} else {
					precands[i].Score = 0
				}
			}
		}
	}

	// --- Step 6: Final filtering, sorting, and truncation ---
	var result []Candidate
	for _, c := range precands {
		if c.Score > 0 {
			result = append(result, c)
		}
	}
	slices.SortFunc(result, func(a, b Candidate) int {
		return cmp.Compare(b.Score, a.Score)
	})
	if len(result) > maxCandidates {
		result = result[:maxCandidates]
	}

	return Sync8Result{Candidates: result, NoiseFloor: noiseFloor}
}

// sync8LagToTimeOff converts a sync8 time lag index (j) to an absolute
// time offset in seconds from the start of the audio buffer.
//
// WSJT-X convention: xdt = (jpeak − 0.5) × tstep, where xdt=0 means the
// signal starts 0.5 s into the capture. Our convention: TimeOff is absolute
// seconds from audio start. So TimeOff = xdt + 0.5.
func sync8LagToTimeOff(j int, tstep float64) float32 {
	xdt := (float64(j) - 0.5) * tstep
	return float32(xdt + 0.5)
}
