// symbols_test.go — tests for the 8-FSK symbol mapping utilities.

package dsp

import (
	"encoding/hex"
	"testing"
)

// --- Gray code tests ---

// TestGrayEncodeMatchesWSJTX verifies that the grayEncode lookup table
// matches the authoritative FT8 mapping from WSJT-X (genft8.f90, ft8b.f90)
// and ft8_lib (kFT8_Gray_map).
//
// Note: FT8 does NOT use the standard binary-reflected Gray code (n ^ n>>1).
// The mapping differs for values 4–7. This is by design in the FT8 protocol.
func TestGrayEncodeMatchesWSJTX(t *testing.T) {
	// Reference: WSJT-X genft8.f90: data graymap/0,1,3,2,5,6,4,7/
	wsjt := [NumTones]uint8{0, 1, 3, 2, 5, 6, 4, 7}
	for n := range uint8(NumTones) {
		if grayEncode[n] != wsjt[n] {
			t.Errorf("grayEncode[%d] = %d, want %d (WSJT-X graymap)", n, grayEncode[n], wsjt[n])
		}
	}
}

// TestGrayDecodeInverse verifies that grayDecode is the exact inverse of
// grayEncode: decode(encode(n)) == n for all 3-bit values.
func TestGrayDecodeInverse(t *testing.T) {
	for n := range uint8(NumTones) {
		encoded := grayEncode[n]
		decoded := grayDecode[encoded]
		if decoded != n {
			t.Errorf("grayDecode[grayEncode[%d]] = grayDecode[%d] = %d, want %d",
				n, encoded, decoded, n)
		}
	}
}

// TestGrayEncodeBijective verifies that grayEncode is a bijection: every
// output value 0–7 appears exactly once.
func TestGrayEncodeBijective(t *testing.T) {
	var seen [NumTones]bool
	for _, g := range grayEncode {
		if g >= NumTones {
			t.Fatalf("grayEncode contains out-of-range value %d", g)
		}
		if seen[g] {
			t.Errorf("grayEncode has duplicate output %d", g)
		}
		seen[g] = true
	}
}

// TestGrayAdjacentDifferByOneBit verifies the defining property of Gray
// code: consecutive values differ in exactly one-bit position.
func TestGrayAdjacentDifferByOneBit(t *testing.T) {
	for n := range uint8(NumTones - 1) {
		diff := grayEncode[n] ^ grayEncode[n+1]
		// diff should be a power of two (exactly one bit set).
		if diff == 0 || diff&(diff-1) != 0 {
			t.Errorf("grayEncode[%d]=%d and grayEncode[%d]=%d differ in %d bits, want 1",
				n, grayEncode[n], n+1, grayEncode[n+1], popcount3(diff))
		}
	}
}

// popcount3 returns the number of set bits in a 3-bit value.
func popcount3(v uint8) int {
	n := 0
	for v != 0 {
		n++
		v &= v - 1
	}
	return n
}

// --- BitsToSymbols tests ---

// TestBitsToSymbolsAllZeros verifies that an all-zero codeword produces
// all-zero symbols (Gray(0) = 0).
func TestBitsToSymbolsAllZeros(t *testing.T) {
	var cw [CodewordBytes]byte
	syms := BitsToSymbols(cw)
	for i, s := range syms {
		if s != 0 {
			t.Errorf("sym[%d] = %d, want 0", i, s)
		}
	}
}

// TestBitsToSymbolsRange verifies that all output symbol values are in [0, 7].
func TestBitsToSymbolsRange(t *testing.T) {
	// Use a codeword with all bits set to exercise the maximum value path.
	var cw [CodewordBytes]byte
	for i := range cw {
		cw[i] = 0xFF
	}

	syms := BitsToSymbols(cw)
	for i, s := range syms {
		if s >= NumTones {
			t.Errorf("sym[%d] = %d, want < %d", i, s, NumTones)
		}
	}
}

// TestBitsToSymbolsAllOnes verifies that a codeword with all 174 bits set
// produces the expected all-7-binary → Gray(7)=4 symbols.
func TestBitsToSymbolsAllOnes(t *testing.T) {
	var cw [CodewordBytes]byte
	for i := range cw {
		cw[i] = 0xFF
	}

	syms := BitsToSymbols(cw)
	// Every 3-bit group is 111 = 7 → Gray(7) = 4.
	for i, s := range syms {
		if s != 4 {
			t.Errorf("sym[%d] = %d, want 4 (Gray(7))", i, s)
		}
	}
}

