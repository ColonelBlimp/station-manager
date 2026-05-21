package audio

import "math"

// Real-to-complex FFT for real-valued input. The standard naive
// approach packs N real samples into N complex (zero-imaginary)
// values and runs an N-point complex FFT, then discards the
// redundant upper half of the output. That wastes ~2× the work —
// for real input the FFT output has Hermitian symmetry X[N-k] =
// conj(X[k]), so only N/2+1 unique bins exist.
//
// This implementation uses the standard "pack and unpack" trick
// (the same technique FFTW's r2c does internally):
//
//  1. Pack N real values into N/2 complex values:
//       z[k] = x[2k] + j·x[2k+1]
//  2. Compute an N/2-point complex FFT of z → Z[0..N/2-1]
//  3. Unpack Z into the N/2+1 unique real-FFT outputs using
//     Hermitian symmetry relations.
//
// Net result: about half the work of the naive approach. For SM's
// hot path that's the Spectrogram's 372 sliding-window FFTs of
// size NFFT1=3840 — RealPlan halves that work.
//
// **Reference:** Brigham, "The Fast Fourier Transform", §10-5 (1974);
// any modern DSP textbook section on "real-valued FFTs". Standard
// technique, predates FT8 by decades — not a port of any specific
// implementation.
//
// **Provenance.** Algorithm adapted into SM from the operator's own
// `go-ft8/research/realfft.go` (MIT-able, operator-authored;
// describes the standard textbook pack-and-unpack technique with
// reference to Brigham §10-5).

// RealPlan holds precomputed state for repeated real-to-complex
// FFTs of the same size N. Internally wraps a half-size complex
// Plan (size N/2) plus a precomputed twiddle table for the unpack
// stage. Reuse a single RealPlan across many same-N transforms —
// the constructor does the table + half-plan setup once, then
// each FFT call reuses both.
//
// **Allocation cost.** Each FFT call allocates exactly one slice
// — the returned result of length N/2+1. The packed-input scratch
// is owned by the RealPlan (pooled across calls) and the half-
// size complex FFT's internal workspace is owned by the half-Plan.
// For Spectrogram's 372 × FFT(N=3840) per slot, RealPlan trims
// both compute time and allocation count vs the complex-Plan path.
//
// **Thread safety.** Not safe for concurrent use; the underlying
// half-size Plan + packed-input scratch are shared mutable state.
// Callers needing parallel real-FFTs should construct one
// RealPlan per goroutine (same rule as the complex Plan).
type RealPlan struct {
	// n is the LOGICAL FFT size — the number of real input samples.
	// Must be even (n/2 must be 5-smooth so the half Plan works).
	n int

	// halfPlan is the (n/2)-point complex Plan that does the heavy
	// lifting. The N/2 number-theoretic constraint matches because
	// our FT8 sizes are 5-smooth and n/2 stays 5-smooth (e.g.
	// 3840 = 2^7·3·5 → 1920 = 2^6·3·5, both 5-smooth).
	halfPlan *Plan

	// twiddles[k] = exp(-j·2π·k/n) for k = 0..n/4-1. The unpack
	// stage's combining-twiddle for output bin k is read from this
	// table with half-cycle symmetry: w(n/2-k) = -conj(w(k)) so
	// only n/4 values are stored.
	twiddles []complex128

	// zScratch is the packed-input buffer of length n/2 used by
	// every FFT call to hold z[k] = x[2k] + j·x[2k+1] before
	// handing off to halfPlan. Owned by the RealPlan; refilled
	// per call. Not safe under concurrent use.
	zScratch []complex128
}

// NewRealPlan builds a RealPlan for size n. n must be even and
// n/2 must be 5-smooth (factors of 2, 3, 5 only) — the standard
// Plan size constraint applied to the half-size internal FFT.
// Non-conforming sizes panic.
func NewRealPlan(n int) *RealPlan {
	if n <= 1 {
		return &RealPlan{n: n}
	}
	if n%2 != 0 {
		panic("audio.NewRealPlan: size must be even; got " + itoa(n))
	}
	half := n / 2
	quarter := half / 2
	p := &RealPlan{
		n:        n,
		halfPlan: NewPlan(half), // validates 5-smoothness, panics if not
		twiddles: make([]complex128, quarter),
		zScratch: make([]complex128, half),
	}
	twopi := 2.0 * math.Pi
	for k := range p.twiddles {
		angle := -twopi * float64(k) / float64(n)
		sin, cos := math.Sincos(angle)
		p.twiddles[k] = complex(cos, sin)
	}
	return p
}

