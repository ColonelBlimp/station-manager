// spectrum_test.go — tests for power spectrum computation.

package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

// --- PowerSpectrum tests ---

// TestPowerSpectrumNil verifies that nil/empty input returns nil.
func TestPowerSpectrumNil(t *testing.T) {
	if ps := PowerSpectrum(nil); ps != nil {
		t.Errorf("PowerSpectrum(nil) = %v, want nil", ps)
	}
	if ps := PowerSpectrum([]complex64{}); ps != nil {
		t.Errorf("PowerSpectrum([]) = %v, want nil", ps)
	}
}

// TestPowerSpectrumLength verifies the output length matches the input.
func TestPowerSpectrumLength(t *testing.T) {
	for _, n := range []int{1, 5, 33, 1025} {
		bins := make([]complex64, n)
		ps := PowerSpectrum(bins)
		if len(ps) != n {
			t.Errorf("len(PowerSpectrum) = %d for %d bins, want %d", len(ps), n, n)
		}
	}
}

// TestPowerSpectrumPureReal verifies that purely real bins produce re².
func TestPowerSpectrumPureReal(t *testing.T) {
	bins := []complex64{3 + 0i, -4 + 0i, 0 + 0i, 0.5 + 0i}
	ps := PowerSpectrum(bins)

	want := []float32{9, 16, 0, 0.25}
	for i, w := range want {
		if !approxEq(ps[i], w, float32Eps) {
			t.Errorf("ps[%d] = %g, want %g", i, ps[i], w)
		}
	}
}

// TestPowerSpectrumPureImag verifies that purely imaginary bins produce im².
func TestPowerSpectrumPureImag(t *testing.T) {
	bins := []complex64{0 + 3i, 0 - 4i, 0 + 0i}
	ps := PowerSpectrum(bins)

	want := []float32{9, 16, 0}
	for i, w := range want {
		if !approxEq(ps[i], w, float32Eps) {
			t.Errorf("ps[%d] = %g, want %g", i, ps[i], w)
		}
	}
}

// TestPowerSpectrumComplex verifies re² + im² for general complex values.
func TestPowerSpectrumComplex(t *testing.T) {
	bins := []complex64{3 + 4i, 1 - 1i, -2 + 3i}
	ps := PowerSpectrum(bins)

	want := []float32{25, 2, 13} // 9+16, 1+1, 4+9
	for i, w := range want {
		if !approxEq(ps[i], w, float32Eps) {
			t.Errorf("ps[%d] = %g, want %g", i, ps[i], w)
		}
	}
}

// TestPowerSpectrumNonNegative verifies that all power values are ≥ 0.
func TestPowerSpectrumNonNegative(t *testing.T) {
	bins := []complex64{-3 - 4i, 0, 1 + 1i, -0.5 + 0.5i}
	ps := PowerSpectrum(bins)
	for i, v := range ps {
		if v < 0 {
			t.Errorf("ps[%d] = %g < 0", i, v)
		}
	}
}

// TestPowerSpectrumZero verifies that zero bins produce zero power.
func TestPowerSpectrumZero(t *testing.T) {
	bins := []complex64{0, 0, 0}
	ps := PowerSpectrum(bins)
	for i, v := range ps {
		if v != 0 {
			t.Errorf("ps[%d] = %g, want 0", i, v)
		}
	}
}

// TestPowerSpectrumConsistentWithFFT verifies that the power spectrum of a
// known sinusoid FFT has energy concentrated at the correct bin.
func TestPowerSpectrumConsistentWithFFT(t *testing.T) {
	const n = 64
	const k = 5

	samples := make([]float32, n)
	for i := range samples {
		samples[i] = float32(math.Cos(
			2 * math.Pi * float64(k) * float64(i) / float64(n)))
	}

	bins := RealFFT(samples)
	ps := PowerSpectrum(bins)

	// Expected power at bin k: |N/2|² = (32)² = 1024.
	wantPower := float32(n*n) / 4.0
	if !approxEq(ps[k], wantPower, 1.0) {
		t.Errorf("ps[%d] = %g, want %g", k, ps[k], wantPower)
	}

	// All other bins should have ~0 power.
	for i, v := range ps {
		if i == k {
			continue
		}
		if v > 1.0 {
			t.Errorf("ps[%d] = %g, want ~0 (leakage)", i, v)
		}
	}
}

