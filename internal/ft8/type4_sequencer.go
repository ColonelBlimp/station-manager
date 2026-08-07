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
// (which fires only non-terminal openings, with no completion). It gets its own
// immediate-fire path, fireWorkT4, which keys the RR73 WITH the onDone completion, and
// onSlotWorkingT4 drives the same terminal rung on later slots (both via
// fireWorkT4RungLocked).

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
	t, err := parseFreshSlotUTC(theirSlotUTC, now)
	if err != nil {
		return err
	}

	ex := NewT4Exchange(ourCall, theirCall, theirGrid, theirSnr)
	// Validate our opening packs as a GENUINE type-4 message BEFORE committing (mirrors
	// StartQso): fail up front rather than publish a ladder that can never produce RF.
	// Encodability alone is insufficient — a standard callsign pair also encodes (as
	// type 1), but it doesn't belong on the reduced type-4 ladder; require i3=4 so a
	// standard call is routed to the standard answer path instead.
	if msg, ok := ex.TxMessage(); ok && !encodesAsType4(msg) {
		return ErrTxBadMessage
	}

	s.mu.Lock()
	if s.mode != seqIdle {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	s.mode = seqAnsweringT4
	s.contact = contactFlags{}
	s.sessionGen++
	s.logbookID = s.pendingLogbookID           // pin the staged logbook atomically with activation
	s.allowDuplicate = s.pendingAllowDuplicate // ...and the deliberate-repeat intent with it
	s.t4Ex = &ex
	s.theirPeriod = SlotRefFromTime(t).Period
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	st := s.statusLocked()
	theirPeriod := s.theirPeriod // capture under s.mu; the log below runs after Unlock
	s.publish(st)
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", ex.TheirCall).Str("their_period", theirPeriod).
		Float64("offset_hz", offsetHz).Msg("ft8 seq: answering CQ (type-4)")
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
			s.contact.repeats = 0
			advanced = true
			break
		}
	}
	if heard != "" {
		s.log.InfoWith().Str("from_worked", heard).Bool("advanced", advanced).
			Str("now_rung", s.t4Ex.State.label()).Msg("ft8 seq: type-4 decode from worked station")
	}
	if advanced {
		s.contact.skipIfSilent = false // they came back — an armed skip no longer applies
	}

	msg, ok := s.t4Ex.TxMessage()
	if !ok { // already done — clear defensively.
		s.retireSessionLocked(func() { s.t4Ex = nil })
		s.mu.Unlock()
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
		s.logTxDeferral("type4_answer", dt)
		st := s.statusLocked()
		s.publish(st)
		s.mu.Unlock()
		return
	}
	// Slot already fired (immediate fireOpening vs this slot's pending OnSlot —
	// review 2026-07-20 #2); see onSlotAnswering.
	if s.lastTxSlot.Equal(curStart.Add(SlotDuration)) {
		s.logSameSlotDedup("type4_answer", curStart.Add(SlotDuration))
		st := s.statusLocked()
		s.publish(st)
		s.mu.Unlock()
		return
	}

	confirming := s.t4Ex.State == t4Confirming
	if !confirming {
		// Operator-armed skip — see onSlotAnswering; same semantics.
		if s.contact.skipIfSilent && !advanced && s.contact.repeats > 0 {
			s.retireSessionLocked(func() { s.t4Ex = nil })
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: type-4 skip-if-silent — no reply; ending without repeat")
			return
		}
		if s.contact.repeats >= s.maxRepeats {
			s.retireSessionLocked(func() { s.t4Ex = nil })
			s.mu.Unlock()
			s.log.InfoWith().Str("reason", causeNoAnswer).Msg("ft8 seq: type-4 no answer after max repeats; abandoning")
			return
		}
		s.contact.repeats++
	}

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, gen := s.transmitLocked()
	offset, dial := s.offsetHz, s.dialFreqMHz
	repeats := s.contact.repeats
	// GROUP A final rung (see finalrung.go): their RR73 is what advanced us here,
	// so the contact is complete on their side and this 73 is a courtesy — send
	// once and record the QSO on either outcome.
	var onDone func(bool)
	if confirming {
		onDone = s.finalRungDoneLocked(
			s.completedQsoT4Locked(),
			func() { s.t4Ex = nil },
			"ft8 seq: type-4 QSO complete (73 sent)",
			"ft8 seq: type-4 final 73 did not transmit; QSO logged anyway (partner already rogered)",
		)
	}
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Msg("ft8 seq: transmitting type-4 rung")

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: type-4 rung transmit failed")
		// Group A final rung: onDone never fired, and it is send-once — complete
		// the contact here instead of retrying or dropping it (terminal errors
		// included; TX going away does not un-make a contact they already rogered).
		if onDone != nil {
			onDone(false)
			return
		}
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.abandonNamedIfCurrent(gen, endReasonForTxErr(err), "")
			return
		}
		s.publishCurrent()
		return
	}
	s.publishCurrent()
}

