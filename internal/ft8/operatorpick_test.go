package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	OPERATOR_PICK specification (ADR 0065 decision 3), written before the
	implementation (2026-08-07).

	Operator statement of the need: during a Call-CQ run, answerers queue into the
	pile-up drawer instead of being auto-committed; the CQ keeps calling until the
	operator pops one; the run works that station and resumes CQ after. Four forks
	ratified at build time (2026-08-07, all recommended options): candidates expire
	after 3 min unheard (matching the SPA's act-on-decode staleness bound); a pop
	while a contact is in flight is REFUSED distinctly (Next already parks); a new
	answerer announces itself by badge only; the mode stays config.json-only.

	Acceptance criteria (operator-observable, ATDD):

	  When I call CQ with caller_answer_mode=operator_pick, stations that answer
	  appear in the pile-up drawer and my next slot still transmits CQ — and I can
	  tell it apart from auto_first, where the reply would transmit instead. When I
	  click a listed answerer, my next slot transmits the report to that station,
	  the QSO logs exactly once on RR73, and the run resumes CQ — and I can tell a
	  refused pop's three causes apart: no pick run, station no longer listed,
	  contact already in flight.

	Rules:

	 1. (service level) A Call-CQ start with operator_pick is ACCEPTED — this
	    deliberately flips review H2's TestStartCallCq_RejectsOperatorPick, which
	    pinned the rejection "until the stack exists". The stack now exists. The
	    session calls CQ and works nobody without a pop.
	 2. An answerer is LISTED, not worked. The fixture is the exact one
	    TestCallerSequencer_HappyPathLoops proves auto_first commits on — under
	    operator_pick the frame stays calling-cq, carries the candidate (call+SNR)
	    and answer_mode, and the slot's transmission is the CQ. The frame is the
	    observable (codex P1 on 7de6708e): the drawer renders from it, so internal-
	    state assertions prove too little.
	 3. A pop commits the candidate into the run: the frame published BY THE POP
	    (under the sequencer lock — invariant 3) shows the contact on the reporting
	    rung with the popped call gone from the list, and the next slot evaluation
	    transmits the report to that station.
	 4. A popped contact completes exactly as an auto-picked one: RR73 logs the QSO
	    once and the run resumes CQ — and candidates collected meanwhile survive
	    the completion, ready for the next pop.
	 5. The three refusals are DISTINCT errors: ErrNoCqPickRun when idle OR when
	    the run is an auto mode (a pop against auto_first is client drift, not a
	    listing miss); ErrAnswererNotListed for a call not on the list;
	    ErrCqContactInFlight while a contact is being worked. Distinct because the
	    operator's next action differs: nothing / wait for a fresh answer / finish
	    or press Next first.
	 6. A candidate unheard for more than cqAnswererStaleAfter (3 min) leaves the
	    list — checked at slot evaluations AND at the pop itself, so a pop between
	    slots cannot commit a station that has been gone longer than the bound.
	    At exactly the bound it is still listed (the SPA's 3-min rule is "older
	    than", so the daemon matches).
	 7. A park under operator_pick (Next, or the repeat cap) resumes CQ and NEVER
	    auto-picks — the fixture offers a second live answerer that auto modes
	    provably take (TestNext_ParksTheAnswererAndKeepsTheRunGoing). And the
	    parked station is NOT blacklisted: still calling, it relists at the next
	    slot evaluation. Same reasoning as Next rule 4 — the exclusion machinery
	    means "not waiting out the retries", never "I don't want this station",
	    and under operator_pick the operator IS the selector, so stalledCalls /
	    cooloff do not filter the list at all (they exist to stop AUTO re-lock).
	 8. An answerer whose reply does not encode (compound/portable) is never
	    listed — a pop of it would hand seqTransmit a terminal ErrTxBadMessage and
	    end the whole run (review M2's hazard, moved from pick time to list time).
	    The fixture pairs it with an encodable answerer as the positive control.
	 9. Candidates keep accumulating while a popped contact is being worked (they
	    are still calling; the list must be ready when CQ resumes) — but the
	    in-flight station is never its own candidate, even when it repeats its
	    grid answer (it does, whenever it misses our report).
	10. Abandon clears the list with the run: the terminal frame carries no
	    answerers, and a fresh run starts empty — pinned with stale entries left
	    behind, which an implementation that forgot the StartCallCq reset would
	    render on the new run's first frame.

	Wire shape this file pins: QsoStatus.answer_mode (caller frames) and
	QsoStatus.answerers = [{call, snr}] (operator_pick caller frames, omitted when
	empty). Expiry threshold: cqAnswererStaleAfter = 3 min (operator-ratified
	2026-08-07; do not change without asking).
*/

// startCqPick begins an operator_pick Call-CQ run with theirPeriod forced to
// "even" so the even-sec driveTheir helper drives the answerers' slots (same
// parity setup as startCq).
func startCqPick(t *testing.T, s *Sequencer) {
	t.Helper()
	require.NoError(t, s.StartCallCq("7Q5MLV", "KH78", 2700, 28.074,
		types.Ft8CallerAnswerOperatorPick, "", time.Unix(0, 0).UTC()))
	require.Equal(t, "even", s.theirPeriod)
}

// --- 1: the service accepts operator_pick (flips review H2) ------------------

func TestOperatorPick_StartCallCqIsAccepted(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	s.cfg.TX.CallerAnswerMode = types.Ft8CallerAnswerOperatorPick
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", 1),
		"operator_pick is implemented; the H2 rejection must be gone")
	require.True(t, s.seq.Active(), "the run starts and calls CQ")
	require.Equal(t, types.Ft8CallerAnswerOperatorPick, s.seq.statusForTest().AnswerMode,
		"the frame names the mode, so the SPA can tell a pick run from an auto one")
}

// --- 2: an answerer is listed, not worked ------------------------------------

func TestOperatorPick_AnswererIsListedNotWorked(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)

	// The exact fixture auto_first commits on (TestCallerSequencer_HappyPathLoops).
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})

	st := r.lastStatus()
	require.Equal(t, "", st.TheirCall, "no contact committed without a pop")
	require.Equal(t, "calling-cq", st.State)
	require.Equal(t, types.Ft8CallerAnswerOperatorPick, st.AnswerMode)
	require.Equal(t, []CqAnswerer{{Call: "DL9UW", Snr: -8}}, st.Answerers,
		"the answerer is offered on the frame the drawer renders from")
	sent := r.sentMsgs()
	require.Equal(t, "CQ 7Q5MLV KH78", sent[len(sent)-1],
		"the CQ keeps calling — auto_first would have transmitted DL9UW's report here")
}

