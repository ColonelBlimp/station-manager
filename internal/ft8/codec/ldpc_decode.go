package codec

// LDPC decoder via belief propagation per QEX paper §6.
//
// Input: 174 log-likelihood ratios (LLRs), one per codeword bit. The
// convention matches WSJT-X: **positive LLR ⟹ bit-is-0 favoured;
// negative ⟹ bit-is-1 favoured**. The soft demodulator estimates
// these from observed channel-symbol correlations (see Taylor 2020
// §6 for the L_j formula).
//
// Output: the 174-bit hard-decided codeword and a converged flag.
// The first InfoBits (91) bits of the codeword are the systematic
// information word (77-bit message + 14-bit CRC). On converged=true
// the codeword satisfies the parity-check equations (zero syndrome);
// callers chain CRC14 verification to gate semantic acceptance.
//
// **Algorithm.** Standard sum-product BP on the bipartite graph
// defined by ldpcParity:
//
//   - Initialize variable→check messages from input LLRs.
//   - For each iteration:
//     1. Check→variable update via the tanh product rule:
//        m_c→v = 2·atanh(∏_{v'∈N(c)\{v}} tanh(m_v'→c / 2)).
//        Computed efficiently per check via prefix/suffix products
//        so each excluded-variable case is O(1) instead of O(|N(c)|).
//        Each tanh-product is clamped to [-(1-ε), 1-ε] before atanh
//        to keep numerics finite when tanh(m/2) → ±1.
//     2. Variable→check update via the standard sum form:
//        m_v→c = LLR_in[v] + ∑_{c'∈N(v)\{c}} m_c'→v.
//        Computed via "total minus excluded" using the per-variable
//        posterior LLR.
//     3. Hard-decide and check the syndrome. If syndrome = 0,
//        terminate early with converged=true.
//   - After maxIterations without convergence, return the current
//     hard-decision with converged=false. Callers can chain OSD
//     for the harder cases (Taylor 2020 §6 — not implemented in
//     this commit; BP alone catches the vast majority of decodes
//     and is the right starting point per the AWGN thresholds in
//     QEX Table 5).
//
// **What this function does NOT do.** The soft-demodulator producing
// the LLRs is a separate piece (FT8 channel symbol → 3 soft bits per
// symbol via Taylor 2020 §6's L_j formula). CRC14 validation lives
// at the message-extraction layer (LDPCDecode wrapper, future). This
// function is the pure BP step.

// LDPCMaxIterationsDefault is the default iteration cap for
// LDPCDecodeBP — Taylor 2020 §6 says "in practice we find that
// most received signals are decoded with just a few iterations of
// the BP algorithm". 50 is a safety bound; convergence almost
// always happens by iteration ~5 on real signals.
const LDPCMaxIterationsDefault = 50

// llrClamp bounds the tanh-product magnitude before atanh to keep
// the BP update numerically finite. tanh approaches ±1 asymptotically
// — at m≈18 the value is within 2^-26 of 1 and atanh(±1) = ±Inf,
// poisoning subsequent iterations. ε = 1e-15 ≈ 2^-50 is well below
// the float64 precision floor for FT8-scale LLR magnitudes.
const llrClamp = 1.0 - 1e-15

// LDPCDecodeBP runs belief-propagation decoding on a 174-element
// slice of soft decisions and returns the recovered codeword + a
// flag indicating whether the parity-check equations were satisfied
// (zero syndrome) at termination.
//
// The codeword is in bit-per-byte form (each byte 0 or 1), matching
// the project convention. Even when converged=false the returned
// slice is the current hard-decision — useful for OSD post-processing
// or for inspection.
//
// Panics if len(llrs) != CodewordBits. The 174-element contract is
// upstream-guaranteed by the demodulator; a length mismatch here is
// a programmer bug, not user data.
func LDPCDecodeBP(llrs []float64, maxIterations int) ([]byte, bool) {
	codeword, converged, _ := ldpcDecodeBPCore(llrs, maxIterations)
	return codeword, converged
}