// TestBitsToSymbolsKnownVector verifies the first several symbols of a
// known codeword against hand-computed values.
//
// Codeword: deadbeef123456789abcdee9f4dfae4ab374d7b4c33c
// (the "deadbeef" encoder test vector)
//
// Manual computation for the first 6 symbols:
//
//	Byte 0: 0xde = 11011110
//	Byte 1: 0xad = 10101101
//	Byte 2: 0xbe = 10111110
//
//	Sym 0: bits [0,1,2]  → 1,1,0 → binary 6 → Gray(6) = 5
//	Sym 1: bits [3,4,5]  → 1,1,1 → binary 7 → Gray(7) = 4
//	Sym 2: bits [6,7,8]  → 1,0,1 → binary 5 → Gray(5) = 7
//	Sym 3: bits [9,10,11] → 0,1,0 → binary 2 → Gray(2) = 3
//	Sym 4: bits [12,13,14] → 1,1,0 → binary 6 → Gray(6) = 5
//	Sym 5: bits [15,16,17] → 1,1,0 → binary 6 → Gray(6) = 5
func TestBitsToSymbolsKnownVector(t *testing.T) {
	cwHex := "deadbeef123456789abcdee9f4dfae4ab374d7b4c33c"
	cwBytes, err := hex.DecodeString(cwHex)
	if err != nil {
		t.Fatalf("bad hex: %v", err)
	}
	if len(cwBytes) != CodewordBytes {
		t.Fatalf("hex length %d, want %d bytes", len(cwBytes), CodewordBytes)
	}

	var cw [CodewordBytes]byte
	copy(cw[:], cwBytes)

	syms := BitsToSymbols(cw)

	wantPrefix := []uint8{5, 4, 7, 3, 5, 5}
	for i, want := range wantPrefix {
		if syms[i] != want {
			t.Errorf("sym[%d] = %d, want %d", i, syms[i], want)
		}
	}
}

// TestBitsToSymbolsSingleBitPatterns verifies that setting each of the 3
// bit positions within a symbol independently produces the correct Gray-
// coded tone. This exercises all 8 possible 3-bit patterns in symbol 0.
func TestBitsToSymbolsSingleBitPatterns(t *testing.T) {
	tests := []struct {
		name   string
		byte0  byte  // first byte of codeword (bits 0–7)
		wantS0 uint8 // expected symbol 0 (Gray-coded)
	}{
		{"000", 0x00, grayEncode[0]}, // 000 → 0
		{"001", 0x20, grayEncode[1]}, // 001 → bit 2 set → byte0 bit 5
		{"010", 0x40, grayEncode[2]}, // 010 → bit 1 set → byte0 bit 6
		{"011", 0x60, grayEncode[3]}, // 011
		{"100", 0x80, grayEncode[4]}, // 100 → bit 0 set → byte0 bit 7
		{"101", 0xA0, grayEncode[5]}, // 101
		{"110", 0xC0, grayEncode[6]}, // 110
		{"111", 0xE0, grayEncode[7]}, // 111
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cw [CodewordBytes]byte
			cw[0] = tc.byte0
			syms := BitsToSymbols(cw)
			if syms[0] != tc.wantS0 {
				t.Errorf("sym[0] = %d, want %d", syms[0], tc.wantS0)
			}
			// All other symbols should be 0 (all remaining bits are zero).
			for i := 1; i < NumDataSyms; i++ {
				if syms[i] != 0 {
					t.Errorf("sym[%d] = %d, want 0 (only byte 0 is non-zero)", i, syms[i])
				}
			}
		})
	}
}

// --- SymbolsToBits tests ---

// TestSymbolsToBitsRoundTrip verifies that BitsToSymbols → SymbolsToBits
// recovers the original codeword for several test vectors.
func TestSymbolsToBitsRoundTrip(t *testing.T) {
	vectors := []struct {
		name  string
		cwHex string
	}{
		{"all_zeros", "00000000000000000000000000000000000000000000"},
		{"bit0_set", "80000000000000000000001414a11b18e4052fd0eae0"},
		{"deadbeef", "deadbeef123456789abcdee9f4dfae4ab374d7b4c33c"},
		{"all_ones_91bits", "ffffffffffffffffffffffe08721adcae4ae81569878"},
	}

	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			cwBytes, err := hex.DecodeString(tc.cwHex)
			if err != nil {
				t.Fatalf("bad hex: %v", err)
			}
			var cw [CodewordBytes]byte
			copy(cw[:], cwBytes)

			syms := BitsToSymbols(cw)
			recovered := SymbolsToBits(syms)

			if recovered != cw {
				t.Errorf("round-trip mismatch:\n  original  %x\n  recovered %x", cw, recovered)
			}
		})
	}
}

