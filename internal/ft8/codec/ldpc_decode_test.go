package codec

import (
	"math/rand/v2"
	"testing"
)

// TestComputeSyndrome_CleanCodewordIsZero pins the basic invariant:
// every codeword produced by LDPCEncode satisfies the parity-check
// equations, so its 83-bit syndrome is all zero.
func TestComputeSyndrome_CleanCodewordIsZero(t *testing.T) {
	// Run a handful of deterministic info patterns through encode →
	// syndrome to catch any matrix-row mis-parse or transposition.
	cases := []struct {
		name string
		info [InfoBits]byte
	}{
		{"all_zero", [InfoBits]byte{}},
		{"all_one", func() [InfoBits]byte {
			var b [InfoBits]byte
			for i := range b {
				b[i] = 1
			}
			return b
		}()},
		{"alternating_01", func() [InfoBits]byte {
			var b [InfoBits]byte
			for i := range b {
				b[i] = byte(i & 1)
			}
			return b
		}()},
		{"random_seed_1", randomInfo(1, 2)},
		{"random_seed_2", randomInfo(99, 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cw := LDPCEncode(tc.info[:])
			syn := computeSyndrome(cw)
			for i, b := range syn {
				if b != 0 {
					t.Errorf("syndrome bit %d = 1 on clean codeword; want 0", i)
				}
			}
		})
	}
}

// TestComputeSyndrome_BitFlipMakesNonZero verifies the parity-check
// machinery responds to errors: flipping any single bit in a clean
// codeword must produce a non-zero syndrome (single-bit errors are
// always detected by the column-density-3 LDPC code).
func TestComputeSyndrome_BitFlipMakesNonZero(t *testing.T) {
	info := randomInfo(42, 43)
	cw := LDPCEncode(info[:])
	for flipIdx := range CodewordBits {
		cw[flipIdx] ^= 1
		syn := computeSyndrome(cw)
		zero := true
		for _, b := range syn {
			if b != 0 {
				zero = false
				break
			}
		}
		if zero {
			t.Errorf("flipping bit %d produced zero syndrome; want non-zero", flipIdx)
		}
		cw[flipIdx] ^= 1 // restore
	}
}

// TestLDPCDecodeBP_CleanCodewordRecovers is the headline regression
// for the decoder: a clean-channel codeword (large-magnitude LLRs
// matching the bit values) must decode in 0-1 iterations with
// converged=true and recover the original codeword exactly.
func TestLDPCDecodeBP_CleanCodewordRecovers(t *testing.T) {
	cases := []struct {
		name string
		info [InfoBits]byte
	}{
		{"all_zero", [InfoBits]byte{}},
		{"random_seed_1", randomInfo(1, 2)},
		{"random_seed_42", randomInfo(42, 43)},
		{"random_seed_99", randomInfo(99, 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cw := LDPCEncode(tc.info[:])
			llrs := codewordToLLRs(cw, 5.0) // strong: ±5 is huge in LLR terms
			got, converged := LDPCDecodeBP(llrs, LDPCMaxIterationsDefault)
			if !converged {
				t.Fatal("LDPCDecodeBP did not converge on clean codeword")
			}
			for i := range CodewordBits {
				if got[i] != cw[i] {
					t.Errorf("decoded bit %d = %d, want %d", i, got[i], cw[i])
				}
			}
		})
	}
}

// TestLDPCDecodeBP_CorrectsSingleBitError verifies the decoder
// recovers from one flipped codeword bit. Single-bit errors are
// well below the LDPC correction capacity.
func TestLDPCDecodeBP_CorrectsSingleBitError(t *testing.T) {
	info := randomInfo(7, 8)
	cw := LDPCEncode(info[:])
	// Sample of error positions: every 17th bit, covers different
	// row-weight neighbourhoods in the parity graph.
	for flipIdx := 0; flipIdx < CodewordBits; flipIdx += 17 {
		llrs := codewordToLLRs(cw, 5.0)
		// Flip the LLR sign at flipIdx — equivalent to inverting the bit.
		llrs[flipIdx] = -llrs[flipIdx]
		got, converged := LDPCDecodeBP(llrs, LDPCMaxIterationsDefault)
		if !converged {
			t.Errorf("flip @ %d: BP did not converge", flipIdx)
			continue
		}
		for i := range CodewordBits {
			if got[i] != cw[i] {
				t.Errorf("flip @ %d: decoded bit %d = %d, want %d", flipIdx, i, got[i], cw[i])
				break
			}
		}
	}
}

// TestLDPCDecodeBP_CorrectsMultipleBitErrors covers a range of error
// counts spread across the codeword. The FT8 (174,91) code's
// minimum distance isn't formally documented in the QEX paper, but
// empirically BP corrects double-digit error counts on most inputs.
// We don't try to find the threshold here — just verify that "a
// reasonable number of errors" still decodes.
func TestLDPCDecodeBP_CorrectsMultipleBitErrors(t *testing.T) {
	info := randomInfo(11, 12)
	cw := LDPCEncode(info[:])
	// 5 errors at spaced positions.
	errPositions := []int{3, 41, 88, 137, 170}
	llrs := codewordToLLRs(cw, 5.0)
	for _, p := range errPositions {
		llrs[p] = -llrs[p]
	}
	got, converged := LDPCDecodeBP(llrs, LDPCMaxIterationsDefault)
	if !converged {
		t.Fatal("BP did not converge on 5-bit error pattern")
	}
	for i := range CodewordBits {
		if got[i] != cw[i] {
			t.Errorf("decoded bit %d = %d, want %d", i, got[i], cw[i])
			break
		}
	}
}

