// fft_mixedradix.go — mixed-radix Cooley-Tukey FFT for 5-smooth sizes.
//
// When N = 2^a × 3^b × 5^c (a "5-smooth" or "regular" number), the DFT can
// be computed in O(N log N) with the Cooley-Tukey mixed-radix algorithm,
// using radix-2, radix-3, and radix-5 butterfly kernels. This avoids the
// overhead of Bluestein's algorithm, which pads to the next power of 2 and
// performs three FFTs of that padded size.
//
// Hot-path sizes that benefit:
//   - NFFT1 = 192000 = 2⁹ × 3 × 5³  (long FFT in baseband pipeline)
//   - NFFT2 = 3200   = 2⁷ × 5²       (short IFFT in baseband pipeline)
//   - 3840           = 2⁸ × 3 × 5     (spectrogram FFT)
//
// The implementation uses decimation-in-frequency (DIF) for the forward
// transform. Factor ordering is largest-first (5, 3, 2) so the outermost
// loops handle the largest strides (better cache locality for big N),
// and the innermost radix-2 butterflies are the cheapest.
//
// Twiddle factors are precomputed and cached per FFT size, matching the
// caching pattern used by the radix-2 FFT in fft.go.

package dsp

import (
	"math"
	"sync"
)

// is5Smooth returns true if n > 0 and n's only prime factors are 2, 3, and 5.
func is5Smooth(n int) bool {
	if n <= 0 {
		return false
	}
	for n%2 == 0 {
		n /= 2
	}
	for n%3 == 0 {
		n /= 3
	}
	for n%5 == 0 {
		n /= 5
	}
	return n == 1
}

// factorize235 returns the prime factorisation of n using only factors 2, 3,
// and 5, ordered largest-first (5s, then 3s, then 2s). This ordering places
// the largest radix in the outermost loops for better cache behaviour, and
// the cheapest radix-2 butterflies innermost.
//
// Panics if n is not 5-smooth (caller must check with is5Smooth first).
func factorize235(n int) []int {
	if n <= 0 || !is5Smooth(n) {
		panic("dsp.factorize235: n is not 5-smooth")
	}

	var factors []int

	for n%5 == 0 {
		factors = append(factors, 5)
		n /= 5
	}
	for n%3 == 0 {
		factors = append(factors, 3)
		n /= 3
	}
	for n%2 == 0 {
		factors = append(factors, 2)
		n /= 2
	}

	return factors
}

// --- Mixed-radix twiddle cache ---

// mixedRadixTable holds precomputed data for all stages of a mixed-radix
// FFT of size N: the factor sequence and twiddle factors per stage.
type mixedRadixTable struct {
	factors []int          // factor sequence, largest-first
	twiddle [][]complex128 // twiddle[stage]: (r-1)*m_prev entries per stage
}

var (
	mixedRadixMu    sync.Mutex
	mixedRadixCache = make(map[int]*mixedRadixTable)
)

// getMixedRadixTable returns cached twiddle factors for a mixed-radix FFT
// of size n. On first call for a given n, factors and twiddles are computed.
func getMixedRadixTable(n int) *mixedRadixTable {
	mixedRadixMu.Lock()
	defer mixedRadixMu.Unlock()

	if t, ok := mixedRadixCache[n]; ok {
		return t
	}

	factors := factorize235(n)
	nStages := len(factors)

	t := &mixedRadixTable{
		factors: factors,
		twiddle: make([][]complex128, nStages),
	}

	// DIT stages process factors in reverse order (smallest-first).
	// Stage s uses radix r = factors[nf-1-s].
	// The sub-DFT size at stage s is: m = product of factors[nf-1], factors[nf-2], ..., factors[nf-1-s].
	// m_prev = m / r.
	//
	// Twiddle factors: W_m^{j*k} = exp(-2πi*j*k/m) for j=1..r-1, k=0..m_prev-1.
	// Stored in row-major: tw[(j-1)*m_prev + k]. j=0 needs no twiddle (always 1).
	m := 1
	for s := range nStages {
		r := factors[nStages-1-s]
		mPrev := m
		m *= r

		tw := make([]complex128, (r-1)*mPrev)
		baseAngle := -2 * math.Pi / float64(m)

		for j := 1; j < r; j++ {
			off := (j - 1) * mPrev
			for k := range mPrev {
				angle := baseAngle * float64(j) * float64(k)
				tw[off+k] = complex(math.Cos(angle), math.Sin(angle))
			}
		}

		t.twiddle[s] = tw
	}

	mixedRadixCache[n] = t
	return t
}

