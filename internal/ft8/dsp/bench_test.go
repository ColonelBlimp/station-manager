// bench_test.go — performance benchmarks for the FT8 DSP pipeline.
//
// Run with:
//   go test -bench=. -benchmem -timeout 300s ./ft8/dsp/
//
// These benchmarks use realistic FT8-sized inputs (1920-sample frames,
// 180 000-sample windows) to identify bottlenecks before they appear in
// production. For real-time FT8, the full ProcessWindow pipeline must
// complete well within the 15-second RX window.

package dsp

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// --- Low-level DSP primitives ---

func BenchmarkRealFFT(b *testing.B) {
	b.Run("1920", func(b *testing.B) {
		frame := makeSinFrame(1920, 1000.0)
		b.ResetTimer()
		for range b.N {
			RealFFT(frame)
		}
	})
	b.Run("2048", func(b *testing.B) {
		frame := makeSinFrame(2048, 1000.0)
		b.ResetTimer()
		for range b.N {
			RealFFT(frame)
		}
	})
}

func BenchmarkRealFFTN_MixedRadix(b *testing.B) {
	b.Run("3840", func(b *testing.B) {
		frame := makeSinFrame(3840, 1000.0)
		b.ResetTimer()
		for range b.N {
			RealFFTN(frame, 3840)
		}
	})
	b.Run("3200", func(b *testing.B) {
		frame := makeSinFrame(3200, 1000.0)
		b.ResetTimer()
		for range b.N {
			RealFFTN(frame, 3200)
		}
	})
}

func BenchmarkMixedRadixDFT_vs_Bluestein(b *testing.B) {
	// Direct comparison of mixed-radix vs Bluestein for the three hot-path sizes.
	for _, n := range []int{3200, 3840, 192000} {
		b.Run(itoa(n)+"/mixed_radix", func(b *testing.B) {
			x := make([]complex128, n)
			for i := range x {
				x[i] = complex(math.Sin(float64(i)*0.001), 0)
			}
			buf := make([]complex128, n)
			b.ResetTimer()
			for range b.N {
				copy(buf, x)
				mixedRadixDFT(buf)
			}
		})
		b.Run(itoa(n)+"/bluestein", func(b *testing.B) {
			x := make([]complex128, n)
			for i := range x {
				x[i] = complex(math.Sin(float64(i)*0.001), 0)
			}
			buf := make([]complex128, n)
			b.ResetTimer()
			for range b.N {
				copy(buf, x)
				bluesteinDFT(buf)
			}
		})
	}
}

func BenchmarkLongFFT(b *testing.B) {
	samples := makeSinFrame(WindowSamples, 1000.0)
	b.ResetTimer()
	for range b.N {
		LongFFT(samples)
	}
}

func BenchmarkGoertzel(b *testing.B) {
	frame := makeSinFrame(SamplesPerSymbol, 1000.0)
	hann := HannCoefficients(SamplesPerSymbol)
	b.ResetTimer()
	for range b.N {
		Goertzel(frame, hann, 1000.0)
	}
}

func BenchmarkGoertzelTones(b *testing.B) {
	frame := makeSinFrame(SamplesPerSymbol, 1000.0)
	hann := HannCoefficients(SamplesPerSymbol)
	b.ResetTimer()
	for range b.N {
		GoertzelTones(frame, hann, 1000.0)
	}
}

// --- Spectrum ---

func BenchmarkPowerSpectrum(b *testing.B) {
	bins := make([]complex64, 1025)
	for i := range bins {
		bins[i] = complex(float32(i), float32(i)*0.5)
	}
	b.ResetTimer()
	for range b.N {
		PowerSpectrum(bins)
	}
}

func BenchmarkLog2PowerSpectrum(b *testing.B) {
	bins := make([]complex64, 1025)
	for i := range bins {
		bins[i] = complex(float32(i+1), float32(i+1)*0.5)
	}
	b.ResetTimer()
	for range b.N {
		Log2PowerSpectrum(bins)
	}
}

// --- Spectrogram ---

func BenchmarkSpectrogramFT8(b *testing.B) {
	samples := makeSinFrame(WindowSamples, 1000.0)
	b.ResetTimer()
	for range b.N {
		SpectrogramFT8(samples)
	}
}

