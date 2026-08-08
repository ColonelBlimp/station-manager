package ft8

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	AUTO-WORK-CALLERS specification (ADR 0059), written before the implementation.

	Operator statement of the need: "I answer a CQ call, and now stations are calling
	me directly. The ft8 subsystem recognizes that I have someone calling me and
	answers the call processing the qso through the ladder. At the end, if there is 1
	or more stations calling, one is selected auto_first/auto_strongest and answers the
	call processing the qso through the ladder. The process is stopped on Abandon."

	ACCEPTANCE CRITERION:

	  When I have answered someone's CQ and stations then call me directly, SM works
	  them one after another through the full ladder without my clicking each one,
	  until I press Abandon — and at any moment I can tell whether the run is still
	  ARMED AND WAITING (it will key the rig when the next caller appears) from
	  STOPPED.

	The third clause is the load-bearing one: an armed run with nobody calling looks
	exactly like nothing happening, and it will transmit. W7 pins that it is visible.

	Operator's calls, folded in (ADR 0059):

	  1. Armed ONLY by an operator-started QSO — never from idle. This is what keeps
	     the operator-initiated invariant in internal/ft8/CLAUDE.md intact: one
	     operator action per run, then a sequence of contacts, exactly the shape a
	     Call-CQ run already has. W5 is that rule, and it is the one whose failure
	     would make the daemon initiate operation on its own.
	  2. A QSO that ends WITHOUT a completed exchange continues the run (W4).
	  3. Stops on Abandon, TX disarmed, rig disconnect / CAT lost, band or dial change,
	     and on starting a Call-CQ run (W12, added 2026-08-01).
	  4. Duplicates unchanged from the Call-CQ loop's ratified position: no
	     completed-call suppression.

	SECOND CRITERION, added 2026-08-01 from a dogfood report — "when answering a cq
	and auto-work armed, when the contact has completed and nobody calls you, so you
	start a cq call - the auto-work armed stays active, or the pill stays viewable":

	  When I start calling CQ, the "Auto-work armed" indicator goes out in the same
	  frame that announces the CQ — so the indicator names the machine that is
	  actually running, and I can tell "the CQ run is working answerers" apart from
	  "an auto-work run is armed and waiting", which otherwise render identically.

	The report was an either/or — state or display — and it is BOTH. StartCallCq
	resets caller / stalledCalls / confirmHold / contact for the fresh session and
	never touched autoWork, so the run really did survive it and the pill was
	honestly reporting a real state.

	It could NOT fire: onSlotIdleArmed gates on mode == seqIdle, and a Call-CQ
	contact resumes CQ rather than ending (see retireSessionLocked's comment), so the
	only exit from the run is Abandon, which disarms. This is therefore not a
	rogue-transmission fix. What it removes is an indicator naming the wrong
	mechanism — during a CQ run answerers ARE worked without a click, but by
	pickAnswererLocked, not by the auto-work run — and a call/offset/dial pinned from
	the PREVIOUS session that any future path back to seqIdle would transmit on.

	Operator's call (2026-08-01): CLEAR it. Starting a CQ is a new operator-started
	session and pins its own parameters, and Abandon stops everything; re-arming on
	the CQ's parameters instead would mean an abandoned CQ left the station still
	auto-working callers, which contradicts what Abandon means everywhere else.

	W12 pins the state; V5 pins that the CQ's OWN frame already carries it. V5 is the
	load-bearing half: StartCallCq's sequence is set-mode → reset fields →
	statusLocked → publish → unlock, and a clear placed after the publish leaves the
	field right internally while the frame the SPA receives still says armed — the
	pill stays lit until something else happens to publish. Naming which step the
	clear belongs to is the whole rule.

	NOT REBUILT, and deliberately so: pickAnswererLocked already selects a station
	calling us from one slot's decodes, honouring auto_first / auto_strongest, skipping
	stalled and unencodable callers. This feature is the loop around it, not a second
	matcher — a parallel implementation is how the two would drift.

	SCOPE, stated rather than assumed: only the STANDARD ladders arm a run
	(answer-a-CQ and work-a-caller). Field Day and type-4 sessions are deliberately
	left out — they are distinct exchange types with their own ladders, the operator
	asked about the standard case, and arming them would mean claiming behaviour on
	paths nobody has described.
*/

// autoWorkRun starts an operator-initiated work-a-caller QSO with the session
// facts staged (ADR 0066: per-click intent + the session's auto answer mode —
// no config policy exists to install), drives it to a completed exchange, and
// returns with the sequencer idle but armed. This is the state every rule below
// starts from: ONE operator action has happened, and the run is now the
// daemon's to continue.
func autoWorkRun(t *testing.T, s *Sequencer, mode string) {
	t.Helper()
	s.setPendingAnswerMode(mode)
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)}) // our report
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)}) // their roger → our RR73
	require.Nil(t, s.caller, "fixture: the seeding QSO must complete, else nothing is armed")
}

