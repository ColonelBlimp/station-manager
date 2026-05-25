package ldpc

import (
	"math"
	"sort"
)

// osdOrder is the test-pattern enumeration depth. Order 2 means we
// enumerate:
//
//	  1 codeword from the MRB hard decision   (OSD-0)
//	 91 single-bit flips of the MRB           (OSD-1)
//	4095 pair flips of the MRB                (OSD-2 = C(91, 2))
//	----
//	4187 total candidate codewords per OSD call.
//
// Order 3 would add C(91,3) = 121,485 more — roughly 30× the work
// for diminishing returns. Order 2 is the standard cost-conscious
// FT8 setting and the operator's directive for the first cut.
const osdOrder = 2

// osd runs Fossorier-Lin Ordered Statistics Decoding (order=2) on
// the supplied posterior LLRs. Called by Decode when belief
// propagation alone fails to produce a CRC-clean codeword.
//
// Returns the recovered Result, a success boolean (true iff a
// CRC-passing codeword was found), and the number of candidate
// codewords explored. On failure, the returned Result is zero —
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
func osd(posterior [codewordBits]float64) (Result, bool, int) {
	perm := reliabilityPerm(posterior)

	hp, mrbCols, parityCols, ok := osdReduce(perm)
	if !ok {
		// Should never happen for FT8 LDPC (H has full row rank 83).
		// Defensive only.
		return Result{}, false, 0
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
	)

	// scoreOne mutates cwPerm/cwOrig/explored as a side effect and
	// updates bestMetric/bestCw on improvement.
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
	}

	// OSD-0: MRB hard-decision as-is.
	info = mrbHard
	scoreOne()

	// OSD-1: single-bit flips.
	if osdOrder >= 1 {
		for f1 := 0; f1 < infoBits; f1++ {
			info = mrbHard
			info[f1] ^= 1
			scoreOne()
		}
	}

	// OSD-2: pair flips.
	if osdOrder >= 2 {
		for f1 := 0; f1 < infoBits; f1++ {
			for f2 := f1 + 1; f2 < infoBits; f2++ {
				info = mrbHard
				info[f1] ^= 1
				info[f2] ^= 1
				scoreOne()
			}
		}
	}

	// Check CRC on the ML candidate.
	var infoOut [infoBits]uint8
	for i := 0; i < infoBits; i++ {
		infoOut[i] = bestCw[i]
	}
	if !crc14Matches(infoOut) {
		return Result{}, false, explored
	}

	return Result{Info: infoOut, Codeword: bestCw}, true, explored
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