// StartWorkCallerT4 begins working a nonstandard station that called US (reduced type-4).
// theirCall/theirGrid come from the decode the operator picked; theirSnr is our SNR of
// their calling signal (RST_SENT). The sole rung is the terminal RR73; unlike the other
// openings it is NOT fired via fireOpening (which has no completion path) but via
// fireWorkT4, so it still keys immediately in the current our-parity slot WITH the onDone
// that logs the QSO — otherwise it would wait a full ~30 s for the next theirPeriod slot.
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
	t, err := parseFreshSlotUTC(theirSlotUTC, now)
	if err != nil {
		return err
	}

	c := NewT4WorkExchange(ourCall, theirCall, theirGrid, theirSnr)
	// The RR73 must pack as a GENUINE type-4 message: a standard callsign pair still
	// encodes (as type 1), but working it here would key an immediate RR73 with no
	// valid reduced type-4 exchange. Reject a standard call up front — it belongs on
	// the standard work-a-caller path (StartWorkCaller), not this one.
	if msg, ok := c.TxMessage(); ok && !encodesAsType4(msg) {
		return ErrTxBadMessage
	}

	s.mu.Lock()
	if s.mode != seqIdle {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	s.mode = seqWorkingT4
	s.contact = contactFlags{}
	s.sessionGen++
	s.logbookID = s.pendingLogbookID           // pin the staged logbook atomically with activation
	s.allowDuplicate = s.pendingAllowDuplicate // ...and the deliberate-repeat intent with it
	s.t4Work = &c
	s.ourCall = c.OurCall
	s.theirPeriod = SlotRefFromTime(t).Period
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	st := s.statusLocked()
	theirPeriod := s.theirPeriod // capture under s.mu; the log below runs after Unlock
	s.publish(st)
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", c.TheirCall).Str("their_period", theirPeriod).
		Float64("offset_hz", offsetHz).Msg("ft8 seq: working a caller (type-4)")
	// Immediate terminal fire: the sole RR73 rung is terminal, so it goes through
	// fireWorkT4 (not fireOpening) — keying now in the current our-parity slot WITH
	// the onDone completion, instead of waiting ~30 s for the next theirPeriod OnSlot.
	s.fireWorkT4(now)
	return nil
}

// onSlotWorkingT4 drives a work-a-caller type-4 contact — the type-4 twin of
// onSlotWorkingFd, collapsed to the single RR73 rung. That rung is always the terminal
// (confirming) rung, so there is no skip-if-silent path to walk: we key RR73 in our next
// opposite-parity slot and the QSO logs from onDone only after it truly transmits,
// gen-guarded. On RF failure the contact stays put and the next slot retries — bounded by
// the Group B final-rung cap in fireWorkT4RungLocked (see finalrung.go), since with no
// pre-final rung there is nothing else to carry one.
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
		s.retireSessionLocked(func() { s.t4Work = nil })
		s.mu.Unlock()
		return
	}
	rung := s.t4Work.State.label()

	curStart, perr := time.Parse(time.RFC3339, ref.StartUTC)
	if perr != nil {
		s.mu.Unlock()
		return
	}
	// ref is the caller's slot (their parity); our RR73 goes out in the NEXT slot.
	txSlot := curStart.Add(SlotDuration)
	s.fireWorkT4RungLocked(msg, rung, txSlot, now.Sub(txSlot).Seconds()) // fires or defers; UNLOCKS s.mu
}

