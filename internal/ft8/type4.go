package ft8

import (
	"strings"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
)

// Reduced type-4 (nonstandard/compound call) QSO ladder — ADR 0048.
//
// A nonstandard callsign (compound "PJ4/NA2AA", odd suffix "/D", "/MM", special-event
// "YW18FIFA") does not compress to a 28-bit standard call, so it cannot ride a standard
// FT8 message. It needs a type-4 message (i3=4), which spells the nonstandard call in
// ~58 bits and reduces the OTHER call to a 12-bit hash rendered "<...>". After the type
// tag that leaves room only for {blank, RRR, RR73, 73} — there is NO wire form for a
// grid or a signal report (QEX Jul/Aug 2020, §type-4). So the standard grid→report→73
// ladder is unencodable for a type-4 partner, and SM needs this reduced ladder to work
// one at all.
//
// These are the pure, value-returning state machines (twins of Exchange / FdExchange in
// sequence.go / field_day.go); the daemon sequencer drives them one slot at a time. Two
// directions, mirroring Field Day:
//
//	answer-a-CQ (T4Exchange):     we answer their "CQ <them>"
//	  t4Calling    : TX "<them> <us>"      (on air "<them> <...>", our call hashed)
//	                 ── receive their roger "<...> <them> RR73" ──>
//	  t4Confirming : TX "<them> <us> 73"   (on air "<them> <...> 73"; QSO logs after this slot)
//	  t4Done
//
//	work-a-caller (T4WorkExchange): a nonstandard station calls us "<us> <them>"
//	  t4wRogering  : TX "<them> <us> RR73"  (on air "<them> <...> RR73"; QSO logs after this slot)
//	  t4wDone
//
// The reduced ladder is bare-calls → RR73 → 73, alternating between the stations; each
// side transmits its part. There is no report on the wire, so — like Field Day — we log
// OUR measured SNR as RST_SENT and leave RST_RCVD blank.
//
// Inbound matching is on the SPELLED nonstandard partner (from == TheirCall). Our own
// standard call is always hashed to "<...>" in a type-4 exchange, so the addressed call
// cannot be verified exactly; a hashed "<...>" in the "to" slot is treated as
// presumed-us while a single exchange is active (ADR 0048 — the bounded, documented
// false-match risk). Every transmitted message encodes via go-ft8's EncodeStandardMessage
// (which packs type-4) — see TestType4_RoundTrip for the offline RF-safety proof.

// isHashedCall reports whether a token is a hashed callsign as go-ft8 renders it — an
// angle-bracketed placeholder ("<...>", or "<CALL>" if a peer resolved the hash). In a
// type-4 exchange we take part in, our own standard call is always hashed, so this marks
// the "to" token that stands in for us. The standard looksLikeCall deliberately rejects
// these tokens (sequence.go); type-4 accepts them.
func isHashedCall(t string) bool {
	return len(t) >= 2 && t[0] == '<' && t[len(t)-1] == '>'
}

// ft8Type4I3 is the FT8 message-type (i3) code for a type-4 (nonstandard/compound
// call) message. i3 is the 3-bit field at bits 74..76 of the packed 77-bit payload
// (WSJT-X FT8 spec, MSB first).
const ft8Type4I3 = 4

// encodesAsType4 reports whether msg packs to a GENUINE type-4 message — one whose
// nonstandard/compound callsign actually requires the reduced type-4 ladder.
// go-ft8's EncodeStandardMessage tries the type-4 packer first but falls through to
// the standard packer (i3=1/2) when both calls are standard, so a pair like
// "K1ABC 7Q5MLV RR73" encodes fine — as type 1, NOT type 4. The type-4 entry points
// must reject those: driving a standard message through the reduced ladder (an
// immediate RR73 with no valid type-4 exchange) is not a real QSO. Authoritative
// check — encode, then read i3 straight from the packed bits (whatever go-ft8 chose).
func encodesAsType4(msg string) bool {
	enc, err := goft8.EncodeStandardMessage(msg)
	if err != nil {
		return false
	}
	i3 := int(enc.Bits77[74])<<2 | int(enc.Bits77[75])<<1 | int(enc.Bits77[76])
	return i3 == ft8Type4I3
}

