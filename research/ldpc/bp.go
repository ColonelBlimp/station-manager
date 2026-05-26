package ldpc

import "math"

// Decode runs normalised-min-sum belief-propagation on the supplied
// 174 channel LLRs, gating acceptance on CRC14 verification.
//
// Termination rules per the design discussion (Session 87):
//
//   - Each iteration: BP update → hard-decide posterior LLRs →
//     compute syndrome H·ĉ.
//   - If syndrome=0 AND CRC14 passes ⇒ success, return immediately.
//   - If syndrome=0 AND CRC14 fails ⇒ keep iterating — BP sometimes
//     moves away from a wrong-but-parity-clean codeword on later
//     iterations.
//   - After maxIter ⇒ failure, return the best hard-decision seen.
//
// Sign convention matches research/demod.LLRs: positive LLR ⇒ bit 0
// more likely. Output bits are 1 = bit-1, 0 = bit-0.
func Decode(llrs [codewordBits]float64) (Result, Stats) {
	var stats Stats
	stats.BestSyndromeWeight = parityRows + 1 // sentinel — every real value beats this
	// +Inf sentinels for OSD metric fields. Meaning: "no measurement
	// taken." Either OSD didn't run (UsedOSD=false) or it ran and
	// found no CRC-passing candidate (UsedOSD=true, CRCPassingCount=0).
	// Callers should gate on UsedOSD before consuming OSDBestCRCMetric
	// or OSDMLMetric.
	stats.OSDBestCRCMetric = math.Inf(1)
	stats.OSDBestCRCNormMetric = math.Inf(1)
	stats.OSDMLMetric = math.Inf(1)

	// Message state. vToC[v][k] = message from variable v along its
	// k-th edge (to check c = hColumns[v][k]). cToV[c][i] = message
	// from check c along its i-th edge (to variable v = hRows[c][i]).
	// Initial: vToC = channel LLR (intrinsic info only, no incoming
	// extrinsic yet); cToV starts at zero by Go's array semantics.
	var vToC [codewordBits][3]float64
	for v := 0; v < codewordBits; v++ {
		for k := 0; k < 3; k++ {
			vToC[v][k] = llrs[v]
		}
	}
	cToV := make([][]float64, parityRows)
	for c := 0; c < parityRows; c++ {
		cToV[c] = make([]float64, len(hRows[c]))
	}

	var result Result

	for iter := 1; iter <= maxIter; iter++ {
		updateChecks(&vToC, cToV)
		updateVariables(&llrs, &vToC, cToV, &result.Codeword)

		// Syndrome H·ĉ. A bit set means the parity equation at that
		// row is unsatisfied; weight=0 means a valid codeword.
		syndromeWeight := 0
		for c := 0; c < parityRows; c++ {
			var s uint8
			for _, v := range hRows[c] {
				s ^= result.Codeword[v]
			}
			if s != 0 {
				syndromeWeight++
			}
		}

		if syndromeWeight < stats.BestSyndromeWeight {
			stats.BestSyndromeWeight = syndromeWeight
		}
		stats.FinalSyndromeWeight = syndromeWeight
		stats.Iterations = iter

		if syndromeWeight == 0 {
			stats.ConvergedParity = true
			// Pull the 91 info bits out of the systematic prefix.
			for k := 0; k < infoBits; k++ {
				result.Info[k] = result.Codeword[k]
			}
			if crc14Matches(result.Info) {
				stats.ConvergedCRC = true
				return result, stats
			}
			// Parity-clean but CRC-failing: keep iterating.
		}
	}

	// BP exhausted maxIter without a CRC-clean codeword. Fall through
	// to OSD-2 on the BP-final posterior LLRs — those carry the
	// extrinsic accumulation from every iteration and are strictly
	// more informative than channel LLRs alone, even when BP didn't
	// converge.
	var posterior [codewordBits]float64
	for v := 0; v < codewordBits; v++ {
		posterior[v] = llrs[v]
		for k := 0; k < 3; k++ {
			c := hColumns[v][k]
			i := varEdgeRowPos[v][k]
			posterior[v] += cToV[c][i]
		}
	}
	osdResult, osdOK, diag := osd(posterior)
	stats.UsedOSD = true
	stats.OSDCandidatesExplored = diag.explored
	stats.OSDCRCPassingCount = diag.crcPassCount
	stats.OSDBestCRCMetric = diag.bestCRCMetric
	stats.OSDBestCRCHamming = diag.bestCRCHamming
	if diag.bestCRCHamming > 0 {
		stats.OSDBestCRCNormMetric = diag.bestCRCMetric / float64(diag.bestCRCHamming)
	}
	stats.OSDMLMetric = diag.mlMetric
	stats.OSDMLCRCPass = diag.mlCRCPass
	if osdOK {
		// OSD returns a parity-clean codeword by construction (it
		// systematically encodes from the MRB) and has already
		// verified CRC14 internally before returning success.
		result.Codeword = osdResult.Codeword
		result.Info = osdResult.Info
		stats.ConvergedParity = true
		stats.ConvergedCRC = true
		stats.FinalSyndromeWeight = 0
	}
	return result, stats
}

