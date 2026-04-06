// encoder_test.go — tests for the LDPC(174,91) systematic encoder.

package codec

import (
	"encoding/hex"
	"encoding/json"
	"math/bits"
	"os"
	"testing"
)

// --- Test helpers ---

// unpackCodeword expands a packed [NBytes]byte codeword into a per-bit
// [N]uint8 array for use with syndromeOK and other bit-level helpers.
func unpackCodeword(packed [NBytes]byte) [N]uint8 {
	var cw [N]uint8
	for i := range N {
		cw[i] = (packed[i/8] >> uint(7-i%8)) & 1
	}
	return cw
}

// --- Tests ---

// TestEncodeAllZeros verifies that an all-zero information vector produces
// an all-zero codeword.
func TestEncodeAllZeros(t *testing.T) {
	var info [KBytes]byte
	cw := Encode(info)
	var want [NBytes]byte
	if cw != want {
		t.Errorf("Encode(all-zeros) produced non-zero codeword: %x", cw)
	}
}

// TestEncodeInfoBitsPreserved verifies that the first K=91 bits of the
// codeword are identical to the input information bits (systematic property).
func TestEncodeInfoBitsPreserved(t *testing.T) {
	vectors := [][KBytes]byte{
		{0x80},
		{0xA5, 0x3C},
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xE0},
	}

	for i, info := range vectors {
		cw := Encode(info)
		// First 11 full bytes must match.
		for b := range 11 {
			if cw[b] != info[b] {
				t.Errorf("vector %d: cw[%d]=0x%02x, info[%d]=0x%02x", i, b, cw[b], b, info[b])
			}
		}
		// Byte 11: upper 3 bits (info bits 88–90) must match.
		if cw[11]&0xE0 != info[11]&0xE0 {
			t.Errorf("vector %d: cw[11]&0xE0=0x%02x, info[11]&0xE0=0x%02x",
				i, cw[11]&0xE0, info[11]&0xE0)
		}
	}
}

// TestEncodeParityChecksPass verifies that every encoded codeword satisfies
// all 83 parity checks (H × codeword = 0), using the Nm/NmCount tables.
func TestEncodeParityChecksPass(t *testing.T) {
	vectors := [][KBytes]byte{
		{},
		{0x80},
		{0x01},
		{0xA5, 0x3C},
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xE0},
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20},
		{0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x40},
	}

	for i, info := range vectors {
		cw := Encode(info)
		cwBits := unpackCodeword(cw)
		if !syndromeOK(&cwBits) {
			t.Errorf("vector %d: parity check failed for info %x", i, info)
		}
	}
}

// TestEncodeSingleBitParity exercises the generator matrix one row at a time.
// For each of the 91 information bit positions, set only that bit, encode,
// and verify the parity checks pass. This confirms every column of G is
// correct.
func TestEncodeSingleBitParity(t *testing.T) {
	for k := range K {
		var info [KBytes]byte
		info[k/8] = 1 << uint(7-k%8)

		cw := Encode(info)
		unpacked := unpackCodeword(cw)
		if !syndromeOK(&unpacked) {
			t.Errorf("parity check failed for single info bit %d", k)
		}
	}
}

// TestEncodeSingleBitParityPattern verifies that for each single-bit info
// vector, the parity bits match the corresponding generator matrix row.
// When only info bit k is set, parity bit p should equal G[p]'s k-th bit.
func TestEncodeSingleBitParityPattern(t *testing.T) {
	for k := range K {
		var info [KBytes]byte
		info[k/8] = 1 << uint(7-k%8)

		cw := Encode(info)

		// Extract each parity bit and compare to the k-th bit of G[p].
		for p := range M {
			parityPos := K + p
			gotBit := (cw[parityPos/8] >> uint(7-parityPos%8)) & 1
			wantBit := (G[p][k/8] >> uint(7-k%8)) & 1
			if gotBit != wantBit {
				t.Errorf("info bit %d, parity bit %d: got %d, want %d (G[%d][%d] bit %d)",
					k, p, gotBit, wantBit, p, k/8, 7-k%8)
			}
		}
	}
}

// TestEncodeUnusedBitsZero verifies that the 2 unused low-order bits of the
// 22nd byte are always zero.
func TestEncodeUnusedBitsZero(t *testing.T) {
	// 174 bits occupy bytes 0–21. Byte 21 has bits 168–173 in positions 7–2.
	// Positions 1–0 (the 2 LSBs) are unused. But NBytes=22 and 22*8=176,
	// so 176-174=2 unused bits. Actually let's be precise:
	// Bit 173 is at byte 21, bit position 7-(173%8) = 7-5 = 2.
	// So bits 1 and 0 of byte 21 are unused.
	vectors := [][KBytes]byte{
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xE0},
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0},
		{0xA5, 0x3C},
	}

	for i, info := range vectors {
		cw := Encode(info)
		if cw[NBytes-1]&0x03 != 0 {
			t.Errorf("vector %d: unused bits set in last byte: 0x%02x & 0x03 = 0x%02x",
				i, cw[NBytes-1], cw[NBytes-1]&0x03)
		}
	}
}

// TestEncodeDirtyInfoBitsMasked verifies that stale bits in info[11] beyond
// bit 90 are cleared before encoding. A dirty input must produce the same
// codeword as the equivalent clean input.
func TestEncodeDirtyInfoBitsMasked(t *testing.T) {
	clean := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}
	dirty := clean
	dirty[11] |= 0x1F // set all 5 unused low-order bits

	cwClean := Encode(clean)
	cwDirty := Encode(dirty)

	if cwClean != cwDirty {
		t.Errorf("dirty info produced different codeword:\n  clean %x\n  dirty %x", cwClean, cwDirty)
	}

	// The information portion (bits 0–90) must match exactly. Byte 11 is
	// shared between info bits (7–5) and parity bits (4–0), so only check
	// the upper 3 bits.
	if cwDirty[11]&0xE0 != clean[11]&0xE0 {
		t.Errorf("info bits in codeword byte 11 corrupted: got 0x%02x, want upper 3 bits 0x%02x",
			cwDirty[11]&0xE0, clean[11]&0xE0)
	}
}

