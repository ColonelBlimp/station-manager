package ft8

import (
	stderrors "errors"
	"sync"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// FT8 manual sequencer (ADR 0031): operator-initiated, auto-advancing. The
// operator clicks a CQ to answer (StartQso) with TX armed; this walks the CQ→73
// ladder via the e2 Exchange — transmitting each rung in the slot opposite the
// worked station, advancing on the decode directed to us — and finishes when the
// 73 is sent (e4 will log it). The operator intervenes only to Abandon.
//
// Timing (ADR 0032): we answer in the slot immediately AFTER the worked station's,
// which we only learn after decoding their slot (~0.7–1.6 s into our slot). The
// rung is sent on the synchronised timebase (seqTransmit → TransmitCurrentSlot),
// in the parity opposite theirs — NOT at the next boundary, which would be their
// parity and collide. Because the decode lands after the slot's nominal +0.5 s
// start, the controller drops the elapsed head and transmits the synchronised
// remainder (truncate-don't-shift); the receiver re-syncs on the Costas arrays.
// OnSlot is the single trigger: the decode loop calls it once per completed slot.

// defaultSeqMaxRepeats caps consecutive unanswered transmissions of a rung before
// the QSO is abandoned (ADR 0031 off-ramp). ~6 of our slots ≈ 90 s of calling. The
// operator can override it via ft8.tx.max_repeats (clamped ≤ Ft8MaxRepeatsCeiling);
// this is the fallback when unset.
const defaultSeqMaxRepeats = types.DefaultFt8MaxRepeats

// txLateWindowSec is the latest into our slot we will START a rung. Past this,
// too few symbols / Costas sync words survive the head-truncation (ADR 0032) for
// a reliable decode, so we skip and retry on our next cycle. The truncation
// itself lives in the TxController. Package var so tests can dial it.
var txLateWindowSec = 4.5

// Sequencer sentinels (mapped to HTTP by the api layer).
var (
	// ErrQsoInProgress: a StartQso/StartCallCq while a session is already active.
	ErrQsoInProgress = stderrors.New("ft8: a QSO is already in progress")
	// ErrNoOffset: started without a TX offset.
	ErrNoOffset = stderrors.New("ft8: no TX offset selected")
	// ErrNoCall: StartCallCq without an operator callsign.
	ErrNoCall = stderrors.New("ft8: no operator callsign configured")
)

// seqMode is the active sequencer session: idle, answering a CQ (ex drives), or
// calling CQ (caller drives, ADR 0033). Only one session is active at a time.
type seqMode int

const (
	seqIdle seqMode = iota
	seqAnswering
	seqCalling
	// seqWorking: working a station that is calling US (it sent a grid directed to
	// our call), picked by the operator from the pile-up (ADR 0033 "work a caller").
	// Reuses the caller ladder (we report first), but unlike seqCalling it has no
	// CQ phase and on completion/off-ramp it goes idle rather than resuming CQ.
	seqWorking
)

// QsoStatus roles (the `ft8-qso` SSE Role field) — which side of the contact we
// are, so the SPA renders the matching ladder.
const (
	roleAnswerer = "answerer"
	roleCaller   = "caller"
	// roleWorker: working a station that called US, picked from the pile-up (ADR 0033
	// "work a caller"). Caller-style exchange (we report first) but with NO CQ phase,
	// so the SPA renders a dedicated ladder (no leading CQ row) — distinct from
	// roleCaller, whose ladder opens with our CQ.
	roleWorker = "worker"
)

// QsoStatus is the `ft8-qso` SSE payload + the sequencer's exposed state: the role
// (answerer/caller), the active contact's rung, the worked call, the message we'll
// send next, and the unanswered-repeat count. Active=false means idle (no session).
type QsoStatus struct {
	Active    bool   `json:"active"`
	Role      string `json:"role,omitempty"` // "answerer" | "caller" | "worker"; empty when idle
	TheirCall string `json:"their_call,omitempty"`
	// TheirGrid — the worked station's 4-char grid, for the ladder's opening row (so it
	// shows the real grid, not a "<GRID>" placeholder). Empty until known.
	TheirGrid string `json:"their_grid,omitempty"`
	// State — answerer: calling|reporting|confirming; caller: calling-cq|reporting|rogering.
	State       string `json:"state,omitempty"`
	NextMessage string `json:"next_message,omitempty"`
	Repeats     int    `json:"repeats,omitempty"`
	// MaxRepeats — the unanswered-rung repeat cap, set ONLY while the current rung is
	// actually subject to it (an answerer pre-73, or a caller working an answerer
	// pre-RR73). Zero (omitted) on the uncapped rungs (calling CQ) and the one-shot
	// final rungs (73/RR73), so the SPA shows the "calls left" countdown iff this is
	// >0 without re-deriving the cap-vs-one-shot rule. Remaining = MaxRepeats-Repeats.
	MaxRepeats int `json:"max_repeats,omitempty"`
	// OurReport / TheirReport — the signal reports exchanged, formatted exactly as
	// they appear on the air (e.g. "-12", "+04"); empty until known. OurReport is
	// the report WE send (our SNR of their signal); TheirReport is the one THEY sent
	// us. The SPA fills the ladder's <RST> placeholders from these.
	OurReport   string `json:"our_report,omitempty"`
	TheirReport string `json:"their_report,omitempty"`
}

// CompletedQso is captured when an exchange finishes (73 sent) — the data e4 maps
// to a types.Qso (BuildQso) and submits via qsoservice.
type CompletedQso struct {
	TheirCall      string
	TheirGrid      string
	OurReport      int // the report WE sent (our SNR of their signal)
	HasOurReport   bool
	TheirReport    int // the report THEY sent us
	HasTheirReport bool
	OffsetHz       float64
	// DialFreqMHz is the rig's dial frequency at QSO start (from the SPA, which
	// reads it from the live rig state). The logged QSO frequency is this plus
	// the audio offset; the band is derived from the sum.
	DialFreqMHz float64
	// AntPath is the operator's antenna-path choice for this contact, "S" (short)
	// or "L" (long), used only to annotate the logged QSO (ADIF ANT_PATH + the
	// short/long bearing+distance). It is NOT part of the on-air exchange — FT8
	// messages carry no path info. The sequencer doesn't set it; the Service
	// stamps the current exchange path onto the CompletedQso just before handing
	// it to the logger (see Service.onComplete). Empty = unset (defaults to short).
	AntPath string
}

// Sequencer owns the (single) active session — answering a CQ or calling CQ, never
// both. Its dependencies are injected so it stays decoupled from the Service and
// unit-testable: transmit sends a rung (Service.seqTransmit — current-slot late-dt),
// publish fans out QsoStatus on the ft8-qso SSE, and onComplete (optional, e4)
// consumes a finished QSO. The caller-side flow (ADR 0033) lives in
// caller_sequencer.go but shares this struct's deps + the OnSlot entry point.
type Sequencer struct {
	mu   sync.Mutex
	mode seqMode

	// Answering a CQ (seqAnswering): ex is the active exchange; theirGrid is the
	// worked station's grid from the CQ we answered.
	ex        *Exchange
	theirGrid string

	// Calling CQ (seqCalling, ADR 0033): caller is nil while still calling (phase 1),
	// set once an answerer is chosen (phase 2). cqMessage is repeated each of our
	// slots until answered; answerMode is auto_first | operator_pick.
	caller     *CallerExchange
	ourCall    string
	ourGrid    string
	cqMessage  string
	answerMode string

	// Shared by both modes. theirPeriod is the parity of the slots we PROCESS (the
	// worked station's when answering, or — when calling CQ — the answerers', i.e.
	// opposite our CQ parity); we transmit in the opposite (current) slot.
	theirPeriod string
	offsetHz    float64
	dialFreqMHz float64 // rig dial freq at start, for the logged QSO frequency
	repeats     int
	maxRepeats  int

	// sessionGen is bumped on every session-identity change (StartQso /
	// StartCallCq commit, Abandon). A final-rung onDone closure captures the gen
	// at queue time and acts (logs, mutates state, publishes) ONLY if the gen
	// still matches — so an Abandon/disarm (or any future superseding path) that
	// fires while the final 73/RR73 is still in flight can't let a stale callback
	// log a QSO or publish idle over a newer session (review follow-up M1).
	sessionGen uint64

	// transmit sends a rung. onDone (optional) fires from the transmit goroutine
	// once the transmission finishes — ok=true only on a clean on-air success.
	// The final rung passes a completion closure so the QSO is logged ONLY after
	// the 73/RR73 actually transmitted, never on "queued" (review H1).
	transmit   func(message string, offsetHz float64, onDone func(ok bool)) error
	publish    func(QsoStatus)
	onComplete func(CompletedQso)
	log        logging.Logger
}

func newSequencer(transmit func(string, float64, func(ok bool)) error, publish func(QsoStatus), maxRepeats int, log logging.Logger) *Sequencer {
	if log == nil {
		log = logging.Noop()
	}
	if maxRepeats <= 0 {
		maxRepeats = defaultSeqMaxRepeats
	}
	return &Sequencer{
		maxRepeats: maxRepeats,
		transmit:   transmit,
		publish:    publish,
		log:        log,
	}
}

// StartQso begins answering a CQ. theirSlotUTC is the RFC3339 start of the slot
// the CQ was heard in (it fixes the worked station's parity — we transmit in the
// opposite one). offsetHz is the operator-picked clear offset. Idempotency: only
// one QSO at a time (ErrQsoInProgress).
func (s *Sequencer) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64, now time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	ex := NewExchange(ourCall, ourGrid, theirCall)
	// Validate the opening message encodes BEFORE committing the session (review
	// M1): the CQ parser accepts compound/portable calls (with `/`) the standard
	// encoder rejects, so an unanswerable call would otherwise publish an active
	// ladder that can never produce RF. Fail up front with a clear error.
	if msg, ok := ex.TxMessage(); ok {
		if _, err := goft8.EncodeStandardMessage(msg); err != nil {
			return ErrTxBadMessage
		}
	}

	s.mu.Lock()
	if s.mode != seqIdle {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	s.mode = seqAnswering
	s.sessionGen++
	s.ex = &ex
	s.theirPeriod = SlotRefFromTime(t).Period
	s.theirGrid = theirGrid
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.repeats = 0
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", theirCall).Str("their_period", s.theirPeriod).
		Float64("offset_hz", offsetHz).Msg("ft8 seq: answering CQ")
	s.publish(st)
	// Send the opening call this slot if we're already in our TX window — else the
	// first rung waits for the next qualifying OnSlot (up to a full cycle late).
	s.fireOpening(now)
	return nil
}

// Abandon drops the active session — answering or calling CQ (operator action,
// disarm, or off-ramp). Idempotent.
func (s *Sequencer) Abandon() {
	s.mu.Lock()
	was := s.mode != seqIdle
	s.mode = seqIdle
	s.sessionGen++ // supersede any in-flight final-rung callback (review follow-up M1)
	s.ex = nil
	s.caller = nil
	s.repeats = 0
	s.mu.Unlock()
	if was {
		s.log.InfoWith().Msg("ft8 seq: session abandoned")
		s.publish(QsoStatus{Active: false})
	}
}

// Active reports whether a session (answering or calling CQ) is in progress.
func (s *Sequencer) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode != seqIdle
}

