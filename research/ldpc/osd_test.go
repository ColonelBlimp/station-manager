package ldpc

import (
	"math/rand"
	"testing"
)

// TestOSDReduce_StructuralInvariants pins the partition produced by
// osdReduce: 91 MRB columns + 83 parity columns, no overlap, full
// coverage of all 174 permuted indices. This is the cheapest check
// against an off-by-one or accounting bug — any drift here means
// OSD's encoding is operating on the wrong column sets.
func TestOSDReduce_StructuralInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 10; trial++ {
		var perm [codewordBits]int
		for i := 0; i < codewordBits; i++ {
			perm[i] = i
		}
		rng.Shuffle(codewordBits, func(i, j int) {
			perm[i], perm[j] = perm[j], perm[i]
		})

		_, mrbCols, parityCols, ok := osdReduce(perm)
		if !ok {
			t.Fatalf("trial %d: osdReduce failed (rank deficiency on a permutation it should handle)", trial)
		}

		seen := make(map[int]string, codewordBits)
		for i, c := range mrbCols {
			if c < 0 || c >= codewordBits {
				t.Errorf("trial %d: mrbCols[%d] = %d out of range", trial, i, c)
			}
			if where, dup := seen[c]; dup {
				t.Errorf("trial %d: column %d appears in both mrbCols[%d] and %s", trial, c, i, where)
			}
			seen[c] = "mrbCols"
		}
		for i, c := range parityCols {
			if c < 0 || c >= codewordBits {
				t.Errorf("trial %d: parityCols[%d] = %d out of range", trial, i, c)
			}
			if where, dup := seen[c]; dup {
				t.Errorf("trial %d: column %d appears in both parityCols[%d] and %s", trial, c, i, where)
			}
			seen[c] = "parityCols"
		}
		if got := len(seen); got != codewordBits {
			t.Errorf("trial %d: union covers %d cols, want %d", trial, got, codewordBits)
		}
	}
}

// TestOSDReduce_EncodingProducesValidCodeword is the load-bearing
// correctness check for osdReduce: encode a random MRB info word
// via the systematic encoding it produces (info at MRB positions,
// parity = P · info at parity positions, then un-permute) and verify
// the resulting codeword satisfies H · c = 0.
//
// If P is wrong — e.g., a column got swapped without being eliminated,
// or the parity-pivot row mapping is off — every OSD candidate fails
// the parity check and OSD's ML output is never valid. This test
// catches that class of bug independently of any decode scenario.
func TestOSDReduce_EncodingProducesValidCodeword(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 20; trial++ {
		var perm [codewordBits]int
		for i := 0; i < codewordBits; i++ {
			perm[i] = i
		}
		rng.Shuffle(codewordBits, func(i, j int) {
			perm[i], perm[j] = perm[j], perm[i]
		})

		hp, mrbCols, parityCols, ok := osdReduce(perm)
		if !ok {
			t.Fatalf("trial %d: osdReduce failed", trial)
		}

		// Random 91-bit MRB info.
		var info [infoBits]uint8
		for i := 0; i < infoBits; i++ {
			info[i] = uint8(rng.Intn(2))
		}

		// Systematic encoding: info at MRB, parity via hp at parity cols.
		var cwPerm [codewordBits]uint8
		for i, col := range mrbCols {
			cwPerm[col] = info[i]
		}
		for r := 0; r < parityRows; r++ {
			var p uint8
			for i := 0; i < infoBits; i++ {
				p ^= hp[r][i] & info[i]
			}
			cwPerm[parityCols[r]] = p
		}

		// Un-permute to original ordering.
		var cwOrig [codewordBits]uint8
		for j := 0; j < codewordBits; j++ {
			cwOrig[perm[j]] = cwPerm[j]
		}

		// Verify H · cwOrig = 0 against the original H matrix.
		for r := 0; r < parityRows; r++ {
			var s uint8
			for _, v := range hRows[r] {
				s ^= cwOrig[v]
			}
			if s != 0 {
				t.Errorf("trial %d: parity row %d unsatisfied — encoding is wrong", trial, r)
				break
			}
		}
	}
}

