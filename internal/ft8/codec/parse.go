package codec

import (
	stderrors "errors"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// ErrEmptyMessage is returned by ParseMessage when the input has no
// non-whitespace content. Phase 2D's parser doesn't accept empty
// strings as the type-0.0 Free Text "blank message"; that lands with
// the Type 0.0 parser in Phase 3.
var ErrEmptyMessage = stderrors.New("codec: empty message")

// ErrUnrecognisedFormat is returned by ParseMessage when the input
// doesn't match any known message layout (Type 1 / Type 2 / Free Text
// at the time of writing). Distinguishes "we don't know how to parse
// this" from "we know it's a Type X message that hasn't been
// implemented".
var ErrUnrecognisedFormat = stderrors.New("codec: input doesn't match any recognised message layout")

// ParseMessage parses the human-readable FT8 text form of a message
// into a Message struct ready to feed EncodeMessage. Inverse of
// FormatMessage; together they form the text-layer above the
// bit-level codec.
//
// Currently supported types: Type 1 ("Std Msg", Phase 2D), Type 2
// ("EU VHF /P", Phase 3B), Type 4 ("NonStd Call", Phase 3C), and
// Type 0.0 ("Free Text", Phase 3A).
//
// Type 1 patterns recognised:
//
//	<call1> <call2>                  - 2-field, blank g15 slot
//	<call1> <call2> <field>          - 3-field
//	<call1> <call2> R <field>        - 4-field, AckBit set, grid/token field
//	CQ <call2> [<field>]             - CQ Call1
//	CQ <suffix> <call2> [<field>]    - "CQ <suffix>" Call1
//	DE <call2> [<field>]             - DE Call1 (uncommon)
//	QRZ <call2> [<field>]            - QRZ Call1 (uncommon)
//
// <call1> / <call2> accept the trailing "/R" rover suffix.
// <field> accepts a 4-char grid, a signed report ("+02", "-11"), a
// reserved token ("RRR", "RR73", "73"), or an R-fused report ("R-09").
//
// Type 2 patterns recognised (mirror Type 1 but with /P portable
// suffix in place of /R, and no token escape in Call1):
//
//	<call1>[/P] <call2>[/P] [<field>]
//
// Type 4 patterns recognised:
//
//	<hashed> <nonstd> [<token>]      - angle-bracket display form
//	<nonstd> <hashed> [<token>]      - hashed-second variant
//	CQ <nonstd> [<token>]            - c1=1 wire form
//
// where <hashed> is a std callsign wrapped in angle brackets
// (display convention for the h12 wire field; parser strips
// brackets), <nonstd> is a compound or special-event callsign in
// the c58 alphabet, and <token> is one of "RRR", "RR73", "73"
// (mapped to the 2-bit r2 wire field).
//
// Classifier dispatch order:
//
//  1. Free Text — presence of '.' or '?' (chars unique to the f71
//     alphabet).
//  2. Type 4 — angle brackets present, OR any token has a non-
//     trailing slash, OR a trailing slash that isn't /R or /P.
//  3. Type 2 — any token has a /P trailing suffix.
//  4. Type 1 — default for unambiguous std-callsign-shaped inputs.
//
// Inputs are upper-cased internally; the caller doesn't need to
// pre-normalise. Anything that fails to match returns
// ErrUnrecognisedFormat, ErrEmptyMessage, or a per-field validation
// error.
func ParseMessage(text string) (Message, error) {
	normalised := normalizeText(text)

	// Empty / whitespace-only check first.
	trimmed := strings.TrimSpace(normalised)
	if trimmed == "" {
		return Message{}, ErrEmptyMessage
	}

	// Free Text dispatch: '.' and '?' are unique to the f71 alphabet
	// (not present in the std-callsign / suffix / signed-report
	// alphabet that structured parsing uses). Their presence is the
	// operator's unambiguous Free Text signal — UNLESS the input also
	// contains angle brackets, which are not in the f71 alphabet but
	// ARE in the structured-message hashed-call display form
	// (Type 4 / Type 5 "<call>" and the "<...>" sentinel both contain
	// dots inside brackets). Inputs without a Free Text trigger char
	// try structured first; if structured fails they error out rather
	// than silently falling back — see the Phase 3A Step A design
	// rationale.
	if strings.ContainsAny(trimmed, ".?") && !strings.ContainsAny(trimmed, "<>") {
		return parseFreeText(trimmed)
	}

	tokens := strings.Fields(normalised)

	// Type 5 (EU VHF hashes+g25) dispatch: BOTH first two tokens are
	// angle-bracketed (the wire hashes both sides, so WSJT-X display
	// brackets both) AND the last token is a 6-char Maidenhead grid.
	// Type 4 has at most one bracketed call AND no 6-char grid, so the
	// dual-bracket + g25-grid combination is unambiguous. Checked
	// before Type 4 so a Type 5 input isn't mis-routed to Type 4's
	// single-bracket parser.
	if isType5Trigger(tokens) {
		return parseEUVHFHash(tokens)
	}

	// Type 4 (NonStd Call) dispatch: angle brackets or a slash in a
	// position that isn't the Type 1 /R or Type 2 /P trailing suffix
	// signal a nonstd callsign. Checked before Type 2 so a compound
	// nonstd ending in /P (e.g., "PJ4/K1ABC/P") routes to Type 4
	// rather than Type 2 (Type 2's c28 partition is std-callsign-only).
	for _, tok := range tokens {
		if isType4Trigger(tok) {
			return parseNonStdCall(tokens)
		}
	}

	// Type 2 (EU VHF /P) dispatch: any field token with a /P suffix
	// signals Type 2. /P is unique to Type 2 — Type 1's per-call
	// suffix is /R, handled inside consumeCall further down. The
	// pre-scan is one HasSuffix check per token; structurally
	// symmetric with the . / ? Free Text trigger above.
	for _, tok := range tokens {
		if strings.HasSuffix(tok, "/P") {
			return parseEUVHFP(tokens)
		}
	}

	// Type 3 (RTTY Roundup) dispatch: "TU;" first-token prefix OR a
	// "5N9" 3-digit report token (N ∈ 2..9). Neither shape overlaps
	// with Type 1 (whose g15 field is a 4-char grid / signed report /
	// reserved token — none match "5N9") or any other type, so the
	// trigger is unambiguous.
	if isType3Trigger(tokens) {
		return parseRTTYRoundup(tokens)
	}

	// Try Type 1 (Std Msg) — the default for std-callsign-shaped
	// inputs. If parseStd fails AND the input fits the Free Text
	// constraints (≤ 13 chars, all chars in the f71 alphabet), fall
	// back to Type 0.0. Per QEX paper Table 1, the Type 0.0 example
	// "TNX BOB 73 GL" contains no '.' or '?' so the eager Free Text
	// trigger above wouldn't fire — without this fallback, valid
	// Free Text inputs without those marker chars would error out
	// (finding #3). The fallback is conservative: it only applies
	// when no structured-message classifier triggered AND parseStd
	// failed AND the input is genuinely f71-shaped.
	msg, err := parseStd(tokens)
	if err == nil {
		return msg, nil
	}
	if validateFreeText(trimmed) == nil {
		return parseFreeText(trimmed)
	}
	return Message{}, err
}

// isType4Trigger reports whether a parsed field-token signals a
// Type 4 (NonStd Call) message. Triggers:
//
//   - Angle brackets ('<' or '>') — WSJT-X hashed-call display form.
//   - A '/' that isn't the Type 1 /R or Type 2 /P trailing suffix
//     (covers mid-position slashes like "PJ4/K1ABC" and non-/R/non-/P
//     trailing suffixes like "/M", "/MM", "/AM", "/QRP").
//   - Length > 6 with no slash — special-event calls like
//     "YW18FIFA" have no slash but exceed the std-callsign max
//     length per QEX paper §A (prefix 1-2 + digit + suffix 1-3 =
//     6 chars). /R-suffixed std calls can be 7-8 chars (caught by
//     the slash branch above, which routes them back to Type 1).
func isType4Trigger(tok string) bool {
	if strings.ContainsAny(tok, "<>") {
		return true
	}
	idx := strings.IndexByte(tok, '/')
	if idx < 0 {
		return len(tok) > 6
	}
	// A slash exists. If it's the trailing two characters AND the
	// suffix is /R or /P, treat as Type 1 / Type 2; otherwise it's
	// nonstd-call territory.
	if idx == len(tok)-2 {
		suffix := tok[idx:]
		if suffix == "/R" || suffix == "/P" {
			return false
		}
	}
	return true
}

// isType5Trigger reports whether the parsed token stream signals a
// Type 5 (EU VHF hashes+g25) message. Trigger: tokens[0] and tokens[1]
// both have outer angle brackets AND tokens[len-1] is a 6-char
// Maidenhead grid. Type 4 (NonStd Call) carries at most one bracketed
// call and no 6-char grid, so this combination unambiguously routes
// to Type 5 before the Type 4 trigger fires.
//
// Minimum length 4 (two bracketed calls + report+serial + grid6);
// the AckBit prefix adds a fifth "R" token.
func isType5Trigger(tokens []string) bool {
	if len(tokens) < 4 {
		return false
	}
	if !isAngleBracketed(tokens[0]) || !isAngleBracketed(tokens[1]) {
		return false
	}
	return isGrid6(tokens[len(tokens)-1])
}

// isAngleBracketed reports whether s is wrapped in outer '<' / '>'
// (length ≥ 2 and the literal sentinel "<...>" both qualify).
func isAngleBracketed(s string) bool {
	return len(s) >= 2 && s[0] == '<' && s[len(s)-1] == '>'
}

// parseEUVHFHash parses the Type 5 layout per the format produced by
// formatEUVHFHash:
//
//	<call1> <call2> [R ]rrSSSS GRID6
//
// where rr is the 2-digit display form of Report3 (52..59) and SSSS
// is the zero-padded serial (s11 max 2047 fits in 4 decimal digits).
// The 4- and 5-token layouts (with/without ack "R") are the two
// recognised shapes.
//
// Reaches here only when isType5Trigger has fired, so tokens[0] and
// tokens[1] are guaranteed bracketed and tokens[len-1] is a 6-char
// grid; this parser strips brackets, splits report+serial, validates,
// and packages the Message.
func parseEUVHFHash(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 4 || len(tokens) > 5 {
		return Message{}, errors.New(op).WithMsgf("Type 5 (EU VHF hashes+g25) message has %d field(s); want 4 or 5", len(tokens))
	}

	call1 := stripAngleBrackets(tokens[0])
	call2 := stripAngleBrackets(tokens[1])
	grid6 := tokens[len(tokens)-1]

	ack := false
	bodyIdx := 2
	if len(tokens) == 5 {
		if tokens[2] != "R" {
			return Message{}, errors.New(op).WithMsgf("Type 5 message of 5 fields must have \"R\" ack at index 2, got %q", tokens[2])
		}
		ack = true
		bodyIdx = 3
	}

	report, serial, err := splitReportSerial(tokens[bodyIdx])
	if err != nil {
		return Message{}, err
	}

	return Message{
		Type:    MessageTypeEUVHFHash,
		Call1:   call1,
		Call2:   call2,
		AckBit:  ack,
		Report3: report,
		Serial:  serial,
		Grid6:   grid6,
	}, nil
}

// splitReportSerial splits the fused "rrSSSS" body token (e.g.
// "570007") into its r3 code and s11 serial. The 2-char report prefix
// is the 2-digit form of Report3 ("52".."59"); the trailing 1..4
// digits (s11 max 2047 → 4 decimal digits max) are the serial.
// Inputs shorter than 3 chars or with a non-digit report prefix are
// rejected — the trigger guarantees Type 5 routing got here for a
// reason, but the body's exact shape still needs validation.
func splitReportSerial(tok string) (uint8, uint16, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tok) < 3 || len(tok) > 6 {
		return 0, 0, errors.New(op).WithMsgf("Type 5 report+serial token %q has length %d, want 3..6 (2-digit report + 1-4 digit serial)", tok, len(tok))
	}
	for i := range len(tok) {
		if tok[i] < '0' || tok[i] > '9' {
			return 0, 0, errors.New(op).WithMsgf("Type 5 report+serial token %q has non-digit char %q at index %d", tok, string(tok[i]), i)
		}
	}
	reportInt, _ := strconv.Atoi(tok[:2])
	if reportInt < r3Bias+r3Min || reportInt > r3Bias+r3Max {
		return 0, 0, errors.New(op).WithMsgf("Type 5 report %q is outside the QEX display range [%d, %d]", tok[:2], r3Bias+r3Min, r3Bias+r3Max)
	}
	serialInt, _ := strconv.Atoi(tok[2:])
	if serialInt > s11Max {
		return 0, 0, errors.New(op).WithMsgf("Type 5 serial %q exceeds s11 max %d", tok[2:], s11Max)
	}
	return uint8(reportInt - r3Bias), uint16(serialInt), nil
}

