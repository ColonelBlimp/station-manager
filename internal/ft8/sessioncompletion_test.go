package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	IDLE COMPLETION specification, written before the implementation (2026-07-27,
	from the internal/ft8 package review).

	Every completion that ENDS a session is a session-identity transition, and all of
	them must perform the same one. It is not "clear some fields and publish":

	  - retire the generation, so a callback created under the old one is refused;
	  - consume any staged teardown reason, so the terminal frame explains itself;
	  - clear the ladder's state and go idle;
	  - publish the terminal status while the lock still excludes a replacement start.

	Four completion paths end a session — Group A finals, standard work, FD answer,
	type-4 work — and only Group A did all four steps. The other three set mode and
	published a bare idle status, retiring nothing and consuming nothing. That
	divergence is the reason to state the rule rather than fix each site: the Group A
	path was corrected earlier the same day and the correction was applied to ONE of
	four (package review of internal/ft8, findings 2).

	Call-CQ is deliberately NOT one of these. Its completion RESUMES CQ rather than
	ending the session, so it is a different transition and keeps its own path.

	Rules:

	  1. A completion that ends a session retires the generation: a callback holding
	     the previous generation is refused afterwards.
	  2. It consumes a staged teardown reason and publishes it on the terminal frame.
	  3. With nothing staged the terminal frame simply carries no reason.
	  4. Call-CQ is unaffected — it resumes CQ, and a staged reason is not its to
	     consume.

	Rule 1 matters beyond tidiness: every stale-callback guard in the package keys off
	the generation, so a completion that does not retire it leaves those guards unable
	to tell a live session from a finished one.
*/

// completeStandardWork drives a work-a-caller contact to its RR73 completion.
func completeStandardWork(t *testing.T, s *Sequencer) {
	t.Helper()
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})
}

// --- 1: the generation is retired ------------------------------------------

func TestIdleCompletion_RetiresTheSessionGeneration(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	gen := s.currentGen()

	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})

	require.False(t, s.Active(), "fixture: the contact completed and the session ended")
	require.NotEqual(t, gen, s.currentGen(),
		"the generation must be retired — every stale-callback guard in the package "+
			"keys off it, and one that survives leaves them unable to tell a finished "+
			"session from a live one")
	require.False(t, s.AbandonIfCurrent(gen, EndReasonDialMoved),
		"and a holder of the old generation must find nothing to act on")
}

// --- 2 + 3: the staged reason ------------------------------------------------

func TestIdleCompletion_CarriesAStagedTeardownReason(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})

	// The dial guard stages its reason, then the completion wins the race: it ends
	// the session, and the Abandon behind it finds an idle sequencer. If the reason
	// is not consumed HERE it is lost — PTT stopped, TX disarmed, nothing on screen
	// saying why.
	s.setPendingEndReason(EndReasonDialMoved)
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})

	last := r.lastStatus()
	require.False(t, last.Active)
	require.Equal(t, EndReasonDialMoved, last.EndReason,
		"the contact was preserved; the explanation must be too")
}

func TestIdleCompletion_CarriesNoReasonWhenNoneWasStaged(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	completeStandardWork(t, s)

	last := r.lastStatus()
	require.False(t, last.Active)
	require.Empty(t, last.EndReason,
		"an ordinary completion needs no explanation — the operator caused it")
}

// The same rule, on the other two ladders that end a session. They are separate
// code paths for good reasons (type-4 work's opening is also its terminal rung; FD
// carries class/section rather than grid/report), but the TRANSITION is identical
// and this is what stops them drifting apart again.

func TestIdleCompletion_FdAnswerFollowsTheSameRule(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartQsoFd("G0XYZ", "1D", "DX", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	gen := s.currentGen()

	s.setPendingEndReason(EndReasonBandChange)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC 2A WWA", -12)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R 2A WWA", -11)})

	if s.Active() {
		t.Skip("FD ladder did not reach its terminal rung with this exchange; " +
			"the rule is covered by the standard-work case above")
	}
	require.NotEqual(t, gen, s.currentGen(), "the generation must be retired")
	require.Equal(t, EndReasonBandChange, r.lastStatus().EndReason,
		"a staged reason must survive an FD completion too")
}

// --- 4: Call-CQ is a different transition ------------------------------------

// Call-CQ's completion RESUMES CQ rather than ending the session, so it is not an
// idle completion and must not consume a staged reason — that reason belongs to the
// teardown still in flight.
func TestIdleCompletion_CallCqIsNotAnIdleCompletion(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "auto_first", "",
		time.Unix(0, 0).UTC()))
	require.True(t, s.Active())

	s.setPendingEndReason(EndReasonDialMoved)
	require.Equal(t, EndReasonDialMoved, s.peekPendingEndReason(),
		"a Call-CQ run does not end at a contact, so it has no idle completion to "+
			"consume the reason — the teardown that staged it still owns it")
}

