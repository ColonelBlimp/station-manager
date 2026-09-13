package ft8

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Same-band/profile repeat hold (operator ruling 2026-09-13, W-0019).
//
// The Africa FT4 DX Contest (2026-09-12) had the Call-CQ run work ZS6BOS twice
// on 20 m and twice on 40 m an hour apart: the run had no view of the logbook
// (internal/ft8 must not import storage), and ADR 0059's "no completed-call
// suppression" covered only the same session's memory. Ruling: surface a
// same-band/profile repeat in the ladder; never auto-skip and never auto-answer
// it. The run HOLDS before TX to that station, marks it as already worked, and
// the operator chooses Answer anyway (the pick intent) or Next. A failed or
// unknown lookup must not block operation — it is answered as usual.
//
// Shape, so nobody re-derives it: the hold blocks answering THAT station only.
// The run otherwise carries on — a CQ run keeps calling, an auto-work run keeps
// listening, and other callers are worked as usual (pickAnswererLocked skips
// the held station and looks at the next candidate). One hold at a time: a
// second repeat heard while one is held is skipped this slot and gets its own
// hold once the first resolves. A hold ends on Answer anyway, on Next (the
// station joins declinedRepeats for the rest of the run), when a contact commits
// (the operator's attention has moved on), when the station stops calling past
// cqAnswererStaleAfter, or when the run ends.

// WorkedBeforeFunc answers "worked this call on this band and ADIF mode/submode
// in this logbook?" — the contest-dupe question the SPA already asks. Injected
// by cmd/smd (the storage read lives on the other side of the package boundary);
// nil = no view of the logbook, nothing is ever held.
type WorkedBeforeFunc func(ctx context.Context, logbookID int64, call, band, mode, submode string) (bool, error)

// repeatLookupBudget bounds the lookup, which runs under s.mu inside a slot
// evaluation: a local SQLite read answers in milliseconds, and past this the
// answer is treated as unknown (answer as usual) rather than delaying the slot.
// An implementation bound, not an operator threshold.
const repeatLookupBudget = 500 * time.Millisecond

// HeldReasonWorkedBefore is the one hold reason today; a stable code the SPA renders.
const HeldReasonWorkedBefore = "worked_before"

// HeldRepeat rides the ft8-qso frame while a station is held: who, what they
// sent, and the axis the repeat was found on, so the operator can decide.
type HeldRepeat struct {
	Call    string `json:"call"`
	Grid    string `json:"grid,omitempty"`
	Snr     int    `json:"snr"`
	Band    string `json:"band"`
	Mode    string `json:"mode"`
	Submode string `json:"submode,omitempty"`
	Reason  string `json:"reason"`
}

// repeatHold is the sequencer's live hold (mu-guarded).
type repeatHold struct {
	HeldRepeat
	period    string // the slot parity the station was heard in — the commit needs it
	lastHeard time.Time
}

// isWorkedRepeatLocked asks the seam whether c is a same-band/profile repeat on
// the logbook the next contact would log to. Unknown (no seam, error, timeout)
// is false: never block operation on a lookup. Caller holds s.mu.
func (s *Sequencer) isWorkedRepeatLocked(call string, dialMHz float64) (bool, string) {
	if s.workedBefore == nil {
		return false, ""
	}
	band := utils.FrequencyToBand(fmt.Sprintf("%.6f", dialMHz))
	if band == "" {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), repeatLookupBudget)
	defer cancel()
	worked, err := s.workedBefore(ctx, s.pendingLogbookID, call, band, s.profile.AdifMode, s.profile.AdifSubmode)
	if err != nil {
		s.log.WarnWith().Err(err).Str("call", call).
			Msg("ft8 seq: worked-before lookup failed; answering as usual")
		return false, ""
	}
	return worked, band
}

// holdRepeatLocked opens the hold on c (no transmission to them) and publishes
// the frame that announces it, under the lock (invariant 3). Caller holds s.mu.
func (s *Sequencer) holdRepeatLocked(c *CallerExchange, band, period string, now time.Time) {
	s.repeatHold = &repeatHold{
		HeldRepeat: HeldRepeat{
			Call: c.TheirCall, Grid: c.TheirGrid, Snr: c.SendSnr,
			Band: band, Mode: s.profile.AdifMode, Submode: s.profile.AdifSubmode,
			Reason: HeldReasonWorkedBefore,
		},
		period:    period,
		lastHeard: now,
	}
	s.log.InfoWith().Str("call", c.TheirCall).Str("band", band).
		Str("mode", s.profile.AdifMode).Str("submode", s.profile.AdifSubmode).
		Msg("ft8 seq: holding a repeat — already worked on this band and mode; awaiting Answer anyway or Next")
	s.publish(s.statusLocked())
}

// expireRepeatHoldLocked drops a hold whose station has not been heard past the
// answerer staleness bound — the same bound the pick list uses, so the two
// cannot drift. Publishes when it changes anything. Caller holds s.mu.
func (s *Sequencer) expireRepeatHoldLocked(now time.Time) {
	h := s.repeatHold
	if h == nil || now.Sub(h.lastHeard) <= cqAnswererStaleAfter {
		return
	}
	s.repeatHold = nil
	s.log.InfoWith().Str("call", h.Call).
		Msg("ft8 seq: releasing the repeat hold — station unheard past the staleness bound")
	s.publish(s.statusLocked())
}

// declineRepeatLocked is Next on a hold: the station is excluded from selection
// for the rest of the run. Caller holds s.mu; the caller publishes.
func (s *Sequencer) declineRepeatLocked() {
	h := s.repeatHold
	s.repeatHold = nil
	if h == nil {
		return
	}
	if !slices.Contains(s.declinedRepeats, h.Call) {
		s.declinedRepeats = append(s.declinedRepeats, h.Call)
	}
	s.log.InfoWith().Str("call", h.Call).
		Msg("ft8 seq: repeat declined (operator pressed Next) — excluded for the rest of the run")
}

// resetRepeatStateLocked forgets the hold and the declined list — a run ended
// or a fresh one started. Caller holds s.mu.
func (s *Sequencer) resetRepeatStateLocked() {
	s.repeatHold = nil
	s.declinedRepeats = nil
}

// heldStatusLocked is the frame's view of the hold. Caller holds s.mu.
func (s *Sequencer) heldStatusLocked() *HeldRepeat {
	if s.repeatHold == nil {
		return nil
	}
	h := s.repeatHold.HeldRepeat
	return &h
}