// W1 — THE FEATURE. A station calling us after the seeding QSO is worked with no
// further operator action, through the normal ladder.
func TestAutoWork_WorksTheNextCallerWithNoOperatorAction(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.True(t, s.Active(), "the run must pick the caller up")
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall)
	require.Equal(t, "DL9UW G0XYZ -08", lastSent(r), "the ladder starts with our report")
}

// W2 — THE DISCRIMINATOR. Without the per-click intent the identical slot
// produces nothing. (Re-derived by ADR 0066: the config knob's gate role is
// gone — the intent, which nothing here stages, is what separates this from an
// implementation that works callers unconditionally.)
func TestAutoWork_NoIntentLeavesTheCallerAlone(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})

	before := len(r.sentMsgs())
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.False(t, s.Active(), "with no intent the session ends and stays ended")
	require.Nil(t, s.caller)
	require.Len(t, r.sentMsgs(), before, "nothing may be transmitted to the caller")
}

// W3 — it is a RUN, not a one-shot: the contact after the auto-worked one is also
// picked up. An implementation that armed once and cleared on first use passes W1
// and fails here.
func TestAutoWork_KeepsGoingAfterAnAutoWorkedContact(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ DL9UW R-08", -11)}) // completes
	require.Nil(t, s.caller, "fixture: the auto-worked contact completed")

	driveTheir(s, 150, []goft8.DecodedMessage{dm("G0XYZ 9A4ZM JN95", -6)})

	require.True(t, s.Active(), "the run continues past its own contacts")
	require.Equal(t, "9A4ZM", s.caller.TheirCall)
}

// W4 — operator's call 2: a contact that ends WITHOUT a completed exchange leaves
// the run armed. One awkward partner must not end a productive pile-up.
func TestAutoWork_RunSurvivesAContactThatNeverCompletes(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.maxRepeats = 2
	autoWorkRun(t, s, "auto_first")

	// This contact never rogers, so it runs out the repeat cap and is dropped without
	// a completed exchange — no failure injection needed, just a partner who stops.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	driveTheir(s, 120, nil)
	driveTheir(s, 150, nil)
	driveTheir(s, 180, nil)
	require.Nil(t, s.caller, "fixture: the stalled contact was dropped without completing")

	driveTheir(s, 210, []goft8.DecodedMessage{dm("G0XYZ 9A4ZM JN95", -6)})

	require.True(t, s.Active(), "a failed contact is not a reason to end the run")
	require.Equal(t, "9A4ZM", s.caller.TheirCall)
}

// W5 — THE INVARIANT RULE. The policy being on is NOT enough: with no
// operator-started QSO there is no run, so a station calling an idle daemon is left
// alone. If this fails, the daemon initiates operation on its own, which
// internal/ft8/CLAUDE.md forbids and ADR 0059 explicitly declines to change.
func TestAutoWork_NeverArmsFromIdle(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	// ADR 0066: even WITH intent and an auto mode staged, no slot can arm a
	// run — only an operator-started session consumes the staging.
	s.setPendingAnswerMode("auto_first")

	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.False(t, s.Active(), "no operator action has happened; there is no run")
	require.Nil(t, s.caller)
	require.Empty(t, r.sentMsgs(), "an idle daemon must not answer a caller")
}

