// Package message
// Top-level FT8/FT4 77-bit message type definitions with Pack/Unpack dispatch.
//
// FT8 messages are 77 bits packed MSB-first into 10 bytes. The last 3 bits
// (positions 74–76) encode the i3 field that identifies the message type.
// For i3=0, the preceding 3 bits (positions 71–73) encode the n3 sub-type.
//
// This file defines the Type enum, the Message struct (carrying both
// human-readable and encoded field values), and the top-level Pack/Unpack
// entry points that dispatch to type-specific packers.
//
// Reference: ft8_lib src/ft8/message.c pack77()/unpack77(), WSJT-X lib/ft8/pack77.f90.
package message

import (
	"fmt"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// --- Message type enum -------------------------------------------------------

// Type identifies an FT8 message type via its i3/n3 fields.
type Type int

const (
	// TypeFreeText is a free-text message (i3=0, n3=0): 71 bits of base-42
	// encoded text + 6 zero bits (n3=000, i3=000).
	TypeFreeText Type = iota

	// TypeStandard is a Type 1 standard message (i3=1): two 28-bit callsigns,
	// two 1-bit type flags, a 1-bit Roger flag, a 15-bit grid/report, and i3=001.
	TypeStandard

	// TypeNonStandard is a Type 4 non-standard callsign message (i3=4).
	// Not yet implemented — Pack/Unpack return an "unsupported" error.
	TypeNonStandard

	// TypeContestRTTY is i3=0, n3=1 (ARRL RTTY Roundup). Unsupported.
	TypeContestRTTY

	// TypeContestFieldDay is i3=0, n3=3 (ARRL Field Day). Unsupported.
	TypeContestFieldDay

	// TypeContestTelemetry is i3=0, n3=4 (telemetry). Unsupported.
	TypeContestTelemetry
)

// String returns a human-readable name for a message type.
func (t Type) String() string {
	switch t {
	case TypeFreeText:
		return "Free Text (i3=0, n3=0)"
	case TypeStandard:
		return "Standard (i3=1)"
	case TypeNonStandard:
		return "Non-Standard (i3=4)"
	case TypeContestRTTY:
		return "Contest RTTY (i3=0, n3=1)"
	case TypeContestFieldDay:
		return "Contest Field Day (i3=0, n3=3)"
	case TypeContestTelemetry:
		return "Telemetry (i3=0, n3=4)"
	default:
		return fmt.Sprintf("Unknown(%d)", int(t))
	}
}

// --- Message struct ----------------------------------------------------------

// Message represents a decoded FT8 77-bit message.
//
// Human-readable fields are set by Unpack and consumed by String(). Encoded
// field values are set by Pack (or directly by the caller for low-level use).
//
// For TypeStandard (Type 1):
//
//	Call1, Call2  — decoded callsign strings
//	Grid         — decoded grid/report/token string (e.g. "FN31", "-12", "R-08", "RR73")
//	N28a, N28b   — 28-bit encoded callsign values
//	P1, P2       — 1-bit callsign type flags (0=standard/token, 1=hash)
//	IR           — 1-bit Roger flag
//	IGrid4       — 15-bit encoded grid/report value
//
// For TypeFreeText (Type 0):
//
//	FreeText     — decoded free-text string (up to 13 chars)
//	FreeTextHi   — high 7 bits of the 71-bit free-text encoding
//	FreeTextLo   — low 64 bits of the 71-bit free-text encoding
type Message struct {
	// MsgType identifies the message type (set by Unpack, required by Pack).
	MsgType Type

	// --- Human-readable fields (populated by Unpack, read by String) ---

	// Call1 is the first callsign or token (e.g. "CQ", "DE", "W1AW").
	Call1 string
	// Call2 is the second callsign (e.g. "VK2XYZ").
	Call2 string
	// Grid is the grid/report/token string (e.g. "FN31", "-12", "R-08", "RR73", "73").
	Grid string
	// FreeText is the decoded free-text string (up to 13 characters).
	FreeText string

	// --- Encoded field values (populated by Pack or set directly) ---

	// N28a is the 28-bit encoded first callsign field value.
	N28a uint32
	// N28b is the 28-bit encoded second callsign field value.
	N28b uint32
	// P1 is the 1-bit type flag for the first callsign (0=std/token, 1=hash).
	P1 uint8
	// P2 is the 1-bit type flag for the second callsign (0=std/token, 1=hash).
	P2 uint8
	// IR is the 1-bit Roger flag.
	IR bool
	// IGrid4 is the 15-bit encoded grid/report field value.
	IGrid4 uint16
	// FreeTextHi holds bits 64–70 of the 71-bit free-text encoding.
	FreeTextHi uint8
	// FreeTextLo holds bits 0–63 of the 71-bit free-text encoding.
	FreeTextLo uint64
}

// String returns the human-readable text representation of the message,
// matching the format used by WSJT-X and ft8_lib for display.
//
// Examples:
//
//	"CQ W1AW FN31"
//	"W1AW VK2XYZ -12"
//	"VK2XYZ W1AW R-08"
//	"W1AW VK2XYZ RR73"
//	"TNX BOB 73 GL"
func (m *Message) String() string {
	switch m.MsgType {
	case TypeStandard:
		parts := []string{m.Call1, m.Call2}
		if m.Grid != "" {
			parts = append(parts, m.Grid)
		}
		return strings.Join(parts, " ")
	case TypeFreeText:
		return m.FreeText
	default:
		return fmt.Sprintf("[%s]", m.MsgType)
	}
}

// --- Pack --------------------------------------------------------------------

// Pack encodes a Message into a 10-byte (77-bit) MSB-first payload.
//
// The caller must set MsgType and the relevant human-readable fields (Call1,
// Call2, Grid for TypeStandard; FreeText for TypeFreeText). Pack fills in
// the encoded field values (N28a, N28b, etc.) as a side effect.
//
// Returns an error if the message type is unsupported or field encoding fails.
func Pack(msg *Message) ([MsgBytes]byte, error) {
	const op errors.Op = "message.Pack"

	switch msg.MsgType {
	case TypeStandard:
		return packType1(msg)
	case TypeFreeText:
		return packType0(msg)
	default:
		return [MsgBytes]byte{}, errors.New(op).Msgf("unsupported message type: %s", msg.MsgType)
	}
}

// --- Unpack ------------------------------------------------------------------

// Unpack decodes a 10-byte (77-bit) MSB-first payload into a Message.
//
// The i3/n3 fields are read to determine the message type, then type-specific
// unpacking fills in both encoded field values and human-readable strings.
//
// Returns an error if the message type is unsupported or field decoding fails.
func Unpack(payload [MsgBytes]byte) (*Message, error) {
	const op errors.Op = "message.Unpack"

	i3 := int(UnpackBits(payload[:], 74, 3))

	switch i3 {
	case 1:
		return unpackType1(payload)
	case 0:
		n3 := int(UnpackBits(payload[:], 71, 3))
		switch n3 {
		case 0:
			return unpackType0(payload)
		default:
			return nil, errors.New(op).Msgf(
				"unsupported i3=0 sub-type n3=%d", n3)
		}
	case 4:
		return nil, errors.New(op).Msg(
			"Type 4 (non-standard callsign) messages are not yet supported")
	default:
		return nil, errors.New(op).Msgf("unsupported message type i3=%d", i3)
	}
}