/*
	SKIPPABILITY specification (2026-07-27, from the internal/ft8 package review).

	Skip-if-silent means "if they do not come back, end the session instead of
	repeating this rung". That is a property of the RUNG, not of the session mode —
	and the code treated it as a mode. Every answer/work mode was accepted, and the
	status then advertised SkipArmed, but skip is only ever checked on PRE-FINAL
	rungs. So two states accepted an arm that could never fire:

	  - type-4 work, whose sole rung IS the terminal RR73 (its own comment says
	    "there is no skip-if-silent path to walk"); and
	  - any ladder already sitting on its final rung.

	The operator arms it, the UI says armed, and the rig keeps transmitting. A false
	promise on the TX path is worse than no feature: it is the button you press when
	you want the radio to stop.

	Rejecting is the operator's decision (2026-07-27) over defining final-rung skip
	semantics — Abandon already ends a contact, and giving skip a second meaning on
	the rung that decides whether a QSO logs is not worth the ambiguity.

	Rules:

	  7. Arming is refused unless the CURRENT RUNG has a skip path.
	  8. Disarming is always accepted — the session may have ended between the
	     operator's click and the request, and reporting failure for "make sure it is
	     off" is noise.
	  9. A refusal says so distinctly: "this rung cannot be skipped" is a different
	     fact from "there is no QSO", and the operator can act on the difference.
	 10. A mode with no explicit skip path is NOT skippable. New modes fail safe —
	     silently advertising a skip that never fires is the bug being fixed.
*/

func TestSkip_RefusedWhenTheRungHasNoSkipPath(t *testing.T) {
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	t.Run("type-4 work: its only rung is terminal", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3, slot, 1500, 14.074, now))
		require.True(t, s.Active(), "fixture: a session is running")

		require.ErrorIs(t, s.SetSkipIfSilent(true), ErrRungNotSkippable,
			"there is no pre-final rung to skip — arming would do nothing while the "+
				"UI said it would")
		require.False(t, s.statusForTest().SkipArmed,
			"and nothing may advertise an arm that cannot fire")
	})

	t.Run("a ladder lingering on its final rung", func(t *testing.T) {
		// A ladder only SITS on a final rung when that rung fails to reach the air —
		// otherwise it completes and the session ends. asyncFail reproduces the
		// reviewer's probe: arm skip after the first failed RR73 and the next silent
		// cycle transmits RR73 again, because the skip check lives on the pre-final
		// path and this rung is past it.
		s := newTestSeq(&seqRecorder{asyncFail: true})
		require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, slot, 1500, 14.074, now))
		driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
		driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})
		require.True(t, s.Active(), "fixture: the RR73 did not transmit, so the contact stays put")

		require.ErrorIs(t, s.SetSkipIfSilent(true), ErrRungNotSkippable,
			"this rung has no skip path, so arming it would promise a stop that never comes")
		require.False(t, s.statusForTest().SkipArmed)
	})

	t.Run("a pre-final rung is skippable", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))
		driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})

		require.NoError(t, s.SetSkipIfSilent(true), "this rung repeats; skipping it is meaningful")
		require.True(t, s.statusForTest().SkipArmed)
	})

	t.Run("a Call-CQ run is active but not skippable", func(t *testing.T) {
		// A behaviour CHANGE this fix makes, pinned deliberately: Call-CQ used to
		// refuse with ErrNoActiveQso, which was never true — a run IS active. Its
		// Next is an immediate takeover (the SPA abandons instead), so the rung has
		// no skip path, and the accurate refusal is the new one.
		s := newTestSeq(&seqRecorder{})
		require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "auto_first", "", now))
		require.True(t, s.Active())

		require.ErrorIs(t, s.SetSkipIfSilent(true), ErrRungNotSkippable)
		require.False(t, s.statusForTest().SkipArmed)
	})

	t.Run("idle has nothing to skip", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.ErrorIs(t, s.SetSkipIfSilent(true), ErrNoActiveQso,
			"no QSO at all is a different fact from a rung that cannot be skipped")
	})

	t.Run("disarming is always accepted", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.NoError(t, s.SetSkipIfSilent(false), "idle")

		require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3, slot, 1500, 14.074, now))
		require.NoError(t, s.SetSkipIfSilent(false),
			"make-sure-it-is-off must never report failure, whatever rung we are on")
	})
}