// type4msg is a type-4 directed line reduced to the fields the reduced ladder needs. It
// is deliberately separate from parseMessage's message model: parseMessage drops hashed
// tokens (they aren't callsigns), which is correct for the standard ladders but would
// discard exactly the replies this ladder must match.
type type4msg struct {
	to   string  // addressed call — may be the hashed "<...>" (us) or a spelled call
	from string  // the SPELLED nonstandard partner (never hashed in our own exchange)
	kind msgKind // msgOther (bare call-to-call), msgRoger (RRR/RR73), or msg73
}

// parseType4 reduces a decoded type-4 directed line to type4msg. Unrecognised or
// non-type-4 lines parse to a zero value (kind msgOther, empty from) which the resolvers
// ignore. Additive: parseMessage / looksLikeCall are untouched.
func parseType4(text string) type4msg {
	toks := strings.Fields(strings.ToUpper(strings.TrimSpace(text)))
	if len(toks) < 2 {
		return type4msg{}
	}
	// The addressed call may be hashed (our call, hashed by the partner) or spelled.
	if !isHashedCall(toks[0]) && !looksLikeCall(toks[0]) {
		return type4msg{}
	}
	// The sender is the spelled nonstandard partner — always a real call in an exchange
	// we are part of, so a hashed "from" is not one of our replies.
	if !looksLikeCall(toks[1]) {
		return type4msg{}
	}
	m := type4msg{to: toks[0], from: toks[1]}
	if len(toks) >= 3 {
		switch toks[2] {
		case "RRR", "RR73":
			m.kind = msgRoger
		case "73":
			m.kind = msg73
		}
	}
	return m
}

// --- answer-a-CQ: their call is nonstandard, we answer their CQ ---------------------

// t4State is the rung of the answer-a-CQ type-4 ladder, named for what we transmit.
type t4State int

const (
	t4Calling    t4State = iota // sending "<them> <us>"; waiting for their roger
	t4Confirming                // sending "<them> <us> 73"; QSO logs after this slot
	t4Done                      // 73 sent; nothing more to transmit
)

func (s t4State) label() string {
	switch s {
	case t4Calling:
		return "calling"
	case t4Confirming:
		return "confirming"
	default:
		return "done"
	}
}

// T4Exchange is the state of one answer-a-CQ contact with a nonstandard partner. Like
// Exchange, its resolver methods return a new value rather than mutating, keeping the
// rung logic pure and testable without a transmitter.
type T4Exchange struct {
	OurCall   string
	TheirCall string // the SPELLED nonstandard partner (e.g. "PJ4/NA2AA")
	TheirGrid string // usually empty — a type-4 CQ carries no grid; kept for symmetry

	State t4State

	// SendSnr is OUR measured SNR of their CQ (the decode the operator clicked). Type-4
	// exchanges no report on the air, but we still log it as RST_SENT so the row isn't
	// reportless — as Field Day does. Always set here, so HasSendSnr is true.
	SendSnr    int
	HasSendSnr bool
}

