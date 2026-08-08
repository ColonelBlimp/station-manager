package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	ADR 0067 slice A — one rule, five entry points: the Answer mode ALONE
	decides how every run treats callers. Written before the implementation.

	ACCEPTANCE CRITERION (operator, 2026-08-08, worked case by case):

	  However I start a session, stations that then call me are treated per
	  the session's Answer mode and nothing else: auto modes run hands-off in
	  their order with NO arming gesture; under "I pick" nothing transmits
	  beyond the contact I started until I choose — callers appear in one
	  list, the same list, whatever the entry point.

	This SUPERSEDES the ADR 0065 per-click intent grammar (checkbox, chord,
	auto_work wire flag, arm-refusal). The confusable each rule guards:

	  A1 — a run needing a leftover arming gesture: the intent grammar
	       surviving in disguise. The fixture stages NO intent — the staged
	       session mode alone must arm.
	  A2 — "plain start works one station only" (the OLD rule) surviving: a
	       plain answer under an auto mode must arm the run.
	  A3 — the pick asymmetry surviving: a completed work-caller contact
	       under "I pick" must leave a LISTING run — callers get listed on
	       frames, and NOTHING transmits (the licensing-critical half; a
	       transmission here would be automation the operator never chose).
	  A4 — the pop staying CQ-only: popping a listed caller from the idle
	       listing run must commit and work them.
	  A5 — mid-contact blindness: a second caller arriving WHILE the pick
	       contact is worked must join the list (the CQ run's rule 9,
	       generalised).
	  A6 — list/run leakage: a fresh operator start replaces the run and
	       clears the list — a station listed for the OLD session must not
	       resurrect into the new one.
	  A7 — FD/type-4 arming: still never (ADR 0059 scope note) — an FD start
	       under an auto session mode arms nothing.
*/

// stagedStart begins an operator-started work-caller session with ONLY the
// session mode staged — no intent (the grammar this ADR retires).
func stagedStart(t *testing.T, s *Sequencer, mode string) {
	t.Helper()
	s.setPendingAnswerMode(mode)
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
}

func completeSeedContact(t *testing.T, s *Sequencer) {
	t.Helper()
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})
	require.Nil(t, s.caller, "fixture: the seeding QSO must complete")
}

// A1 — the staged session mode alone arms; no intent exists to stage.
func TestAdr0067_AutoModeAloneArmsTheRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedStart(t, s, "auto_first")
	completeSeedContact(t, s)
	require.True(t, s.AutoWorkArmed(),
		"an auto session mode must arm the run with no arming gesture")

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NotNil(t, s.caller, "the run must pick the next caller up")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// A2 — a PLAIN answer-a-CQ start under an auto mode arms too (supersedes
// ADR 0065 decision 1's "plain click works one station only").
func TestAdr0067_PlainAnswerUnderAutoModeArms(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.setPendingAnswerMode("auto_first")
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	completeSeedContact(t, s)
	require.True(t, s.AutoWorkArmed(),
		"answering a CQ under an auto mode must leave the run armed")
}

// A3 — "I pick" leaves a LISTING run: callers are listed on the published
// frames and NOTHING is transmitted at them.
func TestAdr0067_PickLeavesAListingRunThatNeverTransmits(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedStart(t, s, "operator_pick")
	completeSeedContact(t, s)

	before := len(r.sentMsgs())
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ M0AAA IO83", -15)})

	st := r.lastStatus()
	calls := make([]string, 0, len(st.Answerers))
	for _, a := range st.Answerers {
		calls = append(calls, a.Call)
	}
	require.ElementsMatch(t, []string{"DL9UW", "M0AAA"}, calls,
		"both callers must be LISTED on the frame")
	require.Len(t, r.sentMsgs(), before, "a listing run transmits NOTHING on its own")
	require.Nil(t, s.caller, "no contact may be committed without a pop")
}

// A4 — the pop generalises: picking a listed caller from the idle listing
// run commits and works them.
func TestAdr0067_PopFromListingRunWorksTheCaller(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedStart(t, s, "operator_pick")
	completeSeedContact(t, s)
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(95, 0).UTC()),
		"the pop must accept an idle listing run, not just a CQ run")
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall, "the popped caller is worked")
}

// A5 — the list stays warm DURING the pick contact (the CQ run's rule 9
// generalised): a second caller arriving mid-contact joins the list.
func TestAdr0067_ListCollectsDuringThePickContact(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedStart(t, s, "operator_pick")
	// Mid-contact: K1ABC answers our report; M0AAA calls us in the same slot.
	driveTheir(s, 30, []goft8.DecodedMessage{
		dm("G0XYZ K1ABC FN42", -12),
		dm("G0XYZ M0AAA IO83", -15),
	})
	st := r.lastStatus()
	require.NotEmpty(t, st.Answerers, "a caller arriving mid-contact must be listed")
	require.Equal(t, "M0AAA", st.Answerers[0].Call)
}

// A6 — a fresh operator start replaces the run and clears the list.
func TestAdr0067_FreshStartClearsTheListedCallers(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedStart(t, s, "operator_pick")
	completeSeedContact(t, s)
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NotEmpty(t, r.lastStatus().Answerers, "fixture: a caller is listed")

	// The operator starts a NEW session at a different station and completes
	// it (a pop mid-contact is refused by the ratified refuse-mid-contact
	// rule, so the no-resurrection check happens once the run is idle).
	s.setPendingAnswerMode("operator_pick")
	require.NoError(t, s.StartWorkCaller("G0XYZ", "W1AW", "FN31", -5,
		time.Unix(90, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(90, 0).UTC()))
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ W1AW FN31", -5)})
	driveTheir(s, 150, []goft8.DecodedMessage{dm("G0XYZ W1AW R-08", -4)})
	require.Nil(t, s.caller, "fixture: the second QSO must complete")
	require.ErrorIs(t, s.PickAnswerer("DL9UW", time.Unix(155, 0).UTC()), ErrAnswererNotListed,
		"the old session's listed caller must not resurrect into the new run")
}

// A7 — FD starts still arm nothing, whatever the session mode (ADR 0059
// scope note: only the standard ladders head runs).
func TestAdr0067_FdStartArmsNothing(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.setPendingAnswerMode("auto_first")
	require.NoError(t, s.StartQsoFd("G0XYZ", "2A", "DX", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.False(t, s.AutoWorkArmed(), "an FD start must not arm a run")
}
