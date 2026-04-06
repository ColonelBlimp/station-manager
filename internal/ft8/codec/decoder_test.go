package codec

import (
	"math/rand"
	"testing"
)

// --- Test helpers ---

// encodeUnpacked encodes an information vector and returns the codeword as
// a per-bit [N]uint8 array suitable for syndromeOK and bitsToLLR.
func encodeUnpacked(info [KBytes]byte) [N]uint8 {
	packed := Encode(info)
	return unpackCodeword(packed)
}

// bitsToLLR converts hard bits to LLR values.
// bit=0 → +magnitude, bit=1 → −magnitude.
func bitsToLLR(cw [N]uint8, magnitude float32) [N]float32 {
	var llr [N]float32
	for i := range N {
		if cw[i] == 0 {
			llr[i] = magnitude
		} else {
			llr[i] = -magnitude
		}
	}
	return llr
}

// addGaussianNoise adds Gaussian noise to LLR values using a seeded PRNG.
// sigma controls the noise standard deviation.
func addGaussianNoise(llr *[N]float32, sigma float32, rng *rand.Rand) {
	for i := range N {
		llr[i] += sigma * float32(rng.NormFloat64())
	}
}

// --- Tests ---

// TestDecodePerfectLLR verifies that decoding perfect (noiseless) LLR values
// recovers the original information bits. Should converge in 1 iteration.
func TestDecodePerfectLLR(t *testing.T) {
	cases := []struct {
		name string
		info [KBytes]byte
	}{
		{
			name: "bit0_set",
			info: [KBytes]byte{0x80},
		},
		{
			name: "multi_byte",
			info: [KBytes]byte{0xA5, 0x3C, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xE0},
		},
		{
			name: "all_ones_91bits",
			info: func() [KBytes]byte {
				// Set all 91 info bits to 1.
				var info [KBytes]byte
				for i := range 11 {
					info[i] = 0xFF
				}
				info[11] = 0xE0 // bits 88,89,90 set; bits 91-95 zero
				return info
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cw := encodeUnpacked(tc.info)
			llr := bitsToLLR(cw, 6.0)

			decoded, ok := Decode(llr, 50)
			if !ok {
				t.Fatal("Decode returned ok=false for perfect LLR")
			}
			if decoded != tc.info {
				t.Errorf("decoded info mismatch:\n  got  %x\n  want %x", decoded, tc.info)
			}
		})
	}
}

// TestDecodeConvergesIn1Iteration verifies that perfect LLRs converge with
// maxIter=1, since the hard decision on noiseless input is already correct.
func TestDecodeConvergesIn1Iteration(t *testing.T) {
	info := [KBytes]byte{0xA5, 0x3C}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 6.0)

	decoded, ok := Decode(llr, 1)
	if !ok {
		t.Fatal("Decode failed to converge in 1 iteration with perfect LLR")
	}
	if decoded != info {
		t.Errorf("decoded info mismatch:\n  got  %x\n  want %x", decoded, info)
	}
}

// TestDecodeNoisyRoundTrip adds moderate Gaussian noise and verifies the
// decoder can still recover the original information bits.
func TestDecodeNoisyRoundTrip(t *testing.T) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}
	cw := encodeUnpacked(info)

	// Moderate noise: σ = 1.0 with LLR magnitude 4.0.
	// This is well within decoding capability.
	rng := rand.New(rand.NewSource(42))
	llr := bitsToLLR(cw, 4.0)
	addGaussianNoise(&llr, 1.0, rng)

	decoded, ok := Decode(llr, 50)
	if !ok {
		t.Fatal("Decode returned ok=false for moderate noise")
	}
	if decoded != info {
		t.Errorf("decoded info mismatch:\n  got  %x\n  want %x", decoded, info)
	}
}

// TestDecodeHeavyNoise runs several seeded trials at higher noise levels
// to exercise the decoder more thoroughly.
func TestDecodeHeavyNoise(t *testing.T) {
	info := [KBytes]byte{0x42, 0x73, 0x1A, 0xF0, 0x00, 0x55, 0xAA, 0x0F, 0xE1, 0xC3, 0x87, 0x00}
	cw := encodeUnpacked(info)

	// σ = 2.0 with LLR magnitude 3.0 — challenging but decodable.
	// With seeded PRNG these trials are deterministic; all 5 decode at these
	// parameters. Require ≥ 4/5 to catch performance regressions while
	// allowing for one marginal seed.
	seeds := []int64{100, 200, 300, 400, 500}
	decoded := 0
	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		llr := bitsToLLR(cw, 3.0)
		addGaussianNoise(&llr, 2.0, rng)

		result, ok := Decode(llr, 50)
		if ok && result == info {
			decoded++
		}
	}
	if decoded < 4 {
		t.Errorf("only %d/%d trials decoded successfully, want >= 4", decoded, len(seeds))
	}
	t.Logf("%d/%d trials decoded successfully", decoded, len(seeds))
}

