package ldpc

import (
	"math"
	"sort"
)

// This file implements Fossorier-Lin Ordered Statistics Decoding
// at order 2. The enumeration depth covers:
//
//	  1 codeword from the MRB hard decision   (OSD-0)
//	 91 single-bit flips of the MRB           (OSD-1)
//	4095 pair flips of the MRB                (OSD-2 = C(91, 2))
//	----
//	4187 total candidate codewords per OSD call.
//
// Order 3 would add C(91,3) = 121,485 more — roughly 30× the work
// for diminishing returns at FT8 SNRs. Order 2 is the standard
// cost-conscious FT8 setting; raise it only after coherent demod
// has been measured (and not before — see operator directive in
// docs/session-handoff.md, Session 88 area).

// osdDiag carries OSD-side diagnostics out of `osd` to populate the
// public Stats. Bundles the existing `explored` count with the
// CRC-pass instrumentation added 2026-05-26: how many of the
// 4187 enumerated candidates passed CRC14, the metric (raw + the
// Hamming distance for a per-flip normalisation) of the best
// CRC-passing one, the ML metric, and whether the ML itself
// passed CRC.
//
// The instrumentation does NOT change the return policy — OSD still
// returns success iff the ML candidate passes CRC. The diagnostics
// were added to answer the calibration question raised by the
// 2026-05-26 code review: when the ML candidate fails CRC, is there
// a better (lower-metric) CRC-clean candidate elsewhere in the same
// OSD-2 neighbourhood that a relaxed return policy would accept?
//
// **Measured result on the real-capture corpus (2026-05-26):
// negative.** 495 OSD invocations decomposed: 9 ml-passed (current
// policy already wins; 8 near-truth, 1 noise), 379 no-CRC-pass in
// the 4187-enumeration (29 near-truth — the OSD-3 / AP territory),
// 107 ml-failed-but-rescuable (13 near-truth, 94 noise). The 13
// rescuable near-truth candidates are indistinguishable from the
// 94 noise CRC-lottery passes on both the raw soft-distance axis
// AND the normalised metric/Hamming-distance axis — distributions
// overlap with noise actually sitting LOWER than near-truth
// (median norm 0.89 vs 2.23). No threshold separates them.
//
// Verdict: OSD's current ML-only return policy is already extracting
// the cleanly-decodable rescues; the 41-signal found-but-not-decoded
// gap is dominated by OSD-2-unreachable cases (29) plus lottery-
// indistinguishable cases (13). Next levers must come from a
// fundamentally different signal source (OSD-3 enumeration depth
// or AP prior-LLR injection from a callsign hashtable) rather than
// post-hoc CRC re-ranking.
//
// The diagnostic fields are RETAINED — the result is the artifact;
// any future proposal to relax the return policy should re-run this
// pass and compare. decode-eval's `printOSDDiagnostics` renders the
// separation table.
type osdDiag struct {
	// explored is the test-pattern enumeration count (≤ 4187 for OSD-2).
	explored int

	// crcPassCount is the number of enumerated candidates whose
	// 91-bit info word passed CRC14.
	crcPassCount int

	// bestCRCMetric is the soft-distance metric Σ|posterior_i| of
	// the LOWEST-metric CRC-passing candidate, or +Inf if none.
	bestCRCMetric float64

	// bestCRCHamming is the Hamming distance from the BP-final hard
	// decision of the best-CRC-passing candidate. Zero when
	// crcPassCount == 0.
	bestCRCHamming int

	// mlMetric is the soft-distance metric of the ML (best-by-metric)
	// candidate — what `osd` actually returns regardless of CRC.
	mlMetric float64

	// mlCRCPass reports whether the ML candidate itself passed CRC.
	// True iff the current "ML-only CRC check" return policy yielded
	// a successful decode.
	mlCRCPass bool
}

