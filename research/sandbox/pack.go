package sandbox

import (
	"fmt"
	"strings"
)

// PackError reports a packing failure. Returned values that don't
// fit the FT8 Type 1 schema (unknown callsign characters, grid out
// of range, etc.) produce a PackError instead of returning a wrong
// codeword.
type PackError struct{ Msg string }

func (e *PackError) Error() string { return "sandbox: pack: " + e.Msg }

// PackStandardCallsign encodes a standard FT8 callsign into the
// 28-bit c28 form. The callsign must follow the FT8 standard shape
// (1–2 leading letters/digits, exactly one digit at slot 3 of the
// 6-character mixed-radix encoding, then 0–3 trailing letters).
//
// Algorithm (inverse of decodeStandardCall):
//
//  1. Find the unique digit in the callsign.
//  2. Pad with leading spaces so the digit lands at slot 3 of the
//     6-character encoding (1-indexed; 0-indexed slot 2).
//  3. Right-pad with spaces to 6 total characters.
//  4. Look up each character in its alphabet (a1 / a2 / a3 / a4)
//     and compute the mixed-radix integer per std_call_to_c28.f90:
//     n = i1·(36·10·27³) + i2·(10·27³) + i3·(27³) + i4·(27²) + i5·27 + i6
//  5. Add NTOKENS + MAX22 offset to land in the standard-callsign
//     range.
func PackStandardCallsign(call string) (uint32, error) {
	call = strings.TrimSpace(call)
	if call == "" {
		return 0, &PackError{"empty callsign"}
	}
	// Find the digit whose prefix (chars before it) is ≤ 2 and suffix
	// (chars after it) is ≤ 3 — these are the slot-1/2 and slot-4/5/6
	// capacities of the 6-character encoding. Callsigns with a digit
	// in the prefix (e.g., "7Q5MLV") and a second digit at slot 3 are
	// handled correctly: the second digit is the slot-3 one.
	digitPos := -1
	for i, c := range call {
		if c < '0' || c > '9' {
			continue
		}
		prefixLen := i
		suffixLen := len(call) - i - 1
		if prefixLen <= 2 && suffixLen <= 3 {
			digitPos = i
			break
		}
	}
	if digitPos < 0 {
		return 0, &PackError{"no digit fits slot 3 of c28 encoding: " + call}
	}

	leadPad := 2 - digitPos
	if leadPad < 0 {
		return 0, &PackError{"digit beyond slot 3: " + call}
	}
	padded := strings.Repeat(" ", leadPad) + call
	if len(padded) > 6 {
		return 0, &PackError{"callsign too long after alignment: " + call}
	}
	for len(padded) < 6 {
		padded += " "
	}

	// Look up characters in their slot-specific alphabets.
	i1, err := alphaIndex(callA1, padded[0])
	if err != nil {
		return 0, &PackError{"slot 1: " + err.Error()}
	}
	i2, err := alphaIndex(callA2, padded[1])
	if err != nil {
		return 0, &PackError{"slot 2: " + err.Error()}
	}
	i3, err := alphaIndex(callA3, padded[2])
	if err != nil {
		return 0, &PackError{"slot 3 (must be digit): " + err.Error()}
	}
	i4, err := alphaIndex(callA4, padded[3])
	if err != nil {
		return 0, &PackError{"slot 4: " + err.Error()}
	}
	i5, err := alphaIndex(callA4, padded[4])
	if err != nil {
		return 0, &PackError{"slot 5: " + err.Error()}
	}
	i6, err := alphaIndex(callA4, padded[5])
	if err != nil {
		return 0, &PackError{"slot 6: " + err.Error()}
	}

	n := uint32(i1)*(36*10*27*27*27) +
		uint32(i2)*(10*27*27*27) +
		uint32(i3)*(27*27*27) +
		uint32(i4)*(27*27) +
		uint32(i5)*27 +
		uint32(i6)
	return uint32(callBase) + n, nil
}

