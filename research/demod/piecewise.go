package demod

import "math"

// LocalBlockFit captures a linear `phase(sym) = Phi0 + Slope·sym`
// fit to a single Costas block's 7 anchors. The fit is used both
// for the max-block-RMS fallback signal and as the bridge sanity
// reference (the average of two adjacent block slopes is what the
// bridge slope is compared against).
type LocalBlockFit struct {
	Phi0     float64
	Slope    float64
	RMSResid float64
}

// PhaseFitPiecewise captures the piecewise-linear phase model fitted
// to all 21 Costas anchors of a candidate FT8 signal.
//
// The model: each Costas block has its own local linear fit. Data
// symbols between blocks are predicted by anchor-to-anchor linear
// interpolation over the bridge:
//
//	sym 7..35  → linear interp from phase(sym 6) to phase(sym 36)
//	sym 43..71 → linear interp from phase(sym 42) to phase(sym 72)
//
// **Anchor-to-anchor, not block-fit-smoothed.** Bridge endpoints
// are the raw unwrapped Goertzel phases at the bordering anchors,
// not values from the per-block linear fit. The per-block fits
// exist only to compute the RMS-residual diagnostic and the bridge
// sanity reference. Future work could test smoothed endpoints if
// raw is too noisy.
//
// **Bridge sanity** (per operator spec, Session 90): the slope of
// each bridge (computed from anchor phase difference) must agree
// with the average of the two adjacent block slopes within
// piecewiseBridgeDeltaThreshold. Mismatch indicates either a
// phase jump in the data region (multipath, frequency hop) or
// noisy Costas anchors that fit each block cleanly but disagree
// between blocks — both are conditions where piecewise-coherent
// would produce confident-but-wrong output, so we fall back to
// incoherent for the candidate.
type PhaseFitPiecewise struct {
	// AnchorPhases are the 21 unwrapped per-anchor phases. Within
	// each block, anchors are unwrapped using 1-sym diffs (safe,
	// no 2π ambiguity). Between blocks, the next block's first
	// anchor is unwrapped using the preceding block's local fit
	// to predict-and-choose the right 2π branch — see
	// fitCostasPhasePiecewise for the algorithm.
	AnchorPhases [costasAnchors]float64

	// AnchorWeights[i] is the per-anchor weight (max(0, log-contrast
	// of expected vs max-other tone)). Same as the linear PhaseFit
	// convention — zero-weight anchors are excluded from per-block
	// fits.
	AnchorWeights [costasAnchors]float64

	// BlockFits[b] is the local linear fit on block b's 7 anchors.
	// Phi0 and Slope of blocks 1 and 2 are shifted to be consistent
	// with the global unwrap chain (i.e., they reflect the unwrapped
	// AnchorPhases, not the raw cmplx.Phase outputs).
	BlockFits [numCostasBlocks]LocalBlockFit

	// MaxBlockRMSResid is max over b of BlockFits[b].RMSResid.
	// Primary fallback signal: if linear fit doesn't hold even
	// within a 7-anchor Costas block, the global phase dynamics
	// are too non-linear for piecewise coherent demod to help.
	MaxBlockRMSResid float64

	// Bridge slopes — phase change per sym across each 30-sym data
	// region. Computed from raw unwrapped anchor phases.
	BridgeSlope01 float64 // (phi36 - phi6) / 30
	BridgeSlope12 float64 // (phi72 - phi42) / 30

	// Bridge sanity deltas — how much the bridge slope differs from
	// the average of the two adjacent block slopes. Secondary
	// fallback signal: a bridge that doesn't follow the local
	// slopes is a sign of either a phase jump or noisy anchors
	// fitting their own block cleanly but disagreeing across.
	BridgeDelta01 float64 // |BridgeSlope01 - 0.5·(BlockFits[0].Slope + BlockFits[1].Slope)|
	BridgeDelta12 float64 // |BridgeSlope12 - 0.5·(BlockFits[1].Slope + BlockFits[2].Slope)|

	AccessibleAnchors int
}

// piecewiseBridgeDeltaThreshold is the absolute-rad/sym ceiling on
// |bridgeSlope - avgAdjacentBlockSlope|. Operator-set 2026-05-25
// per Session 90 diagnostic: 0.3 rad/sym maps to ~0.3 Hz of
// effective freq drift across a 30-sym bridge, the same magnitude
// as the slope bucket that the diagnostic flagged as problematic.
// Tracked separately from the linear-coherent threshold so the two
// can be retuned independently.
const piecewiseBridgeDeltaThreshold = 0.3

// piecewiseMaxBlockRMSThreshold is the per-block RMS ceiling for
// the piecewise fallback. Looser than the linear-coherent threshold
// (which had to fit one slope across 78 syms) because each block
// only has to fit 7 anchors over 6 syms — much smaller window of
// phase dynamics. Operator-tunable; start at the same 0.4 rad
// as the linear path for symmetry, then loosen if real captures
// suggest per-block fits land well below 0.4.
const piecewiseMaxBlockRMSThreshold = 0.4