// osd runs Fossorier-Lin Ordered Statistics Decoding (order=2) on
// the supplied posterior LLRs. Called by Decode when belief
// propagation alone fails to produce a CRC-clean codeword.
//
// Returns the recovered Result, a success boolean (true iff a
// CRC-passing codeword was found), and an osdDiag with enumeration
// + CRC-pass statistics. On failure, the returned Result is zero —
// the caller should not consult it.
//
// Algorithm:
//
//  1. Sort the 174 bit positions by |posterior| descending; the
//     resulting permutation `perm` puts the most reliable bits in
//     positions 0..90 and the least reliable in 91..173.
//  2. Column-permute H and run Gauss-Jordan elimination (right-to-
//     left) to identify a Most-Reliable Basis (MRB): 91 columns
//     whose complement 83 columns are non-singular. Columns that
//     fail to yield a pivot during right-to-left elimination get
//     demoted from the parity side to the MRB.
//  3. Hard-decide the 91 MRB bits from their LLR signs.
//  4. Enumerate test patterns (the MRB hard-decision plus all
//     single and pair flips); for each, systematically encode the
//     parity bits, reassemble the codeword in original ordering,
//     and score by Σ|posterior_i| over positions where the
//     codeword disagrees with the LLR sign.
//  5. The lowest-scoring (= maximum-likelihood) candidate is the
//     OSD output. Check its CRC14: pass ⇒ success; fail ⇒ reject.
//
// Cost: ~4187 candidates × (83×91 GF(2) matvec + 174-bit metric +
// one CRC14) per OSD call. Well under 10 ms in practice.
func osd(posterior [codewordBits]float64) (Result, bool, osdDiag) {
	diag := osdDiag{bestCRCMetric: math.Inf(1), mlMetric: math.Inf(1)}
	perm := reliabilityPerm(posterior)

	hp, mrbCols, parityCols, ok := osdReduce(perm)
	if !ok {
		// Should never happen for FT8 LDPC (H has full row rank 83).
		// Defensive only.
		return Result{}, false, diag
	}

	// MRB hard-decision from posterior LLR signs.
	var mrbHard [infoBits]uint8
	for i, col := range mrbCols {
		if posterior[perm[col]] <= 0 {
			mrbHard[i] = 1
		}
	}

	// Pre-compute the |posterior| values in PERMUTED ordering — the
	// metric sums over original-order positions, but we'll convert
	// once and avoid repeated permutation lookups inside the hot loop.
	var absPosOrig [codewordBits]float64
	for i := 0; i < codewordBits; i++ {
		absPosOrig[i] = math.Abs(posterior[i])
	}

	// Pre-compute hard-decision in original ordering for the metric.
	var hardOrig [codewordBits]uint8
	for i := 0; i < codewordBits; i++ {
		if posterior[i] <= 0 {
			hardOrig[i] = 1
		}
	}

	// Scratch buffers for the inner loop. Allocated once outside
	// so 4187 iterations don't churn the heap.
	var (
		info       [infoBits]uint8
		cwPerm     [codewordBits]uint8
		cwOrig     [codewordBits]uint8
		bestMetric = math.Inf(1)
		bestCw     [codewordBits]uint8
		explored   int

		// CRC-pass instrumentation (added 2026-05-26, gated by
		// `osdinstr` build tag — see osd_instr_{off,on}.go).
		// crcPassCount + bestCRCMetric/bestCRCHamming track the
		// lowest-metric CRC-passing candidate across the 4187-codeword
		// enumeration so the diag struct can report it. The hot-path
		// extraction of infoCheck and the crc14Matches call live
		// inside scoreOne's gated block; these accumulators are
		// declared at the outer scope because the diag assignment
		// at end of osd reads them unconditionally (zero / +Inf
		// when instrumentation is off).
		crcPassCount   int
		bestCRCMetric  = math.Inf(1)
		bestCRCHamming int
	)

	// scoreOne mutates cwPerm/cwOrig/explored as a side effect and
	// updates bestMetric/bestCw on improvement. Also tracks the
	// best CRC-passing metric for diagnostics — the CRC check adds
	// ~18% to per-candidate cost but the absolute runtime (~1s per
	// corpus run) is acceptable and the data is only useful to
	// collect once.
	scoreOne := func() {
		explored++

		// Place info at MRB positions.
		for i, col := range mrbCols {
			cwPerm[col] = info[i]
		}
		// Derive parity at parity positions: cwPerm[parityCols[r]] =
		// XOR over MRB bits where hp[r][i] == 1.
		for r := 0; r < parityRows; r++ {
			var p uint8
			for i := 0; i < infoBits; i++ {
				p ^= hp[r][i] & info[i]
			}
			cwPerm[parityCols[r]] = p
		}
		// Un-permute to original ordering: cwOrig[perm[j]] = cwPerm[j].
		for j := 0; j < codewordBits; j++ {
			cwOrig[perm[j]] = cwPerm[j]
		}
		// Metric: sum of |posterior_i| at positions where this codeword
		// disagrees with the hard decision (i.e., flipped bits).
		var m float64
		for i := 0; i < codewordBits; i++ {
			if cwOrig[i] != hardOrig[i] {
				m += absPosOrig[i]
			}
		}
		if m < bestMetric {
			bestMetric = m
			bestCw = cwOrig
		}

		// CRC-pass instrumentation. First 91 bits of cwOrig are the
		// systematic info word; check CRC14 and track the lowest
		// metric among passes. Does not affect bestCw / bestMetric.
		//
		// Build-tag-gated (see osd_instr_{off,on}.go): the per-
		// candidate CRC14 call costs ~12% of total runtime on the
		// real-capture corpus; production runs default to OFF and
		// pay nothing. The compiler eliminates the entire block
		// (including the Hamming-distance second pass and the
		// infoCheck array allocation) when osdInstrEnabled is the
		// const false.
		if osdInstrEnabled {
			var hammingDist int
			for i := 0; i < codewordBits; i++ {
				if cwOrig[i] != hardOrig[i] {
					hammingDist++
				}
			}
			var infoCheck [infoBits]uint8
			for i := 0; i < infoBits; i++ {
				infoCheck[i] = cwOrig[i]
			}
			if crc14Matches(infoCheck) {
				crcPassCount++
				if m < bestCRCMetric {
					bestCRCMetric = m
					bestCRCHamming = hammingDist
				}
			}
		}
	}

	// Test-pattern enumeration. `info` starts at the MRB hard
	// decision and is mutated in place — each iteration flips its
	// trial bits, calls scoreOne, then unflips before moving on.
	// This keeps `info` synchronised with mrbHard between trials
	// without the 91-byte array copy that a re-assignment would
	// cost (saves ~4186 copies over the full enumeration).
	info = mrbHard

	// OSD-0: MRB hard-decision as-is.
	scoreOne()

	// OSD-1: single-bit flips.
	for f1 := 0; f1 < infoBits; f1++ {
		info[f1] ^= 1
		scoreOne()
		info[f1] ^= 1
	}

	// OSD-2: pair flips. Outer flip is "set then restore"; the inner
	// loop walks the second bit through positions > f1 (so each
	// unordered pair is visited exactly once).
	for f1 := 0; f1 < infoBits; f1++ {
		info[f1] ^= 1
		for f2 := f1 + 1; f2 < infoBits; f2++ {
			info[f2] ^= 1
			scoreOne()
			info[f2] ^= 1
		}
		info[f1] ^= 1
	}

	// Populate diag with the final enumeration stats. mlMetric is
	// the metric of bestCw — what OSD's current return policy uses.
	diag.explored = explored
	diag.crcPassCount = crcPassCount
	diag.bestCRCMetric = bestCRCMetric
	diag.bestCRCHamming = bestCRCHamming
	diag.mlMetric = bestMetric

	// Check CRC on the ML candidate. Current return policy: success
	// iff the ML (lowest-metric) candidate is CRC-clean. The
	// instrumentation above records when this policy throws away a
	// CRC-clean codeword that's NOT the ML.
	var infoOut [infoBits]uint8
	for i := 0; i < infoBits; i++ {
		infoOut[i] = bestCw[i]
	}
	if !crc14Matches(infoOut) {
		return Result{}, false, diag
	}
	diag.mlCRCPass = true

	return Result{Info: infoOut, Codeword: bestCw}, true, diag
}

