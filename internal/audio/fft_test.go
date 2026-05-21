package audio

import (
	"math"
	"math/cmplx"
	"testing"
)

// closeEnough reports whether |a - b| ≤ tol.
func closeEnough(a, b complex128, tol float64) bool {
	return cmplx.Abs(a-b) <= tol
}

// slicesCloseEnough checks element-wise closeness with per-call tol.
func slicesCloseEnough(t *testing.T, got, want []complex128, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if !closeEnough(got[i], want[i], tol) {
			t.Errorf("bin %d: got %v, want %v (diff %g)", i, got[i], want[i], cmplx.Abs(got[i]-want[i]))
		}
	}
}

// TestFFT_DC_GivesImpulseAtBin0 pins the DC-in-time → impulse-in-freq
// identity. A constant-amplitude input has all energy concentrated at
// DC (bin 0); every other bin should be ~0. This is the simplest
// FFT correctness check.
func TestFFT_DC_GivesImpulseAtBin0(t *testing.T) {
	for _, n := range []int{2, 3, 4, 5, 6, 8, 9, 10, 15, 16, 24, 27, 32, 60, 125, 1920, 3200} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			x := make([]complex128, n)
			for i := range x {
				x[i] = 1
			}
			X := FFT(x)
			if !closeEnough(X[0], complex(float64(n), 0), 1e-9) {
				t.Errorf("X[0] = %v, want %v (DC bin should sum to N)", X[0], complex(float64(n), 0))
			}
			for k := 1; k < n; k++ {
				if !closeEnough(X[k], 0, 1e-9) {
					t.Errorf("X[%d] = %v, want ~0 (non-DC bin)", k, X[k])
				}
			}
		})
	}
}

// TestFFT_Impulse_GivesFlatSpectrum pins the impulse-in-time →
// flat-in-freq identity. An impulse at sample 0 has equal magnitude
// at every output bin.
func TestFFT_Impulse_GivesFlatSpectrum(t *testing.T) {
	for _, n := range []int{2, 3, 4, 5, 8, 9, 15, 27, 125, 1920} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			x := make([]complex128, n)
			x[0] = 1
			X := FFT(x)
			for k := 0; k < n; k++ {
				if !closeEnough(X[k], 1, 1e-9) {
					t.Errorf("X[%d] = %v, want 1+0i (impulse should give flat spectrum)", k, X[k])
				}
			}
		})
	}
}

// TestFFT_SingleSinusoid_PeaksAtBin pins the single-frequency case:
// a discrete sinusoid at integer bin k0 produces peaks at bins k0
// and N-k0 (the negative-frequency mirror). For x[n] = cos(2π·k0·n/N)
// the peak magnitude at each is N/2.
func TestFFT_SingleSinusoid_PeaksAtBin(t *testing.T) {
	cases := []struct {
		n  int
		k0 int
	}{
		{16, 3},
		{16, 7},
		{32, 5},
		{64, 13},
		{1920, 100},
		{3200, 500},
	}
	for _, tc := range cases {
		t.Run("N="+itoa(tc.n)+"_k="+itoa(tc.k0), func(t *testing.T) {
			x := make([]complex128, tc.n)
			for i := range x {
				x[i] = complex(math.Cos(2*math.Pi*float64(tc.k0)*float64(i)/float64(tc.n)), 0)
			}
			X := FFT(x)
			want := float64(tc.n) / 2
			// Peak at k0.
			if math.Abs(cmplx.Abs(X[tc.k0])-want) > 1e-6 {
				t.Errorf("|X[%d]| = %g, want %g", tc.k0, cmplx.Abs(X[tc.k0]), want)
			}
			// Mirror peak at N-k0.
			if math.Abs(cmplx.Abs(X[tc.n-tc.k0])-want) > 1e-6 {
				t.Errorf("|X[%d]| = %g, want %g (mirror)", tc.n-tc.k0, cmplx.Abs(X[tc.n-tc.k0]), want)
			}
			// Other bins ≈ 0.
			for k := 0; k < tc.n; k++ {
				if k == tc.k0 || k == tc.n-tc.k0 {
					continue
				}
				if cmplx.Abs(X[k]) > 1e-6 {
					t.Errorf("|X[%d]| = %g, want ~0", k, cmplx.Abs(X[k]))
					break
				}
			}
		})
	}
}

// TestFFT_Linearity pins FFT(a·x + b·y) = a·FFT(x) + b·FFT(y).
// Property test across a couple of sizes with deterministic inputs.
func TestFFT_Linearity(t *testing.T) {
	for _, n := range []int{8, 15, 27, 1920} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			x := make([]complex128, n)
			y := make([]complex128, n)
			for i := range x {
				x[i] = complex(math.Sin(float64(i)*0.7), math.Cos(float64(i)*0.3))
				y[i] = complex(math.Cos(float64(i)*1.1), math.Sin(float64(i)*0.5))
			}
			a := complex(2.0, -1.5)
			b := complex(-0.7, 0.9)

			// FFT(a·x + b·y)
			combined := make([]complex128, n)
			for i := range combined {
				combined[i] = a*x[i] + b*y[i]
			}
			lhs := FFT(combined)

			// a·FFT(x) + b·FFT(y)
			X := FFT(x)
			Y := FFT(y)
			rhs := make([]complex128, n)
			for i := range rhs {
				rhs[i] = a*X[i] + b*Y[i]
			}

			slicesCloseEnough(t, lhs, rhs, 1e-9)
		})
	}
}

