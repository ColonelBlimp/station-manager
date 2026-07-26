package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// Reduced type-4 (nonstandard/compound call) sequencer paths — ADR 0048. The worked
// station transmits in even slots (driveTheir uses even sec); we reply in the odd ones.
// Our own call (7Q5MLV) is standard, so the partner hashes it to "<...>" on the wire.

// TestType4_RejectsStandardCall guards ADR 0048: the reduced type-4 entry points are
// ONLY for nonstandard/compound callsigns. A standard pair (e.g. "K1ABC 7Q5MLV")
// still ENCODES — but as a type-1 message, not type 4 — so a bare encodability check
// wrongly admits it, and the work path would key an immediate RR73 with no valid
// type-4 exchange. Both entry points must reject a standard call (it belongs on the
// standard answer/work path). A genuine nonstandard call still starts.
func TestType4_RejectsStandardCall(t *testing.T) {
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	t.Run("StartQsoT4 rejects a standard call", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.ErrorIs(t, s.StartQsoT4("7Q5MLV", "K1ABC", "FN42", -12, slot, 1500, 14.074, now),
			ErrTxBadMessage)
		require.False(t, s.Active(), "no type-4 session may start for a standard call")
	})
	t.Run("StartWorkCallerT4 rejects a standard call", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.ErrorIs(t, s.StartWorkCallerT4("7Q5MLV", "K1ABC", "FN42", -12, slot, 1500, 14.074, now),
			ErrTxBadMessage)
		require.False(t, s.Active(), "no type-4 session may start for a standard call")
	})
	t.Run("a genuine nonstandard call is still accepted", func(t *testing.T) {
		s := newTestSeq(&seqRecorder{})
		require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12, slot, 1500, 14.074, now))
		require.True(t, s.Active(), "a nonstandard call is a valid type-4 partner")
	})
}

// TestType4Answer_HappyPath walks answering a nonstandard station's CQ: bare opening,
// their roger (addressed to our hashed call), our 73, log + idle. No report exchanged.
func TestType4Answer_HappyPath(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	// Answer PJ4/NA2AA's CQ heard in the even slot at epoch 0; we measured them at -12
	// (logged as RST_SENT). now even → their parity → no immediate fire.
	require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active())

	st := r.lastStatus()
	require.Equal(t, roleAnswerer, st.Role)
	require.True(t, st.Type4)
	require.Equal(t, "calling", st.State)
	require.Equal(t, "PJ4/NA2AA", st.TheirCall)
	require.Equal(t, "PJ4/NA2AA 7Q5MLV", st.NextMessage)
	require.Equal(t, defaultSeqMaxRepeats, st.MaxRepeats)

	// Their re-CQ (or silence) → we send our bare opening.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ PJ4/NA2AA", -1)})
	// Their roger, addressed to our hashed call → we send 73; the QSO completes + logs.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("<...> PJ4/NA2AA RR73", -11)})

	require.Equal(t, []string{
		"PJ4/NA2AA 7Q5MLV",
		"PJ4/NA2AA 7Q5MLV 73",
	}, r.sentMsgs())
	require.False(t, s.Active(), "answer-a-CQ type-4 goes idle after the 73")
	require.Len(t, r.completed, 1)
	require.Equal(t, "PJ4/NA2AA", r.completed[0].TheirCall)
	require.Equal(t, -12, r.completed[0].OurReport) // our SNR of them → RST_SENT
	require.True(t, r.completed[0].HasOurReport)
	require.False(t, r.completed[0].HasTheirReport, "no report is received in a type-4 exchange")
	require.Empty(t, r.completed[0].TheirGrid, "type-4 CQ carries no grid")
	require.Equal(t, 14.074, r.completed[0].DialFreqMHz)
}

// TestType4Answer_ImmediateOpening fires the opening in the click's current slot when it
// is our parity and within the late window (the answer-a-CQ immediate-fire).
func TestType4Answer_ImmediateOpening(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	// Their CQ was in the even slot at epoch 0; now is 1 s into the odd slot at 15 → our
	// parity, early → fireOpening keys the opening immediately.
	require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -8,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(16, 0).UTC()))
	require.Equal(t, []string{"PJ4/NA2AA 7Q5MLV"}, r.sentMsgs())
}