// FFT computes the forward real-to-complex DFT of x. Input length
// may be less than p.n; missing values are treated as zero (caller
// can rely on zero-padding behaviour).
//
// Returns p.n/2 + 1 complex values representing the positive-
// frequency half of the spectrum. The negative half is the
// conjugate mirror (X[N-k] = conj(X[k])) and is not returned —
// callers that need it can compute it.
//
// Output convention matches the existing complex Plan.FFT — same
// unnormalised DFT, indexed identically — so a caller switching
// from `Plan.FFT(real-padded-with-zeros)` to `RealPlan.FFT(real)`
// sees the same X[k] for k in [0, n/2].
//
// Panics if p.n <= 1 (trivial sizes); the contract requires a
// real input that maps to at least one output bin.
func (p *RealPlan) FFT(x []float32) []complex128 {
	if p.n <= 1 {
		// Degenerate cases — return single element for n=1, empty for n=0.
		out := make([]complex128, p.n)
		if p.n == 1 && len(x) > 0 {
			out[0] = complex(float64(x[0]), 0)
		}
		return out
	}

	half := p.n / 2
	lx := len(x)

	// 1. Pack N real values into N/2 complex values:
	//    z[k] = x[2k] + j·x[2k+1].
	// Missing input samples (lx < n) are treated as zero, matching
	// the existing "complex-with-zero-imaginary" path's zero-pad.
	// Uses the RealPlan-owned scratch (refilled per call).
	z := p.zScratch
	for k := 0; k < half; k++ {
		var re, im float64
		if 2*k < lx {
			re = float64(x[2*k])
		}
		if 2*k+1 < lx {
			im = float64(x[2*k+1])
		}
		z[k] = complex(re, im)
	}

	// 2. N/2-point complex FFT via the cached half-size Plan.
	Z := p.halfPlan.FFT(z)

	// 3. Unpack Z into the N/2+1 real-FFT outputs.
	//
	// Given Z[k] = DFT_{N/2}(z)[k], the full-length DFT X[k] is:
	//
	//   Xe[k] = (Z[k] + conj(Z[N/2-k])) / 2          (even-indexed DFT)
	//   Xo[k] = -j · (Z[k] - conj(Z[N/2-k])) / 2     (odd-indexed DFT)
	//   X[k]  = Xe[k] + W_N^k · Xo[k]                (recombine)
	//
	// where W_N^k = exp(-j·2π·k/N), and Z indices wrap mod N/2.
	out := make([]complex128, half+1)

	// Special cases: k=0 and k=N/2 are real-valued for real input.
	out[0] = complex(real(Z[0])+imag(Z[0]), 0)
	out[half] = complex(real(Z[0])-imag(Z[0]), 0)

	// General case: k = 1..N/2-1. Twiddles for k in [0, N/4) come
	// straight from p.twiddles; for k = N/4 the value is -j; for
	// k in (N/4, N/2) use half-cycle symmetry: W^k = -conj(W^(N/2-k)).
	quarter := half / 2
	for k := 1; k < half; k++ {
		A := Z[k]                                       // Z[k]
		B := complex(real(Z[half-k]), -imag(Z[half-k])) // conj(Z[N/2-k])

		// Even part: (A + B) / 2.
		xe := (A + B) * complex(0.5, 0)
		// Odd part: -j · (A - B) / 2 = (imag(A-B) - j·real(A-B)) / 2.
		diff := A - B
		xo := complex(imag(diff), -real(diff)) * complex(0.5, 0)

		var tw complex128
		switch {
		case k < quarter:
			tw = p.twiddles[k]
		case k == quarter:
			tw = complex(0, -1) // exp(-jπ/2) = -j
		default:
			t := p.twiddles[half-k]
			tw = complex(-real(t), imag(t)) // -conj(p.twiddles[half-k])
		}

		out[k] = xe + tw*xo
	}

	return out
}
