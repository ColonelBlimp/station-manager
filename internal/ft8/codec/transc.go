package codec

import "math"

// tanh and atanh are package-local wrappers around the transcendental
// functions used by LDPCDecodeBP's message-passing update. The
// indirection exists in case a future drop-in alternative
// (lookup table, polynomial approximation, etc.) replaces the
// stdlib calls without touching the decoder.
//
// **Why not CGO Sleef?** Tried, measured 2.7× slower (Session 80).
// CGO's per-call overhead (~200 ns) dominated math.Tanh's actual
// compute (~50-100 ns inline); scalar interop is a loss for the
// ~3 M tanh+atanh calls per slot in BP. To beat pure-Go would
// require SIMD batching (Sleef vector variants with N inputs per
// cgo call) — a significant BP refactor. See memory note
// `feedback_cgo_scalar_interop_overhead.md` for details.

func tanh(x float64) float64  { return math.Tanh(x) }
func atanh(x float64) float64 { return math.Atanh(x) }