// W6 — Abandon stops the run, and a caller in a later slot is left alone. Abandon
// already ends the session; what this pins is that it also DISARMS, so the run does
// not quietly resume on the next caller.
func TestAutoWork_AbandonStopsTheRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")

	s.Abandon()

	before := len(r.sentMsgs())
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.False(t, s.Active())
	require.Nil(t, s.caller)
	require.Len(t, r.sentMsgs(), before, "Abandon must disarm, not merely end the contact")
}

// W7 — the criterion's third clause: ARMED AND WAITING is distinguishable from
// STOPPED. Both look like "no contact in progress", and only one of them will key
// the rig when a caller appears, so the operator cannot be left to infer it.
func TestAutoWork_ArmedRunIsDistinguishableFromStopped(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")

	require.Nil(t, s.caller, "no contact in progress either way — that is the confusion")
	require.True(t, s.AutoWorkArmed(), "armed and waiting for the next caller")

	s.Abandon()
	require.False(t, s.AutoWorkArmed(), "stopped")
}

// W8 — selection is the EXISTING matcher's, so auto_strongest picks the loudest
// caller in the slot rather than the first by decode order. Pins that the run reuses
// pickAnswererLocked instead of growing a second selection path that would drift
// from the Call-CQ side.
func TestAutoWork_HonoursAutoStrongest(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_strongest")

	driveTheir(s, 90, []goft8.DecodedMessage{
		dm("G0XYZ DL9UW JO41", -18), // first by decode order
		dm("G0XYZ 9A4ZM JN95", -4),  // loudest
	})

	require.NotNil(t, s.caller)
	require.Equal(t, "9A4ZM", s.caller.TheirCall, "auto_strongest clears the loud ones first")
}

// lastSent is the most recent message handed to the transmitter, or "".
func lastSent(r *seqRecorder) string {
	sent := r.sentMsgs()
	if len(sent) == 0 {
		return ""
	}
	return sent[len(sent)-1]
}

// W9 — ANSWERING A CQ ARMS THE RUN. This is the entry point the operator's request
// actually names ("I answer a CQ call, and now stations are calling me directly") and
// the one the criterion opens with, yet every rule above seeds the run through
// StartWorkCaller because that ladder is shorter to drive. The two are different
// operator actions on different code paths, and arming one does not arm the other —
// the first implementation armed only the work-a-caller path and this workflow was
// silently dead.
func TestAutoWork_AnsweringACqArmsTheRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	// ADR 0067: the staged session mode IS the arming input — the per-click
	// intent grammar is retired. The entry point is unchanged: answering a CQ
	// under an auto mode arms the run.
	s.setPendingAnswerMode("auto_first")

	// Answer K1ABC's CQ and run the exchange to completion.
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
	require.False(t, s.Active(), "fixture: the answer-a-CQ exchange must complete")
	require.True(t, s.AutoWorkArmed(), "answering a CQ is an operator action and must arm the run")

	// ...and a station calling us afterwards is worked.
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.True(t, s.Active(), "the run must pick the caller up")
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// W10 — THE CONFIG DEFAULT REACHES THE SEQUENCER through the Service start
// path. Every sequencer-level rule above stages the session facts by hand, so
// none of them would notice the Service→staging plumbing being unreachable in
// production — the failure class a review found after two commits that each
// looked complete. Re-derived by ADR 0066: the knob no longer gates, so what
// must reach the sequencer is the config DEFAULT answer mode, resolved by the
// Service when the request carries none (an old client), alongside the
// request's intent. This one starts from config + the wire-shaped call alone.
func TestAutoWork_ConfigDefaultArmsThroughARealService(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	s.cfg.TX.CallerAnswerMode = types.Ft8CallerAnswerAutoFirst
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	// autoWork=true, answerMode "" — exactly what an old client sends. The slot
	// must be FRESH: the Service enforces the 3-min act-on-decode bound against
	// the real clock. The run arms AT START (armAutoWorkLocked runs in the
	// start path), so no exchange needs driving.
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Now().UTC().Format(time.RFC3339), 1500, 14.074, 1, false, ""))
	require.True(t, s.seq.AutoWorkArmed(),
		"the config default mode must arm a run through the Service path without a test reaching in")
}

