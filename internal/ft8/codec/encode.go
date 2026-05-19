package codec

import (
	stderrors "errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// QEX Table 1 i3 tag for Type 1 (Std Msg). Three-bit value written
// into the lowest 3 bits of the 77-bit message body. The tags for
// other message types land alongside their encoders.
const i3Std = 1

// QEX Table 1 i3.n3 tags for the i3=0 message-type family. n3
// occupies bits 71..73 (the 3 bits immediately above f71's 71-bit
// payload); i3=0 occupies bits 74..76. The family multiplexes:
//
//	n3 = 0: Free Text (Type 0.0)         — implemented
//	n3 = 1: DXpedition (Type 0.1)        — Phase 4
//	n3 = 3: Field Day Class A-F (0.3)    — Phase 4
//	n3 = 4: Field Day (0.4)              — Phase 4
//	n3 = 5: Telemetry (0.5)              — Phase 4
//	n3 = 2, 6, 7 are unassigned per QEX paper Table 1.
const (
	i3Zero      = 0
	n3FreeText  = 0
	n3FieldBits = 3
)

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
	case MessageTypeFreeText:
		return encodeFreeText(m)
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
	if err := validateType1Call(m.Call1, "Call1"); err != nil {
		return nil, err
	}
	if err := validateType1Rover(m.Call1, m.Rover1, "Call1"); err != nil {
		return nil, err
	}
	if err := validateType1Call(m.Call2, "Call2"); err != nil {
		return nil, err
	}
	if err := validateType1Rover(m.Call2, m.Rover2, "Call2"); err != nil {
		return nil, err
	}
	if err := validateG15Slot(m.Grid); err != nil {
		return nil, err
	}

	var b BitBuilder
	b.Append(uint64(type1CallToC28(m.Call1)), CallsignBits).
		Append(boolBit(m.Rover1), 1).
		Append(uint64(type1CallToC28(m.Call2)), CallsignBits).
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
	// Detach from the BitBuilder's internal storage. BitBuilder.Bits()
	// aliases its backing array (see bitbuilder.go); returning the
	// aliased slice would couple our output's mutability to any
	// future BitBuilder pooling. The clone is 77 bytes — invisible
	// next to the LDPC encode that follows on the same hot path.
	return slices.Clone(b.Bits()), nil
}

// encodeFreeText packs a Type 0.0 (Free Text) body per QEX Table 1:
//
//	f71(FreeText) | n3=0 | i3=0
//	     71          3      3   = 77 bits
//
// Validates the FreeText shape (length 1..13, all chars in the f71
// alphabet) up front so FreeTextToF71's panic on bad input never
// fires from this path. An empty FreeText is rejected — the wire's
// 13-space encoding would round-trip to the empty string per
// F71ToFreeText's leading-space trim, but the operator semantic is
// "no Free Text message to send."
func encodeFreeText(m Message) ([]byte, error) {
	if err := validateFreeText(m.FreeText); err != nil {
		return nil, err
	}
	f71Bits := FreeTextToF71(m.FreeText)
	var b BitBuilder
	b.AppendBits(f71Bits).
		Append(n3FreeText, n3FieldBits).
		Append(i3Zero, i3Width)
	if b.Len() != MessageBits {
		// Belt-and-braces — width constants out of sync would land here.
		panic("codec.encodeFreeText: assembled bit count is " + strconv.Itoa(b.Len()) + ", want " + strconv.Itoa(MessageBits) + " — width constants out of sync")
	}
	return slices.Clone(b.Bits()), nil
}

