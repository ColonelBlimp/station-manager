package sandbox

import "math"

// Per-anchor Costas-pattern verification, sandbox edition.
//
// **Clean-room provenance.** Independently derived from public sources
// only:
//   - QEX July/August 2020 paper §4 (Costas synchronisation: 3 × 7-symbol
//     blocks at symbol indices {0, 36, 72} with sequence
//     {3, 1, 4, 0, 6, 5, 2}, mapped onto the 79-symbol 8-FSK frame).
//   - QEX July/August 2020 paper §3 (FT8 modulation: 6.25 Hz tone
//     spacing, 1920 samples/symbol at 12 kHz, tone-k frequency =
//     base + k·baud).
//   - Goertzel's IIR resonator (Goertzel 1958) for narrowband DTFT-bin
//     power — standard DSP textbook material (e.g. Oppenheim & Schafer,
//     "Discrete-Time Signal Processing", §10.3).
//
// No code, comments, or design choices imported from
// `research/candidates/verify.go`, WSJT-X, or any GPL source — the
// sandbox firewall stays intact. The candidates-package verifier and
// this one will produce the same metric values on the same audio +
// coordinate because they both implement the same QEX-described
// idea on the same signal model; that's spec parity, not derivation.
//
// **Algorithm.** For each candidate (freq, dt):
//
//  1. The expected TX start sample is round((0.5 + dt) · fs).
//  2. At each of the 21 Costas anchors (block b ∈ {0,1,2}, symInBlock
//     s ∈ {0..6}, channelSym = b·36 + s), pull a 1920-sample audio
//     window starting at txStart + channelSym·nsps. If the window
//     falls outside the audio buffer, skip the anchor (the per-block
//     accessibility counter records the loss; per-block geometric-
//     mean denominators divide by ACCESSIBLE anchors, not 7).
//  3. On that window, compute |X(f_k)|² for each of the 8 FT8 tones
//     f_k = freq + k·baud via 8 parallel Goertzel recursions. The
//     winning tone is argmax of the 8 energies; the expected tone is
//     icos7[s] (sandbox already names this `costasArray`).
//  4. Record:
//     - WinsTotal: count of anchors where expected tone won.
//     - LogContrastBlock[b]: Σ over block-b anchors of
//       log(E[expected]) − log(max E[non-expected]).
//  5. Aggregates:
//     - GeoContrast = exp(Σ log-contrast / Σ accessible) =
//       geometric mean over all accessible anchors.
//     - MinBlockContrast = min over b of exp(log-contrast[b] /
//       accessible[b]) — the weakest block's geometric-mean contrast.
//
// **Why this discriminates better than spectrogram-bin matched filter
// (Sync).** Sync sums 21 spectrogram bins at the canonical Costas
// (frame, bin) coordinates of the global spectrogram grid; a strong
// noise + nearby-signal energy distribution can push that sum up
// without the per-anchor 8-tone alphabet voting for the expected
// pattern. Per-anchor "which of the 8 tones is loudest" is a per-
// symbol categorical vote; one bad anchor cancels with one good
// anchor at the contrast level but not at the win-count level. Real
// FT8 signals win on most anchors AND have high per-anchor contrast;
// structural aliases tend to win at one or two anchors only and
// fail in at least one block.
//
// **Cost.** Eight Goertzel recursions, each ~ 1920 multiply-adds, ×
// 21 anchors = ~ 322k MACs per verify call. For ~1500 post-NMS
// candidates corpus-wide this is well under the existing pipeline's
// budget; the verifier was deliberately added as a "lighter touch"
// front-end gate rather than as a finder replacement so the cost
// only lands on survivors, not the raw (~ 70k coordinate) grid.

