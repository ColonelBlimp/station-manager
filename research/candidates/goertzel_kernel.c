// SIMD-accelerated 8-tone Goertzel kernel for the FT8 candidate
// verifier. Processes a window of float32 samples and returns the
// |X(f_k)|² power for each of 8 frequencies given via Goertzel
// coefficients c_k = 2·cos(2π·f_k/Fs).
//
// Implementation: AVX2 + FMA3. Two 256-bit lanes hold 4 doubles
// each, covering all 8 Goertzel state pairs in parallel. The
// per-sample inner loop is 4 FMAs + 4 broadcasts + a small bundle
// of register copies — the CPU's superscalar pipeline can issue
// all 4 lanes' FMAs simultaneously, hiding latency that the
// scalar Goertzel's serial dependency would expose.
//
// Build: -O3 -mavx2 -mfma (set via the cgo CFLAGS in
// goertzel_simd.go).
//
// Licence: MIT (hand-written by the SM project; no third-party
// code).

#include <immintrin.h>

// sm_goertzel_multi_avx2 runs 8 Goertzel recursions in parallel
// over n_samples float32 inputs starting at `samples`. The
// coefficient array holds c_k = 2·cos(2π·f_k/Fs) for k=0..7.
// Output `energies[k]` is the closed-form power |X(f_k)|² =
// s_{N-1}² + s_{N-2}² - c_k·s_{N-1}·s_{N-2} for each tone.
//
// The target() attribute enables AVX2 + FMA3 inside this function
// without requiring -mavx2 / -mfma on the global CFLAGS (cgo
// blocks those flags by default for security). The function is
// only called on amd64 builds where AVX2 is essentially universal
// since Haswell (2013).
__attribute__((target("avx2,fma")))
void sm_goertzel_multi_avx2(
    const float *samples,
    int n_samples,
    const double *coeffs,
    double *energies) {
    // Load coefficients into two 256-bit lanes.
    __m256d c_lo = _mm256_loadu_pd(coeffs);        // tones 0-3
    __m256d c_hi = _mm256_loadu_pd(coeffs + 4);    // tones 4-7

    // State vectors. s_lo holds tones 0-3, s_hi holds 4-7. _1 is
    // s_{n-1}, _2 is s_{n-2}.
    __m256d s_lo_1 = _mm256_setzero_pd();
    __m256d s_lo_2 = _mm256_setzero_pd();
    __m256d s_hi_1 = _mm256_setzero_pd();
    __m256d s_hi_2 = _mm256_setzero_pd();

    for (int i = 0; i < n_samples; i++) {
        // Broadcast the sample (a single float32, promoted to
        // double then splatted across all 4 lanes).
        __m256d x = _mm256_set1_pd((double)samples[i]);

        // s_new = x + c·s_1 - s_2.  Encoded as:
        //   tmp  = c·s_1 - s_2     (FMS — fused multiply-subtract)
        //   s_new = tmp + x        (add)
        // FMS handles the subtraction with one instruction.
        __m256d new_lo = _mm256_fmsub_pd(c_lo, s_lo_1, s_lo_2);
        new_lo = _mm256_add_pd(new_lo, x);
        s_lo_2 = s_lo_1;
        s_lo_1 = new_lo;

        __m256d new_hi = _mm256_fmsub_pd(c_hi, s_hi_1, s_hi_2);
        new_hi = _mm256_add_pd(new_hi, x);
        s_hi_2 = s_hi_1;
        s_hi_1 = new_hi;
    }

    // Closed-form power per tone: s1² + s2² - c·s1·s2.
    //   = FMA(s2, s2, s1*s1)        // s1² + s2²
    //   - FMA(c·s1, s2, 0)          // - c·s1·s2  (via FNMA)
    __m256d s1s1_lo = _mm256_mul_pd(s_lo_1, s_lo_1);
    __m256d e_lo = _mm256_fmadd_pd(s_lo_2, s_lo_2, s1s1_lo);
    __m256d cs1s2_lo = _mm256_mul_pd(s_lo_1, s_lo_2);
    e_lo = _mm256_fnmadd_pd(c_lo, cs1s2_lo, e_lo);
    _mm256_storeu_pd(energies, e_lo);

    __m256d s1s1_hi = _mm256_mul_pd(s_hi_1, s_hi_1);
    __m256d e_hi = _mm256_fmadd_pd(s_hi_2, s_hi_2, s1s1_hi);
    __m256d cs1s2_hi = _mm256_mul_pd(s_hi_1, s_hi_2);
    e_hi = _mm256_fnmadd_pd(c_hi, cs1s2_hi, e_hi);
    _mm256_storeu_pd(energies + 4, e_hi);
}
