package audio

import (
	"math"
	"math/cmplx"
)

// FFT operations: complex-to-complex Cooley-Tukey transforms with
// mixed-radix decimation-in-time. The implementation supports
// radix-2, radix-3, and radix-5 butterflies — sufficient for every
// FFT size FT8 uses (192000, 180000, 3840, 3200, 1920 are all
// 5-smooth: prime factors of 2, 3, and 5 only). Sizes outside that
// set panic.
//
// Ported from the operator's go-ft8/research/fft.go (a research-grade
// FFTW3 replacement, validated against single-precision FFTW output
// during the go-ft8 reboot). Pure Go; depends only on the stdlib
// math + math/cmplx packages. Re-licensed under MIT for SM as the
// operator's own authored code.
//
// **Algorithmic notes.** The decomposition picks the smallest prime
// factor at each recursion level (2 → 3 → 5). For N = 2^a · 3^b · 5^c
// the recursion has a + b + c levels. Sub-transforms are computed
// recursively; the butterfly stage combines them with twiddle
// factors via a per-radix DFT (closed-form for radix 2, 3, 5).
//
// **Performance.** Each recursion level allocates fresh sub-arrays
// and a result buffer — O(N) per level, O(N log N) total. That's
// asymptotically right but the constant factor lags FFTW3 by ~3-5×
// because of the per-level allocation. For FT8's 15-second slot
// budget it's well in bounds; a buffer-reuse refactor or a CGO
// FFTW path can land if profiling shows FFT is the bottleneck.

// FFT computes the forward complex-to-complex DFT, unnormalised:
//
//	X[k] = Σ_{n=0..N-1} x[n] · exp(-j · 2π · k · n / N)
//
// Input length must be a 5-smooth integer (factors of 2, 3, 5 only);
// non-5-smooth sizes panic. The returned slice is freshly allocated
// — callers cannot rely on x being preserved (it is, but the
// contract is "treat x as caller-owned, X as fresh").
func FFT(x []complex128) []complex128 {
	out := make([]complex128, len(x))
	copy(out, x)
	return fftMixedRadix(out)
}

// IFFT computes the inverse complex-to-complex DFT, normalised by
// 1/N:
//
//	x[n] = (1/N) · Σ_{k=0..N-1} X[k] · exp(+j · 2π · k · n / N)
//
// Implemented via the standard conjugate-trick:
// IFFT(X) = (1/N) · conj(FFT(conj(X))). Sizes follow the same
// 5-smooth constraint as FFT.
func IFFT(x []complex128) []complex128 {
	n := len(x)
	if n <= 1 {
		out := make([]complex128, n)
		copy(out, x)
		return out
	}

	buf := make([]complex128, n)
	for i, v := range x {
		buf[i] = cmplx.Conj(v)
	}
	buf = fftMixedRadix(buf)
	scale := 1.0 / float64(n)
	for i, v := range buf {
		buf[i] = complex(real(v)*scale, -imag(v)*scale)
	}
	return buf
}

// smallestFactor returns the smallest prime factor of n from {2, 3, 5}.
// Panics if n is not 5-smooth — that's a caller contract violation
// (FT8's FFT sizes are documented as 5-smooth; an arbitrary-N caller
// shouldn't be reaching for this FFT).
func smallestFactor(n int) int {
	if n%2 == 0 {
		return 2
	}
	if n%3 == 0 {
		return 3
	}
	if n%5 == 0 {
		return 5
	}
	panic("audio.FFT: size is not 5-smooth (factors of 2, 3, 5 only); got " + itoa(n))
}

