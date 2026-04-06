// Package message
// Type 1 standard message pack/unpack (i3=1).
//
// 77-bit layout (MSB-first):
//
//	c28a(28) | p1(1) | c28b(28) | p2(1) | ir(1) | g15(15) | i3=001(3)
//
// This covers the standard QSO exchange used by ~90% of on-air FT8 traffic:
//
//	CQ W1AW FN31      (CQ with grid)
//	W1AW VK2XYZ -12   (signal report)
//	VK2XYZ W1AW R-08  (Roger + report)
//	W1AW VK2XYZ RR73  (confirmation)
//
// Reference: ft8_lib src/ft8/message.c pack_type1()/unpack_type1().
package message

import (
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// --- Bit layout constants for Type 1 -----------------------------------------

const (
	type1OffC28a = 0  // 28-bit first callsign
	type1OffP1   = 28 // 1-bit first callsign type flag
	type1OffC28b = 29 // 28-bit second callsign
	type1OffP2   = 57 // 1-bit second callsign type flag
	type1OffIR   = 58 // 1-bit Roger flag
	type1OffG15  = 59 // 15-bit grid/report
	type1OffI3   = 74 // 3-bit message type

	type1I3Value = 1 // i3 value for Type 1 messages
)

// --- Pack Type 1 -------------------------------------------------------------

// packType1 encodes a Type 1 standard message from human-readable fields into
// a 10-byte payload.
//
// The caller must set Call1, Call2, and optionally Grid on the message.
// Pack does not modify msg.
func packType1(msg *Message) ([MsgBytes]byte, error) {
	const op errors.Op = "message.packType1"

	// Encode the first callsign/token.
	n28a, err := encodeCallField(msg.Call1)
	if err != nil {
		return [MsgBytes]byte{}, errors.New(op).Err(err).Msgf(
			"first callsign %q: %s", msg.Call1, err)
	}

	// Encode the second callsign/token.
	n28b, err := encodeCallField(msg.Call2)
	if err != nil {
		return [MsgBytes]byte{}, errors.New(op).Err(err).Msgf(
			"second callsign %q: %s", msg.Call2, err)
	}

	// Encode grid/report field.
	igrid4, ir, err := EncodeGridField(msg.Grid)
	if err != nil {
		return [MsgBytes]byte{}, errors.New(op).Err(err).Msgf(
			"grid field %q: %s", msg.Grid, err)
	}

	// Pack into the 77-bit payload.
	var buf [MsgBytes]byte
	PackBits(buf[:], type1OffC28a, 28, uint64(n28a))
	PackBits(buf[:], type1OffP1, 1, 0) // standard/token callsigns → p1=0
	PackBits(buf[:], type1OffC28b, 28, uint64(n28b))
	PackBits(buf[:], type1OffP2, 1, 0) // standard/token callsigns → p2=0

	var irBit uint64
	if ir {
		irBit = 1
	}
	PackBits(buf[:], type1OffIR, 1, irBit)
	PackBits(buf[:], type1OffG15, 15, uint64(igrid4))
	PackBits(buf[:], type1OffI3, 3, type1I3Value)

	return buf, nil
}

// --- Unpack Type 1 -----------------------------------------------------------

// unpackType1 decodes a 10-byte payload with i3=1 into a Message with both
// encoded field values and human-readable strings populated.
func unpackType1(payload [MsgBytes]byte) (*Message, error) {
	const op errors.Op = "message.unpackType1"

	// Extract encoded fields.
	n28a := uint32(UnpackBits(payload[:], type1OffC28a, 28))
	p1 := UnpackBits(payload[:], type1OffP1, 1) != 0
	n28b := uint32(UnpackBits(payload[:], type1OffC28b, 28))
	p2 := UnpackBits(payload[:], type1OffP2, 1) != 0
	ir := UnpackBits(payload[:], type1OffIR, 1) != 0
	igrid4 := uint16(UnpackBits(payload[:], type1OffG15, 15))

	// Decode first callsign.
	call1, err := DecodeCallsign(n28a)
	if err != nil {
		return nil, errors.New(op).Err(err).Msgf("first callsign n28=%d: %s", n28a, err)
	}

	// Decode second callsign.
	call2, err := DecodeCallsign(n28b)
	if err != nil {
		return nil, errors.New(op).Err(err).Msgf("second callsign n28=%d: %s", n28b, err)
	}

	// Decode grid/report field.
	grid, err := DecodeGridField(igrid4, ir)
	if err != nil {
		return nil, errors.New(op).Err(err).Msgf("grid field igrid4=%d ir=%v: %s", igrid4, ir, err)
	}

	return &Message{
		MsgType: TypeStandard,
		Call1:   call1,
		Call2:   call2,
		Grid:    grid,
		N28a:    n28a,
		N28b:    n28b,
		P1:      p1,
		P2:      p2,
		IR:      ir,
		IGrid4:  igrid4,
	}, nil
}

// --- Internal helpers --------------------------------------------------------

// encodeCallField encodes a callsign or token string to a 28-bit value.
// It recognizes the special tokens (CQ, DE, QRZ, CQ nnn, CQ XXXX) as well
// as standard callsigns.
func encodeCallField(call string) (uint32, error) {
	const op errors.Op = "message.encodeCallField"

	call = trimUpper(call)
	if call == "" {
		return 0, errors.New(op).Msg("callsign field is empty")
	}

	// Special tokens.
	switch {
	case call == "CQ":
		return EncodeCQ(), nil
	case call == "DE":
		return EncodeDE(), nil
	case call == "QRZ":
		return EncodeQRZ(), nil
	case len(call) > 3 && call[:3] == "CQ ":
		suffix := call[3:]
		// CQ nnn (3-digit frequency).
		if len(suffix) == 3 && isAllDigits(suffix) {
			freq := int(suffix[0]-'0')*100 + int(suffix[1]-'0')*10 + int(suffix[2]-'0')
			return EncodeCQNum(freq)
		}
		// CQ XXXX (1–4 letter directed suffix).
		if len(suffix) >= 1 && len(suffix) <= 4 && isAllLetters(suffix) {
			return EncodeCQSuffix(suffix)
		}
	}

	// Standard callsign.
	return EncodeCallsign(call)
}

// trimUpper returns the uppercase, whitespace-trimmed version of s.
func trimUpper(s string) string {
	return strings.TrimSpace(strings.ToUpper(s))
}

// isAllDigits returns true if s is non-empty and all bytes are '0'-'9'.
func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// isAllLetters returns true if s is non-empty and all bytes are 'A'-'Z'.
func isAllLetters(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return len(s) > 0
}
