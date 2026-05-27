package sandbox

import (
	"fmt"
	"math"
	"sort"
)

// BPOptions tunes the sum-product BP decoder. Zero-value fields fall
// back to DefaultBPOptions.
type BPOptions struct {
	// MaxIterations bounds the BP loop. Default 30 — well past the
	// typical 5-15 iterations FT8 BP converges in.
	MaxIterations int

	// TargetMedian sets the post-normalisation target |LLR| median.
	// Default 5.0: a "moderate confidence" value where tanh(LLR/2) is
	// neither saturated nor near zero. Channel LLRs are scaled so
	// that their median magnitude lands here.
	TargetMedian float64

	// ClampMagnitude caps the absolute value of any post-normalisation
	// LLR. Default 20.0 — large enough to keep strong bits "confident"
	// in BP but small enough that tanh(LLR/2) ≈ ±1 doesn't overflow
	// double-precision atanh on combine.
	ClampMagnitude float64

	// OSD configures the Ordered Statistics Decoder fallback. When
	// BP fails to find a CRC-valid codeword within MaxIterations and
	// OSD.Enable is true, OSD runs on the channel LLRs and its result
	// is reported in BPResult.
	OSD OSDOptions
}

// DefaultBPOptions returns the baseline tuning.
func DefaultBPOptions() BPOptions {
	return BPOptions{
		MaxIterations:  30,
		TargetMedian:   5.0,
		ClampMagnitude: 20.0,
		OSD:            DefaultOSDOptions(),
	}
}

// BPResult is the outcome of running BPDecode on a 174-LLR vector.
type BPResult struct {
	// OK is true iff the decode produced a codeword with both a clean
	// syndrome AND a valid CRC14 on the leading 91 bits.
	OK bool

	// Iterations is the number of BP iterations actually run. May be
	// less than MaxIterations if early termination fired.
	Iterations int

	// SyndromeClean is true iff the final hard-decision codeword
	// satisfies every parity-check equation. A syndrome-clean codeword
	// is mathematically a valid LDPC codeword — but FT8 still requires
	// a CRC match to accept the underlying 77-bit message.
	SyndromeClean bool

	// CRCValid is true iff the trailing 14 bits of the 91-bit info word
	// match computeCRC14 over the leading 77 payload bits.
	CRCValid bool

	// Message91 holds the 91-bit info word (77 payload + 14 CRC) from
	// the decoded codeword's first 91 bits. Populated whenever the
	// loop produced a syndrome-clean codeword, regardless of CRC.
	Message91 [LDPCInfoBits]uint8

	// Codeword holds all 174 hard-decision bits from the final BP
	// iteration. Useful for diagnostic comparisons even when OK=false.
	Codeword [LDPCCodewordBits]uint8

	// DecodeMethod records the path that produced this result:
	// "BP" when BP alone reached CRC-valid, "OSD-N" when OSD order N
	// finished the job, "fail" when neither did.
	DecodeMethod string
}

