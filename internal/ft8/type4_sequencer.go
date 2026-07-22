package ft8

import (
	stderrors "errors"
	"strings"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
)

// Reduced type-4 (nonstandard/compound call) sequencer paths — ADR 0048. These clone the
// answer-a-CQ (onSlotAnswering) and work-a-caller-FD (onSlotWorkingFd) drivers as an
// ISOLATED PARALLEL path (the pattern ADR 0037 proved for Field Day): separate Start
// funcs, onSlot handlers, and completion snapshots, so the live standard/FD paths are
// untouched. The pure ladders live in type4.go; this is the daemon-side drive (timing,
// keying, completion) sharing the Sequencer deps + the OnSlot entry.
//
// The one structural difference from the FD twins: the WORK side is a single terminal
// rung (RR73), so — unlike every other opening — it is NOT eligible for fireOpening
// (which has no completion path). onSlotWorkingT4 drives it with a proper onDone instead.

// StartQsoT4 begins answering a nonstandard station's CQ with the reduced type-4 ladder.
// ourCall is our (standard) call; theirCall is the spelled nonstandard partner; theirGrid
// is kept for the log if their CQ carried one (usually none in type-4); theirSnr is our
// SNR of their CQ, logged as RST_SENT. theirSlotUTC fixes their parity (we transmit in the
// opposite slot). Same one-session-at-a-time + opening-encode-validation discipline as
// StartQso/StartQsoFd. No config identity is needed (our own call is standard).
func (s *Sequencer) StartQsoT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, now time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	if strings.TrimSpace(ourCall) == "" {
		return ErrNoCall
	}
	if strings.TrimSpace(theirCall) == "" {
		return ErrTxBadMessage
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	ex := NewT4Exchange(ourCall, theirCall, theirGrid, theirSnr)
	// Validate our opening encodes as a type-4 message BEFORE committing (mirrors
	// StartQso): if go-ft8 can't pack it, fail up front rather than publish a ladder
	// that can never produce RF.
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
	s.mode = seqAnsweringT4
	s.skipIfSilent = false
	s.sessionGen++
	s.t4Ex = &ex
	s.theirPeriod = SlotRefFromTime(t).Period
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	s.repeats = 0
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", ex.TheirCall).Str("their_period", s.theirPeriod).
		Float64("offset_hz", offsetHz).Msg("ft8 seq: answering CQ (type-4)")
	s.publish(st)
	s.fireOpening(now)
	return nil
}

