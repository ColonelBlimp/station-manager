// spectrogram_test.go — tests for the spectrogram builder.

package dsp

import (
	"math"
	"testing"
)

// --- Nil / edge-case tests ---

func TestSpectrogramNil(t *testing.T) {
	if sg := Spectrogram(nil, 1920, 1920); sg != nil {
		t.Error("Spectrogram(nil) != nil")
	}
	if sg := Spectrogram([]float32{}, 1920, 1920); sg != nil {
		t.Error("Spectrogram([]) != nil")
	}
}

func TestSpectrogramInvalidParams(t *testing.T) {
	samples := make([]float32, 1920)
	if sg := Spectrogram(samples, 0, 1920); sg != nil {
		t.Error("fftSize=0 should return nil")
	}
	if sg := Spectrogram(samples, 1920, 0); sg != nil {
		t.Error("stepSamples=0 should return nil")
	}
	if sg := Spectrogram(samples, -1, 1920); sg != nil {
		t.Error("fftSize<0 should return nil")
	}
	if sg := Spectrogram(samples, 1920, -1); sg != nil {
		t.Error("stepSamples<0 should return nil")
	}
}

func TestSpectrogramTooShort(t *testing.T) {
	// Buffer shorter than one frame.
	samples := make([]float32, 1919)
	if sg := Spectrogram(samples, 1920, 1920); sg != nil {
		t.Error("buffer shorter than stepSamples should return nil")
	}
}

// --- Dimension tests ---

func TestSpectrogramDimensions(t *testing.T) {
	tests := []struct {
		name        string
		nSamples    int
		fftSize     int
		stepSamples int
		wantFrames  int
		wantBins    int
	}{
		// Exactly 1 frame.
		{"one_frame", 1920, 1920, 1920, 1, 1025},
		// Exactly 2 frames.
		{"two_frames", 3840, 1920, 1920, 2, 1025},
		// Trailing samples discarded (1920 + 100 < 2*1920).
		{"partial_discard", 2020, 1920, 1920, 1, 1025},
		// Small power-of-2 sizes.
		{"small_pow2", 32, 8, 8, 4, 5},
		// fftSize > stepSamples (explicit zero-padding to 2048).
		{"zero_padded", 1920, 2048, 1920, 1, 1025},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			samples := make([]float32, tc.nSamples)
			sg := Spectrogram(samples, tc.fftSize, tc.stepSamples)

			if len(sg) != tc.wantFrames {
				t.Fatalf("frames: got %d, want %d", len(sg), tc.wantFrames)
			}
			for i, row := range sg {
				if len(row) != tc.wantBins {
					t.Errorf("frame[%d] bins: got %d, want %d", i, len(row), tc.wantBins)
				}
			}
		})
	}
}

// TestSpectrogramFT8Dimensions verifies the standard FT8 spectrogram size:
// 180 000 samples → 93 frames × 1025 bins.
func TestSpectrogramFT8Dimensions(t *testing.T) {
	samples := make([]float32, WindowSamples)
	sg := Spectrogram(samples, SamplesPerSymbol, SamplesPerSymbol)

	// 180000 / 1920 = 93.75 → 93 full frames.
	wantFrames := 93
	wantBins := NextPow2(SamplesPerSymbol)/2 + 1 // 2048/2+1 = 1025

	if len(sg) != wantFrames {
		t.Errorf("frames: got %d, want %d", len(sg), wantFrames)
	}
	if len(sg) > 0 && len(sg[0]) != wantBins {
		t.Errorf("bins: got %d, want %d", len(sg[0]), wantBins)
	}
}

// --- Silence test ---

// TestSpectrogramSilence verifies that an all-zero buffer produces an
// all-zero spectrogram.
func TestSpectrogramSilence(t *testing.T) {
	samples := make([]float32, 3840) // 2 frames of 1920
	sg := Spectrogram(samples, 1920, 1920)

	for i, row := range sg {
		for k, v := range row {
			if v != 0 {
				t.Errorf("frame[%d] bin[%d] = %g, want 0 (silence)", i, k, v)
				return // one error is enough
			}
		}
	}
}

// --- Sinusoid peak test ---

// TestSpectrogramSinusoidPeak verifies that a constant-frequency sinusoid
// produces a peak at the correct frequency bin in every frame.
func TestSpectrogramSinusoidPeak(t *testing.T) {
	const nFrames = 4
	const step = 1920
	nSamples := nFrames * step

	// Generate a cosine at exactly bin 100 of the 2048-point FFT.
	// Bin frequency = 100 * 12000/2048 ≈ 585.9 Hz.
	fftN := NextPow2(step) // 2048
	binFreq := float64(100) * float64(SampleRate) / float64(fftN)

	samples := make([]float32, nSamples)
	for i := range samples {
		samples[i] = float32(math.Cos(2 * math.Pi * binFreq * float64(i) / float64(SampleRate)))
	}

	sg := Spectrogram(samples, step, step)

	if len(sg) != nFrames {
		t.Fatalf("frames: got %d, want %d", len(sg), nFrames)
	}

	for f, row := range sg {
		// Find the peak bin.
		peakBin := 0
		peakPow := float32(0)
		for k, v := range row {
			if v > peakPow {
				peakPow = v
				peakBin = k
			}
		}

		// The peak should be near bin 100. Although the frequency is chosen
		// to land on exact bin 100 of a 2048-point FFT, the 1920-sample
		// frame contains a non-integer number of cycles at that frequency
		// (1920/2048 × 100 = 93.75 cycles), so the DFT sees a truncated
		// signal and the Hann window spreads energy across neighbouring
		// bins. Allow ±2 bins of tolerance.
		if absDiff(peakBin, 100) > 2 {
			t.Errorf("frame[%d]: peak at bin %d, want near 100", f, peakBin)
		}

		// The peak should be significantly above the noise floor.
		if peakPow < 1.0 {
			t.Errorf("frame[%d]: peak power %g too low", f, peakPow)
		}
	}
}

