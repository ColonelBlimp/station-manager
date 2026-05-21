package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

// generateTone produces a real sinusoid at frequency f Hz, full
// scale (amplitude 1.0), at sample rate Fs, for NMAX samples
// (the FT8 15-second slot length).
func generateTone(f float64) []float32 {
	out := make([]float32, NMAX)
	for n := range out {
		out[n] = float32(math.Sin(2 * math.Pi * f * float64(n) / Fs))
	}
	return out
}

// peakBaseband returns (index, magnitude²) of the largest baseband
// sample by power. Used to verify a single-tone input produces a
// near-DC baseband (peak at sample 0 for an exactly-centred tone).
func peakBaseband(b []complex128) (int, float64) {
	bestIdx := 0
	bestPow := real(b[0])*real(b[0]) + imag(b[0])*imag(b[0])
	for i, v := range b {
		p := real(v)*real(v) + imag(v)*imag(v)
		if p > bestPow {
			bestPow = p
			bestIdx = i
		}
	}
	return bestIdx, bestPow
}

// TestDownsample_OutputShape pins the output length contract.
// 3200 complex samples per call (16 s at 200 Hz), regardless of
// f0 value.
func TestDownsample_OutputShape(t *testing.T) {
	for _, f0 := range []float64{500, 1500, 2500} {
		audio := generateTone(f0)
		b := Downsample(audio, f0)
		if len(b) != NFFT2 {
			t.Errorf("f0=%g: len = %d, want %d", f0, len(b), NFFT2)
		}
	}
}

// TestDownsample_SinusoidAtCentreIsNearDC pins the basic mixing
// invariant: a pure sinusoid at f0 should mix down to DC, so the
// baseband signal is approximately a complex constant (i.e. all
// energy concentrated in a single low-frequency baseband
// "component" — at exactly bin 0 of the baseband's own spectrum).
//
// Test approach: FFT the baseband and confirm the peak bin is at
// or very near 0.
func TestDownsample_SinusoidAtCentreIsNearDC(t *testing.T) {
	const f0 = 1500.0
	audio := generateTone(f0)
	b := Downsample(audio, f0)
	if b == nil {
		t.Fatal("Downsample returned nil")
	}

	// FFT the baseband; peak should be at bin 0 (DC).
	B := audioFFT(b)

	bestBin := 0
	bestMag := cmplx.Abs(B[0])
	for k, v := range B {
		m := cmplx.Abs(v)
		if m > bestMag {
			bestMag = m
			bestBin = k
		}
	}

	// Allow ±2 bins of slop from quantization (centreBin rounding
	// in Downsample can put the actual mix frequency a fraction of
	// a bin off f0).
	if bestBin > 2 && bestBin < NFFT2-2 {
		t.Errorf("baseband peak at bin %d, want near 0 (DC) for f0=%g", bestBin, f0)
	}
}

// TestDownsample_SinusoidOffsetRotates pins the frequency-shift
// invariant: a sinusoid at f0+Δf should mix to baseband at Δf,
// where the magnitude peaks at bin Δf*NFFT2/Fs2 of the
// baseband FFT.
//
// At Fs2=200 Hz output rate and NFFT2=3200, each baseband-FFT bin
// = 200/3200 = 0.0625 Hz. So Δf=10 Hz → peak at bin 160.
func TestDownsample_SinusoidOffsetRotates(t *testing.T) {
	cases := []struct {
		f0Centre  float64
		deltaFreq float64
	}{
		{1500, 10},
		{1500, 25},
		{1000, -15},
		{2000, 50},
	}
	for _, tc := range cases {
		t.Run("f0="+ftoa(tc.f0Centre)+"_dF="+ftoa(tc.deltaFreq), func(t *testing.T) {
			audio := generateTone(tc.f0Centre + tc.deltaFreq)
			b := Downsample(audio, tc.f0Centre)
			if b == nil {
				t.Fatal("Downsample returned nil")
			}

			B := audioFFT(b)
			bestBin := 0
			bestMag := cmplx.Abs(B[0])
			for k, v := range B {
				m := cmplx.Abs(v)
				if m > bestMag {
					bestMag = m
					bestBin = k
				}
			}

			// Δf>0 → positive baseband bin; Δf<0 → wraps to NFFT2+bin
			// in standard FFT layout.
			const fs2BinHz = Fs2 / float64(NFFT2) // 0.0625 Hz/bin
			wantBin := int(math.Round(tc.deltaFreq / fs2BinHz))
			if wantBin < 0 {
				wantBin += NFFT2
			}

			// Allow ±2 bin tolerance from quantisation.
			diff := bestBin - wantBin
			if diff < 0 {
				diff = -diff
			}
			// Also consider wrap-around: if the diff is close to
			// NFFT2 it's still close.
			if diff > NFFT2/2 {
				diff = NFFT2 - diff
			}
			if diff > 2 {
				t.Errorf("baseband peak bin %d, want ~%d (Δf=%g Hz, %g Hz/bin, ±2 bins tolerance)",
					bestBin, wantBin, tc.deltaFreq, fs2BinHz)
			}
		})
	}
}

