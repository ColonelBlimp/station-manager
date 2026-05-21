package codec

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrShortBody is returned by DecodeMessage when the input bit
// buffer is the wrong length. FT8 message bodies are exactly 77
// bits per QEX paper §2; the codec layer doesn't strip CRC or
// LDPC parity, so callers chain LDPCDecode + CRC14 verification
// before reaching DecodeMessage.
var ErrShortBody = errors.New("codec: message body must be 77 bits")

// ErrUnknownMessageType is returned when the i3.n3 tag bits don't
// match any message type whose decoder has landed. The phased Phase
// 3/4 rollout means receiving a message with an unimplemented type
// (e.g. Type 0.5 Telemetry pre-Phase-4) hits this error rather than
// silently mis-decoding.
var ErrUnknownMessageType = errors.New("codec: unknown or unsupported message type")

// ErrCallsignNeedsHashLookup is returned when a Type 1 message
// carries a c28 in the 22-bit hash partition [nTokens, stdCallOffset).
// The codec layer cannot recover the original callsign on its own —
// the FT8 service layer maintains a running hash table populated by
// prior decodes and resolves the lookup. Phase 2C's codec stops at
// this sentinel.
var ErrCallsignNeedsHashLookup = errors.New("codec: c28 is a 22-bit hash; original callsign needs hash-table lookup")

// ErrTokenInGap is returned when a Type 1 message carries a c28 in
// the special-token partition [0, nTokens) but at a codepoint that
// doesn't correspond to a defined token per QEX paper Table 7 —
// either an inter-row gap (c28 = 1003 / 1030 / 1732..1759 /
// 20686..21442 / 532444..nTokens-1) or an intra-row gap inside the
// CQ-letters range whose 4-char base-27 decode produces an embedded
// or trailing space. The encoder never emits these values, so a
// gap c28 on the wire signals a corrupted message that slipped past
// LDPC + CRC14, or a remote encoder violating the spec.
var ErrTokenInGap = errors.New("codec: c28 lands in the token partition but on a gap codepoint")

// ErrInvalidGrid6 is returned by Type 5 decode when the g25 wire
// slot lands in the unmapped upper range [maxGrid6, 2^25). The
// 25-bit field has 33,554,432 codepoints but only 18,662,400 map
// to valid 6-char Maidenhead grids; the remaining 44% (~14.9M
// codepoints) are unmapped. The encoder never produces an
// unmapped value, so a g25 in that range on the wire signals
// corruption that slipped past LDPC + CRC14 or hostile input.
// Returning this error (rather than panicking — finding #4)
// keeps the decoder safe to call on untrusted wire bytes.
var ErrInvalidGrid6 = errors.New("codec: Type 5 g25 lands in the unmapped upper range — no valid 6-char Maidenhead grid")

// DecodeMessage parses a 77-bit FT8 message body (bit-per-byte form,
// MSB-first per the package convention) into a Message struct,
// inverting EncodeMessage.
//
// Wire-error paths return errors; the codec layer does not panic on
// caller-supplied bit data. Specifically:
//   - ErrShortBody for any input not exactly 77 bits.
//   - ErrUnknownMessageType when the i3 tag doesn't match a
//     known type whose decoder has landed (Phase 2D: only i3=1).
//   - ErrCallsignNeedsHashLookup when a c28 slot is in the hash
//     partition; the caller's hash-table layer (FT8 service) resolves it.
//   - ErrTokenInGap when a c28 slot is in the token partition but on
//     a gap codepoint (the wire is carrying a spec-violating value).
//
// Valid tokens (DE / QRZ / CQ / "CQ NNN" / "CQ XXXX") decode into
// Call1 / Call2 as their string form — the Message struct holds a
// token-call and a callsign-call in the same slot.
func DecodeMessage(bits []byte) (Message, error) {
	if len(bits) != MessageBits {
		return Message{}, fmt.Errorf("%w: got %d", ErrShortBody, len(bits))
	}
	// i3 is the lowest 3 bits of the 77-bit body. Read it first so
	// type dispatch happens before any per-type field decoding.
	i3 := readBitsUint64(bits, i3Offset, i3Width)
	switch i3 {
	case i3Zero:
		return decodeI3Zero(bits)
	case i3Std:
		return decodeStd(bits)
	case i3EUVHFP:
		return decodeEUVHFP(bits)
	case i3EUVHFHash:
		return decodeEUVHFHash(bits)
	case i3NonStdCall:
		return decodeNonStdCall(bits)
	default:
		return Message{}, fmt.Errorf("%w: i3=%d", ErrUnknownMessageType, i3)
	}
}

