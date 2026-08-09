package ft8

/*
   Run identity — spot-network design §6.2, prerequisite 3. One UUIDv7 per
   operator-started run, minted where the run actually begins, stable for the
   run's whole life, gone when it ends. The design doc says "minted at run
   start (cq/start)"; under ADR 0067's mode-only arming the run ALSO begins at
   any operator start that arms (A1/A2 in adr0067_test.go), so the mint point
   is the run's two birth sites — StartCallCq and armAutoWorkLocked — not the
   one endpoint the doc names. (Doc amendment noted the day this was built.)

   OPERATOR-OBSERVABLE CRITERIA, each with its nearest confusable state:

   RI1 — from the moment a run starts, every published ft8-qso frame of that
         run carries the SAME run_id + run_started_at — including a completed
         contact's TERMINAL frame (confusable: the terminal blanking that B12
         fixed for the queue; a replay consumer scoping "this run" would lose
         the id exactly at the frame the drawer re-renders from).
   RI2 — two contacts completed inside one run carry the run's id on their
         CompletedQso records, whichever shape the run is (a Call-CQ session
         or a mode-armed run) (confusable: a fresh id per contact — W4 says
         the run OUTLIVES its contacts).
   RI3 — a pick run's Stop/Resume is a PAUSE, not an end: the id is identical
         before, during, and after, and a contact worked after Resume carries
         it (confusable: pause treated as run end → new id → "logged this
         run" splits one sitting into two).
   RI4 — Abandon ends the run: the post-abandon terminal frame carries NO run
         id, and a fresh start mints a DIFFERENT id (confusable: stale id
         surviving un-cleared state and re-attaching to the next run).
   RI5 — Stop on an AUTO run ends it (ADR 0067: only pick pauses): the frame
         that reports the stop carries no run id (confusable: RI3's pause).
   RI6 — contacts outside a run carry no id: a type-4 work contact's
         CompletedQso.RunID is empty (its snapshot never sees the run state)
         (confusable: the id leaking onto every completion because the stamp
         was placed in a shared path).
   RI7 — BuildQso carries CompletedQso.RunID onto types.Qso.AppSmRunID, so
         the id persists via additional_data and rides the cloud payload
         (empty stays empty — no phantom APP field on non-run QSOs).

   Times in these tests use the fixed test epoch (time.Unix(N, 0)) that the
   sequencer harness already uses; run_started_at is asserted as the start
   call's own Unix time, not "roughly now".
*/

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// RI1 + RI2 (Call-CQ shape) — the id is minted at cq/start, rides every
// frame including terminals, and stamps both contacts of the run.
func TestRunIdentity_CqRun_StableAcrossContactsAndTerminals(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	var logged []CompletedQso
	s.onComplete = func(c CompletedQso) { logged = append(logged, c) }

	start := time.Unix(0, 0).UTC()
	require.NoError(t,
		s.StartCallCq("G0XYZ", "KH78", 2700, 28.074, "operator_pick", "", start))

	st := r.lastStatus()
	require.NotEmpty(t, st.RunID, "RI1: the start frame must carry the run id")
	require.True(t, utils.IsValidUUIDv7(st.RunID), "the run id is a UUIDv7")
	require.Equal(t, start.Unix(), st.RunStartedAt, "run_started_at is the start time")
	runID := st.RunID

	// Contact 1: DL9UW answers, is bagged, drains, completes.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NoError(t, s.BagAnswerer("DL9UW", time.Unix(35, 0).UTC()))
	driveTheir(s, 60, nil)
	require.NotNil(t, s.caller, "fixture: the drain must commit DL9UW")
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW R-08", -8)})
	require.Len(t, logged, 1, "fixture: contact 1 must complete")

	require.Equal(t, runID, r.lastStatus().RunID,
		"RI1: the completion's frame still carries the run id — the run outlives the contact")
	require.Equal(t, runID, logged[0].RunID, "RI2: contact 1 carries the run id")

	// Contact 2, same run.
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ M0AAA IO83", -15)})
	require.NoError(t, s.BagAnswerer("M0AAA", time.Unix(125, 0).UTC()))
	driveTheir(s, 150, nil)
	require.NotNil(t, s.caller, "fixture: the drain must commit M0AAA")
	driveTheir(s, 180, []goft8.DecodedMessage{dm("G0XYZ M0AAA R-15", -15)})
	require.Len(t, logged, 2, "fixture: contact 2 must complete")
	require.Equal(t, runID, logged[1].RunID, "RI2: contact 2 carries the SAME run id")
}

