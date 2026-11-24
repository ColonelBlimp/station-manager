package ft8

import (
	"fmt"
	"regexp"
	"strings"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
)

// FT8 message sequencing for answering a CQ (ADR 0029 step e2).
//
// This is the PURE half of the manual/auto sequencer: a parsed-message model
// plus a next-message resolver with no I/O, no timing, and no rig. The
// daemon-side sequencer (step e3) drives it — it feeds each slot's decode in,
// takes the resulting TX message out, and owns timing + keying via the
// TxController. Keeping the rung logic pure means the whole CQ→73 ladder is
// unit-testable without a transmitter, and the same resolver serves both manual
// and (later) auto sequencing — only the send policy differs (ADR 0029).
//
// Scope: answering a CQ — we are the answering station. Calling CQ, which adds
// multi-answerer management, is a later increment.

// FT8 message-token recognisers. A callsign is letters/digits/'/', ≥3 chars,
// with at least one letter AND one digit (the digit requirement separates a
// call from CQ modifiers like DX/EU/POTA); a 4-char Maidenhead grid also bears
// a digit, so it is excluded explicitly. These mirror the SPA's parseCqCall
// helpers (utils/ft8Message.ts) so daemon and browser agree on what a call is.
var (
	gridRe = regexp.MustCompile(`^[A-R]{2}[0-9]{2}$`)
	callRe = regexp.MustCompile(`^[A-Z0-9/]{3,}$`)
)

func isGrid4(t string) bool { return gridRe.MatchString(t) }

func looksLikeCall(t string) bool {
	if !callRe.MatchString(t) || isGrid4(t) {
		return false
	}
	hasDigit, hasAlpha := false, false
	for i := 0; i < len(t); i++ {
		switch c := t[i]; {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'A' && c <= 'Z':
			hasAlpha = true
		}
	}
	return hasDigit && hasAlpha
}

// parseReport reads a wire-form FT8 signal report — an explicit sign followed by
// exactly two digits ("-13", "+04") — into its integer dB value. The strict
// 3-char shape matches what packReportToken accepts on the wire (go-ft8 always
// emits reports zero-padded), so anything else is "not a report".
func parseReport(s string) (int, bool) {
	if len(s) != 3 || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	if s[1] < '0' || s[1] > '9' || s[2] < '0' || s[2] > '9' {
		return 0, false
	}
	n := int(s[1]-'0')*10 + int(s[2]-'0')
	if s[0] == '-' {
		n = -n
	}
	return n, true
}

// Report bounds — the range packReportToken accepts, so a message built from a
// clamped value is always one EncodeStandardMessage will take.
const (
	minReportDB = -50
	maxReportDB = 49
)

// clampReport folds an SNR into the range an FT8 report can actually express.
//
// Exchanges that TRANSMIT a report clamp at the moment they record it, so the
// stored value IS the value that goes on the air — which is what makes the logged
// RST_SENT, the report on the `ft8-qso` SSE and the report the far end hears the
// same number. Keeping the raw SNR instead let them diverge: `their_snr` arrives
// unvalidated from the client on the work-a-caller path, so an out-of-range value
// transmitted +49 while the QSO logged (and forwarded to QRZ/ClubLog) 99.
//
// Only for reports WE send. A RECEIVED report is logged exactly as decoded — that
// token is what was on the air, so clamping it would falsify the record instead of
// correcting it.
func clampReport(snr int) int {
	if snr < minReportDB {
		return minReportDB
	}
	if snr > maxReportDB {
		return maxReportDB
	}
	return snr
}

// formatReport renders an SNR as a 2-digit signed FT8 report ("-13", "+04"),
// clamped so the resolver never emits a message EncodeStandardMessage would
// reject. Report-carrying exchanges store the clamped value too (clampReport), so
// this is a no-op re-clamp for them and the belt-and-braces guard for any other
// caller (the SSE status formatters).
func formatReport(snr int) string {
	snr = clampReport(snr)
	if snr >= 0 {
		return fmt.Sprintf("+%02d", snr)
	}
	return fmt.Sprintf("-%02d", -snr)
}

// msgKind classifies a parsed standard FT8 message by the role its trailing
// token plays in a contact. Only the shapes the answer-a-CQ ladder reacts to
// are distinguished; everything else is msgOther.
type msgKind int