// fitCostasPhasePiecewise fits the piecewise-linear phase model to
// the 21 Costas anchors of a candidate. Algorithm (per operator
// directive, Session 90 — endpoint-state refactor after a
// pre-shift-vs-post-shift Phi0 bug was traced):
//
//  1. Reuse fitCostasPhase to collect raw per-anchor phases and
//     weights (we discard its global Phi0/Slope/RMSResid).
//
//  2. Unwrap within each block using 1-sym differential phases
//     (gaps are 1 sym, no 2π ambiguity).
//
//  3. Initial per-block weighted LS to extract slopes only. The
//     fitted Phi0 from this pass is THROWN AWAY — it's in each
//     block's local frame and trying to use it for bridge
//     prediction is the bug class we're avoiding.
//
//  4. Bridge unwrap using EXPLICIT ENDPOINT STATE — no reliance on
//     BlockFits.Phi0:
//
//     prevLastPhi   = unwrapped[prevBlockLastIdx]
//     prevSlope     = blockSlopes[prevBlock]
//     predicted     = prevLastPhi + prevSlope·(nextFirstSym - prevLastSym)
//     shift         = round((predicted - rawNextFirst) / 2π)
//     apply +shift·2π to the entire next block
//
//     unwrapped[prevBlockLastIdx] is already in the global frame
//     (block 0 is unshifted by definition; block 1 is shifted by
//     bridge 0→1 before bridge 1→2 reads it). No mutable Phi0
//     coordinate confusion.
//
//  5. Final per-block fits in the global frame. These reflect the
//     post-shift unwrapped phases, so their Phi0 IS the phase at
//     sym=0 in the global frame (usable for diagnostics or any
//     external caller — but not relied on by the bridge logic
//     itself).
//
//  6. Bridge slopes + sanity deltas from the now-globally-consistent
//     unwrapped[] array.
//
// **Caller contract.** Inspect MaxBlockRMSResid and BridgeDelta01/12
// before using the fit for predictions. Use piecewiseFallback for
// the standard fallback rule.
//
// **Requires all 21 anchors accessible** (AccessibleAnchors == 21).
// Partial coverage returns MaxBlockRMSResid = +Inf — bridge
// prediction inherently needs both blocks fully fit.
func fitCostasPhasePiecewise(samples []float32, freqHz, dtSec float64) PhaseFitPiecewise {
	linearFit := fitCostasPhase(samples, freqHz, dtSec)
	var out PhaseFitPiecewise

	if linearFit.AccessibleAnchors < costasAnchors {
		out.MaxBlockRMSResid = math.Inf(1)
		out.AccessibleAnchors = linearFit.AccessibleAnchors
		return out
	}
	out.AccessibleAnchors = linearFit.AccessibleAnchors
	out.AnchorWeights = linearFit.weights

	// Step 1: within-block unwrap (safe, 1-sym gaps).
	var unwrapped [costasAnchors]float64
	for b := 0; b < numCostasBlocks; b++ {
		blockStart := b * costasSymbolsPerBlock
		unwrapped[blockStart] = linearFit.rawPhases[blockStart]
		for k := 1; k < costasSymbolsPerBlock; k++ {
			idx := blockStart + k
			unwrapped[idx] = unwrapNear(unwrapped[idx-1], linearFit.rawPhases[idx])
		}
	}

	// Step 2: initial per-block fits — slopes only. Phi0 from this
	// pass is each block's local-frame intercept; we don't use it.
	var blockSlopes [numCostasBlocks]float64
	for b := 0; b < numCostasBlocks; b++ {
		start := b * costasSymbolsPerBlock
		fit := blockWeightedLS(
			costasSym[start], costasSymbolsPerBlock,
			unwrapped[start:start+costasSymbolsPerBlock],
			out.AnchorWeights[start:start+costasSymbolsPerBlock],
		)
		blockSlopes[b] = fit.Slope
	}

	// Step 3: bridge unwrap using endpoint state.
	for bridge := 0; bridge < 2; bridge++ {
		prevBlock := bridge
		nextBlock := bridge + 1
		prevBlockLastIdx := prevBlock*costasSymbolsPerBlock + costasSymbolsPerBlock - 1
		nextBlockFirstIdx := nextBlock * costasSymbolsPerBlock
		prevLastSym := costasSym[prevBlockLastIdx]
		nextFirstSym := costasSym[nextBlockFirstIdx]

		prevLastPhi := unwrapped[prevBlockLastIdx]
		prevSlope := blockSlopes[prevBlock]
		nextRawFirst := unwrapped[nextBlockFirstIdx]

		predictedNextFirst := prevLastPhi + prevSlope*float64(nextFirstSym-prevLastSym)
		shift := math.Round((predictedNextFirst - nextRawFirst) / (2 * math.Pi))
		offset := shift * 2 * math.Pi

		for k := 0; k < costasSymbolsPerBlock; k++ {
			unwrapped[nextBlockFirstIdx+k] += offset
		}
	}

	// Step 4: final per-block fits in the global frame.
	for b := 0; b < numCostasBlocks; b++ {
		start := b * costasSymbolsPerBlock
		out.BlockFits[b] = blockWeightedLS(
			costasSym[start], costasSymbolsPerBlock,
			unwrapped[start:start+costasSymbolsPerBlock],
			out.AnchorWeights[start:start+costasSymbolsPerBlock],
		)
	}

	out.AnchorPhases = unwrapped

	// Step 5: bridge slopes + sanity deltas from the unwrapped phases.
	// Anchor indices: block 0 ends at idx 6 (sym 6), block 1 starts
	// at idx 7 (sym 36); block 1 ends at idx 13 (sym 42), block 2
	// starts at idx 14 (sym 72). Sym gap is 30 in both bridges.
	const bridgeSymGap = 30.0

	out.BridgeSlope01 = (unwrapped[7] - unwrapped[6]) / bridgeSymGap
	out.BridgeSlope12 = (unwrapped[14] - unwrapped[13]) / bridgeSymGap

	local01 := 0.5 * (out.BlockFits[0].Slope + out.BlockFits[1].Slope)
	local12 := 0.5 * (out.BlockFits[1].Slope + out.BlockFits[2].Slope)

	out.BridgeDelta01 = math.Abs(out.BridgeSlope01 - local01)
	out.BridgeDelta12 = math.Abs(out.BridgeSlope12 - local12)

	out.MaxBlockRMSResid = out.BlockFits[0].RMSResid
	for b := 1; b < numCostasBlocks; b++ {
		if out.BlockFits[b].RMSResid > out.MaxBlockRMSResid {
			out.MaxBlockRMSResid = out.BlockFits[b].RMSResid
		}
	}

	return out
}