// NewT4Exchange starts answering a nonstandard station's CQ. ourCall is our (standard)
// call; theirCall is the spelled nonstandard partner; theirGrid is kept for the log if
// present. Values are upper-cased to match decoded text.
func NewT4Exchange(ourCall, theirCall, theirGrid string, sendSnr int) T4Exchange {
	up := func(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
	grid := up(theirGrid)
	if len(grid) > 4 {
		grid = grid[:4]
	}
	return T4Exchange{
		OurCall:    up(ourCall),
		TheirCall:  up(theirCall),
		TheirGrid:  grid,
		State:      t4Calling,
		SendSnr:    sendSnr,
		HasSendSnr: true,
	}
}

// presumedUs reports whether an addressed call in a decode is us — our exact call, or a
// hashed placeholder (which is how the partner addresses us, since our call is hashed on
// the wire in a type-4 exchange).
func (e T4Exchange) presumedUs(to string) bool {
	return to == e.OurCall || isHashedCall(to)
}

// TxMessage is the message to transmit in the current rung, or ok=false when done.
func (e T4Exchange) TxMessage() (string, bool) {
	switch e.State {
	case t4Calling:
		return e.TheirCall + " " + e.OurCall, true // bare opening; no grid in type-4
	case t4Confirming:
		return e.TheirCall + " " + e.OurCall + " 73", true
	default:
		return "", false
	}
}

// Advance consumes a received line; if it is the partner's roger (or a direct 73)
// addressed to us, it moves t4Calling → t4Confirming. Returns the new exchange and
// whether it advanced.
func (e T4Exchange) Advance(text string) (T4Exchange, bool) {
	m := parseType4(text)
	if m.from != e.TheirCall || !e.presumedUs(m.to) {
		return e, false
	}
	if e.State == t4Calling && (m.kind == msgRoger || m.kind == msg73) {
		e.State = t4Confirming
		return e, true
	}
	return e, false
}

// Sent advances the TX-only final rung: after our 73 leaves the radio the contact is
// complete (t4Confirming → t4Done) and the QSO logs.
func (e T4Exchange) Sent() T4Exchange {
	if e.State == t4Confirming {
		e.State = t4Done
	}
	return e
}

// Done reports whether the exchange is complete (our 73 transmitted) and ready to log.
func (e T4Exchange) Done() bool { return e.State == t4Done }

// --- work-a-caller: a nonstandard station calls US -----------------------------------

// t4wState is the rung of the work-a-caller type-4 ladder. There is a single transmit —
// RR73 — because no report is exchanged; the QSO logs after it, as the caller/FD-work
// ladders log after their final RR73.
type t4wState int

const (
	t4wRogering t4wState = iota // sending "<them> <us> RR73"; QSO logs after this slot
	t4wDone                     // RR73 sent; nothing more to transmit
)

func (s t4wState) label() string {
	if s == t4wRogering {
		return "rogering"
	}
	return "done"
}

// T4WorkExchange is the state of working a nonstandard station that called US. Their
// (spelled) call and our SNR of it come from the decode the operator picked. Pure +
// value-returning like the other ladders.
type T4WorkExchange struct {
	OurCall   string
	TheirCall string // the SPELLED nonstandard partner
	TheirGrid string // usually empty — a type-4 call to us carries no grid

	State t4wState

	// SendSnr is OUR measured SNR of their calling signal (the decode the operator
	// clicked) — logged as RST_SENT, since type-4 exchanges no report on the air. Always
	// set here, so HasSendSnr is true.
	SendSnr    int
	HasSendSnr bool
}

// NewT4WorkExchange begins working a nonstandard caller. ourCall is our (standard) call;
// theirCall is the spelled nonstandard partner; sendSnr is our SNR of their calling
// signal. Values are upper-cased.
func NewT4WorkExchange(ourCall, theirCall, theirGrid string, sendSnr int) T4WorkExchange {
	up := func(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
	grid := up(theirGrid)
	if len(grid) > 4 {
		grid = grid[:4]
	}
	return T4WorkExchange{
		OurCall:    up(ourCall),
		TheirCall:  up(theirCall),
		TheirGrid:  grid,
		State:      t4wRogering,
		SendSnr:    sendSnr,
		HasSendSnr: true,
	}
}

// TxMessage is the message for the current rung, or ok=false when done.
func (e T4WorkExchange) TxMessage() (string, bool) {
	if e.State == t4wRogering {
		return e.TheirCall + " " + e.OurCall + " RR73", true
	}
	return "", false
}

// Advance is a no-op that never advances: the single RR73 rung completes on OUR own
// transmit (Sent), so a received decode — even the partner's closing 73 — changes
// nothing. Present for symmetry with the other exchanges the sequencer drives.
func (e T4WorkExchange) Advance(string) (T4WorkExchange, bool) {
	return e, false
}

// Sent advances the TX-only rung: after RR73 leaves the radio the contact is complete
// (t4wRogering → t4wDone) and the QSO logs.
func (e T4WorkExchange) Sent() T4WorkExchange {
	if e.State == t4wRogering {
		e.State = t4wDone
	}
	return e
}

// Done reports whether the exchange is complete (our RR73 transmitted) and ready to log.
func (e T4WorkExchange) Done() bool { return e.State == t4wDone }