// PackCallsign28 encodes either a token (DE / QRZ / CQ) or a
// standard callsign into the c28 field. Free-text/hash forms are
// out of scope for the fixture-generator path.
func PackCallsign28(s string) (uint32, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DE":
		return 0, nil
	case "QRZ":
		return 1, nil
	case "CQ":
		return 2, nil
	}
	return PackStandardCallsign(s)
}

// PackGrid15 encodes a 4-character Maidenhead grid (e.g. "FN20")
// into the 15-bit g15 form. Range: A-R for the first two slots,
// 0-9 for the last two. Reports / Roger / 73 tokens are not handled
// here — they have specific irpt values past MAXGRID4 and the
// fixture path always uses grids.
func PackGrid15(grid string) (uint32, error) {
	grid = strings.TrimSpace(grid)
	if len(grid) != 4 {
		return 0, &PackError{"grid must be 4 characters: " + grid}
	}
	g := []byte(grid)
	if g[0] < 'A' || g[0] > 'R' || g[1] < 'A' || g[1] > 'R' {
		return 0, &PackError{"grid letters must be A-R: " + grid}
	}
	if g[2] < '0' || g[2] > '9' || g[3] < '0' || g[3] > '9' {
		return 0, &PackError{"grid digits must be 0-9: " + grid}
	}
	j1 := uint32(g[0] - 'A')
	j2 := uint32(g[1] - 'A')
	j3 := uint32(g[2] - '0')
	j4 := uint32(g[3] - '0')
	return j1*18*10*10 + j2*10*10 + j3*10 + j4, nil
}

// PackType1 builds the 77-bit FT8 Type 1 payload for a standard
// CQ-style message: <call1> <call2> <grid> with no /R or /P
// suffixes. Returns the 77-bit payload as a 0/1 array, MSB first.
//
// Layout (matches Unpack77's reader):
//
//	bits  0..27  c28 callsign 1
//	bit  28      p1 (0)
//	bits 29..56  c28 callsign 2
//	bit  57      p2 (0)
//	bit  58      r1 (0)
//	bits 59..73  g15
//	bits 74..76  i3 = 1
func PackType1(call1, call2, grid string) ([LDPCPayloadBits]uint8, error) {
	var payload [LDPCPayloadBits]uint8
	c28a, err := PackCallsign28(call1)
	if err != nil {
		return payload, fmt.Errorf("call1: %w", err)
	}
	c28b, err := PackCallsign28(call2)
	if err != nil {
		return payload, fmt.Errorf("call2: %w", err)
	}
	g15, err := PackGrid15(grid)
	if err != nil {
		return payload, fmt.Errorf("grid: %w", err)
	}
	writeBitsToPayload(payload[:], 0, 28, uint64(c28a))
	// p1 = 0 at bit 28 (already zero)
	writeBitsToPayload(payload[:], 29, 28, uint64(c28b))
	// p2 = 0 at bit 57, r1 = 0 at bit 58
	writeBitsToPayload(payload[:], 59, 15, uint64(g15))
	writeBitsToPayload(payload[:], 74, 3, 1) // i3 = 1
	return payload, nil
}

// PayloadToInfo91 produces the 91-bit info word (77 payload + 14
// CRC) ready to feed into EncodeLDPC.
func PayloadToInfo91(payload [LDPCPayloadBits]uint8) [LDPCInfoBits]uint8 {
	var info [LDPCInfoBits]uint8
	copy(info[:LDPCPayloadBits], payload[:])
	crc := computeCRC14(payload[:])
	copy(info[LDPCPayloadBits:], crc[:])
	return info
}

// alphaIndex returns the position of c within alphabet, or an error
// if c isn't in alphabet.
func alphaIndex(alphabet string, c byte) (int, error) {
	for i := 0; i < len(alphabet); i++ {
		if alphabet[i] == c {
			return i, nil
		}
	}
	return 0, fmt.Errorf("character %q not in alphabet %q", c, alphabet)
}

// writeBitsToPayload packs an integer value into the bit slice at
// offset, n bits wide, MSB first.
func writeBitsToPayload(bits []uint8, offset, n int, value uint64) {
	for i := 0; i < n; i++ {
		bits[offset+i] = uint8((value >> uint(n-1-i)) & 1)
	}
}