// TestPowerSpectrumParseval verifies that the power spectrum preserves the
// Parseval energy identity when summed correctly.
func TestPowerSpectrumParseval(t *testing.T) {
	const n = 64
	samples := make([]float32, n)
	for i := range samples {
		samples[i] = float32(0.5*math.Cos(2*math.Pi*3*float64(i)/float64(n)) +
			0.3*math.Sin(2*math.Pi*11*float64(i)/float64(n)))
	}

	// Time-domain energy.
	var timeEnergy float64
	for _, s := range samples {
		timeEnergy += float64(s) * float64(s)
	}

	bins := RealFFT(samples)
	ps := PowerSpectrum(bins)
	nBins := len(ps)

	// Frequency-domain energy from one-sided power spectrum.
	var freqEnergy float64
	freqEnergy += float64(ps[0])       // DC
	freqEnergy += float64(ps[nBins-1]) // Nyquist
	for k := 1; k < nBins-1; k++ {
		freqEnergy += 2 * float64(ps[k]) // positive + negative
	}
	freqEnergy /= float64(n)

	if !approxEq64(timeEnergy, freqEnergy, timeEnergy*1e-4+1e-10) {
		t.Errorf("Parseval mismatch: time=%g, freq=%g", timeEnergy, freqEnergy)
	}
}

// --- LogPowerSpectrum tests ---

// TestLogPowerSpectrumNil verifies nil/empty handling.
func TestLogPowerSpectrumNil(t *testing.T) {
	if ps := LogPowerSpectrum(nil, -120); ps != nil {
		t.Errorf("LogPowerSpectrum(nil) = %v, want nil", ps)
	}
	if ps := LogPowerSpectrum([]complex64{}, -120); ps != nil {
		t.Errorf("LogPowerSpectrum([]) = %v, want nil", ps)
	}
}

// TestLogPowerSpectrumKnownValues verifies dB values against hand-computed
// references.
func TestLogPowerSpectrumKnownValues(t *testing.T) {
	bins := []complex64{
		complex(10, 0),  // |X|² = 100 → 10·log10(100) = 20 dB
		complex(1, 0),   // |X|² = 1   → 10·log10(1)   = 0 dB
		complex(0.1, 0), // |X|² = 0.01 → 10·log10(0.01) = −20 dB
		complex(3, 4),   // |X|² = 25  → 10·log10(25) ≈ 13.979 dB
	}

	ps := LogPowerSpectrum(bins, -200)

	want := []float64{20, 0, -20, 10 * math.Log10(25)}
	for i, w := range want {
		if !approxEq(ps[i], float32(w), 0.01) {
			t.Errorf("ps[%d] = %g dB, want %g dB", i, ps[i], w)
		}
	}
}

// TestLogPowerSpectrumFloor verifies that zero-power bins are clamped to
// the specified floor.
func TestLogPowerSpectrumFloor(t *testing.T) {
	bins := []complex64{0, 0, 1 + 0i}
	ps := LogPowerSpectrum(bins, -150)

	if ps[0] != -150 {
		t.Errorf("zero-power bin: got %g dB, want -150 dB", ps[0])
	}
	if ps[1] != -150 {
		t.Errorf("zero-power bin: got %g dB, want -150 dB", ps[1])
	}
	// Non-zero bin should not be clamped.
	if !approxEq(ps[2], 0, 0.01) {
		t.Errorf("unit-power bin: got %g dB, want 0 dB", ps[2])
	}
}

// TestLogPowerSpectrumConsistency verifies that LogPowerSpectrum matches
// 10·log10(PowerSpectrum) for non-zero bins.
func TestLogPowerSpectrumConsistency(t *testing.T) {
	bins := []complex64{3 + 4i, 1 - 1i, -2 + 3i, 0.5 + 0i}
	ps := PowerSpectrum(bins)
	lps := LogPowerSpectrum(bins, -200)

	for i, p := range ps {
		if p == 0 {
			continue
		}
		want := float32(10 * math.Log10(float64(p)))
		if !approxEq(lps[i], want, 0.01) {
			t.Errorf("bin[%d]: LogPower=%g, 10·log10(Power)=%g", i, lps[i], want)
		}
	}
}

// --- Cross-check: PowerSpectrum matches manual |X|² from FFT ---

// TestPowerSpectrumMatchesCmplxAbs verifies that PowerSpectrum output
// matches cmplx.Abs()² computed independently.
func TestPowerSpectrumMatchesCmplxAbs(t *testing.T) {
	bins := []complex64{3 + 4i, -1 + 2i, 0, 5 - 12i, 0.1 + 0.2i}
	ps := PowerSpectrum(bins)

	for i, b := range bins {
		absVal := cmplx.Abs(complex128(b))
		want := float32(absVal * absVal)
		if !approxEq(ps[i], want, float32Eps) {
			t.Errorf("ps[%d] = %g, cmplx.Abs²=%g", i, ps[i], want)
		}
	}
}
