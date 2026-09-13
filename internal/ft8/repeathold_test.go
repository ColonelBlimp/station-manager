package ft8

import (
	"context"
	"errors"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	SAME-BAND/PROFILE REPEAT HOLD (operator ruling 2026-09-13, W-0019).

	Contest 2026-09-12: the Call-CQ run worked ZS6BOS twice on 20 m and twice on
	40 m, an hour apart, answering our CQ each time — the run had no view of the
	logbook. Ruling: surface a same-band/profile repeat in the ladder; never
	auto-skip and never auto-answer it. The run HOLDS before TX to that station,
	marks it as already worked, and the operator chooses Answer anyway or Next. A
	failed or unknown lookup must not block operation.

	Shape (stated, not assumed): the hold blocks answering THAT station only — the
	run otherwise carries on (a CQ run keeps calling; an auto-work run keeps
	listening and works other callers), so a hold can never stall a run. It ends
	on Answer anyway (the station is committed with the operator's explicit
	allow-duplicate), on Next (the station is excluded from selection for the
	rest of the run), when the station stops calling (the answerer staleness
	bound), or when the run ends.
*/

// workedOnly makes the seam say "worked before" for exactly these calls, recording
// what it was asked, so a test can check the axis (band, profile) too.
func workedOnly(s *Sequencer, calls ...string) *[]workedQuery {
	var asked []workedQuery
	s.workedBefore = func(_ context.Context, logbookID int64, call, band, mode, submode string) (bool, error) {
		asked = append(asked, workedQuery{logbookID, call, band, mode, submode})
		for _, c := range calls {
			if c == call {
				return true, nil
			}
		}
		return false, nil
	}
	return &asked
}

type workedQuery struct {
	logbookID                 int64
	call, band, mode, submode string
}

// ourSlotAfter is 1 s into OUR slot following the worked station's slot at `sec`
// (the instant driveTheir evaluates), for intents that fire an opening rung.
func ourSlotAfter(sec int64) time.Time {
	return time.Unix(sec+int64(ProfileFT8.Slot/time.Second)+1, 0).UTC()
}

// H1 — a repeat is HELD, not answered: nothing transmits, the frame names the
// station, the reason and the axis it was checked on.
func TestRepeatHold_AutoWorkHoldsAWorkedCallerInsteadOfAnswering(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.mu.Lock()
	s.pendingLogbookID = 7 // what the next contact would log to
	s.mu.Unlock()
	asked := workedOnly(s, "W9XYZ")
	autoWorkRun(t, s, "auto_first")
	before := len(r.sentMsgs())

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})

	require.Len(t, r.sentMsgs(), before, "a worked station must not be answered without the operator")
	require.Nil(t, s.caller, "no contact may be committed while held")
	st := r.lastStatus()
	require.NotNil(t, st.Held, "the frame must carry the hold")
	require.Equal(t, "W9XYZ", st.Held.Call)
	require.Equal(t, "EM12", st.Held.Grid)
	require.Equal(t, HeldReasonWorkedBefore, st.Held.Reason)
	require.Equal(t, "20m", st.Held.Band)
	require.Equal(t, "FT8", st.Held.Mode)
	require.Equal(t, "", st.Held.Submode)
	require.True(t, st.AutoWorkArmed, "the run stays armed — a hold never stops it")
	require.NotEmpty(t, *asked)
	require.Equal(t, workedQuery{7, "W9XYZ", "20m", "FT8", ""}, (*asked)[len(*asked)-1])
}

// H2 — Answer anyway: the pick intent on the held call commits and transmits, and
// the completed contact carries the operator's explicit allow-duplicate.
func TestRepeatHold_AnswerAnywayCommitsWithAllowDuplicate(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	workedOnly(s, "W9XYZ")
	autoWorkRun(t, s, "auto_first")
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})
	require.NotNil(t, r.lastStatus().Held)

	require.NoError(t, s.PickAnswerer("W9XYZ", ourSlotAfter(90)))

	require.Nil(t, r.lastStatus().Held, "the hold ends with the decision")
	require.NotNil(t, s.caller)
	require.Equal(t, "W9XYZ", s.caller.TheirCall)
	require.Equal(t, "W9XYZ G0XYZ -08", lastSent(r), "the ladder starts with our report")
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ W9XYZ R-05", -7)})
	require.Len(t, r.completed, 2)
	require.True(t, r.completed[1].AllowDuplicate, "an operator-chosen repeat must reach the sink as an explicit repeat")
}

// H3 — Next: the held station is excluded for the rest of the run; another,
// unworked caller is still worked as usual.
func TestRepeatHold_NextDeclinesTheStationForTheRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	workedOnly(s, "W9XYZ")
	autoWorkRun(t, s, "auto_first")
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})
	require.NotNil(t, r.lastStatus().Held)

	require.NoError(t, s.NextAnswerer())
	require.Nil(t, r.lastStatus().Held)
	before := len(r.sentMsgs())

	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)}) // they keep calling
	require.Nil(t, s.caller, "a declined repeat is not answered again this run")
	require.Nil(t, r.lastStatus().Held, "…and not held again either")
	require.Len(t, r.sentMsgs(), before)

	driveTheir(s, 150, []goft8.DecodedMessage{
		dm("G0XYZ W9XYZ EM12", -8),
		dm("G0XYZ DL9UW JO41", -9), // not worked before
	})
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall, "the run carries on with other callers")
}