// onSlotAnsweringT4 drives an answer-a-CQ type-4 exchange — the type-4 twin of
// onSlotAnswering. It matches the partner's roger via parseType4 (which, unlike
// parseMessage, accepts the hashed "<...>" that addresses us), then transmits our rung in
// the current opposite-parity slot, late-dt. Final rung is t4Confirming (our 73); the QSO
// logs from the transmit goroutine's onDone only after the 73 truly transmits, gen-guarded
// against a superseding Abandon/disarm.
func (s *Sequencer) onSlotAnsweringT4(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	if s.t4Ex == nil {
		s.mu.Unlock()
		return
	}
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	var heard string
	advanced := false
	for _, m := range msgs {
		if pm := parseType4(m.Text); pm.from == s.t4Ex.TheirCall && s.t4Ex.presumedUs(pm.to) {
			heard = m.Text
		}
		if next, ok := s.t4Ex.Advance(m.Text); ok {
			*s.t4Ex = next
			s.repeats = 0
			advanced = true
			break
		}
	}
	if heard != "" {
		s.log.InfoWith().Str("from_worked", heard).Bool("advanced", advanced).
			Str("now_rung", s.t4Ex.State.label()).Msg("ft8 seq: type-4 decode from worked station")
	}
	if advanced {
		s.skipIfSilent = false // they came back — an armed skip no longer applies
	}

	msg, ok := s.t4Ex.TxMessage()
	if !ok { // already done — clear defensively.
		s.t4Ex = nil
		s.mode = seqIdle
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	rung := s.t4Ex.State.label()

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
	// Slot already fired (immediate fireOpening vs this slot's pending OnSlot —
	// review 2026-07-20 #2); see onSlotAnswering.
	if s.lastTxSlot.Equal(curStart.Add(SlotDuration)) {
		st := s.statusLocked()
		s.mu.Unlock()
		s.publish(st)
		return
	}

	confirming := s.t4Ex.State == t4Confirming
	if !confirming {
		// Operator-armed skip — see onSlotAnswering; same semantics.
		if s.skipIfSilent && !advanced && s.repeats > 0 {
			s.t4Ex = nil
			s.mode = seqIdle
			s.skipIfSilent = false
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: type-4 skip-if-silent — no reply; ending without repeat")
			s.publish(QsoStatus{Active: false})
			return
		}
		if s.repeats >= s.maxRepeats {
			s.t4Ex = nil
			s.mode = seqIdle
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: type-4 no answer after max repeats; abandoning")
			s.publish(QsoStatus{Active: false})
			return
		}
		s.repeats++
	}

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, offset, dial := s.transmitLocked(), s.offsetHz, s.dialFreqMHz
	repeats := s.repeats
	var completed *CompletedQso
	if confirming {
		c := s.completedQsoT4Locked()
		completed = &c
	}
	gen := s.sessionGen
	st := s.statusLocked()
	onComplete, publish := s.onComplete, s.publish
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Msg("ft8 seq: transmitting type-4 rung")

	var onDone func(ok bool)
	if completed != nil {
		c := *completed
		onDone = func(ok bool) {
			s.mu.Lock()
			if s.sessionGen != gen { // superseded — stale callback
				s.mu.Unlock()
				return
			}
			if !ok {
				s.mu.Unlock()
				s.log.WarnWith().Str("their_call", c.TheirCall).
					Msg("ft8 seq: type-4 final 73 did not transmit; will retry next slot")
				return
			}
			s.t4Ex = nil
			s.mode = seqIdle
			s.mu.Unlock()
			s.log.InfoWith().Str("their_call", c.TheirCall).Msg("ft8 seq: type-4 QSO complete (73 sent)")
			if onComplete != nil {
				onComplete(c)
			}
			publish(QsoStatus{Active: false})
		}
	}

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: type-4 rung transmit failed")
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
		publish(st)
		return
	}
	publish(st)
}

