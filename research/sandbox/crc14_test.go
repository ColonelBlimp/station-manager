package sandbox

import (
	"math/rand"
	"testing"
)

// referenceComputeCRC14 is the byte-array implementation that was the
// original port of ref [14] gen_crc14.f90. Retained inline in this test
// file as the equivalence reference for the fast integer version.
// Do not call from production code — slow.
func referenceComputeCRC14(msg77 []uint8) [LDPCCRCBits]uint8 {
	if len(msg77) != LDPCPayloadBits {
		panic("referenceComputeCRC14: input must be 77 bits")
	}
	polyBits := [15]uint8{1, 1, 0, 0, 1, 1, 1, 0, 1, 0, 1, 0, 1, 1, 1}
	var mc [96]uint8
	copy(mc[:], msg77)
	var r [15]uint8
	copy(r[:], mc[0:15])
	for i := 0; i < 82; i++ {
		r[14] = mc[i+14]
		if r[0] == 1 {
			for j := 0; j < 15; j++ {
				r[j] ^= polyBits[j]
			}
		}
		first := r[0]
		for j := 0; j < 14; j++ {
			r[j] = r[j+1]
		}
		r[14] = first
	}
	var crc [LDPCCRCBits]uint8
	copy(crc[:], r[0:LDPCCRCBits])
	return crc
}

// TestComputeCRC14_MatchesReference exercises computeCRC14 against the
// preserved byte-array reference implementation across a large pool of
// random 77-bit inputs and pinned edge cases.
func TestComputeCRC14_MatchesReference(t *testing.T) {
	cases := [][LDPCPayloadBits]uint8{
		{}, // all zeros
		{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, // first 21 set
	}
	// All-ones reference
	var allOnes [LDPCPayloadBits]uint8
	for i := range allOnes {
		allOnes[i] = 1
	}
	cases = append(cases, allOnes)

	// Alternating
	var alt [LDPCPayloadBits]uint8
	for i := range alt {
		alt[i] = uint8(i % 2)
	}
	cases = append(cases, alt)

	rng := rand.New(rand.NewSource(20260528))
	for k := 0; k < 1000; k++ {
		var msg [LDPCPayloadBits]uint8
		for i := range msg {
			msg[i] = uint8(rng.Intn(2))
		}
		cases = append(cases, msg)
	}

	for idx, msg := range cases {
		ref := referenceComputeCRC14(msg[:])
		got := computeCRC14(msg[:])
		if ref != got {
			t.Fatalf("case %d: mismatch\n  msg: %v\n  ref: %v\n  got: %v", idx, msg[:30], ref, got)
		}
	}
}

// BenchmarkComputeCRC14 measures the per-call cost of the production
// computeCRC14 implementation. Useful for confirming the integer port
// landed the expected order-of-magnitude speedup vs the byte-array
// reference.
func BenchmarkComputeCRC14(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	var msg [LDPCPayloadBits]uint8
	for i := range msg {
		msg[i] = uint8(rng.Intn(2))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = computeCRC14(msg[:])
	}
}

// BenchmarkReferenceComputeCRC14 benches the byte-array reference for
// the speedup-ratio comparison.
func BenchmarkReferenceComputeCRC14(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	var msg [LDPCPayloadBits]uint8
	for i := range msg {
		msg[i] = uint8(rng.Intn(2))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = referenceComputeCRC14(msg[:])
	}
}
