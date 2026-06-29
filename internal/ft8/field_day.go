package ft8

import "strings"

// ARRL Field Day — the answerer (search & pounce) side. We answer another station's
// "CQ FD" and run the FD exchange to RR73, logging their class + section. This is the
// FD twin of Exchange (sequence.go): a pure, value-returning state machine the daemon
// sequencer drives one slot at a time. We do NOT call CQ FD (no caller side).
//
// The ladder, us answering THEIRCALL's CQ FD:
//
//	fdCalling : <them> <us> <ourClass> <ourSection>   (e.g. "K1ABC 7Q5MLV 1D DX")
//	            ── receive <us> <them> R <class> <section> ──>
//	fdRogering: <them> <us> RR73                       (QSO logs after this slot)
//	fdDone
//
// Both transmitted messages encode via go-ft8's EncodeStandardMessage (its packer
// handles FD exchange messages and the standard RR73), so the encode/modulate seam is
// the standard one — see TestFieldDay_RoundTrip for the offline proof.

// looksLikeFdClass reports whether s has the shape of a Field Day class: a transmitter
// count (1–2 digits, 0 allowed on the wire) followed by a category letter A–F, e.g.
// "2A", "1D", "33A". Loose by design — it only gates the parser; the section it pairs
// with is confirmed against go-ft8's canonical list.
func looksLikeFdClass(s string) bool {
	if len(s) < 2 || len(s) > 3 {
		return false
	}
	if c := s[len(s)-1]; c < 'A' || c > 'F' {
		return false
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// fdState is the rung of the answer-a-CQ-FD ladder, named for what we transmit.
type fdState int

const (
	fdCalling  fdState = iota // sending <them> <us> <class> <section>; waiting for their R-exchange
	fdRogering                // sending <them> <us> RR73; QSO logs after this slot
	fdDone                    // RR73 sent; nothing more to transmit
)

func (s fdState) label() string {
	switch s {
	case fdCalling:
		return "calling"
	case fdRogering:
		return "rogering"
	default:
		return "done"
	}
}

// FdExchange is the state of one answer-a-CQ-FD contact. Like Exchange, its resolver
// methods return a new value rather than mutating, so the rung logic stays pure.
type FdExchange struct {
	OurCall    string
	OurClass   string
	OurSection string
	TheirCall  string
	TheirGrid  string // from their CQ FD — for bearing/enrichment, not the exchange

	State fdState

	// Their exchange, captured when they send R + class + section (rung 2); these are
	// what the logged QSO records (ADIF CLASS / ARRL_SECT).
	TheirClass   string
	TheirSection string
	HasTheirExch bool

	// SendSnr is OUR measured SNR of their signal (from the decode the operator clicked).
	// FD doesn't exchange a report on the air, but we still log it as RST_SENT — like a
	// standard FT8 QSO — so the QSO isn't reportless. HasSendSnr is true once set.
	SendSnr    int
	HasSendSnr bool
}

// NewFdExchange starts answering a CQ FD. ourClass/ourSection are the operator's
// configured Field Day identity (ft8.field_day); theirGrid comes from their CQ (kept
// for the log, not the exchange). Values are upper-cased to match decoded text and the
// grid trimmed to 4 chars (FT8 standard messages carry only the 4-char field).
func NewFdExchange(ourCall, ourClass, ourSection, theirCall, theirGrid string, sendSnr int) FdExchange {
	up := func(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
	grid := up(theirGrid)
	if len(grid) > 4 {
		grid = grid[:4]
	}
	return FdExchange{
		OurCall:    up(ourCall),
		OurClass:   up(ourClass),
		OurSection: up(ourSection),
		TheirCall:  up(theirCall),
		TheirGrid:  grid,
		State:      fdCalling,
		SendSnr:    sendSnr,
		HasSendSnr: true,
	}
}

// TxMessage is the message to transmit in the current rung, or ok=false when there's
// nothing to send (fdDone).
func (e FdExchange) TxMessage() (string, bool) {
	switch e.State {
	case fdCalling:
		return e.TheirCall + " " + e.OurCall + " " + e.OurClass + " " + e.OurSection, true
	case fdRogering:
		return e.TheirCall + " " + e.OurCall + " RR73", true
	default:
		return "", false
	}
}

// Advance consumes a received line; if it's their R-exchange directed at us, it
// captures their class/section and moves to fdRogering. Returns the new exchange and
// whether it advanced.
func (e FdExchange) Advance(text string) (FdExchange, bool) {
	m := parseMessage(text)
	if m.to != e.OurCall || m.from != e.TheirCall {
		return e, false
	}
	if e.State == fdCalling && m.kind == msgFdExchange {
		e.TheirClass, e.TheirSection, e.HasTheirExch = m.class, m.section, true
		e.State = fdRogering
		return e, true
	}
	return e, false
}

// Sent advances the TX-only final rung: after RR73 leaves the radio the contact is
// complete (fdRogering → fdDone), and the QSO logs.
func (e FdExchange) Sent() FdExchange {
	if e.State == fdRogering {
		e.State = fdDone
	}
	return e
}

// --- ARRL Field Day, the WORKED side (a station calls US with an FD exchange) ------
//
// As a sought-after station we get called directly: "<ourCall> <theirCall> <class>
// <section>" (e.g. "7Q5MLV K7IOC 1D WWA"). The operator picks one; we reply with R +
// OUR class/section, they RR73, we RR73 and log THEIR class/section. The FD twin of
// CallerExchange's working role (report → RR73), with class/section in place of a report.
//
//	fdwReporting: <them> <us> R <ourClass> <ourSection>   (e.g. "K7IOC 7Q5MLV R 1D DX")
//	             ── receive <us> <them> RR73 ──>
//	fdwRogering : <them> <us> RR73                         (QSO logs after this slot)

type fdwState int

const (
	fdwReporting fdwState = iota // send <them> <us> R <class> <section>; wait for their roger
	fdwRogering                  // send <them> <us> RR73; QSO logs after this slot
	fdwDone                      // RR73 sent; nothing more to transmit
)

func (s fdwState) label() string {
	switch s {
	case fdwReporting:
		return "reporting"
	case fdwRogering:
		return "rogering"
	default:
		return "done"
	}
}

// FdWorkExchange is the state of working a station that called US with a Field Day
// exchange. Their class+section come from the call we picked (and are logged); our
// class+section come from config. Pure + value-returning like the other ladders.
type FdWorkExchange struct {
	OurCall      string
	OurClass     string
	OurSection   string
	TheirCall    string
	TheirGrid    string
	TheirClass   string // their exchange, from the picked call — logged (ADIF CLASS)
	TheirSection string // their exchange section — logged (ADIF ARRL_SECT)

	State fdwState

	// SendSnr is OUR measured SNR of their signal (the decode the operator clicked) —
	// logged as RST_SENT, since FD exchanges no report on the air. Always set here (the
	// caller's signal is what we picked), so HasSendSnr is true.
	SendSnr    int
	HasSendSnr bool
}

// NewFdWorkExchange begins working a caller's FD call. ourClass/ourSection are the
// operator's configured identity; theirClass/theirSection are parsed from the call we
// picked. Values are upper-cased; the grid trimmed to 4 chars.
func NewFdWorkExchange(ourCall, ourClass, ourSection, theirCall, theirGrid, theirClass, theirSection string, sendSnr int) FdWorkExchange {
	up := func(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
	grid := up(theirGrid)
	if len(grid) > 4 {
		grid = grid[:4]
	}
	return FdWorkExchange{
		OurCall:      up(ourCall),
		OurClass:     up(ourClass),
		OurSection:   up(ourSection),
		TheirCall:    up(theirCall),
		TheirGrid:    grid,
		TheirClass:   up(theirClass),
		TheirSection: up(theirSection),
		State:        fdwReporting,
		SendSnr:      sendSnr,
		HasSendSnr:   true,
	}
}

// TxMessage is the message for the current rung, or ok=false when done.
func (e FdWorkExchange) TxMessage() (string, bool) {
	switch e.State {
	case fdwReporting:
		return e.TheirCall + " " + e.OurCall + " R " + e.OurClass + " " + e.OurSection, true
	case fdwRogering:
		return e.TheirCall + " " + e.OurCall + " RR73", true
	default:
		return "", false
	}
}

// Advance moves to rogering when the worked station rogers our reply (RR73/RRR directed
// to us). They already sent their exchange in the call we picked, so a bare roger is all
// we await.
func (e FdWorkExchange) Advance(text string) (FdWorkExchange, bool) {
	m := parseMessage(text)
	if m.to != e.OurCall || m.from != e.TheirCall {
		return e, false
	}
	if e.State == fdwReporting && m.kind == msgRoger {
		e.State = fdwRogering
		return e, true
	}
	return e, false
}

// Sent advances the TX-only final rung: after RR73 leaves the radio the contact is
// complete (fdwRogering → fdwDone) and the QSO logs.
func (e FdWorkExchange) Sent() FdWorkExchange {
	if e.State == fdwRogering {
		e.State = fdwDone
	}
	return e
}