// --- 3: a pop commits the candidate ------------------------------------------

func TestOperatorPick_PopCommitsTheCandidate(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	// Load-bearing fixture assertion: without it this test PASSED against the
	// pre-operator_pick code, whose slot evaluation auto-committed DL9UW (any
	// non-auto_strongest mode ran as auto_first) — every post-pop assertion then
	// held with the pop a no-op stub. The commit must come from the POP.
	require.Equal(t, "", r.lastStatus().TheirCall,
		"fixture: nothing may be committed before the pop")

	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(50, 0).UTC()))

	// The pop's own frame is the observable: the SPA learns the commit from it.
	st := r.lastStatus()
	require.Equal(t, "DL9UW", st.TheirCall)
	require.Equal(t, "reporting", st.State)
	require.Empty(t, st.Answerers, "the popped station leaves the list")

	// Next slot evaluation transmits the report to the popped station.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	sent := r.sentMsgs()
	require.Equal(t, "DL9UW 7Q5MLV -08", sent[len(sent)-1],
		"the run now works the operator's choice")
}

// --- 4: a popped contact completes and CQ resumes; the list survives ---------

func TestOperatorPick_PoppedContactLogsOnceAndCqResumes(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{
		dm("7Q5MLV DL9UW JO41", -8),
		dm("7Q5MLV K1ABC FN42", -12),
	})
	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(50, 0).UTC()))

	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})  // report
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)}) // RR73 + log

	require.Len(t, r.completed, 1, "the popped contact logs exactly once")
	require.Equal(t, "DL9UW", r.completed[0].TheirCall)
	st := r.lastStatus()
	require.True(t, st.Active, "the run resumes CQ after the contact")
	require.Equal(t, "", st.TheirCall)
	require.Equal(t, []CqAnswerer{{Call: "K1ABC", Snr: -12}}, st.Answerers,
		"a candidate collected before the pop survives the completion")
}

// --- 5: the three refusals are distinct --------------------------------------

func TestOperatorPick_RefusalsAreDistinct(t *testing.T) {
	now := time.Unix(50, 0).UTC()

	t.Run("idle", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.ErrorIs(t, s.PickAnswerer("DL9UW", now), ErrNoCqPickRun)
	})

	t.Run("auto_first run", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		startCq(t, s) // auto_first
		require.ErrorIs(t, s.PickAnswerer("DL9UW", now), ErrNoCqPickRun,
			"a pop against an auto run is client drift, not a listing miss")
	})

	t.Run("not listed", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		startCqPick(t, s)
		driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
		require.ErrorIs(t, s.PickAnswerer("M0AAA", now), ErrAnswererNotListed)
	})

	t.Run("contact in flight", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		startCqPick(t, s)
		driveTheir(s, 30, []goft8.DecodedMessage{
			dm("7Q5MLV DL9UW JO41", -8),
			dm("7Q5MLV K1ABC FN42", -12),
		})
		require.NoError(t, s.PickAnswerer("DL9UW", now))
		require.ErrorIs(t, s.PickAnswerer("K1ABC", now), ErrCqContactInFlight,
			"finish the contact or press Next first — a pop never silently ends one")
	})
}

// --- 6: candidates expire at 3 min unheard -----------------------------------

