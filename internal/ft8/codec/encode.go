package codec

import (
	stderrors "errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
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
	case MessageTypeEUVHFP:
		return encodeEUVHFP(m)
	case MessageTypeEUVHFHash:
		return encodeEUVHFHash(m)
	case MessageTypeNonStdCall:
		return encodeNonStdCall(m)
	case MessageTypeFreeText:
		return encodeFreeText(m)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedMessageType, m.Type)
	}
}

// encodeStd packs a Type 1 (Std Msg) body per QEX Table 1:
//
//	c28(Call1) | r1(Suffix1) | c28(Call2) | r1(Suffix2) | R1(AckBit) | g15(Grid) | i3=1
//	    28          1             28           1             1           15        3   = 77 bits
//
// Validation runs over all caller-supplied fields before any Layer 1
// primitive is invoked, so the primitives' panics indicate genuine
// internal bugs (not bad user data).
func encodeStd(m Message) ([]byte, error) {
	if err := validateType1Call(m.Call1, "Call1"); err != nil {
		return nil, err
	}
	if err := validateType1Suffix(m.Call1, m.Suffix1, "Call1"); err != nil {
		return nil, err
	}
	if err := validateType1Call(m.Call2, "Call2"); err != nil {
		return nil, err
	}
	if err := validateType1Suffix(m.Call2, m.Suffix2, "Call2"); err != nil {
		return nil, err
	}
	if err := validateG15Slot(m.Grid); err != nil {
		return nil, err
	}

	var b BitBuilder
	b.Append(uint64(type1CallToC28(m.Call1)), CallsignBits).
		Append(boolBit(m.Suffix1), 1).
		Append(uint64(type1CallToC28(m.Call2)), CallsignBits).
		Append(boolBit(m.Suffix2), 1).
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

// encodeEUVHFP packs a Type 2 (EU VHF /P) body per QEX Table 1:
//
//	c28(Call1) | p1(Suffix1) | c28(Call2) | p1(Suffix2) | R1(AckBit) | g15(Grid) | i3=2
//	    28          1             28           1             1           15        3   = 77 bits
//
// Structurally identical to Type 1 — same widths, same offsets — but
// the c28 partition is restricted to standard callsigns: tokens
// (CQ / DE / QRZ / "CQ <suffix>") are NOT valid in Type 2 per QEX
// paper Table 7 (the token partition is specific to Type 1's c28).
// The per-callsign 1-bit slot is named "p1" in Table 1 and renders
// as /P (portable), distinct from Type 1's "r1" (/R rover); the bit
// itself is stored in the same Suffix1/Suffix2 fields and FormatMessage
// disambiguates by Type.
func encodeEUVHFP(m Message) ([]byte, error) {
	if err := validateType2Call(m.Call1, "Call1"); err != nil {
		return nil, err
	}
	if err := validateType2Call(m.Call2, "Call2"); err != nil {
		return nil, err
	}
	if err := validateG15Slot(m.Grid); err != nil {
		return nil, err
	}

	var b BitBuilder
	b.Append(uint64(CallsignC28(m.Call1)), CallsignBits).
		Append(boolBit(m.Suffix1), 1).
		Append(uint64(CallsignC28(m.Call2)), CallsignBits).
		Append(boolBit(m.Suffix2), 1).
		Append(boolBit(m.AckBit), 1).
		Append(uint64(Grid4ToG15(m.Grid)), G15Bits).
		Append(i3EUVHFP, 3)
	if b.Len() != MessageBits {
		// Belt-and-braces: width-constant regression lands here, not
		// on the wire. See encodeStd's panic for the rationale.
		panic("codec.encodeEUVHFP: assembled bit count is " + strconv.Itoa(b.Len()) + ", want " + strconv.Itoa(MessageBits) + " — width constants out of sync")
	}
	return slices.Clone(b.Bits()), nil
}

// encodeEUVHFHash packs a Type 5 (EU VHF hashes+g25) body per QEX
// Table 1:
//
//	h12(Call1) | h22(Call2) | R1(AckBit) | r3(Report3) | s11(Serial) | g25(Grid6) | i3=5
//	     12          22           1            3              11          25         3   = 77 bits
//
// Both callsigns are hashed on the wire (not packed in full), so the
// receiver needs Phase 4's running hash table to resolve either side
// back to a string. The grid field is a strict 6-character Maidenhead
// locator — no reserved-token or signed-report multiplexing (those
// only exist in the g15 slot used by Types 1/2).
//
// The R1 ack bit and r3 report code carry the QSO state across the
// exchange: the contact runs Call1 → Call2 with progressive ack and
// signal-report values per VHF contest convention.
//
// Validation is upstream of every Layer 1 primitive: Call1/Call2 must
// be valid std-callsign shape (HashCodes' alphabet is forgiving but
// the message-level contract is std-shape only on the wire — Phase 4's
// hash table is populated from std-shape calls); Report3 must be
// 0..7; Serial must be 0..2047; Grid6 must be 6-char Maidenhead.
func encodeEUVHFHash(m Message) ([]byte, error) {
	if err := validateType5Call(m.Call1, "Call1"); err != nil {
		return nil, err
	}
	if err := validateType5Call(m.Call2, "Call2"); err != nil {
		return nil, err
	}
	if err := validateType5Report(m.Report3); err != nil {
		return nil, err
	}
	if err := validateType5Serial(m.Serial); err != nil {
		return nil, err
	}
	if err := validateType5Grid(m.Grid6); err != nil {
		return nil, err
	}

	_, h12, _ := HashCodes(m.Call1)
	_, _, h22 := HashCodes(m.Call2)

	var b BitBuilder
	b.Append(uint64(h12), h12Bits).
		Append(uint64(h22), HashBits22).
		Append(boolBit(m.AckBit), 1).
		Append(uint64(m.Report3), r3Bits).
		Append(uint64(m.Serial), s11Bits).
		Append(uint64(Grid6ToG25(m.Grid6)), G25Bits).
		Append(i3EUVHFHash, i3Width)
	if b.Len() != MessageBits {
		panic("codec.encodeEUVHFHash: assembled bit count is " + strconv.Itoa(b.Len()) + ", want " + strconv.Itoa(MessageBits) + " — width constants out of sync")
	}
	return slices.Clone(b.Bits()), nil
}

// validateType5Call enforces the Type 5 hashed-call shape. The wire
// slot is a hash, so any string the hashcode alphabet accepts would
// technically encode, but the protocol-level expectation is a
// standard amateur callsign (Phase 4's hash table only resolves
// std-shaped calls). Rejecting non-std inputs at encode time keeps
// the encoder's contract tight with the receiver's hash-resolution
// path.
//
// Tokens (CQ / DE / QRZ / "CQ <suffix>") are not valid Type 5
// callsigns — Type 5's hashed slots carry contest-exchange identities,
// not directed-call markers.
func validateType5Call(call, field string) error {
	const op errors.Op = "codec.validateType5Call"
	if _, ok := TokenToC28(call); ok {
		return errors.New(op).WithMsgf("%s = %q is a token (CQ / DE / QRZ / CQ <suffix>); tokens are not valid in Type 5 (EU VHF hashes+g25)", field, call)
	}
	return validateStdCallsign(call, field)
}

// validateType5Report bounds Report3 per QEX Table 2. Values outside
// [r3Min, r3Max] either silently wrap on the 3-bit slot or shift garbage
// into adjacent fields if BitBuilder.Append's overflow guard didn't
// catch them — the validator runs first so the diagnostic names the
// field, not the wire offset.
func validateType5Report(r uint8) error {
	const op errors.Op = "codec.validateType5Report"
	if r > r3Max {
		return errors.New(op).WithMsgf("Report3 = %d is outside [%d, %d] (QEX r3 slot is %d bits; values map to display 52..59)", r, r3Min, r3Max, r3Bits)
	}
	return nil
}

// validateType5Serial bounds Serial per QEX Table 2 (s11 = 0..2047).
func validateType5Serial(s uint16) error {
	const op errors.Op = "codec.validateType5Serial"
	if s > s11Max {
		return errors.New(op).WithMsgf("Serial = %d is outside [0, %d] (QEX s11 slot is %d bits)", s, s11Max, s11Bits)
	}
	return nil
}

// validateType5Grid enforces the strict 6-char Maidenhead shape on
// Grid6. Grid6ToG25 panics on out-of-shape input — this validator
// surfaces the same rejection with a useful error rather than the
// primitive's panic path.
func validateType5Grid(g string) error {
	const op errors.Op = "codec.validateType5Grid"
	if !isGrid6(g) {
		return errors.New(op).WithMsgf("Grid6 = %q is not a 6-char Maidenhead locator ([A-R][A-R][0-9][0-9][A-X][A-X])", g)
	}
	return nil
}

// validateType2Call enforces the Type 2 c28 shape: a standard amateur
// callsign with no token escape. Type 1's `type1CallToC28` routes
// CQ / DE / QRZ / "CQ <suffix>" through the token partition; Type 2
// has no such partition and rejects those inputs.
//
// Returned errors are tagged with the field name (Call1 / Call2) so
// the caller can locate the bad input without inspecting the error
// chain.
func validateType2Call(call, field string) error {
	const op errors.Op = "codec.validateType2Call"
	if _, ok := TokenToC28(call); ok {
		return errors.New(op).WithMsgf("%s = %q is a token (CQ / DE / QRZ / CQ <suffix>); tokens are not valid in Type 2 (EU VHF /P)", field, call)
	}
	return validateStdCallsign(call, field)
}

// encodeNonStdCall packs a Type 4 (NonStd Call) body per QEX Table 1:
//
//	h12(hash) | c58(nonstd) | h1 | r2 | c1 | i3=4
//	   12          58          1    2    1    3   = 77 bits
//
// Field semantics:
//   - h12: 12-bit hash of the std-callsign side (or zero when c1=1).
//   - c58: 58-bit packing of the nonstandard callsign (up to 11 chars,
//     compound prefixes / suffixes, special-event calls).
//   - h1: 0 = hash is Call1, 1 = hash is Call2. The encoder picks this
//     from which call has std-callsign shape.
//   - r2: encodes the trailing token (Grid field) — blank / RRR / RR73 / 73.
//   - c1: 1 = Call1 is CQ and h12 is ignored on the wire. Set from
//     Call1 == "CQ".
//
// Shape rules enforced by validateType4Calls (see its doc): exactly
// one std + one nonstd, OR Call1 == "CQ" + Call2 nonstd; no
// CQ-with-suffix (the c1 flag is 1 bit, no room for "CQ <suffix>");
// Grid restricted to {"", "RRR", "RR73", "73"}.
func encodeNonStdCall(m Message) ([]byte, error) {
	const op errors.Op = "codec.encodeNonStdCall"
	if err := validateType4Calls(m); err != nil {
		return nil, err
	}

	var h12 uint32
	var c58 uint64
	var h1 uint64
	var c1 uint64
	if m.Call1 == "CQ" {
		// c1=1: Call1 is CQ, h12 is ignored on the wire (encode as
		// zero by convention so two identical CQ-from-nonstd messages
		// produce identical wire output). c58 carries Call2 (the
		// nonstd side); h1 stays 0 (hash slot is nominally Call1).
		c1 = 1
		h12 = 0
		c58 = CallsignC58(m.Call2)
		h1 = 0
	} else if isStdCallsignShape(m.Call1) {
		// Call1 is std → goes through h12; Call2 is nonstd → c58.
		// h1 = 0 (hash is the first callsign).
		_, h12, _ = HashCodes(m.Call1)
		c58 = CallsignC58(m.Call2)
		h1 = 0
	} else {
		// Call2 is std → goes through h12; Call1 is nonstd → c58.
		// h1 = 1 (hash is the second callsign).
		_, h12, _ = HashCodes(m.Call2)
		c58 = CallsignC58(m.Call1)
		h1 = 1
	}

	r2, ok := gridToR2(m.Grid)
	if !ok {
		// validateType4Calls already constrained Grid; this is belt-
		// and-braces. A regression in the validator would surface
		// here rather than as garbled wire bits.
		return nil, errors.New(op).WithMsgf("Grid %q does not map to an r2 token (validator regression)", m.Grid)
	}

	var b BitBuilder
	b.Append(uint64(h12), h12Bits).
		Append(c58, C58Bits).
		Append(h1, h1Bits).
		Append(uint64(r2), r2Bits).
		Append(c1, c1Bits).
		Append(i3NonStdCall, i3Width)
	if b.Len() != MessageBits {
		panic("codec.encodeNonStdCall: assembled bit count is " + strconv.Itoa(b.Len()) + ", want " + strconv.Itoa(MessageBits) + " — width constants out of sync")
	}
	return slices.Clone(b.Bits()), nil
}

// validateType4Calls enforces the Type 4 shape rules. See
// encodeNonStdCall's doc for the rule list.
func validateType4Calls(m Message) error {
	const op errors.Op = "codec.validateType4Calls"

	// Grid: must be empty or one of the three reserved tokens. Type
	// 4 has no grid / signed-report slot.
	if _, ok := gridToR2(m.Grid); !ok {
		return errors.New(op).WithMsgf("Grid = %q is not a valid Type 4 token; allowed values are \"\", \"RRR\", \"RR73\", \"73\"", m.Grid)
	}

	// CQ-from-nonstd: Call1 == "CQ" exactly. Type 4's c1 flag is one
	// bit — no "CQ <suffix>" support.
	if m.Call1 == "CQ" {
		if isType4ValidNonStdCall(m.Call2) {
			return nil
		}
		return errors.New(op).WithMsgf("Call2 = %q is not a valid Type 4 nonstandard callsign", m.Call2)
	}
	if strings.HasPrefix(m.Call1, "CQ ") {
		return errors.New(op).WithMsgf("Call1 = %q has a CQ-with-suffix form; Type 4 supports only bare \"CQ\" in Call1 (the c1 wire flag is 1 bit)", m.Call1)
	}
	if _, isTok := TokenToC28(m.Call1); isTok {
		return errors.New(op).WithMsgf("Call1 = %q is a Type 1 token; Type 4 supports only bare \"CQ\" or a callsign", m.Call1)
	}
	if _, isTok := TokenToC28(m.Call2); isTok {
		return errors.New(op).WithMsgf("Call2 = %q is a Type 1 token; tokens are not valid in Type 4", m.Call2)
	}

	// Normal path: exactly one std + one nonstd. Both-std should be
	// Type 1; both-nonstd is ambiguous (the encoder has no rule for
	// picking which side is c58 vs h12).
	std1 := isStdCallsignShape(m.Call1)
	std2 := isStdCallsignShape(m.Call2)
	if std1 && std2 {
		return errors.New(op).WithMsgf("both Call1 = %q and Call2 = %q are standard callsigns; use Type 1 (Std Msg) instead", m.Call1, m.Call2)
	}
	if !std1 && !std2 {
		return errors.New(op).WithMsgf("both Call1 = %q and Call2 = %q are nonstandard; Type 4 requires exactly one std + one nonstd callsign", m.Call1, m.Call2)
	}

	nonstd := m.Call2
	if std2 {
		nonstd = m.Call1
	}
	if !isType4ValidNonStdCall(nonstd) {
		return errors.New(op).WithMsgf("nonstandard callsign %q does not fit the c58 alphabet (1-11 chars, alphabet: space + 0-9 + A-Z + /)", nonstd)
	}

	stdCall := m.Call1
	if std2 {
		stdCall = m.Call2
	}
	if err := validateStdCallsign(stdCall, "std callsign"); err != nil {
		return err
	}
	return nil
}

// isType4ValidNonStdCall reports whether s fits the c58 alphabet
// (length 1..11; characters in " 0-9A-Z/"). Used by Type 4's
// validator to gate inputs upstream of CallsignC58's panic path.
func isType4ValidNonStdCall(s string) bool {
	if len(s) == 0 || len(s) > nonstdCallLen {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c == ' ':
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'Z':
		case c == '/':
		default:
			return false
		}
	}
	return true
}

