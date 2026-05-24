package candidates

import "math"

// CostasVerify is the stage-2 verification record for a candidate.
// Populated by verifyCostas; consumed by the Find pipeline's hybrid
// gate (WinsTotal + WinsBlock floors) and its tie-broken ranking
// (GeoContrast → WinsTotal → MinBlockContrast → Stage1Score).
//
// The verifier does 21 per-symbol DTFTs over the raw audio (one per
// Costas anchor), each measuring energy in all 8 FT8 tone bins. The
// per-anchor "winner" is the tone with the highest energy. The
// per-anchor "log contrast" is log(expected_energy / max_other_energy)
// — a continuous measure of how cleanly the expected Costas tone
// dominates the alphabet at that anchor.
//
// The score blends two complementary discriminator families:
//
//   - Categorical (win counts): amplitude-invariant — every anchor
//     contributes a single yes/no vote. Robust against amplitude
//     variation across the slot. The hard gate uses these.
//   - Continuous (log contrast / GeoContrast / MinBlockContrast):
//     fine-grained measure of HOW MUCH the expected tone dominates,
//     used for tie-breaking among accepted candidates.
type CostasVerify struct {
	// WinsTotal is the count of Costas anchors where the expected
	// tone had the highest energy among all 8 FT8 tones. 0..21.
	WinsTotal int

	// WinsBlock[b] is the per-Costas-block win count. 0..7 each.
	// Real FT8 transmissions should win across all three blocks;
	// structural aliases and data-symbol coincidences usually fail
	// in at least one block.
	WinsBlock [3]int

	// WinningTone[i] is the tone index (0..7) that won at anchor i.
	// Indexing convention: i = block*7 + symInBlock, so [0..6] are
	// block 0 (symbols 0-6), [7..13] block 1 (36-42), [14..20]
	// block 2 (72-78). Diagnostic field for calibration — tells us
	// WHICH wrong tone won when an anchor failed.
	WinningTone [21]uint8

	// LogContrastTotal is the sum of per-anchor log contrasts across
	// all 21 anchors: Σ log(expected_energy / max_other_energy).
	// In log space so summing corresponds to geometric mean.
	LogContrastTotal float64

	// LogContrastBlock[b] is the per-block sum of log contrasts —
	// 7 anchors each. Used to compute MinBlockContrast.
	LogContrastBlock [3]float64

	// GeoContrast is exp(LogContrastTotal / 21) — the geometric mean
	// of the per-anchor expected/max-other energy ratios. Primary
	// ranking metric; > 1 means expected dominates on average, < 1
	// means an off-pattern tone is winning.
	GeoContrast float64

	// MinBlockContrast is min over b of exp(LogContrastBlock[b] / 7)
	// — the weakest block's geometric-mean contrast. Catches
	// candidates that look strong in two blocks but collapse in the
	// third (typical of structural aliases / partial coincidences).
	MinBlockContrast float64

	// Stage1Score is the matched-filter SNR ratio from stage 1 that
	// generated this candidate. Carried through for the final
	// tie-break in the ranker.
	Stage1Score float64
}

// synthSlotStartSec is the FT8 nominal TX start offset within the
// 15-second slot, per QEX paper §4. Re-declared here so this file
// stays self-contained relative to the package's other FT8
// constants — the research tree is firewalled from internal/ft8/*.
const synthSlotStartSec = 0.5

// dtPhysicalOffsetSteps is the integer offset, in spectrogram
// steps, between the matched filter's peak and a physically-on-time
// signal's true TX start. Structural to the 3,840-sample (=
// 2-symbol) FFT window: the 4-column block whose FFTs each fully
// cover symbol 0 starts at column 9 rather than column 12 (= floor
// (0.5 / tstep)), so a physically-on-time signal peaks at dtSteps
// = -dtPhysicalOffsetSteps in stage 1.
//
// Held as an integer constant so the Find loop can compute its raw
// dt-step bounds symmetrically in physical-DT terms without any
// float-to-integer rounding (3 × tstep is exactly representable as
// a multiple of tstep, but constructing the bounds via division
// would introduce ULP drift at the boundary).
const dtPhysicalOffsetSteps = 3

// dtPhysicalOffsetSec is added to the stage-1 matched filter's
// reported DT to convert it into the physical "relative to 0.5 s
// nominal TX start" frame.
//
// Positive constant by construction — added, never subtracted, so
// the sign cannot be confused. See costasScore + Find for the use.
const dtPhysicalOffsetSec = float64(dtPhysicalOffsetSteps) * float64(nstep) / fs // = +0.120 s

