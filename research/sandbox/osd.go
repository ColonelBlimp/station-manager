package sandbox

import (
	"math"
	"sort"
)

// OSDOptions tunes the Ordered Statistics Decoder used as the BP
// fallback in BPDecode. OSD is the standard "near-ML" decoding
// technique for short LDPC codes: it sorts received bits by
// reliability, takes the most-reliable 91 as a candidate information
// basis, performs Gaussian elimination on the parity-check matrix to
// reconstruct the corresponding systematic form, and enumerates
// small numbers of bit flips around the resulting hard decision.
//
// The maxosd convention from WSJT-X maps to these options:
//
//	maxosd < 0   Enable = false (BP only)
//	maxosd = 0   Enable = true, Order = 2 (default)
type OSDOptions struct {
	// Enable controls whether OSD runs as a BP fallback. When false,
	// BPDecode returns BP's best-effort result regardless of CRC.
	Enable bool

	// Order is the maximum number of MRB-bit flips to enumerate.
	// Order 0 = 1 candidate, Order 1 = 1 + 91 = 92, Order 2 = 1 + 91 +
	// C(91, 2) = 4187. Higher orders catch BP-fail cases with more
	// errors in the most-reliable positions but the cost grows roughly
	// as 91^order.
	Order int
}

// DefaultOSDOptions returns the baseline: OSD enabled at order 2,
// matching the WSJT-X default for FT8.
func DefaultOSDOptions() OSDOptions {
	return OSDOptions{Enable: true, Order: 2}
}

