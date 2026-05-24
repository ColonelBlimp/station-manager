package candidates

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// loadBenchSamples loads the 10cq_clean fixture once per benchmark
// run. The fixture is the densest synthetic slot we have (10 signals,
// full 12.64-second TX window populated end-to-end) — good worst-case
// stress for the stage-1 sweep + stage-2 verifier counts.
func loadBenchSamples(b *testing.B) []float32 {
	b.Helper()
	data, err := audio.ReadWAV(filepath.Join("..", "10cq_clean.wav"))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	return data.Samples
}

// BenchmarkFind_Clean measures the full Find pipeline on the
// 10-signal clean fixture. This is the headline number we're tuning
// against.
func BenchmarkFind_Clean(b *testing.B) {
	samples := loadBenchSamples(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Find(samples)
	}
}

// BenchmarkSpectrogram isolates the one-shot STFT pass.
func BenchmarkSpectrogram(b *testing.B) {
	samples := loadBenchSamples(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = spectrogram(samples)
	}
}

// BenchmarkCostasScore isolates one stage-1 score evaluation.
// Multiply by ~90,000 calls per slot to get the stage-1 budget.
func BenchmarkCostasScore(b *testing.B) {
	samples := loadBenchSamples(b)
	spec := spectrogram(samples)
	if len(spec) == 0 {
		b.Fatal("spectrogram empty")
	}
	const df = fs / nfft1
	const nominalStartStep = 12
	centreBin := int(500.0 / df)
	rawDtSteps := -3
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = costasScore(spec, centreBin, rawDtSteps, nominalStartStep)
	}
}

// BenchmarkVerifyCostas isolates one stage-2 verification at a
// known real signal's position.
func BenchmarkVerifyCostas(b *testing.B) {
	samples := loadBenchSamples(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = verifyCostas(samples, 500.0, 0.0, 0)
	}
}

// BenchmarkGoertzelMulti isolates the pure-Go 8-tone kernel.
func BenchmarkGoertzelMulti(b *testing.B) {
	samples := loadBenchSamples(b)
	var coeffs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		fk := 500.0 + float64(k)*baud
		coeffs[k] = 2 * math.Cos(2*math.Pi*fk/fs)
	}
	const symStart = 6000
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = goertzelMulti(samples, symStart, nsps, coeffs)
	}
}

// BenchmarkGoertzelMultiSIMDInto isolates the SIMD path (on
// amd64+cgo builds; on others it routes back to the pure-Go
// kernel via the dispatch shim). Use this to compare directly
// against BenchmarkGoertzelMulti above.
func BenchmarkGoertzelMultiSIMDInto(b *testing.B) {
	samples := loadBenchSamples(b)
	var coeffs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		fk := 500.0 + float64(k)*baud
		coeffs[k] = 2 * math.Cos(2*math.Pi*fk/fs)
	}
	var energies [ft8ToneCount]float64
	const symStart = 6000
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		goertzelMultiSIMDInto(samples, symStart, nsps, &coeffs, &energies)
	}
}