// TestSymbolsToBitsHighBitsMasked verifies that SymbolsToBits only uses the
// low 3 bits of each symbol value, ignoring higher bits.
func TestSymbolsToBitsHighBitsMasked(t *testing.T) {
	// Create symbols with high bits set (e.g., 0xF0 | tone).
	var clean [NumDataSyms]uint8
	var dirty [NumDataSyms]uint8
	for i := range NumDataSyms {
		tone := uint8(i % NumTones)
		clean[i] = tone
		dirty[i] = tone | 0xF8 // set bits 3–7
	}

	cwClean := SymbolsToBits(clean)
	cwDirty := SymbolsToBits(dirty)

	if cwClean != cwDirty {
		t.Errorf("high bits affected result:\n  clean %x\n  dirty %x", cwClean, cwDirty)
	}
}

// --- InsertSync tests ---

// TestInsertSyncLength verifies that InsertSync produces a [NumSymbols]uint8
// (compile-time guarantee) and does not panic for zero-valued input.
func TestInsertSyncLength(t *testing.T) {
	var data [NumDataSyms]uint8
	// The return type is [NumSymbols]uint8, so length is guaranteed at
	// compile time. We just verify the call succeeds.
	_ = InsertSync(data)
}

// TestInsertSyncCostasPositions verifies that the three Costas sync blocks
// appear at the correct positions with the correct values.
func TestInsertSyncCostasPositions(t *testing.T) {
	// Use unique data symbols (1–58 range, avoiding Costas values) so we
	// can distinguish data from sync. We add an offset to avoid 0–7 overlap.
	var data [NumDataSyms]uint8
	for i := range data {
		data[i] = uint8(10 + i) // 10–67, well outside 0–7 Costas range
	}

	seq := InsertSync(data)

	// Check all three Costas blocks.
	syncStarts := [3]int{Sync1Start, Sync2Start, Sync3Start}
	for blk, start := range syncStarts {
		for j := range SyncLen {
			pos := start + j
			if seq[pos] != CostasSync[j] {
				t.Errorf("sync block %d, position %d: got %d, want %d (CostasSync[%d])",
					blk, pos, seq[pos], CostasSync[j], j)
			}
		}
	}
}

// TestInsertSyncDataPositions verifies that data symbols appear at the
// correct non-sync positions and in the correct order.
func TestInsertSyncDataPositions(t *testing.T) {
	var data [NumDataSyms]uint8
	for i := range data {
		data[i] = uint8(10 + i)
	}

	seq := InsertSync(data)

	// First data segment: positions 7–35.
	for i := range 29 {
		pos := Sync1Start + SyncLen + i
		if seq[pos] != data[i] {
			t.Errorf("data[%d] at position %d: got %d, want %d", i, pos, seq[pos], data[i])
		}
	}

	// Second data segment: positions 43–71.
	for i := range 29 {
		pos := Sync2Start + SyncLen + i
		if seq[pos] != data[29+i] {
			t.Errorf("data[%d] at position %d: got %d, want %d", 29+i, pos, seq[pos], data[29+i])
		}
	}
}

// TestInsertSyncAllZeroData verifies that with all-zero data, only the
// Costas positions are non-zero.
func TestInsertSyncAllZeroData(t *testing.T) {
	var data [NumDataSyms]uint8
	seq := InsertSync(data)

	syncPos := make(map[int]bool)
	for _, start := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			syncPos[start+j] = true
		}
	}

	for i, v := range seq {
		if syncPos[i] {
			// Determine which Costas block this position belongs to and
			// compute the expected value.
			var want uint8
			switch {
			case i < Sync1Start+SyncLen:
				want = CostasSync[i-Sync1Start]
			case i >= Sync2Start && i < Sync2Start+SyncLen:
				want = CostasSync[i-Sync2Start]
			default:
				want = CostasSync[i-Sync3Start]
			}
			if v != want {
				t.Errorf("sync position %d: got %d, want %d", i, v, want)
			}
		} else {
			if v != 0 {
				t.Errorf("position %d: got %d, want 0 (data position with zero data)", i, v)
			}
		}
	}
}

// --- ExtractData tests ---

// TestExtractDataInverse verifies that ExtractData(InsertSync(data)) == data.
func TestExtractDataInverse(t *testing.T) {
	var data [NumDataSyms]uint8
	for i := range data {
		data[i] = uint8(i % NumTones)
	}

	seq := InsertSync(data)
	recovered := ExtractData(seq)

	if recovered != data {
		t.Error("ExtractData(InsertSync(data)) != data")
	}
}

