package demod

import (
	"math"
	"testing"
)

// TestFitCostasPhasePiecewise_CleanSignal pins the synthetic-ideal
// behaviour: a signal at the candidate's exact freq (zero slope by
// construction) should produce a piecewise fit where each block
// has slope ≈ 0, both bridge slopes ≈ 0, both bridge deltas ≈ 0,
// and max block RMS is tiny.
func TestFitCostasPhasePiecewise_CleanSignal(t *testing.T) {
	samples := synthCostasPhase(1000.0, 0.0, 0.5, 0.0) // phi0=0.5, slope=0
	fit := fitCostasPhasePiecewise(samples, 1000.0, 0.0)

	if fit.AccessibleAnchors != costasAnchors {
		t.Fatalf("AccessibleAnchors = %d, want %d", fit.AccessibleAnchors, costasAnchors)
	}
	for b := 0; b < numCostasBlocks; b++ {
		if math.Abs(fit.BlockFits[b].Slope) > 1e-6 {
			t.Errorf("block %d slope = %g, want ≈ 0 on clean signal", b, fit.BlockFits[b].Slope)
		}
		if fit.BlockFits[b].RMSResid > 1e-6 {
			t.Errorf("block %d RMS = %g, want ≈ 0 on clean signal", b, fit.BlockFits[b].RMSResid)
		}
	}
	if math.Abs(fit.BridgeSlope01) > 1e-6 || math.Abs(fit.BridgeSlope12) > 1e-6 {
		t.Errorf("bridge slopes = %g, %g, want ≈ 0", fit.BridgeSlope01, fit.BridgeSlope12)
	}
	if fit.BridgeDelta01 > 1e-6 || fit.BridgeDelta12 > 1e-6 {
		t.Errorf("bridge deltas = %g, %g, want ≈ 0", fit.BridgeDelta01, fit.BridgeDelta12)
	}
	if fit.MaxBlockRMSResid > 1e-6 {
		t.Errorf("MaxBlockRMSResid = %g, want ≈ 0", fit.MaxBlockRMSResid)
	}
	if piecewiseFallback(fit) {
		t.Error("piecewiseFallback returned true on clean signal")
	}
}

// TestFitCostasPhasePiecewise_LinearOffset confirms a constant
// linear slope across the entire transmission produces:
//   - Identical slope in all three block fits
//   - Bridge slopes equal to that slope (since avg of equal slopes is the same)
//   - Bridge deltas ≈ 0
//
// I.e., piecewise reduces to "the same slope everywhere" when phase
// IS linear-across-78-syms — the same regime where the linear-global
// model would also work.
func TestFitCostasPhasePiecewise_LinearOffset(t *testing.T) {
	// slope = 0.05 rad/sym across all 79 syms — well within wrap budget.
	const slope = 0.05
	samples := synthCostasPhase(1000.0, 0.0, 0.0, slope)
	fit := fitCostasPhasePiecewise(samples, 1000.0, 0.0)

	for b := 0; b < numCostasBlocks; b++ {
		if math.Abs(fit.BlockFits[b].Slope-slope) > 1e-4 {
			t.Errorf("block %d slope = %g, want %g", b, fit.BlockFits[b].Slope, slope)
		}
	}
	if math.Abs(fit.BridgeSlope01-slope) > 1e-3 {
		t.Errorf("BridgeSlope01 = %g, want %g", fit.BridgeSlope01, slope)
	}
	if math.Abs(fit.BridgeSlope12-slope) > 1e-3 {
		t.Errorf("BridgeSlope12 = %g, want %g", fit.BridgeSlope12, slope)
	}
	if fit.BridgeDelta01 > 1e-3 || fit.BridgeDelta12 > 1e-3 {
		t.Errorf("bridge deltas = %g, %g, want ≈ 0 (linear signal across all syms)",
			fit.BridgeDelta01, fit.BridgeDelta12)
	}
	if piecewiseFallback(fit) {
		t.Errorf("piecewiseFallback returned true on linear-offset signal (deltas: %g, %g; maxRMS: %g)",
			fit.BridgeDelta01, fit.BridgeDelta12, fit.MaxBlockRMSResid)
	}
}