// parseFreeText validates and packages a Type 0.0 message.
// Reached via two paths:
//
//   - Eager dispatch when the input contains '.' or '?' (chars
//     unique to the f71 alphabet — unambiguous Free Text signal).
//   - Fallback after structured Type 1 parse fails AND the input
//     fits the f71 constraints (finding #3 — handles QEX Table 1's
//     Type 0.0 example "TNX BOB 73 GL", which has no trigger char).
//
// validateFreeText shares the same gate as the encoder so any text
// that parses also encodes.
func parseFreeText(text string) (Message, error) {
	if err := validateFreeText(text); err != nil {
		return Message{}, err
	}
	return Message{
		Type:     MessageTypeFreeText,
		FreeText: text,
	}, nil
}

// parseEUVHFP dispatches Type 2 (EU VHF /P) inputs based on the
// first token. Per QEX Table 2 the c28 field (used in BOTH Type 1
// and Type 2 Call slots) accepts standard callsigns AND tokens
// (CQ / DE / QRZ / "CQ <suffix>"), so Type 2 has the same token-
// prefixed layouts as Type 1, just with /P suffixes on the std
// calls instead of /R. The earlier carve-out that rejected tokens
// in Type 2 was spec-incorrect — finding #2.
//
// Reaches here only when the classifier has seen a /P-suffixed
// token somewhere in the input. A /R-suffixed token in the same
// input is caught by consumePortableCall as a mixed /R + /P error
// (the wire bit slot is single-Type per message).
func parseEUVHFP(tokens []string) (Message, error) {
	switch tokens[0] {
	case "CQ":
		return parseCQEUVHFP(tokens)
	case "DE", "QRZ":
		return parseDirectedEUVHFP(tokens)
	}
	return parsePlainEUVHFP(tokens)
}

