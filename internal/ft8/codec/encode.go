package codec

import (
	stderrors "errors"
	"fmt"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// QEX Table 1 i3 tag for Type 1 (Std Msg). Three-bit value written
// into the lowest 3 bits of the 77-bit message body. The tags for
// other message types land alongside their encoders.
const i3Std = 1

// FT8 signal-report range per QEX paper §A: "numerical signal
// reports of the form ±nn in the range -30 to +99 dB". Outside this
// band, Grid4ToG15's stored value either collides with the reserved
// tokens (n=-34..-31 maps to the same g15 slot as ""/RRR/RR73/73,
// n=-35 collides with +0) or trips the 15-bit width guard.
// Bounds-checking is delegated to the message-pack layer per the
// Grid4ToG15 doc block.
const (
	reportMin = -30
	reportMax = 99
)

// ErrUnsupportedMessageType is returned by EncodeMessage for any
// MessageType the encoder cannot pack — either the zero value (not a
// valid type in the spec) or a type whose packer hasn't landed yet
// (the phased Phase 3 / Phase 4 rollout). Callers can distinguish
// the two by inspecting m.Type before the call; the sentinel form
// keeps tests sharp (errors.Is over substring match) across the
// rollout.
//
// Defined via stdlib errors.New rather than the project's
// internal/errors so the sentinel is a plain comparable value;
// callers wrap it via fmt.Errorf("%w: ...") at the use site.
var ErrUnsupportedMessageType = stderrors.New("codec: unsupported message type")

// EncodeMessage packs a Message into its 77-bit body per QEX paper
// Table 1, returning the bits in bit-per-byte form (one byte per
// bit, MSB-first). The CRC14 and LDPC parity layers are separate —
// callers chain CRC14 + LDPCEncode to get the 174-bit codeword.
//
// Returns an error if the caller-supplied fields are invalid
// (unsupported MessageType, malformed callsign, out-of-range grid /
// report). Internal-bug paths (BitBuilder overflow, Layer 1
// primitive misuse) still panic per the package's panic-for-
// programmer-error convention.
func EncodeMessage(m Message) ([]byte, error) {
	switch m.Type {
	case MessageTypeStd:
		return encodeStd(m)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedMessageType, m.Type)
	}
}

// encodeStd packs a Type 1 (Std Msg) body per QEX Table 1:
//
//	c28(Call1) | r1(Rover1) | c28(Call2) | r1(Rover2) | R1(AckBit) | g15(Grid) | i3=1
//	    28          1            28          1            1            15        3   = 77 bits
//
// Validation runs over all caller-supplied fields before any Layer 1
// primitive is invoked, so the primitives' panics indicate genuine
// internal bugs (not bad user data).
func encodeStd(m Message) ([]byte, error) {
	if err := validateStdCallsign(m.Call1, "Call1"); err != nil {
		return nil, err
	}
	if err := validateStdCallsign(m.Call2, "Call2"); err != nil {
		return nil, err
	}
	if err := validateG15Slot(m.Grid); err != nil {
		return nil, err
	}

	var b BitBuilder
	b.Append(uint64(stdCallToC28(m.Call1)), CallsignBits).
		Append(boolBit(m.Rover1), 1).
		Append(uint64(stdCallToC28(m.Call2)), CallsignBits).
		Append(boolBit(m.Rover2), 1).
		Append(boolBit(m.AckBit), 1).
		Append(uint64(Grid4ToG15(m.Grid)), G15Bits).
		Append(i3Std, 3)
	if b.Len() != MessageBits {
		// Belt-and-braces: the field widths are constants and the
		// total is fixed at compile time. A regression in any width
		// constant lands here rather than corrupting the wire.
		panic("codec.encodeStd: assembled bit count is " + strconv.Itoa(b.Len()) + ", want " + strconv.Itoa(MessageBits) + " — width constants out of sync")
	}
	return b.Bits(), nil
}

// stdCallToC28 picks between CallsignC28 and HashedCallC28 based on
// whether the call has "long-format" std-call shape per QEX paper
// §A. Only long-format calls produce c28 values in the std-call
// range [stdCallOffset, 2^28) that round-trip cleanly via
// CallsignC28 ↔ C28ToCallsign. Other shapes (3-char, 4-char, and
// 5-char-2-prefix calls) produce values in the hash range either
// directly (via HashedCallC28) or via CallsignC28's negative-index
// arithmetic — but the latter doesn't correspond to the call's
// actual 22-bit hash, so a receiver doing hash-table lookup would
// never find the call. Routing short calls through HashedCallC28
// produces the hash-range c28 the receiver expects.
//
// Long-format shapes:
//   - 5 chars: [letter][digit][letter]{3}            (e.g. G4ABC)
//   - 6 chars: [alnum]{2}[digit][letter]{3} with ≥1  (e.g. AB1CDE, 2E0XYZ)
//     letter in the 2-char prefix
//
// All other std-call-shape inputs route through HashedCallC28.
// Precondition: caller has passed validateStdCallsign so the input
// matches some std-call shape.
func stdCallToC28(call string) uint32 {
	if isLongFormatStdCallsign(call) {
		return CallsignC28(call)
	}
	return HashedCallC28(call)
}

