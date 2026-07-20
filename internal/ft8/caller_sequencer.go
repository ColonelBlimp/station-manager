package ft8

import (
	stderrors "errors"
	"strings"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Caller-side sequencing (ADR 0033): the Sequencer methods that drive a Call-CQ
// session. We call CQ in our chosen slot parity and work the stations that answer —
// one at a time, looping until Abandon (operator-confirmed). Answerer selection is
// the answerMode knob: auto_first works the first valid answerer (wired today);
// operator_pick (the pile-up stack) is a later increment. Shares the Sequencer's
// transmit/publish/onComplete deps and the OnSlot entry point with the answerer side.

// StartCallCq begins a sequenced Call-CQ session. ourCall/ourGrid build the CQ and
// the per-contact exchanges; offsetHz is our TX offset; dialFreqMHz is the rig dial
// for the logged QSO frequency; answerMode selects answerer picking; nowUTC fixes our
// TX parity (we call in the next slot). Requires armed TX (Service.StartCallCq checks
// the arm gate). One session at a time (ErrQsoInProgress).
func (s *Sequencer) StartCallCq(ourCall, ourGrid string, offsetHz, dialFreqMHz float64, answerMode, txParity string, nowUTC time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	call := strings.ToUpper(strings.TrimSpace(ourCall))
	if call == "" {
		return ErrNoCall
	}
	grid := strings.ToUpper(strings.TrimSpace(ourGrid))
	if len(grid) > 4 {
		grid = grid[:4]
	}
	cq := "CQ " + call
	if grid != "" {
		cq += " " + grid
	}
	// Validate our CQ encodes BEFORE committing the session (review M1): a
	// malformed operator callsign or grid would otherwise enter an uncapped
	// calling-cq loop that can never produce RF. Fail up front.
	if _, err := goft8.EncodeStandardMessage(cq); err != nil {
		return ErrTxBadMessage
	}

	s.mu.Lock()
	if s.mode != seqIdle {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	s.mode = seqCalling
	s.skipIfSilent = false
	s.sessionGen++
	s.caller = nil
	s.ourCall = call
	s.ourGrid = grid
	s.cqMessage = cq
	s.answerMode = answerMode
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.repeats = 0
	// Our CQ parity: the operator's explicit even/odd choice (WSJT-X "Tx even/1st"),
	// else the next slot (fire ASAP — the default). We transmit CQ in our parity and
	// PROCESS answers in the opposite (answerers') parity — theirPeriod; onSlotCalling
	// fires the CQ in our parity. A non-even/odd value (e.g. "") keeps the fire-ASAP
	// default. Choosing a parity can delay the first CQ by one extra slot when the next
	// boundary is the other parity — expected (it's the point of the choice).
	ourPeriod := nextSlotPeriod(nowUTC)
	switch strings.ToLower(strings.TrimSpace(txParity)) {
	case "even":
		ourPeriod = "even"
	case "odd":
		ourPeriod = "odd"
	}
	s.theirPeriod = oppositePeriod(ourPeriod)
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("our_call", call).Str("answer_mode", answerMode).
		Float64("offset_hz", offsetHz).Str("cq_period", ourPeriod).
		Str("their_period", s.theirPeriod).Msg("ft8 seq: calling CQ")
	s.publish(st)
	// No immediate-fire here (unlike answering a CQ): we chose our CQ parity as the
	// NEXT slot, so the first CQ goes out at the upcoming boundary (≤ one slot, ~15 s)
	// via onSlotCalling — already far better than the answer-a-CQ worst case, and we
	// don't claim the current slot for a CQ.
	return nil
}

// onSlotCalling drives a Call-CQ session. It acts in the answerers' slots (theirPeriod
// = opposite our CQ parity): phase 1 (caller == nil) scans for an answer and, under
// auto_first, picks the first to work; phase 2 (caller != nil) advances the contact.
// Either way it transmits in the current (our CQ) slot — the CQ until a contact
// starts, then the caller ladder. On RR73 the QSO logs and we resume calling CQ (loop
// the pile-up until Abandon — ADR 0033). A contact abandoned at max repeats re-scans
// the same slot for another live answerer before falling back to the CQ.
func (s *Sequencer) onSlotCalling(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	if s.mode != seqCalling {
		s.mu.Unlock()
		return
	}
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	// Phase 1 (pick an answerer) / phase 2 (advance the contact).
	var heard string
	advanced := false
	if s.caller == nil {
		if pick, text := s.pickAnswererLocked(msgs); pick != nil {
			s.caller = pick
			s.startedAt = now.UTC()
			s.repeats = 0
			heard = text
			advanced = true
		}
	} else {
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
	}

	// Message + rung for the current (our) slot: the caller ladder while working a
	// contact, else the CQ.
	var msg, rung string
	if s.caller != nil {
		if m, ok := s.caller.TxMessage(); ok {
			msg, rung = m, s.caller.State.label()
		} else {
			s.caller = nil // exhausted (shouldn't reach here) — resume CQ
		}
	}
	if s.caller == nil {
		msg, rung = s.cqMessage, "calling-cq"
	}

	if heard != "" {
		s.log.InfoWith().Str("from_worked", heard).Bool("advanced", advanced).
			Str("now_rung", rung).Msg("ft8 seq: caller decode from worked station")
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

	// Repeat cap / off-ramp:
	//   - working a contact, pre-RR73: cap unanswered repeats; if the answerer goes
	//     silent, drop the contact and work another live answerer from THIS slot's
	//     decodes — the pile-up kept calling while we worked the silent one, and a
	//     CQ here would waste their replies (dogfood 2026-07-17). Only when nobody
	//     else is calling do we resume the CQ.
	//   - calling CQ (no contact yet): NO cap — calling CQ is the operator's standing
	//     intent; we keep calling until answered or Abandoned.
	working := s.caller != nil
	confirming := working && s.caller.State == cqRogering
	switch {
	case working && !confirming:
		if s.repeats >= s.maxRepeats {
			s.caller = nil
			s.repeats = 0
			if pick, text := s.pickAnswererLocked(msgs); pick != nil {
				s.caller = pick
				s.startedAt = now.UTC()
				heard = text
				if m, ok := pick.TxMessage(); ok { // encodability pinned by the pick
					msg, rung = m, pick.State.label()
				}
				s.log.InfoWith().Str("next_answerer", pick.TheirCall).
					Msg("ft8 seq: caller — answerer silent after max repeats; working next live answerer")
			} else {
				s.log.InfoWith().Msg("ft8 seq: caller — answerer silent after max repeats; resuming CQ")
				msg, rung = s.cqMessage, "calling-cq"
			}
		} else {
			s.repeats++
		}
	case !working:
		s.repeats++ // CQ repeat count for status; uncapped
	default:
		// working && confirming — about to send RR73 (a one-shot); repeats untouched.
	}

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, offset, dial := s.transmitLocked(), s.offsetHz, s.dialFreqMHz
	repeats := s.repeats
	var completed *CompletedQso
	if confirming {
		// Capture the QSO data but DO NOT clear the contact or resume CQ yet
		// (review follow-up M1): the contact stays in cqRogering until the RR73
		// actually transmits, so a synchronous ErrTxInFlight retries RR73 next slot
		// instead of silently dropping to CQ. Report fields are final at
		// cqRogering — Sent() only flips State — so reading the live exchange works.
		c := s.completedCallerQsoLocked()
		completed = &c
	}
	gen := s.sessionGen
	st := s.statusLocked()
	publish, onComplete := s.publish, s.onComplete
	s.mu.Unlock()

	// Side effects outside the lock.
	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Msg("ft8 seq: caller transmitting rung")

	// Final-rung (RR73) completion (review H1 + follow-up M1): log + resume CQ
	// ONLY on a true on-air success, gen-guarded so an Abandon mid-flight can't
	// log or publish. On RF failure → keep the contact in cqRogering so the next
	// slot retries RR73 rather than silently dropping back to CQ.
	var onDone func(ok bool)
	if completed != nil {
		c := *completed
		onDone = func(ok bool) {
			s.mu.Lock()
			if s.sessionGen != gen { // superseded (abandon) — stale callback
				s.mu.Unlock()
				return
			}
			if !ok {
				s.mu.Unlock()
				s.log.WarnWith().Str("their_call", c.TheirCall).
					Msg("ft8 seq: caller RR73 did not transmit; will retry next slot")
				return
			}
			s.caller = nil // resume calling CQ (work the pile-up)
			s.repeats = 0
			cqSt := s.statusLocked()
			s.mu.Unlock()
			s.log.InfoWith().Str("their_call", c.TheirCall).
				Msg("ft8 seq: caller QSO complete (RR73 sent)")
			if onComplete != nil {
				onComplete(c)
			}
			publish(cqSt) // now back to calling-cq
		}
	}

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: caller rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: caller rung transmit failed")
		// onDone never fired, so a final-rung QSO is correctly not logged.
		// ErrTxNotArmed / ErrTxBadMessage are terminal (review M1); else transient
		// — the contact is untouched (still cqRogering), so the next slot retries.
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
	}
	s.publish(st)
}

// pickAnswererLocked scans one slot's decodes for an answerer to our CQ
// ("<ourCall> <them> <grid>") whose reply encodes: auto_first takes the first by
// decode order; auto_strongest the highest-SNR one in the slot (clear the loud
// ones first). operator_pick is rejected before seqCalling is ever entered, so
// only the two auto modes reach this. A compound/portable answerer (e.g.
// K1ABC/P) yields an unencodable response, which seqTransmit would treat as
// terminal and abandon the whole Call-CQ loop (review M2) — such an answerer is
// skipped and the scan continues; nil when the slot holds no workable answerer.
// Returns the pick and its decode text. Caller holds s.mu.
func (s *Sequencer) pickAnswererLocked(msgs []goft8.DecodedMessage) (*CallerExchange, string) {
	strongest := s.answerMode == types.Ft8CallerAnswerAutoStrongest
	var pick *CallerExchange
	var pickText string
	var pickSnr int
	for _, m := range msgs {
		pm := parseMessage(m.Text)
		if pm.kind != msgGrid || pm.to != s.ourCall {
			continue
		}
		c := NewCallerExchange(s.ourCall, pm.from, pm.grid, m.SNR)
		reply, ok := c.TxMessage()
		if !ok {
			continue
		}
		if _, err := goft8.EncodeStandardMessage(reply); err != nil {
			s.log.InfoWith().Str("answerer", pm.from).
				Msg("ft8 seq: skipping answerer — our reply does not encode (compound/portable call?)")
			continue
		}
		if pick == nil || (strongest && m.SNR > pickSnr) {
			pick = &c
			pickText = m.Text
			pickSnr = m.SNR
		}
		if !strongest {
			break // auto_first: the first encodable answerer wins
		}
	}
	return pick, pickText
}

// completedCallerQsoLocked captures the finished caller-side contact for logging.
// Caller holds s.mu and s.caller is the just-completed exchange.
func (s *Sequencer) completedCallerQsoLocked() CompletedQso {
	return CompletedQso{
		TheirCall:      s.caller.TheirCall,
		TheirGrid:      s.caller.TheirGrid,
		OurReport:      s.caller.SendSnr,
		HasOurReport:   s.caller.HasSendSnr,
		TheirReport:    s.caller.RcvdReport,
		HasTheirReport: s.caller.HasRcvdReport,
		StartedAt:      s.startedAt,
		OffsetHz:       s.offsetHz,
		DialFreqMHz:    s.dialFreqMHz,
	}
}

// nextSlotPeriod is the FT8 even/odd parity of the slot AFTER nowUTC, matching the
// daemon convention (SlotRefFromTime): (unix / slotSeconds) % 2 == 0 → "even".
func nextSlotPeriod(nowUTC time.Time) string {
	if (nowUTC.UTC().Unix()/slotSeconds+1)%2 == 0 {
		return "even"
	}
	return "odd"
}

// oppositePeriod returns the other FT8 slot parity.
func oppositePeriod(p string) string {
	if p == "even" {
		return "odd"
	}
	return "even"
}