const (
	msgOther       msgKind = iota
	msgCQ                  // CQ [mod...] <call> [grid]
	msgGrid                // <to> <from> <grid4>     — a reply carrying a grid
	msgReport              // <to> <from> <±report>   — a signal report (rung 2)
	msgRReport             // <to> <from> R<±report>  — a rogered report (rung 3)
	msgRoger               // <to> <from> RRR | RR73  — rogered (RR73 also says 73)
	msg73                  // <to> <from> 73
	msgFdExchange          // <to> <from> <class> <section>   — ARRL Field Day exchange (bare, un-rogered)
	msgFdRExchange         // <to> <from> R <class> <section> — rogered ARRL Field Day exchange (their ACK)
)

// message is a standard FT8 message reduced to the fields the sequencer needs.
// For a CQ, from is the calling station and to is empty; for a directed message
// to is the addressed call (first on the wire) and from the sender (second).
// grid/report are populated only when the trailing token carries them.
type message struct {
	kind   msgKind
	to     string
	from   string
	grid   string // 4-char Maidenhead — msgCQ / msgGrid only
	report int    // signal report value — msgReport / msgRReport only
	// ARRL Field Day. fd marks a CQ carrying the "FD" modifier (msgCQ); class and
	// section carry the exchange of an msgFdExchange / msgFdRExchange (e.g. "2A" / "EMA").
	fd      bool
	class   string
	section string
	// rogerSignsOff distinguishes the two tokens msgRoger covers. RR73 carries the
	// 73 — the sender is FINISHED. Bare RRR is an acknowledgement only, and still
	// expects a closing 73. They advance the ladder identically (either is a valid
	// roger), so they share a kind; but anywhere the question is "are they DONE?"
	// the difference is decisive — see resolveConfirmHoldLocked (codex cfaa6404 P2).
	rogerSignsOff bool
}

// SpotFrom extracts a PSK Reporter reception spot — the TRANSMITTING station's
// callsign and (when the message carries it) grid — from a decoded FT8 line.
// ok is false for lines with no resolvable sender (hashed `<...>`, free text, bare
// modifiers). The sender is the message's `from`: for a CQ it's the caller (grid
// included); for a directed message it's the second call (grid only on a grid
// reply). Reuses parseMessage so the spot view matches the sequencer's.
func SpotFrom(text string) (call, grid string, ok bool) {
	// A combined message is spotted once, as its resolved sender, with no grid:
	// the line carries none (operator ruling 2026-11-24). Unresolved: no spot.
	if c, isCombined := parseCombined(text); isCombined {
		return c.sender, "", true
	}
	m := parseMessage(text)
	if m.from == "" {
		return "", "", false
	}
	return m.from, m.grid, true
}

// parseMessage reduces a decoded FT8 line to the sequencer's message model. An
// unrecognised or non-standard line parses to a zero message (kind msgOther),
// which the resolver simply ignores — degrade, never break.
func parseMessage(text string) message {
	toks := strings.Fields(strings.ToUpper(strings.TrimSpace(text)))
	if len(toks) == 0 {
		return message{}
	}

	// CQ: skip leading non-call modifiers (DX, EU, POTA, a directed numeric…)
	// and take the first callsign, with an optional trailing grid. "FD" is the
	// ARRL Field Day modifier — noted so the answer routes to the FD ladder, not
	// a standard grid reply (which would be the wrong exchange).
	if toks[0] == "CQ" {
		fd := false
		for i := 1; i < len(toks); i++ {
			if toks[i] == "FD" {
				fd = true
				continue
			}
			if looksLikeCall(toks[i]) {
				m := message{kind: msgCQ, from: toks[i], fd: fd}
				if i+1 < len(toks) && isGrid4(toks[i+1]) {
					m.grid = toks[i+1]
				}
				return m
			}
		}
		return message{}
	}

	// Directed: <to> <from> [token]. Both leading tokens must be callsigns.
	if len(toks) < 2 || !looksLikeCall(toks[0]) || !looksLikeCall(toks[1]) {
		return message{}
	}
	m := message{to: toks[0], from: toks[1]}
	if len(toks) < 3 {
		return m // bare call-to-call, no token (kind msgOther)
	}

	switch tok := toks[2]; {
	case tok == "RRR" || tok == "RR73":
		m.kind, m.rogerSignsOff = msgRoger, tok == "RR73"
	case tok == "73":
		m.kind = msg73
	// ARRL Field Day exchange. The R-variant "<to> <from> R <class> <section>"
	// (msgFdRExchange) is the partner's ROGER — the answerer requires it at rung 2, as it
	// confirms they received OUR exchange. The bare "<to> <from> <class> <section>"
	// (msgFdExchange) is an un-rogered exchange (the opening reply / the future caller
	// side); it must NOT advance the answerer, or a repeated bare exchange would jump the
	// contact to RR73 without the partner ever acknowledging ours. Kept distinct exactly as
	// msgReport (bare) vs msgRReport (R-prefixed) are. The section is confirmed against
	// go-ft8's canonical list so a real report/grid can't be misread as an exchange.
	case tok == "R" && len(toks) >= 5 && looksLikeFdClass(toks[3]) && goft8.ValidARRLFieldDaySection(toks[4]):
		m.kind, m.class, m.section = msgFdRExchange, toks[3], toks[4]
	case looksLikeFdClass(tok) && len(toks) >= 4 && goft8.ValidARRLFieldDaySection(toks[3]):
		m.kind, m.class, m.section = msgFdExchange, tok, toks[3]
	case isGrid4(tok):
		m.kind, m.grid = msgGrid, tok
	default:
		if r, ok := parseReport(tok); ok {
			m.kind, m.report = msgReport, r
		} else if strings.HasPrefix(tok, "R") {
			if r, ok := parseReport(tok[1:]); ok {
				m.kind, m.report = msgRReport, r
			}
		}
	}
	return m
}