// BPDecode runs sum-product belief propagation on a 174-element soft
// LLR vector and returns the decoded codeword + CRC verification.
//
// Sign convention (matching SoftLLRs): positive LLR favours bit 0,
// negative favours bit 1. The channel LLRs are normalised and clamped
// before BP — raw SoftLLRs magnitudes of 1e9+ would saturate every
// tanh in the first iteration and make the decoder numerically brittle.
//
// Termination:
//
//   - Each iteration, after the variable-side update, hard-decide
//     every bit (sign of posterior). If the syndrome is clean, also
//     check CRC14 on bits[0:91] and return with OK = (CRC valid).
//   - If MaxIterations is reached without a clean syndrome, return
//     the best-effort codeword from the last iteration; OK=false.
//
// Check-to-variable update uses the standard tanh/atanh rule of the
// sum-product algorithm. No damping or normalised min-sum — regular
// BP passes only, per the milestone scope.
func BPDecode(channelLLRs [LDPCCodewordBits]float64, opts BPOptions) BPResult {
	opts = applyBPDefaults(opts)

	llrs := normaliseAndClamp(channelLLRs, opts.TargetMedian, opts.ClampMagnitude)

	// Per-edge check-to-variable messages, indexed by checkVars layout:
	//   cv[c][i] = message from check c to its i-th variable
	//             (= checkVars[c][i]).
	// Initialised to zero — first iteration's posterior is just the
	// channel LLR.
	cv := make([][]float64, LDPCParityRows)
	for c := range cv {
		cv[c] = make([]float64, len(checkVars[c]))
	}

	var posteriors [LDPCCodewordBits]float64
	var hard [LDPCCodewordBits]uint8
	var res BPResult

	for iter := 0; iter < opts.MaxIterations; iter++ {
		// 1. Posterior + hard decision per variable.
		//    z_v = channel[v] + Σ_k cv[varChecks[v][k]][varEdgePos[v][k]]
		for v := 0; v < LDPCCodewordBits; v++ {
			z := llrs[v]
			for k := 0; k < 3; k++ {
				z += cv[varChecks[v][k]][varEdgePos[v][k]]
			}
			posteriors[v] = z
			if z < 0 {
				hard[v] = 1
			} else {
				hard[v] = 0
			}
		}

		// 2. Syndrome check on hard-decided bits.
		//    For each check c, parity = ⊕ over checkVars[c] of hard[v].
		clean := true
		for c := 0; c < LDPCParityRows; c++ {
			var p uint8
			for _, v := range checkVars[c] {
				p ^= hard[v]
			}
			if p != 0 {
				clean = false
				break
			}
		}
		if clean {
			res.SyndromeClean = true
			res.Iterations = iter + 1
			copy(res.Codeword[:], hard[:])
			copy(res.Message91[:], hard[:LDPCInfoBits])
			if VerifyCRC14(res.Message91) {
				res.OK = true
				res.CRCValid = true
				res.DecodeMethod = "BP"
				return res
			}
			// Syndrome-clean but CRC-invalid: this codeword exists in
			// the LDPC code's null space but isn't the right one. Fall
			// through to OSD — OSD may still find a CRC-valid neighbour.
			break
		}

		// 3. Update check-to-variable messages using v→c built on the fly.
		//    V→C message for edge (v=checkVars[c][i], c) = z_v - cv[c][i].
		//    New C→V[c][i] = 2 · atanh(Π_{j ≠ i} tanh(V→C[c][j] / 2)).
		//
		//    Numerical safety:
		//      - V→C messages are clamped to ±ClampMagnitude before tanh.
		//      - The product is clamped just shy of ±1 before atanh.
		for c := 0; c < LDPCParityRows; c++ {
			n := len(checkVars[c])
			vc := make([]float64, n)
			for i := 0; i < n; i++ {
				v := checkVars[c][i]
				x := posteriors[v] - cv[c][i]
				if x > opts.ClampMagnitude {
					x = opts.ClampMagnitude
				} else if x < -opts.ClampMagnitude {
					x = -opts.ClampMagnitude
				}
				vc[i] = x
			}
			for i := 0; i < n; i++ {
				prod := 1.0
				for j := 0; j < n; j++ {
					if j == i {
						continue
					}
					prod *= math.Tanh(vc[j] / 2)
				}
				// Guard atanh against ±1.
				const atanhCap = 0.999999
				if prod > atanhCap {
					prod = atanhCap
				} else if prod < -atanhCap {
					prod = -atanhCap
				}
				cv[c][i] = 2 * math.Atanh(prod)
			}
		}
	}

	// Loop exhausted without CRC-valid — capture best-effort state
	// before optionally falling through to OSD.
	if res.Iterations == 0 {
		res.Iterations = opts.MaxIterations
	}
	copy(res.Codeword[:], hard[:])
	copy(res.Message91[:], hard[:LDPCInfoBits])

	// OSD fallback: try Ordered Statistics Decoding on the channel
	// LLRs. Per the maxosd convention, this is the "BP + OSD with
	// channel LLRs" mode (WSJT-X maxosd = 0).
	if opts.OSD.Enable {
		osdCW, ok, _ := runOSD(channelLLRs, opts.OSD.Order, opts.OSD.AcceptDistanceRatio)
		if ok {
			res.OK = true
			res.SyndromeClean = true // by construction; OSD outputs are codewords
			res.CRCValid = true
			res.Codeword = osdCW
			copy(res.Message91[:], osdCW[:LDPCInfoBits])
			res.DecodeMethod = fmt.Sprintf("OSD-%d", opts.OSD.Order)
			return res
		}
	}

	res.DecodeMethod = "fail"
	return res
}

// normaliseAndClamp rescales the channel LLRs so their median |value|
// equals targetMedian, then clamps the result to ±clampMag. This
// keeps BP's tanh-based update numerically stable regardless of the
// raw SoftLLRs scale (which can be 1e9+ on clean fixtures).
//
// A pathological "all-zero" input is preserved as zero; BP will still
// run, declare every bit ambiguous, and return a syndrome-failure
// result.
func normaliseAndClamp(in [LDPCCodewordBits]float64, targetMedian, clampMag float64) [LDPCCodewordBits]float64 {
	absVals := make([]float64, LDPCCodewordBits)
	for i, l := range in {
		a := l
		if a < 0 {
			a = -a
		}
		absVals[i] = a
	}
	sort.Float64s(absVals)
	median := absVals[LDPCCodewordBits/2]

	var out [LDPCCodewordBits]float64
	if median <= 0 {
		return out
	}
	scale := targetMedian / median
	for i, l := range in {
		v := l * scale
		if v > clampMag {
			v = clampMag
		} else if v < -clampMag {
			v = -clampMag
		}
		out[i] = v
	}
	return out
}

func applyBPDefaults(opts BPOptions) BPOptions {
	d := DefaultBPOptions()
	if opts.MaxIterations == 0 {
		opts.MaxIterations = d.MaxIterations
	}
	if opts.TargetMedian == 0 {
		opts.TargetMedian = d.TargetMedian
	}
	if opts.ClampMagnitude == 0 {
		opts.ClampMagnitude = d.ClampMagnitude
	}
	return opts
}
