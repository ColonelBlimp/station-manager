package ft8

import (
	stderrors "errors"
	"sync"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
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
// the QSO is abandoned (ADR 0031 off-ramp). ~6 of our slots ≈ 90 s of calling.
const defaultSeqMaxRepeats = 6

// txLateWindowSec is the latest into our slot we will START a rung. Past this,
// too few symbols / Costas sync words survive the head-truncation (ADR 0032) for
// a reliable decode, so we skip and retry on our next cycle. The truncation
// itself lives in the TxController. Package var so tests can dial it.
var txLateWindowSec = 4.5

// Sequencer sentinels (mapped to HTTP by the api layer).
var (
	// ErrQsoInProgress: StartQso while a QSO is already active.
	ErrQsoInProgress = stderrors.New("ft8: a QSO is already in progress")
	// ErrNoOffset: StartQso without a TX offset.
	ErrNoOffset = stderrors.New("ft8: no TX offset selected")
)

// QsoStatus is the `ft8-qso` SSE payload + the sequencer's exposed state: the
// active contact's rung, the worked call, the message we'll send next, and the
// unanswered-repeat count. Active=false means idle (no QSO).
type QsoStatus struct {
	Active      bool   `json:"active"`
	TheirCall   string `json:"their_call,omitempty"`
	State       string `json:"state,omitempty"` // calling | reporting | confirming
	NextMessage string `json:"next_message,omitempty"`
	Repeats     int    `json:"repeats,omitempty"`
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
}

// Sequencer owns the (single) active exchange. Its dependencies are injected so
// it stays decoupled from the Service and unit-testable: transmit sends a rung
// (Service.seqTransmit — current-slot late-dt), publish fans out QsoStatus on the
// ft8-qso SSE, and onComplete (optional, e4) consumes a finished QSO.
type Sequencer struct {
	mu          sync.Mutex
	ex          *Exchange // active exchange; nil = idle
	theirPeriod string    // the worked station's TX parity ("even"/"odd")
	theirGrid   string
	offsetHz    float64
	dialFreqMHz float64 // rig dial freq at start, for the logged QSO frequency
	repeats     int
	maxRepeats  int

	transmit   func(message string, offsetHz float64) error
	publish    func(QsoStatus)
	onComplete func(CompletedQso)
	log        logging.Logger
}

func newSequencer(transmit func(string, float64) error, publish func(QsoStatus), log logging.Logger) *Sequencer {
	if log == nil {
		log = logging.Noop()
	}
	return &Sequencer{
		maxRepeats: defaultSeqMaxRepeats,
		transmit:   transmit,
		publish:    publish,
		log:        log,
	}
}

// StartQso begins answering a CQ. theirSlotUTC is the RFC3339 start of the slot
// the CQ was heard in (it fixes the worked station's parity — we transmit in the
// opposite one). offsetHz is the operator-picked clear offset. Idempotency: only
// one QSO at a time (ErrQsoInProgress).
func (s *Sequencer) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.ex != nil {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	ex := NewExchange(ourCall, ourGrid, theirCall)
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
	return nil
}

// Abandon drops the active QSO (operator action, disarm, or off-ramp). Idempotent.
func (s *Sequencer) Abandon() {
	s.mu.Lock()
	was := s.ex != nil
	s.ex = nil
	s.repeats = 0
	s.mu.Unlock()
	if was {
		s.log.InfoWith().Msg("ft8 seq: QSO abandoned")
		s.publish(QsoStatus{Active: false})
	}
}

// Active reports whether a QSO is in progress.
func (s *Sequencer) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ex != nil
}

// OnSlot is the per-slot driver, called by the decode loop once each completed
// slot. ref is the slot just decoded; msgs are its decodes; now is wall-clock UTC.
// We act only around the worked station's slot: feed their decodes to Advance,
// then transmit our rung in the (current) opposite-parity slot, late-dt.
func (s *Sequencer) OnSlot(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
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
		// Sending the 73 completes the QSO.
		sent := s.ex.Sent()
		s.ex = &sent
		c := s.completedQsoLocked()
		completed = &c
		s.ex = nil // back to idle once captured
	}
	st := s.statusLocked()
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
	if err := transmit(msg, offset); err != nil {
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: rung transmit failed")
		if stderrors.Is(err, ErrTxNotArmed) {
			s.Abandon() // TX disarmed under us — can't continue.
			return
		}
	}
	s.publish(st)
	if completed != nil {
		s.log.InfoWith().Str("their_call", completed.TheirCall).Msg("ft8 seq: QSO complete (73 sent)")
		if s.onComplete != nil {
			s.onComplete(*completed)
		}
		s.publish(QsoStatus{Active: false})
	}
}

// statusLocked builds the QsoStatus snapshot. Caller holds s.mu.
func (s *Sequencer) statusLocked() QsoStatus {
	if s.ex == nil {
		return QsoStatus{Active: false}
	}
	msg, _ := s.ex.TxMessage()
	return QsoStatus{
		Active:      true,
		TheirCall:   s.ex.TheirCall,
		State:       s.ex.State.label(),
		NextMessage: msg,
		Repeats:     s.repeats,
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
