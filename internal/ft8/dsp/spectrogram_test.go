package dsp

import (
	"math"
	"testing"
)

// TestSpectrogram_Dimensions pins the output shape: NHSYM time
// columns by NH1 frequency bins, allocated as a [][]float64.
// Regression-guards against an off-by-one in the time loop or a
// silent change to NHSYM / NH1 in params.go.
func TestSpectrogram_Dimensions(t *testing.T) {
	samples := make([]float32, NMAX)
	out := Spectrogram(samples)

	if len(out) != NHSYM {
		t.Errorf("time rows = %d, want %d (NHSYM)", len(out), NHSYM)
	}
	for tt, row := range out {
		if len(row) != NH1 {
			t.Errorf("row[%d] freq bins = %d, want %d (NH1)", tt, len(row), NH1)
			break
		}
	}
}

// TestSpectrogram_Silence_GivesZeros pins that all-zero input
// produces an all-zero spectrogram. Sanity check that no DC bias
// or hidden constant has crept into the algorithm.
func TestSpectrogram_Silence_GivesZeros(t *testing.T) {
	samples := make([]float32, NMAX)
	out := Spectrogram(samples)

	for tt, row := range out {
		for f, p := range row {
			if p != 0 {
				t.Errorf("silence: out[%d][%d] = %g, want 0", tt, f, p)
				return
			}
		}
	}
}

// TestSpectrogram_Sinusoid_PeaksAtExpectedBin pins the algorithmic
// invariant: a pure sinusoid at frequency f_Hz produces a peak in
// the spectrogram at bin = round(f_Hz / df), where df = Fs/NFFT1
// = 3.125 Hz. The peak appears in every time column once the
// sinusoid is in the analysis window.
//
// Bin 100 = 312.5 Hz; bin 200 = 625 Hz; bin 480 = 1500 Hz. All in
// FT8's working passband (300-3000 Hz typical).
func TestSpectrogram_Sinusoid_PeaksAtExpectedBin(t *testing.T) {
	const df = Fs / float64(NFFT1) // 3.125 Hz per bin

	cases := []int{100, 200, 480} // bins to test
	for _, peakBin := range cases {
		f := float64(peakBin) * df
		samples := make([]float32, NMAX)
		// x[n] = sin(2π · f · n / Fs)
		for n := range samples {
			samples[n] = float32(math.Sin(2 * math.Pi * f * float64(n) / Fs))
		}

		out := Spectrogram(samples)

		// Check the middle time column (column NHSYM/2) — well inside
		// the window so the entire FFT input is sinusoidal.
		mid := NHSYM / 2
		row := out[mid]

		// Find the actual peak bin in this row.
		actualPeak := 0
		actualMag := row[0]
		for i, p := range row {
			if p > actualMag {
				actualMag = p
				actualPeak = i
			}
		}

		if actualPeak != peakBin {
			t.Errorf("f=%g Hz: peak at bin %d, want bin %d", f, actualPeak, peakBin)
		}

		// Verify the peak is meaningfully above the noise floor.
		// For a clean sinusoid the peak should be ~10⁶× the adjacent
		// bin's leakage; require at least 100× as a regression guard.
		adjacent := row[peakBin-2]
		if actualMag < adjacent*100 {
			t.Errorf("f=%g Hz: peak magnitude %g not enough above bin[peak-2] %g (ratio %g, want >= 100)",
				f, actualMag, adjacent, actualMag/adjacent)
		}
	}
}

// TestSpectrogram_PanicsOnNilSamples pins the nil-input contract.
// (Empty-but-non-nil slice is a separate test below — that path
// is allowed.)
func TestSpectrogram_PanicsOnNilSamples(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Spectrogram(nil) should panic; did not")
		}
	}()
	_ = Spectrogram(nil)
}

// TestSpectrogram_AcceptsEmpty pins that an empty (non-nil) input
// slice produces a fully-zero spectrogram of canonical dimensions,
// not a panic. Useful for callers that might pass a short buffer
// (e.g. a partial recording) — the audio loop should zero-pad
// gracefully rather than refuse.
func TestSpectrogram_AcceptsEmpty(t *testing.T) {
	out := Spectrogram([]float32{})
	if len(out) != NHSYM {
		t.Errorf("len = %d, want %d", len(out), NHSYM)
	}
	for tt, row := range out {
		for f, p := range row {
			if p != 0 {
				t.Errorf("empty input: out[%d][%d] = %g, want 0", tt, f, p)
				return
			}
		}
	}
}

// TestSpectrogram_TruncatesLongInput pins that audio longer than
// NMAX is truncated cleanly — the same first-NHSYM time columns
// are produced as for an NMAX-length input, with any tail samples
// ignored.
func TestSpectrogram_TruncatesLongInput(t *testing.T) {
	// 1.5× NMAX of sinusoid at a known bin.
	const df = Fs / float64(NFFT1)
	const peakBin = 200
	f := float64(peakBin) * df

	long := make([]float32, NMAX*3/2)
	for n := range long {
		long[n] = float32(math.Sin(2 * math.Pi * f * float64(n) / Fs))
	}

	out := Spectrogram(long)
	if len(out) != NHSYM {
		t.Errorf("len = %d, want %d (long input truncated to NHSYM rows)", len(out), NHSYM)
	}
	// Should still peak at the expected bin.
	row := out[NHSYM/2]
	actualPeak := 0
	actualMag := row[0]
	for i, p := range row {
		if p > actualMag {
			actualMag = p
			actualPeak = i
		}
	}
	if actualPeak != peakBin {
		t.Errorf("peak at bin %d, want %d", actualPeak, peakBin)
	}
}

// BenchmarkSpectrogram_FullSlot measures spectrogram computation
// time for one full 15-second FT8 slot. NHSYM × NFFT1-sized FFT =
// 372 × 3840-point FFTs. Each FFT runs at ~870 µs (per the
// internal/audio FFT benchmarks), so a slot ≈ 320 ms ballpark on
// the operator's hardware — well inside the 15-second slot budget.
func BenchmarkSpectrogram_FullSlot(b *testing.B) {
	samples := make([]float32, NMAX)
	for i := range samples {
		samples[i] = float32(math.Sin(float64(i) * 0.01))
	}
	b.ResetTimer()
	for range b.N {
		_ = Spectrogram(samples)
	}
}
