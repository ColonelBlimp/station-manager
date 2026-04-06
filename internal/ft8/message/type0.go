// type0.go — Type 0 free-text message pack/unpack (i3=0, n3=0).
//
// 77-bit layout (MSB-first):
//
//	f71(71) | n3=000(3) | i3=000(3)
//
// The 71-bit payload is a base-42 encoding of up to 13 characters from the
// FT8 free-text alphabet (0-9, A-Z, +, -, ., /, ?, space).
//
// Reference: ft8_lib src/ft8/message.c pack_type0()/unpack_type0().

package message

import (
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// --- Bit layout constants for Type 0 -----------------------------------------

const (
	type0OffF71Hi = 0  // high 7 bits of the 71-bit free-text value
	type0WidthHi  = 7  // number of high bits (71 - 64 = 7)
	type0OffF71Lo = 7  // low 64 bits of the 71-bit free-text value
	type0WidthLo  = 64 // number of low bits
)

// --- Pack Type 0 -------------------------------------------------------------

// packType0 encodes a Type 0 free-text message from the FreeText field into
// a 10-byte payload. Pack does not modify msg.
func packType0(msg *Message) ([MsgBytes]byte, error) {
	const op errors.Op = "message.packType0"

	hi, lo, err := EncodeFreeText(msg.FreeText)
	if err != nil {
		return [MsgBytes]byte{}, errors.New(op).Err(err).Msgf(
			"free text %q", msg.FreeText)
	}

	var buf [MsgBytes]byte
	PackBits(buf[:], type0OffF71Hi, type0WidthHi, uint64(hi))
	PackBits(buf[:], type0OffF71Lo, type0WidthLo, lo)
	// n3=0 and i3=0 are already zero in the buffer.

	return buf, nil
}

// --- Unpack Type 0 -----------------------------------------------------------

// unpackType0 decodes a 10-byte payload with i3=0, n3=0 into a Message with
// the free-text string populated.
func unpackType0(payload [MsgBytes]byte) (*Message, error) {
	hi := uint8(UnpackBits(payload[:], type0OffF71Hi, type0WidthHi))
	lo := UnpackBits(payload[:], type0OffF71Lo, type0WidthLo)

	text := DecodeFreeText(hi, lo)

	return &Message{
		MsgType:    TypeFreeText,
		FreeText:   text,
		FreeTextHi: hi,
		FreeTextLo: lo,
	}, nil
}