// txState is the rung of the answer-a-CQ ladder, named for what we transmit in
// that state.
type txState int

const (
	txCalling    txState = iota // sending <them> <us> <grid>;  waiting to be answered
	txReporting                 // sending <them> <us> R<report>; waiting for RRR/RR73
	txConfirming                // sending <them> <us> 73;        QSO logs after this slot
	txDone                      // 73 sent; nothing more to transmit
)

// label is the lower-case wire/display name for the rung (used in the ft8-qso
// SSE payload).
func (s txState) label() string {
	switch s {
	case txCalling:
		return "calling"
	case txReporting:
		return "reporting"
	case txConfirming:
		return "confirming"
	default:
		return "done"
	}
}

// Exchange is the state of one answer-a-CQ contact. Its resolver methods return
// a new Exchange rather than mutating in place, so the daemon sequencer holds
// one value per active contact and the rung logic stays pure and testable.
type Exchange struct {
	OurCall   string
	OurGrid   string
	TheirCall string

	State txState

	// RcvdReport is the report THEY sent us (rung 2), kept for the logged QSO.
	RcvdReport    int
	HasRcvdReport bool
	// SendSnr is OUR measured SNR of their signal at the moment they reported us
	// (Calling→Reporting) — the value rung 3 reports back, and the report we log
	// as sent. Fixed once set; later rungs don't change it.
	SendSnr    int
	HasSendSnr bool
}

// NewExchange starts answering a CQ: theirCall is the CQing station, ourCall and
// ourGrid identify us. The exchange begins in txCalling, so the first TxMessage
// is our opening call. Callsigns and grid are upper-cased to match decoded text;
// the grid is trimmed to its 4-char Maidenhead field because FT8 standard messages
// carry only 4 characters — a longer configured locator (e.g. "KH78AN") makes
// EncodeStandardMessage reject the message ("not an encodable standard message").
func NewExchange(ourCall, ourGrid, theirCall string) Exchange {
	grid := strings.ToUpper(strings.TrimSpace(ourGrid))
	if len(grid) > 4 {
		grid = grid[:4]
	}
	return Exchange{
		OurCall:   strings.ToUpper(strings.TrimSpace(ourCall)),
		OurGrid:   grid,
		TheirCall: strings.ToUpper(strings.TrimSpace(theirCall)),
		State:     txCalling,
	}
}

// TxMessage returns the standard FT8 message to transmit in the current state
// and whether there is anything to send (false once the exchange is done). The
// text is always a directed "<them> <us> <token>" message and is encodable by
// EncodeStandardMessage.
func (e Exchange) TxMessage() (string, bool) {
	switch e.State {
	case txCalling:
		return e.TheirCall + " " + e.OurCall + " " + e.OurGrid, true
	case txReporting:
		return e.TheirCall + " " + e.OurCall + " R" + formatReport(e.SendSnr), true
	case txConfirming:
		return e.TheirCall + " " + e.OurCall + " 73", true
	default:
		return "", false
	}
}