// CostasVerify is the per-candidate Costas verification record.
// Field semantics match the algorithm description above; populated
// by VerifyCostasAt.
type CostasVerify struct {
	// WinsTotal is the count of Costas anchors where the expected
	// tone had the highest energy among all 8 FT8 tones. 0..21.
	WinsTotal int

	// WinsBlock[b] is the per-Costas-block win count, 0..7 each.
	WinsBlock [3]int

	// AccessibleBlock[b] is the number of anchors in block b whose
	// 1920-sample window fit inside the audio buffer. Signals with
	// |dt| > ~0.5 s near the slot edges may lose anchors here; the
	// per-block contrast aggregates divide by these counts rather
	// than 7 so partial-coverage candidates aren't artificially
	// down-weighted.
	AccessibleBlock [3]int

	// LogContrastTotal is the sum across all accessible anchors of
	// log(expected_energy) − log(max_other_energy). Logs let
	// summing correspond to geometric mean after exponentiation.
	LogContrastTotal float64

	// LogContrastBlock[b] is the per-block log-contrast sum.
	LogContrastBlock [3]float64

	// GeoContrast is exp(LogContrastTotal / Σ accessible) — the
	// geometric mean of per-anchor expected/max-other energy
	// ratios. > 1 means the expected tone dominates on average; < 1
	// means an off-pattern tone is winning.
	GeoContrast float64

	// MinBlockContrast is min over b of
	// exp(LogContrastBlock[b] / AccessibleBlock[b]) — the weakest
	// block's geometric mean. Catches candidates that look strong
	// in two blocks but collapse in the third (typical structural-
	// alias signature).
	MinBlockContrast float64

	// Sync is the candidate's matched-filter score from the finder,
	// passed through unchanged for downstream ranking that wants
	// to break ties between equally-good Stage2 metrics.
	Sync float64

	// Accessible is the total accessible anchor count (sum of
	// AccessibleBlock). Convenience field; 21 when the whole frame
	// fits in the buffer.
	Accessible int
}

// VerifyCostasAt computes a CostasVerify record on the raw audio
// at a single (freqHz, dtSec, sync) coordinate.
//
// dtSec is in the QEX nominal frame: dtSec = 0 means the signal's
// symbol-0 starts at 0.5 s into the slot audio. This matches the
// sandbox finder's Candidate.DtSec convention.
//
// Returns the zero CostasVerify (with Sync populated) if no anchors
// are accessible. The caller is expected to treat that as a fail-
// closed outcome.
func VerifyCostasAt(samples []float32, freqHz, dtSec, sync float64) CostasVerify {
	v := CostasVerify{Sync: sync}

	txStart := int(math.Round((nominalStartSec + dtSec) * fs))

	// Goertzel coefficients c_k = 2·cos(2π·f_k / fs), one per tone.
	// Computed once per call (independent of anchor); the recursion
	// only re-runs over the audio window per anchor.
	const numTones = 8
	var coeffs [numTones]float64
	for k := 0; k < numTones; k++ {
		fk := freqHz + float64(k)*ft8BaudHz
		coeffs[k] = 2 * math.Cos(2*math.Pi*fk/fs)
	}

	const eps = 1e-12
	var energies [numTones]float64

	for b := 0; b < 3; b++ {
		for s := 0; s < 7; s++ {
			channelSym := costasBlockStarts[b] + s
			symStart := txStart + channelSym*nsps
			if symStart < 0 || symStart+nsps > len(samples) {
				continue
			}
			expectedTone := costasArray[s]
			goertzelBank(samples, symStart, nsps, &coeffs, &energies)

			// Find the argmax energy.
			winnerTone := 0
			winnerEnergy := energies[0]
			for k := 1; k < numTones; k++ {
				if energies[k] > winnerEnergy {
					winnerTone = k
					winnerEnergy = energies[k]
				}
			}

			v.AccessibleBlock[b]++
			if winnerTone == expectedTone {
				v.WinsTotal++
				v.WinsBlock[b]++
			}

			// Log-contrast: expected tone vs the strongest non-expected
			// tone. eps prevents log(0) on synthetic-clean inputs that
			// can have exactly-zero energy in some tone.
			maxOther := 0.0
			for k := 0; k < numTones; k++ {
				if k == expectedTone {
					continue
				}
				if energies[k] > maxOther {
					maxOther = energies[k]
				}
			}
			anchorLogContrast := math.Log(energies[expectedTone]+eps) - math.Log(maxOther+eps)
			v.LogContrastBlock[b] += anchorLogContrast
		}
		v.LogContrastTotal += v.LogContrastBlock[b]
	}

	v.Accessible = v.AccessibleBlock[0] + v.AccessibleBlock[1] + v.AccessibleBlock[2]
	if v.Accessible == 0 {
		return v
	}
	v.GeoContrast = math.Exp(v.LogContrastTotal / float64(v.Accessible))

	minBlock := math.Inf(1)
	for b := 0; b < 3; b++ {
		if v.AccessibleBlock[b] == 0 {
			continue
		}
		bc := math.Exp(v.LogContrastBlock[b] / float64(v.AccessibleBlock[b]))
		if bc < minBlock {
			minBlock = bc
		}
	}
	if math.IsInf(minBlock, 1) {
		minBlock = 0
	}
	v.MinBlockContrast = minBlock

	return v
}

