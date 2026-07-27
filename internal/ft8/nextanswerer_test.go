package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	CALL-CQ "NEXT" specification, written before the implementation (2026-07-27).

	Operator statement of the need: "when a QSO is stuck on a rung (the default is to
	try N times) — I want to short-circuit the retries and move to the next in the
	auto_first or (if there is no other stations waiting) go back to calling CQ."

	So Next is NOT "abandon this station" and NOT a second Abandon. It is the repeat
	cap, fired early. Everything the cap already does on a stuck answerer —
	parkAnswererLocked: park it, take another live answerer from that slot, else
	resume CQ — is what Next must do, at the moment the operator can already SEE the
	contact is going nowhere instead of after the remaining retries.

	Two things follow from that framing, and both are the opposite of what an earlier
	draft of this design assumed:

	  - The exclusion keeps the AUTOMATIC path's per-round lifetime. An earlier draft
	    argued for a session-scoped exclusion on the premise that Next meant "I do not
	    want this station". It does not — it means "I am not waiting out the retries".
	    So the empty-rescan clear in parkAnswererLocked stays correct: if the parked
	    station is the ONLY one calling and it answers the next CQ, working it is the
	    right outcome, not a bug.

	  - It CANNOT be skip-if-silent. Skip fires on a silent cycle; a stuck station is
	    not silent — it re-sends the same rung every slot (see
	    TestCallerSequencer_AbandonExcludesStalledAnswererFromRescan). Wiring Next to
	    the skip flag would produce a control that never fires in the exact case it
	    exists for. The trigger is "did not ADVANCE", not "did not transmit".

	Why it takes effect at the next slot evaluation rather than instantly: picking a
	replacement requires a slot's decodes to pick FROM. onSlotCalling runs on the
	answerers' slot and decides what we transmit next; a park raised mid-slot has no
	candidate list, so it would find nobody, clear the exclusion and resume CQ — and
	the stuck station would win the very next pick. Deferring to the next evaluation
	makes Next exactly "the cap fired one slot early" with no new selection path.

	Rules:

	 1. Next parks the current answerer at the next evaluation and the RUN CONTINUES —
	    a Call-CQ run never goes idle from Next (that is Abandon's job).
	 2. The parked answerer is not re-picked by that evaluation, even though it is
	    still calling and would otherwise win auto_first by decode order.
	 3. Another live answerer in that slot is worked immediately — no CQ wasted
	    between the two contacts (matches the cap path).
	 4. With nobody else calling, CQ resumes AND the exclusion clears, so a station
	    that is the only one answering is not locked out of the rest of the run.
	 5. A pending Next fires even though the stuck station keeps TRANSMITTING. This is
	    the rule that separates Next from skip-if-silent; a test that only proves the
	    silent case proves nothing about the case Next is for.
	 6. A pending Next is CANCELLED if the contact advances — the reason for pressing
	    it is gone. (Mirrors skip's existing disarm-on-reply.)
	 7. Next while calling CQ with no contact in progress is refused distinctly:
	    there is no answerer to park, and a CQ run is not "no active QSO".
	 8. Next outside a Call-CQ run is refused — answer/work sessions have skip, whose
	    deferred-end semantics are a different control.
	 9. Abandon still ends the whole run, and clears any pending Next with it.
	10. The pending state is observable in the published status, so the operator can
	    see the press landed on a control that only acts a slot later.

	The AUTOMATIC cap path must be unchanged; it is already pinned by
	TestCallerSequencer_DropsSilentAnswererResumesCq,
	TestCallerSequencer_AbandonExcludesStalledAnswererFromRescan and
	TestCallerSequencer_StalledSetRotatesPastMultipleStallers, which stay green.
*/

// stuckAnswerer sets up a Call-CQ run with DL9UW answering and then stalling —
// re-sending its grid instead of rogering, which is the condition Next is for.
func stuckAnswerer(t *testing.T, s *Sequencer) {
	t.Helper()
	startCq(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.Equal(t, "DL9UW", s.caller.TheirCall, "fixture: DL9UW is the live contact")
}

// --- 1 + 2 + 3: park, exclude, take the live answerer ------------------------

func TestNext_ParksTheAnswererAndKeepsTheRunGoing(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stuckAnswerer(t, s)

	require.NoError(t, s.NextAnswerer())

	// DL9UW is STILL calling and is first by decode order — auto_first would re-lock
	// onto it without the exclusion, and Next would visibly do nothing.
	driveTheir(s, 60, []goft8.DecodedMessage{
		dm("7Q5MLV DL9UW JO41", -8), // the stuck one, still transmitting
		dm("7Q5MLV 9A4ZM JN95", -6), // a live answerer behind it
	})

	require.True(t, s.Active(), "Next must not end the run — that is Abandon")
	require.NotNil(t, s.caller, "the run keeps working the pile-up")
	require.Equal(t, "9A4ZM", s.caller.TheirCall,
		"the parked station must be excluded from this rescan")

	sent := r.sentMsgs()
	require.Equal(t, "9A4ZM 7Q5MLV -06", sent[len(sent)-1],
		"the replacement is worked in this very slot")
	require.NotContains(t, sent, "CQ 7Q5MLV KH78",
		"no CQ wasted when someone else is already calling")
}

// --- 4 + 5: nobody else calling → CQ resumes, and the exclusion clears --------

// This is the rule an implementation is most likely to get wrong in BOTH directions:
// re-picking the stuck station immediately (no exclusion at all), or locking it out
// for the rest of the run (session-scoped exclusion). Neither is what the cap does.
func TestNext_ResumesCqWhenNobodyElseIsCalling(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stuckAnswerer(t, s)

	require.NoError(t, s.NextAnswerer())

	// Rule 5: DL9UW is NOT silent — it re-sends the same rung. Next must still fire.
	// A skip-if-silent implementation passes every other test in this file and fails
	// this one.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})

	require.True(t, s.Active(), "the run continues")
	require.Nil(t, s.caller, "the stuck contact is parked")
	sent := r.sentMsgs()
	require.Equal(t, "CQ 7Q5MLV KH78", sent[len(sent)-1],
		"with nobody else calling, we go back to calling CQ")

	// Rule 4: and the parked station is not locked out — it answers the next CQ and
	// is worked, exactly as it would be after an automatic cap park.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.NotNil(t, s.caller,
		"the exclusion is per-round; a station that is the only one calling must not "+
			"be locked out for the rest of the session")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// --- 11: the closing rung caps too, so Next must reach it --------------------

// Both of onSlotCalling's capped branches are rungs a contact can get stuck on, and
// Next is "the cap, fired early" — so it has to fire on both. The closing RR73 is the
// one with teeth: the cap there drops the contact WITHOUT logging (Group B — they
// never got the roger, so neither side has a QSO), and Next inherits that. Worth
// pinning explicitly rather than inferring, because the cost of pressing Next differs
// between the two rungs even though the mechanism is identical.
//
// A contact only LINGERS on the closing rung when the RR73 fails to reach the air, so
// asyncFail is what puts us there — and the comment on that branch says why an
// unbounded retry is worse here than anywhere else: "the whole CQ loop would freeze on
// one station and the rest of the pile-up goes unworked". Short-circuiting it serves
// exactly that purpose.
func TestNext_FiresOnTheClosingRungToo(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)
	startCq(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-12", -8)})
	require.NotNil(t, s.caller, "fixture: the RR73 did not transmit, so the contact stays put")
	require.Equal(t, cqRogering, s.caller.State, "fixture: stuck on the closing rung")

	require.NoError(t, s.NextAnswerer())
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-12", -8)})

	require.True(t, s.Active(), "the run continues")
	require.Nil(t, s.caller, "the contact is parked so the pile-up is not frozen behind it")
	require.Empty(t, r.completed,
		"nothing is logged: they never received the roger, so neither side has a QSO")
}

// --- 6: advancing cancels a pending Next -------------------------------------

func TestNext_IsCancelledWhenTheContactAdvances(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stuckAnswerer(t, s)

	require.NoError(t, s.NextAnswerer())
	require.True(t, s.statusForTest().NextArmed,
		"fixture: with nothing pending, an advancing contact completes anyway and this "+
			"test would pass against an unimplemented Next")

	// They finally roger. On the caller ladder that is not merely an advance — the
	// next rung is the RR73, which transmits and LOGS. So the observable rule is that
	// a pending Next must not cost us a contact that was about to complete.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-12", -8)})

	require.Len(t, r.completed, 1,
		"the rung was not stuck after all; parking here would throw away a made QSO")
	require.Equal(t, "DL9UW", r.completed[0].TheirCall)
	require.False(t, s.statusForTest().NextArmed, "the pending Next is cleared")

	// And nothing leaked: the next answerer is worked normally rather than being
	// parked by a Next that was never consumed.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV 9A4ZM JN95", -6)})
	require.NotNil(t, s.caller, "a stale pending Next must not park the NEXT contact")
	require.Equal(t, "9A4ZM", s.caller.TheirCall)
}

// --- 7 + 8: refusals ----------------------------------------------------------

func TestNext_RefusedWhenThereIsNoAnswererToPark(t *testing.T) {
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	t.Run("calling CQ with no contact in progress", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		startCq(t, s)
		require.True(t, s.Active(), "a CQ run IS active — this is not ErrNoActiveQso")

		require.ErrorIs(t, s.NextAnswerer(), ErrNoAnswerer,
			"nothing is being worked, so there is nothing to park")
	})

	t.Run("answering a CQ uses skip, not Next", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))
		driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})

		require.ErrorIs(t, s.NextAnswerer(), ErrNoAnswerer,
			"an answer-a-CQ session has no pile-up to move on to; its Next is skip")
	})

	t.Run("idle", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.ErrorIs(t, s.NextAnswerer(), ErrNoAnswerer)
	})
}

// --- 9: Abandon still ends everything ----------------------------------------

func TestNext_AbandonStillEndsTheWholeRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stuckAnswerer(t, s)

	require.NoError(t, s.NextAnswerer())
	require.True(t, s.statusForTest().NextArmed,
		"fixture: without a Next actually pending this test proves nothing about "+
			"Abandon clearing one")
	s.Abandon()

	require.False(t, s.Active(), "Abandon ends the CQ run, unlike Next")
	require.False(t, s.statusForTest().NextArmed,
		"a pending Next must not survive into a later session")
}

// --- 10: the press is visible ------------------------------------------------

// Next only acts at the next slot evaluation, so without a published pending state
// the operator gets no feedback for up to ~15 s and presses it again.
func TestNext_PendingStateIsPublished(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stuckAnswerer(t, s)

	require.False(t, r.lastStatus().NextArmed, "fixture: nothing pending yet")
	require.NoError(t, s.NextAnswerer())
	require.True(t, r.lastStatus().NextArmed,
		"the operator must see the press landed on a deferred control")
}