func TestOperatorPick_CandidatesExpireWhenUnheard(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)
	// Heard once at the slot evaluated at now=46 s; silent from then on.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})

	// 180 s unheard: still listed (the bound is "older than", matching the SPA).
	driveTheir(s, 210, nil)
	require.Equal(t, []CqAnswerer{{Call: "DL9UW", Snr: -8}}, r.lastStatus().Answerers,
		"at exactly the bound the station is still offered")

	// 210 s unheard: gone from the frame.
	driveTheir(s, 240, nil)
	require.Empty(t, r.lastStatus().Answerers,
		"a station gone 3+ minutes is no longer offered")

	// And the pop itself re-checks, so a stale pop between slots is refused too.
	s2 := newTestSeq(&seqRecorder{})
	startCqPick(t, s2)
	driveTheir(s2, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.ErrorIs(t, s2.PickAnswerer("DL9UW", time.Unix(300, 0).UTC()),
		ErrAnswererNotListed,
		"popping a station last heard 4+ minutes ago transmits at nobody")
}

// --- 7: a park never auto-picks, and park is not a blacklist -----------------

func TestOperatorPick_ParkResumesCqWithoutAutoPick(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(50, 0).UTC()))
	require.NoError(t, s.NextAnswerer())

	// The exact situation auto modes auto-pick in: the parked station still
	// calling AND a fresh answerer behind it (TestNext_ParksTheAnswererAndKeeps-
	// TheRunGoing proves auto_first takes 9A4ZM here).
	driveTheir(s, 60, []goft8.DecodedMessage{
		dm("7Q5MLV DL9UW JO41", -8),
		dm("7Q5MLV 9A4ZM JN95", -6),
	})

	st := r.lastStatus()
	require.True(t, st.Active, "the run continues")
	require.Equal(t, "", st.TheirCall, "nobody is worked without a pop")
	sent := r.sentMsgs()
	require.Equal(t, "CQ 7Q5MLV KH78", sent[len(sent)-1],
		"the park resumes CQ — the choice stays with the operator")
	require.Equal(t, []CqAnswerer{{Call: "9A4ZM", Snr: -6}}, st.Answerers,
		"the fresh answerer is offered; the just-parked one was in flight at "+
			"this evaluation and relists next slot")

	// Park ≠ blacklist: DL9UW keeps calling and is offered again.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.Contains(t, r.lastStatus().Answerers, CqAnswerer{Call: "DL9UW", Snr: -8},
		"a parked station that is still calling must reappear — the operator is "+
			"the selector, and hiding it starves a one-station run forever")
}

// --- 8: an unencodable answerer is never listed ------------------------------

func TestOperatorPick_UnencodableAnswererNeverListed(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)

	// PJ4/K1ABC's reply does not encode (compound); DL9UW is the positive
	// control proving the collector ran on this slot.
	driveTheir(s, 30, []goft8.DecodedMessage{
		dm("7Q5MLV PJ4/K1ABC FN42", -5),
		dm("7Q5MLV DL9UW JO41", -8),
	})

	require.Equal(t, []CqAnswerer{{Call: "DL9UW", Snr: -8}}, r.lastStatus().Answerers,
		"a station we cannot reply to must not be offered — popping it would end "+
			"the whole run on ErrTxBadMessage (review M2's hazard)")
}

// --- 9: candidates accumulate mid-contact; the in-flight station never lists --

func TestOperatorPick_CandidatesAccumulateWhileWorking(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(50, 0).UTC()))

	// DL9UW missed our report and repeats its grid answer — the one message that
	// would re-collect it if the in-flight exclusion were missing. K1ABC answers
	// the CQ it can still hear.
	driveTheir(s, 60, []goft8.DecodedMessage{
		dm("7Q5MLV DL9UW JO41", -8),
		dm("7Q5MLV K1ABC FN42", -12),
	})

	st := r.lastStatus()
	require.Equal(t, "DL9UW", st.TheirCall, "the contact is unaffected")
	require.Equal(t, []CqAnswerer{{Call: "K1ABC", Snr: -12}}, st.Answerers,
		"new answerers queue while a contact is worked; the in-flight station is "+
			"never its own candidate")
}

// --- 10: abandon clears the list; a fresh run starts empty -------------------

func TestOperatorPick_AbandonClearsTheList(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCqPick(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.NotEmpty(t, r.lastStatus().Answerers, "fixture: something to clear")

	s.Abandon()
	require.Empty(t, r.lastStatus().Answerers,
		"the terminal frame must not offer stations the run can no longer work")

	// A fresh run starts with an empty list — the stale DL9UW entry above is
	// exactly what an implementation without the StartCallCq reset would render.
	startCqPick(t, s)
	require.Empty(t, r.lastStatus().Answerers, "a new run offers nothing yet")
	driveTheir(s, 90, nil)
	require.Empty(t, r.lastStatus().Answerers,
		"and its first evaluation must not resurrect the previous run's list")
}
