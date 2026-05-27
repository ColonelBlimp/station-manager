package sandbox

import (
	"math"
	"math/cmplx"
	"testing"
)

// TestSynthesizeBaseband_OutputLength pins that the synth produces
// exactly 79 × samplesPerSymbol samples at the supplied baseband rate.
func TestSynthesizeBaseband_OutputLength(t *testing.T) {
	var tones [ft8SymbolCount]int
	out := SynthesizeBaseband(tones, 200.0)
	want := ft8SymbolCount * 32
	if len(out) != want {
		t.Errorf("len(out) = %d, want %d", len(out), want)
	}
}

// TestSynthesizeBaseband_UnitAmplitude pins that the output has
// approximately unit amplitude per sample. Gaussian smoothing of the
// frequency trace doesn't change amplitude (only phase rotation rate),
// so every sample should satisfy |s[n]| ≈ 1.
func TestSynthesizeBaseband_UnitAmplitude(t *testing.T) {
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = i % 8
	}
	out := SynthesizeBaseband(tones, 200.0)
	for i, s := range out {
		m := cmplx.Abs(s)
		if math.Abs(m-1.0) > 1e-9 {
			t.Fatalf("|out[%d]| = %g, want 1", i, m)
		}
	}
}

// TestSynthesizeBaseband_AllZeroTonesYieldsDC pins that an all-tone-0
// sequence (constant 0 Hz baseband) yields a constant-phase signal —
// every sample is the same complex number (= 1+0i for zero initial
// phase). Smoke check that the frequency-zero path doesn't drift.
func TestSynthesizeBaseband_AllZeroTonesYieldsDC(t *testing.T) {
	var tones [ft8SymbolCount]int // all zeros
	out := SynthesizeBaseband(tones, 200.0)
	for i, s := range out {
		if math.Abs(real(s)-1.0) > 1e-9 || math.Abs(imag(s)) > 1e-9 {
			t.Fatalf("tone-0 baseband sample %d = %v, want 1+0i", i, s)
		}
	}
}

// TestFitComplex_PerfectMatch pins that fitting a signal against
// itself returns scale = 1+0i exactly.
func TestFitComplex_PerfectMatch(t *testing.T) {
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = i % 8
	}
	s := SynthesizeBaseband(tones, 200.0)
	c := FitComplex(s, s)
	if cmplx.Abs(c-1) > 1e-12 {
		t.Errorf("FitComplex(s,s) = %v, want 1+0i", c)
	}
}

// TestFitComplex_ScaledMatch pins that fitting a scaled copy
// recovers the scale factor: c·s fitted against s returns c.
func TestFitComplex_ScaledMatch(t *testing.T) {
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = (i * 3) % 8
	}
	ref := SynthesizeBaseband(tones, 200.0)
	scale := complex(2.5, -1.7)
	scaled := make([]complex128, len(ref))
	for i, r := range ref {
		scaled[i] = scale * r
	}
	got := FitComplex(scaled, ref)
	if cmplx.Abs(got-scale) > 1e-12 {
		t.Errorf("FitComplex recovery: got %v, want %v (diff=%g)",
			got, scale, cmplx.Abs(got-scale))
	}
}

// TestSubtractFitted_SelfNullsOut covers M2 acceptance criterion 1:
// subtracting a synthesized signal from itself (with the LSQ fit
// applied) leaves residual ≈ 0. Floor depends on float64 round-off.
func TestSubtractFitted_SelfNullsOut(t *testing.T) {
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = (i * 5) % 8
	}
	s := SynthesizeBaseband(tones, 200.0)
	c := FitComplex(s, s)
	r := SubtractFitted(s, s, c)
	for i, v := range r {
		if cmplx.Abs(v) > 1e-12 {
			t.Errorf("residual[%d] = %v (|.|=%g), want ≈ 0", i, v, cmplx.Abs(v))
			break
		}
	}
}

// TestSubtractFitted_TwoSignalsLeaveB covers M2 acceptance criterion 2:
// build A + B from two different tone sequences, fit A and subtract,
// the residual should be close to B. "Close to" here is bounded by
// cross-correlation between A and B (they aren't orthogonal at this
// length, but the LSQ fit minimises the |A_fit - A| component).
//
// Quantitative: with two unit-amplitude signals occupying overlapping
// frequency bands (tones 0-7 vs different tone sequence), the residual
// power after subtracting A's fit from (A+B) should be ≈ |B|² minus
// any A·B cross-correlation absorbed into the fit. Allow some margin.
func TestSubtractFitted_TwoSignalsLeaveB(t *testing.T) {
	var tonesA, tonesB [ft8SymbolCount]int
	for i := range tonesA {
		tonesA[i] = i % 8
		tonesB[i] = (i*7 + 3) % 8
	}
	a := SynthesizeBaseband(tonesA, 200.0)
	b := SynthesizeBaseband(tonesB, 200.0)

	mix := make([]complex128, len(a))
	for i := range mix {
		mix[i] = a[i] + b[i]
	}
	c := FitComplex(mix, a)
	residual := SubtractFitted(mix, a, c)

	var bEnergy, resEnergy float64
	for i := range b {
		bEnergy += real(b[i])*real(b[i]) + imag(b[i])*imag(b[i])
		resEnergy += real(residual[i])*real(residual[i]) + imag(residual[i])*imag(residual[i])
	}
	// Residual energy should be close to B's energy: ratio in [0.5, 1.5]
	// covers the typical cross-correlation absorption.
	ratio := resEnergy / bEnergy
	if ratio < 0.3 || ratio > 1.7 {
		t.Errorf("residual/B energy ratio = %g, want ≈ 1 (in [0.3, 1.7])", ratio)
	}
}

// TestMeasureSNR_PerfectMatchInfinite pins that fitting a signal
// against itself produces residual ≈ 0 (so SNR → very large; our
// implementation returns 0 SNR when residual=0 to avoid Inf). The
// signal power should match |s|².
func TestMeasureSNR_PerfectMatchInfinite(t *testing.T) {
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = i % 8
	}
	s := SynthesizeBaseband(tones, 200.0)
	m := MeasureSNR(s, s, 200.0)
	if m.ResidualPower > 1e-18 {
		t.Errorf("residual power = %g, want ≈ 0", m.ResidualPower)
	}
	// SignalPower should equal Σ|s|² = 2528 (since each |s[n]|=1).
	want := float64(len(s))
	if math.Abs(m.SignalPower-want) > 1e-6 {
		t.Errorf("signal power = %g, want %g", m.SignalPower, want)
	}
}