// RI2 (armed shape) — a mode-armed run (work-caller start under a pick mode)
// carries one id across its contacts too; the shape must not matter.
func TestRunIdentity_ArmedRun_SameIdAcrossShapeAndContacts(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	var logged []CompletedQso
	s.onComplete = func(c CompletedQso) { logged = append(logged, c) }

	stagedStart(t, s, "operator_pick") // arms the run at start (A1)
	st := r.lastStatus()
	require.NotEmpty(t, st.RunID, "an armed start begins the run — the id is minted here")
	runID := st.RunID

	completeSeedContact(t, s)
	require.Len(t, logged, 1)
	require.Equal(t, runID, logged[0].RunID, "the seeding contact belongs to the run it started")
	require.Equal(t, runID, r.lastStatus().RunID,
		"the run id survives the completed contact (W4 — the B12 terminal rule)")

	// A caller is listed, popped, worked: same run, same id.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(95, 0).UTC()))
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ DL9UW R-08", -8)})
	require.Len(t, logged, 2, "fixture: the popped contact must complete")
	require.Equal(t, runID, logged[1].RunID, "same run, same id, different contact")
}

// RI3 — pick Stop/Resume is a pause: one id through the whole sitting.
func TestRunIdentity_PickPauseKeepsTheId(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	var logged []CompletedQso
	s.onComplete = func(c CompletedQso) { logged = append(logged, c) }

	stagedStart(t, s, "operator_pick")
	completeSeedContact(t, s)
	runID := r.lastStatus().RunID
	require.NotEmpty(t, runID)

	s.StopAutoWorkRun() // pick → pause
	st := r.lastStatus()
	require.True(t, st.DrainPaused, "fixture: the stop must pause, not end")
	require.Equal(t, runID, st.RunID, "RI3: a paused run still carries its id")

	require.NoError(t, s.ResumeDrain(time.Unix(100, 0).UTC()))
	require.Equal(t, runID, r.lastStatus().RunID, "RI3: resume continues the SAME run")

	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NoError(t, s.PickAnswerer("DL9UW", time.Unix(125, 0).UTC()))
	driveTheir(s, 150, []goft8.DecodedMessage{dm("G0XYZ DL9UW R-08", -8)})
	require.NotEmpty(t, logged)
	require.Equal(t, runID, logged[len(logged)-1].RunID,
		"RI3: a contact worked after Resume belongs to the run started before the pause")
}

// RI4 — Abandon ends the run; a fresh start is a fresh identity.
func TestRunIdentity_AbandonEndsRun_FreshStartFreshId(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	start := time.Unix(0, 0).UTC()
	require.NoError(t,
		s.StartCallCq("G0XYZ", "KH78", 2700, 28.074, "operator_pick", "", start))
	first := r.lastStatus().RunID
	require.NotEmpty(t, first)

	s.Abandon()
	st := r.lastStatus()
	require.Empty(t, st.RunID, "RI4: the post-abandon frame must carry no run id")
	require.Zero(t, st.RunStartedAt, "RI4: nor a start time")

	require.NoError(t,
		s.StartCallCq("G0XYZ", "KH78", 2700, 28.074, "operator_pick", "", time.Unix(300, 0).UTC()))
	second := r.lastStatus().RunID
	require.NotEmpty(t, second)
	require.NotEqual(t, first, second, "RI4: a new run must mint a new id — never reuse")
}

// RI5 — an auto run's Stop ENDS it (the contrast to RI3's pause).
func TestRunIdentity_AutoStopEndsRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	stagedStart(t, s, "auto_first")
	completeSeedContact(t, s)
	require.NotEmpty(t, r.lastStatus().RunID, "fixture: the auto run is live with an id")

	s.StopAutoWorkRun() // auto → the run stops outright
	require.Empty(t, r.lastStatus().RunID,
		"RI5: the frame reporting an auto stop carries no run id — the run is over")
}