// verifyCostas runs the stage-2 alias-aware verifier on one
// candidate at (freq, dt). dt must be the PHYSICAL DT — i.e. the
// stage-1 raw DT plus dtPhysicalOffsetSec — so the per-symbol
// DTFTs align with the actual TX window.
//
// Algorithm: at each of the 21 Costas anchors (3 blocks × 7
// symbols), pull NSPS samples starting at the anchor's TX-relative
// position and compute the per-tone energy via 8 parallel
// Goertzel recursions (one per FT8 8-FSK tone). Score by:
//
//   - WinsTotal / WinsBlock: count of anchors where expected tone is
//     the highest-energy tone in the 8-FSK alphabet.
//   - LogContrast: log(expected / max-other) per anchor, summed.
//   - GeoContrast, MinBlockContrast: derived aggregates.
//
// Returns a zero-valued CostasVerify when the candidate's TX
// window falls outside the audio buffer.
func verifyCostas(samples []float32, freq, dt float64, stage1Score float64) CostasVerify {
	var v CostasVerify
	v.Stage1Score = stage1Score

	txStart := int(math.Round((synthSlotStartSec + dt) * fs))
	if txStart < 0 || txStart+nn*nsps > len(samples) {
		return v
	}

	// Precompute the 8 Goertzel coefficients c_k = 2·cos(2π·f_k/Fs).
	// Same coefficients for every anchor — only the audio window
	// changes between anchors.
	var coeffs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		fk := freq + float64(k)*baud
		coeffs[k] = 2 * math.Cos(2*math.Pi*fk/fs)
	}

	const eps = 1e-12

	for block := 0; block < numCostasBlocks; block++ {
		for symInBlock := 0; symInBlock < costasSymbolsPerBlock; symInBlock++ {
			channelSym := block*costasBlockStride + symInBlock
			symStart := txStart + channelSym*nsps
			expectedTone := int(icos7[symInBlock])

			energies := goertzelMulti(samples, symStart, nsps, coeffs)

			// Find the winning tone.
			winnerTone := 0
			winnerEnergy := energies[0]
			for k := 1; k < ft8ToneCount; k++ {
				if energies[k] > winnerEnergy {
					winnerTone = k
					winnerEnergy = energies[k]
				}
			}

			anchorIdx := block*costasSymbolsPerBlock + symInBlock
			v.WinningTone[anchorIdx] = uint8(winnerTone)

			if winnerTone == expectedTone {
				v.WinsTotal++
				v.WinsBlock[block]++
			}

			// Log-contrast: expected vs the strongest off-pattern tone.
			var maxOther float64
			for k := 0; k < ft8ToneCount; k++ {
				if k == expectedTone {
					continue
				}
				if energies[k] > maxOther {
					maxOther = energies[k]
				}
			}
			anchorLogContrast := math.Log(energies[expectedTone]+eps) - math.Log(maxOther+eps)
			v.LogContrastBlock[block] += anchorLogContrast
		}
		v.LogContrastTotal += v.LogContrastBlock[block]
	}

	v.GeoContrast = math.Exp(v.LogContrastTotal / float64(numCostasBlocks*costasSymbolsPerBlock))
	minBlock := math.Inf(1)
	for b := 0; b < numCostasBlocks; b++ {
		bc := math.Exp(v.LogContrastBlock[b] / float64(costasSymbolsPerBlock))
		if bc < minBlock {
			minBlock = bc
		}
	}
	v.MinBlockContrast = minBlock

	return v
}

// goertzelMulti runs 8 Goertzel recursions in parallel over one
// NSPS-sample window of audio, returning |X(f_k)|² for each of the
// 8 FT8 tones.
//
// Per-sample inner loop: 8 multiplies + 24 additions/copies — the
// 8 coefficients live in registers, and the audio sweep is
// cache-friendly (one float32 per iteration). Cost: NSPS × 8 × ~5
// ops per anchor; ~21 × 78K ops per candidate ≈ 1.6M ops; with ~100
// candidates per slot the verifier finishes well under the slot
// wall.
//
// Returns the closed-form Goertzel power expression
//
//	|X(f)|² = s_{N-1}² + s_{N-2}² - c · s_{N-1} · s_{N-2}
//
// at each of the 8 frequencies given by the supplied coefficients
// (c_k = 2 · cos(2π · f_k / fs)). Indexing of the returned array
// matches the FT8 8-FSK alphabet order: 0..7.
func goertzelMulti(samples []float32, start, n int, coeffs [ft8ToneCount]float64) [ft8ToneCount]float64 {
	var s1, s2 [ft8ToneCount]float64
	for i := 0; i < n; i++ {
		x := float64(samples[start+i])
		for k := 0; k < ft8ToneCount; k++ {
			s0 := x + coeffs[k]*s1[k] - s2[k]
			s2[k] = s1[k]
			s1[k] = s0
		}
	}
	var energies [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		energies[k] = s1[k]*s1[k] + s2[k]*s2[k] - coeffs[k]*s1[k]*s2[k]
	}
	return energies
}