// TestType4Work_HappyPath walks working a nonstandard caller: single RR73 rung → log +
// idle. The QSO logs after our own RR73 transmits (there is no report and no fireOpening).
func TestType4Work_HappyPath(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	// PJ4/NA2AA called us "<...> PJ4/NA2AA" in the even slot at epoch 0; we measured them
	// at +3 (RST_SENT). now even → no immediate fire (and the work side never fires the
	// opening anyway).
	require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active())

	st := r.lastStatus()
	require.Equal(t, roleWorker, st.Role)
	require.True(t, st.Type4)
	require.Equal(t, "rogering", st.State)
	require.Equal(t, "PJ4/NA2AA 7Q5MLV RR73", st.NextMessage)
	require.Equal(t, defaultSeqMaxRepeats, st.MaxRepeats,
		"the sole RR73 rung is terminal but Group B — its retry is bounded, so the cap is advertised")

	// Our first qualifying slot → we key RR73 and the QSO completes + logs idle.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("<...> PJ4/NA2AA", 3)})

	require.Equal(t, []string{"PJ4/NA2AA 7Q5MLV RR73"}, r.sentMsgs())
	require.False(t, s.Active(), "work-a-caller type-4 goes idle after RR73")
	require.Len(t, r.completed, 1)
	require.Equal(t, "PJ4/NA2AA", r.completed[0].TheirCall)
	require.Equal(t, 3, r.completed[0].OurReport)
	require.False(t, r.completed[0].HasTheirReport)
}

// TestType4Work_ImmediateReply keys the terminal RR73 in the click's current slot when it
// is our parity and within the late window — instead of waiting a full ~30 s for the next
// theirPeriod OnSlot. The work side's sole rung is terminal, so it fires WITH the onDone
// completion (via fireWorkT4, not fireOpening), and the QSO logs immediately.
func TestType4Work_ImmediateReply(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	// PJ4/NA2AA called us in the even slot at epoch 0; now is 1 s into the odd slot at 15 →
	// our parity, early → the RR73 keys immediately (not a full cycle later).
	require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(16, 0).UTC()))

	require.Equal(t, []string{"PJ4/NA2AA 7Q5MLV RR73"}, r.sentMsgs(),
		"the RR73 must key in the immediate reply slot, not a full cycle later")
	require.False(t, s.Active(), "the terminal RR73 completes the work-a-caller type-4 contact")
	require.Len(t, r.completed, 1)
	require.Equal(t, "PJ4/NA2AA", r.completed[0].TheirCall)
	require.Equal(t, 3, r.completed[0].OurReport)
}

// TestType4Work_LogbookPinnedToSession (ADR 0055 regression): work-a-caller-T4 is the
// TERMINAL-FIRST-RUNG path the review flagged — its sole RR73 can complete in the very
// first slot, so the logbook must be pinned AT ACTIVATION, not by a later bind that a
// completion could outrun. Staging setPendingLogbook before the start and completing
// immediately proves the pin lands with the session on this path (guards the consume at
// the seqWorkingT4 activation site).
func TestType4Work_LogbookPinnedToSession(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	s.setPendingLogbook(99)
	require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// The single RR73 rung completes in the first qualifying slot.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("<...> PJ4/NA2AA", 3)})

	require.Len(t, r.completed, 1)
	require.Equal(t, int64(99), r.completed[0].LogbookID)
}

// TestType4Work_RetriesOnRfFailure: if the RR73 fails on air, the contact stays put and
// the next slot retries (no premature log/idle).
func TestType4Work_RetriesOnRfFailure(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	r.asyncFail = true // transmit "queues" then fails on air
	require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, nil) // RR73 keyed but fails
	require.True(t, s.Active(), "an RR73 that did not transmit must not complete the QSO")
	require.Empty(t, r.completed)

	r.asyncFail = false
	driveTheir(s, 60, nil) // retry succeeds
	require.False(t, s.Active())
	require.Len(t, r.completed, 1)
}

// TestType4Answer_SkipIfSilent: an armed skip ends the session on a silent repeat instead
// of keying the opening again (mirrors the standard answer path).
func TestType4Answer_SkipIfSilent(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, nil) // opening (repeat 1)
	require.NoError(t, s.SetSkipIfSilent(true))
	driveTheir(s, 60, nil) // silent → skip ends the session without a second opening

	require.Equal(t, []string{"PJ4/NA2AA 7Q5MLV"}, r.sentMsgs())
	require.False(t, s.Active())
	require.Empty(t, r.completed)
}

func TestType4_StartErrors(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	require.ErrorIs(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12, slot, 0, 14.074, now), ErrNoOffset)
	require.ErrorIs(t, s.StartQsoT4("", "PJ4/NA2AA", "", -12, slot, 1500, 14.074, now), ErrNoCall)
	require.ErrorIs(t, s.StartQsoT4("7Q5MLV", "", "", -12, slot, 1500, 14.074, now), ErrTxBadMessage)
	require.Error(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12, "not-a-time", 1500, 14.074, now))

	// One session at a time (mixing type-4 answer + work).
	require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12, slot, 1500, 14.074, now))
	require.ErrorIs(t, s.StartWorkCallerT4("7Q5MLV", "K1ABC/D", "", 3, slot, 1500, 14.074, now), ErrQsoInProgress)
}

// TestType4_Abandon drops a type-4 session like any other.
func TestType4_Abandon(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	s.Abandon()
	require.False(t, s.Active())
	driveTheir(s, 30, nil)
	require.Empty(t, r.sentMsgs(), "no transmit after abandon")
}