// parseCQEUVHFP handles Type 2 messages starting with CQ — the
// Type 2 mirror of parseCQ. Layouts:
//
//	"CQ <call2/P> [<field>]"
//	"CQ <suffix> <call2/P> [<field>]"
//
// The /P suffix may attach to Call2 (the responding portable
// station); the CQ token itself cannot take /P (validateType2Suffix
// would reject Suffix1=true on a token at encode/format time).
func parseCQEUVHFP(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("CQ (Type 2) message has only %d field(s); need at least \"CQ <callsign/P>\"", len(tokens))
	}

	hasSuffix := len(tokens) >= 3 && isCQSuffix(tokens[1])
	var call1 string
	var rest []string
	if hasSuffix {
		call1 = "CQ " + tokens[1]
		rest = tokens[2:]
	} else {
		call1 = "CQ"
		rest = tokens[1:]
	}
	if len(rest) == 0 {
		return Message{}, errors.New(op).WithMsgf("CQ (Type 2) message is missing the second callsign")
	}

	call2, suffix2, fieldTokens, err := consumePortableCall(rest)
	if err != nil {
		return Message{}, err
	}
	grid, ack, err := consumeGridField(fieldTokens)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Type:    MessageTypeEUVHFP,
		Call1:   call1,
		Call2:   call2,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}, nil
}

