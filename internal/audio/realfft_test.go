package audio

import (
	"math"
	"math/cmplx"
	"testing"
)

// TestRealPlan_MatchesComplexFFT pins the headline numerical
// contract: RealPlan.FFT(real_samples) must match the complex-FFT
// of (real_samples, zero-imaginary) at every output bin up to N/2.
//
// Tolerance 5e-6: the two paths use different FP operation
// ordering (one N-point complex FFT vs one N/2-point + unpack), so
// they won't be bit-identical. Real audio data typically shows
// < 1e-11; synthetic adversarial signals (multiple sinusoids with
// frequencies close to N/2) push the error toward 2e-6 at N=3840.
// The 5e-6 floor still catches genuine algorithm bugs while
// accommodating legitimate FP variance.
func TestRealPlan_MatchesComplexFFT(t *testing.T) {
	for _, n := range []int{8, 16, 64, 1920, 3840} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			// Synthetic real signal: a few sinusoids at known frequencies.
			x := make([]float32, n)
			for i := range x {
				t := float64(i) / float64(n)
				x[i] = float32(
					100.0*math.Sin(2*math.Pi*7*t) +
						50.0*math.Sin(2*math.Pi*23*t) +
						30.0*math.Cos(2*math.Pi*51*t))
			}

			// Reference: complex Plan with real-padded-with-zero input.
			cIn := make([]complex128, n)
			for i, v := range x {
				cIn[i] = complex(float64(v), 0)
			}
			ref := NewPlan(n).FFT(cIn)

			// Under test: RealPlan path.
			opt := NewRealPlan(n).FFT(x)

			// RealPlan returns N/2+1 bins; complex FFT returns N. The
			// two agree on bins 0..N/2 (the unique real-FFT range).
			if len(opt) != n/2+1 {
				t.Fatalf("RealPlan output length %d, want %d", len(opt), n/2+1)
			}
			maxRelErr := 0.0
			maxBin := -1
			for k := 0; k <= n/2; k++ {
				diff := cmplx.Abs(ref[k] - opt[k])
				mag := cmplx.Abs(ref[k])
				if mag < 1e-10 {
					continue
				}
				if rel := diff / mag; rel > maxRelErr {
					maxRelErr = rel
					maxBin = k
				}
			}
			if maxRelErr > 5e-6 {
				t.Errorf("max relative error %.2e at bin %d exceeds 5e-6", maxRelErr, maxBin)
			} else {
				t.Logf("max relative error: %.2e (bin %d)", maxRelErr, maxBin)
			}
		})
	}
}

// TestRealPlan_ZeroPaddedInput verifies the short-input contract:
// when len(x) < n, the missing samples are treated as zero. This
// matches the existing complex-Plan-with-zero-imaginary path used
// by Spectrogram (which packs NSPS=1920 audio samples into an
// NFFT1=3840 input).
func TestRealPlan_ZeroPaddedInput(t *testing.T) {
	const n = 64
	// Half-length input; the other half is implicitly zero.
	x := make([]float32, n/2)
	for i := range x {
		x[i] = float32(math.Sin(2 * math.Pi * 5 * float64(i) / float64(n)))
	}

	// Reference: complex FFT of explicit zero-padded input.
	cIn := make([]complex128, n)
	for i, v := range x {
		cIn[i] = complex(float64(v), 0)
	}
	ref := NewPlan(n).FFT(cIn)
	opt := NewRealPlan(n).FFT(x)

	for k := 0; k <= n/2; k++ {
		diff := cmplx.Abs(ref[k] - opt[k])
		mag := cmplx.Abs(ref[k])
		if mag > 1e-10 && diff/mag > 1e-6 {
			t.Errorf("zero-padded mismatch at bin %d: ref=%v opt=%v relErr=%.2e",
				k, ref[k], opt[k], diff/mag)
		}
	}
}

// TestRealPlan_RejectsOddSize pins the input-size contract: the
// pack-and-unpack trick requires n even. Odd n panics.
func TestRealPlan_RejectsOddSize(t *testing.T) {
	for _, n := range []int{3, 5, 7, 15, 1921} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("NewRealPlan(%d) should panic; did not", n)
				}
			}()
			_ = NewRealPlan(n)
		})
	}
}

// TestRealPlan_RejectsNon5SmoothHalf pins that n/2 must be 5-smooth
// (the underlying half-Plan's constraint). n=14 is even but n/2=7
// is prime, so it must panic.
func TestRealPlan_RejectsNon5SmoothHalf(t *testing.T) {
	for _, n := range []int{14, 22, 26, 34} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("NewRealPlan(%d) should panic on non-5-smooth half; did not", n)
				}
			}()
			_ = NewRealPlan(n)
		})
	}
}

// TestRealPlan_EdgeCases pins the trivial sizes — n=0 and n=1
// shouldn't blow up.
func TestRealPlan_EdgeCases(t *testing.T) {
	p0 := NewRealPlan(0)
	out0 := p0.FFT(nil)
	if len(out0) != 0 {
		t.Errorf("n=0: got length %d, want 0", len(out0))
	}

	p1 := NewRealPlan(1)
	out1 := p1.FFT([]float32{1.5})
	if len(out1) != 1 {
		t.Fatalf("n=1: got length %d, want 1", len(out1))
	}
	if real(out1[0]) != 1.5 {
		t.Errorf("n=1: got %v, want 1.5+0i", out1[0])
	}
}

// TestRealPlan_RejectsNonMultipleOf4 pins that even sizes whose
// half is 5-smooth but which themselves are not multiples of 4 are
// rejected by the constructor — they would otherwise pass and
// panic later inside FFT (the unpack twiddle table only has n/4
// entries and the k==N/4 special case is only valid when N/4 is
// integral).
func TestRealPlan_RejectsNonMultipleOf4(t *testing.T) {
	for _, n := range []int{6, 10, 18, 30, 50, 54, 90} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("NewRealPlan(%d) should panic (n%%4 != 0); did not", n)
				}
			}()
			_ = NewRealPlan(n)
		})
	}
}

// TestRealPlan_AcceptsN2 pins that n=2 is the one even-non-multiple-of-4
// size that's safe — its inner unpack loop runs zero iterations so
// the twiddle table can be empty. Matches the constructor's contract.
func TestRealPlan_AcceptsN2(t *testing.T) {
	p := NewRealPlan(2)
	out := p.FFT([]float32{3.0, 1.0})
	if len(out) != 2 {
		t.Fatalf("n=2: got length %d, want 2", len(out))
	}
	// For real input [3, 1], the 2-point DFT is X[0] = 4, X[1] = 2.
	const tol = 1e-6
	if math.Abs(real(out[0])-4.0) > tol || math.Abs(imag(out[0])) > tol {
		t.Errorf("n=2 X[0]: got %v, want 4+0i", out[0])
	}
	if math.Abs(real(out[1])-2.0) > tol || math.Abs(imag(out[1])) > tol {
		t.Errorf("n=2 X[1]: got %v, want 2+0i", out[1])
	}
}

// BenchmarkRealPlan_FFT_N3840 measures the per-call cost at the
// spectrogram's hot-path size. Compare to BenchmarkPlan_FFT (the
// complex-Plan-with-zero-imaginary path Spectrogram used to take).
func BenchmarkRealPlan_FFT_N3840(b *testing.B) {
	const n = 3840
	x := make([]float32, n)
	for i := range x {
		x[i] = float32(math.Sin(2 * math.Pi * 100 * float64(i) / float64(n)))
	}
	p := NewRealPlan(n)
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = p.FFT(x)
	}
}
