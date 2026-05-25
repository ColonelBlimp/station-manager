package demod

import (
	"math"
	"math/rand"
	"testing"
)

// synthCostasPhase fills the audio buffer so each Costas anchor's
// expected-tone signal has the precise phase the linear model
// predicts at the START of that anchor's window:
//
//	target_phase(sym, tone) = phi0 + slope·sym + γ·tone
//
// γ = 2π·baud·(0.5+dtSec) matches the term fitCostasPhase subtracts
// before fitting. Each anchor window holds a clean cosine at the
// expected tone frequency (freqHz + tone·baud); other windows /
// gaps are left as zeros so the Goertzel at each anchor sees a
// pure single-tone signal.
//
// This produces the "synthetic ideal" the fit must recover exactly.
// Realistic signals will have non-zero RMSResid; the unit test
// pinning recovery within ~1e-3 is a bug guard, not a performance
// target.
func synthCostasPhase(freqHz, dtSec, phi0, slope float64) []float32 {
	txStart := int(math.Round((synthSlotStartSec + dtSec) * fs))
	gamma := 2 * math.Pi * baud * (synthSlotStartSec + dtSec)

	// Buffer extends through the end of sym 78 plus a small tail.
	bufLen := txStart + nn*nsps + 1
	samples := make([]float32, bufLen)

	for i := 0; i < costasAnchors; i++ {
		sym := costasSym[i]
		tone := costasExpectedTone[i]
		target := phi0 + slope*float64(sym) + gamma*float64(tone)
		f := freqHz + float64(tone)*baud
		omega := 2 * math.Pi * f / fs
		windowStart := txStart + sym*nsps
		for j := 0; j < nsps; j++ {
			samples[windowStart+j] = float32(math.Cos(omega*float64(j) + target))
		}
	}
	return samples
}

// equalModulo2Pi reports whether two angles agree modulo 2π within tol.
func equalModulo2Pi(a, b, tol float64) bool {
	diff := math.Mod(a-b, 2*math.Pi)
	if diff > math.Pi {
		diff -= 2 * math.Pi
	} else if diff <= -math.Pi {
		diff += 2 * math.Pi
	}
	return math.Abs(diff) < tol
}

// TestFitCostasPhase_RecoversKnownPhaseModel pins the load-bearing
// recovery property: a synthetic signal whose Costas anchors
// EXACTLY match the linear-phase model (phi0 + slope·sym + γ·tone)
// must be fitted back to that model within tight numerical
// tolerance, and the residual must be near zero.
//
// Verifies the algorithm end-to-end: complex Goertzel, γ·tone
// correction, weighted LS, residual computation. A failure here
// points at either the model (wrong γ formula) or the LS arithmetic.
func TestFitCostasPhase_RecoversKnownPhaseModel(t *testing.T) {
	cases := []struct {
		name   string
		freqHz float64
		dtSec  float64
		phi0   float64
		slope  float64
	}{
		{"on_grid_zero", 1000.0, 0.0, 0.0, 0.0},
		{"on_grid_phi0_pi4_no_slope", 1000.0, 0.0, math.Pi / 4, 0.0},
		{"on_grid_small_slope", 1000.0, 0.0, 0.0, 0.02},
		{"on_grid_negative_slope", 1000.0, 0.0, 1.2, -0.03},
		{"on_grid_phi0_and_slope", 1000.0, 0.0, math.Pi / 3, 0.05},
		{"off_grid_dt_+0.1", 1000.0, 0.1, 0.4, 0.01},
		{"off_grid_dt_-0.1", 1000.0, -0.1, -0.5, -0.02},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			samples := synthCostasPhase(c.freqHz, c.dtSec, c.phi0, c.slope)
			fit := fitCostasPhase(samples, c.freqHz, c.dtSec)

			if fit.AccessibleAnchors != costasAnchors {
				t.Errorf("AccessibleAnchors = %d, want %d", fit.AccessibleAnchors, costasAnchors)
			}
			if !equalModulo2Pi(fit.Phi0, c.phi0, 1e-3) {
				t.Errorf("Phi0 = %g, want %g (mod 2π)", fit.Phi0, c.phi0)
			}
			if math.Abs(fit.Slope-c.slope) > 1e-4 {
				t.Errorf("Slope = %g, want %g (Δ=%g)", fit.Slope, c.slope, fit.Slope-c.slope)
			}
			if fit.RMSResid > 1e-3 {
				t.Errorf("RMSResid = %g, want ≤ 1e-3 for synthetic-ideal input", fit.RMSResid)
			}
			t.Logf("phi0=%.6g slope=%.6g rms=%.3g acc=%d", fit.Phi0, fit.Slope, fit.RMSResid, fit.AccessibleAnchors)
		})
	}
}