// isLongFormatStdCallsign reports whether s is a std-call shape
// that produces a c28 value in the std-call range (rather than the
// hash range). See stdCallToC28 for the shape catalog.
func isLongFormatStdCallsign(s string) bool {
	switch len(s) {
	case 5:
		// 1-char prefix: [letter][digit][letter]{3}
		return isLetter(s[0]) && isDigit(s[1]) && allLetters(s[2:])
	case 6:
		// 2-char prefix: [alnum]{2} ≥1-letter + [digit] + [letter]{3}
		return allAlnum(s[:2]) && hasLetter(s[:2]) && isDigit(s[2]) && allLetters(s[3:])
	}
	return false
}

func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

// boolBit converts a Go bool to its 1-bit numeric form for BitBuilder.
func boolBit(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}

// validateStdCallsign rejects callsigns that CallsignC28 would
// either panic on or silently mis-encode. CallsignC28's own checks
// cover length and per-character alphabet but not the std-callsign
// shape (prefix + digit + suffix with the right character classes)
// — that's a Layer 2 routing concern, since CallsignC28 vs C58 is
// chosen by message type, not by the encoder itself.
//
// Returned errors are tagged with the field name (Call1 / Call2) so
// the caller can locate the bad input without inspecting the error
// chain.
func validateStdCallsign(call, field string) error {
	const op errors.Op = "codec.validateStdCallsign"
	if len(call) < 3 || len(call) > 6 {
		return errors.New(op).WithMsgf("%s = %q has length %d, want 3..6 chars", field, call, len(call))
	}
	for i := range len(call) {
		c := call[i]
		if !(c >= '0' && c <= '9') && !(c >= 'A' && c <= 'Z') {
			return errors.New(op).WithMsgf("%s = %q contains invalid character %q at index %d (allowed: 0-9, A-Z uppercase)", field, call, string(c), i)
		}
	}
	if !isStdCallsignShape(call) {
		return errors.New(op).WithMsgf("%s = %q does not match standard-callsign shape (prefix 1-2 alphanumeric with at least 1 letter, exactly 1 digit, suffix 1-3 letters)", field, call)
	}
	return nil
}

// isStdCallsignShape reports whether s matches the FT8 standard-
// callsign format per QEX paper §A:
//
//	[A-Z0-9]{1,2} + [0-9] + [A-Z]{1,3}
//
// with the constraint that at least one of the 1-2 prefix chars is
// a letter. Length-3..6 and per-character alphabet are pre-checks;
// this function only verifies the shape after those pass.
//
// Non-std calls (/P, /M, compound prefixes, etc.) return false here
// and are routed to CallsignC58 by Layer 2 — they do not produce a
// Type 1 message.
func isStdCallsignShape(s string) bool {
	// Try prefix length 1 (must be a letter), then length 2 (>=1 letter).
	for prefixLen := 1; prefixLen <= 2; prefixLen++ {
		if len(s) < prefixLen+2 || len(s) > prefixLen+4 {
			continue
		}
		prefix := s[:prefixLen]
		if !allAlnum(prefix) || !hasLetter(prefix) {
			continue
		}
		digit := s[prefixLen]
		if digit < '0' || digit > '9' {
			continue
		}
		suffix := s[prefixLen+1:]
		if len(suffix) < 1 || len(suffix) > 3 || !allLetters(suffix) {
			continue
		}
		return true
	}
	return false
}

func allAlnum(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

func hasLetter(s string) bool {
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			return true
		}
	}
	return false
}

func allLetters(s string) bool {
	for i := range len(s) {
		c := s[i]
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// validateG15Slot rejects Grid strings that Grid4ToG15 would either
// panic on or silently mis-encode. Mirrors Grid4ToG15's accepted
// forms (4-char grid, reserved tokens, signed report) and adds the
// FT8 protocol's signal-report range guard that Grid4ToG15
// explicitly delegates upward — without it, "-34" stores to the
// same g15 slot as "" (blank), and the wire receiver would decode
// the report as a blank message.
//
// Lenient shape for signed reports: accepts "+0", "+00", "-0", "+2",
// "+02" interchangeably — all valid forms per the shape predicate
// (sign + 1-or-2 digits). The encoder produces the same g15 value
// for these equivalent inputs (e.g. "+0", "+00", "-0" all encode to
// the +0 slot; the protocol has only one "zero report" cell). On
// the receive side, G15ToGrid4 always canonicalises to the 2-digit
// form ("+00"), so a UI that re-displays decoded values will show
// "+00" even if the operator typed "+0".
func validateG15Slot(g string) error {
	const op errors.Op = "codec.validateG15Slot"
	switch g {
	case "", "RRR", "RR73", "73":
		return nil
	}
	if isGrid4(g) {
		return nil
	}
	if isSignedReport(g) {
		n := signedReportValue(g)
		if n < reportMin || n > reportMax {
			return errors.New(op).WithMsgf("Grid = %q is a signed report %d outside the FT8 protocol range [%d, %d] — values outside this band collide with the g15 reserved-token slots", g, n, reportMin, reportMax)
		}
		return nil
	}
	return errors.New(op).WithMsgf("Grid = %q is not a 4-char Maidenhead locator, reserved token (\"\", RRR, RR73, 73), or signed report (sign + 1-or-2 digits)", g)
}

// signedReportValue parses a pre-validated signed-report string
// ("+02", "-11") into its integer value. isSignedReport must have
// returned true; otherwise the result is the strconv.Atoi error
// path (ignored — the precondition rules this out).
func signedReportValue(s string) int {
	n, _ := strconv.Atoi(s) // precondition: isSignedReport(s) == true
	return n
}
