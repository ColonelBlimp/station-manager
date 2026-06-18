package ft8

import (
	stderr "errors"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// Work-a-caller (ADR 0033): the operator picks a station calling us from the pile-up;
// we run a caller-style exchange (report → RR73) and go idle on completion. The worked
// station transmits in even slots (driveTheir uses even sec); we reply in the odd ones.

func TestWorkCaller_HappyPath(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	// Work K1ABC, who called us "G0XYZ K1ABC FN42" in the even slot at epoch 0; we
	// measured them at -12 (the report we send). now even → no immediate fire.
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active())

	// Initial status: worker role (caller-style ladder, no CQ), reporting rung, cap
	// advertised, our report known, and the caller's grid carried for the ladder.
	st := r.lastStatus()
	require.Equal(t, roleWorker, st.Role)
	require.Equal(t, "reporting", st.State)
	require.Equal(t, "K1ABC", st.TheirCall)
	require.Equal(t, "FN42", st.TheirGrid)
	require.Equal(t, "K1ABC G0XYZ -12", st.NextMessage)
	require.Equal(t, "-12", st.OurReport)
	require.Equal(t, defaultSeqMaxRepeats, st.MaxRepeats)

	// Our first slot: they re-call (no roger yet) → we send our report.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
	// They roger with their report → we send RR73 and the QSO completes + logs idle.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)})

	require.Equal(t, []string{
		"K1ABC G0XYZ -12",
		"K1ABC G0XYZ RR73",
	}, r.sentMsgs())
	for _, o := range r.offsets {
		require.Equal(t, 1500.0, o)
	}
	require.False(t, s.Active(), "work-a-caller goes idle after RR73 (does NOT resume CQ)")
	require.Len(t, r.completed, 1)
	require.Equal(t, "K1ABC", r.completed[0].TheirCall)
	require.Equal(t, "FN42", r.completed[0].TheirGrid)
	require.Equal(t, -12, r.completed[0].OurReport)  // we sent -12 (our SNR of them)
	require.Equal(t, -8, r.completed[0].TheirReport) // they sent us R-08
	require.Equal(t, 14.074, r.completed[0].DialFreqMHz)
	require.Equal(t, 1500.0, r.completed[0].OffsetHz)
}

// TestWorkCaller_AbandonsAfterMaxRepeats: a silent caller off-ramps to IDLE (not CQ).
func TestWorkCaller_AbandonsAfterMaxRepeats(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.maxRepeats = 2
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// No roger ever: report, report, then abandon (no third transmit).
	driveTheir(s, 30, nil) // repeat 1
	driveTheir(s, 60, nil) // repeat 2
	driveTheir(s, 90, nil) // would be repeat 3 > max → abandon

	require.Equal(t, []string{"K1ABC G0XYZ -12", "K1ABC G0XYZ -12"}, r.sentMsgs())
	require.False(t, s.Active(), "abandons after max unanswered repeats")
	require.Empty(t, r.completed, "an abandoned (un-rogered) contact is not logged")
}

func TestWorkCaller_StartErrors(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)

	require.ErrorIs(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, slot, 0, 14.074, time.Unix(0, 0).UTC()), ErrNoOffset)
	require.ErrorIs(t, s.StartWorkCaller("", "K1ABC", "FN42", -12, slot, 1500, 14.074, time.Unix(0, 0).UTC()), ErrNoCall)
	require.Error(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, "not-a-time", 1500, 14.074, time.Unix(0, 0).UTC()))

	// One session at a time.
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, slot, 1500, 14.074, time.Unix(0, 0).UTC()))
	require.ErrorIs(t, s.StartWorkCaller("G0XYZ", "W2XYZ", "EM12", -12, slot, 1500, 14.074, time.Unix(0, 0).UTC()), ErrQsoInProgress)
}

// A compound/portable caller yields an unencodable reply → refused up front, nothing
// committed (mirrors StartQso / the Call-CQ M2 guard).
func TestWorkCaller_RejectsUnencodableCaller(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	// Type-4 compound — still unencodable (the standard /P variant encodes on
	// go-ft8 ≥ v0.3.5, so it's no longer a rejection example).
	err := s.StartWorkCaller("G0XYZ", "PJ4/K1ABC", "FN42", -12, slot, 1500, 14.074, time.Unix(0, 0).UTC())
	require.True(t, stderr.Is(err, ErrTxBadMessage))
	require.False(t, s.Active(), "an unencodable caller must not commit a session")
}

// Abandon drops a work-a-caller session like any other.
func TestWorkCaller_Abandon(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	s.Abandon()
	require.False(t, s.Active())
	driveTheir(s, 30, nil)
	require.Empty(t, r.sentMsgs(), "no transmit after abandon")
}