// W11 — under operator_pick no AUTO-working run is armed. That mode promises the
// operator chooses; a run that auto-picked would break the promise, and reporting
// one as armed is the false-advertisement failure invariant 7 exists to prevent.
func TestAutoWork_OperatorPickDoesNotArmARun(t *testing.T) {
	s := newService(types.Ft8Config{
		Enabled: true,
		TX:      &types.Ft8TXConfig{CallerAnswerMode: types.Ft8CallerAnswerOperatorPick},
	}, logging.Noop(), nil)

	s.seq.setPendingAnswerMode("operator_pick")
	require.NoError(t, s.seq.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	// Re-derived by ADR 0067: pick now arms a LISTING run — the promise it
	// advertises is listing, not auto-working, and adr0067_test.go A3 pins
	// that such a run transmits NOTHING without a pop.
	require.True(t, s.seq.AutoWorkArmed(), "operator_pick arms a listing run")
}

// --- STOP CONDITIONS (ADR 0059, operator's call 3) ---------------------------
//
// Abandon, TX disarmed, rig disconnect / CAT lost, band or dial change.
//
// All four already hold, and NOT by accident of this feature: every one of those
// paths tears down through disarmTx (both branches, including the already-idle one),
// which calls seq.Abandon, which disarms the run. These rules exist because that is
// ROUTING, and nothing else would notice it changing: a refactor that stopped
// disarmTx abandoning, or a new disarm path that skipped it, would leave the run
// armed and working the next caller the moment TX came back — silently, since an
// armed run looks exactly like a stopped one.
//
// A FOURTH stop falls out and is worth knowing rather than discovering: the last
// /v1/ft8/events subscriber leaving past the linger window disarms TX as
// attended-only housekeeping, so closing the browser also ends a run.

// autoWorkService builds a real Service with the knob on and an armed run.
func autoWorkService(t *testing.T) *Service {
	t.Helper()
	s := newService(types.Ft8Config{
		Enabled: true,
		// An explicit auto mode: these rules need an ARMED run, i.e. a
		// session that opted in (the setPendingAnswerMode below is the
		// arming input since ADR 0067; the config mode is only its default).
		TX: &types.Ft8TXConfig{CallerAnswerMode: types.Ft8CallerAnswerAutoFirst},
	}, logging.Noop(), nil)
	s.seq.setPendingAnswerMode("auto_first") // the session's auto mode (ADR 0066)
	require.NoError(t, s.seq.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.seq.AutoWorkArmed(), "fixture: the run must be armed to prove it stops")
	return s
}

// X1 — disarming TX stops the run. Without this an operator who disarms sees the
// contact end and reasonably concludes the station is quiet, while the run is still
// armed and will work the next caller as soon as TX is re-armed.
func TestAutoWork_DisarmingTxStopsTheRun(t *testing.T) {
	s := autoWorkService(t)
	require.NoError(t, s.ArmTx(false))
	require.False(t, s.seq.AutoWorkArmed())
}

// X2 — losing CAT stops the run. The rig is gone, so the run cannot key; leaving it
// armed would mean a reconnect silently resumes unattended transmission.
func TestAutoWork_CatLossStopsTheRun(t *testing.T) {
	// A REAL capturing service, built the way the existing CAT-gate tests build one:
	// the earlier version of this fixture set s.capturing by hand and panicked inside
	// releaseCaptureLocked, because a faked flag is not a capture session.
	withReconcileInterval(t, time.Hour) // only the reconcileCat() below runs
	src := newFakeSource()
	s := newService(types.Ft8Config{
		Enabled: true,
		// An explicit auto mode: these rules need an ARMED run, i.e. a
		// session that opted in (the setPendingAnswerMode below is the
		// arming input since ADR 0067; the config mode is only its default).
		TX: &types.Ft8TXConfig{CallerAnswerMode: types.Ft8CallerAnswerAutoFirst},
	}, logging.Noop(), src)
	var live atomic.Bool
	live.Store(true) // rig on
	s.SetCatGate(live.Load)
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe() // FT8 view open + CAT live → capturing
	defer unsub()

	s.seq.setPendingAnswerMode("auto_first") // the session's auto mode (ADR 0066)
	require.NoError(t, s.seq.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.seq.AutoWorkArmed(), "fixture: the run must be armed to prove it stops")

	live.Store(false) // the rig drops
	s.reconcileCat()

	require.False(t, s.seq.AutoWorkArmed())
}

// X3 — the rig leaving the frequency the run was armed on stops it. The run pins the
// dial precisely so a contact cannot be worked on a frequency the operator did not
// choose; continuing on the new one would be the wrong-band QSO invariant 1 forbids.
func TestAutoWork_DialMoveStopsTheRun(t *testing.T) {
	s := autoWorkService(t)
	// A run only ever exists DOWNSTREAM of an armed, dial-bound TX: arming binds a
	// frequency, sessionTxGate refuses a session without it, and every disarm path
	// clears the run. The fixture states that rather than leaving TX disarmed, where
	// onDialMoved correctly returns early because nothing is bound to a frequency —
	// a state this run cannot be in.
	s.txMu.Lock()
	s.txArmed = true
	s.armDialMHz = 14.074
	s.txMu.Unlock()

	s.onDialMoved(14.074, 21.074)

	require.False(t, s.seq.AutoWorkArmed())
}

// X4 — an operator-requested retune stops it too. Same teardown as the dial guard,
// different initiator: the operator is deliberately moving the station, so a run
// bound to where it used to be must not survive.
func TestAutoWork_OperatorRetuneStopsTheRun(t *testing.T) {
	s := autoWorkService(t)
	s.txMu.Lock()
	s.txArmed = true // StopForRetune returns early with nothing armed
	s.txMu.Unlock()

	require.NoError(t, s.StopForRetune())
	require.False(t, s.seq.AutoWorkArmed())
}

// --- VISIBILITY (the criterion's third clause) -------------------------------

// V1 — THE FRAME THAT MATTERS. When a contact ends with the run still live, the
// published status says so: not active, but armed. Without it the operator sees the
// same "no session" frame whether the station is finished or about to transmit at
// whoever calls next.
func TestAutoWork_IdleStatusReportsAnArmedRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")

	st := r.lastStatus()
	require.False(t, st.Active, "the contact has ended")
	require.True(t, st.AutoWorkArmed, "...but the run has not, and the frame must say so")
}