// StartWorkCallerT4 begins working a nonstandard station that called US (reduced type-4).
// theirCall/theirGrid come from the decode the operator picked; theirSnr is our SNR of
// their calling signal (RST_SENT). It does NOT fire the opening immediately: the sole rung
// is the terminal RR73, which needs onSlotWorkingT4's completion path — fireOpening has
// none — so the RR73 goes out on the next qualifying slot instead.
func (s *Sequencer) StartWorkCallerT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, now time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	if strings.TrimSpace(ourCall) == "" {
		return ErrNoCall
	}
	if strings.TrimSpace(theirCall) == "" {
		return ErrTxBadMessage
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	c := NewT4WorkExchange(ourCall, theirCall, theirGrid, theirSnr)
	if msg, ok := c.TxMessage(); ok {
		if _, err := goft8.EncodeStandardMessage(msg); err != nil {
			return ErrTxBadMessage
		}
	}

	s.mu.Lock()
	if s.mode != seqIdle {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	s.mode = seqWorkingT4
	s.skipIfSilent = false
	s.sessionGen++
	s.t4Work = &c
	s.ourCall = c.OurCall
	s.theirPeriod = SlotRefFromTime(t).Period
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	s.repeats = 0
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", c.TheirCall).Str("their_period", s.theirPeriod).
		Float64("offset_hz", offsetHz).Msg("ft8 seq: working a caller (type-4)")
	s.publish(st)
	// No fireOpening: the sole RR73 rung is terminal and must run through the onDone
	// completion path (see onSlotWorkingT4), which fireOpening does not provide.
	return nil
}

// onSlotWorkingT4 drives a work-a-caller type-4 contact — the type-4 twin of
// onSlotWorkingFd, collapsed to the single RR73 rung. That rung is always the terminal
// (confirming) rung, so there is no repeat cap or skip-if-silent path to walk: we key RR73
// in our next opposite-parity slot and the QSO logs from onDone only after it truly
// transmits, gen-guarded. On RF failure the contact stays put and the next slot retries.
func (s *Sequencer) onSlotWorkingT4(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	if s.t4Work == nil {
		s.mu.Unlock()
		return
	}
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	// Capture what the caller said this slot for diagnosability (a stalled exchange). The
	// work side never advances on a received decode — it completes on our own transmit.
	var heard string
	for _, m := range msgs {
		if pm := parseType4(m.Text); pm.from == s.t4Work.TheirCall && isPresumedUs(pm.to, s.ourCall) {
			heard = m.Text
		}
	}
	if heard != "" {
		s.log.InfoWith().Str("from_worked", heard).Str("now_rung", s.t4Work.State.label()).
			Msg("ft8 seq: type-4 work — decode from worked station")
	}

	msg, ok := s.t4Work.TxMessage()
	if !ok {
		s.mode = seqIdle
		s.t4Work = nil
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	rung := s.t4Work.State.label()

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
	// Slot already fired (immediate fireOpening vs this slot's pending OnSlot —
	// review 2026-07-20 #2); see onSlotAnswering.
	if s.lastTxSlot.Equal(curStart.Add(SlotDuration)) {
		st := s.statusLocked()
		s.mu.Unlock()
		s.publish(st)
		return
	}

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, offset, dial := s.transmitLocked(), s.offsetHz, s.dialFreqMHz
	c := s.completedT4WorkQsoLocked()
	gen := s.sessionGen
	st := s.statusLocked()
	publish, onComplete := s.publish, s.onComplete
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Msg("ft8 seq: transmitting type-4 work rung")

	onDone := func(ok bool) {
		s.mu.Lock()
		if s.sessionGen != gen { // superseded (abandon) — stale callback
			s.mu.Unlock()
			return
		}
		if !ok {
			s.mu.Unlock()
			s.log.WarnWith().Str("their_call", c.TheirCall).
				Msg("ft8 seq: type-4 work RR73 did not transmit; will retry next slot")
			return
		}
		s.t4Work = nil
		s.mode = seqIdle
		s.repeats = 0
		s.mu.Unlock()
		s.log.InfoWith().Str("their_call", c.TheirCall).
			Msg("ft8 seq: type-4 work QSO complete (RR73 sent)")
		if onComplete != nil {
			onComplete(c)
		}
		publish(QsoStatus{Active: false})
	}

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: type-4 work rung transmit failed")
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
	}
	s.publish(st)
}

// completedQsoT4Locked snapshots an answer-a-CQ type-4 contact for logging. Our SNR of
// them → RST_SENT; no report is received, so TheirReport is left unset (RST_RCVD blank);
// no grid (type-4 CQ carries none). Caller holds s.mu.
func (s *Sequencer) completedQsoT4Locked() CompletedQso {
	return CompletedQso{
		LogbookID:    s.logbookID,
		TheirCall:    s.t4Ex.TheirCall,
		TheirGrid:    s.t4Ex.TheirGrid,
		OurReport:    s.t4Ex.SendSnr,
		HasOurReport: s.t4Ex.HasSendSnr,
		StartedAt:    s.startedAt,
		OffsetHz:     s.offsetHz,
		DialFreqMHz:  s.dialFreqMHz,
	}
}

// completedT4WorkQsoLocked snapshots a work-a-caller type-4 contact for logging. Same
// degraded shape: our SNR → RST_SENT, blank RST_RCVD, no grid. Caller holds s.mu.
func (s *Sequencer) completedT4WorkQsoLocked() CompletedQso {
	return CompletedQso{
		LogbookID:    s.logbookID,
		TheirCall:    s.t4Work.TheirCall,
		TheirGrid:    s.t4Work.TheirGrid,
		OurReport:    s.t4Work.SendSnr,
		HasOurReport: s.t4Work.HasSendSnr,
		StartedAt:    s.startedAt,
		OffsetHz:     s.offsetHz,
		DialFreqMHz:  s.dialFreqMHz,
	}
}

// isPresumedUs reports whether an addressed call is us — our exact call, or a hashed
// placeholder (how a partner addresses us when our call is hashed on the wire). The
// package-level twin of T4Exchange.presumedUs, for the work path (which has no exchange
// value carrying OurCall).
func isPresumedUs(to, ourCall string) bool {
	return to == ourCall || isHashedCall(to)
}
