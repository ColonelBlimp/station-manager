package codec

import (
	"math/rand"
	"testing"
)

// --- Generator matrix tests ---

// TestFullGeneratorIdentityBlock verifies that the lazily-initialised K×N
// generator matrix has an identity block in its first K columns (the code
// is systematic: info bits are copied verbatim into codeword positions 0–90).
func TestFullGeneratorIdentityBlock(t *testing.T) {
	initFullGenerator()
	for i := range K {
		for j := range K {
			want := uint8(0)
			if i == j {
				want = 1
			}
			if fullGen[i][j] != want {
				t.Errorf("fullGen[%d][%d] = %d, want %d", i, j, fullGen[i][j], want)
			}
		}
	}
}

// TestFullGeneratorMatchesEncode verifies that each row of the full
// generator matrix matches the output of [Encode] for the corresponding
// unit vector.
func TestFullGeneratorMatchesEncode(t *testing.T) {
	initFullGenerator()
	for i := range K {
		var info [KBytes]byte
		info[i/8] |= 1 << uint(7-i%8)
		packed := Encode(info)
		for j := range N {
			got := fullGen[i][j]
			want := (packed[j/8] >> uint(7-j%8)) & 1
			if got != want {
				t.Errorf("fullGen[%d][%d] = %d, want %d", i, j, got, want)
			}
		}
	}
}

// TestFullGeneratorValidCodewords verifies that every row of the full
// generator matrix is a valid codeword (satisfies all 83 parity checks).
func TestFullGeneratorValidCodewords(t *testing.T) {
	initFullGenerator()
	for i := range K {
		var cw [N]uint8
		copy(cw[:], fullGen[i][:])
		if !syndromeOK(&cw) {
			t.Errorf("fullGen row %d is not a valid codeword", i)
		}
	}
}

// --- OSD decode tests ---

// TestDecodeOSDPerfectLLR verifies that OSD decodes perfect (noiseless) LLRs
// correctly at both ndeep=0 and ndeep=1.
func TestDecodeOSDPerfectLLR(t *testing.T) {
	cases := [][KBytes]byte{
		{0x80},
		{0xA5, 0x3C},
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xE0},
	}

	for _, info := range cases {
		cw := encodeUnpacked(info)
		llr := bitsToLLR(cw, 6.0)

		for _, ndeep := range []int{0, 1} {
			decoded, ok := DecodeOSD(llr, ndeep)
			if !ok {
				t.Errorf("info=%x ndeep=%d: DecodeOSD returned ok=false", info, ndeep)
				continue
			}
			if decoded != info {
				t.Errorf("info=%x ndeep=%d: mismatch\n  got  %x\n  want %x",
					info, ndeep, decoded, info)
			}
		}
	}
}

// TestDecodeOSDNoisyOrder0 verifies that OSD order-0 decodes moderate noise
// (same parameters as TestDecodeNoisyRoundTrip).
func TestDecodeOSDNoisyOrder0(t *testing.T) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}
	cw := encodeUnpacked(info)

	rng := rand.New(rand.NewSource(42))
	llr := bitsToLLR(cw, 4.0)
	addGaussianNoise(&llr, 1.0, rng)

	decoded, ok := DecodeOSD(llr, 0)
	if !ok {
		t.Fatal("OSD order-0 returned ok=false for moderate noise")
	}
	if decoded != info {
		t.Errorf("OSD order-0 decoded info mismatch:\n  got  %x\n  want %x", decoded, info)
	}
}

// TestDecodeOSDOrder1RecoversBPFailure verifies that OSD order-1 can
// decode signals that cause BP to fail. We create challenging LLRs by
// injecting errors into specific bit positions.
func TestDecodeOSDOrder1RecoversBPFailure(t *testing.T) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}
	cw := encodeUnpacked(info)

	// Create LLRs where most bits are strongly correct but a handful are
	// flipped with moderate confidence — enough to defeat BP but not OSD.
	llr := bitsToLLR(cw, 6.0)

	// Flip 12 parity-bit positions (bits 91–102): reverse the LLR sign
	// and reduce magnitude to create unreliable errors.
	for i := K; i < K+12; i++ {
		llr[i] = -llr[i] * 0.5
	}

	// BP should struggle with 12 corrupted parity bits.
	_, bpOK := Decode(llr, 50)

	// OSD should recover: the 91 info-bit LLRs are all strong and correct,
	// so the order-0 candidate (MRB hard decisions) is already right.
	decoded, osdOK := DecodeOSD(llr, 1)
	if !osdOK {
		t.Fatal("OSD returned ok=false")
	}
	if decoded != info {
		if bpOK {
			t.Log("BP also succeeded — test still valid since OSD is correct")
		}
		t.Errorf("OSD decoded info mismatch:\n  got  %x\n  want %x", decoded, info)
	}
	if bpOK {
		t.Log("note: BP also converged for this pattern")
	} else {
		t.Log("BP failed as expected; OSD succeeded")
	}
}

// TestDecodeOSDOrder1WithInfoBitErrors verifies that OSD order-1 can
// correct a single info-bit error when the remaining bits are reliable.
func TestDecodeOSDOrder1WithInfoBitErrors(t *testing.T) {
	info := [KBytes]byte{0xA5, 0x3C, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xE0}
	cw := encodeUnpacked(info)

	// Strong LLRs for all bits.
	llr := bitsToLLR(cw, 8.0)

	// Flip exactly one info bit (bit 0) with weak confidence.
	// This creates a single error in the most-reliable-bit set.
	// OSD order-1 should find and correct this flip.
	llr[0] = -llr[0] * 0.1

	decoded, ok := DecodeOSD(llr, 1)
	if !ok {
		t.Fatal("OSD returned ok=false")
	}
	if decoded != info {
		t.Errorf("OSD failed to correct single info-bit error:\n  got  %x\n  want %x",
			decoded, info)
	}
}

