// codec_test.go — tests for the high-level EncodeMessage / DecodeMessage
// convenience functions.

package codec

import (
	"math/rand"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// --- EncodeMessage tests ---

// TestEncodeMessageAllZeros verifies that an all-zero 77-bit message produces
// a valid codeword whose information bits match Append91 output.
func TestEncodeMessageAllZeros(t *testing.T) {
	var msg77 [10]byte
	cw := EncodeMessage(msg77)

	// The info portion (first 91 bits) must equal message.Append91.
	info := message.Append91(msg77[:])
	for i := range KBytes {
		if cw[i] != info[i] {
			t.Errorf("cw[%d]=0x%02x != info[%d]=0x%02x", i, cw[i], i, info[i])
		}
	}

	// The codeword must pass all parity checks.
	unpacked := unpackCodeword(cw)
	if !syndromeOK(&unpacked) {
		t.Error("parity check failed for all-zero message")
	}
}

// TestEncodeMessageEquivalence verifies that EncodeMessage(msg77) produces
// the same codeword as Append91 → Encode.
func TestEncodeMessageEquivalence(t *testing.T) {
	vectors := [][10]byte{
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xF8},
		{0x42, 0x73, 0x1A, 0xF0, 0x00, 0x55, 0xAA, 0x0F, 0xE1, 0xC0},
	}

	for i, msg77 := range vectors {
		got := EncodeMessage(msg77)

		info := message.Append91(msg77[:])
		want := Encode(info)

		if got != want {
			t.Errorf("vector %d: EncodeMessage != Append91+Encode:\n  got  %x\n  want %x", i, got, want)
		}
	}
}

// TestEncodeMessageParityChecks verifies that every EncodeMessage output
// passes all 83 LDPC parity checks.
func TestEncodeMessageParityChecks(t *testing.T) {
	vectors := [][10]byte{
		{},
		{0x80},
		{0xA5, 0x3C, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00},
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8},
	}

	for i, msg77 := range vectors {
		cw := EncodeMessage(msg77)
		unpacked := unpackCodeword(cw)
		if !syndromeOK(&unpacked) {
			t.Errorf("vector %d: parity check failed", i)
		}
	}
}

// --- DecodeMessage tests ---

// TestDecodeMessagePerfectRoundTrip verifies the full encode→decode round trip
// with perfect (noiseless) LLRs.
func TestDecodeMessagePerfectRoundTrip(t *testing.T) {
	vectors := [][10]byte{
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8},
		{0x42, 0x73, 0x1A, 0xF0, 0x00, 0x55, 0xAA, 0x0F, 0xE1, 0xC0},
		{0x80},
	}

	for i, msg77 := range vectors {
		cw := EncodeMessage(msg77)
		unpacked := unpackCodeword(cw)
		llr := bitsToLLR(unpacked, 6.0)

		decoded, ok := DecodeMessage(llr, 50)
		if !ok {
			t.Errorf("vector %d: DecodeMessage returned ok=false", i)
			continue
		}

		// Compare only the first 77 bits (bytes 0–8 fully, byte 9 upper 5 bits).
		want := msg77
		want[9] &= 0xF8
		got := decoded
		got[9] &= 0xF8
		if got != want {
			t.Errorf("vector %d: decoded message mismatch:\n  got  %x\n  want %x", i, got, want)
		}
	}
}

// TestDecodeMessageNoisyRoundTrip verifies the full round trip with moderate
// Gaussian noise.
func TestDecodeMessageNoisyRoundTrip(t *testing.T) {
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	cw := EncodeMessage(msg77)
	unpacked := unpackCodeword(cw)

	rng := rand.New(rand.NewSource(42))
	llr := bitsToLLR(unpacked, 4.0)
	addGaussianNoise(&llr, 1.0, rng)

	decoded, ok := DecodeMessage(llr, 50)
	if !ok {
		t.Fatal("DecodeMessage returned ok=false for moderate noise")
	}

	want := msg77
	want[9] &= 0xF8
	got := decoded
	got[9] &= 0xF8
	if got != want {
		t.Errorf("decoded message mismatch:\n  got  %x\n  want %x", got, want)
	}
}