// parseDirectedEUVHFP handles DE / QRZ as Call1 for Type 2 — the
// Type 2 mirror of parseDirected.
//
//	"DE <call2/P> [<field>]"
//	"QRZ <call2/P> [<field>]"
func parseDirectedEUVHFP(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("%s (Type 2) message is missing the second callsign", tokens[0])
	}
	call2, suffix2, fieldTokens, err := consumePortableCall(tokens[1:])
	if err != nil {
		return Message{}, err
	}
	grid, ack, err := consumeGridField(fieldTokens)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Type:    MessageTypeEUVHFP,
		Call1:   tokens[0],
		Call2:   call2,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}, nil
}

// parsePlainEUVHFP handles "<call1/P> <call2/P> [<field>]" — the
// canonical non-token Type 2 layout, e.g. "G4ABC/P PA9XYZ JO22"
// per QEX Table 1's example.
func parsePlainEUVHFP(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("Type 2 (EU VHF /P) message has only %d field(s); need at least two callsigns", len(tokens))
	}
	call1, suffix1, rest, err := consumePortableCall(tokens)
	if err != nil {
		return Message{}, err
	}
	call2, suffix2, fieldTokens, err := consumePortableCall(rest)
	if err != nil {
		return Message{}, err
	}
	grid, ack, err := consumeGridField(fieldTokens)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Type:    MessageTypeEUVHFP,
		Call1:   call1,
		Call2:   call2,
		Suffix1: suffix1,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}, nil
}