// ldpcDecodeBPCore is the shared BP implementation behind
// LDPCDecodeBP (legacy 2-return signature) and LDPCDecodeBPWithStats
// (3-return signature including the iteration count). Returns the
// final hard-decided codeword, the convergence flag, and the number
// of iterations actually run.
func ldpcDecodeBPCore(llrs []float64, maxIterations int) (codeword []byte, converged bool, itersRun int) {
	if len(llrs) != CodewordBits {
		panic("codec.LDPCDecodeBP: llrs must be exactly " + itoaCodewordBits + " long")
	}
	if maxIterations <= 0 {
		maxIterations = LDPCMaxIterationsDefault
	}

	// Variable→check messages: one per (variable, check-slot) edge.
	// Each variable has exactly LDPCParityColumnDensity (=3) edges.
	var mVtoC [CodewordBits][LDPCParityColumnDensity]float64
	for v := range CodewordBits {
		for k := range LDPCParityColumnDensity {
			mVtoC[v][k] = llrs[v]
		}
	}

	// Check→variable messages, sized per check (row weight varies 6-7).
	mCtoV := make([][]float64, ParityBits)
	for c := range ParityBits {
		mCtoV[c] = make([]float64, len(ldpcCheckRows[c]))
	}

	decided := make([]byte, CodewordBits)
	// Pre-allocate prefix/suffix scratch buffers sized to the max row
	// weight (7 per QEX paper §3). Re-used across all checks within
	// each iteration to keep the hot loop allocation-free.
	const maxRowWeight = 8
	var prefix, suffix [maxRowWeight + 1]float64

	for iter := 0; iter < maxIterations; iter++ {
		// --- Check-to-variable update -----------------------------
		for c := range ParityBits {
			vars := ldpcCheckRows[c]
			poss := ldpcCheckPos[c]
			n := len(vars)

			// Prefix/suffix products of tanh(m/2) over all variables
			// in this check, so the "excluded variable" product needed
			// for each outgoing message is O(1) instead of O(n).
			prefix[0] = 1.0
			for k := range n {
				t := tanh(mVtoC[vars[k]][poss[k]] / 2)
				prefix[k+1] = prefix[k] * t
			}
			suffix[n] = 1.0
			for k := n - 1; k >= 0; k-- {
				t := tanh(mVtoC[vars[k]][poss[k]] / 2)
				suffix[k] = suffix[k+1] * t
			}
			for k := range n {
				p := prefix[k] * suffix[k+1]
				if p > llrClamp {
					p = llrClamp
				} else if p < -llrClamp {
					p = -llrClamp
				}
				mCtoV[c][k] = 2 * atanh(p)
			}
		}

		// --- Variable-to-check update + hard decision -------------
		// posterior[v] = llrs[v] + ∑_{c ∈ N(v)} m_c→v
		// m_v→c = posterior[v] - m_c→v  (for the specific c)
		for v := range CodewordBits {
			post := llrs[v]
			for k := range LDPCParityColumnDensity {
				c := ldpcParity[v][k]
				varSlot := ldpcVarPos[v][k]
				post += mCtoV[c][varSlot]
			}
			if post < 0 {
				decided[v] = 1
			} else {
				decided[v] = 0
			}
			for k := range LDPCParityColumnDensity {
				c := ldpcParity[v][k]
				varSlot := ldpcVarPos[v][k]
				mVtoC[v][k] = post - mCtoV[c][varSlot]
			}
		}

		// --- Syndrome check ---------------------------------------
		if syndromeZero(decided) {
			return decided, true, iter + 1
		}
	}

	return decided, false, maxIterations
}

// computeSyndrome returns the 83-bit parity-check syndrome of a 174-
// bit codeword candidate. The syndrome is all-zero iff the candidate
// is a valid LDPC codeword. Used by BP convergence checks and by
// any external caller wanting to verify a codeword's structural
// validity independent of the message-level CRC14.
//
// Output bits are in bit-per-byte form (each byte 0 or 1), length
// ParityBits (83). Panics if codeword length is wrong.
func computeSyndrome(codeword []byte) [ParityBits]byte {
	if len(codeword) != CodewordBits {
		panic("codec.computeSyndrome: codeword must be exactly " + itoaCodewordBits + " long")
	}
	var syn [ParityBits]byte
	for v := range CodewordBits {
		if codeword[v] == 0 {
			continue
		}
		for k := range LDPCParityColumnDensity {
			syn[ldpcParity[v][k]] ^= 1
		}
	}
	return syn
}