// Advance applies a decoded message to the exchange. If text is a standard
// message addressed to us from TheirCall that advances the current rung, it
// returns the exchange moved to the next state — recording the report they sent
// and our SNR of their signal — and true. Any other decode (addressed to
// someone else, the wrong token for this rung, or unparseable) returns the
// exchange unchanged and false: the off-ramp / keep-repeating case.
//
// snr is OUR signal-to-noise measurement of this decode (DecodedMessage.SNR),
// recorded as the value we report back at rung 3. The ladder is followed
// strictly — a partner skipping rungs simply does not advance us.
func (e Exchange) Advance(text string, snr int) (Exchange, bool) {
	m := directedTo(text, e.OurCall) // a combined message's half for us counts as its plain form
	if m.to != e.OurCall || m.from != e.TheirCall {
		return e, false
	}
	switch e.State {
	case txCalling:
		// They answered us with a report → roger it next slot.
		if m.kind == msgReport || m.kind == msgRReport {
			// RcvdReport keeps the decoded token verbatim (it IS what was on the
			// air); SendSnr is clamped to the report we will transmit, so the QSO
			// logs the number the far end actually hears.
			e.RcvdReport, e.HasRcvdReport = m.report, true
			e.SendSnr, e.HasSendSnr = clampReport(snr), true
			e.State = txReporting
			return e, true
		}
	case txReporting:
		// They rogered (RRR / RR73) → send our 73. SendSnr is NOT updated here:
		// it's the report we already sent at rung 3 (fixed on entering Reporting),
		// and this decode's SNR is irrelevant — we send no further report.
		if m.kind == msgRoger {
			e.State = txConfirming
			return e, true
		}
	default:
		// txConfirming and txDone have no RX-driven advance: txConfirming
		// completes when WE transmit the 73 (Sent()), and txDone is terminal.
		// A decode in either state is just more band traffic.
	}
	return e, false
}

// Sent advances the exchange after we transmit the current state's message. It
// is the TX-side counterpart to Advance: the only rung whose completion is
// decided by our own transmission rather than a received decode is the final
// 73, which moves txConfirming → txDone — the point at which the QSO is logged
// (step e4). Other states advance only on a received decode, so Sent leaves
// them unchanged.
func (e Exchange) Sent() Exchange {
	if e.State == txConfirming {
		e.State = txDone
	}
	return e
}

// Done reports whether the exchange is complete (our 73 transmitted) and the
// QSO is ready to be logged.
//
// SPECIFICATION-ONLY, deliberately (operator decision, 2026-07-27, on a package
// review that flagged Sent/Done across all six ladders as production-unreachable).
// Production completion runs in the transmit callback instead, and the two must NOT
// be connected: Sent() advances unconditionally, whereas the real terminal
// transition turns on whether the closing message REACHED THE AIR — the Group A /
// Group B distinction (finalrung.go). A pure state function cannot know that, so
// wiring these in would log Group B contacts the partner never received. They stay
// as the readable statement of each ladder's rung model, which is what the pure
// ladder tests assert against; the lifecycle that actually logs is covered by the
// sequencer tests.
func (e Exchange) Done() bool { return e.State == txDone }

// combinedMessage is the DXpedition message: one transmission with two directed
// meanings, sent by a station that is working several callers at once. The
// decoder (go-ft8, unpackDXpedition) renders it
//
//	<call1> RR73; <call2> <SENDER> <report>
//
// a roger to call1 and a report to call2. The sender's own call is carried as a
// hash, printed in angle brackets when the decoder has resolved it and as "<...>"
// when it has not.
//
// Until 2026-11-24 no reader here understood it: parseMessage wants a callsign
// as the second word and finds "RR73;", so the whole line was nothing, and a
// contact the DX station had rogered and logged was abandoned here as "no
// answer" (operator's report; docs/dogfood-inbox.md).
type combinedMessage struct {
	// sender is the transmitting station: the call from the angle brackets. It is
	// the DECODER's attribution (a short hash looked up among calls it has heard),
	// which the rule below accepts. It is not proof of identity.
	sender string
	roger  message // to call1: exactly what "<call1> <sender> RR73" parses to
	report message // to call2: exactly what "<call2> <sender> <report>" parses to
}