// TestFitCostasPhase_PureNoiseHasHighRMSResid confirms the fallback
// guard: a buffer of pure Gaussian noise (no signal) produces low
// weights at every anchor (since no tone dominates), and the fit
// either has near-zero accessible anchors with weight > 0 OR a
// large RMSResid. Either path lets the caller decide to fall back
// to incoherent demod.
func TestFitCostasPhase_PureNoiseHasHighRMSResid(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	txStart := int(math.Round(synthSlotStartSec * fs))
	bufLen := txStart + nn*nsps + 1
	samples := make([]float32, bufLen)
	for i := range samples {
		samples[i] = float32(rng.NormFloat64() * 0.1)
	}

	fit := fitCostasPhase(samples, 1000.0, 0.0)

	// On pure noise, the expected-tone log-contrast should be ~0
	// (no tone dominates). With weight ≤ 0 clamped to 0, most
	// anchors should contribute zero weight. RMSResid could be
	// anything (Inf if too few accessible; large finite if some
	// scrape through with small positive weight).
	totalWeight := 0.0
	for _, w := range fit.weights {
		totalWeight += w
	}
	t.Logf("noise fit: phi0=%.3g slope=%.3g rms=%.3g total weight=%.3g accAnchors=%d",
		fit.Phi0, fit.Slope, fit.RMSResid, totalWeight, fit.AccessibleAnchors)

	// Don't make a strict assertion on RMSResid (noise realisation
	// can luck into a low value); just confirm we get either Inf
	// or a "noticeably large" residual or total weight ≤ a tiny
	// number. This is a smoke test: caller will compare RMSResid
	// against a threshold (TBD) when implementing the fallback.
	if !math.IsInf(fit.RMSResid, 1) && fit.RMSResid < 0.1 && totalWeight > 1.0 {
		t.Errorf("noise produced RMSResid=%g and totalWeight=%g — fit looks too confident on pure noise",
			fit.RMSResid, totalWeight)
	}
}

// TestFitCostasPhase_SlopeMatchesFreqOffset is a sanity check on the
// physical interpretation of Slope: for a real-signal frequency
// offset Δf from freqHz, the predicted slope is
//
//	Slope = 2π · Δf · NSPS / fs
//
// I.e., the phase accumulates by 2π·Δf radians per second, and one
// symbol is NSPS/fs seconds. Pin the relationship using the same
// synthetic-ideal generator with different (freqHz, slope) pairs.
func TestFitCostasPhase_SlopeMatchesFreqOffset(t *testing.T) {
	// 0.5 Hz frequency offset → slope = 2π·0.5·1920/12000 = 2π·0.08 = π/6.25
	for _, deltaFHz := range []float64{0.0, 0.1, 0.3, 0.5, -0.2, -0.5} {
		expectedSlope := 2 * math.Pi * deltaFHz * nsps / fs
		samples := synthCostasPhase(1000.0, 0.0, 0.5, expectedSlope)
		fit := fitCostasPhase(samples, 1000.0, 0.0)
		if math.Abs(fit.Slope-expectedSlope) > 1e-4 {
			t.Errorf("ΔfHz=%+g: Slope=%g, want %g (Δ=%g)",
				deltaFHz, fit.Slope, expectedSlope, fit.Slope-expectedSlope)
		}
	}
}