// syndromeZero is an early-exit variant: checks each parity equation
// against the hard-decided codeword on the fly and returns false the
// moment any check fails. Used in the BP iteration's per-iter
// convergence test where most iterations either converge or fail on
// the first check — full syndrome computation is wasted work.
func syndromeZero(codeword []byte) bool {
	for c := range ParityBits {
		var bit byte
		for _, v := range ldpcCheckRows[c] {
			bit ^= codeword[v]
		}
		if bit != 0 {
			return false
		}
	}
	return true
}

// itoaCodewordBits is the precomputed string form of CodewordBits.
// Used by panic messages in the hot path to avoid a strconv import.
const itoaCodewordBits = "174"

// LDPCDecode is the headline decode entry point: run BP on 174 LLRs,
// extract the systematic 91-bit info word from the recovered codeword,
// split it into the 77-bit message body + 14-bit received CRC, and
// validate the CRC over the message. Returns the 77-bit message body
// (bit-per-byte form) and an ok flag that's true iff BP converged
// AND the CRC matched.
//
// Combined-pass acceptance per QEX paper §6 ("If the decoder returns
// a codeword whose 77-bit decoded CRC matches the decoded CRC, the
// algorithm terminates and the decoded message is unpacked and
// displayed to the user."). Both gates are required:
//
//   - BP convergence (zero parity-check syndrome) means the recovered
//     bits ARE a valid LDPC codeword. Without it the first 91 bits
//     are likely garbage even if a few happen to form a self-consistent
//     CRC pair.
//   - CRC validation catches the case where BP converged to a
//     valid-but-wrong codeword (LDPC's decision space has many
//     codewords; the CRC is the semantic-layer tiebreaker).
//
// On ok=false the returned slice is nil. Callers that want the BP
// hard-decision regardless of CRC for diagnostic / OSD post-processing
// should call LDPCDecodeBP directly.
//
// Panics if len(llrs) != CodewordBits (the BP layer's contract).
func LDPCDecode(llrs []float64, maxIterations int) ([]byte, bool) {
	codeword, converged := LDPCDecodeBP(llrs, maxIterations)
	if !converged {
		return nil, false
	}

	// Systematic LDPC: the first InfoBits (91) bits of the codeword
	// are the message+CRC; the remaining 83 are computed parity.
	msg := codeword[:MessageBits]
	rxCRC := codeword[MessageBits:InfoBits]

	expected := CRC14(msg)
	received := packBitsMSBFirst(rxCRC)

	if expected != received {
		return nil, false
	}

	// Return a copy of the message slice so the caller can hold onto it
	// independently of the BP decoder's internal buffer.
	out := make([]byte, MessageBits)
	copy(out, msg)
	return out, true
}

// LDPCStats reports per-call diagnostics from a single LDPC + OSD
// decode attempt. Populated by LDPCDecodeWithOSDStats. Used for
// calibrating BP early-exit / give-up heuristics.
type LDPCStats struct {
	// BPIterations is the number of BP iterations actually run.
	// Equals the maxIterations argument when BP didn't converge.
	BPIterations int

	// BPSyndromeWeight is the count of unsatisfied parity-check
	// equations at BP exit. Zero ⇔ BPConverged is true.
	BPSyndromeWeight int

	// BPConverged is true when BP found a hard-decided codeword
	// whose syndrome is all-zero.
	BPConverged bool

	// BPCRCValid is true when BPConverged is true AND the codeword's
	// embedded CRC14 matches the CRC computed over its 77-bit message
	// field. Meaningful only when BPConverged is true.
	BPCRCValid bool

	// OSDInvoked is true when BP failed (either no convergence, or
	// CRC mismatch) and the OSD fallback was actually run.
	OSDInvoked bool
}