// isType3Trigger reports whether the parsed token stream signals a
// Type 3 (RTTY Roundup) message. Two unambiguous markers:
//
//   - "TU;" as the first token — Type 3's t1 prefix bit is the
//     only Type that renders this string. Other Types' parsers
//     would error on it.
//   - Any "5N9" 3-digit report token (N ∈ 2..9). Per QEX Table 2
//     r3 row, Type 3 reports display as 529, 539, …, 599 — a
//     shape that doesn't overlap with Type 1's g15 field
//     (4-char grids never look like "5N9"; signed reports always
//     carry a leading +/-; reserved tokens are letter-strings).
func isType3Trigger(tokens []string) bool {
	if len(tokens) > 0 && tokens[0] == "TU;" {
		return true
	}
	for _, tok := range tokens {
		if isType3Report(tok) {
			return true
		}
	}
	return false
}

// isType3Report reports whether tok matches the Type 3 r3 display
// form "5N9" where N is one of the digits 2..9 (mapping to
// Report3 = 0..7 via the r3DisplayBiasType3 offset).
func isType3Report(tok string) bool {
	if len(tok) != 3 {
		return false
	}
	if tok[0] != '5' || tok[2] != '9' {
		return false
	}
	n := tok[1]
	return n >= '2' && n <= '9'
}

// parseRTTYRoundup parses the Type 3 layout per formatRTTYRoundup's
// output shape:
//
//	[TU; ]<call1> <call2> [R ]<5N9> <exchange>
//
// where call1 / call2 are full c28 (std callsign OR token per QEX
// Table 2), R is the optional ack flag, the 3-digit report carries
// r3, and exchange is either a digit string (serial 0..7999) or
// a state/province abbreviation from the QEX ref [14] lookup table.
//
// Type 3 has no per-call suffix slots, so /R and /P in call tokens
// surface as a "not a valid std callsign" error from validateType1Call
// rather than as wire-format suffix bits.
func parseRTTYRoundup(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"

	tu := false
	if len(tokens) > 0 && tokens[0] == "TU;" {
		tu = true
		tokens = tokens[1:]
	}
	if len(tokens) < 4 {
		return Message{}, errors.New(op).WithMsgf("Type 3 (RTTY RU) message has only %d field(s) after stripping optional \"TU;\"; need <call1> <call2> [R] <report> <exchange>", len(tokens))
	}

	// Pull Call1 (token-aware: 1 or 2 tokens depending on CQ-suffix).
	var call1 string
	var rest []string
	if tokens[0] == "CQ" && isCQSuffix(tokens[1]) {
		call1 = "CQ " + tokens[1]
		rest = tokens[2:]
	} else {
		call1 = tokens[0]
		rest = tokens[1:]
	}
	if err := validateType1Call(call1, "Call1"); err != nil {
		return Message{}, err
	}

	// Call2 (no suffix possible in Type 3).
	if len(rest) < 3 {
		return Message{}, errors.New(op).WithMsgf("Type 3 message is missing call2 / report / exchange after %q", call1)
	}
	call2 := rest[0]
	rest = rest[1:]
	if err := validateType1Call(call2, "Call2"); err != nil {
		return Message{}, err
	}

	// Optional "R" ack flag.
	ack := false
	if rest[0] == "R" {
		ack = true
		rest = rest[1:]
	}

	// Must have report + exchange remaining.
	if len(rest) != 2 {
		return Message{}, errors.New(op).WithMsgf("Type 3 message has %d unexpected trailing field(s) after report+exchange should be the last two", len(rest))
	}
	reportTok := rest[0]
	exchangeTok := rest[1]

	if !isType3Report(reportTok) {
		return Message{}, errors.New(op).WithMsgf("Type 3 report %q is not in the QEX r3 display form \"5N9\" (N ∈ 2..9)", reportTok)
	}
	r3 := uint8(reportTok[1]-'0') - r3DisplayBiasType3

	// Exchange: digits → serial; else → state/province lookup.
	var serial uint16
	var state string
	if allDigits(exchangeTok) {
		// 1..4 digit positive serial 0..7999.
		var v uint64
		for i := range len(exchangeTok) {
			v = v*10 + uint64(exchangeTok[i]-'0')
		}
		if v > s13SerialMax {
			return Message{}, errors.New(op).WithMsgf("Type 3 serial %q (%d) is outside [0, %d]", exchangeTok, v, s13SerialMax)
		}
		serial = uint16(v)
	} else {
		if _, ok := StateToS13(exchangeTok); !ok {
			return Message{}, errors.New(op).WithMsgf("Type 3 exchange %q is neither a digit-only serial nor a recognised state/province abbreviation", exchangeTok)
		}
		state = exchangeTok
	}

	return Message{
		Type:          MessageTypeRTTYRU,
		Call1:         call1,
		Call2:         call2,
		TU:            tu,
		AckBit:        ack,
		Report3:       r3,
		Serial:        serial,
		StateProvince: state,
	}, nil
}

