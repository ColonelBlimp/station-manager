package codec

import (
	"math/rand/v2"
	"testing"
)

// llrsFromCodeword fabricates a soft-LLR vector for a known clean
// codeword: each LLR has magnitude `mag` and the sign matching the
// LDPC-literature convention (positive ⟹ bit=0). This is the
// noise-free baseline OSD's order-0 path should always recover.
func llrsFromCodeword(codeword []byte, mag float64) []float64 {
	llrs := make([]float64, len(codeword))
	for i, b := range codeword {
		if b == 0 {
			llrs[i] = mag
		} else {
			llrs[i] = -mag
		}
	}
	return llrs
}

// flipBits inverts the requested codeword positions in-place on a
// scratch copy. Used to simulate channel errors in OSD tests.
func flipBits(codeword []byte, positions ...int) []byte {
	out := make([]byte, len(codeword))
	copy(out, codeword)
	for _, p := range positions {
		out[p] ^= 1
	}
	return out
}

// makeCleanCodeword encodes a deterministic test message + CRC and
// returns the 174-bit codeword. The all-zero message gives the
// all-zero codeword for valid LDPC; using a non-trivial message
// exercises the encoder's parity computation.
func makeCleanCodeword(t *testing.T, msg []byte) []byte {
	t.Helper()
	info := make([]byte, InfoBits)
	copy(info[:MessageBits], msg)
	crc := CRC14(msg)
	for i := 0; i < CRCBits; i++ {
		info[MessageBits+i] = byte((crc >> (CRCBits - 1 - i)) & 1)
	}
	return LDPCEncode(info)
}

// TestOSDDecode_CleanSignalOrder0 pins the noise-free baseline: a
// codeword with sign-perfect LLRs decodes via OSD order-0 (no bit
// flips needed). This is the structural correctness check — if
// this fails, the MRB setup or re-encode is broken.
func TestOSDDecode_CleanSignalOrder0(t *testing.T) {
	cases := []struct {
		name string
		msg  []byte
	}{
		{"all_zero", make([]byte, MessageBits)},
		{"random_seed_1", randomBits(1, 2, MessageBits)},
		{"random_seed_42", randomBits(42, 43, MessageBits)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codeword := makeCleanCodeword(t, tc.msg)
			llrs := llrsFromCodeword(codeword, 10.0)

			recovered, ok := OSDDecode(llrs, 0)
			if !ok {
				t.Fatal("OSDDecode(clean, order=0) returned ok=false")
			}
			for i := 0; i < MessageBits; i++ {
				if recovered[i] != tc.msg[i] {
					t.Errorf("msg bit %d = %d, want %d", i, recovered[i], tc.msg[i])
					break
				}
			}
		})
	}
}

// TestOSDDecode_SingleBitErrorOrder1 verifies that flipping a
// single bit in the codeword can be corrected by OSD order-1.
// Specifically the case BP usually handles, but OSD must too as
// a sanity check.
func TestOSDDecode_SingleBitErrorOrder1(t *testing.T) {
	msg := randomBits(7, 8, MessageBits)
	codeword := makeCleanCodeword(t, msg)

	// Flip one of the data bits (position 5 — arbitrary).
	corrupted := flipBits(codeword, 5)
	llrs := llrsFromCodeword(corrupted, 10.0)

	// Reduce the flipped bit's reliability so OSD's MRB selection
	// places it among less-reliable positions (where order-1 will
	// flip it back).
	llrs[5] *= 0.1

	recovered, ok := OSDDecode(llrs, 1)
	if !ok {
		t.Fatal("OSDDecode(1-bit-error, order=1) returned ok=false")
	}
	for i := 0; i < MessageBits; i++ {
		if recovered[i] != msg[i] {
			t.Errorf("msg bit %d = %d, want %d", i, recovered[i], msg[i])
			break
		}
	}
}