// V2 — and STOPPED is the other value, on an otherwise identical frame. V1 alone
// would pass on an implementation that always reported armed.
func TestAutoWork_IdleStatusReportsAStoppedRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")
	require.True(t, r.lastStatus().AutoWorkArmed, "fixture: armed before the abandon")

	s.Abandon()

	st := r.lastStatus()
	require.False(t, st.Active)
	require.False(t, st.AutoWorkArmed, "Abandon stopped the run and the frame must say so")
}

// V3 — with no intent staged no frame ever claims an armed run, so the indicator
// cannot appear on a station that will never auto-work anyone. (Re-derived by
// ADR 0066 from the knob-off form: absent intent is the no-arm state now.)
func TestAutoWork_StatusNeverClaimsArmedWithoutIntent(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})

	r.mu.Lock()
	published := append([]QsoStatus(nil), r.statuses...)
	r.mu.Unlock()
	require.NotEmpty(t, published, "fixture: the session must have published something")
	for _, st := range published {
		require.False(t, st.AutoWorkArmed, "no frame may claim a run that cannot exist")
	}
}

// V4 — an ACTIVE frame from a run also reports the run. Not decoration: without it
// the flag is true on the idle frame, false for the whole of the next contact, and
// true again afterwards, so the operator's indicator blinks off exactly while the run
// is doing the thing it exists to do. The run is live throughout; the frames must say
// so throughout.
func TestAutoWork_ActiveStatusAlsoReportsTheRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	st := r.lastStatus()
	require.True(t, st.Active, "fixture: the run picked a caller up")
	require.True(t, st.AutoWorkArmed, "the run is still live while working its contact")
}