// consumePortableCall is the Type 2 analogue of consumeCall. Strips
// the trailing /P portable suffix and reports it via the returned
// suffix bool. If the call carries /R instead, that's a mixed Type 1
// + Type 2 input — the bit slot is single-Type per message — and
// surfaces as an explicit error rather than a generic callsign-shape
// rejection.
func consumePortableCall(tokens []string) (string, bool, []string, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) == 0 {
		return "", false, nil, errors.New(op).WithMsgf("missing callsign field")
	}
	raw := tokens[0]
	suffix := false
	if strings.HasSuffix(raw, "/P") {
		suffix = true
		raw = raw[:len(raw)-2]
	} else if strings.HasSuffix(raw, "/R") {
		return "", false, nil, errors.New(op).WithMsgf("callsign %q has /R suffix in a Type 2 (EU VHF /P) context; mixed /R + /P is not a valid single-Type wire shape", tokens[0])
	}
	if err := validateStdCallsign(raw, "callsign"); err != nil {
		return "", false, nil, err
	}
	return raw, suffix, tokens[1:], nil
}

// parseNonStdCall parses the Type 4 (NonStd Call) layout. Two
// callsign tokens (in either order — std-shaped is the hashed side,
// nonstd-shaped is the c58 side) optionally followed by one of the
// four r2 trailing tokens ("", "RRR", "RR73", "73"). The CQ-from-
// nonstd form is "CQ <nonstd> [<token>]".
//
// Reaches here only when isType4Trigger has fired for at least one
// field token. The encoder downstream of this parser enforces the
// full shape rules (exactly one std + one nonstd, etc.) — this
// parser strips brackets uniformly and routes; the encoder
// validates.
func parseNonStdCall(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("Type 4 (NonStd Call) message has only %d field(s); need at least two callsigns", len(tokens))
	}
	if len(tokens) > 3 {
		return Message{}, errors.New(op).WithMsgf("Type 4 (NonStd Call) message has %d fields; max is two callsigns + one trailing token", len(tokens))
	}

	call1 := stripAngleBrackets(tokens[0])
	call2 := stripAngleBrackets(tokens[1])

	grid := ""
	if len(tokens) == 3 {
		raw := tokens[2]
		if _, ok := gridToR2(raw); !ok {
			return Message{}, errors.New(op).WithMsgf("Type 4 trailing token %q is not valid; allowed values are \"RRR\", \"RR73\", \"73\"", raw)
		}
		grid = raw
	}

	return Message{
		Type:  MessageTypeNonStdCall,
		Call1: call1,
		Call2: call2,
		Grid:  grid,
	}, nil
}

// stripAngleBrackets removes a matched pair of '<' / '>' from the
// outer ends of s. Used by parseNonStdCall to undo the WSJT-X display
// convention for hashed callsigns. The literal "<...>" sentinel is
// passed through unchanged so a parser → encode cycle on a decoded
// unresolved-hash Message surfaces as a validation error at the
// encode step rather than as a corrupted "..." callsign.
func stripAngleBrackets(s string) string {
	if s == hashedCallSentinel {
		return s
	}
	if len(s) >= 2 && s[0] == '<' && s[len(s)-1] == '>' {
		return s[1 : len(s)-1]
	}
	return s
}

