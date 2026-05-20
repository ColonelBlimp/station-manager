package codec

import (
	stderrors "errors"
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
// ("EU VHF /P", Phase 3B), and Type 0.0 ("Free Text", Phase 3A).
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
// Free Text dispatch: per the Phase 3A out-of-alphabet rule, the
// presence of '.' or '?' (characters in the f71 alphabet but NOT in
// the std-callsign alphabet) signals operator intent for Type 0.0.
// Such inputs route to parseFreeText, which preserves internal
// whitespace as message content (trimmed of leading/trailing only).
// Inputs that look structured but don't parse (e.g. "HELLO" with no
// trigger char and no second callsign) still return an error —
// strict structured-or-Free-Text choice.
//
// Type 2 dispatch is symmetric: presence of "/P" as the trailing two
// characters of any whitespace-separated token routes to parseEUVHFP.
// /P is unique to Type 2 (Type 1 uses /R), so the trigger is
// unambiguous. Mixed /R + /P in a single input is rejected by the
// per-call validator inside parseEUVHFP.
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
	// operator's unambiguous Free Text signal. Inputs without these
	// chars try structured first; if structured fails they error out
	// rather than silently falling back — see the Phase 3A Step A
	// design rationale.
	if strings.ContainsAny(trimmed, ".?") {
		return parseFreeText(trimmed)
	}

	tokens := strings.Fields(normalised)

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

	return parseStd(tokens)
}

// parseFreeText validates and packages a Type 0.0 message. The input
// has already passed the classifier (contains '.' or '?' so the
// operator clearly intended Free Text); validateFreeText shares the
// same gate as the encoder so any text that parses also encodes.
func parseFreeText(text string) (Message, error) {
	if err := validateFreeText(text); err != nil {
		return Message{}, err
	}
	return Message{
		Type:     MessageTypeFreeText,
		FreeText: text,
	}, nil
}

// parseEUVHFP parses the Type 2 (EU VHF /P) layout. Mirrors
// parsePlainStd's two-callsign + grid-field shape, but the
// per-callsign suffix is /P (portable) rather than /R (rover),
// and the c28 partition is std-callsign-only — tokens (CQ / DE /
// QRZ / "CQ <suffix>") are not valid in Type 2 per QEX Table 7.
//
// Reaches here only when the classifier has already seen at least
// one /P-suffixed token. A /R-suffixed token in the same input is
// caught by consumePortableCall as a mixed /R + /P error (the
// wire bit slot is single-Type per message).
func parseEUVHFP(tokens []string) (Message, error) {
	const op errors.Op = "codec.ParseMessage"
	if len(tokens) < 2 {
		return Message{}, errors.New(op).WithMsgf("Type 2 (EU VHF /P) message has only %d field(s); need at least two callsigns", len(tokens))
	}
	// Reject Type 1 token starters (CQ / DE / QRZ / "CQ <suffix>") up
	// front so the error message is specific. validateStdCallsign
	// would otherwise reject these on length, with a less informative
	// "callsign too short" diagnostic.
	if isType1TokenStart(tokens[0]) {
		return Message{}, errors.New(op).WithMsgf("Type 2 (EU VHF /P) input begins with token %q; tokens are not valid in Type 2", tokens[0])
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

// isType1TokenStart reports whether the first field of a parsed input
// is a Type 1 c28 token. Used by parseEUVHFP to reject token-bearing
// inputs explicitly, since Type 2 has no token partition. The check
// covers the single-token forms (CQ / DE / QRZ) plus the "CQ <suffix>"
// two-token form (whose first field is "CQ").
func isType1TokenStart(first string) bool {
	switch first {
	case "CQ", "DE", "QRZ":
		return true
	}
	return false
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
