//go:build amd64 && cgo

package candidates

/*
#cgo CFLAGS: -O3 -Wall

void sm_goertzel_multi_avx2(
    const float *samples,
    int n_samples,
    const double *coeffs,
    double *energies);
*/
import "C"

import "unsafe"

// hasSIMDGoertzel is the build-time flag that gates whether
// verifyCostas dispatches to the AVX2-accelerated kernel. Always
// true on this build (amd64 + cgo); a parallel non-cgo file flips
// it to false on platforms / build modes where CGO is unavailable.
const hasSIMDGoertzel = true

// goertzelMultiSIMDInto wraps the hand-written AVX2 + FMA3 C
// kernel. The kernel runs 8 Goertzel recursions in parallel across
// two 256-bit lanes (each holding 4 doubles) and uses fused
// multiply-subtract for the per-sample update. Several times faster
// than the pure-Go equivalent.
//
// Caller-provided output convention. The earlier
// `return [ft8ToneCount]float64` API made Go's escape analysis spill
// the return array to the heap (because the C call took its
// address) — 42 allocs per verifyCostas call. Passing `energies`
// in by pointer keeps it on the verifier's stack, eliminating the
// alloc-per-anchor.
//
// All pointers passed to C reference Go-stack or Go-slice memory
// that the synchronous C call cannot retain past return; no GC
// hazards.
func goertzelMultiSIMDInto(samples []float32, start, n int, coeffs *[ft8ToneCount]float64, energies *[ft8ToneCount]float64) {
	C.sm_goertzel_multi_avx2(
		(*C.float)(unsafe.Pointer(&samples[start])),
		C.int(n),
		(*C.double)(unsafe.Pointer(&coeffs[0])),
		(*C.double)(unsafe.Pointer(&energies[0])),
	)
}