// W12 — STARTING A CALL-CQ RUN STOPS THE AUTO-WORK RUN. The second criterion's state
// half. StartCallCq already resets the rest of the per-session state for a fresh
// session (caller, stalledCalls, confirmHold, contact); the run was simply missed, so
// it survived into a session that pins its own callsign, offset and dial.
//
// The fixture starts the CQ on a DIFFERENT offset from the seeding QSO's on purpose.
// That is the real shape of the report — the operator picks a clear frequency to call
// on — and it is what makes the surviving run's pinned 1500 Hz wrong rather than
// merely redundant.
func TestAutoWork_StartingCallCqStopsTheRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")
	require.True(t, s.AutoWorkArmed(),
		"fixture: a run must be armed before the CQ starts, or this rule proves nothing")

	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1200, 14.074, "auto_first", "",
		time.Unix(90, 0).UTC()))

	require.False(t, s.AutoWorkArmed(),
		"a Call-CQ run is a new operator-started session; the previous run must not survive it")
}

// V5 — and the frame that ANNOUNCES the CQ already carries the stopped run, so the
// indicator goes out at the moment the operator acts rather than at some later
// publish.
//
// Separate from W12 because the two fail differently, which is the point of writing
// both: clearing the run AFTER s.publish(st) satisfies W12 (the field ends up right)
// and fails this one (the frame the SPA actually receives still claims an armed run).
// The SPA rebuilds its whole qso object per frame, so a stale "armed" persists until
// something else publishes — and a CQ waiting for answerers may not publish again for
// a long time.
func TestAutoWork_CallCqFrameReportsTheStoppedRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")
	require.True(t, r.lastStatus().AutoWorkArmed, "fixture: the pre-CQ frame must claim the armed run")

	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1200, 14.074, "auto_first", "",
		time.Unix(90, 0).UTC()))

	st := r.lastStatus()
	require.Equal(t, "calling-cq", st.State, "fixture: this must be the frame the CQ start published")
	require.False(t, st.AutoWorkArmed, "the CQ's own frame must already report the run stopped")
}

/*
	--- ADR 0065: PER-CLICK ARMING GRAMMAR (operator-ratified 2026-08-07) ---

	The policy knob stops arming anything by itself: it becomes a GATE, and the
	operator's click carries the intent (staged per start, like the logbook and
	the deliberate-repeat flag). Evidence for the change is in the day's log:
	the always-arm rule collected an Abandon-debt twice in one morning
	(06:01:17, 07:26:06 — a run the operator never asked for had to be
	explicitly stopped after every CQ answer).

	Fixture honesty: G2 and G5 are the differentiating rules (the pre-0065 code
	arms on ANY start when the policy is on, so both fail against it); W9/W10
	pin that the intent path still arms; G3 pins the gate's refusing side —
	against pre-0065 code it passes (the policy check existed), so what it
	guards is a FUTURE implementation arming on intent alone. G6/G7 pin the new
	run-only stop, which no other control provides (Abandon ends the contact
	too).
*/

// G2 — re-derived by ADR 0067 (the old rule — a plain start arms nothing —
// is SUPERSEDED; adr0067_test.go A2 pins the new behaviour): a start with NO
// session mode staged arms nothing. Unreachable through the Service (it
// always stages a resolved mode) — this pins the sequencer-level staging
// contract so a future start path cannot skip the staging and inherit a run.
func TestAutoWork_NoStagedModeArmsNothing(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	// No setPendingAnswerMode — the staging contract violation under test.

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
	require.False(t, s.Active(), "fixture: the exchange must complete")
	require.False(t, s.AutoWorkArmed(), "a plain click must not arm a run (ADR 0065 fork 1)")

	before := len(r.sentMsgs())
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.False(t, s.Active(), "a caller after a work-only contact is left alone")
	require.Len(t, r.sentMsgs(), before)
}

