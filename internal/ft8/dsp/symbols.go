// symbols.go — 8-FSK symbol mapping utilities for FT8.
//
// The FT8 protocol maps 174 LDPC-coded bits to 58 data symbols (3 bits per
// symbol, Gray-coded) and interleaves them with three 7-symbol Costas sync
// blocks to produce a 79-symbol channel sequence.
//
// This file provides the mapping functions used by both the TX synthesis
// path (codec.Encode → BitsToSymbols → InsertSync → GFSK modulation) and
// the RX demodulation path (ExtractData → SymbolsToBits → codec.Decode).
//
// Gray coding: FT8 applies a 3-bit Gray code to each symbol so that
// adjacent tones differ by only one bit, reducing the bit-error rate for
// small frequency estimation errors. The mapping is:
//
//	binary  0 1 2 3 4 5 6 7
//	Gray    0 1 3 2 6 7 5 4
//
// Reference: WSJT-X genft8.f90, ft8_lib encode.c.

package dsp

// --- FT8 protocol constants ---

const (
	// NumTones is the number of FSK tones (8-FSK for FT8).
	// TODO: FT4 uses 4-FSK.
	NumTones = 8

	// NumSymbols is the total number of channel symbols per FT8 message.
	NumSymbols = 79

	// NumDataSyms is the number of data symbols carrying coded bits.
	NumDataSyms = 58

	// NumSyncSyms is the number of synchronisation symbols (3 × 7 Costas).
	NumSyncSyms = 21

	// BitsPerSymbol is the number of coded bits per data symbol.
	BitsPerSymbol = 3

	// SymbolPeriod is the FT8 symbol duration in seconds (1 / 6.25 baud).
	// TODO: FT4 uses ~83.3 ms (12.0 baud).
	SymbolPeriod = 0.160

	// ToneSpacing is the FT8 tone separation in Hz.
	// TODO: FT4 also uses 6.25 Hz spacing.
	ToneSpacing = 6.25

	// SampleRate is the standard audio sample rate for WSJT modes.
	SampleRate = 12000

	// SamplesPerSymbol is the number of audio samples in one symbol period.
	// SampleRate × SymbolPeriod = 12000 × 0.160 = 1920.
	SamplesPerSymbol = 1920

	// WindowSamples is the total number of samples in one FT8 RX window
	// (15 s × 12 000 Hz = 180 000).
	WindowSamples = 180000

	// CodedBits is the LDPC codeword length in bits.
	CodedBits = 174

	// CodewordBytes is the number of bytes needed to hold CodedBits
	// (⌈174/8⌉ = 22).
	CodewordBytes = (CodedBits + 7) / 8
)

// --- Costas synchronisation ---

// CostasSync is the 7-symbol Costas synchronisation array for FT8.
// Reference: WSJT-X, ft8_lib.
var CostasSync = [7]uint8{3, 1, 4, 0, 6, 5, 2}

// Costas sync block positions within the 79-symbol sequence.
const (
	Sync1Start = 0  // positions 0–6
	Sync2Start = 36 // positions 36–42
	Sync3Start = 72 // positions 72–78
	SyncLen    = 7
)

// --- Gray code tables ---

// grayEncode maps a 3-bit binary value (0–7) to its Gray code equivalent.
// Formula: Gray(n) = n ^ (n >> 1).
var grayEncode = [NumTones]uint8{0, 1, 3, 2, 6, 7, 5, 4}

// grayDecode maps a 3-bit Gray code value (0–7) back to binary.
// This is the inverse permutation of grayEncode.
var grayDecode = [NumTones]uint8{0, 1, 3, 2, 7, 6, 4, 5}

// --- Symbol mapping functions ---

// BitsToSymbols maps 174 coded bits (packed MSB-first into 22 bytes) to 58
// data symbols. Each consecutive group of 3 bits is converted to a tone
// index (0–7) via Gray code mapping, per the FT8 protocol specification.
//
// The codeword format matches the output of [codec.Encode]: bits 0–90 are
// information bits, bits 91–173 are parity bits, packed MSB-first. The 2
// unused trailing bits of byte 21 are ignored.
func BitsToSymbols(codeword [CodewordBytes]byte) [NumDataSyms]uint8 {
	var syms [NumDataSyms]uint8
	for i := range NumDataSyms {
		bitIdx := BitsPerSymbol * i
		// Extract 3 consecutive bits, MSB-first.
		var val uint8
		for j := range BitsPerSymbol {
			b := bitIdx + j
			bit := (codeword[b/8] >> uint(7-b%8)) & 1
			val = (val << 1) | bit
		}
		syms[i] = grayEncode[val]
	}
	return syms
}

// SymbolsToBits converts 58 data symbols back to a 174-bit codeword packed
// MSB-first into 22 bytes. Each symbol is Gray-decoded to recover the 3-bit
// binary value. This is the inverse of [BitsToSymbols].
//
// Only the low 3 bits of each symbol value are used; higher bits are masked.
// The 2 unused trailing bits of the returned byte 21 are always zero.
func SymbolsToBits(syms [NumDataSyms]uint8) [CodewordBytes]byte {
	var cw [CodewordBytes]byte
	for i, sym := range syms {
		val := grayDecode[sym&0x07]
		bitIdx := BitsPerSymbol * i
		for j := range BitsPerSymbol {
			b := bitIdx + j
			bit := (val >> uint(2-j)) & 1
			cw[b/8] |= bit << uint(7-b%8)
		}
	}
	return cw
}

// InsertSync interleaves 58 data symbols with three 7-symbol Costas sync
// blocks to produce the full 79-symbol FT8 channel sequence.
//
// Output layout:
//
//	[0..6]   Costas sync block 1
//	[7..35]  data symbols 0–28
//	[36..42] Costas sync block 2
//	[43..71] data symbols 29–57
//	[72..78] Costas sync block 3
func InsertSync(data [NumDataSyms]uint8) [NumSymbols]uint8 {
	var out [NumSymbols]uint8

	// First Costas block (positions 0–6).
	copy(out[Sync1Start:], CostasSync[:])

	// First data segment: 29 symbols (positions 7–35).
	copy(out[Sync1Start+SyncLen:], data[:29])

	// Second Costas block (positions 36–42).
	copy(out[Sync2Start:], CostasSync[:])

	// Second data segment: 29 symbols (positions 43–71).
	copy(out[Sync2Start+SyncLen:], data[29:])

	// Third Costas block (positions 72–78).
	copy(out[Sync3Start:], CostasSync[:])

	return out
}

// ExtractData extracts the 58 data symbols from a full 79-symbol FT8
// channel sequence, discarding the three Costas sync blocks. This is the
// inverse of [InsertSync].
func ExtractData(seq [NumSymbols]uint8) [NumDataSyms]uint8 {
	var data [NumDataSyms]uint8

	// First data segment: positions 7–35 (29 symbols).
	copy(data[:29], seq[Sync1Start+SyncLen:Sync2Start])

	// Second data segment: positions 43–71 (29 symbols).
	copy(data[29:], seq[Sync2Start+SyncLen:Sync3Start])

	return data
}