// TestDecodeRandomLLRFails verifies that uniformly random LLR values
// (no valid codeword structure) result in ok=false.
func TestDecodeRandomLLRFails(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	var llr [N]float32
	for i := range N {
		llr[i] = float32(rng.NormFloat64() * 2.0)
	}

	_, ok := Decode(llr, 50)
	if ok {
		t.Error("Decode returned ok=true for random LLR input")
	}
}

// TestDecodeAllPositiveLLR verifies that all-positive LLRs (pointing at the
// all-zero codeword) are rejected by the all-zero prohibition guard.
func TestDecodeAllPositiveLLR(t *testing.T) {
	var llr [N]float32
	for i := range N {
		llr[i] = 6.0 // all positive → hard decision all zeros
	}

	_, ok := Decode(llr, 50)
	if ok {
		t.Error("Decode returned ok=true for all-positive LLR (all-zero codeword)")
	}
}

// TestSyndromeOK verifies the syndrome check helper directly.
func TestSyndromeOK(t *testing.T) {
	// A valid codeword should pass.
	info := [KBytes]byte{0xA5, 0x3C}
	cw := encodeUnpacked(info)
	if !syndromeOK(&cw) {
		t.Error("syndromeOK returned false for valid codeword")
	}

	// Flipping any single bit should fail.
	for i := range N {
		flipped := cw
		flipped[i] ^= 1
		if syndromeOK(&flipped) {
			t.Errorf("syndromeOK returned true after flipping bit %d", i)
		}
	}
}

// TestPackInfoBits verifies the bit-packing helper.
func TestPackInfoBits(t *testing.T) {
	// Set specific bit patterns and verify packing.
	var plain [N]uint8

	// Set bit 0 (MSB of byte 0).
	plain[0] = 1
	got := packInfoBits(&plain)
	if got[0] != 0x80 {
		t.Errorf("bit 0: got 0x%02x, want 0x80", got[0])
	}

	// Set bit 7 (LSB of byte 0).
	plain = [N]uint8{}
	plain[7] = 1
	got = packInfoBits(&plain)
	if got[0] != 0x01 {
		t.Errorf("bit 7: got 0x%02x, want 0x01", got[0])
	}

	// Set bit 90 (last info bit: byte 11, bit position 5).
	plain = [N]uint8{}
	plain[90] = 1
	got = packInfoBits(&plain)
	if got[11] != 0x20 {
		t.Errorf("bit 90: got 0x%02x, want 0x20", got[11])
	}

	// Bits beyond K=91 should NOT appear in the packed output.
	plain = [N]uint8{}
	plain[91] = 1 // parity bit — should be ignored
	got = packInfoBits(&plain)
	for i := range KBytes {
		if got[i] != 0 {
			t.Errorf("parity bit leaked into info: byte %d = 0x%02x", i, got[i])
		}
	}
}

// TestDecodeMultipleInfoVectors runs the decoder on several different
// information vectors to exercise diverse bit patterns.
func TestDecodeMultipleInfoVectors(t *testing.T) {
	vectors := [][KBytes]byte{
		{0x01},
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20}, // bit 90 only
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xE0}, // all 91 bits
		{0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x40}, // alternating
	}

	for i, info := range vectors {
		cw := encodeUnpacked(info)
		llr := bitsToLLR(cw, 6.0)

		decoded, ok := Decode(llr, 1)
		if !ok {
			t.Errorf("vector %d: Decode returned ok=false", i)
			continue
		}
		if decoded != info {
			t.Errorf("vector %d: decoded info mismatch:\n  got  %x\n  want %x", i, decoded, info)
		}
	}
}

// TestDecodeRespectsMaxIter verifies that maxIter=0 means no iterations
// are performed and the decoder returns ok=false.
func TestDecodeRespectsMaxIter(t *testing.T) {
	info := [KBytes]byte{0xA5, 0x3C}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 6.0)

	_, ok := Decode(llr, 0)
	if ok {
		t.Error("Decode returned ok=true with maxIter=0")
	}
}
