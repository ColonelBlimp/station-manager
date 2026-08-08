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

/*
	Slice B — the pick QUEUE (bag-and-drain), ratified semantics:

	  Bagged stations are auto-worked from the queue in order — each was
	  individually chosen, so the drain keeps every transmission
	  operator-selected. `Stop run` on a pick run PAUSES the drain (queue
	  kept; Resume continues) — today's curated-stack semantics carried over
	  so a stop never costs the operator their choices. Confusables:

	  B1 — bagged vs merely-listed rendered the same: the frame must move the
	       station from `answerers` to `queue`, in bag order.
	  B2/B3 — the drain: a completed contact + a fresh queue head = the head
	       is worked WITHOUT a further gesture, in order, until empty.
	  B4/B5 — Stop pauses (nothing drains, queue+run survive, the frame says
	       paused); Resume continues.
	  B6 — bag refusals mirror the pop's (not-listed / no pick run).
	  B7 — a STALE queue entry is expired at drain, not transmitted at (the
	       0065 rationale: a pop at a gone station wastes ~max_repeats calls).
	  B8 — unbag returns the station to the listed set.
	  B9 — Stop on an AUTO run still stops it outright (G6-family unchanged).
	  B10 — the CQ run drains its queue too, resuming CQ when it empties.
*/

func listedCaller(t *testing.T, s *Sequencer, r *seqRecorder) {
	t.Helper()
	stagedStart(t, s, "operator_pick")
	completeSeedContact(t, s)
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NotEmpty(t, r.lastStatus().Answerers, "fixture: DL9UW is listed")
}

// B1 — bagging moves the station from the listed set to the queue, in order.
func TestAdr0067_BagMovesListedToQueue(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r)
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ M0AAA IO83", -15)})

	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(125, 0).UTC()))
	require.NoError(t, s.BagAnswerer("M0AAA", time.Unix(126, 0).UTC()))

	st := r.lastStatus()
	require.Empty(t, st.Answerers, "bagged stations leave the listed set")
	require.Len(t, st.Queue, 2)
	require.Equal(t, "DL9UW", st.Queue[0].Call, "bag order is queue order")
	require.Equal(t, "M0AAA", st.Queue[1].Call)
}

// B2/B3 — the drain works the queue in order without further gestures.
func TestAdr0067_QueueDrainsInOrder(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r)
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ M0AAA IO83", -15)})
	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(125, 0).UTC()))
	require.NoError(t, s.BagAnswerer("M0AAA", time.Unix(126, 0).UTC()))

	// The next slot evaluation drains the head: DL9UW is worked.
	driveTheir(s, 150, nil)
	require.NotNil(t, s.caller, "the queue head must be worked without a gesture")
	require.Equal(t, "DL9UW", s.caller.TheirCall)

	// Complete DL9UW; the drain continues with M0AAA.
	driveTheir(s, 180, []goft8.DecodedMessage{dm("G0XYZ DL9UW R-08", -8)})
	require.Nil(t, s.caller, "fixture: DL9UW's QSO completes")
	driveTheir(s, 210, nil)
	require.NotNil(t, s.caller, "the drain continues")
	require.Equal(t, "M0AAA", s.caller.TheirCall)
}

// B4/B5 — Stop pauses the drain; Resume continues it.
func TestAdr0067_StopPausesDrainResumeContinues(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r)
	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(95, 0).UTC()))

	s.StopAutoWorkRun()
	st := r.lastStatus()
	require.True(t, st.DrainPaused, "the frame must say the drain is paused")
	require.Len(t, st.Queue, 1, "the queue survives a Stop")

	driveTheir(s, 120, nil)
	require.Nil(t, s.caller, "nothing drains while paused")

	require.NoError(t, s.ResumeDrain(time.Unix(125, 0).UTC()))
	driveTheir(s, 150, nil)
	require.NotNil(t, s.caller, "Resume lets the drain continue")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// B6 — bag refusals mirror the pop's.
func TestAdr0067_BagRefusals(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.ErrorIs(t, s.BagAnswerer("DL9UW", time.Unix(0, 0).UTC()), ErrNoCqPickRun,
		"no pick run — nothing to bag into")
	listedCaller(t, s, r)
	require.ErrorIs(t, s.BagAnswerer("W1AW", time.Unix(95, 0).UTC()), ErrAnswererNotListed,
		"a station never listed cannot be bagged")
}

// B7 — a stale queue entry is expired at drain, never transmitted at.
func TestAdr0067_StaleQueueEntryExpiresAtDrain(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r)
	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(95, 0).UTC()))

	// Far past the staleness bound with DL9UW never heard again.
	before := len(r.sentMsgs())
	driveTheir(s, 90+400, nil)
	require.Nil(t, s.caller, "a stale entry must not be worked")
	require.Len(t, r.sentMsgs(), before, "nothing transmits at a gone station")
	require.Empty(t, r.lastStatus().Queue, "the stale entry is expired off the queue")
}