// OnSlot is the per-slot driver, called by the decode loop once each completed slot.
// ref is the slot just decoded; msgs are its decodes; now is wall-clock UTC. It
// dispatches to the active session's handler (answering a CQ, or calling CQ); idle
// is a no-op. Each handler re-validates its mode under s.mu, so the brief unlocked
// read here is safe (a concurrent Abandon at worst makes a handler no-op).
func (s *Sequencer) OnSlot(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	mode := s.mode
	s.mu.Unlock()
	switch mode {
	case seqAnswering:
		s.onSlotAnswering(ref, msgs, now)
	case seqCalling:
		s.onSlotCalling(ref, msgs, now)
	case seqWorking:
		s.onSlotWorking(ref, msgs, now)
	default:
		// seqIdle — no active session; nothing to drive this slot.
	}
}

// onSlotAnswering drives an answer-a-CQ exchange: feed the worked station's decodes
// to Advance, then transmit our rung in the (current) opposite-parity slot, late-dt.
func (s *Sequencer) onSlotAnswering(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	// Decide everything under the lock, then run side effects (transmit, publish,
	// complete) after releasing it — transmit takes the Service's txMu.
	s.mu.Lock()
	if s.ex == nil {
		s.mu.Unlock()
		return
	}
	// The slot we can still transmit in is ref+1 (the current one), whose parity
	// is opposite ref's. Only the worked station's slots set up our reply; a slot
	// of our own parity just decoded our own transmission — nothing to do.
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	// Advance on their decode directed to us (records their report + our SNR).
	// Capture what the worked station said to us this slot and whether it advanced
	// us, so a stalled exchange is diagnosable: did their roger arrive and we fail
	// to parse it, or did they send nothing we decoded?
	var heard string
	advanced := false
	for _, m := range msgs {
		if pm := parseMessage(m.Text); pm.to == s.ex.OurCall && pm.from == s.ex.TheirCall {
			heard = m.Text
		}
		if next, ok := s.ex.Advance(m.Text, m.SNR); ok {
			*s.ex = next
			s.repeats = 0
			advanced = true
			break
		}
	}
	if heard != "" {
		s.log.InfoWith().
			Str("from_worked", heard).
			Bool("advanced", advanced).
			Str("now_rung", s.ex.State.label()).
			Msg("ft8 seq: decode from worked station")
	}

	msg, ok := s.ex.TxMessage()
	if !ok { // exchange already done — shouldn't happen here; clear defensively.
		s.ex = nil
		s.mode = seqIdle
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	rung := s.ex.State.label()

	// Late-window guard: the current (our) slot started at ref.start + SlotDuration.
	// Too late into it and too few symbols survive head-truncation to decode — skip
	// and retry next cycle (ADR 0032).
	curStart, perr := time.Parse(time.RFC3339, ref.StartUTC)
	if perr != nil {
		s.mu.Unlock()
		return
	}
	dt := now.Sub(curStart.Add(SlotDuration)).Seconds()
	if dt < 0 || dt > txLateWindowSec {
		st := s.statusLocked()
		s.mu.Unlock()
		s.publish(st)
		return
	}

	confirming := s.ex.State == txConfirming
	if !confirming {
		// Calling / Reporting wait for the partner; cap the repeats (off-ramp).
		if s.repeats >= s.maxRepeats {
			s.ex = nil
			s.mode = seqIdle
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: no answer after max repeats; abandoning")
			s.publish(QsoStatus{Active: false})
			return
		}
		s.repeats++
	}

	transmit, offset := s.transmit, s.offsetHz
	repeats := s.repeats
	var completed *CompletedQso
	if confirming {
		// Capture the QSO data for logging, but DO NOT clear the exchange or go
		// idle yet (review follow-up M1): the session stays in txConfirming until
		// the 73 actually transmits, so a synchronous ErrTxInFlight can retry next
		// slot, and the operator can't start a new QSO (ErrQsoInProgress) while we
		// are still keying the 73. The report fields are final at txConfirming —
		// Sent() only flips State — so reading the live exchange is correct.
		c := s.completedQsoLocked()
		completed = &c
	}
	gen := s.sessionGen
	st := s.statusLocked()
	onComplete, publish := s.onComplete, s.publish
	s.mu.Unlock()

	// Side effects outside the lock. Log every rung transmit (msg, rung, and how
	// late into our slot it fired) so an on-air failure is diagnosable from the
	// log — a successful send was previously silent.
	s.log.InfoWith().
		Str("msg", msg).
		Str("rung", rung).
		Float64("offset_hz", offset).
		Float64("dt_s", dt).
		Int("repeats", repeats).
		Msg("ft8 seq: transmitting rung")

	// Final-rung completion (review H1 + follow-up M1): the QSO logs ONLY after
	// the 73 truly transmits, and the gen guard means an Abandon/disarm that
	// superseded this session while the 73 was in flight neither logs nor
	// publishes. On success → log + idle + publish idle. On RF failure → leave
	// the exchange in txConfirming so the next slot retries the 73 (don't drop
	// the contact).
	var onDone func(ok bool)
	if completed != nil {
		c := *completed
		onDone = func(ok bool) {
			s.mu.Lock()
			if s.sessionGen != gen { // superseded (abandon/disarm) — stale callback
				s.mu.Unlock()
				return
			}
			if !ok {
				s.mu.Unlock()
				s.log.WarnWith().Str("their_call", c.TheirCall).
					Msg("ft8 seq: final 73 did not transmit; will retry next slot")
				return
			}
			s.ex = nil
			s.mode = seqIdle
			s.mu.Unlock()
			s.log.InfoWith().Str("their_call", c.TheirCall).Msg("ft8 seq: QSO complete (73 sent)")
			if onComplete != nil {
				onComplete(c)
			}
			publish(QsoStatus{Active: false})
		}
	}

	if err := transmit(msg, offset, onDone); err != nil {
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: rung transmit failed")
		// onDone never fired (the goroutine didn't start). ErrTxNotArmed (TX gone)
		// and ErrTxBadMessage (will never encode — review M1) are terminal. Anything
		// else (e.g. ErrTxInFlight) is transient: the state is untouched (a final
		// rung is still txConfirming), so the next slot retries.
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
		publish(st)
		return
	}
	publish(st)
	// Completion (confirming) is handled by onDone from the transmit goroutine.
}

// slotStart returns the UTC 15-second slot boundary containing t, epoch-aligned to
// match SlotRefFromTime's parity convention (:00/:15/:30/:45).
func slotStart(t time.Time) time.Time {
	u := t.Unix()
	return time.Unix(u-u%slotSeconds, 0).UTC()
}

// fireOpening transmits the opening rung IMMEDIATELY if, at start time, we are
// already inside our own TX slot (parity opposite the partner's) and early enough
// that head-truncation still leaves a decodable signal (ADR 0032). Without this,
// the opening rung waits for the next qualifying OnSlot — which lands at a slot
// boundary, so a session started just after one misses the current TX slot and
// stalls a full ~30 s cycle. A no-op (the OnSlot path then drives normally) when
// we're in the partner's slot or past the late window. Called once, right after
// StartQso / StartCallCq set up state. The opening rung is always non-terminal
// (answer: "calling"; call-CQ: the CQ), so there is no completion path here.
func (s *Sequencer) fireOpening(now time.Time) {
	s.mu.Lock()
	// Resolve the opening message for the active mode.
	var msg, rung string
	switch s.mode {
	case seqAnswering:
		if s.ex == nil {
			s.mu.Unlock()
			return
		}
		m, ok := s.ex.TxMessage()
		if !ok {
			s.mu.Unlock()
			return
		}
		msg, rung = m, s.ex.State.label()
	case seqCalling:
		if s.caller != nil { // a contact already started — not an opening
			s.mu.Unlock()
			return
		}
		msg, rung = s.cqMessage, "calling-cq"
	case seqWorking:
		// Opening rung is our report (cqReporting) — always non-terminal, so the
		// no-completion contract here holds (RR73 is never an opening).
		if s.caller == nil {
			s.mu.Unlock()
			return
		}
		m, ok := s.caller.TxMessage()
		if !ok {
			s.mu.Unlock()
			return
		}
		msg, rung = m, s.caller.State.label()
	default:
		s.mu.Unlock()
		return
	}

	// Only the current slot of OUR parity (opposite the partner's) is transmittable,
	// and only within the late window (else too few symbols survive truncation).
	curStart := slotStart(now)
	if SlotRefFromTime(curStart).Period == s.theirPeriod {
		s.mu.Unlock()
		return // current slot is the partner's parity — leave it to OnSlot
	}
	dt := now.Sub(curStart).Seconds()
	if dt < 0 || dt > txLateWindowSec {
		s.mu.Unlock()
		return
	}

	s.repeats++
	transmit, offset, repeats := s.transmit, s.offsetHz, s.repeats
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Bool("immediate", true).
		Msg("ft8 seq: transmitting rung")
	// The opening rung is always non-terminal (no completion), so no onDone.
	if err := transmit(msg, offset, nil); err != nil {
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: rung transmit failed")
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon() // TX gone / message can't encode — can't continue.
			return
		}
	}
	s.publish(st)
}