// LDPCDecodeBPWithStats is the diagnostic-instrumented variant of
// LDPCDecodeBP. Returns the same (codeword, converged) pair plus the
// iteration count consumed and the final syndrome weight at exit.
// On convergence the syndrome weight is zero by construction. On
// non-convergence the weight is the count of violated parity checks
// at the iteration cap — closer to zero means BP got closer to a
// valid codeword.
//
// Panics if len(llrs) != CodewordBits.
func LDPCDecodeBPWithStats(llrs []float64, maxIterations int) (codeword []byte, converged bool, iters int, syndromeWeight int) {
	codeword, converged, iters = ldpcDecodeBPCore(llrs, maxIterations)
	if !converged {
		syn := computeSyndrome(codeword)
		for _, b := range syn {
			if b != 0 {
				syndromeWeight++
			}
		}
	}
	return codeword, converged, iters, syndromeWeight
}

// LDPCDecodeWithOSDStats is the diagnostic-instrumented variant of
// LDPCDecodeWithOSD. Returns the same (msg, ok) pair plus a fully-
// populated LDPCStats. Zero-cost compared to LDPCDecodeWithOSD on
// the success path; the stats fields just record outcomes that the
// non-stats variant would discard.
func LDPCDecodeWithOSDStats(llrs []float64, maxIterations, osdOrder int, osdMaxNormDist float64) ([]byte, bool, LDPCStats) {
	var stats LDPCStats
	codeword, converged, iters, syndromeWeight := LDPCDecodeBPWithStats(llrs, maxIterations)
	stats.BPIterations = iters
	stats.BPSyndromeWeight = syndromeWeight
	stats.BPConverged = converged

	if converged {
		// BP succeeded — try the CRC. If it matches, we're done.
		msg := codeword[:MessageBits]
		rxCRC := codeword[MessageBits:InfoBits]
		expected := CRC14(msg)
		received := packBitsMSBFirst(rxCRC)
		if expected == received {
			stats.BPCRCValid = true
			out := make([]byte, MessageBits)
			copy(out, msg)
			return out, true, stats
		}
		// BP-converged-but-CRC-failed → fall through to OSD if enabled.
	}

	if osdOrder <= 0 {
		return nil, false, stats
	}
	stats.OSDInvoked = true
	msg, ok := osdDecode(llrs, osdOrder, osdMaxNormDist)
	return msg, ok, stats
}

// LDPCDecodeWithOSD tries Belief Propagation first via LDPCDecode;
// if BP fails to converge or its CRC doesn't validate, falls back
// to Ordered Statistics Decoding (OSDDecode) at the given order.
// Use this entry point when the caller wants the BP-then-OSD
// sensitivity gain documented in Taylor 2020 §6 (~1 dB on AWGN).
//
//   - osdOrder = 0: no OSD fallback. Behaves identically to LDPCDecode.
//   - osdOrder = 1: order-1 OSD (single-bit flips, ~91 trials).
//   - osdOrder = 2: order-2 OSD (paired flips, ~4095 trials).
//
// Returns the recovered 77-bit message + true on success; nil, false
// when neither BP nor OSD finds a CRC14-valid codeword.
//
// Panics if len(llrs) != CodewordBits.
// osdMaxNormDist is the B2 soft-distance acceptance ceiling for OSD
// codewords (see osdDecode / softDistanceNorm); <= 0 disables the gate.
// BP decodes are NOT gated — BP convergence + CRC is trustworthy; the
// false positives come from OSD's bit-flip search.
func LDPCDecodeWithOSD(llrs []float64, maxIterations, osdOrder int, osdMaxNormDist float64) ([]byte, bool) {
	if msg, ok := LDPCDecode(llrs, maxIterations); ok {
		return msg, true
	}
	if osdOrder <= 0 {
		return nil, false
	}
	return osdDecode(llrs, osdOrder, osdMaxNormDist)
}

// packBitsMSBFirst converts a bit-per-byte slice to a uint16,
// MSB-first. Input length must be ≤ 16; len > 16 is a programmer
// bug, not user data. Used by the CRC validation path where the
// 14-bit field is in bit-per-byte form.
func packBitsMSBFirst(bits []byte) uint16 {
	var v uint16
	for _, b := range bits {
		v = (v << 1) | uint16(b)
	}
	return v
}