// parseCombined reads a combined message, or reports that the line is not one.
// The WHOLE shape is checked before either half is taken: five words, the
// literal "RR73;", two recipients, a RESOLVED sender in angle brackets and a
// valid report. Anything else is not this message, and angle brackets are
// stripped from no other kind of line.
//
//   - An unresolved sender ("<...>") is nobody. No heuristic stands in for it:
//     not "the station we are working", not "the only DX on the band".
//
//   - A recipient may be a hashed token, "<...>" or a call in angle brackets: the
//     other half is often for a station whose call this decoder never resolved,
//     and that must not cost us our own half. A hashed recipient is never us: its
//     token is kept as printed, and no plain call equals it.
//
//     A KNOWN LIMIT, kept on purpose (the clean-room review of 9acd5ce2 raised
//     it). The decoder prints a recipient in angle brackets only when the message
//     carries that call as a hash, which a sender does for a call that does not
//     fit the 28-bit field: a compound call, or one with a suffix (go-ft8,
//     unpack28WithHashes). A plain call such as this station's is carried in
//     full, so it is never hashed by a conforming sender. An operator whose own
//     call is compound or portable would be addressed as "<CALL> RR73; ..." and
//     that half is NOT read here. Reading it means deciding that the decoder's
//     resolution of a recipient hash is enough to say "that is us", and which
//     ladder such a station is on (the type-4 ladder has its own parser and does
//     not read this line at all). That is a decision for the operator, not a
//     default: W-0030 records it as not covered.
//
//   - The same plain call as both recipients is ambiguous and is taken as
//     neither. That is this parser's rule; the decoder does not forbid it.
func parseCombined(text string) (combinedMessage, bool) {
	toks := strings.Fields(strings.ToUpper(strings.TrimSpace(text)))
	if len(toks) != 5 || toks[1] != "RR73;" {
		return combinedMessage{}, false
	}
	to1, to2 := toks[0], toks[2]
	if !combinedRecipient(to1) || !combinedRecipient(to2) {
		return combinedMessage{}, false
	}
	if to1 == to2 && looksLikeCall(to1) {
		return combinedMessage{}, false
	}
	sender, ok := hashedCall(toks[3])
	if !ok || !looksLikeCall(sender) {
		return combinedMessage{}, false
	}
	report, ok := parseReport(toks[4])
	if !ok {
		return combinedMessage{}, false
	}
	return combinedMessage{
		sender: sender,
		roger:  message{kind: msgRoger, to: to1, from: sender, rogerSignsOff: true},
		report: message{kind: msgReport, to: to2, from: sender, report: report},
	}, true
}

// hashedCall returns what stands between the angle brackets of a hashed token,
// and false for anything that is not one. "<...>" yields "...", which is no call.
func hashedCall(tok string) (string, bool) {
	if len(tok) < 3 || tok[0] != '<' || tok[len(tok)-1] != '>' {
		return "", false
	}
	return tok[1 : len(tok)-1], true
}

// combinedRecipient reports whether tok can stand as a recipient of a combined
// message: a plain call, or a hashed token as the decoder prints one, which is
// its unresolved placeholder "<...>" or a call in angle brackets. Angle brackets
// alone do not make a recipient: "<HELLO>" and "<!!!>" are not this message
// (codex-243).
func combinedRecipient(tok string) bool {
	if looksLikeCall(tok) {
		return true
	}
	inner, hashed := hashedCall(tok)
	return hashed && (inner == "..." || looksLikeCall(inner))
}

// directedTo reads what one decoded line says TO ourCall. For a plain line it is
// parseMessage, unchanged. For a combined message it is the one half addressed
// to us, and the zero message when neither is: never the first half by default.
//
// It is the single reader for every place that asks "what did the station we
// are working say to us": a ladder's advance, its diagnostic "heard" line, and
// the hold after a roger. They must agree, or a line could advance an exchange
// that its own diagnostics say was never heard. The caller still checks the
// sender against the station it is working.
//
// parseMessage itself is deliberately NOT taught the combined line: it has many
// callers that mean "a line with one sender and one addressee" (the answerer
// collectors, Field Day), and a combined line must never become a caller or a
// Field Day exchange through it.
func directedTo(text, ourCall string) message {
	c, ok := parseCombined(text)
	if !ok {
		return parseMessage(text)
	}
	switch ourCall {
	case c.roger.to:
		return c.roger
	case c.report.to:
		return c.report
	}
	return message{}
}

// transmitterOf returns the station that transmitted a decoded line, or "" when
// the line names none that is resolved: a plain message's sender, a CQ's
// caller, a combined message's resolved sender.
func transmitterOf(text string) string {
	if c, ok := parseCombined(text); ok {
		return c.sender
	}
	return parseMessage(text).from
}