// validateFreeText enforces the FreeText slot's shape upstream of
// the FreeTextToF71 primitive so its panic path stays reserved for
// genuine programmer bugs.
//
// Rules:
//   - Length 1..13. Empty is rejected (see encodeFreeText doc).
//   - Each character must be in the f71 alphabet (space + 0-9 + A-Z
//   - + - . / ?). Lower-case and other punctuation are rejected;
//     callers normalise upstream.
func validateFreeText(text string) error {
	const op errors.Op = "codec.validateFreeText"
	if len(text) == 0 {
		return errors.New(op).WithMsgf("FreeText is empty; Type 0.0 carries a 1-13 character payload")
	}
	if len(text) > f71MessageLen {
		return errors.New(op).WithMsgf("FreeText %q has length %d, want 1..%d", text, len(text), f71MessageLen)
	}
	for i := range len(text) {
		if !isF71Char(text[i]) {
			return errors.New(op).WithMsgf("FreeText %q contains invalid character %q at index %d (alphabet: space + 0-9 + A-Z + + - . / ?)", text, string(text[i]), i)
		}
	}
	return nil
}

// isF71Char reports whether c is in the f71 alphabet (the 42-char
// free-text alphabet from FreeTextToF71). Mirrors strings.IndexByte
// over f71Alphabet but avoids the import + helper for one-byte
// membership checks in tight validation loops.
func isF71Char(c byte) bool {
	switch {
	case c == ' ':
		return true
	case c >= '0' && c <= '9':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c == '+', c == '-', c == '.', c == '/', c == '?':
		return true
	}
	return false
}

// type1CallToC28 routes a Type 1 Call1/Call2 string to its c28 value.
// Dispatches to TokenToC28 first (so "CQ", "DE", "QRZ", "CQ NNN",
// "CQ XXXX" land in the [0, nTokens) token partition); falls through
// to stdCallToC28 for actual callsigns.
//
// Precondition: the caller has passed validateType1Call, so the input is
// either a recognised token or a std-callsign-shape input.
func type1CallToC28(call string) uint32 {
	if c28, ok := TokenToC28(call); ok {
		return c28
	}
	return stdCallToC28(call)
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

// validateType1Rover rejects the nonsensical combination of a token
// in a Type 1 Call slot with the matching rover bit set. The /R
// suffix means "rover callsign" — tokens (CQ, DE, QRZ, "CQ ...")
// aren't callsigns, so the rover bit on a token slot would have
// no text-layer rendering and no semantic meaning.
//
// The bit-level wire format DOES allow the combination (a remote
// encoder, malformed corpus, or post-LDPC corruption could produce
// it on a 77-bit body), and DecodeMessage stays bit-faithful — it
// will return Message{Call1: "CQ", Rover1: true, ...} for such a
// wire input rather than erroring. This validator is the encode-
// side + format-side gate that prevents OUR encoder from emitting
// the combination and our formatter from rendering it. The
// asymmetry (decode accepts, encode/format reject) is intentional:
// the codec layer is bit-faithful; semantic guards run at encode
// and format boundaries.
func validateType1Rover(call string, rover bool, field string) error {
	const op errors.Op = "codec.validateType1Rover"
	if !rover {
		return nil
	}
	if _, isTok := TokenToC28(call); isTok {
		return errors.New(op).WithMsgf("%s = %q is a token; rover bit cannot be set on a non-callsign", field, call)
	}
	return nil
}

// validateType1Call accepts either a recognised token (per QEX paper
// Table 7) or a standard amateur callsign in the Call1/Call2 slot of
// a Type 1 message. Type 1's c28 slots are multi-modal — both shapes
// land in the same 28-bit field on the wire, distinguished only by
// the c28 partition the value falls into. Routing happens in
// type1CallToC28; this validator is the shape gate.
//
// Returned errors are tagged with the field name (Call1 / Call2)
// and mention both acceptable forms, so the caller sees one error
// covering both rejection paths instead of having to choose between
// "not a callsign" and "not a token".
func validateType1Call(call, field string) error {
	const op errors.Op = "codec.validateType1Call"
	if _, ok := TokenToC28(call); ok {
		return nil
	}
	if err := validateStdCallsign(call, field); err == nil {
		return nil
	}
	return errors.New(op).WithMsgf("%s = %q is neither a recognised token (DE, QRZ, CQ, CQ NNN, CQ X..XXXX) nor a standard amateur callsign (prefix + digit + suffix, 3-6 chars)", field, call)
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