// gridToR2 maps the Grid string to the 2-bit r2 wire value for Type 4.
// The same string set Type 1 uses for the g15 reserved-token slot,
// minus the grid / signed-report forms (which Type 4 has no slot for).
//
// Returns (r2Value, ok). ok=false signals a Grid value that doesn't
// fit Type 4; callers (validator + encoder) treat it as a validation
// failure.
func gridToR2(grid string) (uint8, bool) {
	switch grid {
	case "":
		return r2Blank, true
	case "RRR":
		return r2RRR, true
	case "RR73":
		return r2RR73, true
	case "73":
		return r2_73, true
	}
	return 0, false
}

// r2ToGrid is the inverse of gridToR2. Returns the canonical string
// form for the 2-bit r2 wire value. Panics for r2 ≥ 4 (the wire layer
// masks to 2 bits, so this only fires on internal misuse).
func r2ToGrid(r2 uint8) string {
	switch r2 {
	case r2Blank:
		return ""
	case r2RRR:
		return "RRR"
	case r2RR73:
		return "RR73"
	case r2_73:
		return "73"
	}
	panic("codec.r2ToGrid: r2=" + strconv.Itoa(int(r2)) + " exceeds 2 bits")
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
// to CallsignC28 for actual standard callsigns.
//
// Precondition: the caller has passed validateType1Call, so the input is
// either a recognised token or a std-callsign-shape input. Per QEX
// Appendix A, every std-shape callsign packs into the [stdCallOffset,
// 2^28) range — short calls don't go through HashedCallC28.
func type1CallToC28(call string) uint32 {
	if c28, ok := TokenToC28(call); ok {
		return c28
	}
	return CallsignC28(call)
}

// boolBit converts a Go bool to its 1-bit numeric form for BitBuilder.
func boolBit(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}

// validateType1Suffix rejects the nonsensical combination of a token
// in a Type 1 Call slot with the matching suffix bit set. The /R
// suffix means "rover callsign" — tokens (CQ, DE, QRZ, "CQ ...")
// aren't callsigns, so the suffix bit on a token slot would have
// no text-layer rendering and no semantic meaning.
//
// The bit-level wire format DOES allow the combination (a remote
// encoder, malformed corpus, or post-LDPC corruption could produce
// it on a 77-bit body), and DecodeMessage stays bit-faithful — it
// will return Message{Call1: "CQ", Suffix1: true, ...} for such a
// wire input rather than erroring. This validator is the encode-
// side + format-side gate that prevents OUR encoder from emitting
// the combination and our formatter from rendering it. The
// asymmetry (decode accepts, encode/format reject) is intentional:
// the codec layer is bit-faithful; semantic guards run at encode
// and format boundaries.
func validateType1Suffix(call string, suffix bool, field string) error {
	const op errors.Op = "codec.validateType1Suffix"
	if !suffix {
		return nil
	}
	if _, isTok := TokenToC28(call); isTok {
		return errors.New(op).WithMsgf("%s = %q is a token; suffix bit cannot be set on a non-callsign", field, call)
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

// stdCallPrefixLen reports the prefix length (1 or 2) of a std-shape
// FT8 callsign per QEX paper §A:
//
//	[A-Z0-9]{1,2} + [0-9] + [A-Z]{1,3}
//
// with the constraint that at least one of the 1-2 prefix chars is
// a letter. Returns 0 if the input doesn't match either prefix
// length. The prefix length determines the digit's position (input
// index 1 for 1-char prefix, index 2 for 2-char prefix) which
// CallsignC28 uses to align the call to field position 3.
//
// Non-std calls (/P, /M, compound prefixes, etc.) return 0 and are
// routed to CallsignC58 by Layer 2 — they do not produce a
// Type 1 message.
func stdCallPrefixLen(s string) int {
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
		return prefixLen
	}
	return 0
}

// isStdCallsignShape reports whether s matches the FT8 standard-
// callsign format. Thin wrapper around stdCallPrefixLen for callers
// that only need the boolean answer.
func isStdCallsignShape(s string) bool {
	return stdCallPrefixLen(s) > 0
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