// --- Mixed-radix DFT ---

// mixedRadixDFT computes an in-place N-point DFT of x using the
// Cooley-Tukey mixed-radix algorithm. len(x) must be 5-smooth.
//
// Uses decimation-in-time (DIT):
//  1. Apply digit-reversal permutation.
//  2. Process stages from innermost (smallest sub-DFT) to outermost,
//     using radix-2, radix-3, or radix-5 butterflies.
//
// This mirrors the structure of fftDIT but generalised to mixed radices.
func mixedRadixDFT(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	tab := getMixedRadixTable(n)
	factors := tab.factors
	nStages := len(factors)

	// Step 1: Digit-reversal permutation.
	digitReversalPermute(x, factors)

	// Step 2: DIT butterfly stages.
	// Stages process factors in reverse order: factors[nf-1], factors[nf-2], ...
	m := 1 // current sub-DFT size before this stage
	for s := range nStages {
		r := factors[nStages-1-s] // radix for this stage
		mPrev := m                // previous sub-DFT size
		m *= r                    // new sub-DFT size after this stage
		numGroups := n / m        // number of sub-DFTs at this size
		tw := tab.twiddle[s]

		for g := range numGroups {
			base := g * m

			switch r {
			case 2:
				butterfly2DIT(x, base, mPrev, tw)
			case 3:
				butterfly3DIT(x, base, mPrev, tw)
			case 5:
				butterfly5DIT(x, base, mPrev, tw)
			}
		}
	}
}

// butterfly2DIT performs a radix-2 DIT butterfly combining 2 sub-DFTs of
// size mPrev into one DFT of size 2*mPrev.
//
// For k = 0..mPrev-1:
//
//	a = x[base + k],  b = x[base + mPrev + k] * tw[k]
//	x[base + k]       = a + b
//	x[base + mPrev+k] = a - b
func butterfly2DIT(x []complex128, base, mPrev int, tw []complex128) {
	for k := range mPrev {
		i0 := base + k
		i1 := i0 + mPrev
		b := x[i1]
		if k > 0 {
			b *= tw[k] // tw[(1-1)*mPrev + k] = tw[k]
		}
		a := x[i0]
		x[i0] = a + b
		x[i1] = a - b
	}
}

// butterfly3DIT performs a radix-3 DIT butterfly combining 3 sub-DFTs of
// size mPrev into one DFT of size 3*mPrev.
//
// Uses the identity for a 3-point DFT with W = exp(-2πi/3):
//
//	re(W) = -1/2,  im(W) = -√3/2
func butterfly3DIT(x []complex128, base, mPrev int, tw []complex128) {
	const (
		cos2pi3 = -0.5                          // cos(2π/3)
		sin2pi3 = -0.86602540378443864676372317 // -sin(2π/3) for exp(-j)
	)

	for k := range mPrev {
		i0 := base + k
		i1 := i0 + mPrev
		i2 := i1 + mPrev

		a := x[i0]
		b := x[i1]
		c := x[i2]

		// Apply twiddle factors to b and c
		if k > 0 {
			b *= tw[k]       // tw[(1-1)*mPrev + k]
			c *= tw[mPrev+k] // tw[(2-1)*mPrev + k]
		}

		// 3-point DFT kernel
		t1 := b + c
		t2r := real(b) - real(c)
		t2i := imag(b) - imag(c)

		x[i0] = a + t1

		ar := real(a) + cos2pi3*real(t1)
		ai := imag(a) + cos2pi3*imag(t1)

		x[i1] = complex(ar-sin2pi3*t2i, ai+sin2pi3*t2r)
		x[i2] = complex(ar+sin2pi3*t2i, ai-sin2pi3*t2r)
	}
}

