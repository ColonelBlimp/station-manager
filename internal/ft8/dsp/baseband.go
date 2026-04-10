// baseband.go — frequency-domain downsampling of FT8 signals to complex baseband.
//
// This is the Go port of WSJT-X's ft8_downsample.f90. For a given candidate
// frequency f0, the routine:
//
//  1. Computes a long real-to-complex FFT (NFFT1 = 192000) of the 15 s
//     audio capture (180 000 samples, zero-padded to 192 000).
//  2. Extracts the frequency-domain bins from f0 − 1.5×baud to f0 + 8.5×baud
//     (the 10-tone-wide band containing all 8 FT8 tones plus margin).
//  3. Applies a cosine taper (101-point half-cosine) to the band edges.
//  4. Circular-shifts so f0 maps to DC (bin 0 in the output).
//  5. Computes an NFFT2 = 3200-point inverse complex FFT.
//  6. Scales by 1/√(NFFT1 × NFFT2).
//
// The result is 3200 complex128 samples at 200 Hz (32 samples per FT8 symbol),
// with the candidate signal centered at DC.
//
// The long FFT (step 1) depends only on the audio data, not on f0, so it is
// computed once and reused across all candidates in a window via [LongFFT].
//
// Reference: WSJT-X lib/ft8/ft8_downsample.f90, ft8_params.f90.

package dsp

import "math"

// Downsample constants matching WSJT-X ft8_params.f90.
const (
	NFFT1 = 192000 // Long FFT size (zero-padded from 180000)
	NFFT2 = 3200   // Short IFFT size (192000 / 60)
	NDOWN = 60     // Downsample factor (12000 / 200)

	// BasebandRate is the complex baseband sample rate after downsampling.
	BasebandRate = float64(SampleRate) / float64(NDOWN) // 200 Hz

	// BasebandSamplesPerSymbol is the number of complex samples per FT8 symbol
	// at the baseband rate: 1920/60 = 32.
	BasebandSamplesPerSymbol = SamplesPerSymbol / NDOWN // 32

	// taperLen is the half-cosine taper length applied to band edges.
	taperLen = 101
)

// precomputed cosine taper: taper[i] = 0.5*(1 + cos(i*π/100)), i = 0..100.
var cosineTaper [taperLen]float64

func init() {
	for i := range taperLen {
		cosineTaper[i] = 0.5 * (1.0 + math.Cos(float64(i)*math.Pi/float64(taperLen-1)))
	}
}

// LongFFT computes the NFFT1-point real-to-complex FFT of the audio samples,
// returning the non-negative frequency bins (NFFT1/2 + 1 complex values).
//
// The input is zero-padded from len(samples) to NFFT1. This FFT depends only
// on the audio data and should be computed once per window, then passed to
// [DownsampleBaseband] for each candidate.
func LongFFT(samples []float32) []complex128 {
	x := make([]complex128, NFFT1)
	limit := len(samples)
	if limit > NFFT1 {
		limit = NFFT1
	}
	for i := 0; i < limit; i++ {
		x[i] = complex(float64(samples[i]), 0)
	}

	// NFFT1 = 192000 = 2⁷×3×5³ (5-smooth) → mixed-radix FFT.
	generalDFT(x)

	// Return only the non-negative frequency bins (real input symmetry).
	bins := NFFT1/2 + 1
	out := make([]complex128, bins)
	copy(out, x[:bins])
	return out
}

