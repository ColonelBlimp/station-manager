// bench_test.go — performance benchmarks for the FT8 GFSK synthesis pipeline.
//
// Run with:
//
//	go test -bench=. -benchmem -timeout 60s ./ft8/synth/
//
// These benchmarks use realistic FT8-sized inputs (79-symbol sequences,
// 1920 samples/symbol, BT=2.0) to track the cost of the TX synthesis path.
// For real-time FT8, the full synthesis pipeline must complete well within
// the ~1 s delay between TX decision and the start of the TX window.

package synth

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// --- Gaussian filter ---

func BenchmarkGaussianFilter(b *testing.B) {
	b.Run("FT8", func(b *testing.B) {
		for range b.N {
			GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
		}
	})
	b.Run("FT4_BT1", func(b *testing.B) {
		for range b.N {
			GaussianFilter(1.0, KernelSpan, dsp.SamplesPerSymbol)
		}
	})
}

// --- Synthesize (full pipeline) ---

func BenchmarkSynthesize(b *testing.B) {
	// Realistic symbol sequence (from an encoded message).
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = uint8(i % dsp.NumTones)
	}
	b.ResetTimer()
	for range b.N {
		Synthesize(symbols, 1000.0)
	}
}

// --- Smoothed frequency trajectory ---

func BenchmarkSmoothedFrequency(b *testing.B) {
	kernel := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)

	b.Run("constant", func(b *testing.B) {
		// Best case: all symbols the same (no transitions).
		var symbols [dsp.NumSymbols]uint8
		b.ResetTimer()
		for range b.N {
			SmoothedFrequency(symbols, 1000.0, kernel, dsp.SamplesPerSymbol)
		}
	})
	b.Run("varied", func(b *testing.B) {
		// Realistic case: symbols cycling through all 8 tones.
		var symbols [dsp.NumSymbols]uint8
		for i := range symbols {
			symbols[i] = uint8(i % dsp.NumTones)
		}
		b.ResetTimer()
		for range b.N {
			SmoothedFrequency(symbols, 1000.0, kernel, dsp.SamplesPerSymbol)
		}
	})
	b.Run("worst_case", func(b *testing.B) {
		// Worst case: alternating tone 0 / tone 7 (maximum transitions).
		var symbols [dsp.NumSymbols]uint8
		for i := range symbols {
			if i%2 != 0 {
				symbols[i] = 7
			}
		}
		b.ResetTimer()
		for range b.N {
			SmoothedFrequency(symbols, 1000.0, kernel, dsp.SamplesPerSymbol)
		}
	})
}