// normalizeText upper-cases ASCII letters and is a no-op for other
// runes. ParseMessage uses strings.Fields after this so any sequence
// of whitespace collapses to a single field separator naturally.
func normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// parseStd dispatches on the recognised Type 1 layouts. The first
// token classifies: "CQ" / "DE" / "QRZ" → directed-call patterns;
// anything else → the standard <call1> <call2> [field] pattern.
func parseStd(tokens []string) (Message, error) {
	switch tokens[0] {
	case "CQ":
		return parseCQ(tokens)
	case "DE", "QRZ":
		return parseDirected(tokens)
	}
	return parsePlainStd(tokens)
}

// parseCQ handles the four CQ-prefixed layouts:
//
//	"CQ <call2>"                              len 2
//	"CQ <call2> <field>"                      len 3
//	"CQ <suffix> <call2>"                     len 3
//	"CQ <suffix> <call2> <field>"             len 4
//	"CQ <call2> R <field>"                    len 4 (ack + grid)
//	"CQ <suffix> <call2> R <field>"           len 5 (ack + grid)
//
// Disambiguation for 3-token CQ messages: if tokens[1] is a valid
// CQ-suffix (3-digit or 1-4-letter all-uppercase, NOT a callsign)
// AND tokens[2] is a callsign, treat tokens[1] as the suffix; else
// tokens[1] is Call2. Tokens cannot be both — CQ-suffix shapes
// (all-digit 3-char or all-letter ≤4-char) don't overlap with std-
// callsign shapes (prefix+digit+suffix).
func parseCQ(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("CQ message has only %d field(s); need at least \"CQ <callsign>\"", len(tokens))
	}

	// Detect optional CQ-suffix at tokens[1].
	hasSuffix := len(tokens) >= 3 && isCQSuffix(tokens[1])
	var call1 string
	var rest []string
	if hasSuffix {
		call1 = "CQ " + tokens[1]
		rest = tokens[2:]
	} else {
		call1 = "CQ"
		rest = tokens[1:]
	}
	if len(rest) == 0 {
		// "CQ <suffix>" with no Call2 isn't a valid Type 1 — both
		// c28 slots must be populated.
		return Message{}, errors.New(op).WithMsgf("CQ message is missing the second callsign")
	}

	call2, suffix2, fieldTokens, err := consumeCall(rest)
	if err != nil {
		return Message{}, err
	}
	grid, ack, err := consumeGridField(fieldTokens)
	if err != nil {
		return Message{}, err
	}

	msg := Message{
		Type:    MessageTypeStd,
		Call1:   call1,
		Call2:   call2,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}
	return msg, nil
}

// parseDirected handles DE / QRZ as Call1 — both are valid Type 1
// tokens. Layout mirrors plain Std but Call1 is the token.
//
//	"DE <call2> [<field>]"
//	"QRZ <call2> [<field>]"
func parseDirected(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("%s message is missing the second callsign", tokens[0])
	}
	call2, suffix2, fieldTokens, err := consumeCall(tokens[1:])
	if err != nil {
		return Message{}, err
	}
	grid, ack, err := consumeGridField(fieldTokens)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Type:    MessageTypeStd,
		Call1:   tokens[0],
		Call2:   call2,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}, nil
}

// parsePlainStd handles "<call1> <call2> [field]" — the canonical
// non-CQ Type 1 layout.
func parsePlainStd(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("message has only %d field(s); need at least two callsigns", len(tokens))
	}
	call1, suffix1, rest, err := consumeCall(tokens)
	if err != nil {
		return Message{}, err
	}
	call2, suffix2, fieldTokens, err := consumeCall(rest)
	if err != nil {
		return Message{}, err
	}
	grid, ack, err := consumeGridField(fieldTokens)
	if err != nil {
		return Message{}, err
	}
	if _, isTok := TokenToC28(call1); isTok {
		// Tokens belong in the CQ/DE/QRZ branches; reaching here
		// means a token-shaped first field that the classifier
		// missed. Reject explicitly so we don't silently produce
		// a token in Call1 with a path other than parseCQ /
		// parseDirected (which gate suffix usage and CQ-suffix logic).
		return Message{}, errors.New(op).WithMsgf("first field %q is a token; CQ / DE / QRZ have dedicated parse paths", call1)
	}
	return Message{
		Type:    MessageTypeStd,
		Call1:   call1,
		Call2:   call2,
		Suffix1: suffix1,
		Suffix2: suffix2,
		AckBit:  ack,
		Grid:    grid,
	}, nil
}