// TestDownsample_OutOfBandRejection verifies the band-extraction
// rejects content outside ±100 Hz of f0. A tone at f0+500 Hz
// should leave the baseband mostly empty (peak magnitude well
// below an in-band sinusoid's peak).
func TestDownsample_OutOfBandRejection(t *testing.T) {
	const f0 = 1500.0

	// In-band reference: pure tone at f0 itself.
	inBand := generateTone(f0)
	bIn := Downsample(inBand, f0)
	_, inPeakPow := peakBaseband(bIn)

	// Out-of-band: tone 500 Hz above f0, well outside ±100 Hz band.
	outBand := generateTone(f0 + 500)
	bOut := Downsample(outBand, f0)
	_, outPeakPow := peakBaseband(bOut)

	// Out-of-band peak should be at least 100× below in-band peak.
	// 100× = 20 dB rejection; the rectangular bin-extraction with
	// Hann edges should provide much more than this on a 500-Hz
	// offset (well past the band edge), but a 100× threshold
	// catches any catastrophic regression.
	if outPeakPow*100 > inPeakPow {
		t.Errorf("out-of-band (f0+500Hz) baseband peak power %g is within 100× of in-band peak %g — rejection too weak (want >100× = 20dB)",
			outPeakPow, inPeakPow)
	}
}

// TestDownsample_RejectsInvalidF0 pins the input-validation contract.
// f0 outside (0, Fs/2) returns nil rather than producing garbage.
func TestDownsample_RejectsInvalidF0(t *testing.T) {
	audio := generateTone(1500)
	cases := []float64{0, -100, Fs / 2, Fs, Fs + 100}
	for _, f0 := range cases {
		if b := Downsample(audio, f0); b != nil {
			t.Errorf("f0=%g should return nil; got %d-sample result", f0, len(b))
		}
	}
}

// TestDownsample_HandlesShortInput pins that input shorter than
// NMAX gets zero-padded transparently — useful for callers that
// might pass partial recordings or test fixtures.
func TestDownsample_HandlesShortInput(t *testing.T) {
	// 5 seconds of audio (60000 samples) instead of 15 s.
	const f0 = 1500.0
	short := generateTone(f0)[:Fs*5]
	b := Downsample(short, f0)
	if len(b) != NFFT2 {
		t.Errorf("short input: len = %d, want %d", len(b), NFFT2)
	}
	// The output should still show energy at DC (sinusoid mixed
	// down), just with lower SNR than a full slot.
	B := audioFFT(b)
	if math.Abs(real(B[0])*real(B[0])+imag(B[0])*imag(B[0])) == 0 {
		t.Error("baseband DC bin = 0 for short-input sinusoid; expected non-zero energy")
	}
}

// BenchmarkDownsample_FullSlot measures one full-slot mix-and-
// decimate. The forward 192k FFT dominates the cost — by the
// internal/audio FFT benchmarks, that's the lion's share of
// runtime.
func BenchmarkDownsample_FullSlot(b *testing.B) {
	audio := generateTone(1500)
	b.ResetTimer()
	for range b.N {
		_ = Downsample(audio, 1500)
	}
}

// ftoa is a no-allocation float-to-string helper for subtest names.
// Strips trailing zeros for compact output.
func ftoa(f float64) string {
	// Simple integer + decimal form; doesn't handle exponents.
	neg := f < 0
	if neg {
		f = -f
	}
	intPart := int64(f)
	frac := f - float64(intPart)
	out := ""
	if neg {
		out = "-"
	}
	out += itoaSigned(intPart)
	if frac > 0.0001 {
		out += "."
		for range 3 {
			frac *= 10
			d := int(frac)
			out += string(byte('0' + d))
			frac -= float64(d)
		}
	}
	return out
}

func itoaSigned(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