// B8 — unbag returns the station to the listed set.
func TestAdr0067_UnbagReturnsToListed(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r)
	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(95, 0).UTC()))
	require.NoError(t, s.UnbagAnswerer("DL9UW", time.Unix(96, 0).UTC()))
	st := r.lastStatus()
	require.Empty(t, st.Queue)
	require.Len(t, st.Answerers, 1)
	require.Equal(t, "DL9UW", st.Answerers[0].Call)
}

// B9 — Stop on an AUTO run still stops it outright.
func TestAdr0067_StopOnAutoRunStillStops(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stagedStart(t, s, "auto_first")
	completeSeedContact(t, s)
	require.True(t, s.AutoWorkArmed())
	s.StopAutoWorkRun()
	require.False(t, s.AutoWorkArmed(), "an auto run stops outright — no pause semantics")
}

// B10 — the CQ run drains its queue too, resuming CQ when it empties.
func TestAdr0067_CqRunDrainsQueue(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t,
		s.StartCallCq("G0XYZ", "KH78", 2700, 28.074, "operator_pick", "", time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NotEmpty(t, r.lastStatus().Answerers, "fixture: an answerer is listed")

	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(35, 0).UTC()))
	driveTheir(s, 60, nil)
	require.NotNil(t, s.caller, "the CQ run works the queue head")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// B11 — codex f6e93efd P1: a bagged station that keeps calling must not be
// LISTED again (the operator could bag it twice → the drain works it twice),
// and its queue entry must REFRESH — the staleness bound runs from the last
// time they were heard, not from the bag.
func TestAdr0067_ReheardBaggedCallerRefreshesNotRelists(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r) // DL9UW heard at slot 90
	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(95, 0).UTC()))
	// Paused BEFORE the re-hear: an unpaused drain would work the head at the
	// very slot that re-hears it, hiding what this rule pins.
	s.StopAutoWorkRun()

	// They keep calling. Heard again at slot 240 — 150 s after the first.
	driveTheir(s, 240, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -7)})
	st := r.lastStatus()
	require.Empty(t, st.Answerers, "a bagged station must not relist")
	require.Len(t, st.Queue, 1, "…and must not duplicate in the queue")

	// Resume at a time stale from the FIRST hearing but fresh from the
	// SECOND: the drain must still work them — the refresh keeps the choice
	// alive while they are audibly still calling.
	require.NoError(t, s.ResumeDrain(time.Unix(300, 0).UTC()))
	driveTheir(s, 330, nil) // 330-90=240s > bound from first; 330-256≈74s < bound from second
	require.NotNil(t, s.caller, "the refreshed entry must still drain")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// B12 — codex dd44784b P2: the TERMINAL frame of a completed pick contact must
// carry the run's whole pick state — mode, listed callers, bagged queue, pause
// flag — not just auto_work_armed. The SPA renders drawer + run surface solely
// from the latest ft8-qso frame, and the terminal frame is replay-cached, so a
// bare terminal frame makes a live pick run masquerade as an auto run: the
// drawer empties, a paused queue loses its Resume control, the state line
// stops opening the drawer — until the next slot happens to publish a
// decorated status (or indefinitely, for a client that reconnects into the
// replay). The nearest confusable is exactly that auto-run appearance, so the
// fixture pins every field the confusion would blank: paused drain, one
// bagged, one still listed.
func TestAdr0067_TerminalFrameCarriesPickRunState(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r) // pick listing run; DL9UW listed at slot 90

	// A second caller, bagged; then Stop pauses the drain (queue kept).
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ M0AAA IO83", -15)})
	require.NoError(t, s.BagAnswerer("M0AAA", time.Unix(125, 0).UTC()))
	s.StopAutoWorkRun()

	// The operator works DL9UW now (a pop is not the drain — the pause does
	// not gate it), and the contact completes.
	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(130, 0).UTC()))
	require.NotNil(t, s.caller, "fixture: the pop must commit DL9UW")
	// A third station calls DURING the contact (A5 collection) — it must be
	// listed on the terminal frame too, or the drawer's top half blanks.
	driveTheir(s, 150, []goft8.DecodedMessage{
		dm("G0XYZ DL9UW R-08", -8),
		dm("G0XYZ PA3KUS IO70", -17),
	})
	require.Nil(t, s.caller, "fixture: DL9UW's QSO must complete")

	// The frame published BY the completion — no further slot may decorate it.
	st := r.lastStatus()
	require.False(t, st.Active, "fixture: the last frame is the terminal one")
	require.True(t, st.AutoWorkArmed, "the run survives the completed contact (W4)")
	require.Equal(t, "operator_pick", st.AnswerMode,
		"the terminal frame must keep saying this is a pick run")
	require.Len(t, st.Answerers, 1, "callers listed mid-contact must ride the terminal frame")
	require.Equal(t, "PA3KUS", st.Answerers[0].Call)
	require.Len(t, st.Queue, 1, "the bagged queue must ride the terminal frame")
	require.Equal(t, "M0AAA", st.Queue[0].Call)
	require.True(t, st.DrainPaused,
		"the pause must ride the terminal frame — it is what offers Resume")
}
