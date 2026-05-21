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
//
// **Performance note.** This entry point allocates an ad-hoc Plan
// on every call, recomputing twiddle factors and scratch buffers
// from scratch. Callers that run many FFTs of the same size N
// (e.g. dsp.Spectrogram's 372 × FFT(3840) per slot) should build
// one Plan up-front with NewPlan(N) and call Plan.FFT instead —
// the precomputed twiddle table + workspace arena cut wall time
// ~50% and allocations >99% for batched same-size FFT workloads.
func FFT(x []complex128) []complex128 {
	return NewPlan(len(x)).FFT(x)
}

// IFFT computes the inverse complex-to-complex DFT, normalised by
// 1/N:
//
//	x[n] = (1/N) · Σ_{k=0..N-1} X[k] · exp(+j · 2π · k · n / N)
//
// Implemented via the standard conjugate-trick:
// IFFT(X) = (1/N) · conj(FFT(conj(X))). Sizes follow the same
// 5-smooth constraint as FFT. Same performance caveat as FFT —
// callers running many same-N IFFTs should reuse a Plan via
// NewPlan(N) + Plan.IFFT.
func IFFT(x []complex128) []complex128 {
	return NewPlan(len(x)).IFFT(x)
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

// Plan holds precomputed state for repeated FFT calls of the same
// size N. Reuse a single Plan across many same-N transforms — the
// constructor precomputes the twiddle table and workspace arena
// once, and each Plan.FFT / Plan.IFFT call reuses both instead of
// allocating per-call.
//
// **Allocation cost.** Each FFT call allocates exactly one
// complex128 slice of length N (the returned result). The recursion
// itself uses Plan-owned workspace buffers — no per-level
// allocations as the original FFT did. For a typical FT8 slot
// (Spectrogram = 372 × FFT(3840)), this drops allocation count
// roughly 11M → 372 and CPU time roughly 50%.
//
// **Thread safety.** A Plan is NOT safe for concurrent use; the
// workspace buffers are shared mutable state. Callers needing
// parallel FFTs should construct one Plan per goroutine.
type Plan struct {
	// n is the FFT size this Plan was built for. All inputs to
	// Plan.FFT / Plan.IFFT must have len == n.
	n int

	// twiddles[k] = exp(-j · 2π · k / n) for k = 0..n-1. The
	// recursive butterfly at sub-level of size m reads
	// twiddles[k · (n/m)] for its k-th butterfly twiddle, so this
	// master table serves every recursion level.
	twiddles []complex128

	// wsA, wsB are two workspace buffers of length n each. The
	// recursion alternates between them at each level — at level
	// L the "input" buffer is one and the "scratch" buffer is the
	// other; one level deeper the roles flip. Total memory: 2N.
	wsA, wsB []complex128
}

// NewPlan builds a Plan for size n. n must be 5-smooth (factors
// of 2, 3, 5 only); non-5-smooth sizes panic — the same contract
// as the raw FFT entry point.
func NewPlan(n int) *Plan {
	if n <= 1 {
		// Trivial sizes — Plan still works but does no recursion.
		return &Plan{n: n}
	}
	// Validate 5-smoothness up-front (panics if not). Run through
	// the smallest-factor decomposition to catch the violation
	// before any allocation.
	m := n
	for m > 1 {
		p := smallestFactor(m)
		m /= p
	}

	p := &Plan{
		n:        n,
		twiddles: make([]complex128, n),
		wsA:      make([]complex128, n),
		wsB:      make([]complex128, n),
	}
	twopi := 2.0 * math.Pi
	for k := 0; k < n; k++ {
		angle := -twopi * float64(k) / float64(n)
		sin, cos := math.Sincos(angle)
		p.twiddles[k] = complex(cos, sin)
	}
	return p
}

// FFT runs Plan's precomputed transform on x. Returns a freshly-
// allocated result slice of length n. The input x is not mutated.
//
// Panics if len(x) != p.n.
func (p *Plan) FFT(x []complex128) []complex128 {
	if len(x) != p.n {
		panic("audio.Plan.FFT: input length " + itoa(len(x)) + " does not match plan size " + itoa(p.n))
	}
	out := make([]complex128, p.n)
	if p.n <= 1 {
		copy(out, x)
		return out
	}
	copy(p.wsA, x)
	p.execute(p.wsA, p.wsB, 1)
	copy(out, p.wsA)
	return out
}

// IFFT runs Plan's precomputed inverse transform on x. Returns a
// freshly-allocated result slice of length n, normalised by 1/n.
// Implemented via the standard conjugate-trick:
// IFFT(X) = (1/N) · conj(FFT(conj(X))).
//
// Panics if len(x) != p.n.
func (p *Plan) IFFT(x []complex128) []complex128 {
	if len(x) != p.n {
		panic("audio.Plan.IFFT: input length " + itoa(len(x)) + " does not match plan size " + itoa(p.n))
	}
	out := make([]complex128, p.n)
	if p.n <= 1 {
		copy(out, x)
		return out
	}
	for i, v := range x {
		p.wsA[i] = cmplx.Conj(v)
	}
	p.execute(p.wsA, p.wsB, 1)
	scale := 1.0 / float64(p.n)
	for i, v := range p.wsA {
		out[i] = complex(real(v)*scale, -imag(v)*scale)
	}
	return out
}

// execute is the in-place mixed-radix Cooley-Tukey recursion. It
// reads from `in`, uses `scratch` as workspace for the decimation
// step + recursive sub-FFTs, and writes the final result back into
// `in`. stride is the master-table stride: at level n, butterfly
// twiddle for index k is p.twiddles[k * stride], because the
// outer plan has size p.n and the current level's W_n^k =
// W_{p.n}^(k · p.n / n) = twiddles[k · stride] with stride = p.n / n.
//
// Both in and scratch must have length n; they alternate roles at
// each recursion level. The caller (FFT / IFFT) seeds the top-level
// call with the input data in `in` and an unused scratch buffer.
//
// **Correctness sketch.** At level n with radix p and sub-length
// m = n/p:
//   - Decimate `in` into `scratch`: scratch[j*m + k] = in[k*p + j],
//     so scratch[j*m..(j+1)*m] holds the j-th interleaved sub-array.
//   - Recurse on each sub-array, using in[j*m..(j+1)*m] as its
//     scratch (we no longer need in's original values — they were
//     copied to scratch in the previous step). After recursion
//     scratch[j*m..(j+1)*m] holds the FFT of the j-th sub-array.
//   - Butterfly: combine all p sub-FFT results from scratch into
//     in (per the QEX-paper-style mixed-radix Cooley-Tukey
//     decimation-in-time formula).
func (p *Plan) execute(in, scratch []complex128, stride int) {
	n := len(in)
	if n <= 1 {
		return
	}

	radix := smallestFactor(n)
	m := n / radix

	// Decimate in → scratch (interleaved sub-arrays laid out
	// contiguously).
	for j := 0; j < radix; j++ {
		for k := 0; k < m; k++ {
			scratch[j*m+k] = in[k*radix+j]
		}
	}

	// Recurse on each sub-array. The sub-array (scratch[j*m..]) is
	// the recursive call's "in"; the corresponding region of THIS
	// level's `in` becomes the recursive scratch.
	subStride := stride * radix
	for j := 0; j < radix; j++ {
		subIn := scratch[j*m : (j+1)*m]
		subScratch := in[j*m : (j+1)*m]
		p.execute(subIn, subScratch, subStride)
	}

	// Butterfly: combine the p sub-FFTs in scratch into `in` per
	// the mixed-radix DFT formula. At index k (k in [0, m)) the
	// output for the q-th band (q in [0, radix)) is:
	//
	//	X[q*m + k] = Σ_{j=0..radix-1} W_radix^(j·q) · w_jk · S_j[k]
	//
	// where w_jk = twiddles[j*k*stride] is the butterfly twiddle
	// and W_radix^(j·q) is the per-radix DFT root.
	//
	// We pre-multiply s_j' = w_jk · S_j[k] then apply the inner
	// DFT to get all p outputs.
	switch radix {
	case 2:
		// W_2 = -1. X[k] = s0 + s1·w_k; X[m+k] = s0 - s1·w_k.
		for k := 0; k < m; k++ {
			s0 := scratch[k]
			w := p.twiddles[k*stride]
			t := w * scratch[m+k]
			in[k] = s0 + t
			in[k+m] = s0 - t
		}

	case 3:
		// W_3 = exp(-j·2π/3) = -1/2 - j·√3/2.
		const (
			cos3 = -0.5                   // cos(2π/3)
			sin3 = -0.8660254037844386468 // -sin(2π/3)
		)
		for k := 0; k < m; k++ {
			s0 := scratch[k]
			w1 := p.twiddles[k*stride]
			w2 := p.twiddles[2*k*stride]
			s1 := w1 * scratch[m+k]
			s2 := w2 * scratch[2*m+k]

			// 3-point DFT (s0 has no twiddle since W_3^0 = 1 for j=0):
			//	X[0]   = s0 + s1 + s2
			//	X[m]   = s0 + W_3 · s1 + W_3^2 · s2
			//	X[2m]  = s0 + W_3^2 · s1 + W_3 · s2
			t1 := s1 + s2
			t2 := s1 - s2
			in[k] = s0 + t1
			in[k+m] = s0 + complex(cos3*real(t1)-sin3*imag(t2), cos3*imag(t1)+sin3*real(t2))
			in[k+2*m] = s0 + complex(cos3*real(t1)+sin3*imag(t2), cos3*imag(t1)-sin3*real(t2))
		}

	case 5:
		// W_5 = exp(-j·2π/5). Inner DFT roots (4 unique values:
		// W_5, W_5^2, conj(W_5^2), conj(W_5)).
		twopi := 2.0 * math.Pi
		cos15 := math.Cos(twopi / 5)
		sin15 := -math.Sin(twopi / 5)
		cos25 := math.Cos(2 * twopi / 5)
		sin25 := -math.Sin(2 * twopi / 5)

		for k := 0; k < m; k++ {
			s0 := scratch[k]
			w1 := p.twiddles[k*stride]
			w2 := p.twiddles[2*k*stride]
			w3 := p.twiddles[3*k*stride]
			w4 := p.twiddles[4*k*stride]
			s1 := w1 * scratch[m+k]
			s2 := w2 * scratch[2*m+k]
			s3 := w3 * scratch[3*m+k]
			s4 := w4 * scratch[4*m+k]

			in[k] = s0 + s1 + s2 + s3 + s4
			sj := [4]complex128{s1, s2, s3, s4}
			for q := 1; q < 5; q++ {
				sum := s0
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
				in[k+q*m] = sum
			}
		}
	}
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
