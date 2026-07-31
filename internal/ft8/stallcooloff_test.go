package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	STALLED-CALLER COOL-OFF — found in dogfooding, 2026-07-31.

	WHAT HAPPENED (smd.log, times UTC+02:00). A Call-CQ run answered UA4FKT at
	03:27:30. They repeated their grid once, never copied our report, and went
	silent — their last decode of the night was 03:27:45. We sent the report six
	times into that silence and gave up at 03:30:30. The operator then worked them
	by hand at 03:33:16 and we sent SIX MORE reports, 03:33:30 to 03:36:00, to a
	station that had been off the air for over five minutes.

	THE DEFECT THIS FIXES is the one underneath that. The Call-CQ path records an
	answerer that stalls at the repeat cap in stalledCalls, so pickAnswererLocked
	will not immediately re-lock onto it. THE WORK-A-CALLER PATH RECORDS NOTHING —
	onSlotWorking just retires the session. Auto-work (ADR 0059) selects through
	that same pickAnswererLocked, so a station that stalls the ladder AND KEEPS
	CALLING is re-picked on the very next slot, stalls again, and is re-picked
	again: six transmissions a cycle, indefinitely, with the rest of the pile-up
	starved behind it. It did not fire on 2026-07-31 only because UA4FKT had
	stopped transmitting altogether.

	ACCEPTANCE CRITERIA (operator-approved 2026-07-31):

	  1. When an auto-work contact stalls at the repeat cap and that station keeps
	     calling, the run does NOT re-pick it while the exclusion holds — and I can
	     tell that apart from the run having STOPPED, because the armed indicator
	     is still lit and another caller is still picked up.
	  2. When the exclusion expires, that station can be worked again — I can tell
	     that apart from it never having been excluded.
	  3. A stalled station does not block anyone else: if someone else is calling
	     in the same slot, they are worked.

	OPERATOR'S NUMBERS, not inferred: exclusion lasts FIVE SLOTS, and Abandon
	clears it. Five slots is 75 s of slot time, so it is expressed as a deadline
	rather than a countdown — a counter would be wrong across a capture gap, where
	slots stop arriving but the wall clock does not.

	SCOPE, stated rather than assumed:

	  - Only the SILENT-ANSWERER stall records a cool-off. The other branch of the
	    same cap ("final RR73 never transmitted") is OUR failure, not theirs: that
	    station is still waiting for a roger it never got, and caller_sequencer.go
	    already carries the operator-ratified position that working them again is
	    the CORRECT on-air behaviour. Excluding them would deny a station its
	    contact to keep our log tidy.
	  - Only the standard work-a-caller ladder. FD and type-4 sessions never arm an
	    auto-work run (ADR 0059 scope), so they cannot produce this loop.
	  - The Call-CQ path is untouched: it populates stalledCalls, not this, so its
	    existing per-round exclusion and the load-bearing clear-when-the-rescan-is-
	    empty behaviour both stay exactly as they were.

	NOT REUSING stalledCalls, and this is the whole design point. Its expiry rule
	is "clear the moment a rescan finds nobody else calling" — deliberately, so
	that in a CQ round the ONLY station answering does not stay excluded forever.
	In an auto-work run "nobody else is calling" is the normal quiet state, so a
	shared list would be wiped on the next empty slot and the stalled station
	re-picked immediately. Same word, opposite lifetime.
*/

// stallDL9UW drives the fixture every rule here starts from: an operator-started
// run is armed, DL9UW is auto-picked, and then stalls at the repeat cap having
// never answered. Returns with the sequencer idle, the run still armed, and DL9UW
// inside its cool-off.
//
// TIMING, measured rather than assumed (the first draft of this file was a slot
// out and C2 passed for the wrong reason). maxRepeats is 2 and the pick's own
// fireOpening is the FIRST transmission, so the cap fires on the SECOND silent
// slot, 150. driveTheir hands OnSlot now = sec+16, so the stall is recorded at
// now=166 and the five-slot cool-off runs to 241.
func stallDL9UW(t *testing.T, s *Sequencer) {
	t.Helper()
	s.maxRepeats = 2
	autoWorkRun(t, s, "auto_first")

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.Equal(t, "DL9UW", s.caller.TheirCall, "fixture: the run must pick DL9UW up")
	driveTheir(s, 120, nil)
	require.NotNil(t, s.caller, "fixture: one silent slot is not yet the cap")
	driveTheir(s, 150, nil)
	require.Nil(t, s.caller, "fixture: DL9UW must stall at the repeat cap here")
}