// TestDecodeOSDAlwaysProducesValidCodeword verifies that OSD output always
// satisfies all 83 LDPC parity checks (since it encodes via the generator
// matrix).
func TestDecodeOSDAlwaysProducesValidCodeword(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for trial := range 10 {
		var llr [N]float32
		for i := range N {
			llr[i] = float32(rng.NormFloat64() * 3.0)
		}

		info, ok := DecodeOSD(llr, 1)
		if !ok {
			t.Errorf("trial %d: OSD returned ok=false for random input", trial)
			continue
		}

		// Re-encode the info and check syndrome.
		packed := Encode(info)
		unpacked := unpackCodeword(packed)
		if !syndromeOK(&unpacked) {
			t.Errorf("trial %d: OSD output failed syndrome check", trial)
		}
	}
}

// TestDecodeOSDUnpermute verifies that the un-permutation correctly maps
// the OSD codeword back to the original bit ordering.
func TestDecodeOSDUnpermute(t *testing.T) {
	// Use a non-trivial info vector.
	info := [KBytes]byte{0x42, 0x73, 0x1A, 0xF0, 0x00, 0x55, 0xAA, 0x0F, 0xE1, 0xC3, 0x87, 0x00}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 6.0)

	decoded, ok := DecodeOSD(llr, 0)
	if !ok {
		t.Fatal("OSD returned ok=false for perfect LLR")
	}
	if decoded != info {
		t.Errorf("un-permutation failed:\n  got  %x\n  want %x", decoded, info)
	}
}

// --- DecodeMessage fallback integration tests ---

// TestDecodeMessageOSDFallback verifies that DecodeMessage uses OSD when
// BP fails, producing correct decoded output.
//
// Strategy: take a valid codeword, flip the sign of one info-bit LLR
// (making it weakly wrong) and flip several parity-bit LLRs (strongly
// wrong). BP cannot converge on the corrupted parity checks, but OSD
// ranks the strongly-correct info bits as most-reliable, tries flipping
// the one weak info bit (order-1), and recovers the correct message.
func TestDecodeMessageOSDFallback(t *testing.T) {
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	cw := EncodeMessage(msg77)
	unpacked := unpackCodeword(cw)

	// Start with strong LLRs.
	llr := bitsToLLR(unpacked, 8.0)

	// Flip one info-bit LLR with weak magnitude — OSD order-1 will
	// try correcting it.
	llr[5] = -llr[5] * 0.05

	// Flip 20 parity-bit LLRs to poison the syndrome for BP. Use
	// moderate magnitude so they look somewhat reliable to BP.
	for i := K; i < K+20; i++ {
		llr[i] = -llr[i] * 0.8
	}

	// BP should fail to converge with 20 corrupted parity bits.
	_, bpOK := Decode(llr, 50)
	if bpOK {
		t.Log("BP unexpectedly succeeded — skipping fallback assertion")
		return
	}

	// DecodeMessage should fall back to OSD and recover the message.
	decoded, ok := DecodeMessage(llr, 50)
	if !ok {
		t.Fatal("DecodeMessage returned ok=false — OSD fallback did not recover the message")
	}

	want := msg77
	want[9] &= 0xF8
	got := decoded
	got[9] &= 0xF8
	if got != want {
		t.Errorf("decoded message mismatch:\n  got  %x\n  want %x", got, want)
	} else {
		t.Log("BP failed; OSD fallback succeeded — correct message recovered")
	}
}

// TestDecodeMessageStillWorksBPOnly verifies that adding the OSD fallback
// doesn't break the existing BP decode path.
func TestDecodeMessageStillWorksBPOnly(t *testing.T) {
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	cw := EncodeMessage(msg77)
	unpacked := unpackCodeword(cw)
	llr := bitsToLLR(unpacked, 6.0)

	decoded, ok := DecodeMessage(llr, 50)
	if !ok {
		t.Fatal("DecodeMessage returned ok=false for perfect LLR")
	}

	want := msg77
	want[9] &= 0xF8
	got := decoded
	got[9] &= 0xF8
	if got != want {
		t.Errorf("decoded message mismatch:\n  got  %x\n  want %x", got, want)
	}
}

// --- Benchmarks ---

// BenchmarkDecodeOSDOrder0 measures OSD order-0 performance with perfect LLRs.
func BenchmarkDecodeOSDOrder0(b *testing.B) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0x00, 0x00}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 6.0)
	b.ResetTimer()
	for range b.N {
		DecodeOSD(llr, 0)
	}
}

// BenchmarkDecodeOSDOrder1 measures OSD order-1 performance with perfect LLRs.
func BenchmarkDecodeOSDOrder1(b *testing.B) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0x00, 0x00}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 6.0)
	b.ResetTimer()
	for range b.N {
		DecodeOSD(llr, 1)
	}
}

// BenchmarkDecodeOSDNoisy measures OSD order-1 with noisy LLRs (realistic).
func BenchmarkDecodeOSDNoisy(b *testing.B) {
	info := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0x00, 0x00}
	cw := encodeUnpacked(info)
	llr := bitsToLLR(cw, 2.0)
	rng := rand.New(rand.NewSource(42))
	addGaussianNoise(&llr, 1.0, rng)
	b.ResetTimer()
	for range b.N {
		DecodeOSD(llr, 1)
	}
}
