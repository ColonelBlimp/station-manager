package ft8

import (
	stderrors "errors"
	"slices"
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
	s.logbookID = s.pendingLogbookID           // pin the staged logbook atomically with activation
	s.allowDuplicate = s.pendingAllowDuplicate // ...and the deliberate-repeat intent with it
	s.caller = nil
	s.stalledCalls = nil // fresh session — no abandoned answerers to exclude yet
	s.confirmHold = nil
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
	theirPeriod := s.theirPeriod // capture under s.mu; the log below runs after Unlock
	s.mu.Unlock()

	s.log.InfoWith().Str("our_call", call).Str("answer_mode", answerMode).
		Float64("offset_hz", offsetHz).Str("cq_period", ourPeriod).
		Str("their_period", theirPeriod).Msg("ft8 seq: calling CQ")
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

	// Phase 0: a just-completed contact may still be asking for the RR73 it never
	// copied (see confirmHold). Resolved BEFORE picking a new answerer — finishing
	// the contact we already logged takes precedence over starting another, and when
	// there is nothing to re-send the hold releases here so this same slot's decodes
	// still feed the normal pick (so a partner who copied it costs us no throughput).
	var resendRR73 string
	if s.caller == nil && s.confirmHold != nil {
		resendRR73 = s.resolveConfirmHoldLocked(msgs)
	}

	// Phase 1 (pick an answerer) / phase 2 (advance the contact).
	var heard string
	advanced := false
	if s.caller == nil {
		// A due re-send owns this slot, so no new contact is picked — the branch
		// below must stay guarded by caller!=nil or it dereferences a nil contact.
		if resendRR73 == "" {
			if pick, text := s.pickAnswererLocked(msgs); pick != nil {
				s.caller = pick
				s.startedAt = now.UTC()
				s.repeats = 0
				heard = text
				advanced = true
			}
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

	// Message + rung for the current (our) slot: a confirm-hold re-send if one is
	// due, else the caller ladder while working a contact, else the CQ.
	var msg, rung string
	switch {
	case resendRR73 != "":
		msg, rung = resendRR73, "rogering"
	case s.caller != nil:
		if m, ok := s.caller.TxMessage(); ok {
			msg, rung = m, s.caller.State.label()
		} else {
			s.caller = nil // exhausted (shouldn't reach here) — resume CQ
		}
	}
	if msg == "" {
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
	//   - working a contact, at the RR73: a GROUP B final rung — capped too, see the
	//     default branch.
	working := s.caller != nil
	confirming := working && s.caller.State == cqRogering
	switch {
	case working && !confirming:
		if s.repeats >= s.maxRepeats {
			msg, rung = s.parkAnswererLocked(msgs, now, "answerer silent after max repeats")
		} else {
			s.repeats++
		}
	case !working:
		if resendRR73 == "" {
			s.repeats++ // CQ repeat count for status; uncapped
		}
		// A confirm-hold re-send is not a CQ, so it must not advance that counter.
		// It also leaves `confirming` false, so no completion callback is built —
		// which is what keeps the re-send from logging the QSO a second time.
	default:
		// working && confirming — the RR73 the answerer is WAITING for: a GROUP B
		// final rung (see finalrung.go). Re-entering it means the previous attempt
		// failed to transmit, and an unbounded retry hurts more here than on the
		// other ladders — the contact only clears on success, so the whole CQ loop
		// would freeze on one station and the rest of the pile-up goes unworked.
		if s.repeats >= s.maxRepeats {
			s.log.WarnWith().Str("their_call", s.caller.TheirCall).Int("attempts", s.maxRepeats).
				Msg("ft8 seq: caller — final RR73 never transmitted; dropping contact without logging")
			// Nothing is logged: they never got the roger, so neither side has a QSO.
			// Park it through the SHARED path so it gets the same rescan-or-clear the
			// pre-final cap gets — parking without the clear would exclude the station
			// for the rest of the SESSION, and if it is the only one answering we would
			// CQ forever and reject every answer it sends (codex 3c1ee047 P1).
			msg, rung = s.parkAnswererLocked(msgs, now, "final RR73 never transmitted")
			// Load-bearing: stops the completion callback below being built against
			// the contact we just released.
			confirming = false
		} else {
			s.repeats++
		}
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
	publish, prepareComplete, onComplete := s.publish, s.prepareComplete, s.onComplete
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
					Msg("ft8 seq: caller RR73 did not transmit; will retry next slot")
				return
			}
			s.caller = nil // resume calling CQ (work the pile-up)
			s.repeats = 0
			s.stalledCalls = nil // completed — fresh CQ round; previously-stalled callers retry
			// Stay listenable for one of their slots: if they did not copy this RR73
			// they will repeat their R-report, which no longer reaches us once the
			// contact is cleared (see confirmHold).
			s.confirmHold = &confirmHold{
				call:              c.TheirCall,
				resends:           confirmResendLimit,
				slots:             confirmHoldSlotLimit,
				rogeredWithReport: c.HasTheirReport,
			}
			cqSt := s.statusLocked()
			publish(cqSt) // ordered before a concurrent Abandon/replacement start
			s.mu.Unlock()
			s.log.InfoWith().Str("their_call", c.TheirCall).
				Msg("ft8 seq: caller QSO complete (RR73 sent)")
			if onComplete != nil {
				onComplete(c)
			}
		}
	}

	if resendRR73 != "" {
		// A confirm-hold re-send carries its OWN completion callback purely to
		// account for the RF budget. `transmit` returning nil means only that the
		// goroutine was queued — keying, playback and device errors surface later
		// through onDone — so spending the budget on acceptance counted failed
		// transmissions as successful re-sends (codex c2a8bea6 P2). It never logs:
		// the QSO was recorded when the ORIGINAL RR73 went out.
		onDone = func(ok bool) {
			if ok {
				s.spendConfirmResend()
			}
		}
	}
	err := transmit(msg, offset, dial, onDone)
	if err != nil {
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
	s.publishCurrent()
}

// pickAnswererLocked scans one slot's decodes for an answerer to our CQ
// ("<ourCall> <them> <grid>") whose reply encodes: auto_first takes the first by
// decode order; auto_strongest the highest-SNR one in the slot (clear the loud
// ones first). operator_pick is rejected before seqCalling is ever entered, so
// only the two auto modes reach this. A compound/portable answerer (e.g.
// K1ABC/P) yields an unencodable response, which seqTransmit would treat as
// terminal and abandon the whole Call-CQ loop (review M2) — such an answerer is
// skipped and the scan continues; nil when the slot holds no workable answerer.
// Answerers in s.stalledCalls (abandoned at the repeat cap this CQ round) are
// skipped, so a few stations that keep repeating their grid can't be re-selected
// in rotation and starve the rest of the pile-up (113e14b8 review P2). Returns the
// pick and its decode text. Caller holds s.mu.
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
		if slices.Contains(s.stalledCalls, c.TheirCall) {
			continue // already tried-and-stalled this CQ round — don't re-lock onto it
		}
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

// confirmResendLimit bounds how many times a completed Call-CQ contact will re-send
// its closing RR73 to a partner who is still asking for it. Two covers the observed
// failure — a single-slot fade or collision swallowing one RR73 — without letting a
// deaf partner hold the CQ loop hostage; past that they will restart their own
// sequence and be worked as a fresh contact.
const confirmResendLimit = 2

// confirmHoldSlotLimit bounds how many of the answerers' slots a hold may live for,
// independently of whether any re-send reached the air. It is the backstop that makes
// the hold terminate even when every transmission is refused; generous relative to
// confirmResendLimit so a couple of refused slots don't cost the repair.
const confirmHoldSlotLimit = 5

// confirmHold is a completed Call-CQ contact kept listenable for one more of the
// answerers' slots.
//
// WHY (dogfood 2026-07-26, XE1GM): the QSO logs the moment our RR73 transmits and
// the contact is cleared, so a partner who did not copy that RR73 becomes invisible
// — pickAnswererLocked only accepts a GRID answer, and they are repeating an
// R-report. XE1GM repeated `7Q5MLV XE1GM R-07` ELEVEN times at −9..−13 while the
// sequencer, having logged and moved on, ignored every one. They eventually give up,
// restart from the top with a grid answer, and get worked and logged a SECOND time —
// which is how AC8MR, KI2Y and KE4IHI became duplicate rows in the log and in QRZ,
// ClubLog and SM Cloud.
//
// The hold closes that gap: for one of their slots after the RR73, a message from
// that station is still understood. The decode log settled both parameters — the
// repeat arrives in the VERY NEXT slot (+15 s), and a partner who DID copy sends
// `73` (four for four), so the hold releases early on that and costs nothing in the
// common case.
//
// It is deliberately Call-CQ only. The other Group B ladders go idle after their
// RR73, so re-working a station there needs an operator click; the CQ loop is the
// one path that re-works automatically.
//
// ACCEPTED LIMITATION (codex 5a623c1a P1 #2 — operator-ratified 2026-07-26, not a
// defect; do not "fix" it by suppressing the re-work): the hold NARROWS the
// duplicate window, it does not close it. Once the budget is spent the call is
// forgotten, so a partner who heard none of our RR73s and later restarts with a grid
// answer is picked up as a fresh contact and logged a second time —
// pickAnswererLocked has no completed-call suppression. That is left deliberately:
// by then they genuinely never received the roger, so working them again is the
// CORRECT on-air behaviour and the only way they get their contact. The defect in
// that case is the SECOND ROW, not the second QSO, and the fix for it is
// duplicate detection/merge at log level — a separate piece, since suppressing the
// re-work would deny a station its contact to keep our log tidy (the same
// recoverability argument that settled the Group A/B split in finalrung.go).
// TestCallerSequencer_ConfirmHoldExpiryStillAllowsARestart pins the behaviour.
// Two counters, because one cannot do both jobs. `resends` is the RF budget and is
// spent ONLY when a re-send is actually accepted for transmission, so a slot the
// transmitter refuses (ErrTxInFlight / ErrTxNotReady) or the late-window defers costs
// nothing — those never reached the air, and spending on them throws the repair away
// (codex dab1143a P2). `slots` is the lifetime bound and is spent on EVERY slot the
// hold is consulted with the partner still asking, so a rig that cannot transmit at
// all still terminates the hold instead of stalling the CQ loop on one contact —
// the unbounded-retry failure the Group B cap exists to prevent (see finalrung.go).
type confirmHold struct {
	call    string // the completed partner
	resends int    // remaining RR73 re-sends that reach the air
	slots   int    // remaining slots the hold may live, whatever the outcome
	// rogeredWithReport records that the partner rogered with an R-REPORT
	// ("<us> <them> R-17") rather than a bare RRR/RR73. It disambiguates the one
	// message that is otherwise unreadable — see resolveConfirmHoldLocked.
	rogeredWithReport bool
}

// resolveConfirmHoldLocked decides what an outstanding hold does with one slot of
// the answerers' decodes, returning the RR73 to re-send (empty = nothing to send).
//
// Three outcomes, all of which either clear the hold or spend one of its re-sends,
// so it can never persist: `73`/`RR73` from them means they copied it (release);
// silence means they copied it or are gone (release); anything else addressed to us
// means they are still asking (re-send). The QSO is ALREADY logged, so a re-send
// carries no completion callback and cannot log again. Caller holds s.mu with
// s.confirmHold non-nil.
func (s *Sequencer) resolveConfirmHoldLocked(msgs []goft8.DecodedMessage) string {
	h := s.confirmHold
	// Lifetime spent — every consulted slot costs one whether or not it reached the
	// air, so this is what terminates a hold against a transmitter that keeps
	// refusing. Checked here (not only where the RF budget is spent) because those
	// slots never get that far.
	if h.slots <= 0 {
		s.confirmHold = nil
		s.log.InfoWith().Str("their_call", h.call).
			Msg("ft8 seq: caller — confirm-hold lifetime expired; releasing")
		return ""
	}
	asking := ""
	for _, m := range msgs {
		pm := parseMessage(m.Text)
		if pm.from != h.call || pm.to != s.ourCall {
			continue
		}
		// A bare 73 always confirms. msgRoger (RRR *and* RR73) is the hard case: the
		// caller ladder accepts either as the partner's roger of our report (see
		// CallerExchange.Advance), so a partner who MISSED our RR73 repeats exactly
		// that token — reading it as confirmation would release the hold at the one
		// moment the re-send is needed (codex 5a623c1a P1).
		//
		// It is only ambiguous when their roger WAS a bare RRR/RR73. If they rogered
		// with an R-REPORT and now send RR73, they have moved PAST that rung, and
		// advancing requires having received our RR73 — a partner who missed it
		// repeats their R-report instead (SQ2LXX, VK6WTF, HL3KPJ all did). So that
		// case is a sign-off, not a plea. Observed live: EW8DU rogered "R-17", got our
		// RR73, and closed with "RR73" rather than a bare 73 — which cost a needless
		// re-send until this distinction existed (dogfood 2026-07-26).
		//
		// RR73 ONLY, never bare RRR (codex cfaa6404 P2): RR73 carries the 73, so the
		// sender is finished; RRR is an acknowledgement that still expects a closing
		// 73, so a partner sending it is NOT done and may still be waiting on us.
		// The asymmetry decides the doubtful case as always — releasing too early
		// costs a duplicate QSO in three logbooks, holding too long costs one slot.
		advancedPastRoger := pm.kind == msgRoger && pm.rogerSignsOff && h.rogeredWithReport
		if pm.kind == msg73 || advancedPastRoger {
			s.confirmHold = nil
			s.log.InfoWith().Str("their_call", h.call).Str("heard", m.Text).
				Msg("ft8 seq: caller — partner confirmed the contact; releasing hold")
			return ""
		}
		asking = m.Text
	}
	if asking == "" { // silent: they copied it, or they have gone
		s.confirmHold = nil
		return ""
	}
	// Spend a slot of the hold's lifetime. This is the bound that always advances,
	// so the hold terminates even if no re-send is ever accepted.
	h.slots--
	s.log.InfoWith().Str("their_call", h.call).Str("heard", asking).
		Int("resends_left", h.resends).Int("slots_left", h.slots).
		Msg("ft8 seq: caller — partner still asking after our RR73; re-sending it")
	return h.call + " " + s.ourCall + " RR73"
}

// consumeConfirmResendLocked spends one of the hold's re-sends. Called only once
// this slot is actually COMMITTED to the transmission — past the late-window and
// already-fired guards — because the budget bounds re-sends that reach the air, and
// decrementing at decision time let a deferred slot burn one with no RF at all
// (codex 5a623c1a P2).
//
// A committed attempt that then fails still counts. That is deliberate: the budget's
// other job is to stop an unbounded re-send loop against a rig that cannot transmit,
// which is the same failure the Group B final-rung cap bounds (see finalrung.go), and
// counting attempts rather than successes is what makes it terminate. Caller holds
// s.mu with s.confirmHold non-nil.
func (s *Sequencer) spendConfirmResend() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.confirmHold == nil { // Abandon / a new session won the race — nothing to spend
		return
	}
	s.confirmHold.resends--
	if s.confirmHold.resends <= 0 {
		s.confirmHold = nil
	}
}

// parkAnswererLocked drops the current contact and decides what to transmit in its
// place: another live answerer from THIS slot's decodes, else a fresh CQ. It is the
// single off-ramp for both ways a Call-CQ contact can end badly — the answerer going
// silent on a pre-final rung, and the closing RR73 failing to transmit — so the two
// can't drift apart (they did: the final-rung branch originally parked WITHOUT the
// clear below, codex 3c1ee047 P1).
//
// The parked station is excluded from the rescan so we don't immediately re-lock onto
// the one that just failed us — but the exclusion set is CLEARED the moment the rescan
// comes up empty. That clear is load-bearing: `stalledCalls` is a per-ROUND exclusion,
// and without it a station that is the ONLY one answering stays excluded for the rest
// of the session — we would call CQ forever and reject every answer it sent.
//
// reason is folded into the log line so each caller stays greppable. It does NOT touch
// `heard`: the decode-log line for this slot has already been emitted by the time any
// caller reaches here. Caller holds s.mu with s.caller non-nil.
func (s *Sequencer) parkAnswererLocked(msgs []goft8.DecodedMessage, now time.Time, reason string) (msg, rung string) {
	s.stalledCalls = append(s.stalledCalls, s.caller.TheirCall)
	s.caller = nil
	s.repeats = 0
	if pick, _ := s.pickAnswererLocked(msgs); pick != nil {
		s.caller = pick
		s.startedAt = now.UTC()
		// Count the rung we are about to transmit. The replacement is handed straight
		// to this slot's transmit, but we are already INSIDE the switch that would
		// normally do the increment — leaving it at 0 gave the replacement one extra
		// unanswered call before its own cap fired (maxRepeats=2 → 3 transmissions),
		// blowing the configured cap and slowing rotation through the pile-up
		// (codex a301d350 P2). A first-pick answerer reaches its first transmit at
		// repeats=1; this matches.
		s.repeats = 1
		if m, ok := pick.TxMessage(); ok { // encodability pinned by the pick
			msg, rung = m, pick.State.label()
		}
		s.log.InfoWith().Str("next_answerer", pick.TheirCall).
			Msg("ft8 seq: caller — " + reason + "; working next live answerer")
		return msg, rung
	}
	// Nobody else is calling — start a fresh CQ round and let every parked station
	// back in, including the one just dropped.
	s.stalledCalls = nil
	s.log.InfoWith().Msg("ft8 seq: caller — " + reason + "; resuming CQ")
	return s.cqMessage, "calling-cq"
}

// completedCallerQsoLocked captures the finished caller-side contact for logging.
// Caller holds s.mu and s.caller is the just-completed exchange.
func (s *Sequencer) completedCallerQsoLocked() CompletedQso {
	return CompletedQso{
		LogbookID:      s.logbookID,
		AllowDuplicate: s.allowDuplicate,
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