// TestFitCostasPhasePiecewise_WrapBridge pins the bridge unwrap
// behaviour. With a moderately large slope (0.3 rad/sym), the
// 30-sym bridge accumulates 9 rad of phase, which is more than 2π.
// Raw cmplx.Phase outputs would wrap; our predict-and-unwrap step
// chooses the right 2π branch for block 1's anchors.
//
// If the bridge unwrap were broken, the per-block slope would still
// match `slope` (since it fits within-block diffs), but the bridge
// slope would land at slope ± 2π/30 ≈ 0.21 — different enough to
// fail the sanity check.
func TestFitCostasPhasePiecewise_WrapBridge(t *testing.T) {
	const slope = 0.3 // 30·0.3 = 9 rad bridge accumulation, > 2π
	samples := synthCostasPhase(1000.0, 0.0, 0.0, slope)

	fit := fitCostasPhasePiecewise(samples, 1000.0, 0.0)

	if math.Abs(fit.BridgeSlope01-slope) > 1e-3 {
		t.Errorf("BridgeSlope01 = %g, want %g — bridge unwrap likely broken", fit.BridgeSlope01, slope)
	}
	if math.Abs(fit.BridgeSlope12-slope) > 1e-3 {
		t.Errorf("BridgeSlope12 = %g, want %g — bridge unwrap likely broken", fit.BridgeSlope12, slope)
	}
	if fit.BridgeDelta01 > 1e-2 || fit.BridgeDelta12 > 1e-2 {
		t.Errorf("bridge deltas = %g, %g, want small — bridge unwrap likely broken",
			fit.BridgeDelta01, fit.BridgeDelta12)
	}
	t.Logf("9-rad bridge unwrapped cleanly: slopes %.4f / %.4f, deltas %.4f / %.4f",
		fit.BridgeSlope01, fit.BridgeSlope12, fit.BridgeDelta01, fit.BridgeDelta12)
}

// TestPiecewiseFallback_ThresholdGates pins the fallback predicate's
// behaviour at the threshold boundaries.
func TestPiecewiseFallback_ThresholdGates(t *testing.T) {
	cases := []struct {
		name string
		fit  PhaseFitPiecewise
		want bool
	}{
		{
			"clean", PhaseFitPiecewise{
				AccessibleAnchors: costasAnchors,
				MaxBlockRMSResid:  0.01, BridgeDelta01: 0.01, BridgeDelta12: 0.01,
			}, false,
		},
		{
			"max_block_rms_high", PhaseFitPiecewise{
				AccessibleAnchors: costasAnchors,
				MaxBlockRMSResid:  piecewiseMaxBlockRMSThreshold + 0.01,
			}, true,
		},
		{
			"bridge_delta01_high", PhaseFitPiecewise{
				AccessibleAnchors: costasAnchors,
				MaxBlockRMSResid:  0.1, BridgeDelta01: piecewiseBridgeDeltaThreshold + 0.01,
			}, true,
		},
		{
			"bridge_delta12_high", PhaseFitPiecewise{
				AccessibleAnchors: costasAnchors,
				MaxBlockRMSResid:  0.1, BridgeDelta12: piecewiseBridgeDeltaThreshold + 0.01,
			}, true,
		},
		{
			"insufficient_anchors", PhaseFitPiecewise{
				AccessibleAnchors: 20,
				MaxBlockRMSResid:  0.01,
			}, true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := piecewiseFallback(c.fit); got != c.want {
				t.Errorf("piecewiseFallback = %v, want %v", got, c.want)
			}
		})
	}
}

// TestFitCostasPhasePiecewise_CleanFixture is the integration test
// against the real synthetic clean fixture (10cq_clean.wav). All
// 21 anchors must be accessible at any of the 10 known signal
// positions, max block RMS should be tiny (clean signal), bridge
// deltas should be near zero. If THIS fails, something structural
// is wrong even though the synthetic-ideal tests pass.
func TestFitCostasPhasePiecewise_CleanFixture(t *testing.T) {
	wavPath := "../10cq_clean.wav"
	data, err := readWAV(wavPath)
	if err != nil {
		t.Skipf("clean fixture not available at %s: %v", wavPath, err)
	}
	const freqHz = 500.0
	const dtSec = 0.0
	fit := fitCostasPhasePiecewise(data, freqHz, dtSec)

	if fit.AccessibleAnchors != costasAnchors {
		t.Errorf("AccessibleAnchors = %d, want %d", fit.AccessibleAnchors, costasAnchors)
	}
	if fit.MaxBlockRMSResid > 0.1 {
		t.Errorf("MaxBlockRMSResid = %g, want ≤ 0.1 on clean fixture", fit.MaxBlockRMSResid)
	}
	if fit.BridgeDelta01 > 0.05 || fit.BridgeDelta12 > 0.05 {
		t.Errorf("bridge deltas = %g, %g, want ≤ 0.05 on clean fixture",
			fit.BridgeDelta01, fit.BridgeDelta12)
	}
	if piecewiseFallback(fit) {
		t.Errorf("piecewiseFallback returned true on clean fixture (deltas %g, %g; maxRMS %g)",
			fit.BridgeDelta01, fit.BridgeDelta12, fit.MaxBlockRMSResid)
	}
	t.Logf("clean fixture piecewise: block slopes %.4f / %.4f / %.4f, bridge slopes %.4f / %.4f, deltas %.4f / %.4f, maxRMS %.4f",
		fit.BlockFits[0].Slope, fit.BlockFits[1].Slope, fit.BlockFits[2].Slope,
		fit.BridgeSlope01, fit.BridgeSlope12,
		fit.BridgeDelta01, fit.BridgeDelta12,
		fit.MaxBlockRMSResid)
}
