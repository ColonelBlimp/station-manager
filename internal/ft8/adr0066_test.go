package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	ADR 0066 — FT8 run knobs are session state; config.json holds only their
	defaults. Daemon half, written before the implementation.

	ACCEPTANCE CRITERION (operator, 2026-08-08, "too confusing — simplify"):

	  When I start a Call-CQ run, it answers callers the way the SESSION's
	  Answer selector says — not the way config.json says — and changing how
	  the next run behaves never needs a config edit or a daemon restart. When
	  I arm auto-work with the session set to an auto mode, the run arms with
	  NO config knob involved; with the session on "I pick", the arm is
	  refused and the contact proceeds — and I can tell those apart from the
	  frame's auto_work_armed flag, exactly as ADR 0065's gate grammar
	  already promised.

	The confusable state each rule guards:

	  R1 — a run obeying config while the UI shows a session selector (the
	       selector would be decoration; the bug class that prompted the ADR).
	  R2 — an old client (no answer_mode field) silently losing today's
	       config-driven behaviour: empty mode MUST mean the config default.
	  R3 — the run arming from config alone: the 2026-08-08 dogfood incident
	       inverted — after this ADR the knob must be UNNECESSARY, where
	       before it was silently REQUIRED. The fixture installs NO policy;
	       only the staged session facts arm.
	  R4 — intent + "I pick": arming would advertise a run that cannot pick
	       (invariant 7). Refused, contact untouched — G3's refusing side,
	       re-derived from the policy knob to the session mode.
	  R5 — the armed run selecting with a GLOBAL mode while the session
	       carried another: the run's selection mode is per-run state, pinned
	       by a fixture where first-by-decode-order and strongest DIFFER.
	  R6 — a staged mode leaking between operator actions: every start stages
	       fresh (the staged-setter contract, inherited).
*/

// startCqWithMode is the service-level start used by R1/R2/R6: TX armed, then
// StartCallCq carrying the session's answer mode.
func startCqWithMode(t *testing.T, s *Service, mode string) {
	t.Helper()
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) })
	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, mode, "", 1))
}

// R1 — the session's mode wins over config.
func TestAdr0066_RequestModeOverridesConfig(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	s.cfg.TX.CallerAnswerMode = types.Ft8CallerAnswerOperatorPick
	startCqWithMode(t, s, types.Ft8CallerAnswerAutoFirst)
	require.Equal(t, types.Ft8CallerAnswerAutoFirst, s.seq.statusForTest().AnswerMode,
		"the frame must name the SESSION's mode, not config's")
}

// R2 — an empty mode is the config default (old clients keep today's shape).
// Config is set to a NON-default literal so this cannot pass by accident.
func TestAdr0066_EmptyModeFallsBackToConfigDefault(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	s.cfg.TX.CallerAnswerMode = types.Ft8CallerAnswerAutoStrongest
	startCqWithMode(t, s, "")
	require.Equal(t, types.Ft8CallerAnswerAutoStrongest, s.seq.statusForTest().AnswerMode)
}

// stagedRun drives an operator-started work-a-caller QSO to completion with the
// session facts staged directly — NO SetAutoWorkCallers, no config knob. This is
// the post-0066 shape of autowork_test.go's autoWorkRun helper.
func stagedRun(t *testing.T, s *Sequencer, mode string) {
	t.Helper()
	s.setPendingAnswerMode(mode) // ADR 0067: the mode IS the arming input
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})
	require.Nil(t, s.caller, "fixture: the seeding QSO must complete")
}

// R3 — the session facts alone arm the run; no config policy exists to consult.
func TestAdr0066_SessionFactsArmWithoutAnyConfigKnob(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedRun(t, s, "auto_first")
	require.True(t, s.AutoWorkArmed(),
		"intent + an auto session mode must arm with no config knob involved")
}

// R4 — re-derived by ADR 0067: "I pick" now arms a LISTING run beside the
// contact (the old arm-refusal is superseded — the run advertises exactly
// what it does). The contact proceeds; adr0067_test.go A3 pins that the
// listing run transmits nothing without a pop.
func TestAdr0066_PickModeArmsAListingRunContactProceeds(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.setPendingAnswerMode("operator_pick")
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active(), "the contact itself must start")
	require.True(t, s.AutoWorkArmed(), "a listing run is armed beside it")
}

// R5 — the armed run selects with the mode the SESSION carried. Two callers,
// the weaker first by decode order: auto_first would take M0AAA (-15); the
// carried auto_strongest must take DL9UW (-3).
func TestAdr0066_RunSelectsWithTheCarriedMode(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedRun(t, s, "auto_strongest")
	driveTheir(s, 90, []goft8.DecodedMessage{
		dm("G0XYZ M0AAA IO83", -15),
		dm("G0XYZ DL9UW JO41", -3),
	})
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall,
		"the run must select with the carried auto_strongest, not a global default")
}

// R6 — the staged mode does not leak between operator actions: a second CQ
// start staging a different mode publishes ITS mode.
func TestAdr0066_StagedModeDoesNotLeakBetweenStarts(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	s.cfg.TX.CallerAnswerMode = types.Ft8CallerAnswerAutoFirst
	startCqWithMode(t, s, types.Ft8CallerAnswerOperatorPick)
	require.Equal(t, types.Ft8CallerAnswerOperatorPick, s.seq.statusForTest().AnswerMode)
	s.AbandonQso()
	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1))
	require.Equal(t, types.Ft8CallerAnswerAutoFirst, s.seq.statusForTest().AnswerMode,
		"the second start's empty mode must resolve to config, not inherit the first's")
}