// --- Window application test ---

// TestSpectrogramWindowApplied verifies that the Hann window is actually
// being applied by checking that a DC signal (all ones) does NOT produce
// the same power in every frame bin as an un-windowed FFT would.
//
// Without windowing, DC input → all energy in bin 0, zero elsewhere.
// With Hann windowing, the DC energy is still concentrated in bin 0 but
// some leaks into neighbouring bins (the Hann main lobe), and the magnitude
// at bin 0 is reduced by the window's coherent gain (~0.5).
func TestSpectrogramWindowApplied(t *testing.T) {
	const step = 64
	samples := make([]float32, step)
	for i := range samples {
		samples[i] = 1.0
	}

	sg := Spectrogram(samples, step, step)
	if len(sg) != 1 {
		t.Fatalf("frames: got %d, want 1", len(sg))
	}

	// Un-windowed DC: bin 0 power = N² = 64² = 4096.
	// With Hann window (coherent gain ~0.5): bin 0 power ≈ (N*0.5)² = 1024.
	// The actual value depends on the exact discrete Hann sum, but it should
	// be significantly less than 4096.
	dcPower := sg[0][0]
	unwindowedDC := float32(step * step)

	if dcPower >= unwindowedDC*0.9 {
		t.Errorf("DC power %g is too close to un-windowed %g — window may not be applied",
			dcPower, unwindowedDC)
	}
	if dcPower < unwindowedDC*0.1 {
		t.Errorf("DC power %g is too low (expected ~25%% of un-windowed %g)",
			dcPower, unwindowedDC)
	}
}

// --- Consistency with manual pipeline ---

// TestSpectrogramMatchesManualPipeline verifies that the Spectrogram output
// matches the result of manually calling HannCoefficients + ApplyWindow +
// RealFFT + PowerSpectrum for each frame.
func TestSpectrogramMatchesManualPipeline(t *testing.T) {
	const step = 64
	const nFrames = 3
	nSamples := nFrames * step

	// Fill with a known signal.
	samples := make([]float32, nSamples)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * 5 * float64(i) / float64(step)))
	}

	sg := Spectrogram(samples, step, step)
	if len(sg) != nFrames {
		t.Fatalf("frames: got %d, want %d", len(sg), nFrames)
	}

	// Manually compute the same pipeline.
	window := HannCoefficients(step)
	for f := range nFrames {
		start := f * step
		frame := make([]float32, step)
		copy(frame, samples[start:start+step])
		ApplyWindow(frame, window)

		bins := RealFFT(frame)
		manual := PowerSpectrum(bins)

		if len(sg[f]) != len(manual) {
			t.Fatalf("frame[%d]: bins %d != manual %d", f, len(sg[f]), len(manual))
		}
		for k := range manual {
			if !approxEq(sg[f][k], manual[k], float32Eps) {
				t.Errorf("frame[%d] bin[%d]: sg=%g, manual=%g", f, k, sg[f][k], manual[k])
			}
		}
	}
}

// --- Zero-padding equivalence ---

// TestSpectrogramZeroPadEquivalence verifies that passing fftSize > stepSamples
// produces the same result as fftSize == stepSamples (since RealFFT pads
// to the same next power of 2 in both cases).
func TestSpectrogramZeroPadEquivalence(t *testing.T) {
	const step = 1920

	// Single frame with a known signal.
	samples := make([]float32, step)
	for i := range samples {
		samples[i] = float32(math.Cos(2 * math.Pi * 500 * float64(i) / float64(SampleRate)))
	}

	sg1920 := Spectrogram(samples, 1920, step)
	sg2048 := Spectrogram(samples, 2048, step)

	if len(sg1920) != 1 || len(sg2048) != 1 {
		t.Fatalf("expected 1 frame each: got %d, %d", len(sg1920), len(sg2048))
	}
	if len(sg1920[0]) != len(sg2048[0]) {
		t.Fatalf("bin counts differ: %d vs %d", len(sg1920[0]), len(sg2048[0]))
	}

	for k := range sg1920[0] {
		if !approxEq(sg1920[0][k], sg2048[0][k], float32Eps) {
			t.Errorf("bin[%d]: fftSize=1920 → %g, fftSize=2048 → %g",
				k, sg1920[0][k], sg2048[0][k])
		}
	}
}

// --- Non-negative output ---

// TestSpectrogramNonNegative verifies that all spectrogram values are ≥ 0.
func TestSpectrogramNonNegative(t *testing.T) {
	samples := make([]float32, WindowSamples)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / float64(SampleRate)))
	}

	sg := Spectrogram(samples, SamplesPerSymbol, SamplesPerSymbol)

	for f, row := range sg {
		for k, v := range row {
			if v < 0 {
				t.Errorf("frame[%d] bin[%d] = %g < 0", f, k, v)
				return
			}
		}
	}
}
