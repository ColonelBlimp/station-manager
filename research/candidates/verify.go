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

	// AccessibleBlock[b] is the number of Costas anchors in block b
	// whose audio window fit inside the input buffer at the verifier's
	// chosen (freq, dt). For signals with |dt| > ~0.5 s some anchors
	// fall outside the slot audio — they are skipped in scoring
	// rather than dragging the whole candidate to zero. Sum across
	// blocks gives the total anchor count that actually contributed
	// to the aggregates. Always ≤ costasSymbolsPerBlock (= 7) per block.
	AccessibleBlock [3]int
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
// Anchors whose audio window falls outside the buffer (signals with
// |dt| > ~0.5 s sit close to the slot edges) are skipped rather than
// dragging the whole candidate to zero — v.AccessibleBlock[b]
// records how many of block b's 7 anchors were actually scored, and
// the per-block contrast aggregates divide by the accessible count
// rather than the ideal 7. Returns the zero CostasVerify only when
// NO anchors are accessible.
func verifyCostas(samples []float32, freq, dt float64, stage1Score float64) CostasVerify {
	var v CostasVerify
	v.Stage1Score = stage1Score

	txStart := int(math.Round((synthSlotStartSec + dt) * fs))

	// Precompute the 8 Goertzel coefficients c_k = 2·cos(2π·f_k/Fs).
	// Same coefficients for every anchor — only the audio window
	// changes between anchors.
	var coeffs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		fk := freq + float64(k)*baud
		coeffs[k] = 2 * math.Cos(2*math.Pi*fk/fs)
	}

	const eps = 1e-12

	// energies is the per-anchor tone-power buffer. Declared once
	// outside the loop so the SIMD wrapper's caller-provided
	// output sits on the verifier's stack — escape analysis can
	// keep it there because the synchronous C call cannot retain
	// the pointer past return.
	var energies [ft8ToneCount]float64

	for block := 0; block < numCostasBlocks; block++ {
		for symInBlock := 0; symInBlock < costasSymbolsPerBlock; symInBlock++ {
			channelSym := block*costasBlockStride + symInBlock
			symStart := txStart + channelSym*nsps
			// Per-anchor bounds check: skip anchors whose NSPS-sample
			// window falls outside the audio buffer. Whole-candidate
			// rejection here would lose every early-arrival or
			// late-arrival signal at the slot edges.
			if symStart < 0 || symStart+nsps > len(samples) {
				continue
			}
			expectedTone := int(icos7[symInBlock])

			// Dispatch to the SIMD kernel on amd64+cgo builds;
			// fall back to the pure-Go implementation elsewhere.
			// The branch is a build-time const so the compiler
			// eliminates the dead arm — no runtime overhead.
			if hasSIMDGoertzel {
				goertzelMultiSIMDInto(samples, symStart, nsps, &coeffs, &energies)
			} else {
				energies = goertzelMulti(samples, symStart, nsps, coeffs)
			}

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
			v.AccessibleBlock[block]++

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

	// Aggregates divide by ACCESSIBLE anchor counts so partial-coverage
	// candidates aren't artificially down-weighted vs. full-coverage.
	accessibleTotal := v.AccessibleBlock[0] + v.AccessibleBlock[1] + v.AccessibleBlock[2]
	if accessibleTotal == 0 {
		return v
	}
	v.GeoContrast = math.Exp(v.LogContrastTotal / float64(accessibleTotal))
	minBlock := math.Inf(1)
	for b := 0; b < numCostasBlocks; b++ {
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

// Refinement-grid parameters. Two passes — coarse first to locate
// the rough peak around the supplied stage-1 coordinate, then fine
// to polish — give sub-bin frequency and sub-step DT precision at
// 17 additional verify calls per survivor. Cost is bounded
// (refinement runs only on candidates that already survived NMS,
// typically tens per slot, not thousands).
//
// Coarse step sizes are roughly one spectrogram bin / one
// spectrogram step (3 Hz, 40 ms), so the 3×3 coarse grid covers ±
// one bin / step. Fine step sizes are about a third of the coarse,
// giving final precision near 1 Hz / 10 ms.
const (
	refineCoarseFreqStepHz = 3.0   // Hz — coarse-grid freq step
	refineCoarseDTStepSec  = 0.040 // s — coarse-grid DT step
	refineFineFreqStepHz   = 1.0   // Hz — fine-grid freq step
	refineFineDTStepSec    = 0.010 // s — fine-grid DT step
	refineGridHalfSpan     = 1     // ±1 step each axis → 3×3 grids
)

// refineCandidate searches a small (df, ddt) grid around the
// supplied (freq, dt) for the position that maximises the
// verifier's GeoContrast. Two passes: coarse (3×3 = 9 positions
// at refineCoarseFreqStepHz / refineCoarseDTStepSec spacing) then
// fine (3×3 around the coarse peak at refineFine* spacing). Returns
// the refined (freq, dt) and the updated CostasVerify at that
// position.
//
// Cost: 17 verify calls per survivor (8 coarse around the centre
// + 8 fine around the coarse peak; the centre point is shared).
// Intended to run only on the small set of post-NMS survivors.
//
// On-grid synthetic signals see the peak at the supplied (freq, dt)
// already; refinement finds the same position and the (freq, dt)
// returned are unchanged. For real captures (off-grid) the peak
// shifts toward the actual transmitter frequency and timing.
func refineCandidate(samples []float32, freq, dt, stage1Score float64) (float64, float64, CostasVerify) {
	bestFreq, bestDT := freq, dt
	bestV := verifyCostas(samples, freq, dt, stage1Score)

	// Coarse pass.
	for dfi := -refineGridHalfSpan; dfi <= refineGridHalfSpan; dfi++ {
		for ddti := -refineGridHalfSpan; ddti <= refineGridHalfSpan; ddti++ {
			if dfi == 0 && ddti == 0 {
				continue
			}
			f := freq + float64(dfi)*refineCoarseFreqStepHz
			d := dt + float64(ddti)*refineCoarseDTStepSec
			v := verifyCostas(samples, f, d, stage1Score)
			if v.GeoContrast > bestV.GeoContrast {
				bestFreq, bestDT, bestV = f, d, v
			}
		}
	}

	// Fine pass — search around the coarse peak.
	coarseFreq, coarseDT := bestFreq, bestDT
	for dfi := -refineGridHalfSpan; dfi <= refineGridHalfSpan; dfi++ {
		for ddti := -refineGridHalfSpan; ddti <= refineGridHalfSpan; ddti++ {
			if dfi == 0 && ddti == 0 {
				continue
			}
			f := coarseFreq + float64(dfi)*refineFineFreqStepHz
			d := coarseDT + float64(ddti)*refineFineDTStepSec
			v := verifyCostas(samples, f, d, stage1Score)
			if v.GeoContrast > bestV.GeoContrast {
				bestFreq, bestDT, bestV = f, d, v
			}
		}
	}

	return bestFreq, bestDT, bestV
}

// goertzelMulti runs 8 Goertzel recursions in parallel over one
// NSPS-sample window of audio, returning |X(f_k)|² for each of the
// 8 FT8 tones.
//
// THE inner kernel of the pipeline — verifyCostas calls this 21
// times per invocation, and verifyCostas is essentially all of Find.
// On a typical 10-CQ slot the kernel runs ~245,000 times per Find,
// so every nanosecond we shave off here multiplies by ~245k.
//
// **Unrolled.** The 8 Goertzel recursions are mathematically
// independent — only the serial chain within ONE recursion
// (s_n = x_n + c·s_{n-1} - s_{n-2}) forces ordering. By manually
// unrolling the 8-tone inner loop and holding state in 16 stack
// variables (instead of two 8-element arrays), the compiler keeps
// the state in registers and the CPU's superscalar pipeline can
// dispatch all 8 multiply-add chains in parallel. Empirically this
// is ~2-3× faster than the array-indexed version on amd64.
//
// Returns the closed-form Goertzel power expression
//
//	|X(f)|² = s_{N-1}² + s_{N-2}² - c · s_{N-1} · s_{N-2}
//
// at each of the 8 frequencies given by the supplied coefficients
// (c_k = 2 · cos(2π · f_k / fs)). Indexing of the returned array
// matches the FT8 8-FSK alphabet order: 0..7.
func goertzelMulti(samples []float32, start, n int, coeffs [ft8ToneCount]float64) [ft8ToneCount]float64 {
	// Hoist coefficients into named locals. Saves an array index
	// per tone per sample in the inner loop.
	c0, c1, c2, c3, c4, c5, c6, c7 := coeffs[0], coeffs[1], coeffs[2], coeffs[3], coeffs[4], coeffs[5], coeffs[6], coeffs[7]

	// State pairs (s1, s2) per recursion, held in stack locals so
	// the compiler keeps them in registers across the inner loop.
	var (
		a1, a2   float64 // tone 0
		b1, b2   float64 // tone 1
		c_1, c_2 float64 // tone 2 (name avoids c-style shadow)
		d1, d2   float64 // tone 3
		e1, e2   float64 // tone 4
		f1, f2   float64 // tone 5
		g1, g2   float64 // tone 6
		h1, h2   float64 // tone 7
	)

	// Slice once + range over the window so the Go compiler can
	// elide bounds checks AND fold the loop counter into the
	// range iterator's internal index. Manual `for i; i < len(audio);`
	// still emits the index increment + compare; `range` lets the
	// CPU's loop predictor work on a tighter sequence.
	audio := samples[start : start+n]

	for _, sample := range audio {
		x := float64(sample)

		// Eight independent Goertzel recursion steps. Each computes
		// s0 = x + c·s1 - s2; then s2 ← s1, s1 ← s0. The CPU can
		// dispatch all eight in parallel — no cross-recursion
		// dependency.
		na := x + c0*a1 - a2
		a2 = a1
		a1 = na
		nb := x + c1*b1 - b2
		b2 = b1
		b1 = nb
		nc := x + c2*c_1 - c_2
		c_2 = c_1
		c_1 = nc
		nd := x + c3*d1 - d2
		d2 = d1
		d1 = nd
		ne := x + c4*e1 - e2
		e2 = e1
		e1 = ne
		nf := x + c5*f1 - f2
		f2 = f1
		f1 = nf
		ng := x + c6*g1 - g2
		g2 = g1
		g1 = ng
		nh := x + c7*h1 - h2
		h2 = h1
		h1 = nh
	}

	// Closed-form power per tone: s1² + s2² - c·s1·s2.
	return [ft8ToneCount]float64{
		a1*a1 + a2*a2 - c0*a1*a2,
		b1*b1 + b2*b2 - c1*b1*b2,
		c_1*c_1 + c_2*c_2 - c2*c_1*c_2,
		d1*d1 + d2*d2 - c3*d1*d2,
		e1*e1 + e2*e2 - c4*e1*e2,
		f1*f1 + f2*f2 - c5*f1*f2,
		g1*g1 + g2*g2 - c6*g1*g2,
		h1*h1 + h2*h2 - c7*h1*h2,
	}
}