// TestDecodeMessageCRCFailure verifies that DecodeMessage returns ok=false
// when the LDPC decode converges to a valid codeword but the CRC does not
// match. We simulate this by flipping a single info bit in the LLR input
// that still allows the decoder to converge (to wrong info bits).
func TestDecodeMessageCRCFailure(t *testing.T) {
	// Encode a known message.
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	cw := EncodeMessage(msg77)

	// Create a different message and encode it — the CRC-14 embedded in
	// the info bits will differ from what msg77 would produce.
	msg77b := [10]byte{0x42, 0x73, 0x1A, 0xF0, 0x00, 0x55, 0xAA, 0x0F, 0xE1, 0xC0}
	cwB := EncodeMessage(msg77b)

	// Splice: take parity bits from cw but info bits from cwB.
	// This creates an invalid codeword that the decoder should either
	// fail to decode or decode to info bits whose CRC won't match.
	var spliced [NBytes]byte
	copy(spliced[:KBytes], cwB[:KBytes]) // info from msg77b
	for i := KBytes; i < NBytes; i++ {
		spliced[i] = cw[i] // parity from msg77
	}
	// Byte 11 is shared: upper bits are info, lower bits are parity.
	spliced[11] = (cwB[11] & 0xE0) | (cw[11] & 0x1F)

	unpacked := unpackCodeword(spliced)
	llr := bitsToLLR(unpacked, 6.0)

	_, ok := DecodeMessage(llr, 50)
	if ok {
		t.Error("DecodeMessage returned ok=true for spliced codeword with CRC mismatch")
	}
}

// TestDecodeMessageLDPCFailure verifies that DecodeMessage returns ok=false
// when the LDPC decode itself fails to converge.
func TestDecodeMessageLDPCFailure(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	var llr [N]float32
	for i := range N {
		llr[i] = float32(rng.NormFloat64() * 2.0)
	}

	_, ok := DecodeMessage(llr, 50)
	if ok {
		t.Error("DecodeMessage returned ok=true for random LLR input")
	}
}

// TestDecodeMessageClearsTrailingBits verifies that the returned msg77 has
// the 3 trailing bits of byte 9 (beyond bit 76) cleared to zero.
func TestDecodeMessageClearsTrailingBits(t *testing.T) {
	// Use a message where byte 9 has active upper bits to be interesting.
	msg77 := [10]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xF8}
	cw := EncodeMessage(msg77)
	unpacked := unpackCodeword(cw)
	llr := bitsToLLR(unpacked, 6.0)

	decoded, ok := DecodeMessage(llr, 50)
	if !ok {
		t.Fatal("DecodeMessage returned ok=false")
	}

	if decoded[9]&0x07 != 0 {
		t.Errorf("trailing bits not cleared: byte 9 = 0x%02x, want lower 3 bits zero", decoded[9])
	}
}

// TestEncodeDecodeMessageConvergesIn1Iter verifies that with perfect LLRs,
// the full round trip converges in 1 iteration.
func TestEncodeDecodeMessageConvergesIn1Iter(t *testing.T) {
	msg77 := [10]byte{0xA5, 0x3C, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00}
	cw := EncodeMessage(msg77)
	unpacked := unpackCodeword(cw)
	llr := bitsToLLR(unpacked, 6.0)

	decoded, ok := DecodeMessage(llr, 1)
	if !ok {
		t.Fatal("DecodeMessage failed to converge in 1 iteration with perfect LLR")
	}

	want := msg77
	want[9] &= 0xF8
	got := decoded
	got[9] &= 0xF8
	if got != want {
		t.Errorf("decoded message mismatch:\n  got  %x\n  want %x", got, want)
	}
}