// decodeI3Zero sub-dispatches the i3=0 message-type family on the n3
// field (3 bits immediately above f71, at offset MessageBits - i3Width
// - n3FieldBits). Phase 3A handles n3=0 (Free Text); the other n3
// values (1 = DXpedition, 3/4 = Field Day variants, 5 = Telemetry)
// land in Phase 4 and currently return ErrUnknownMessageType.
func decodeI3Zero(bits []byte) (Message, error) {
	n3Offset := MessageBits - i3Width - n3FieldBits
	n3 := readBitsUint64(bits, n3Offset, n3FieldBits)
	switch n3 {
	case n3FreeText:
		return decodeFreeText(bits)
	default:
		return Message{}, fmt.Errorf("%w: i3=0 n3=%d", ErrUnknownMessageType, n3)
	}
}

// decodeFreeText inverts encodeFreeText. Bit layout per QEX Table 1:
//
//	f71(FreeText) | n3=0 | i3=0
//	     71          3      3
//
// The 71-bit f71 slot decodes via F71ToFreeText which already trims
// leading spaces (the encoder right-justifies). The recovered text
// goes straight into Message.FreeText.
func decodeFreeText(bits []byte) (Message, error) {
	// f71 occupies bits[0..70].
	text := F71ToFreeText(bits[:F71Bits])
	return Message{
		Type:     MessageTypeFreeText,
		FreeText: text,
	}, nil
}

// decodeStd inverts encodeStd. Bit layout per QEX Table 1:
//
//	c28(Call1) | r1 | c28(Call2) | r1 | R1 | g15 | i3=1
//	   28        1       28        1    1    15     3
//
// Decode is bit-faithful: every legal 77-bit body produces a
// Message, including spec-violating combinations the encoder
// refuses (e.g. token c28 with the matching suffix bit set — a
// remote encoder bug, malformed corpus, or post-LDPC corruption
// could plant this on the wire). The returned Message captures
// what the bits said; the semantic gate at FormatMessage /
// EncodeMessage rejects the same Message on the way back out.
// See validateType1Suffix for the asymmetry rationale and
// TestDecodeMessage_TokenWithSuffixIsBitFaithful for the pin.
func decodeStd(bits []byte) (Message, error) {
	c28First := uint32(readBitsUint64(bits, 0, CallsignBits))
	suffix1 := bits[28] == 1
	c28Second := uint32(readBitsUint64(bits, 29, CallsignBits))
	suffix2 := bits[57] == 1
	ack := bits[58] == 1
	g15 := uint16(readBitsUint64(bits, 59, G15Bits))

	call1, err := type1CallFromC28(c28First, "Call1")
	if err != nil {
		return Message{}, err
	}
	call2, err := type1CallFromC28(c28Second, "Call2")
	if err != nil {
		return Message{}, err
	}

	grid, kind := G15ToGrid4(g15)
	// G15KindUnknown is impossible per G15ToGrid4's contract: every
	// 15-bit value falls into Grid4 / Reserved / Report. A return
	// here would be an internal corruption (not bad wire data), so
	// panic per the package convention rather than surfacing as a
	// recoverable error.
	if kind == G15KindUnknown {
		panic("codec.DecodeMessage: g15=" + strconv.Itoa(int(g15)) + " decoded to G15KindUnknown — G15ToGrid4 contract regression")
	}

	return Message{
		Type:    MessageTypeStd,
		Call1:   call1,
		Call2:   call2,
		Suffix1: suffix1,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}, nil
}

