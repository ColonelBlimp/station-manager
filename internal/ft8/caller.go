package ft8

import "strings"

// FT8 caller-side sequencing (ADR 0033) — the mirror of the answer-a-CQ resolver in
// sequence.go. Here WE called CQ and a station answered; this drives the per-contact
// ladder from our reply onward: we send a report, they roger with their report, we
// send RR73, and the QSO logs (e4).
//
// The CQ call itself and *which* answerer to work (ADR 0033's auto_first vs
// operator_pick config knob) are sequencer-level policy, NOT this resolver: a
// CallerExchange begins once an answerer has been chosen, so TheirCall, their grid,
// and our SNR of their answering signal are all known at construction. Like Exchange,
// methods return a new value rather than mutating, so the ladder stays pure and
// unit-testable without a transmitter. The message model (parseMessage, formatReport,
// the report fields) is shared with the answerer side.

// callerState is the rung of the caller-side ladder, named for what we transmit.
type callerState int

const (
	cqReporting callerState = iota // sending <them> <us> <report>; waiting for their R-report
	cqRogering                     // sending <them> <us> RR73;      QSO logs after this slot
	cqDone                         // RR73 sent; nothing more to transmit
)

// label is the lower-case wire/display name for the rung.
func (s callerState) label() string {
	switch s {
	case cqReporting:
		return "reporting"
	case cqRogering:
		return "rogering"
	default:
		return "done"
	}
}

// CallerExchange is the state of one contact we are working after it answered our CQ.
// It mirrors Exchange (the answerer side) for the calling role: the ladder is just two
// of our transmits — the report, then RR73 — because calling CQ and picking the
// answerer happen before this (sequencer policy, ADR 0033).
type CallerExchange struct {
	OurCall   string
	TheirCall string
	TheirGrid string // from their answer (<us> <them> <grid>), kept for the logged QSO

	State callerState

	// SendSnr is OUR measured SNR of their answering signal — the report we send at
	// cqReporting and log as RST_SENT. Known at construction (their answer is what
	// started this exchange), so HasSendSnr is always true.
	SendSnr    int
	HasSendSnr bool
	// RcvdReport is the report THEY sent us (their R<report> at cqReporting), logged
	// as RST_RCVD. Absent if they rogered with a bare RRR/RR73.
	RcvdReport    int
	HasRcvdReport bool
}

// NewCallerExchange begins working a station that answered our CQ. theirCall and
// theirGrid come from their answer (<us> <them> <grid>); sendSnr is our SNR of that
// answering signal — the report we send back. The exchange starts at cqReporting, so
// the first TxMessage is our report. Callsigns are upper-cased and the grid trimmed to
// its 4-char Maidenhead field to match decoded text (the grid is logged, not
// transmitted, but kept consistent with NewExchange).
func NewCallerExchange(ourCall, theirCall, theirGrid string, sendSnr int) CallerExchange {
	grid := strings.ToUpper(strings.TrimSpace(theirGrid))
	if len(grid) > 4 {
		grid = grid[:4]
	}
	return CallerExchange{
		OurCall:   strings.ToUpper(strings.TrimSpace(ourCall)),
		TheirCall: strings.ToUpper(strings.TrimSpace(theirCall)),
		TheirGrid: grid,
		State:     cqReporting,
		// Clamped here, not just when the message is formatted: the report we LOG
		// must be the report we SEND. sendSnr reaches this from the client on the
		// work-a-caller path, where nothing bounds it (see clampReport).
		SendSnr:    clampReport(sendSnr),
		HasSendSnr: true,
	}
}

// TxMessage returns the standard FT8 message to transmit in the current state and
// whether there is anything to send (false once done). Always a directed
// "<them> <us> <token>" message, encodable by EncodeStandardMessage. The report is
// bare (no R prefix): the caller reports first, having nothing yet to roger.
func (e CallerExchange) TxMessage() (string, bool) {
	switch e.State {
	case cqReporting:
		return e.TheirCall + " " + e.OurCall + " " + formatReport(e.SendSnr), true
	case cqRogering:
		return e.TheirCall + " " + e.OurCall + " RR73", true
	default:
		return "", false
	}
}

// Advance applies a decoded message to the exchange. A standard message addressed to
// us from TheirCall that advances the current rung returns the moved-on exchange and
// true; anything else (addressed elsewhere, the wrong token for this rung, a repeated
// answer, or unparseable) returns it unchanged and false — the repeat / off-ramp case.
//
// No SNR parameter (unlike Exchange.Advance): our report was measured from their answer
// at construction, so the caller never reads a fresh decode's SNR.
func (e CallerExchange) Advance(text string) (CallerExchange, bool) {
	m := parseMessage(text)
	if m.to != e.OurCall || m.from != e.TheirCall {
		return e, false
	}
	if e.State == cqReporting {
		// They rogered our report. The standard answer carries their report of us
		// (R<report> → msgRReport); a bare roger (RRR / RR73 → msgRoger) also advances
		// us, just without an RST_RCVD to log.
		switch m.kind {
		case msgRReport:
			e.RcvdReport, e.HasRcvdReport = m.report, true
			e.State = cqRogering
			return e, true
		case msgRoger:
			e.State = cqRogering
			return e, true
		default:
			// Any other kind (a re-sent grid, our own echo, plain band traffic)
			// is not a roger — don't advance; fall through to the no-advance return.
		}
	}
	// cqRogering completes on OUR transmit (Sent); cqDone is terminal. A decode in
	// either state is just more band traffic.
	return e, false
}

// Sent advances the exchange after we transmit the current state's message. The only
// rung whose completion is our own transmission is the final RR73, which moves
// cqRogering → cqDone — the point the QSO is logged (e4). The caller's contact is
// complete once RR73 is sent (both reports exchanged and rogered); the partner's
// closing 73 is courtesy and not awaited.
func (e CallerExchange) Sent() CallerExchange {
	if e.State == cqRogering {
		e.State = cqDone
	}
	return e
}

// Done reports whether the exchange is complete (our RR73 transmitted) and ready to log.
func (e CallerExchange) Done() bool { return e.State == cqDone }