// goertzelBank runs eight independent Goertzel recursions in
// parallel over a contiguous audio window and writes |X(f_k)|² for
// k=0..7 into out.
//
// The recursion is the standard Goertzel form:
//
//	s[n] = x[n] + c·s[n−1] − s[n−2]
//
// with the closed-form power expression at the end:
//
//	|X(f)|² = s[N−1]² + s[N−2]² − c · s[N−1] · s[N−2]
//
// `c` is the supplied coefficient 2·cos(2π·f/fs); one recursion per
// tone reuses the same audio window with a different coefficient.
//
// Array-indexed form is intentionally kept simple — the audit
// budget is dominated by elsewhere in the pipeline and an unrolled
// register-pressure-aware variant is a tunable optimisation, not
// a behavioural difference.
func goertzelBank(samples []float32, start, n int, coeffs, out *[8]float64) {
	var s1, s2 [8]float64
	window := samples[start : start+n]
	for _, sample := range window {
		x := float64(sample)
		for k := 0; k < 8; k++ {
			s0 := x + coeffs[k]*s1[k] - s2[k]
			s2[k] = s1[k]
			s1[k] = s0
		}
	}
	for k := 0; k < 8; k++ {
		out[k] = s1[k]*s1[k] + s2[k]*s2[k] - coeffs[k]*s1[k]*s2[k]
	}
}

// **Threshold provenance — important.** The verifier algorithm above
// (per-anchor 8-tone vote + log-contrast aggregation) is derived
// entirely from public-spec sources (QEX § 4 + Goertzel 1958) and is
// clean-room safe. The OPERATING POINT used by sandbox strict mode
// (Filter / GeoContrast / threshold 0.70) is NOT spec-derived — it
// is an empirical sweet spot measured on the 6-fixture strict
// corpus on 2026-05-29 (zero matched-truth loss, 2 false-positive
// decodes eliminated, 83% post-NMS candidate-volume reduction). The
// same trade is reachable via WinsTotal ≥ 8 on the same corpus.
// Any production-bound consumer of this verifier should re-measure
// its operating point on its own corpus or a holdout split — 0.70 is
// a corpus calibration, not a published constant.

