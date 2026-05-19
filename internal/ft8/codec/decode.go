package codec

import (
	"errors"
	"fmt"
	"strconv"
)

// i3Width is the width of the i3 message-type tag in bits per QEX
// paper Table 1; i3Offset is the bit position of the i3 tag's MSB
// within the 77-bit body (i3 lives in the lowest 3 bits). Named so
// a future width change is a one-line edit and the layout reads as
// "i3 spans [i3Offset, MessageBits)" at the call sites.
const (
	i3Width  = 3
	i3Offset = MessageBits - i3Width
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
	case i3Std:
		return decodeStd(bits)
	default:
		return Message{}, fmt.Errorf("%w: i3=%d", ErrUnknownMessageType, i3)
	}
}

// decodeStd inverts encodeStd. Bit layout per QEX Table 1:
//
//	c28(Call1) | r1 | c28(Call2) | r1 | R1 | g15 | i3=1
//	   28        1       28        1    1    15     3
//
// Decode is bit-faithful: every legal 77-bit body produces a
// Message, including spec-violating combinations the encoder
// refuses (e.g. token c28 with the matching rover bit set — a
// remote encoder bug, malformed corpus, or post-LDPC corruption
// could plant this on the wire). The returned Message captures
// what the bits said; the semantic gate at FormatMessage /
// EncodeMessage rejects the same Message on the way back out.
// See validateType1Rover for the asymmetry rationale and
// TestDecodeMessage_TokenWithRoverIsBitFaithful for the pin.
func decodeStd(bits []byte) (Message, error) {
	c28First := uint32(readBitsUint64(bits, 0, CallsignBits))
	rover1 := bits[28] == 1
	c28Second := uint32(readBitsUint64(bits, 29, CallsignBits))
	rover2 := bits[57] == 1
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
		Type:   MessageTypeStd,
		Call1:  call1,
		Call2:  call2,
		Rover1: rover1,
		Rover2: rover2,
		AckBit: ack,
		Grid:   grid,
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