func BenchmarkSpectrogram(b *testing.B) {
	samples := makeSinFrame(WindowSamples, 1000.0)
	b.ResetTimer()
	for range b.N {
		Spectrogram(samples, SamplesPerSymbol, SamplesPerSymbol)
	}
}

// --- Candidate detection ---

func BenchmarkFindCandidates(b *testing.B) {
	samples := makeSinFrame(WindowSamples, 1000.0)
	sg := SpectrogramFT8(samples)
	b.ResetTimer()
	for range b.N {
		FindCandidates(sg, 100, 2)
	}
}

func BenchmarkRefineCandidateAudio(b *testing.B) {
	samples := benchSynthSignal(1000.0)
	hann := HannCoefficients(SamplesPerSymbol)
	cand := Candidate{Freq: 1000.0, TimeOff: 0}
	b.ResetTimer()
	for range b.N {
		RefineCandidateAudio(samples, hann, cand)
	}
}

// --- Demodulation ---

func BenchmarkDemodulateAudio(b *testing.B) {
	samples := benchSynthSignal(1000.0)
	hann := HannCoefficients(SamplesPerSymbol)
	cand := Candidate{Freq: 1000.0, TimeOff: 0}
	b.ResetTimer()
	for range b.N {
		DemodulateAudio(samples, hann, cand)
	}
}

func BenchmarkNormalizeLLR(b *testing.B) {
	var llr [CodedBits]float32
	for i := range llr {
		llr[i] = float32(i%20) - 10
	}
	b.ResetTimer()
	for range b.N {
		NormalizeLLR(&llr)
	}
}

// --- Full pipeline ---

func BenchmarkProcessWindow(b *testing.B) {
	b.Run("1signal", func(b *testing.B) {
		samples := benchSynthSignal(1000.0)
		b.ResetTimer()
		for range b.N {
			ProcessWindow(samples, 50, 50)
		}
	})
	b.Run("3signals", func(b *testing.B) {
		samples := benchSynthMultiSignal(800.0, 1400.0, 2000.0)
		b.ResetTimer()
		for range b.N {
			ProcessWindow(samples, 50, 50)
		}
	})
}

// --- Helpers ---

// makeSinFrame generates a float32 cosine wave at the given frequency.
func makeSinFrame(n int, freqHz float64) []float32 {
	frame := make([]float32, n)
	for i := range frame {
		frame[i] = float32(math.Cos(2 * math.Pi * freqHz * float64(i) / SampleRate))
	}
	return frame
}

// benchSynthSignal synthesises a full FT8 signal at the given base frequency,
// embedded in a WindowSamples-length buffer. Uses the same pattern as
// TestProcessWindowRoundTrip.
func benchSynthSignal(baseFreqHz float64) []float32 {
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	cw := codec.EncodeMessage(msg77)
	var cwDSP [CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := BitsToSymbols(cwDSP)
	chanSyms := InsertSync(dataSyms)

	samples := make([]float32, WindowSamples)
	for sym := range NumSymbols {
		toneFreq := baseFreqHz + float64(chanSyms[sym])*ToneSpacing
		sampleStart := sym * SamplesPerSymbol
		for n := range SamplesPerSymbol {
			g := sampleStart + n
			samples[g] = float32(math.Cos(2 * math.Pi * toneFreq * float64(g) / SampleRate))
		}
	}
	return samples
}

// benchSynthMultiSignal synthesises multiple FT8 signals at different
// frequencies into the same buffer.
func benchSynthMultiSignal(freqs ...float64) []float32 {
	msgs := [][10]byte{
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8},
		{0xCA, 0xFE, 0xBA, 0xBE, 0xDE, 0xAD, 0x00, 0x11, 0x22, 0x30},
		{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x40},
	}

	samples := make([]float32, WindowSamples)
	for i, freq := range freqs {
		msg := msgs[i%len(msgs)]
		cw := codec.EncodeMessage(msg)
		var cwDSP [CodewordBytes]byte
		copy(cwDSP[:], cw[:])
		dataSyms := BitsToSymbols(cwDSP)
		chanSyms := InsertSync(dataSyms)

		for sym := range NumSymbols {
			toneFreq := freq + float64(chanSyms[sym])*ToneSpacing
			sampleStart := sym * SamplesPerSymbol
			for n := range SamplesPerSymbol {
				g := sampleStart + n
				samples[g] += float32(math.Cos(2 * math.Pi * toneFreq * float64(g) / SampleRate))
			}
		}
	}
	return samples
}