// statusLocked builds the QsoStatus snapshot for the active session. Caller holds s.mu.
func (s *Sequencer) statusLocked() QsoStatus {
	switch s.mode {
	case seqAnswering:
		if s.ex == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.ex.TxMessage()
		st := QsoStatus{
			Active:      true,
			Role:        roleAnswerer,
			TheirCall:   s.ex.TheirCall,
			TheirGrid:   s.theirGrid,
			State:       s.ex.State.label(),
			NextMessage: msg,
			Repeats:     s.repeats,
		}
		// Advertise the cap only on the rungs it governs (everything but the one-shot
		// 73), so the SPA's countdown shows iff max_repeats>0.
		if s.ex.State != txConfirming {
			st.MaxRepeats = s.maxRepeats
		}
		if s.ex.HasSendSnr {
			st.OurReport = formatReport(s.ex.SendSnr)
		}
		if s.ex.HasRcvdReport {
			st.TheirReport = formatReport(s.ex.RcvdReport)
		}
		return st
	case seqCalling:
		// Calling CQ: active from the first CQ. Until a station is being worked
		// (caller != nil) the rung is "calling-cq" and the next message is the CQ.
		st := QsoStatus{Active: true, Role: roleCaller, Repeats: s.repeats}
		if s.caller != nil {
			msg, _ := s.caller.TxMessage()
			st.TheirCall = s.caller.TheirCall
			st.TheirGrid = s.caller.TheirGrid
			st.State = s.caller.State.label()
			st.NextMessage = msg
			// Cap governs the working-an-answerer rungs but not the one-shot RR73; on
			// plain calling-cq (caller==nil) it's uncapped, so MaxRepeats stays 0.
			if s.caller.State != cqRogering {
				st.MaxRepeats = s.maxRepeats
			}
			if s.caller.HasSendSnr {
				st.OurReport = formatReport(s.caller.SendSnr)
			}
			if s.caller.HasRcvdReport {
				st.TheirReport = formatReport(s.caller.RcvdReport)
			}
		} else {
			st.State = "calling-cq"
			st.NextMessage = s.cqMessage
		}
		return st
	case seqWorking:
		// Working a station that called us: caller-role ladder (report → RR73), but
		// no calling-cq phase — s.caller is always set. Role "caller" so the SPA
		// renders the same ladder as Call-CQ; the cap governs the pre-RR73 rung.
		if s.caller == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.caller.TxMessage()
		st := QsoStatus{
			Active:      true,
			Role:        roleWorker,
			TheirCall:   s.caller.TheirCall,
			TheirGrid:   s.caller.TheirGrid,
			State:       s.caller.State.label(),
			NextMessage: msg,
			Repeats:     s.repeats,
		}
		if s.caller.State != cqRogering {
			st.MaxRepeats = s.maxRepeats
		}
		if s.caller.HasSendSnr {
			st.OurReport = formatReport(s.caller.SendSnr)
		}
		if s.caller.HasRcvdReport {
			st.TheirReport = formatReport(s.caller.RcvdReport)
		}
		return st
	default:
		return QsoStatus{Active: false}
	}
}

// completedQsoLocked captures the finished exchange for logging. Caller holds s.mu.
func (s *Sequencer) completedQsoLocked() CompletedQso {
	return CompletedQso{
		TheirCall:      s.ex.TheirCall,
		TheirGrid:      s.theirGrid,
		OurReport:      s.ex.SendSnr,
		HasOurReport:   s.ex.HasSendSnr,
		TheirReport:    s.ex.RcvdReport,
		HasTheirReport: s.ex.HasRcvdReport,
		OffsetHz:       s.offsetHz,
		DialFreqMHz:    s.dialFreqMHz,
	}
}