// decodeEUVHFP inverts encodeEUVHFP. Bit layout per QEX Table 1:
//
//	c28(Call1) | p1 | c28(Call2) | p1 | R1 | g15 | i3=2
//	   28        1       28        1    1    15     3
//
// Per QEX Table 2 the c28 field has the same partitioning in Type 2
// as in Type 1: std callsign, recognised token (CQ / DE / QRZ /
// "CQ <suffix>"), or a 22-bit hash. Token-bearing Type 2 wires
// ("CQ G4ABC/P JO22" and similar) are valid and round-trip
// cleanly; the earlier carve-out that returned ErrTokenInGap for
// any token-range Type 2 c28 was spec-incorrect (finding #2).
//
// Hash-range c28 values surface as ErrCallsignNeedsHashLookup per
// the same protocol-layer hash-table contract Type 1 uses.
func decodeEUVHFP(bits []byte) (Message, error) {
	c28First := uint32(readBitsUint64(bits, 0, CallsignBits))
	suffix1 := bits[28] == 1
	c28Second := uint32(readBitsUint64(bits, 29, CallsignBits))
	suffix2 := bits[57] == 1
	ack := bits[58] == 1
	g15 := uint16(readBitsUint64(bits, 59, G15Bits))

	call1, err := type1CallFromC28(c28First, "Call1")
	if err != nil {
		return Message{}, err
	}
	call2, err := type1CallFromC28(c28Second, "Call2")
	if err != nil {
		return Message{}, err
	}

	grid, kind := G15ToGrid4(g15)
	if kind == G15KindUnknown {
		panic("codec.decodeEUVHFP: g15=" + strconv.Itoa(int(g15)) + " decoded to G15KindUnknown — G15ToGrid4 contract regression")
	}

	return Message{
		Type:    MessageTypeEUVHFP,
		Call1:   call1,
		Call2:   call2,
		Suffix1: suffix1,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}, nil
}

// hashedCallSentinel is the WSJT-X-convention placeholder used when a
// Type 4 (NonStd Call) hash slot hasn't been resolved to a string by
// Phase 4's hash table. Operators familiar with WSJT-X output see this
// for first-decoded messages from new stations, and `<W9XYZ>` (real
// callsign in angle brackets) once the table has the resolution.
const hashedCallSentinel = "<...>"

// decodeNonStdCall inverts encodeNonStdCall. Bit layout per QEX Table 1:
//
//	h12 | c58 | h1 | r2 | c1 | i3=4
//	 12    58    1    2    1    3
//
// Returns a Message with:
//   - The nonstd side (Call1 or Call2 depending on h1) fully recovered
//     via C58ToCallsign.
//   - The hashed side set to hashedCallSentinel ("<...>") and the raw
//     12-bit hash exposed as Message.Hash12. Phase 4's hash table fills
//     the string in when the lookup is available.
//   - Grid populated from r2 (one of "", "RRR", "RR73", "73").
//   - Type = MessageTypeNonStdCall.
//
// When c1=1, Call1 is "CQ" and the h12 wire bits are ignored per QEX
// Table 2; Hash12 stays zero.
func decodeNonStdCall(bits []byte) (Message, error) {
	h12 := uint16(readBitsUint64(bits, 0, h12Bits))
	c58 := readBitsUint64(bits, h12Bits, C58Bits)
	h1 := bits[h12Bits+C58Bits]
	r2 := uint8(readBitsUint64(bits, h12Bits+C58Bits+h1Bits, r2Bits))
	c1 := bits[h12Bits+C58Bits+h1Bits+r2Bits]

	nonstd := C58ToCallsign(c58)
	grid := r2ToGrid(r2)

	msg := Message{
		Type: MessageTypeNonStdCall,
		Grid: grid,
	}
	switch {
	case c1 == 1:
		// Call1 is CQ; h12 wire bits are ignored. Call2 holds the
		// nonstd side regardless of h1.
		msg.Call1 = "CQ"
		msg.Call2 = nonstd
		msg.Hash12 = 0
	case h1 == 0:
		// Hash is the first callsign; nonstd is the second.
		msg.Call1 = hashedCallSentinel
		msg.Call2 = nonstd
		msg.Hash12 = h12
	default:
		// Hash is the second callsign; nonstd is the first.
		msg.Call1 = nonstd
		msg.Call2 = hashedCallSentinel
		msg.Hash12 = h12
	}
	return msg, nil
}