// runOSD performs Ordered Statistics Decoding on the supplied
// 174-element LLR vector. Returns the decoded codeword + a flag
// indicating whether a CRC-valid candidate was found. The
// dminSoftDistance return value is the soft distance of the
// minimum-distance CRC-valid candidate (useful as a "decoder
// confidence" tag); meaningless when ok == false.
//
// Sign convention matches BP: positive LLR favours bit 0.
//
// Algorithm (per Fossorier & Lin 1995, adapted to the FT8 LDPC
// code's (174, 91) shape):
//
//  1. Sort positions by |LLR| descending. The first 91 are the
//     "Most Reliable Basis" (MRB); remaining 83 are LRB.
//  2. Permute H so MRB columns come first; Gauss-eliminate to
//     [A | I_83] where A is the 83×91 left half. (Column swaps
//     between LRB and MRB occur if needed to make LRB block
//     full-rank.)
//  3. Hard-decide the MRB bits from their LLR signs → info_0.
//  4. Enumerate test patterns S ⊆ MRB with |S| ≤ order. For each S:
//     · info_S = info_0 XOR e_S (flip bits in S).
//     · parity_S = A · info_S (matrix-vector in GF(2)).
//     · Build permuted codeword [info_S | parity_S], de-permute to
//     natural order, check CRC14 on bits[0:91].
//     · If CRC passes, compute soft distance Σ |LLR_v| · [cw_v ≠
//     hard(LLR_v)] and track minimum.
//  5. Return minimum-soft-distance CRC-valid candidate.
func runOSD(llrs [LDPCCodewordBits]float64, order int) (cw [LDPCCodewordBits]uint8, ok bool, dmin float64) {
	// --- Step 1: reliability ordering ---
	type rankEntry struct {
		origIdx int
		absLLR  float64
	}
	rank := make([]rankEntry, LDPCCodewordBits)
	for i, l := range llrs {
		a := l
		if a < 0 {
			a = -a
		}
		rank[i] = rankEntry{i, a}
	}
	sort.Slice(rank, func(i, j int) bool {
		return rank[i].absLLR > rank[j].absLLR
	})
	// perm[i] = original codeword index now sitting at permuted slot i.
	// Slots [0, 91) are the candidate MRB; [91, 174) are LRB.
	perm := make([]int, LDPCCodewordBits)
	for i, e := range rank {
		perm[i] = e.origIdx
	}

	// --- Step 2: build permuted H and Gauss-eliminate ---
	// Dense H from the sparse var↔check graph.
	Hperm := make([][]uint8, LDPCParityRows)
	for c := 0; c < LDPCParityRows; c++ {
		Hperm[c] = make([]uint8, LDPCCodewordBits)
	}
	for v := 0; v < LDPCCodewordBits; v++ {
		for k := 0; k < 3; k++ {
			Hperm[varChecks[v][k]][v] = 1
		}
	}
	// Apply column permutation: column i of Hperm should hold
	// original column perm[i]. Build a fresh permuted copy.
	Hp := make([][]uint8, LDPCParityRows)
	for c := 0; c < LDPCParityRows; c++ {
		Hp[c] = make([]uint8, LDPCCodewordBits)
		for i := 0; i < LDPCCodewordBits; i++ {
			Hp[c][i] = Hperm[c][perm[i]]
		}
	}

	// Gaussian elimination: for each row r, install a pivot at column
	// LDPCInfoBits + r. If no candidate pivot exists in the LRB block
	// (cols >= LDPCInfoBits + r), search the MRB block and swap.
	for r := 0; r < LDPCParityRows; r++ {
		pivotCol := LDPCInfoBits + r
		if Hp[r][pivotCol] != 1 {
			found := -1
			// Search LRB to the right first.
			for col := pivotCol + 1; col < LDPCCodewordBits; col++ {
				if Hp[r][col] == 1 {
					found = col
					break
				}
			}
			if found < 0 {
				// Fall back to MRB. Iterate from the end (least
				// reliable MRB position) so we demote the least-
				// reliable info bit when forced to swap.
				for col := LDPCInfoBits - 1; col >= 0; col-- {
					if Hp[r][col] == 1 {
						found = col
						break
					}
				}
			}
			if found < 0 {
				// Row is all-zero — skip (shouldn't happen for full-rank H).
				continue
			}
			// Swap column found with pivotCol everywhere.
			for cc := 0; cc < LDPCParityRows; cc++ {
				Hp[cc][found], Hp[cc][pivotCol] = Hp[cc][pivotCol], Hp[cc][found]
			}
			perm[found], perm[pivotCol] = perm[pivotCol], perm[found]
		}
		// Eliminate pivotCol from other rows by XORing row r into them.
		for r2 := 0; r2 < LDPCParityRows; r2++ {
			if r2 == r || Hp[r2][pivotCol] != 1 {
				continue
			}
			for c := 0; c < LDPCCodewordBits; c++ {
				Hp[r2][c] ^= Hp[r][c]
			}
		}
	}

	// Now Hp = [A | I_83]. Extract A.
	A := make([][]uint8, LDPCParityRows)
	for r := 0; r < LDPCParityRows; r++ {
		A[r] = make([]uint8, LDPCInfoBits)
		copy(A[r], Hp[r][:LDPCInfoBits])
	}

	// Precompute the column-of-A vectors as A_col[i] = column i of A
	// (length 83). Used for incremental parity updates on bit flips.
	Acol := make([][]uint8, LDPCInfoBits)
	for i := 0; i < LDPCInfoBits; i++ {
		Acol[i] = make([]uint8, LDPCParityRows)
		for r := 0; r < LDPCParityRows; r++ {
			Acol[i][r] = A[r][i]
		}
	}

	// --- Step 3: hard-decide MRB bits ---
	info0 := make([]uint8, LDPCInfoBits)
	for i := 0; i < LDPCInfoBits; i++ {
		if llrs[perm[i]] < 0 {
			info0[i] = 1
		}
	}

	// Compute initial parity = A · info0.
	parity0 := make([]uint8, LDPCParityRows)
	for r := 0; r < LDPCParityRows; r++ {
		var p uint8
		for i := 0; i < LDPCInfoBits; i++ {
			p ^= A[r][i] & info0[i]
		}
		parity0[r] = p
	}

	// Precompute the inverse permutation: invPerm[v] = i such that
	// perm[i] == v. Used to read natural-order bits out of cwPerm =
	// [info | parity].
	invPerm := make([]int, LDPCCodewordBits)
	for i, p := range perm {
		invPerm[p] = i
	}

	// Precompute absLLR for soft-distance computation.
	absLLR := make([]float64, LDPCCodewordBits)
	for v, l := range llrs {
		a := l
		if a < 0 {
			a = -a
		}
		absLLR[v] = a
	}

	// Hard decision per natural-order bit, for soft-distance.
	hardOrig := make([]uint8, LDPCCodewordBits)
	for v, l := range llrs {
		if l < 0 {
			hardOrig[v] = 1
		}
	}

	bestDist := math.Inf(1)
	var bestCW [LDPCCodewordBits]uint8
	found := false

	// trial evaluates a flip pattern given the resulting (info, parity)
	// in permuted coords. Checks CRC; if valid, updates bestCW/bestDist.
	trial := func(info, parity []uint8) {
		// De-permute to natural order to check CRC and compute distance.
		var cw [LDPCCodewordBits]uint8
		for i, p := range perm {
			if i < LDPCInfoBits {
				cw[p] = info[i]
			} else {
				cw[p] = parity[i-LDPCInfoBits]
			}
		}
		var msg91 [LDPCInfoBits]uint8
		copy(msg91[:], cw[:LDPCInfoBits])
		if !VerifyCRC14(msg91) {
			return
		}
		// Soft distance: Σ |LLR_v| · [cw_v ≠ hardOrig_v].
		dist := 0.0
		for v := 0; v < LDPCCodewordBits; v++ {
			if cw[v] != hardOrig[v] {
				dist += absLLR[v]
			}
		}
		if dist < bestDist {
			bestDist = dist
			bestCW = cw
			found = true
		}
	}

	// Order 0: the un-flipped hard decision.
	trial(info0, parity0)

	// Order 1: flip one MRB bit at a time. Incremental parity update.
	if order >= 1 {
		info1 := make([]uint8, LDPCInfoBits)
		parity1 := make([]uint8, LDPCParityRows)
		for i := 0; i < LDPCInfoBits; i++ {
			copy(info1, info0)
			info1[i] ^= 1
			for r := 0; r < LDPCParityRows; r++ {
				parity1[r] = parity0[r] ^ Acol[i][r]
			}
			trial(info1, parity1)
		}
	}

	// Order 2: flip two MRB bits. Incremental: starting from order-1
	// state for first flip i, flipping a second bit j XORs Acol[j] in.
	if order >= 2 {
		info2 := make([]uint8, LDPCInfoBits)
		parity1 := make([]uint8, LDPCParityRows)
		parity2 := make([]uint8, LDPCParityRows)
		for i := 0; i < LDPCInfoBits; i++ {
			// Build parity1 for first flip i.
			for r := 0; r < LDPCParityRows; r++ {
				parity1[r] = parity0[r] ^ Acol[i][r]
			}
			for j := i + 1; j < LDPCInfoBits; j++ {
				copy(info2, info0)
				info2[i] ^= 1
				info2[j] ^= 1
				for r := 0; r < LDPCParityRows; r++ {
					parity2[r] = parity1[r] ^ Acol[j][r]
				}
				trial(info2, parity2)
			}
		}
	}

	// Soft-distance acceptance gate. OSD enumerates ~4187 candidates
	// at order 2, each subject to the 1/16384 CRC14 false-positive
	// rate. A "lottery winner" — random codeword that passes CRC by
	// chance — disagrees with about half the channel evidence on
	// average, so its soft distance hovers near Σ|LLR|/2. A legitimate
	// decode disagrees only at the few channel-error positions, well
	// below the random-codeword floor. The Σ|LLR|-relative cutoff
	// separates the two on FT8 fixtures.
	//
	// Note: when BP fails and the truth codeword isn't reachable by
	// MRB hard decision ± 2 flips, OSD-2 may return a CRC-valid
	// alternative whose soft distance is *small* (it's near the wrong
	// MRB hard decision). The soft-distance gate cannot reject these;
	// they require higher OSD order or per-candidate Costas alignment
	// checks to filter. We accept the residual false-positive rate
	// here — the alternative is a more aggressive cutoff that costs
	// us legitimate marginal decodes.
	if found {
		totalAbs := 0.0
		for _, l := range llrs {
			a := l
			if a < 0 {
				a = -a
			}
			totalAbs += a
		}
		if bestDist > totalAbs*osdAcceptDistanceRatio {
			found = false
		}
	}

	return bestCW, found, bestDist
}

// osdAcceptDistanceRatio is the fraction of total channel evidence
// (Σ|LLR|) above which a CRC-passing OSD candidate is treated as a
// CRC-lottery false positive and rejected.
//
// Tuned empirically on the 10cq fixture set (clean / SNR-16dB /
// SNR-20dB / SNR-22dB): 0.05 cleanly separates legitimate decodes
// (typical ratio 4-5%) from random CRC-passing codewords (ratio 8%+).
// Side-by-side sweep showed:
//
//	thresh 0.05: zero spurious false-positives on noisy fixtures;
//	             8/10 truth on SNR-20dB (one BP-fail decode lost).
//	thresh 0.35: more truth survives but 10 spurious false-positives
//	             appear per noisy fixture.
//
// Stricter is better for FT8: a single wrong-text decode is more
// damaging than a missed legitimate one (which the operator can
// retry next slot).
const osdAcceptDistanceRatio = 0.05