// consumeCall pulls a callsign (with optional "/R" suffix) off the
// head of tokens and returns (call, suffix, remaining-tokens, error).
// The /R rover suffix encodes to a Suffix1/Suffix2 bit and is stripped
// from the callsign string before validation. (Type 1 only — Type 2's
// /P parser is parseEUVHFP, which uses a similar pattern but recognises
// /P instead of /R.)
func consumeCall(tokens []string) (string, bool, []string, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) == 0 {
		return "", false, nil, errors.New(op).WithMsgf("missing callsign field")
	}
	raw := tokens[0]
	suffix := false
	if strings.HasSuffix(raw, "/R") {
		suffix = true
		raw = raw[:len(raw)-2]
	}
	if err := validateStdCallsign(raw, "callsign"); err != nil {
		return "", false, nil, err
	}
	return raw, suffix, tokens[1:], nil
}

// consumeGridField parses the trailing 0..2 tokens as the g15 slot
// + optional ack prefix. Recognised patterns (in order):
//
//	[]                  - empty: AckBit=0, Grid=""
//	["RRR"|"RR73"|"73"] - reserved token
//	["IO91"]            - grid4
//	["-11"|"+02"]       - signed report
//	["R-11"|"R+02"]     - ack-fused report
//	["R", "IO91"]       - ack + grid (separate tokens)
//	["R", "RRR"|"RR73"|"73"] - ack + reserved
//	["R"]               - bare ack, blank g15
//
// Returns (Grid, AckBit, error). Validation against validateG15Slot
// happens here too so a bad shape surfaces at parse time with a
// useful diagnostic rather than at the encode layer.
func consumeGridField(tokens []string) (string, bool, error) {
	const op errors.Op = "codec.ParseMessage"

	switch len(tokens) {
	case 0:
		return "", false, nil
	case 1:
		field := tokens[0]
		// Bare ack (just "R") → empty Grid, AckBit set.
		if field == "R" {
			return "", true, nil
		}
		// Ack-fused report: starts with R then a signed report.
		if len(field) > 1 && field[0] == 'R' && isSignedReport(field[1:]) {
			grid := field[1:]
			if err := validateG15Slot(grid); err != nil {
				return "", false, err
			}
			return grid, true, nil
		}
		// Plain grid / report / reserved token.
		if err := validateG15Slot(field); err != nil {
			return "", false, err
		}
		return field, false, nil
	case 2:
		// "R <field>" — ack separated from a non-report g15 token.
		if tokens[0] != "R" {
			return "", false, errors.New(op).WithMsgf("two-token grid field %q %q must start with \"R\" (ack prefix)", tokens[0], tokens[1])
		}
		// Per format convention, R<report> is fused; if R is
		// separated, the g15 field shouldn't be a signed report
		// (would re-fuse on round-trip).
		grid := tokens[1]
		if err := validateG15Slot(grid); err != nil {
			return "", false, err
		}
		if isSignedReport(grid) {
			return "", false, errors.New(op).WithMsgf("two-token \"R %s\" must not separate the ack from a signed report; use the fused form \"R%s\" instead", grid, grid)
		}
		return grid, true, nil
	}
	return "", false, errors.New(op).WithMsgf("grid field has %d tokens (max 2)", len(tokens))
}

// isCQSuffix reports whether s is a valid CQ <suffix> shape per QEX
// Table 7 — a 3-digit decimal or a 1-4-letter all-uppercase string.
// The shape predicate is sharp enough to disambiguate from a std
// callsign (which always contains a digit AND a letter prefix).
func isCQSuffix(s string) bool {
	if len(s) == 3 && allDigits(s) {
		return true
	}
	if len(s) >= 1 && len(s) <= 4 && allLetters(s) {
		return true
	}
	return false
}