// VerifyCostasGrid runs the same per-Costas-anchor 8-tone vote +
// log-contrast aggregation as VerifyCostasAt, but on a previously-
// extracted SymbolGrid instead of raw audio. Used as a downstream
// "did the symbol-quality survive the channelizer + refine + extract
// chain" probe — reports of grid GeoContrast can be compared against
// audio GeoContrast (VerifyCostasAt) on the same candidate to
// attribute decoder-bound truths to either an upstream extract
// degradation (audio good, grid bad) or a downstream BP/OSD/LLR
// problem (audio good, grid good, no decode).
//
// Provenance: same QEX § 4 / Goertzel-shaped idea as VerifyCostasAt,
// but with the per-symbol DTFT already done by `ExtractSymbols`
// (32-point complex FFT per symbol → 8 in-band tone bins). The
// "Goertzel" here is implicit in the FFT — both compute the DTFT at
// the same tone frequencies, just by different algorithms; the
// scoring layer (per-anchor argmax, log-contrast aggregation) is
// identical.
//
// AccessibleBlock is set to {7, 7, 7} unconditionally — the symbol
// grid always covers all 79 symbols by ExtractSymbols' contract
// (if any symbol fell outside the baseband buffer, Extract would
// have returned an error and this function would not have been
// called).
//
// Sync is zero here — the candidate's matched-filter score is a
// raw-audio-stage artefact, not preserved through Extract.
func VerifyCostasGrid(grid *SymbolGrid) CostasVerify {
	var v CostasVerify
	if grid == nil {
		return v
	}
	const eps = 1e-12
	const numTones = 8

	for b := 0; b < 3; b++ {
		for s := 0; s < 7; s++ {
			channelSym := costasBlockStarts[b] + s
			expectedTone := costasArray[s]
			energies := grid.Tones[channelSym]

			winnerTone := 0
			winnerEnergy := energies[0]
			for k := 1; k < numTones; k++ {
				if energies[k] > winnerEnergy {
					winnerTone = k
					winnerEnergy = energies[k]
				}
			}
			v.AccessibleBlock[b]++
			if winnerTone == expectedTone {
				v.WinsTotal++
				v.WinsBlock[b]++
			}

			maxOther := 0.0
			for k := 0; k < numTones; k++ {
				if k == expectedTone {
					continue
				}
				if energies[k] > maxOther {
					maxOther = energies[k]
				}
			}
			anchorLogContrast := math.Log(energies[expectedTone]+eps) - math.Log(maxOther+eps)
			v.LogContrastBlock[b] += anchorLogContrast
		}
		v.LogContrastTotal += v.LogContrastBlock[b]
	}

	v.Accessible = v.AccessibleBlock[0] + v.AccessibleBlock[1] + v.AccessibleBlock[2]
	if v.Accessible == 0 {
		return v
	}
	v.GeoContrast = math.Exp(v.LogContrastTotal / float64(v.Accessible))

	minBlock := math.Inf(1)
	for b := 0; b < 3; b++ {
		if v.AccessibleBlock[b] == 0 {
			continue
		}
		bc := math.Exp(v.LogContrastBlock[b] / float64(v.AccessibleBlock[b]))
		if bc < minBlock {
			minBlock = bc
		}
	}
	if math.IsInf(minBlock, 1) {
		minBlock = 0
	}
	v.MinBlockContrast = minBlock

	return v
}

// Stage2Mode selects how the post-NMS Stage2 verifier interacts
// with the multipass pipeline.
type Stage2Mode int

const (
	// Stage2Off (zero value) skips the verifier entirely — the
	// pipeline behaves exactly as before the verifier was added.
	Stage2Off Stage2Mode = iota

	// Stage2Observe runs the verifier on every post-NMS candidate
	// but does NOT change the candidate list. Intended for the
	// audit instrumentation path that records each candidate's
	// metrics without perturbing the decode.
	Stage2Observe

	// Stage2Filter drops every candidate whose configured metric
	// falls below the configured threshold. The remaining order is
	// preserved (still sorted by Sync, as the finder returned).
	Stage2Filter

	// Stage2Rerank re-sorts the post-NMS candidate list descending
	// by the configured metric. No candidate is dropped; the
	// downstream MaxResults cap then admits the top-K by Stage2
	// metric instead of by Sync.
	Stage2Rerank
)

// Stage2Metric selects which Stage2 discriminator drives Stage2Filter
// / Stage2Rerank. Audit numbers (2026-05-29) showed MinBlockContrast
// as the cleanest single-metric separator at the near-truth median;
// GeoContrast as the cleanest at p25; WinsTotal as a strong but
// coarser categorical alternative.
type Stage2Metric int

const (
	// Stage2MetricMinBlock (zero value) selects MinBlockContrast.
	Stage2MetricMinBlock Stage2Metric = iota
	// Stage2MetricGeo selects GeoContrast.
	Stage2MetricGeo
	// Stage2MetricWins selects WinsTotal (treated as float).
	Stage2MetricWins
)

// extract returns the configured metric value from a CostasVerify
// record. Single source of truth for the filter / rerank paths so
// "which metric drives Stage2" is captured in one place.
func (m Stage2Metric) extract(v CostasVerify) float64 {
	switch m {
	case Stage2MetricGeo:
		return v.GeoContrast
	case Stage2MetricWins:
		return float64(v.WinsTotal)
	default:
		return v.MinBlockContrast
	}
}