// piecewiseFallback applies the standard fallback rule: piecewise
// coherent demod should be skipped (fall back to incoherent) if
// any of the fit's quality signals exceed their thresholds. The
// thresholds are package-level constants — tune by editing them,
// not by passing parameters.
//
// Conditions:
//
//   - Any block's RMS residual > piecewiseMaxBlockRMSThreshold:
//     local linear model doesn't fit even within a 7-anchor block,
//     so cross-block bridge predictions are unreliable.
//   - Either bridge's |slope - avgAdjacentBlockSlope| >
//     piecewiseBridgeDeltaThreshold: bridge phase doesn't follow
//     the local slopes, indicating a phase jump or noisy anchors
//     that fit each block but disagree across.
//   - AccessibleAnchors < 21: incomplete coverage; piecewise can't
//     bridge if any block has missing anchors.
func piecewiseFallback(fit PhaseFitPiecewise) bool {
	if fit.AccessibleAnchors < costasAnchors {
		return true
	}
	if math.IsInf(fit.MaxBlockRMSResid, 1) || fit.MaxBlockRMSResid > piecewiseMaxBlockRMSThreshold {
		return true
	}
	if fit.BridgeDelta01 > piecewiseBridgeDeltaThreshold || fit.BridgeDelta12 > piecewiseBridgeDeltaThreshold {
		return true
	}
	return false
}

// unwrapNear returns `target + n·2π` such that the result is the
// closest 2π-equivalent of `target` to `reference`. Used for
// within-block unwrap (1-sym gaps) and bridge unwrap (after
// block-fit prediction supplies the reference).
func unwrapNear(reference, target float64) float64 {
	diff := target - reference
	n := math.Round(diff / (2 * math.Pi))
	return target - n*2*math.Pi
}

// blockWeightedLS runs a weighted closed-form LS on a single Costas
// block's 7 anchors. anchorStart is the channel-symbol index of the
// block's first anchor (0, 36, or 72). phases and weights are the
// 7-element slices for this block. Returns Phi0 and Slope of the
// `phase(sym) = Phi0 + Slope·sym` model, plus the weighted RMS
// residual. Inaccessible (weight=0) anchors are skipped.
func blockWeightedLS(anchorStart, anchorCount int, phases, weights []float64) LocalBlockFit {
	var w0, wS, wP, wSS, wSP float64
	for k := 0; k < anchorCount; k++ {
		w := weights[k]
		if w == 0 {
			continue
		}
		s := float64(anchorStart + k)
		p := phases[k]
		w0 += w
		wS += w * s
		wP += w * p
		wSS += w * s * s
		wSP += w * s * p
	}
	det := w0*wSS - wS*wS
	if det == 0 || w0 == 0 {
		return LocalBlockFit{RMSResid: math.Inf(1)}
	}
	slope := (w0*wSP - wS*wP) / det
	phi0 := (wP - slope*wS) / w0

	var residSqSum float64
	for k := 0; k < anchorCount; k++ {
		w := weights[k]
		if w == 0 {
			continue
		}
		s := float64(anchorStart + k)
		p := phases[k]
		r := p - phi0 - slope*s
		residSqSum += w * r * r
	}
	rmsResid := math.Sqrt(residSqSum / w0)
	return LocalBlockFit{Phi0: phi0, Slope: slope, RMSResid: rmsResid}
}