// C1 — THE FEATURE. The station that just stalled keeps calling and is left
// alone, while the run stays armed. Both halves are the criterion: an
// implementation that stopped the run would also produce no transmission here,
// and that is the behaviour the operator explicitly did NOT want.
func TestStallCooloff_StalledCallerIsNotRePickedImmediately(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stallDL9UW(t, s)

	before := len(r.sentMsgs())
	driveTheir(s, 180, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.Nil(t, s.caller, "the station that just stalled must not be re-picked")
	require.False(t, s.Active())
	require.Len(t, r.sentMsgs(), before, "nothing may be transmitted to a station in cool-off")
	require.True(t, s.AutoWorkArmed(),
		"this is an exclusion, not a stop — the run must still be armed and waiting")
}

// C2 — it is a COOL-OFF, not a ban. After five slots the same station is worked
// normally. Without this rule an implementation that excluded forever passes C1,
// and a station that merely had a bad couple of minutes would be locked out for
// the rest of the session.
func TestStallCooloff_ExpiresAfterFiveSlots(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stallDL9UW(t, s)

	// Still inside the window (stall recorded at now=166; five slots ends at 241).
	driveTheir(s, 180, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.Nil(t, s.caller, "slot 180 (now=196): still in cool-off")
	driveTheir(s, 210, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})
	require.Nil(t, s.caller, "slot 210 (now=226): still in cool-off")

	// Past it (now=256).
	driveTheir(s, 240, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.NotNil(t, s.caller, "the cool-off must expire, not exclude for the session")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// C3 — THE DISCRIMINATOR, and the rule that makes this an exclusion of one
// STATION rather than a pause of the whole run. A blanket "wait five slots after
// any stall" would pass C1 and C2 and quietly starve the pile-up it exists to
// keep moving.
func TestStallCooloff_DoesNotBlockAnyoneElse(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stallDL9UW(t, s)

	driveTheir(s, 180, []goft8.DecodedMessage{
		dm("G0XYZ DL9UW JO41", -8), // in cool-off — skipped
		dm("G0XYZ 9A4ZM JN95", -6), // free to be worked, same slot
	})

	require.NotNil(t, s.caller, "another caller in the same slot must still be worked")
	require.Equal(t, "9A4ZM", s.caller.TheirCall)
}

// C4 — Abandon clears the cool-off (operator's call). Paired with C5, which is
// what makes this test mean "ABANDON cleared it" rather than "something between
// here and there cleared it".
func TestStallCooloff_AbandonClearsIt(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stallDL9UW(t, s)

	s.Abandon()

	// A fresh Call-CQ round, still well inside the five slots. DL9UW answers it.
	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "auto_first", "odd",
		time.Unix(160, 0).UTC()))
	driveTheir(s, 180, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.NotNil(t, s.caller, "Abandon must clear the cool-off")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
}

// C5 — the cool-off SURVIVES a new session that is not an Abandon. Without this,
// C4 proves nothing: if merely starting the next session cleared the exclusion,
// the auto-work loop this whole change exists to break would still be reachable
// (every re-pick starts a session), and C4 would pass anyway.
func TestStallCooloff_SurvivesANewSessionWithoutAbandon(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	stallDL9UW(t, s)

	// No Abandon — straight into a fresh Call-CQ round, as an operator picking the
	// run back up would.
	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "auto_first", "odd",
		time.Unix(160, 0).UTC()))
	driveTheir(s, 180, []goft8.DecodedMessage{dm("G0XYZ DL9UW JO41", -8)})

	require.Nil(t, s.caller,
		"only Abandon clears the cool-off; starting a session must not launder it")
}