// fireWorkT4RungLocked keys the type-4 work side's SINGLE terminal RR73 rung in
// txSlot (an our-parity slot, dt seconds into it) and wires the onDone that logs
// the QSO once the RR73 truly transmits (gen-guarded). Because that rung is
// terminal, this is the one place a work-T4 contact both fires and completes —
// shared by the immediate-start fire (fireWorkT4) and the per-slot handler
// (onSlotWorkingT4), so the completion path can never drift between them. Caller
// holds s.mu with t4Work active and msg/rung resolved; the helper applies the
// late-window + already-fired-this-slot guards, snapshots, UNLOCKS s.mu, then
// transmits. It ALWAYS leaves s.mu unlocked.
func (s *Sequencer) fireWorkT4RungLocked(msg, rung string, txSlot time.Time, dt float64) {
	// Too early/late in the slot, or this exact slot already fired (an immediate
	// fire vs its own pending OnSlot — review 2026-07-20 #2): leave it for the next
	// qualifying slot; the session stays active.
	if dt < 0 || dt > txLateWindowSec {
		s.logTxDeferral("type4_work", dt)
		st := s.statusLocked()
		s.publish(st)
		s.mu.Unlock()
		return
	}
	if s.lastTxSlot.Equal(txSlot) {
		s.logSameSlotDedup("type4_work", txSlot)
		st := s.statusLocked()
		s.publish(st)
		s.mu.Unlock()
		return
	}

	// GROUP B final rung (see finalrung.go): this ladder's SOLE rung is the RR73 the
	// caller is waiting for, so re-entry means the previous attempt failed to
	// transmit. There is no pre-final rung here to carry a cap, so bound it directly
	// — counted only past the guards above, since a deferred slot is not an attempt.
	if s.contact.repeats >= s.maxRepeats {
		call, attempts := s.t4Work.TheirCall, s.maxRepeats
		s.retireSessionLocked(func() { s.t4Work = nil })
		s.mu.Unlock()
		// Nothing is logged: they never got the roger, so neither side has a QSO.
		s.log.WarnWith().Str("their_call", call).Int("attempts", attempts).
			Msg("ft8 seq: type-4 work — final RR73 never transmitted; abandoning without logging")
		return
	}
	s.contact.repeats++

	s.lastTxSlot = txSlot
	transmit, gen := s.transmitLocked()
	offset, dial := s.offsetHz, s.dialFreqMHz
	c := s.completedT4WorkQsoLocked()
	prepareComplete, onComplete := s.prepareComplete, s.onComplete
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Msg("ft8 seq: transmitting type-4 work rung")

	onDone := func(ok bool) {
		if ok && prepareComplete != nil {
			prepareComplete(&c)
		}
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
		// Same session-identity transition as every other ending completion.
		s.retireSessionLocked(func() { s.t4Work = nil })
		s.mu.Unlock()
		s.log.InfoWith().Str("their_call", c.TheirCall).
			Msg("ft8 seq: type-4 work QSO complete (RR73 sent)")
		if onComplete != nil {
			onComplete(c)
		}
	}

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: type-4 work rung transmit failed")
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.abandonNamedIfCurrent(gen, endReasonForTxErr(err), "")
			return
		}
	}
	s.publishCurrent()
}

// fireWorkT4 keys the terminal RR73 IMMEDIATELY at StartWorkCallerT4 time when the
// current slot is ours and still within the late window — the work-T4 twin of
// fireOpening. It is separate from fireOpening because the work side's SOLE rung is
// terminal: fireOpening fires only non-terminal openings (it passes a nil onDone),
// so without this the RR73 would wait for the next theirPeriod OnSlot — a full ~30 s
// away — missing the slot immediately after the caller's transmission. Falls through
// to onSlotWorkingT4 when the current slot is the caller's parity or the late window
// has closed.
func (s *Sequencer) fireWorkT4(now time.Time) {
	s.mu.Lock()
	if s.mode != seqWorkingT4 || s.t4Work == nil {
		s.mu.Unlock()
		return
	}
	msg, ok := s.t4Work.TxMessage()
	if !ok {
		s.mu.Unlock()
		return
	}
	rung := s.t4Work.State.label()
	curStart := slotStart(now)
	if SlotRefFromTime(curStart).Period == s.theirPeriod {
		s.mu.Unlock()
		return // current slot is the caller's parity — leave it to OnSlot
	}
	s.fireWorkT4RungLocked(msg, rung, curStart, now.Sub(curStart).Seconds()) // fires or defers; UNLOCKS s.mu
}

// completedQsoT4Locked snapshots an answer-a-CQ type-4 contact for logging. Our SNR of
// them → RST_SENT; no report is received, so TheirReport is left unset (RST_RCVD blank);
// no grid (type-4 CQ carries none). Caller holds s.mu.
func (s *Sequencer) completedQsoT4Locked() CompletedQso {
	return CompletedQso{
		LogbookID:      s.logbookID,
		AllowDuplicate: s.allowDuplicate,
		TheirCall:      s.t4Ex.TheirCall,
		TheirGrid:      s.t4Ex.TheirGrid,
		OurReport:      s.t4Ex.SendSnr,
		HasOurReport:   s.t4Ex.HasSendSnr,
		StartedAt:      s.startedAt,
		OffsetHz:       s.offsetHz,
		DialFreqMHz:    s.dialFreqMHz,
	}
}

// completedT4WorkQsoLocked snapshots a work-a-caller type-4 contact for logging. Same
// degraded shape: our SNR → RST_SENT, blank RST_RCVD, no grid. Caller holds s.mu.
func (s *Sequencer) completedT4WorkQsoLocked() CompletedQso {
	return CompletedQso{
		LogbookID:      s.logbookID,
		AllowDuplicate: s.allowDuplicate,
		TheirCall:      s.t4Work.TheirCall,
		TheirGrid:      s.t4Work.TheirGrid,
		OurReport:      s.t4Work.SendSnr,
		HasOurReport:   s.t4Work.HasSendSnr,
		StartedAt:      s.startedAt,
		OffsetHz:       s.offsetHz,
		DialFreqMHz:    s.dialFreqMHz,
	}
}

// isPresumedUs reports whether an addressed call is us — our exact call, or a hashed
// placeholder (how a partner addresses us when our call is hashed on the wire). The
// package-level twin of T4Exchange.presumedUs, for the work path (which has no exchange
// value carrying OurCall).
func isPresumedUs(to, ourCall string) bool {
	return to == ourCall || isHashedCall(to)
}