// TestLDPCDecodeBP_RejectsUncorrectableErrors verifies the decoder
// returns converged=false when the error pattern exceeds correction
// capacity (corrupt every other bit — far beyond what any
// reasonable LDPC code can correct). The function must terminate
// gracefully (no infinite loop, no NaN propagation) and return a
// false flag rather than claiming success.
func TestLDPCDecodeBP_RejectsUncorrectableErrors(t *testing.T) {
	info := randomInfo(13, 14)
	cw := LDPCEncode(info[:])
	llrs := codewordToLLRs(cw, 5.0)
	// Flip every other bit — ~87 errors, well beyond correction.
	for i := 0; i < CodewordBits; i += 2 {
		llrs[i] = -llrs[i]
	}
	_, converged := LDPCDecodeBP(llrs, 10) // tight iteration cap to keep test quick
	if converged {
		t.Error("BP claimed convergence on a deeply corrupted codeword; want false")
	}
}

// TestLDPCDecodeBP_TerminatesEarlyOnConvergence pins that the
// iteration cap is an upper bound, not a fixed cost. Clean inputs
// should converge in 0-2 iterations even with maxIterations=200.
// (We can't directly observe the iteration count without modifying
// the function's return signature, but we verify it doesn't run
// forever on a converging input — a regression that disabled the
// syndrome-zero early exit would hang or take much longer here.)
func TestLDPCDecodeBP_TerminatesEarlyOnConvergence(t *testing.T) {
	info := randomInfo(23, 24)
	cw := LDPCEncode(info[:])
	llrs := codewordToLLRs(cw, 5.0)
	got, converged := LDPCDecodeBP(llrs, 200)
	if !converged {
		t.Fatal("BP did not converge")
	}
	for i := range CodewordBits {
		if got[i] != cw[i] {
			t.Errorf("decoded bit %d = %d, want %d", i, got[i], cw[i])
			break
		}
	}
}

// TestLDPCDecodeBP_PanicsOnWrongLength pins the input-length contract.
func TestLDPCDecodeBP_PanicsOnWrongLength(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("LDPCDecodeBP with len(llrs)=42 should panic; did not")
		}
	}()
	LDPCDecodeBP(make([]float64, 42), 10)
}

// --- helpers ---------------------------------------------------------------

// randomInfo deterministically generates a 91-bit info pattern.
// Uses two seeds to thread the PCG generator's state explicitly so
// each test gets a different but reproducible bit pattern.
func randomInfo(seed1, seed2 uint64) [InfoBits]byte {
	r := rand.New(rand.NewPCG(seed1, seed2))
	var b [InfoBits]byte
	for i := range b {
		b[i] = byte(r.UintN(2))
	}
	return b
}

// codewordToLLRs converts a hard-decided codeword (0/1 bytes) to a
// synthetic LLR vector with the given magnitude. Bit 0 → +mag,
// bit 1 → -mag, matching WSJT-X's LLR sign convention.
func codewordToLLRs(cw []byte, mag float64) []float64 {
	llrs := make([]float64, len(cw))
	for i, b := range cw {
		if b == 0 {
			llrs[i] = mag
		} else {
			llrs[i] = -mag
		}
	}
	return llrs
}

// BenchmarkLDPCDecodeBP_CleanCodeword measures the BP decoder cost
// on a converging input. Each iteration over 83 checks × ~6.3
// variables/check × ~3 tanh/atanh ops per (check, variable) ≈ 1500
// transcendental-function calls per BP iteration. The clean-codeword
// case typically converges in 1 iteration, so this measures the
// best-case decoder cost.
func BenchmarkLDPCDecodeBP_CleanCodeword(b *testing.B) {
	info := randomInfo(101, 202)
	cw := LDPCEncode(info[:])
	llrs := codewordToLLRs(cw, 5.0)
	b.ResetTimer()
	for range b.N {
		_, _ = LDPCDecodeBP(llrs, LDPCMaxIterationsDefault)
	}
}

// BenchmarkLDPCDecodeBP_FiveBitErrors measures the converging-but-
// noisy case — 5 bit errors at spaced positions, the same pattern
// the correctness test uses. Typically 3-10 iterations; representative
// of a "tough but recoverable" decode.
func BenchmarkLDPCDecodeBP_FiveBitErrors(b *testing.B) {
	info := randomInfo(101, 202)
	cw := LDPCEncode(info[:])
	llrs := codewordToLLRs(cw, 5.0)
	errPositions := []int{3, 41, 88, 137, 170}
	for _, p := range errPositions {
		llrs[p] = -llrs[p]
	}
	b.ResetTimer()
	for range b.N {
		_, _ = LDPCDecodeBP(llrs, LDPCMaxIterationsDefault)
	}
}