// DownsampleBaseband mixes a candidate frequency f0 to baseband and
// downsamples to 3200 complex samples at 200 Hz, matching WSJT-X's
// ft8_downsample.
//
// Parameters:
//   - longFFT: the NFFT1/2+1 complex frequency-domain bins from [LongFFT].
//   - f0: the candidate center frequency in Hz.
//
// Returns 3200 complex128 samples at 200 Hz sample rate, with f0 at DC.
func DownsampleBaseband(longFFT []complex128, f0 float64) []complex128 {
	df := float64(SampleRate) / float64(NFFT1) // bin width = 12000/192000 = 0.0625 Hz
	baud := float64(SampleRate) / float64(SamplesPerSymbol)

	i0 := int(math.Round(f0 / df)) // bin index of f0
	ft := f0 + 8.5*baud            // upper edge
	it := int(math.Round(ft / df)) // upper bin
	fb := f0 - 1.5*baud            // lower edge
	ib := int(math.Round(fb / df)) // lower bin
	if ib < 1 {
		ib = 1 // Skip DC bin (index 0), matching WSJT-X ft8_downsample.f90 line 36.
	}
	maxBin := NFFT1 / 2
	if it > maxBin {
		it = maxBin
	}

	// Extract bins [ib..it] into c1, zero-padded to NFFT2.
	c1 := make([]complex128, NFFT2)
	k := 0
	for i := ib; i <= it; i++ {
		if k >= NFFT2 {
			break
		}
		if i >= 0 && i < len(longFFT) {
			c1[k] = longFFT[i]
		}
		k++
	}

	// Apply cosine taper to the band edges.
	// Lower edge: c1[0..taperLen-1] *= taper[taperLen-1..0]
	for i := 0; i < taperLen && i < k; i++ {
		c1[i] *= complex(cosineTaper[taperLen-1-i], 0)
	}
	// Upper edge: c1[k-taperLen..k-1] *= taper[0..taperLen-1]
	for i := 0; i < taperLen && i < k; i++ {
		idx := k - 1 - taperLen + 1 + i
		if idx >= 0 && idx < k {
			c1[idx] *= complex(cosineTaper[i], 0)
		}
	}

	// Circular shift so f0 maps to DC: cshift by (i0 - ib).
	shift := i0 - ib
	if shift != 0 && k > 0 {
		cshiftSlice(c1, shift)
	}

	// Inverse complex FFT (NFFT2-point), unnormalized — matching WSJT-X's
	// four2a with isign=1 which does not include 1/N scaling.
	complexIFFTUnnorm(c1)

	// Scale by 1/sqrt(NFFT1 * NFFT2), matching WSJT-X.
	fac := 1.0 / math.Sqrt(float64(NFFT1)*float64(NFFT2))
	for i := range c1 {
		c1[i] *= complex(fac, 0)
	}

	return c1
}

// cshiftSlice performs a circular left-shift of a complex128 slice by n
// positions (matching Fortran's CSHIFT intrinsic).
func cshiftSlice(x []complex128, n int) {
	length := len(x)
	if length == 0 {
		return
	}
	n = n % length
	if n < 0 {
		n += length
	}
	if n == 0 {
		return
	}

	// Use the triple-reverse algorithm for in-place circular shift.
	reverseComplex(x[:n])
	reverseComplex(x[n:])
	reverseComplex(x)
}

// reverseComplex reverses a slice of complex128 in place.
func reverseComplex(x []complex128) {
	for i, j := 0, len(x)-1; i < j; i, j = i+1, j-1 {
		x[i], x[j] = x[j], x[i]
	}
}

// complexIFFT computes an in-place inverse FFT of x: IFFT(x) = conj(FFT(conj(x))) / N.
// len(x) can be any positive integer (uses Bluestein for non-power-of-2).
func complexIFFT(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	// Conjugate input.
	for i := range x {
		x[i] = complex(real(x[i]), -imag(x[i]))
	}

	// Forward DFT (auto-dispatches: radix-2, mixed-radix, or Bluestein).
	generalDFT(x)

	// Conjugate and scale.
	invN := 1.0 / float64(n)
	for i := range x {
		x[i] = complex(real(x[i])*invN, -imag(x[i])*invN)
	}
}

// complexIFFTUnnorm computes an in-place UNNORMALIZED inverse FFT:
// IFFT_unnorm(x)[n] = sum(X[k] * exp(+j*2*pi*k*n/N)).
// This matches WSJT-X's four2a with isign=1 (no 1/N scaling).
func complexIFFTUnnorm(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	// Conjugate input.
	for i := range x {
		x[i] = complex(real(x[i]), -imag(x[i]))
	}

	// Forward DFT (auto-dispatches: radix-2, mixed-radix, or Bluestein).
	generalDFT(x)

	// Conjugate only (no 1/N scaling).
	for i := range x {
		x[i] = complex(real(x[i]), -imag(x[i]))
	}
}