// TestIFFT_RoundTrip pins IFFT(FFT(x)) = x for arbitrary inputs.
// This is the integrated correctness check for the whole pipeline:
// forward transform, inverse via the conjugate trick, normalisation
// by 1/N — all four steps lined up.
func TestIFFT_RoundTrip(t *testing.T) {
	for _, n := range []int{2, 3, 4, 5, 8, 9, 15, 27, 125, 1920, 3200} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			x := make([]complex128, n)
			for i := range x {
				x[i] = complex(math.Sin(float64(i)*0.7)+0.3, math.Cos(float64(i)*1.1)-0.2)
			}
			X := FFT(x)
			y := IFFT(X)
			// Numeric tolerance scales with N for accumulating
			// floating error across the log-N butterfly levels.
			tol := 1e-9 * float64(n)
			slicesCloseEnough(t, y, x, tol)
		})
	}
}

// TestFFT_PanicsOnNon5SmoothSize verifies the size contract: sizes
// with prime factors > 5 trip a panic, not a silent wrong-answer.
// FT8 only ever feeds 5-smooth sizes; an arbitrary-N call is a
// caller bug.
func TestFFT_PanicsOnNon5SmoothSize(t *testing.T) {
	cases := []int{7, 11, 13, 14, 21, 22, 77, 91} // all contain a prime > 5
	for _, n := range cases {
		t.Run("N="+itoa(n), func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("FFT(N=%d) should panic; did not", n)
				}
			}()
			x := make([]complex128, n)
			_ = FFT(x)
		})
	}
}

// TestFFT_EdgeCases pins N=0 and N=1.
func TestFFT_EdgeCases(t *testing.T) {
	// N=0: empty input → empty output (no recursion entered).
	if got := FFT(nil); len(got) != 0 {
		t.Errorf("FFT(nil) length = %d, want 0", len(got))
	}
	if got := FFT([]complex128{}); len(got) != 0 {
		t.Errorf("FFT([]) length = %d, want 0", len(got))
	}

	// N=1: trivial transform, X[0] = x[0].
	in := []complex128{complex(3.14, -2.71)}
	got := FFT(in)
	if len(got) != 1 || !closeEnough(got[0], in[0], 0) {
		t.Errorf("FFT(N=1): got %v, want %v", got, in)
	}

	// IFFT edge cases mirror FFT's.
	if got := IFFT(nil); len(got) != 0 {
		t.Errorf("IFFT(nil) length = %d, want 0", len(got))
	}
	if got := IFFT(in); len(got) != 1 || !closeEnough(got[0], in[0], 0) {
		t.Errorf("IFFT(N=1): got %v, want %v", got, in)
	}
}

// TestFFT_DoesNotMutateInput pins the contract that FFT/IFFT treat
// the input as caller-owned. A regression that wrote back to x would
// break callers that hold the original samples for reuse.
func TestFFT_DoesNotMutateInput(t *testing.T) {
	const n = 16
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(float64(i), float64(-i))
	}
	saved := make([]complex128, n)
	copy(saved, x)

	_ = FFT(x)
	for i := range x {
		if x[i] != saved[i] {
			t.Errorf("FFT mutated x[%d]: got %v, was %v", i, x[i], saved[i])
		}
	}

	_ = IFFT(x)
	for i := range x {
		if x[i] != saved[i] {
			t.Errorf("IFFT mutated x[%d]: got %v, was %v", i, x[i], saved[i])
		}
	}
}

// --- benchmarks ------------------------------------------------------------

// BenchmarkFFT_1920 measures the per-symbol FFT size FT8 uses for
// the downsampled baseband path (12000 Hz / 6.25 baud = 1920 samples
// per symbol). 1920 = 2^7 · 3 · 5, so radix-2 dominates.
func BenchmarkFFT_1920(b *testing.B) {
	x := makeBenchInput(1920)
	b.ResetTimer()
	for range b.N {
		_ = FFT(x)
	}
}

// BenchmarkFFT_3200 measures the downsampler inverse FFT size from
// the operator's research path (NFFT2 = 3200). 3200 = 2^7 · 5^2.
func BenchmarkFFT_3200(b *testing.B) {
	x := makeBenchInput(3200)
	b.ResetTimer()
	for range b.N {
		_ = FFT(x)
	}
}

// BenchmarkFFT_3840 measures the spectrogram FFT size from the
// operator's research path (NFFT1 = 3840). 3840 = 2^8 · 3 · 5.
func BenchmarkFFT_3840(b *testing.B) {
	x := makeBenchInput(3840)
	b.ResetTimer()
	for range b.N {
		_ = FFT(x)
	}
}

func makeBenchInput(n int) []complex128 {
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(math.Sin(float64(i)*0.1), math.Cos(float64(i)*0.07))
	}
	return x
}