// reliabilityPerm returns a permutation of 0..173 ordered by
// |posterior| descending — perm[0] is the index of the most-reliable
// bit, perm[173] the least. Standard `sort.Slice` is fine here:
// 174 elements, one call per decode.
func reliabilityPerm(posterior [codewordBits]float64) [codewordBits]int {
	var perm [codewordBits]int
	for i := 0; i < codewordBits; i++ {
		perm[i] = i
	}
	sort.Slice(perm[:], func(i, j int) bool {
		return math.Abs(posterior[perm[i]]) > math.Abs(posterior[perm[j]])
	})
	return perm
}

// osdReduce performs column-permuted Gauss-Jordan elimination on H
// to identify the Most-Reliable Basis (MRB) for OSD.
//
// Conceptually H is column-permuted by `perm`, then row-reduced.
// We sweep columns from least-reliable (rightmost in permuted
// order) toward most-reliable. Each column that yields a pivot
// becomes a "parity" column; columns that don't yield pivots (or
// where 83 pivots are already found) become MRB columns. After
// 83 successful pivots, every remaining column is MRB.
//
// On return:
//
//   - mrbCols[0..90]: indices (in permuted column space) of the MRB.
//   - parityCols[0..82]: indices of the parity pivots, in row-order
//     (parityCols[r] is the unique permuted column where reduced
//     row r has its 1).
//   - hp[r][i]: contribution of MRB column i to parity row r. After
//     reduction, hp restricted to MRB columns is the 83×91 P matrix
//     of the systematic encoding.
//   - ok: false iff H is rank-deficient (defensive only; FT8's H
//     always has full row rank 83).
//
// Important: parity-column ordering vs row ordering is locked by
// construction. When the caller derives parity bits, parityCols[r]
// is the codeword position that depends on hp[r] · info.
func osdReduce(perm [codewordBits]int) (hp [parityRows][infoBits]uint8, mrbCols [infoBits]int, parityCols [parityRows]int, ok bool) {
	// Build the full permuted H matrix (83 rows × 174 cols).
	var hFull [parityRows][codewordBits]uint8
	for c := 0; c < codewordBits; c++ {
		origBit := perm[c]
		for _, row := range hColumns[origBit] {
			hFull[row][c] = 1
		}
	}

	var mrbIdx, parityIdx int

	for col := codewordBits - 1; col >= 0; col-- {
		if parityIdx == parityRows {
			// All 83 pivots found — every remaining column is MRB.
			mrbCols[mrbIdx] = col
			mrbIdx++
			continue
		}
		// Look for a pivot in this column among rows [parityIdx..parityRows).
		pivotRow := -1
		for r := parityIdx; r < parityRows; r++ {
			if hFull[r][col] == 1 {
				pivotRow = r
				break
			}
		}
		if pivotRow < 0 {
			// No pivot available in this column → it's an MRB column.
			mrbCols[mrbIdx] = col
			mrbIdx++
			continue
		}
		// Swap pivotRow into the next pivot slot.
		if pivotRow != parityIdx {
			hFull[parityIdx], hFull[pivotRow] = hFull[pivotRow], hFull[parityIdx]
		}
		// Eliminate this column from every other row.
		for r := 0; r < parityRows; r++ {
			if r == parityIdx || hFull[r][col] == 0 {
				continue
			}
			for k := 0; k < codewordBits; k++ {
				hFull[r][k] ^= hFull[parityIdx][k]
			}
		}
		parityCols[parityIdx] = col
		parityIdx++
	}

	if parityIdx != parityRows || mrbIdx != infoBits {
		return hp, mrbCols, parityCols, false
	}

	// Extract the 83×91 P submatrix at MRB columns. After
	// reduction, hFull[r][parityCols[r']] = 1 iff r == r' and
	// hFull[r][parityCols[r']] = 0 otherwise, so hFull restricted
	// to mrbCols carries all the structure we need.
	for r := 0; r < parityRows; r++ {
		for i, mc := range mrbCols {
			hp[r][i] = hFull[r][mc]
		}
	}
	return hp, mrbCols, parityCols, true
}