// fftMixedRadix recursively computes the forward FFT of x. The slice
// is consumed (not preserved); callers allocate a fresh copy via
// FFT/IFFT before invoking this.
func fftMixedRadix(x []complex128) []complex128 {
	n := len(x)
	if n <= 1 {
		return x
	}

	p := smallestFactor(n) // radix at this level
	m := n / p             // sub-transform length

	// Decimation-in-time split: p interleaved sub-sequences of
	// length m.
	subs := make([][]complex128, p)
	for j := 0; j < p; j++ {
		subs[j] = make([]complex128, m)
		for k := 0; k < m; k++ {
			subs[j][k] = x[k*p+j]
		}
	}

	// Recursively transform each sub-sequence.
	for j := 0; j < p; j++ {
		subs[j] = fftMixedRadix(subs[j])
	}

	// Butterfly combine with twiddle factors and the per-radix DFT.
	twopi := 2.0 * math.Pi
	result := make([]complex128, n)

	switch p {
	case 2:
		for k := 0; k < m; k++ {
			angle := -twopi * float64(k) / float64(n)
			sin, cos := math.Sincos(angle)
			w := complex(cos, sin)
			t := w * subs[1][k]
			result[k] = subs[0][k] + t
			result[k+m] = subs[0][k] - t
		}

	case 3:
		// Constants for the 3-point DFT roots W_3 = exp(-j·2π/3).
		const (
			cos3 = -0.5                   // cos(2π/3)
			sin3 = -0.8660254037844386468 // -sin(2π/3)
		)
		for k := 0; k < m; k++ {
			s0 := subs[0][k]

			angle1 := -twopi * float64(k) / float64(n)
			sin1, cos1 := math.Sincos(angle1)
			w1 := complex(cos1, sin1)
			s1 := w1 * subs[1][k]

			angle2 := -twopi * float64(2*k) / float64(n)
			sin2, cos2 := math.Sincos(angle2)
			w2 := complex(cos2, sin2)
			s2 := w2 * subs[2][k]

			// 3-point DFT:
			//	X[0] = s0 + s1 + s2
			//	X[1] = s0 + s1·W₃   + s2·W₃²
			//	X[2] = s0 + s1·W₃²  + s2·W₃
			t1 := s1 + s2
			t2 := s1 - s2

			result[k] = s0 + t1
			result[k+m] = s0 + complex(cos3*real(t1)-sin3*imag(t2), cos3*imag(t1)+sin3*real(t2))
			result[k+2*m] = s0 + complex(cos3*real(t1)+sin3*imag(t2), cos3*imag(t1)-sin3*real(t2))
		}

	case 5:
		// W_5 = exp(-j·2π/5) root constants for the 5-point DFT.
		cos15 := math.Cos(twopi / 5)
		sin15 := -math.Sin(twopi / 5)
		cos25 := math.Cos(2 * twopi / 5)
		sin25 := -math.Sin(2 * twopi / 5)

		for k := 0; k < m; k++ {
			s0 := subs[0][k]

			angle1 := -twopi * float64(k) / float64(n)
			sin1, cos1 := math.Sincos(angle1)
			w1 := complex(cos1, sin1)

			angle2 := -twopi * float64(2*k) / float64(n)
			sin2, cos2 := math.Sincos(angle2)
			w2 := complex(cos2, sin2)

			angle3 := -twopi * float64(3*k) / float64(n)
			sin3a, cos3a := math.Sincos(angle3)
			w3 := complex(cos3a, sin3a)

			angle4 := -twopi * float64(4*k) / float64(n)
			sin4, cos4 := math.Sincos(angle4)
			w4 := complex(cos4, sin4)

			s1 := w1 * subs[1][k]
			s2 := w2 * subs[2][k]
			s3 := w3 * subs[3][k]
			s4 := w4 * subs[4][k]

			result[k] = s0 + s1 + s2 + s3 + s4

			// X[q] = s0 + Σ_{j=1..4} s_j · W_5^(j·q), for q=1..4.
			// W_5^0=1, W_5^3=conj(W_5^2), W_5^4=conj(W_5^1).
			for q := 1; q < 5; q++ {
				sum := s0
				sj := [4]complex128{s1, s2, s3, s4}
				for j := 1; j <= 4; j++ {
					jq := (j * q) % 5
					var wq complex128
					switch jq {
					case 0:
						wq = 1
					case 1:
						wq = complex(cos15, sin15)
					case 2:
						wq = complex(cos25, sin25)
					case 3:
						wq = complex(cos25, -sin25)
					case 4:
						wq = complex(cos15, -sin15)
					}
					sum += wq * sj[j-1]
				}
				result[k+q*m] = sum
			}
		}
	}

	return result
}

// itoa is a tiny stdlib-free int formatter for the size-mismatch
// panic message. Avoids pulling strconv just for one panic.
func itoa(n int) string {
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