// butterfly5DIT performs a radix-5 DIT butterfly combining 5 sub-DFTs of
// size mPrev into one DFT of size 5*mPrev.
//
// Uses the standard 5-point DFT kernel with precomputed sin/cos constants.
func butterfly5DIT(x []complex128, base, mPrev int, tw []complex128) {
	const (
		c1 = 0.30901699437494742410229341  // cos(2π/5)
		c2 = -0.80901699437494742410229341 // cos(4π/5)
		s1 = -0.95105651629515357211643933 // -sin(2π/5)
		s2 = -0.58778525229247312916870595 // -sin(4π/5)
	)

	for k := range mPrev {
		i0 := base + k
		i1 := i0 + mPrev
		i2 := i1 + mPrev
		i3 := i2 + mPrev
		i4 := i3 + mPrev

		a := x[i0]
		b := x[i1]
		c := x[i2]
		d := x[i3]
		e := x[i4]

		// Apply twiddle factors
		if k > 0 {
			b *= tw[k]
			c *= tw[mPrev+k]
			d *= tw[2*mPrev+k]
			e *= tw[3*mPrev+k]
		}

		// 5-point DFT kernel (symmetric/antisymmetric decomposition)
		t1 := b + e // b + e
		t2 := c + d // c + d
		t3 := b - e // b - e
		t4 := c - d // c - d

		x[i0] = a + t1 + t2

		// j=1: W^1
		r1 := real(a) + c1*real(t1) + c2*real(t2)
		q1 := imag(a) + c1*imag(t1) + c2*imag(t2)
		r1 -= s1*imag(t3) + s2*imag(t4)
		q1 += s1*real(t3) + s2*real(t4)

		// j=2: W^2
		r2 := real(a) + c2*real(t1) + c1*real(t2)
		q2 := imag(a) + c2*imag(t1) + c1*imag(t2)
		r2 -= s2*imag(t3) - s1*imag(t4)
		q2 += s2*real(t3) - s1*real(t4)

		// j=3: W^3 (conjugate of W^2)
		r3 := real(a) + c2*real(t1) + c1*real(t2)
		q3 := imag(a) + c2*imag(t1) + c1*imag(t2)
		r3 += s2*imag(t3) - s1*imag(t4)
		q3 -= s2*real(t3) - s1*real(t4)

		// j=4: W^4 (conjugate of W^1)
		r4 := real(a) + c1*real(t1) + c2*real(t2)
		q4 := imag(a) + c1*imag(t1) + c2*imag(t2)
		r4 += s1*imag(t3) + s2*imag(t4)
		q4 -= s1*real(t3) + s2*real(t4)

		x[i1] = complex(r1, q1)
		x[i2] = complex(r2, q2)
		x[i3] = complex(r3, q3)
		x[i4] = complex(r4, q4)
	}
}

// --- Digit-reversal permutation ---

// digitReversalPermute reorders x from natural order to digit-reversed order
// for DIT processing. factors is the sequence of radices (largest-first).
//
// The digit-reversal is the mixed-radix generalisation of bit-reversal:
// index i is decomposed into digits (d0, d1, ...) using the factor sequence,
// and the reversed digit sequence (d_{n-1}, ..., d1, d0) gives the source
// index. The permuted output is: x_out[i] = x_in[σ(i)].
//
// For mixed radices, this permutation is NOT an involution (σ(σ(i)) ≠ i),
// so a temporary buffer is used instead of in-place swaps.
func digitReversalPermute(x []complex128, factors []int) {
	n := len(x)
	if n <= 1 {
		return
	}

	nf := len(factors)

	// strides[k] = product of factors[k+1..nf-1].
	strides := make([]int, nf)
	strides[nf-1] = 1
	for k := nf - 2; k >= 0; k-- {
		strides[k] = strides[k+1] * factors[k+1]
	}

	// revStrides[k] = strides for the reversed factor sequence.
	revStrides := make([]int, nf)
	revStrides[nf-1] = 1
	for k := nf - 2; k >= 0; k-- {
		revStrides[k] = revStrides[k+1] * factors[nf-2-k]
	}

	// Apply permutation: x_out[i] = x_in[σ(i)].
	temp := make([]complex128, n)
	copy(temp, x)

	for i := range n {
		j := 0
		tmp := i
		for k := range nf {
			digit := tmp / strides[k]
			tmp %= strides[k]
			j += digit * revStrides[nf-1-k]
		}
		x[i] = temp[j]
	}
}

// --- Unified dispatcher ---

// generalDFT computes an in-place forward DFT of x for any positive length.
// Routes to the most efficient algorithm:
//   - Power of 2: radix-2 Cooley-Tukey (fftDIT)
//   - 5-smooth:   mixed-radix Cooley-Tukey (mixedRadixDFT)
//   - Other:      Bluestein's algorithm (bluesteinDFT)
func generalDFT(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	if n&(n-1) == 0 {
		fftDIT(x) // power-of-2 fast path
	} else if is5Smooth(n) {
		mixedRadixDFT(x)
	} else {
		bluesteinDFT(x)
	}
}