// TestOSDDecode_ManyWeakBitsOrder1 simulates a noisier channel:
// most bits are clear but a handful of low-reliability positions
// have wrong signs. Order-1 should still recover.
func TestOSDDecode_ManyWeakBitsOrder1(t *testing.T) {
	msg := randomBits(11, 12, MessageBits)
	codeword := makeCleanCodeword(t, msg)

	// Pick one weak bit randomly and flip it. With 1 bit error among
	// random reliability levels, OSD order-1 should find it.
	r := rand.New(rand.NewPCG(99, 100))
	flipPos := int(r.UintN(CodewordBits))
	corrupted := flipBits(codeword, flipPos)

	llrs := make([]float64, CodewordBits)
	for i, b := range corrupted {
		baseMag := 5.0 + 5.0*r.Float64() // 5..10 magnitude
		if b == 0 {
			llrs[i] = baseMag
		} else {
			llrs[i] = -baseMag
		}
	}
	// Make the flipped bit low-reliability so it shows up in the
	// order-1 flip search.
	llrs[flipPos] *= 0.1

	recovered, ok := OSDDecode(llrs, 1)
	if !ok {
		t.Fatal("OSDDecode(noisy single-flip, order=1) returned ok=false")
	}
	for i := 0; i < MessageBits; i++ {
		if recovered[i] != msg[i] {
			t.Errorf("msg bit %d = %d, want %d", i, recovered[i], msg[i])
			break
		}
	}
}

// TestOSDDecode_PureNoiseRejected pins that random LLRs don't
// produce a spurious CRC-passing candidate. The 14-bit CRC means
// false-positive probability is roughly 1 / 16384; running a few
// random vectors should reliably return false.
func TestOSDDecode_PureNoiseRejected(t *testing.T) {
	r := rand.New(rand.NewPCG(123, 456))
	for trial := 0; trial < 20; trial++ {
		llrs := make([]float64, CodewordBits)
		for i := range llrs {
			llrs[i] = r.NormFloat64() // ~N(0,1), no signal
		}
		if _, ok := OSDDecode(llrs, 1); ok {
			t.Errorf("trial %d: OSDDecode on pure noise returned ok=true (false positive)", trial)
		}
	}
}

// TestOSDDecode_RejectsBadInputLength pins the input contract.
func TestOSDDecode_RejectsBadInputLength(t *testing.T) {
	for _, n := range []int{0, 1, CodewordBits - 1, CodewordBits + 1, 200} {
		if _, ok := OSDDecode(make([]float64, n), 1); ok {
			t.Errorf("OSDDecode(len=%d) returned ok=true; want false", n)
		}
	}
}

// randomBits generates a deterministic bit slice of the requested
// length using PCG seeded with two integers.
func randomBits(seed1, seed2 uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed1, seed2))
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(r.UintN(2))
	}
	return out
}

// BenchmarkOSDDecode_Order0 measures the cheapest OSD path (MRB
// re-encode + CRC, no flip search).
func BenchmarkOSDDecode_Order0(b *testing.B) {
	msg := randomBits(1, 2, MessageBits)
	codeword := makeCleanCodeword(&testing.T{}, msg)
	llrs := llrsFromCodeword(codeword, 10.0)
	b.ResetTimer()
	for range b.N {
		_, _ = OSDDecode(llrs, 0)
	}
}

// BenchmarkOSDDecode_Order1 measures the standard OSD path (~91
// trial flips). The hot loop in Decode runs this on every
// candidate that BP fails to decode.
func BenchmarkOSDDecode_Order1(b *testing.B) {
	msg := randomBits(1, 2, MessageBits)
	codeword := makeCleanCodeword(&testing.T{}, msg)
	corrupted := flipBits(codeword, 50)
	llrs := llrsFromCodeword(corrupted, 10.0)
	llrs[50] *= 0.1
	b.ResetTimer()
	for range b.N {
		_, _ = OSDDecode(llrs, 1)
	}
}