// TestDecode_OSDRecoversFromBitErrors constructs a valid FT8 codeword
// (msg + CRC at positions [0..90], parity at [91..173], satisfying
// H·c=0), converts to high-confidence LLRs, then flips a small
// number of LLR signs to simulate channel errors. Decode should
// still report ConvergedCRC=true, with UsedOSD=true if BP couldn't
// recover alone.
//
// Two careful pieces of bookkeeping in this test:
//
//  1. The OSD encoder's `info` vector is indexed by MRB POSITION
//     (0..90), where MRB position i corresponds to permuted column
//     mrbCols[i]. Under identity permutation, the FT8 info range
//     [0..90] IS the MRB column set, but mrbCols can iterate it in
//     a different order. So `info[i] = codeword[mrbCols[i]]`, not
//     `info[i] = codeword[i]`.
//
//  2. After OSD encoding, parity bits land at permuted indices
//     parityCols[r]. Under identity permutation, those are original
//     positions [91..173] — matching the FT8 systematic layout.
func TestDecode_OSDRecoversFromBitErrors(t *testing.T) {
	rng := rand.New(rand.NewSource(11))

	// Random 77-bit message + the corresponding CRC14 placed at
	// positions [77..90], MSB first (matching crc14Matches's read order).
	var codeword [codewordBits]uint8
	var msg [payloadBits]uint8
	for i := 0; i < payloadBits; i++ {
		msg[i] = uint8(rng.Intn(2))
		codeword[i] = msg[i]
	}
	crc := crc14(msg)
	for i := 0; i < crcBits; i++ {
		codeword[payloadBits+i] = uint8((crc >> uint(crcBits-1-i)) & 1)
	}

	// Identity permutation — keeps original and permuted indices identical
	// so we can reason about positions directly.
	var perm [codewordBits]int
	for i := 0; i < codewordBits; i++ {
		perm[i] = i
	}
	hp, mrbCols, parityCols, ok := osdReduce(perm)
	if !ok {
		t.Fatal("osdReduce failed on identity permutation")
	}

	// Under identity perm, mrbCols should be exactly the set [0..90]
	// (the FT8 info range) and parityCols should be exactly [91..173].
	// Order WITHIN those sets may be reversed by osdReduce's right-to-
	// left sweep, but the partition itself must match the FT8 layout.
	for _, c := range mrbCols {
		if c >= infoBits {
			t.Fatalf("identity-perm mrbCols contains %d (expected all < %d / FT8 info range)", c, infoBits)
		}
	}
	for _, c := range parityCols {
		if c < infoBits {
			t.Fatalf("identity-perm parityCols contains %d (expected all >= %d / FT8 parity range)", c, infoBits)
		}
	}

	// Build the OSD-encoder info vector: info[i] = bit at MRB position i
	// = codeword[mrbCols[i]] (= codeword[mrbCols[i]] since perm is identity).
	var info [infoBits]uint8
	for i, col := range mrbCols {
		info[i] = codeword[col]
	}

	// Compute parity bits and place at parityCols positions.
	for r := 0; r < parityRows; r++ {
		var p uint8
		for i := 0; i < infoBits; i++ {
			p ^= hp[r][i] & info[i]
		}
		codeword[parityCols[r]] = p
	}

	// Verify the constructed codeword satisfies H·c=0 against the
	// original H. If this fails, osdReduce's encoding output is wrong.
	for r := 0; r < parityRows; r++ {
		var s uint8
		for _, v := range hRows[r] {
			s ^= codeword[v]
		}
		if s != 0 {
			t.Fatalf("constructed codeword fails original-H parity row %d — osdReduce encoding bug", r)
		}
	}

	// Convert to high-confidence LLRs: bit 0 → +10, bit 1 → -10.
	// Sign convention matches research/demod.LLRs (positive ⇒ bit 0).
	var llrs [codewordBits]float64
	for i := 0; i < codewordBits; i++ {
		if codeword[i] == 0 {
			llrs[i] = +10.0
		} else {
			llrs[i] = -10.0
		}
	}

	// Sanity: error-free LLRs decode trivially via BP.
	{
		result, stats := Decode(llrs)
		if !stats.ConvergedCRC {
			t.Fatalf("error-free LLRs failed to decode: parity=%v iters=%d", stats.ConvergedParity, stats.Iterations)
		}
		if stats.UsedOSD {
			t.Errorf("error-free LLRs triggered OSD (BP should handle these): iters=%d", stats.Iterations)
		}
		// Decoded info must match the systematic prefix of the codeword.
		for i := 0; i < infoBits; i++ {
			if result.Info[i] != codeword[i] {
				t.Errorf("error-free decode: info bit %d = %d, want %d", i, result.Info[i], codeword[i])
				break
			}
		}
	}

	// Now flip a few LLR signs and confirm Decode still succeeds. Six
	// errors is comfortably within OSD-2's reach (which can correct
	// up to 2 MRB-bit errors plus structural correction from H).
	flipped := llrs
	for _, pos := range []int{3, 17, 42, 73, 110, 150} {
		flipped[pos] = -flipped[pos]
	}
	result, stats := Decode(flipped)
	if !stats.ConvergedCRC {
		t.Fatalf("Decode failed on 6-bit-error LLRs: parity=%v iters=%d osd=%v explored=%d",
			stats.ConvergedParity, stats.Iterations, stats.UsedOSD, stats.OSDCandidatesExplored)
	}
	for i := 0; i < infoBits; i++ {
		if result.Info[i] != codeword[i] {
			t.Errorf("recovered info bit %d = %d, want %d", i, result.Info[i], codeword[i])
			break
		}
	}
	t.Logf("recovered 6-bit-error LLRs: BP iters=%d, OSD used=%v, candidates explored=%d",
		stats.Iterations, stats.UsedOSD, stats.OSDCandidatesExplored)
}

// TestDecode_RandomLLRsWithOSDStillDontFalseAccept extends the
// existing TestDecode_RandomLLRsDoNotConverge to the OSD-enabled
// pipeline. OSD explores ~4187 candidates per failed BP, so the
// pressure on CRC14 as the false-accept gate is higher. A failure
// here would mean OSD is producing CRC-passing codewords from pure
// noise more often than the CRC14 ~2⁻¹⁴ rate allows.
//
// 100 trials with random ±10 LLRs. Expected zero successes.
func TestDecode_RandomLLRsWithOSDStillDontFalseAccept(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 100; trial++ {
		var llrs [codewordBits]float64
		for i := range llrs {
			llrs[i] = rng.Float64()*20 - 10
		}
		_, stats := Decode(llrs)
		if stats.ConvergedCRC {
			t.Errorf("trial %d: random LLRs produced CRC-valid decode via %s (iters=%d, osdExplored=%d)",
				trial,
				map[bool]string{true: "OSD", false: "BP"}[stats.UsedOSD],
				stats.Iterations, stats.OSDCandidatesExplored)
		}
	}
}
