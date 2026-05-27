package pfft

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// TestRealPlan_RoundTrip pins the contract that Forward followed by
// Backward (with explicit 1/N scaling) recovers the original input to
// machine precision. Catches obvious wiring bugs (wrong plan direction,
// scale missing, layout mismatch).
func TestRealPlan_RoundTrip(t *testing.T) {
	for _, n := range []int{64, 256, 1024, 3840} {
		t.Run("", func(t *testing.T) {
			plan, err := NewRealPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()

			orig := make([]float64, n)
			rng := rand.New(rand.NewSource(int64(n)))
			for i := range orig {
				orig[i] = rng.NormFloat64()
			}

			x := append([]float64(nil), orig...)
			if err := plan.Forward(x); err != nil {
				t.Fatal(err)
			}
			if err := plan.Backward(x); err != nil {
				t.Fatal(err)
			}

			maxDiff := 0.0
			inv := 1.0 / float64(n)
			for i := range x {
				d := math.Abs(x[i]*inv - orig[i])
				if d > maxDiff {
					maxDiff = d
				}
			}
			if maxDiff > 1e-12 {
				t.Errorf("n=%d round-trip max error %g (expected < 1e-12)", n, maxDiff)
			}
		})
	}
}

// TestComplexPlan_RoundTrip pins the complex-input round-trip contract.
func TestComplexPlan_RoundTrip(t *testing.T) {
	for _, n := range []int{64, 256, 1024, 3840} {
		t.Run("", func(t *testing.T) {
			plan, err := NewComplexPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()

			orig := make([]complex128, n)
			rng := rand.New(rand.NewSource(int64(n)))
			for i := range orig {
				orig[i] = complex(rng.NormFloat64(), rng.NormFloat64())
			}

			x := append([]complex128(nil), orig...)
			if err := plan.Forward(x); err != nil {
				t.Fatal(err)
			}
			if err := plan.Backward(x); err != nil {
				t.Fatal(err)
			}

			maxDiff := 0.0
			inv := complex(1.0/float64(n), 0)
			for i := range x {
				d := cmplx.Abs(x[i]*inv - orig[i])
				if d > maxDiff {
					maxDiff = d
				}
			}
			if maxDiff > 1e-12 {
				t.Errorf("n=%d round-trip max error %g (expected < 1e-12)", n, maxDiff)
			}
		})
	}
}

// TestComplexPlan_AgainstAudioFFT cross-checks PocketFFT's forward
// transform against SM's pure-Go internal/audio.FFT at sizes the
// pure-Go path supports (5-smooth: factors of 2, 3, 5 only). Numerical
// agreement should sit at the float64 noise floor — both compute the
// same DFT, differences come only from butterfly-order rounding.
func TestComplexPlan_AgainstAudioFFT(t *testing.T) {
	for _, n := range []int{64, 240, 1920, 3840} {
		t.Run("", func(t *testing.T) {
			plan, err := NewComplexPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()

			rng := rand.New(rand.NewSource(int64(n) * 7))
			orig := make([]complex128, n)
			for i := range orig {
				orig[i] = complex(rng.NormFloat64(), rng.NormFloat64())
			}

			pkResult := append([]complex128(nil), orig...)
			if err := plan.Forward(pkResult); err != nil {
				t.Fatal(err)
			}
			audioResult := audio.FFT(append([]complex128(nil), orig...))

			maxDiff := 0.0
			for i := range pkResult {
				d := cmplx.Abs(pkResult[i] - audioResult[i])
				if d > maxDiff {
					maxDiff = d
				}
			}
			// Tolerance scales with sqrt(n) (random-walk rounding accumulation)
			// and float64 machine epsilon (~2.2e-16). For n=3840: ~62 × 2.2e-16
			// × ~10 (output magnitude scale) ≈ 1.4e-13. Allow a small margin.
			tol := 1e-10
			if maxDiff > tol {
				t.Errorf("n=%d PocketFFT vs audio.FFT max diff %g (expected < %g)", n, maxDiff, tol)
			}
		})
	}
}

// TestRealPlan_Bin pins the FFTPack-packed-layout bin accessor against
// PocketFFT's complex transform of the same real input zero-padded
// into the imaginary axis. Bin(k) should equal the k-th entry of the
// complex transform of the same data.
func TestRealPlan_Bin(t *testing.T) {
	n := 256
	rp, err := NewRealPlan(n)
	if err != nil {
		t.Fatal(err)
	}
	defer rp.Close()
	cp, err := NewComplexPlan(n)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()

	rng := rand.New(rand.NewSource(99))
	realIn := make([]float64, n)
	for i := range realIn {
		realIn[i] = rng.NormFloat64()
	}
	cplxIn := make([]complex128, n)
	for i := range realIn {
		cplxIn[i] = complex(realIn[i], 0)
	}

	if err := rp.Forward(realIn); err != nil {
		t.Fatal(err)
	}
	if err := cp.Forward(cplxIn); err != nil {
		t.Fatal(err)
	}

	// Verify every unique bin agrees between the two paths.
	maxDiff := 0.0
	for k := 0; k <= n/2; k++ {
		got := rp.Bin(realIn, k)
		want := cplxIn[k]
		d := cmplx.Abs(got - want)
		if d > maxDiff {
			maxDiff = d
		}
	}
	if maxDiff > 1e-10 {
		t.Errorf("Bin accessor disagreed with cfft path: max diff %g", maxDiff)
	}
}

// TestRealPlan_LengthMismatch covers the input-validation path.
func TestRealPlan_LengthMismatch(t *testing.T) {
	plan, err := NewRealPlan(64)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if err := plan.Forward(make([]float64, 65)); err == nil {
		t.Errorf("expected error on length mismatch")
	}
}

// TestNewPlan_Invalid covers the n<1 guard.
func TestNewPlan_Invalid(t *testing.T) {
	if _, err := NewRealPlan(0); err == nil {
		t.Errorf("expected error from NewRealPlan(0)")
	}
	if _, err := NewComplexPlan(-1); err == nil {
		t.Errorf("expected error from NewComplexPlan(-1)")
	}
}
