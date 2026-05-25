package demod

import (
	"math"
	"math/rand"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// readWAV wraps internal/audio.ReadWAV with a returns-just-the-samples
// signature for test brevity.
func readWAV(path string) ([]float32, error) {
	d, err := audio.ReadWAV(path)
	if err != nil {
		return nil, err
	}
	return d.Samples, nil
}

// synthCostasPhase generates an FSK-like signal where each Costas
// anchor's window holds a clean cosine whose START-of-window phase
// matches the linear model phase(sym) = phi0 + slope·sym. This
// uses CUMULATIVE-PHASE FSK so the signal is phase-continuous
// across symbol boundaries — same shape real FT8 signals have, and
// the shape fitCostasPhase's model assumes.
//
// `slope` here is the phase-per-symbol the fit should recover.
// Internally that maps to a carrier-frequency offset
//
//	freqOffset = slope / (2π · T_sym)
//
// which is then folded into the cumulative phase walk. The fit
// sees the resulting Costas anchor phases and recovers (phi0, slope).
//
// Data symbols (non-Costas positions) are left silent. The fit only
// reads Costas anchor windows.
func synthCostasPhase(freqHz, dtSec, phi0, slope float64) []float32 {
	txStart := int(math.Round((synthSlotStartSec + dtSec) * fs))
	bufLen := txStart + nn*nsps + 1
	samples := make([]float32, bufLen)

	// Slope per sym → carrier-frequency offset. The phase model
	// is phase(s) = phi0 + α·s with α = 2π·f_carrier·T_sym (mod 2π).
	// For freqHz an integer multiple of baud, 2π·freqHz·T_sym ≡ 0
	// (mod 2π), so the residual α corresponds to a small offset.
	freqOffset := slope / (2 * math.Pi * float64(nsps) / fs)
	fCarrier := freqHz + freqOffset

	// Walk symbol-by-symbol, integrating phase across the boundaries.
	phi := phi0 - 2*math.Pi*fCarrier*float64(txStart)/fs // back out the cumulative phase from t=0 to txStart
	// so that phi at sym=0's window-start is exactly phi0.
	// (The integration is: phi at window start of sym = phi at t=0
	// + 2π·f_carrier·t_start(s). We want phi at sym 0 = phi0, so
	// phi at t=0 must be phi0 - 2π·f_carrier·t_start(0).)
	_ = phi // re-assigned below in the loop start

	// Reset: track absolute phase from t=0.
	phiAt0 := phi0 - 2*math.Pi*fCarrier*float64(txStart)/fs
	for sym := 0; sym < nn; sym++ {
		if !isCostasInTest(sym) {
			continue
		}

		// Tone for this Costas anchor.
		var symInBlock int
		switch {
		case sym < costasSymbolsPerBlock:
			symInBlock = sym
		case sym >= costasBlockStride && sym < costasBlockStride+costasSymbolsPerBlock:
			symInBlock = sym - costasBlockStride
		default:
			symInBlock = sym - 2*costasBlockStride
		}
		tone := icos7[symInBlock]

		toneFreq := fCarrier + float64(tone)*baud
		omega := 2 * math.Pi * toneFreq / fs
		windowStart := txStart + sym*nsps

		// Phase at window start = phiAt0 + 2π·f_carrier·t_start(s),
		// since the cumulative tone-only term ≡ 0 mod 2π for FT8.
		phaseAtStart := phiAt0 + 2*math.Pi*fCarrier*float64(windowStart)/fs
		for j := 0; j < nsps; j++ {
			samples[windowStart+j] = float32(math.Cos(phaseAtStart + omega*float64(j)))
		}
	}
	return samples
}

// isCostasInTest mirrors demod.isCostas for the test helper.
func isCostasInTest(sym int) bool {
	if sym < costasSymbolsPerBlock {
		return true
	}
	if sym >= costasBlockStride && sym < costasBlockStride+costasSymbolsPerBlock {
		return true
	}
	if sym >= 2*costasBlockStride && sym < 2*costasBlockStride+costasSymbolsPerBlock {
		return true
	}
	return false
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
			// Phi0 represents Goertzel's measured phase at sym 0's
			// window, which is (window-start phase) + slope/2 for a
			// frequency-offset sinusoid. Our synth puts phi0 at the
			// window START, so the fit recovers phi0 + slope/2.
			// Data-symbol coherent demod cancels this offset because
			// the same convention applies on both sides.
			if !equalModulo2Pi(fit.Phi0-c.slope/2, c.phi0, 1e-3) {
				t.Errorf("Phi0 - slope/2 = %g, want %g (mod 2π)", fit.Phi0-c.slope/2, c.phi0)
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

// TestDemodCoherent_CleanFixtureProducesUsableLLRs runs the coherent
// pipeline end-to-end on a real synthetic fixture: clean 10cq WAV
// → candidates.Find → DemodCoherent → LLRsCoherent. The clean
// fixture's signals decode in 1 BP iteration via the incoherent
// path; the coherent path must produce LLRs that are still
// high-confidence (the bulk at |LLR| = ±20 clamp), with a healthy
// phase fit (low RMSResid).
//
// Doesn't test LDPC decode directly (that would need ldpc imported
// from this package's tests — possible but cleaner to keep that
// integration in research/cmd/decode-eval). What this DOES pin:
// the coherent-demod numerics are well-conditioned on a strong
// signal, not just on the synthetic-ideal phase-fit test.
func TestDemodCoherent_CleanFixtureProducesUsableLLRs(t *testing.T) {
	// Use the in-package clean fixture path. The other demod test
	// files use "../10cq_clean.wav" from the package directory.
	wavPath := "../10cq_clean.wav"
	data, err := readWAV(wavPath)
	if err != nil {
		t.Skipf("clean fixture not available at %s: %v", wavPath, err)
	}

	// Run on the first known signal: CQ K1JT FN20 at 500 Hz dt=0.
	const (
		freqHz = 500.0
		dtSec  = 0.0
	)
	metrics, fit := DemodCoherent(data, freqHz, dtSec)

	if fit.AccessibleAnchors != costasAnchors {
		t.Errorf("AccessibleAnchors = %d, want %d", fit.AccessibleAnchors, costasAnchors)
	}
	if fit.RMSResid > 0.5 {
		t.Errorf("phase-fit RMSResid = %g, want ≤ 0.5 rad on clean fixture", fit.RMSResid)
	}
	t.Logf("clean fixture coherent: phi0=%.3g slope=%.3g rms=%.3g acc=%d",
		fit.Phi0, fit.Slope, fit.RMSResid, fit.AccessibleAnchors)

	// Quick smoke: the bulk of metrics should be non-trivial in
	// magnitude. Strict LLR-clamping behaviour is exercised by the
	// decode-eval integration test in research/cmd/decode-eval.
	llrs := LLRsCoherent(metrics)
	atClamp := 0
	for _, v := range llrs {
		if math.Abs(v) >= llrClamp-1e-9 {
			atClamp++
		}
	}
	t.Logf("coherent LLRs at clamp: %d / 174", atClamp)
	if atClamp < 120 {
		t.Errorf("clean fixture: only %d / 174 LLRs at clamp — coherent path too tentative", atClamp)
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