// RI6 — a contact outside the run carries no id EVEN WHILE A RUN IS LIVE: a
// type-4 interlude worked from an armed (paused) pick run is operator
// freelancing, not run output — its completion must not inherit the standing
// run's identity. The live run in the fixture is what makes this test bite:
// without it, a stamp misplaced into a shared completion path would still
// yield empty and the test would pass vacuously.
func TestRunIdentity_NonRunContactCarriesNone(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	var logged []CompletedQso
	s.onComplete = func(c CompletedQso) { logged = append(logged, c) }

	stagedStart(t, s, "operator_pick")
	completeSeedContact(t, s)
	require.NotEmpty(t, r.lastStatus().RunID, "fixture: a run is live and identified")

	require.NoError(t, s.StartWorkCallerT4("G0XYZ", "PJ4/K1ABC", "", -12,
		time.Unix(90, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(90, 0).UTC()))
	// The type-4 work ladder's sole rung is the terminal RR73: it fires at our
	// first qualifying slot and the QSO completes with it (Group B, no report).
	driveTheir(s, 120, []goft8.DecodedMessage{dm("<...> PJ4/K1ABC", -12)})
	require.Len(t, logged, 2, "fixture: the type-4 contact must complete")
	require.Empty(t, logged[1].RunID,
		"RI6: a type-4 interlude carries no run id even while the run stands armed")
	require.Equal(t, logged[0].RunID, r.lastStatus().RunID,
		"RI6: and the standing run keeps its identity through the interlude")
}

// RI8 (codex P1a on f3043e80) — the ANSWER-a-CQ seed contact belongs to the
// run it starts. StartQso arms the run (A2) but completes through
// completedQsoLocked, a different snapshot from the caller-side one — a stamp
// on only one of them splits a run's QSO history and drops the seed contact
// from "logged this run".
func TestRunIdentity_AnswerSeedContactCarriesTheRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	var logged []CompletedQso
	s.onComplete = func(c CompletedQso) { logged = append(logged, c) }

	s.setPendingAnswerMode("auto_first")
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	runID := r.lastStatus().RunID
	require.NotEmpty(t, runID, "fixture: answering under an auto mode arms the run (A2)")

	// The answer ladder to completion (the TestSequencer_HappyPath drive):
	// their report → our roger-report → their RR73 → our 73, QSO logs.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
	require.Len(t, logged, 1, "fixture: the answer contact must complete")
	require.Equal(t, runID, logged[0].RunID,
		"RI8: the answer seed contact carries the run it started")
}

// RI9 (codex P1b on f3043e80) — a contact in flight when the operator stops
// an auto run STILL belongs to that run: identity is pinned per contact at
// commit (the ADR 0055 pin-at-arm discipline), never read live at completion.
// The confusable pair: the run's PUBLIC state, which correctly reports the
// run ended the moment Stop lands, versus the contact's DURABLE association,
// which must not depend on when the operator pressed Stop.
func TestRunIdentity_StopMidContactKeepsTheContactsRun(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	var logged []CompletedQso
	s.onComplete = func(c CompletedQso) { logged = append(logged, c) }

	stagedStart(t, s, "auto_first")
	completeSeedContact(t, s)
	runID := r.lastStatus().RunID
	require.NotEmpty(t, runID)

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.NotNil(t, s.caller, "fixture: the auto run must pick DL9UW up")

	s.StopAutoWorkRun() // run ends NOW; the contact runs on
	require.Empty(t, r.lastStatus().RunID,
		"the run's public identity ends with the stop — that half is correct")

	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ DL9UW R-08", -8)})
	require.Len(t, logged, 2, "fixture: the in-flight contact must complete")
	require.Equal(t, runID, logged[1].RunID,
		"RI9: the contact keeps the run that worked it, however the run ended")
}

// RI7 — BuildQso carries the id onto the QSO's APP field; empty stays empty.
func TestRunIdentity_BuildQsoStampsAppField(t *testing.T) {
	station := types.LoggingStation{StationCallsign: "G0XYZ", MyGridsquare: "IO91"}
	now := time.Unix(1000, 0).UTC()

	withRun := BuildQso(CompletedQso{
		TheirCall: "DL9UW", TheirGrid: "JO41",
		StartedAt: now, DialFreqMHz: 14.074, OffsetHz: 1500,
		RunID: "0198c0de-0000-7000-8000-000000000001",
	}, station, 1, now, nil)
	require.Equal(t, "0198c0de-0000-7000-8000-000000000001", withRun.AppSmRunID,
		"RI7: the run id must reach the QSO's APP field")

	without := BuildQso(CompletedQso{
		TheirCall: "DL9UW", TheirGrid: "JO41",
		StartedAt: now, DialFreqMHz: 14.074, OffsetHz: 1500,
	}, station, 1, now, nil)
	require.Empty(t, without.AppSmRunID, "RI7: no phantom APP field on non-run QSOs")
}
