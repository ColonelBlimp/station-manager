package sandbox

import (
	"math"
	"math/rand"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// TestSpectrogram_PureTonePeak feeds a pure sine wave and confirms
// every time frame's peak bin lands at the expected frequency bin.
// FFT bin spacing is fs/nfft = 12000/3840 = 3.125 Hz; a 1500 Hz tone
// peaks at bin 480.
func TestSpectrogram_PureTonePeak(t *testing.T) {
	const (
		toneHz  = 1500.0
		amp     = 0.5
		seconds = 5.0
	)
	n := int(seconds * fs)
	samples := make([]float32, n)
	for i := range samples {
		tt := float64(i) / fs
		samples[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}

	spec := Spectrogram(samples)
	if len(spec) == 0 {
		t.Fatal("Spectrogram returned no frames")
	}

	expectedBin := int(math.Round(toneHz * nfft / fs))
	for t_, row := range spec {
		peakBin := 0
		peakP := 0.0
		for f, p := range row {
			if p > peakP {
				peakBin = f
				peakP = p
			}
		}
		if peakBin != expectedBin {
			// Allow ±1 bin slack — mainlobe leakage from the rectangular
			// 1-symbol window can shift the peak by one bin when the
			// tone isn't exactly on a bin centre.
			if peakBin < expectedBin-1 || peakBin > expectedBin+1 {
				// Show first failing frame and stop.
				t.Fatalf("frame %d: peak bin %d, expected %d±1", t_, peakBin, expectedBin)
			}
		}
	}
}

// TestSpectrogram_FrameGeometry pins the number of frames as a
// function of input length. For len=180000 (a full FT8 slot at 12 kHz),
// frames = floor((180000-1920)/480) + 1 = floor(178080/480) + 1 =
// 371 + 1 = 372.
func TestSpectrogram_FrameGeometry(t *testing.T) {
	samples := make([]float32, 180000)
	spec := Spectrogram(samples)
	if len(spec) != 372 {
		t.Fatalf("frame count = %d, expected 372", len(spec))
	}
	if len(spec[0]) != nfft/2 {
		t.Fatalf("bins per frame = %d, expected %d", len(spec[0]), nfft/2)
	}
}

// TestSpectrogram_ShortInputReturnsNil documents the contract that
// Spectrogram returns nil when there's less than one symbol of audio.
func TestSpectrogram_ShortInputReturnsNil(t *testing.T) {
	if spec := Spectrogram(make([]float32, nsps-1)); spec != nil {
		t.Errorf("expected nil for short input, got %d frames", len(spec))
	}
}

// BenchmarkSpectrogram measures the per-slot cost of building a
// 372-frame spectrogram from a 15s 12 kHz buffer (180000 samples).
// Pure-Go internal/audio.RealPlan benchmark for the same workload
// lives in the equivalent Session-96 candidates package — comparing
// the two informs the FFT-backend speedup at the per-frame size
// (nfft = 3840).
func BenchmarkSpectrogram(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	samples := make([]float32, 180000)
	for i := range samples {
		samples[i] = float32(rng.NormFloat64() * 0.1)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Spectrogram(samples)
	}
}

// BenchmarkSpectrogram_AudioBackend mirrors BenchmarkSpectrogram but
// runs the equivalent workload via internal/audio.RealPlan — the
// pre-Session-97 FFT path. Side-by-side numbers tell us the actual
// per-slot speedup at the 3840-point FFT size used here.
func BenchmarkSpectrogram_AudioBackend(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	samples := make([]float32, 180000)
	for i := range samples {
		samples[i] = float32(rng.NormFloat64() * 0.1)
	}
	plan := audio.NewRealPlan(nfft)
	halfFFT := nfft / 2

	nFrames := 0
	for s := 0; s+nsps <= len(samples); s += nstep {
		nFrames++
	}
	chunk := make([]float32, nfft)

	b.ResetTimer()
	b.ReportAllocs()
	for it := 0; it < b.N; it++ {
		backing := make([]float64, nFrames*halfFFT)
		spec := make([][]float64, nFrames)
		for t := 0; t < nFrames; t++ {
			copy(chunk[:nsps], samples[t*nstep:t*nstep+nsps])
			X := plan.FFT(chunk)
			row := backing[t*halfFFT : (t+1)*halfFFT]
			for f := 0; f < halfFFT; f++ {
				re, im := real(X[f]), imag(X[f])
				row[f] = re*re + im*im
			}
			spec[t] = row
		}
		_ = spec
	}
}
