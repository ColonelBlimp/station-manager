// validate.go — post-decode plausibility checks for FT8 messages.
//
// These checks provide an additional layer of false-decode rejection beyond
// CRC-14, (i3,n3) type validation, and Unpack success. A message that
// passes CRC and unpacks successfully may still contain structurally
// implausible callsigns that indicate a noise-derived false decode.
//
// [PlausibleCallsign] validates individual callsign strings against basic
// amateur radio structural rules. [PlausibleMessage] validates an entire
// decoded 77-bit message by unpacking and checking all callsign fields.

package message

import "strings"

// PlausibleCallsign checks whether a decoded callsign string passes basic
// structural plausibility rules for an amateur radio callsign or token.
//
// This catches noise-derived false decodes that produce syntactically valid
// but structurally implausible callsign strings. It is intentionally lenient
// to avoid rejecting unusual but valid callsigns (e.g., 3DA0XYZ, 3X1ABC).
//
// Rules:
//   - Tokens (CQ, DE, QRZ, CQ nnn, CQ XXXX) are always plausible.
//   - Hash references (<...>) are always plausible (can't verify).
//   - Empty/whitespace-only strings are implausible.
//   - Standard callsigns must contain at least one letter AND at least one
//     digit (ITU requirement: all amateur callsigns have both).
//   - All-digit or all-letter "callsigns" are implausible.
func PlausibleCallsign(call string) bool {
	call = strings.TrimSpace(call)
	if call == "" {
		return false
	}

	// Tokens and hash references are always plausible.
	if call == "CQ" || call == "DE" || call == "QRZ" {
		return true
	}
	if strings.HasPrefix(call, "CQ ") {
		return true
	}
	if call == "<...>" || (strings.HasPrefix(call, "<") && strings.HasSuffix(call, ">")) {
		return true
	}

	// Standard callsign: must contain at least one letter and one digit.
	hasLetter := false
	hasDigit := false
	for i := 0; i < len(call); i++ {
		c := call[i]
		if c >= 'A' && c <= 'Z' {
			hasLetter = true
		}
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
	}

	return hasLetter && hasDigit
}

// PlausibleMessage checks whether a decoded 77-bit message contains
// plausible callsigns. Returns true if the message passes all plausibility
// checks, or if the message type doesn't contain checkable callsigns.
//
// This unpacks the message and validates each callsign field using
// [PlausibleCallsign]. It's designed to be called after CRC-14 verification
// and [ValidateMsg77] — those catch structural invalidity; this catches
// structurally valid but implausible content.
//
// Messages that fail to unpack (unsupported types) are considered plausible
// by default — we can't validate what we can't decode.
func PlausibleMessage(msg77 [MsgBytes]byte) bool {
	msg, err := Unpack(msg77)
	if err != nil {
		// Can't unpack — assume plausible (unsupported type).
		return true
	}

	switch msg.MsgType {
	case TypeStandard:
		if !PlausibleCallsign(msg.Call1) {
			return false
		}
		if !PlausibleCallsign(msg.Call2) {
			return false
		}
		return true

	case TypeNonStandard:
		// Type 4 messages have one decoded callsign and one hash.
		if msg.Call1 != "" && msg.Call1 != "<...>" && !PlausibleCallsign(msg.Call1) {
			return false
		}
		if msg.Call2 != "" && msg.Call2 != "<...>" && !PlausibleCallsign(msg.Call2) {
			return false
		}
		return true

	case TypeFreeText:
		// Free text messages don't have callsign fields to validate.
		return true

	default:
		// Unknown/unsupported type — assume plausible.
		return true
	}
}
