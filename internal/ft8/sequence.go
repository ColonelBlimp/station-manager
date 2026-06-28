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

// formatReport renders an SNR as a 2-digit signed FT8 report ("-13", "+04"),
// clamped to the [-50, 49] range packReportToken accepts so the resolver never
// emits a message EncodeStandardMessage would reject.
func formatReport(snr int) string {
	if snr < -50 {
		snr = -50
	}
	if snr > 49 {
		snr = 49
	}
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
	msgOther      msgKind = iota
	msgCQ                 // CQ [mod...] <call> [grid]
	msgGrid               // <to> <from> <grid4>     — a reply carrying a grid
	msgReport             // <to> <from> <±report>   — a signal report (rung 2)
	msgRReport            // <to> <from> R<±report>  — a rogered report (rung 3)
	msgRoger              // <to> <from> RRR | RR73  — rogered (RR73 also says 73)
	msg73                 // <to> <from> 73
	msgFdExchange         // <to> <from> [R] <class> <section> — ARRL Field Day exchange
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
	// section carry the exchange of an msgFdExchange (e.g. "2A" / "EMA").
	fd      bool
	class   string
	section string
}

// SpotFrom extracts a PSK Reporter reception spot — the TRANSMITTING station's
// callsign and (when the message carries it) grid — from a decoded FT8 line.
// ok is false for lines with no resolvable sender (hashed `<...>`, free text, bare
// modifiers). The sender is the message's `from`: for a CQ it's the caller (grid
// included); for a directed message it's the second call (grid only on a grid
// reply). Reuses parseMessage so the spot view matches the sequencer's.
func SpotFrom(text string) (call, grid string, ok bool) {
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
		m.kind = msgRoger
	case tok == "73":
		m.kind = msg73
	// ARRL Field Day exchange. The R-variant "<to> <from> R <class> <section>" is what
	// an answerer receives (rung 2); the bare "<to> <from> <class> <section>" is the
	// opening reply (parsed for symmetry / the future caller side). The section is
	// confirmed against go-ft8's canonical list so a real report/grid can't be misread
	// as an exchange.
	case tok == "R" && len(toks) >= 5 && looksLikeFdClass(toks[3]) && goft8.ValidARRLFieldDaySection(toks[4]):
		m.kind, m.class, m.section = msgFdExchange, toks[3], toks[4]
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
	m := parseMessage(text)
	if m.to != e.OurCall || m.from != e.TheirCall {
		return e, false
	}
	switch e.State {
	case txCalling:
		// They answered us with a report → roger it next slot.
		if m.kind == msgReport || m.kind == msgRReport {
			e.RcvdReport, e.HasRcvdReport = m.report, true
			e.SendSnr, e.HasSendSnr = snr, true
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
func (e Exchange) Done() bool { return e.State == txDone }
