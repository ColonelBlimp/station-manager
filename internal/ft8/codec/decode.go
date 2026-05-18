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

// ErrCallsignIsToken is returned when a Type 1 message carries a
// c28 in the special-token partition [0, nTokens). Token decoding
// (CQ / DE / QRZ / "CQ <suffix>") lands in Phase 2D alongside
// ParseMessage / FormatMessage; Phase 2C's codec stops at this
// sentinel.
var ErrCallsignIsToken = errors.New("codec: c28 is a special token; token decoding not yet implemented")

// DecodeMessage parses a 77-bit FT8 message body (bit-per-byte form,
// MSB-first per the package convention) into a Message struct,
// inverting EncodeMessage.
//
// Wire-error paths return errors; the codec layer does not panic on
// caller-supplied bit data. Specifically:
//   - ErrShortBody for any input not exactly 77 bits.
//   - ErrUnknownMessageType when the i3 tag doesn't match a
//     known type whose decoder has landed (Phase 2C: only i3=1).
//   - ErrCallsignNeedsHashLookup when a c28 slot is in the hash
//     partition; the caller's hash-table layer (FT8 service) resolves it.
//   - ErrCallsignIsToken when a c28 slot is a special token;
//     token decoding lands in Phase 2D.
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
func decodeStd(bits []byte) (Message, error) {
	c28First := uint32(readBitsUint64(bits, 0, CallsignBits))
	rover1 := bits[28] == 1
	c28Second := uint32(readBitsUint64(bits, 29, CallsignBits))
	rover2 := bits[57] == 1
	ack := bits[58] == 1
	g15 := uint16(readBitsUint64(bits, 59, G15Bits))

	call1, kind1 := C28ToCallsign(c28First)
	if err := callsignKindError(kind1, "Call1"); err != nil {
		return Message{}, err
	}
	call2, kind2 := C28ToCallsign(c28Second)
	if err := callsignKindError(kind2, "Call2"); err != nil {
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

// callsignKindError translates a non-StdCall C28Kind into the
// appropriate sentinel-wrapped error, with the field name tagged.
// Returns nil for C28KindStdCall. C28KindUnknown is impossible per
// C28ToCallsign's contract; hitting it means an internal regression
// — panic per the package convention rather than absorbing as a
// wire-decode error.
func callsignKindError(kind C28Kind, field string) error {
	switch kind {
	case C28KindStdCall:
		return nil
	case C28KindToken:
		return fmt.Errorf("%w: %s", ErrCallsignIsToken, field)
	case C28KindHash22:
		return fmt.Errorf("%w: %s", ErrCallsignNeedsHashLookup, field)
	}
	panic("codec.DecodeMessage: " + field + " decoded to C28KindUnknown — C28ToCallsign contract regression")
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