// decodeEUVHFHash inverts encodeEUVHFHash. Bit layout per QEX Table 1:
//
//	h12 | h22 | R1 | r3 | s11 | g25 | i3=5
//	 12    22    1    3    11    25     3
//
// Both call slots are hashes on the wire — the codec layer has no
// way to recover the original strings on its own, so Call1 and Call2
// surface as the WSJT-X "<...>" sentinel and the raw 12-bit and
// 22-bit hash values land in Message.Hash12 / Message.Hash22. Phase
// 4's running hash table fills the strings in when a prior decode
// has seen the underlying calls and their hashes match.
//
// Grid6, Report3, Serial, and AckBit fully decode from the wire — no
// hash-table dependency.
func decodeEUVHFHash(bits []byte) (Message, error) {
	h12 := uint16(readBitsUint64(bits, 0, h12Bits))
	h22 := uint32(readBitsUint64(bits, h12Bits, HashBits22))
	off := h12Bits + HashBits22
	ack := bits[off] == 1
	off++
	r3 := uint8(readBitsUint64(bits, off, r3Bits))
	off += r3Bits
	serial := uint16(readBitsUint64(bits, off, s11Bits))
	off += s11Bits
	g25 := uint32(readBitsUint64(bits, off, G25Bits))

	grid6, ok := G25ToGrid6(g25)
	if !ok {
		return Message{}, fmt.Errorf("%w: g25=%d", ErrInvalidGrid6, g25)
	}

	return Message{
		Type:    MessageTypeEUVHFHash,
		Call1:   hashedCallSentinel,
		Call2:   hashedCallSentinel,
		Hash12:  h12,
		Hash22:  h22,
		AckBit:  ack,
		Report3: r3,
		Serial:  serial,
		Grid6:   grid6,
	}, nil
}

// type1CallFromC28 recovers the Type 1 Call1/Call2 string from a c28
// value, dispatching on C28Kind. Inverse of type1CallToC28.
//
//   - StdCall partition: returns the recovered callsign.
//   - Token partition: looks up the token text via C28ToToken; if the
//     c28 lands in a gap codepoint, returns ErrTokenInGap (the wire is
//     carrying a spec-violating value).
//   - Hash22 partition: returns ErrCallsignNeedsHashLookup so the
//     FT8 service layer can resolve via its running hash table.
//
// C28KindUnknown is impossible per C28ToCallsign's contract; hitting
// it means an internal regression — panic per the package convention.
func type1CallFromC28(c28 uint32, field string) (string, error) {
	call, kind := C28ToCallsign(c28)
	switch kind {
	case C28KindStdCall:
		return call, nil
	case C28KindToken:
		token, ok := C28ToToken(c28)
		if !ok {
			return "", fmt.Errorf("%w: %s c28=%d", ErrTokenInGap, field, c28)
		}
		return token, nil
	case C28KindHash22:
		return "", fmt.Errorf("%w: %s", ErrCallsignNeedsHashLookup, field)
	default:
		panic("codec.DecodeMessage: " + field + " decoded to unknown C28Kind — C28ToCallsign contract regression")
	}
}

// readBitsUint64 extracts a bit-field from the bit-per-byte buffer,
// MSB-first per the package convention. Inverse of BitBuilder.Append.
//
// Reads bits[offset..offset+nbits) and packs them into the low
// nbits of a uint64. Bit at offset is the most-significant; bit at
// offset+nbits-1 is the least-significant. Precondition: bits has
// at least offset+nbits elements (DecodeMessage guards the length).
//
// Each byte is masked to its low bit before being shifted in. The
// encoder side (BitBuilder) only ever writes 0 or 1, and
// DecodeMessage's only caller chain stays inside the codec — but
// the mask is a one-instruction defense against any future raw-bit
// caller that might smuggle a non-bit-valued byte through, which
// would otherwise silently shift garbage into the high bits of c28
// and produce a nonsense partition classification.
func readBitsUint64(bits []byte, offset, nbits int) uint64 {
	var v uint64
	for i := range nbits {
		v = (v << 1) | uint64(bits[offset+i]&1)
	}
	return v
}
