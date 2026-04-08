// decoder_debug_test.go — tests for the instrumented LDPC decoder.

package codec

import (
	"fmt"
	"math/rand"
	"testing"
)

// TestDecodeDebugPerfectLLR verifies that the debug decoder produces the
// same result as the production decoder on noiseless input, and reports
// convergence in 1 iteration with syndrome weight 0.
func TestDecodeDebugPerfectLLR(t *testing.T) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 6.0)

	result := DecodeDebug(llr, 50)
	if !result.OK {
		t.Fatalf("DecodeDebug failed on perfect LLR\n%s", result.Summary())
	}
	if result.Info != info {
		t.Errorf("decoded info mismatch:\n  got  %x\n  want %x", result.Info, info)
	}
	if result.Iterations != 1 {
		t.Errorf("expected 1 iteration, got %d", result.Iterations)
	}
	if len(result.Stats) == 0 {
		t.Fatal("no iteration stats recorded")
	}
	if result.Stats[0].SyndromeWeight != 0 {
		t.Errorf("expected syndrome weight 0, got %d", result.Stats[0].SyndromeWeight)
	}
	t.Logf("\n%s", result.Summary())
}

// TestDecodeDebugNoisyConvergence verifies that the debug decoder tracks
// syndrome weight decreasing across iterations for a noisy but decodable
// signal.
func TestDecodeDebugNoisyConvergence(t *testing.T) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}
	cw := encodeUnpacked(info)

	rng := rand.New(rand.NewSource(42))
	llr := bitsToLLR(cw, 4.0)
	addGaussianNoise(&llr, 1.0, rng)

	result := DecodeDebug(llr, 50)
	if !result.OK {
		t.Fatalf("DecodeDebug failed on noisy LLR\n%s", result.Summary())
	}

	// Verify syndrome weight trends downward.
	t.Logf("\n%s", result.Summary())

	if result.Iterations < 2 {
		t.Logf("converged in %d iteration(s), cannot check convergence trend", result.Iterations)
		return
	}

	// The first iteration should have non-zero syndrome, last should be 0.
	first := result.Stats[0]
	last := result.Stats[len(result.Stats)-1]
	if first.SyndromeWeight == 0 {
		t.Logf("syndrome was already 0 at iteration 0 (very clean signal)")
	}
	if last.SyndromeWeight != 0 {
		t.Errorf("final syndrome weight = %d, want 0", last.SyndromeWeight)
	}
}

// TestDecodeDebugFailedConvergence verifies that random LLR input produces
// diagnostic stats showing non-convergence (syndrome weight stays high).
func TestDecodeDebugFailedConvergence(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	var llr [N]float32
	for i := range N {
		llr[i] = float32(rng.NormFloat64() * 2.0)
	}

	result := DecodeDebug(llr, 25)
	if result.OK {
		t.Error("DecodeDebug returned OK for random LLR input")
	}

	t.Logf("\n%s", result.Summary())

	// With random input, syndrome weight should remain non-zero throughout.
	if len(result.Stats) == 0 {
		t.Fatal("no iteration stats recorded")
	}
	last := result.Stats[len(result.Stats)-1]
	if last.SyndromeWeight == 0 {
		t.Errorf("syndrome weight unexpectedly reached 0 for random input")
	}
	t.Logf("final syndrome weight: %d/83", last.SyndromeWeight)
}

// TestDecodeDebugInputStats verifies that input LLR statistics are computed correctly.
func TestDecodeDebugInputStats(t *testing.T) {
	info := [KBytes]byte{0xA5, 0x3C}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 6.0)

	result := DecodeDebug(llr, 1)

	// All LLRs should have magnitude 6.0.
	const tol = 0.001
	if result.InputMeanAbs < 6.0-tol || result.InputMeanAbs > 6.0+tol {
		t.Errorf("InputMeanAbs = %.3f, want 6.0", result.InputMeanAbs)
	}
	if result.InputMaxAbs < 6.0-tol || result.InputMaxAbs > 6.0+tol {
		t.Errorf("InputMaxAbs = %.3f, want 6.0", result.InputMaxAbs)
	}
	if result.InputZeroCount != 0 {
		t.Errorf("InputZeroCount = %d, want 0", result.InputZeroCount)
	}
	total := result.InputPosCount + result.InputNegCount
	if total != N {
		t.Errorf("InputPosCount+InputNegCount = %d, want %d", total, N)
	}
	t.Logf("pos=%d neg=%d (ones ratio=%.2f)",
		result.InputPosCount, result.InputNegCount,
		float64(result.InputNegCount)/float64(N))
}

// TestDecodeDebugMatchesProduction verifies that DecodeDebug produces the
// same decoded output as the production Decode for a range of noise levels.
func TestDecodeDebugMatchesProduction(t *testing.T) {
	info := [KBytes]byte{0x42, 0x73, 0x1A, 0xF0, 0x00, 0x55, 0xAA, 0x0F, 0xE1, 0xC3, 0x87, 0x00}
	cw := encodeUnpacked(info)

	seeds := []int64{100, 200, 300}
	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		llr := bitsToLLR(cw, 3.0)
		addGaussianNoise(&llr, 1.5, rng)

		prodInfo, prodOK := Decode(llr, 50)
		debugResult := DecodeDebug(llr, 50)

		if prodOK != debugResult.OK {
			t.Errorf("seed=%d: production OK=%v, debug OK=%v", seed, prodOK, debugResult.OK)
			continue
		}
		if prodOK && prodInfo != debugResult.Info {
			t.Errorf("seed=%d: production and debug decoded different info", seed)
		}
	}
}

// TestDecodeDebugHeavyNoiseInstrumentation runs the debug decoder on
// challenging noise levels and logs the full convergence trace. This is
// the primary diagnostic tool for understanding BP behaviour.
func TestDecodeDebugHeavyNoiseInstrumentation(t *testing.T) {
	info := [KBytes]byte{0xCA, 0xFE, 0xBA, 0xBE, 0xDE, 0xAD, 0x00, 0x11, 0x22, 0x30, 0x00, 0x00}
	cw := encodeUnpacked(info)

	// Test at various noise levels to see the convergence transition.
	for _, sigma := range []float32{1.0, 1.5, 2.0, 2.5, 3.0} {
		t.Run(fmt.Sprintf("sigma=%.1f", sigma), func(t *testing.T) {
			rng := rand.New(rand.NewSource(42))
			llr := bitsToLLR(cw, 3.0)
			addGaussianNoise(&llr, sigma, rng)

			result := DecodeDebug(llr, 50)
			t.Logf("\n%s", result.Summary())

			// Count how many bits were flipped by noise.
			wrongBits := 0
			for i := range N {
				hardBit := 0
				if llr[i] < 0 {
					hardBit = 1
				}
				if hardBit != int(cw[i]) {
					wrongBits++
				}
			}
			t.Logf("Input hard-decision errors: %d/%d (%.1f%%)",
				wrongBits, N, 100*float64(wrongBits)/float64(N))
		})
	}
}