// TestEncodeLinearity verifies the GF(2) linearity property of the encoder:
// Encode(a XOR b) == Encode(a) XOR Encode(b).
func TestEncodeLinearity(t *testing.T) {
	a := [KBytes]byte{0xA5, 0x3C, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xE0}
	b := [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}

	cwA := Encode(a)
	cwB := Encode(b)

	// Compute a XOR b.
	var ab [KBytes]byte
	for i := range KBytes {
		ab[i] = a[i] ^ b[i]
	}
	cwAB := Encode(ab)

	// cwA XOR cwB should equal cwAB.
	var cwXOR [NBytes]byte
	for i := range NBytes {
		cwXOR[i] = cwA[i] ^ cwB[i]
	}

	if cwXOR != cwAB {
		t.Errorf("linearity violated:\n  Encode(a^b) = %x\n  Encode(a)^Encode(b) = %x", cwAB, cwXOR)
	}
}

// TestEncodeFlippedBitFailsSyndrome verifies that flipping any single bit
// in a valid codeword causes at least one parity check to fail.
func TestEncodeFlippedBitFailsSyndrome(t *testing.T) {
	info := [KBytes]byte{0xA5, 0x3C, 0x00, 0x00, 0x00, 0x00, 0xFF}
	cw := Encode(info)
	unpacked := unpackCodeword(cw)

	for i := range N {
		flipped := unpacked
		flipped[i] ^= 1
		if syndromeOK(&flipped) {
			t.Errorf("syndromeOK returned true after flipping bit %d", i)
		}
	}
}

// TestEncodeKnownParityBits verifies that the parity bits produced by Encode
// match the GF(2) dot product computed directly from the generator matrix G.
// This ensures the encoder is consistent with the matrix data without
// duplicating golden values (which would drift independently).
func TestEncodeKnownParityBits(t *testing.T) {
	vectors := []struct {
		name string
		info [KBytes]byte
	}{
		{name: "bit0_set", info: [KBytes]byte{0x80}},
		{name: "multi_bit", info: [KBytes]byte{0xA5, 0x3C}},
		{name: "dense", info: [KBytes]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xE0}},
	}

	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			cw := Encode(tc.info)

			for p := range M {
				// Compute expected parity bit directly from G.
				var acc uint8
				for b := range KBytes {
					acc ^= G[p][b] & tc.info[b]
				}
				want := uint8(bits.OnesCount8(acc) % 2)

				parityPos := K + p
				got := (cw[parityPos/8] >> uint(7-parityPos%8)) & 1
				if got != want {
					t.Errorf("parity[%d] = %d, want %d", p, got, want)
				}
			}
		})
	}
}

// TestEncodeWeightDistribution performs a basic sanity check on the Hamming
// weight of encoded codewords — they must have non-trivial weight (neither
// all-zero except for the all-zero input, nor all-one).
func TestEncodeWeightDistribution(t *testing.T) {
	vectors := [][KBytes]byte{
		{0x80},
		{0x01},
		{0xA5, 0x3C},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xE0},
	}

	for i, info := range vectors {
		cw := Encode(info)
		weight := 0
		for b := range NBytes {
			weight += bits.OnesCount8(cw[b])
		}
		// For a (174,91) code, the minimum distance is at least 10.
		// Any non-zero info should produce a codeword with weight ≥ 10.
		if weight < 10 {
			t.Errorf("vector %d: Hamming weight %d < 10 (info %x)", i, weight, info)
		}
		if weight > N {
			t.Errorf("vector %d: Hamming weight %d > N=%d", i, weight, N)
		}
	}
}

// --- Reference vector regression test ---

// encoderVector is the JSON schema for testdata/encoder_vectors.json.
type encoderVector struct {
	Name        string `json:"name"`
	InfoHex     string `json:"info_hex"`
	CodewordHex string `json:"codeword_hex"`
}

// TestEncodeReferenceVectors loads pre-computed encoder vectors from
// testdata/encoder_vectors.json and verifies Encode produces identical output.
// These vectors serve as a regression guard against accidental changes.
func TestEncodeReferenceVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/encoder_vectors.json")
	if err != nil {
		t.Skipf("skipping reference vector regression: %v (ensure testdata/encoder_vectors.json is committed)", err)
	}

	var vectors []encoderVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("failed to parse reference vectors: %v", err)
	}

	if len(vectors) == 0 {
		t.Fatal("no reference vectors found")
	}

	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			infoBytes, err := hex.DecodeString(v.InfoHex)
			if err != nil {
				t.Fatalf("bad info_hex: %v", err)
			}
			wantBytes, err := hex.DecodeString(v.CodewordHex)
			if err != nil {
				t.Fatalf("bad codeword_hex: %v", err)
			}

			if len(infoBytes) != KBytes {
				t.Fatalf("info_hex length %d, want %d", len(infoBytes), KBytes)
			}
			if len(wantBytes) != NBytes {
				t.Fatalf("codeword_hex length %d, want %d", len(wantBytes), NBytes)
			}

			var info [KBytes]byte
			copy(info[:], infoBytes)

			var want [NBytes]byte
			copy(want[:], wantBytes)

			got := Encode(info)
			if got != want {
				t.Errorf("codeword mismatch:\n  got  %x\n  want %x", got, want)
			}
		})
	}
}
