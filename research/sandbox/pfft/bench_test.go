package pfft

import (
	"math/rand"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// Benchmark sizes chosen for the channelizer pipeline (per
// 2026-05-27 sandbox design):
//   - benchForwardN = 192,000 — single FFT of the whole 15 s slot at
//     12 kHz, zero-padded to a 5-smooth size. 5-smooth factorisation:
//     192000 = 2^9 × 3 × 5^3.
//   - benchSliceN = 3,200 — per-candidate IFFT slice width.
//     3200 = 2^7 × 5^2. Corresponds to 200 Hz complex baseband
//     bandwidth (3200 bins × 0.0625 Hz/bin).
//
// Both compare PocketFFT (CGO) against SM's pure-Go internal/audio
// FFT to inform the sandbox architecture: per-slot FFT throughput
// drives the candidate-detection budget.
//
// Run:  go test -bench=. -benchmem -benchtime=2s ./research/sandbox/pfft/
const (
	benchForwardN = 192_000
	benchSliceN   = 3_200
)

// BenchmarkForwardFFT_PocketFFT_Real measures the 192k-real-input
// forward path via PocketFFT's rfft. In-place; we copy a prepared
// input into a working buffer each iteration to avoid the FFT
// destroying its own input on the next call.
func BenchmarkForwardFFT_PocketFFT_Real(b *testing.B) {
	plan, err := NewRealPlan(benchForwardN)
	if err != nil {
		b.Fatal(err)
	}
	defer plan.Close()

	rng := rand.New(rand.NewSource(1))
	input := make([]float64, benchForwardN)
	for i := range input {
		input[i] = rng.NormFloat64()
	}
	work := make([]float64, benchForwardN)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		copy(work, input)
		if err := plan.Forward(work); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkForwardFFT_PocketFFT_Complex measures 192k complex-input
// forward via cfft. For real audio we'd feed real-with-zero-imag,
// which doubles the work vs the real path — included for comparison.
func BenchmarkForwardFFT_PocketFFT_Complex(b *testing.B) {
	plan, err := NewComplexPlan(benchForwardN)
	if err != nil {
		b.Fatal(err)
	}
	defer plan.Close()

	rng := rand.New(rand.NewSource(1))
	input := make([]complex128, benchForwardN)
	for i := range input {
		input[i] = complex(rng.NormFloat64(), 0)
	}
	work := make([]complex128, benchForwardN)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		copy(work, input)
		if err := plan.Forward(work); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkForwardFFT_AudioRealPlan measures 192k-real-input forward
// via SM's pure-Go internal/audio.RealPlan. RealPlan.FFT returns a
// freshly-allocated complex output, so no input copy is needed —
// the allocation cost is part of every call.
func BenchmarkForwardFFT_AudioRealPlan(b *testing.B) {
	plan := audio.NewRealPlan(benchForwardN)

	rng := rand.New(rand.NewSource(1))
	input := make([]float32, benchForwardN)
	for i := range input {
		input[i] = float32(rng.NormFloat64())
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = plan.FFT(input)
	}
}

// BenchmarkForwardFFT_AudioPlan measures 192k complex-input forward
// via SM's pure-Go internal/audio.Plan. Same allocation model as
// AudioRealPlan — fresh output slice per call.
func BenchmarkForwardFFT_AudioPlan(b *testing.B) {
	plan := audio.NewPlan(benchForwardN)

	rng := rand.New(rand.NewSource(1))
	input := make([]complex128, benchForwardN)
	for i := range input {
		input[i] = complex(rng.NormFloat64(), 0)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = plan.FFT(input)
	}
}

// BenchmarkInverseFFT_PocketFFT_Slice measures the per-candidate
// 3200-point complex IFFT — the channelizer's per-candidate output
// stage. In-place via cfft_backward.
func BenchmarkInverseFFT_PocketFFT_Slice(b *testing.B) {
	plan, err := NewComplexPlan(benchSliceN)
	if err != nil {
		b.Fatal(err)
	}
	defer plan.Close()

	rng := rand.New(rand.NewSource(2))
	input := make([]complex128, benchSliceN)
	for i := range input {
		input[i] = complex(rng.NormFloat64(), rng.NormFloat64())
	}
	work := make([]complex128, benchSliceN)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		copy(work, input)
		if err := plan.Backward(work); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInverseFFT_AudioPlan_Slice measures the equivalent
// 3200-point IFFT via internal/audio.Plan. Comparison gives us the
// per-candidate cost on the pure-Go side.
func BenchmarkInverseFFT_AudioPlan_Slice(b *testing.B) {
	plan := audio.NewPlan(benchSliceN)

	rng := rand.New(rand.NewSource(2))
	input := make([]complex128, benchSliceN)
	for i := range input {
		input[i] = complex(rng.NormFloat64(), rng.NormFloat64())
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = plan.IFFT(input)
	}
}