// TestExtractDataIgnoresSync verifies that ExtractData discards the Costas
// sync symbols regardless of their values.
func TestExtractDataIgnoresSync(t *testing.T) {
	var data [NumDataSyms]uint8
	for i := range data {
		data[i] = uint8(10 + i)
	}

	seq := InsertSync(data)

	// Corrupt the sync positions with different values.
	for _, start := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			seq[start+j] = 0xFF
		}
	}

	recovered := ExtractData(seq)
	if recovered != data {
		t.Error("ExtractData was affected by sync symbol corruption")
	}
}

// --- Full chain round-trip tests ---

// TestFullChainRoundTrip verifies the complete symbol mapping chain:
// BitsToSymbols → InsertSync → ExtractData → SymbolsToBits recovers the
// original codeword. Uses the encoder test vectors for known-good codewords.
func TestFullChainRoundTrip(t *testing.T) {
	vectors := []struct {
		name  string
		cwHex string
	}{
		{"all_zeros", "00000000000000000000000000000000000000000000"},
		{"bit0_set", "80000000000000000000001414a11b18e4052fd0eae0"},
		{"multi_byte", "a53c0000000000000000000b770bc76fc101d0dea56c"},
		{"deadbeef", "deadbeef123456789abcdee9f4dfae4ab374d7b4c33c"},
		{"all_ones_91bits", "ffffffffffffffffffffffe08721adcae4ae81569878"},
		{"bit90_only", "00000000000000000000002be28c18a1d1486d73ab10"},
	}

	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			cwBytes, err := hex.DecodeString(tc.cwHex)
			if err != nil {
				t.Fatalf("bad hex: %v", err)
			}
			var cw [CodewordBytes]byte
			copy(cw[:], cwBytes)

			// Forward chain: codeword → symbols → channel sequence.
			syms := BitsToSymbols(cw)
			seq := InsertSync(syms)

			// Verify the channel sequence has 79 symbols, all in [0, 7].
			for i, v := range seq {
				if v >= NumTones {
					t.Errorf("seq[%d] = %d, out of range [0, %d)", i, v, NumTones)
				}
			}

			// Reverse chain: channel sequence → symbols → codeword.
			recoveredSyms := ExtractData(seq)
			recoveredCW := SymbolsToBits(recoveredSyms)

			if recoveredCW != cw {
				t.Errorf("full chain round-trip failed:\n  original  %x\n  recovered %x", cw, recoveredCW)
			}
		})
	}
}

// --- Constant sanity checks ---

// TestConstantConsistency verifies that the symbol-related constants are
// internally consistent.
func TestConstantConsistency(t *testing.T) {
	if NumDataSyms+NumSyncSyms != NumSymbols {
		t.Errorf("NumDataSyms(%d) + NumSyncSyms(%d) != NumSymbols(%d)",
			NumDataSyms, NumSyncSyms, NumSymbols)
	}

	if NumDataSyms*BitsPerSymbol != CodedBits {
		t.Errorf("NumDataSyms(%d) * BitsPerSymbol(%d) = %d != CodedBits(%d)",
			NumDataSyms, BitsPerSymbol, NumDataSyms*BitsPerSymbol, CodedBits)
	}

	if NumSyncSyms != 3*SyncLen {
		t.Errorf("NumSyncSyms(%d) != 3 * SyncLen(%d)", NumSyncSyms, SyncLen)
	}

	if dataSegmentLen != Sync2Start-Sync1Start-SyncLen {
		t.Errorf("dataSegmentLen(%d) != Sync2Start-Sync1Start-SyncLen(%d)",
			dataSegmentLen, Sync2Start-Sync1Start-SyncLen)
	}

	if 2*dataSegmentLen != NumDataSyms {
		t.Errorf("2*dataSegmentLen(%d) != NumDataSyms(%d)", 2*dataSegmentLen, NumDataSyms)
	}

	if CodewordBytes != (CodedBits+7)/8 {
		t.Errorf("CodewordBytes(%d) != ceil(CodedBits(%d)/8) = %d",
			CodewordBytes, CodedBits, (CodedBits+7)/8)
	}

	// Verify sync block positions don't overlap and cover exactly NumSyncSyms positions.
	syncPositions := make(map[int]bool)
	for _, start := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			pos := start + j
			if pos >= NumSymbols {
				t.Errorf("sync position %d out of range [0, %d)", pos, NumSymbols)
			}
			if syncPositions[pos] {
				t.Errorf("overlapping sync position %d", pos)
			}
			syncPositions[pos] = true
		}
	}
	if len(syncPositions) != NumSyncSyms {
		t.Errorf("total sync positions %d != NumSyncSyms(%d)", len(syncPositions), NumSyncSyms)
	}
}
