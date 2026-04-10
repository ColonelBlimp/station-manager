// type4.go — FT8 Type 4 non-standard callsign message packing and unpacking.
//
// Type 4 messages (i3=4) carry one non-standard callsign (up to 11 characters
// from a 38-symbol alphabet: space, 0-9, A-Z, /) encoded in 58 bits, plus a
// 12-bit hash of the other callsign. This handles callsigns containing '/'
// (e.g. VK/ZL4XZ, PJ4/KA1ABC) that cannot be packed into the standard 28-bit
// callsign field.
//
// Bit layout (77 bits, MSB-first):
//
//	Bits  0–11  (12)  n12   — 12-bit hash of the hashed callsign
//	Bits 12–69  (58)  n58   — base-38 encoded non-standard callsign (up to 11 chars)
//	Bit  70      (1)  iflip — 0: n12=call1, n58=call2; 1: n58=call1, n12=call2
//	Bits 71–72   (2)  nrpt  — report: 0=none, 1=RRR, 2=RR73, 3=73
//	Bit  73      (1)  icq   — 1: first field is CQ (overrides n12/iflip for call1)
//	Bits 74–76   (3)  i3    — message type = 4
//
// Reference: ft8_lib src/ft8/message.c ftx_message_decode_nonstd()/ftx_message_encode_nonstd().

package message

import (
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Bit offsets for Type 4 fields.
const (
	type4OffN12   = 0  // 12-bit hash
	type4OffN58   = 12 // 58-bit encoded callsign
	type4OffIflip = 70 // 1-bit flip flag
	type4OffNrpt  = 71 // 2-bit report
	type4OffICQ   = 73 // 1-bit CQ flag
	// i3 at bits 74–76
)

// charset38 is the 38-symbol alphabet for Type 4 non-standard callsigns:
// " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/"
//
// Index mapping (matching ft8_lib FT8_CHAR_TABLE_ALPHANUM_SPACE_SLASH):
//
//	0      = space
//	1–10   = '0'–'9'
//	11–36  = 'A'–'Z'
//	37     = '/'
const charset38 = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/"

// unpackType4 decodes a 10-byte payload with i3=4 into a Message.
//
// Because we don't maintain a callsign hash lookup table, the 12-bit hashed
// callsign is rendered as "<...>" (same as hashed callsigns in Type 1).
func unpackType4(payload [MsgBytes]byte) (*Message, error) {
	const op errors.Op = "message.unpackType4"

	// Extract fields.
	n12 := uint16(UnpackBits(payload[:], type4OffN12, 12))
	n58 := UnpackBits(payload[:], type4OffN58, 58)
	iflip := UnpackBits(payload[:], type4OffIflip, 1) != 0
	nrpt := int(UnpackBits(payload[:], type4OffNrpt, 2))
	icq := UnpackBits(payload[:], type4OffICQ, 1) != 0

	// Decode the 58-bit non-standard callsign (base-38, 11 characters).
	callDecoded := decodeCallsign58(n58)

	// The 12-bit hash cannot be reversed without a lookup table.
	callHashed := "<...>"
	_ = n12 // n12 would be used for hash table lookup if available

	// Assign call1 and call2 based on iflip.
	var call1, call2 string
	if iflip {
		call1 = callDecoded
		call2 = callHashed
	} else {
		call1 = callHashed
		call2 = callDecoded
	}

	// Decode report field.
	var grid string
	switch nrpt {
	case 1:
		grid = "RRR"
	case 2:
		grid = "RR73"
	case 3:
		grid = "73"
	default:
		grid = ""
	}

	// If icq is set, call1 is "CQ" regardless of flip/hash.
	if icq {
		call1 = "CQ"
		grid = "" // CQ messages don't carry a report
	}

	return &Message{
		MsgType: TypeNonStandard,
		Call1:   call1,
		Call2:   call2,
		Grid:    grid,
	}, nil
}

// decodeCallsign58 decodes a 58-bit value into a non-standard callsign string
// using the base-38 alphabet (space, 0-9, A-Z, /). The encoded value represents
// up to 11 characters, right-aligned with leading spaces.
//
// Reference: ft8_lib unpack58() (message.c line 1023).
func decodeCallsign58(n58 uint64) string {
	var c11 [11]byte
	for i := 10; i >= 0; i-- {
		c11[i] = charset38[n58%38]
		n58 /= 38
	}
	return strings.TrimSpace(string(c11[:]))
}