// G3 — THE GATE's refusing side (re-derived by ADR 0067: no mode staged —
// the one refusal left): the CONTACT proceeds normally and no run is armed.
// A refused arm must never cost the QSO — the 15 s slot window leaves no
// room for a retry after a rejection.
func TestAutoWork_GateRefusesArmNotContact(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	// No mode staged — the gate's only refusing condition now.

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active(), "the contact must start despite the refused arm")
	require.False(t, s.AutoWorkArmed(), "a closed gate refuses the arm")
	require.False(t, r.lastStatus().AutoWorkArmed,
		"the frame must tell the SPA the arm was refused, so intent-vs-outcome can toast")

	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
	require.False(t, s.Active(), "the refused arm must not disturb the exchange")
}

// G5 — a plain start CLEARS an armed run. "Work that station only" defines the
// operator's whole intent, and leaving the old run armed would resume
// auto-working on call/offset/dial pinned by a PREVIOUS session — the same
// stale-pin hazard the StartCallCq clear removed (W12).
func TestAutoWork_UnstagedStartClearsAnArmedRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")
	require.True(t, s.AutoWorkArmed(), "fixture: a run is armed")

	// Re-derived by ADR 0067: every start REPLACES the run; one that stages
	// no mode (unreachable through the Service) must CLEAR rather than
	// inherit the previous session's pins — the stale-pin hazard unchanged.
	s.setPendingAnswerMode("")
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "M7ABC", "IO83",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	require.False(t, s.AutoWorkArmed(),
		"an unstaged start must clear the previous session's run, not inherit its pins")
	require.False(t, r.lastStatus().AutoWorkArmed, "…and the frame must say so")
}

// G6 — StopAutoWorkRun during an ACTIVE contact stops ONLY the run. Abandon is
// the other control and it ends both; the pill needs one that does not.
func TestAutoWork_StopRunLeavesActiveContact(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.setPendingAnswerMode("auto_first")
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active())
	require.True(t, s.AutoWorkArmed())

	s.StopAutoWorkRun()

	require.False(t, s.AutoWorkArmed(), "the run is stopped")
	require.True(t, s.Active(), "the active contact must survive a run-only stop")
	st := r.lastStatus()
	require.True(t, st.Active, "the frame still reports the live session")
	require.False(t, st.AutoWorkArmed, "…with the run cleared")
}

// G7 — StopAutoWorkRun while idle-and-armed publishes the cleared state (the V2
// shape: armed-and-waiting is invisible without a frame), and a second stop is
// an idempotent no-publish no-op.
func TestAutoWork_StopRunWhileIdlePublishesAndIsIdempotent(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	autoWorkRun(t, s, "auto_first")
	require.True(t, s.AutoWorkArmed())

	s.StopAutoWorkRun()
	require.False(t, s.AutoWorkArmed())
	st := r.lastStatus()
	require.False(t, st.Active)
	require.False(t, st.AutoWorkArmed, "the pill must go out on the wire, not just internally")

	r.mu.Lock()
	n := len(r.statuses)
	r.mu.Unlock()
	s.StopAutoWorkRun()
	r.mu.Lock()
	after := len(r.statuses)
	r.mu.Unlock()
	require.Equal(t, n, after, "stopping a stopped run publishes nothing")
}

// G8 — the FIRST published frame of an intent-carrying work-caller start already
// carries the granted arm (codex P1 on 7de6708e: commitWorkCallerLocked publishes
// as its last act, and arming AFTER it made the first frame report
// auto_work_armed=false on a GRANTED arm — which the SPA's one-shot verdict logic
// correctly reads as a refusal, toasting "disabled in settings" on a run that is
// live). The frame is the observable; s.AutoWorkArmed() alone proved too little.
func TestAutoWork_WorkCallerFirstFrameCarriesTheGrantedArm(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.setPendingAnswerMode("auto_first")

	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	st := r.lastStatus()
	require.True(t, st.Active)
	require.True(t, st.AutoWorkArmed,
		"the start's own frame must carry the verdict — the SPA treats the first active frame as final")
}