// updateChecks runs one round of check-node updates: for each parity
// row c, the outgoing message along each edge is the
// scaled-min-sum-with-sign of the OTHER incoming variable messages
// on that row. Reads vToC, writes cToV.
//
// Inner loop is O(n²) where n = row weight ∈ {6, 7}, giving ~42 ops
// per check × 83 checks = ~3.5k ops per BP iteration. A standard
// linear-time optimisation (track global min + 2nd-min once, then
// fan out by index) is available if profiling later shows this loop
// matters; for now the explicit O(n²) keeps the algorithm transparent.
func updateChecks(vToC *[codewordBits][3]float64, cToV [][]float64) {
	for c := 0; c < parityRows; c++ {
		row := hRows[c]
		n := len(row)

		// Read incoming v→c messages for this row.
		var incoming [7]float64 // row weight is at most 7
		for i := 0; i < n; i++ {
			v := row[i]
			k := chkEdgeColPos[c][i]
			incoming[i] = vToC[v][k]
		}

		// Outgoing message for each edge: sign-product × min-abs over
		// the OTHER incoming, scaled by msScaleFactor.
		for i := 0; i < n; i++ {
			signProd := 1.0
			minAbs := math.Inf(1)
			for j := 0; j < n; j++ {
				if j == i {
					continue
				}
				if incoming[j] < 0 {
					signProd = -signProd
				}
				a := math.Abs(incoming[j])
				if a < minAbs {
					minAbs = a
				}
			}
			cToV[c][i] = msScaleFactor * signProd * minAbs
		}
	}
}

// updateVariables runs one round of variable-node updates:
// posterior LLR = channel LLR + sum of incoming check messages; the
// outgoing v→c message is posterior minus the incoming c→v on that
// edge (the standard "extrinsic information" pattern that prevents
// a node from echoing its own contribution back into the loop).
//
// Reads llrs + cToV, writes vToC and the hard-decision codeword.
// codeword[v] = 0 when posterior > 0, 1 otherwise (matches the
// positive=bit0 sign convention from research/demod.LLRs).
func updateVariables(llrs *[codewordBits]float64, vToC *[codewordBits][3]float64, cToV [][]float64, codeword *[codewordBits]uint8) {
	for v := 0; v < codewordBits; v++ {
		// Incoming check messages along v's 3 edges.
		var incoming [3]float64
		for k := 0; k < 3; k++ {
			c := hColumns[v][k]
			i := varEdgeRowPos[v][k]
			incoming[k] = cToV[c][i]
		}

		posterior := llrs[v] + incoming[0] + incoming[1] + incoming[2]

		// Extrinsic v→c message on each edge.
		for k := 0; k < 3; k++ {
			vToC[v][k] = posterior - incoming[k]
		}

		if posterior > 0 {
			codeword[v] = 0
		} else {
			codeword[v] = 1
		}
	}
}
