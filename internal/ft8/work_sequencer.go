package ft8

import (
	stderrors "errors"
	"strings"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
)

// FT8 "work a caller" sequencing (ADR 0033) — working a station that is calling US.
// When we are on frequency (often having just answered someone else's CQ) other
// stations call us directly with "<ourCall> <theirCall> <grid>". The operator picks
// one from the Band-Activity pile-up; this drives that single contact.
//
// Mechanically it is the caller-side exchange (we report first, they R-report, we
// RR73 — reusing CallerExchange), since the caller has already sent us their grid. It
// differs from a Call-CQ session (caller_sequencer.go) in two ways: there is no
// CQ-calling phase (the contact is chosen up front), and on completion or off-ramp it
// goes IDLE rather than looping back to calling CQ. Attended throughout — the operator
// initiates each contact by picking it. Shares the Sequencer deps + the OnSlot entry.

// StartWorkCaller begins working a station that is calling us. theirCall/theirGrid come
// from the decode the operator picked ("<ourCall> <theirCall> <grid>"); theirSnr is our
// SNR of that calling signal — the report we send back (RST_SENT). theirSlotUTC is the
// RFC3339 start of the slot that decode was heard in, fixing the caller's parity (we
// transmit in the opposite/current slot). offsetHz is the operator-picked TX offset.
// Requires armed TX (Service.StartWorkCaller checks the gate). One session at a time
// (ErrQsoInProgress).
func (s *Sequencer) StartWorkCaller(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, now time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	call := strings.ToUpper(strings.TrimSpace(ourCall))
	if call == "" {
		return ErrNoCall
	}
	if strings.TrimSpace(theirCall) == "" {
		return ErrTxBadMessage
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	c := NewCallerExchange(call, theirCall, theirGrid, theirSnr)
	// Validate OUR reply encodes BEFORE committing the session (mirrors StartQso /
	// the Call-CQ M2 guard): a compound/portable caller (e.g. K1ABC/P) yields an
	// unencodable report, which would otherwise commit a ladder that can never
	// produce RF. Fail up front with a clear error.
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
	s.mode = seqWorking
	s.skipIfSilent = false
	s.sessionGen++
	s.logbookID = s.pendingLogbookID           // pin the staged logbook atomically with activation
	s.allowDuplicate = s.pendingAllowDuplicate // ...and the deliberate-repeat intent with it
	s.caller = &c
	s.ourCall = call
	s.theirPeriod = SlotRefFromTime(t).Period
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	s.repeats = 0
	st := s.statusLocked()
	theirPeriod := s.theirPeriod // capture under s.mu; the log below runs after Unlock
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", c.TheirCall).Str("their_period", theirPeriod).
		Float64("offset_hz", offsetHz).Msg("ft8 seq: working a caller")
	s.publish(st)
	// Send our opening report this slot if we're already in our TX window — else the
	// first rung waits for the next qualifying OnSlot (mirrors StartQso).
	s.fireOpening(now)
	return nil
}

// onSlotWorking drives a "work a caller" contact (seqWorking). It mirrors phase 2 of
// onSlotCalling — advance the CallerExchange on a decode from the worked station, then
// transmit our rung in the current (opposite-parity) slot, late-dt — but with NO CQ
// fallback and an idle terminal: on the final RR73 it logs + goes idle, and a silent
// answerer abandons rather than dropping to CQ (there is no standing CQ to fall back to).
func (s *Sequencer) onSlotWorking(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	if s.mode != seqWorking {
		s.mu.Unlock()
		return
	}
	if s.caller == nil { // defensive: seqWorking always has a contact
		s.mode = seqIdle
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	// Only the worked station's slots set up our reply; our own parity just decoded
	// our own transmission — nothing to do.
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	// Advance on their decode directed to us (records their report). Capture what the
	// worked station said this slot for diagnosability of a stalled exchange.
	var heard string
	advanced := false
	for _, m := range msgs {
		if pm := parseMessage(m.Text); pm.to == s.ourCall && pm.from == s.caller.TheirCall {
			heard = m.Text
		}
		if next, ok := s.caller.Advance(m.Text); ok {
			*s.caller = next
			s.repeats = 0
			advanced = true
			break
		}
	}

	msg, ok := s.caller.TxMessage()
	if !ok { // exchange exhausted off the onDone path — shouldn't happen; clear defensively.
		s.mode = seqIdle
		s.caller = nil
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	rung := s.caller.State.label()
	if heard != "" {
		s.log.InfoWith().Str("from_worked", heard).Bool("advanced", advanced).
			Str("now_rung", rung).Msg("ft8 seq: working caller — decode from worked station")
	}
	if advanced {
		s.skipIfSilent = false // they came back — an armed skip no longer applies
	}

	// Late-window guard (ADR 0032): too late into our slot and head-truncation loses
	// too much; skip this slot and retry next cycle.
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

	// Repeat cap / off-ramp: pre-RR73 rungs are capped; on silence ABANDON (unlike
	// Call-CQ, which resumes calling CQ). The RR73 (cqRogering) is a GROUP B final
	// rung (see finalrung.go): the answerer is waiting on it, so it is retried
	// rather than sent once — but under the SAME cap, because re-entering that rung
	// means the previous attempt failed to transmit and an unbounded retry re-keys
	// the rig every cycle forever.
	confirming := s.caller.State == cqRogering
	if !confirming {
		// Operator-armed skip — see onSlotAnswering; same semantics. Deliberately
		// pre-final only: it means "stop calling a station that isn't answering",
		// and at the final rung nobody is being waited for.
		if s.skipIfSilent && !advanced && s.repeats > 0 {
			s.caller = nil
			s.mode = seqIdle
			s.skipIfSilent = false
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: working caller — skip-if-silent; ending without repeat")
			s.publish(QsoStatus{Active: false})
			return
		}
	}
	if s.repeats >= s.maxRepeats {
		call, attempts := s.caller.TheirCall, s.maxRepeats
		s.caller = nil
		s.mode = seqIdle
		s.mu.Unlock()
		if confirming {
			// Group B: they never received the roger, so neither side has a QSO —
			// abandon WITHOUT logging rather than invent a contact they don't hold.
			s.log.WarnWith().Str("their_call", call).Int("attempts", attempts).
				Msg("ft8 seq: working caller — final RR73 never transmitted; abandoning without logging")
		} else {
			s.log.InfoWith().Msg("ft8 seq: working caller — no answer after max repeats; abandoning")
		}
		s.publish(QsoStatus{Active: false})
		return
	}
	s.repeats++

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, offset, dial := s.transmitLocked(), s.offsetHz, s.dialFreqMHz
	repeats := s.repeats
	var completed *CompletedQso
	if confirming {
		// Capture the QSO data but DO NOT clear the contact yet (review follow-up M1):
		// the contact stays in cqRogering until the RR73 actually transmits, so a
		// synchronous ErrTxInFlight retries RR73 next slot instead of dropping idle.
		c := s.completedCallerQsoLocked()
		completed = &c
	}
	gen := s.sessionGen
	prepareComplete, onComplete := s.prepareComplete, s.onComplete
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Msg("ft8 seq: working caller transmitting rung")

	// Final-rung (RR73) completion (review H1 + follow-up M1): log + go IDLE ONLY on a
	// true on-air success, gen-guarded so an Abandon mid-flight can't log or publish.
	// On RF failure → keep the contact in cqRogering so the next slot retries RR73.
	var onDone func(ok bool)
	if completed != nil {
		c := *completed
		onDone = func(ok bool) {
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
					Msg("ft8 seq: working caller RR73 did not transmit; will retry next slot")
				return
			}
			// Terminal: go idle (NOT resume CQ). Through the shared primitive so
			// this ending performs the SAME session-identity transition as every
			// other — retire the generation, consume any staged teardown reason,
			// publish under the lock. Doing it by hand here is how it drifted.
			s.retireSessionLocked(func() { s.caller = nil })
			s.mu.Unlock()
			s.log.InfoWith().Str("their_call", c.TheirCall).
				Msg("ft8 seq: working caller QSO complete (RR73 sent)")
			if onComplete != nil {
				onComplete(c)
			}
		}
	}

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: working caller rung transmit failed")
		// onDone never fired, so a final-rung QSO is correctly not logged. Terminal
		// errors abandon; transient ones leave the contact for the next slot to retry.
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
	}
	s.publishCurrent()
}

// StartWorkCallerFd begins working a station that called US with a Field Day exchange
// (the FD twin of StartWorkCaller). theirClass/theirSection are parsed from the call we
// picked ("<ourCall> <theirCall> <class> <section>"); ourClass/ourSection are the
// operator's configured identity. Requires armed TX; one session at a time.
func (s *Sequencer) StartWorkCallerFd(ourCall, ourClass, ourSection, theirCall, theirGrid, theirClass, theirSection string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, now time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	call := strings.ToUpper(strings.TrimSpace(ourCall))
	if call == "" {
		return ErrNoCall
	}
	if strings.TrimSpace(ourClass) == "" || strings.TrimSpace(ourSection) == "" {
		return ErrFdIdentityUnset
	}
	if strings.TrimSpace(theirCall) == "" {
		return ErrTxBadMessage
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	c := NewFdWorkExchange(call, ourClass, ourSection, theirCall, theirGrid, theirClass, theirSection, theirSnr)
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
	s.mode = seqWorkingFd
	s.skipIfSilent = false
	s.sessionGen++
	s.logbookID = s.pendingLogbookID           // pin the staged logbook atomically with activation
	s.allowDuplicate = s.pendingAllowDuplicate // ...and the deliberate-repeat intent with it
	s.fdWork = &c
	s.ourCall = call
	s.theirPeriod = SlotRefFromTime(t).Period
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	s.repeats = 0
	st := s.statusLocked()
	theirPeriod := s.theirPeriod // capture under s.mu; the log below runs after Unlock
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", c.TheirCall).Str("their_period", theirPeriod).
		Float64("offset_hz", offsetHz).Str("our_class", ourClass).Str("our_section", ourSection).
		Str("their_class", theirClass).Str("their_section", theirSection).
		Msg("ft8 seq: working a caller (FD)")
	s.publish(st)
	s.fireOpening(now)
	return nil
}

// onSlotWorkingFd drives a work-a-caller-FD contact (seqWorkingFd) — the FD twin of
// onSlotWorking: send R+our class/section, advance on their RR73, send our RR73, log
// their class/section. Same idle-terminal + off-ramp + final-rung onDone discipline.
func (s *Sequencer) onSlotWorkingFd(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	if s.mode != seqWorkingFd {
		s.mu.Unlock()
		return
	}
	if s.fdWork == nil {
		s.mode = seqIdle
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	var heard string
	advanced := false
	for _, m := range msgs {
		if pm := parseMessage(m.Text); pm.to == s.ourCall && pm.from == s.fdWork.TheirCall {
			heard = m.Text
		}
		if next, ok := s.fdWork.Advance(m.Text); ok {
			*s.fdWork = next
			s.repeats = 0
			advanced = true
			break
		}
	}

	msg, ok := s.fdWork.TxMessage()
	if !ok {
		s.mode = seqIdle
		s.fdWork = nil
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	rung := s.fdWork.State.label()
	if heard != "" {
		s.log.InfoWith().Str("from_worked", heard).Bool("advanced", advanced).
			Str("now_rung", rung).Msg("ft8 seq: working caller (FD) — decode from worked station")
	}
	if advanced {
		s.skipIfSilent = false // they came back — an armed skip no longer applies
	}

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

	confirming := s.fdWork.State == fdwRogering
	if !confirming {
		// Operator-armed skip — see onSlotAnswering; same semantics.
		if s.skipIfSilent && !advanced && s.repeats > 0 {
			s.fdWork = nil
			s.mode = seqIdle
			s.skipIfSilent = false
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: working caller (FD) — skip-if-silent; ending without repeat")
			s.publish(QsoStatus{Active: false})
			return
		}
		if s.repeats >= s.maxRepeats {
			s.fdWork = nil
			s.mode = seqIdle
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: working caller (FD) — no answer after max repeats; abandoning")
			s.publish(QsoStatus{Active: false})
			return
		}
		s.repeats++
	}

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, offset, dial := s.transmitLocked(), s.offsetHz, s.dialFreqMHz
	repeats := s.repeats
	// GROUP A final rung (see finalrung.go): in Field Day the ANSWERING station
	// sends the closing RR73, and receiving theirs is what advanced us here — so on
	// the work side the contact is already complete for them and our RR73 is the
	// courtesy. Send once and record the QSO on either outcome. (Note this is the
	// mirror of the standard work-a-caller ladder, which is Group B.)
	var onDone func(bool)
	if confirming {
		onDone = s.finalRungDoneLocked(
			s.completedFdWorkQsoLocked(),
			func() { s.fdWork = nil },
			"ft8 seq: working caller (FD) QSO complete (RR73 sent)",
			"ft8 seq: working caller (FD) RR73 did not transmit; QSO logged anyway (partner already rogered)",
		)
	}
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Msg("ft8 seq: working caller (FD) transmitting rung")

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: working caller (FD) rung transmit failed")
		// Group A final rung: onDone never fired, and it is send-once — complete
		// the contact here instead of retrying or dropping it (terminal errors
		// included; TX going away does not un-make a contact they already rogered).
		if onDone != nil {
			onDone(false)
			return
		}
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
	}
	s.publishCurrent()
}
