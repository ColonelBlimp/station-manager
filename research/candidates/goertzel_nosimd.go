//go:build !amd64 || !cgo

package candidates

// hasSIMDGoertzel is the build-time gate for the SIMD kernel
// dispatcher. On non-amd64 or CGO-disabled builds the SIMD
// implementation is unavailable; verifyCostas falls back to the
// pure-Go goertzelMulti.
const hasSIMDGoertzel = false

// goertzelMultiSIMDInto has the same signature as the AVX2 kernel
// but is never called on these builds (hasSIMDGoertzel = false
// short-circuits the dispatcher). The body just routes to the
// pure-Go implementation so the symbol exists for the dispatcher's
// branch to compile.
func goertzelMultiSIMDInto(samples []float32, start, n int, coeffs *[ft8ToneCount]float64, energies *[ft8ToneCount]float64) {
	*energies = goertzelMulti(samples, start, n, *coeffs)
}