// H4 — a failed lookup never blocks: the station is answered as if unknown.
func TestRepeatHold_LookupFailureAnswersAsUsual(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.workedBefore = func(context.Context, int64, string, string, string, string) (bool, error) {
		return false, errors.New("db away")
	}
	autoWorkRun(t, s, "auto_first")

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})

	require.NotNil(t, s.caller)
	require.Equal(t, "W9XYZ", s.caller.TheirCall)
	require.Nil(t, r.lastStatus().Held)
}

// H5 — a hold never stalls the run: while W9XYZ is held, an unworked caller in a
// later slot is worked; the hold is cleared by that commit. And a hold whose
// station falls silent expires on the answerer staleness bound.
func TestRepeatHold_RunCarriesOnAndAStaleHoldExpires(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	workedOnly(s, "W9XYZ")
	autoWorkRun(t, s, "auto_first")
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})
	require.NotNil(t, r.lastStatus().Held)

	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -9)})
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall, "another caller is worked while a repeat is held")
	require.Nil(t, r.lastStatus().Held, "committing a contact ends the hold")

	// A fresh hold, then silence past the staleness bound.
	driveTheir(s, 150, []goft8.DecodedMessage{dm("G0XYZ DL9UW R-05", -7)}) // completes DL9UW
	require.Nil(t, s.caller)
	driveTheir(s, 180, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})
	require.NotNil(t, r.lastStatus().Held)
	late := 180 + int64(cqAnswererStaleAfter/time.Second) + 30
	late -= late % 30 // keep the worked station's even parity
	driveTheir(s, late, nil)
	require.Nil(t, r.lastStatus().Held, "a hold whose station stopped calling expires")
}

// H6 — the Call-CQ run: a worked answerer is held (no report goes out), the CQ
// continues, and Answer anyway commits the contact on the next slot with the
// explicit allow-duplicate.
func TestRepeatHold_CallCqRunHoldsAWorkedAnswerer(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	workedOnly(s, "W9XYZ")
	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "auto_first", "", time.Unix(0, 0).UTC(), ""))

	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})
	require.Nil(t, s.caller, "a worked answerer is not committed")
	st := r.lastStatus()
	require.NotNil(t, st.Held)
	require.Equal(t, "W9XYZ", st.Held.Call)
	require.Equal(t, "caller", st.Role, "the CQ run is still the live session")
	for _, m := range r.sentMsgs() {
		require.NotContains(t, m, "W9XYZ", "no report to the held station")
	}

	require.NoError(t, s.PickAnswerer("W9XYZ", ourSlotAfter(30)))
	require.NotNil(t, s.caller)
	require.Equal(t, "W9XYZ", s.caller.TheirCall)
	require.Nil(t, r.lastStatus().Held)
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)}) // our report goes out this slot
	require.Equal(t, "W9XYZ G0XYZ -08", lastSent(r))
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ W9XYZ R-05", -7)})
	require.Len(t, r.completed, 1)
	require.True(t, r.completed[0].AllowDuplicate)
}

// H7 — no seam wired (a daemon without a logbook view): nothing is held.
func TestRepeatHold_NoSeamMeansNoHold(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ W9XYZ EM12", -8)})
	require.NotNil(t, s.caller)
	require.Nil(t, r.lastStatus().Held)
}

// H8 (codex 76e28cb7 P1) — a second repeat is not answered while another is held:
// every candidate is classified; a known repeat is skipped when the one hold is
// taken, an unworked station still goes through. And a worked station arriving in
// a LATER slot while the hold stands is skipped, not committed.
func TestRepeatHold_SecondRepeatIsNeverAutoAnsweredWhileOneIsHeld(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	workedOnly(s, "W9XYZ", "W8REP")
	autoWorkRun(t, s, "auto_first")
	before := len(r.sentMsgs())

	driveTheir(s, 90, []goft8.DecodedMessage{
		dm("G0XYZ W9XYZ EM12", -8), // repeat → held
		dm("G0XYZ W8REP EM13", -9), // repeat too → skipped, never committed
	})
	require.Nil(t, s.caller, "a second repeat must not be answered while another is held")
	require.Len(t, r.sentMsgs(), before)
	require.Equal(t, "W9XYZ", r.lastStatus().Held.Call)

	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ W8REP EM13", -9)}) // later slot, hold still on W9XYZ
	require.Nil(t, s.caller, "a worked station in a later slot is skipped while the hold stands")
	require.Equal(t, "W9XYZ", r.lastStatus().Held.Call, "the hold stays on the first repeat")

	driveTheir(s, 150, []goft8.DecodedMessage{
		dm("G0XYZ W8REP EM13", -9),
		dm("G0XYZ DL9UW JO41", -7), // not worked before
	})
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall, "an unworked station still goes through")
}
