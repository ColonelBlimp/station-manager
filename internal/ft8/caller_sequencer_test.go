package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// startCq begins a Call-CQ session with theirPeriod forced to "even" so the
// even-sec driveTheir helper drives the answerers' slots: now (Unix 0) sits in an
// even slot → the next slot is odd (our CQ parity) → answerers' parity is even.
func startCq(t *testing.T, s *Sequencer) {
	t.Helper()
	require.NoError(t, s.StartCallCq("7q5mlv", "kh78", 2700, 28.074, "auto_first", "", time.Unix(0, 0).UTC()))
	require.Equal(t, "even", s.theirPeriod)
}

// Caller happy path: CQ → DL9UW answers → we report → they R-report → we RR73 →
// QSO logged → resume calling CQ (loop).
func TestCallerSequencer_HappyPathLoops(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)
	require.True(t, s.Active())

	driveTheir(s, 30, nil)                                                  // no answer → keep calling CQ
	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})  // DL9UW answers → we report
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)}) // they R-report → we RR73 (logs)
	driveTheir(s, 120, nil)                                                 // back to calling CQ (loop)

	require.Equal(t, []string{
		"CQ 7Q5MLV KH78",
		"DL9UW 7Q5MLV -08",
		"DL9UW 7Q5MLV RR73",
		"CQ 7Q5MLV KH78",
	}, r.sentMsgs())

	require.Len(t, r.completed, 1)
	require.Equal(t, "DL9UW", r.completed[0].TheirCall)
	require.Equal(t, "JO41", r.completed[0].TheirGrid)
	require.Equal(t, -8, r.completed[0].OurReport)    // we sent -08
	require.Equal(t, -15, r.completed[0].TheirReport) // they sent R-15
	require.True(t, s.Active())                       // still calling CQ after the QSO
}

// auto_strongest: when several stations answer in the same slot, the daemon works
// the highest-SNR encodable one (clear the loud signals first) regardless of decode
// order — here DL9UW at -3, not the first-decoded K1ABC at -18.
func TestCallerSequencer_AutoStrongestPicksHighestSnr(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t,
		s.StartCallCq("7Q5MLV", "KH78", 2700, 28.074, "auto_strongest", "", time.Unix(0, 0).UTC()))
	require.Equal(t, "even", s.theirPeriod)

	driveTheir(s, 60, []goft8.DecodedMessage{
		dm("7Q5MLV K1ABC FN42", -18),
		dm("7Q5MLV DL9UW JO41", -3),
		dm("7Q5MLV 9A4ZM JN95", -12),
	})
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall) // strongest, not first

	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-10", -3)}) // they R → we RR73 (logs)
	require.Len(t, r.completed, 1)
	require.Equal(t, "DL9UW", r.completed[0].TheirCall)
}

// auto_first ignores SNR: the first answerer by decode order wins even when a
// louder one is in the same slot. The contrast with auto_strongest is the point.
func TestCallerSequencer_AutoFirstPicksFirstByOrder(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t,
		s.StartCallCq("7Q5MLV", "KH78", 2700, 28.074, "auto_first", "", time.Unix(0, 0).UTC()))

	driveTheir(s, 60, []goft8.DecodedMessage{
		dm("7Q5MLV K1ABC FN42", -18), // first by order, weakest
		dm("7Q5MLV DL9UW JO41", -3),  // louder, but later
	})
	require.NotNil(t, s.caller)
	require.Equal(t, "K1ABC", s.caller.TheirCall)
}

func TestCallerSequencer_StartErrors(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	now := time.Unix(0, 0).UTC()
	require.ErrorIs(t, s.StartCallCq("7Q5MLV", "KH78", 0, 28.074, "auto_first", "", now), ErrNoOffset)
	require.ErrorIs(t, s.StartCallCq("", "KH78", 2700, 28.074, "auto_first", "", now), ErrNoCall)
	require.NoError(t, s.StartCallCq("7Q5MLV", "KH78", 2700, 28.074, "auto_first", "", now))
	// One session at a time — a second call-CQ OR an answer-a-CQ is refused.
	require.ErrorIs(t, s.StartCallCq("7Q5MLV", "KH78", 2700, 28.074, "auto_first", "", now), ErrQsoInProgress)
	require.ErrorIs(t,
		s.StartQso("7Q5MLV", "KH78", "K1ABC", "", now.Format(time.RFC3339), 2700, 28.074, now),
		ErrQsoInProgress)
}

// TxParity (WSJT-X "Tx even/1st") picks the CQ slot parity. theirPeriod (the
// answerers' slots we process) is the OPPOSITE of our CQ parity, so asserting it
// pins the choice. now = Unix 0 is an even slot, so the next slot is odd → the
// default (fire ASAP) calls CQ on odd and processes even.
func TestCallerSequencer_TxParityChoice(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	cases := []struct{ parity, wantTheir string }{
		{"", "even"},      // default → CQ on the next slot (odd) → process even
		{"even", "odd"},   // CQ on even slots → process odd
		{"odd", "even"},   // CQ on odd slots → process even
		{"EVEN", "odd"},   // case-insensitive
		{"bogus", "even"}, // unknown value → fall back to the fire-ASAP default
	}
	for _, c := range cases {
		s := newTestSeq(&seqRecorder{})
		require.NoErrorf(t,
			s.StartCallCq("7Q5MLV", "KH78", 2700, 28.074, "auto_first", c.parity, now),
			"parity=%q", c.parity)
		require.Equalf(t, c.wantTheir, s.theirPeriod, "parity=%q", c.parity)
	}
}

// Calling CQ has no repeat cap — it keeps calling until answered or abandoned.
func TestCallerSequencer_NoCapWhileCallingCq(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)
	for i := int64(0); i < int64(s.maxRepeats)+5; i++ {
		driveTheir(s, 30+i*30, nil) // even secs = answerers' parity
	}
	require.True(t, s.Active(), "calling CQ must not auto-abandon")
	for _, m := range r.sentMsgs() {
		require.Equal(t, "CQ 7Q5MLV KH78", m)
	}
}

// A silent answerer mid-exchange is dropped after maxRepeats; the session continues
// calling CQ rather than abandoning.
func TestCallerSequencer_DropsSilentAnswererResumesCq(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)}) // DL9UW answers → reporting
	require.NotNil(t, s.caller)
	require.Equal(t, "DL9UW", s.caller.TheirCall)

	for i := int64(0); i < int64(s.maxRepeats)+1; i++ {
		driveTheir(s, 60+i*30, nil) // they go silent
	}
	require.True(t, s.Active(), "session continues (still calling CQ)")
	require.Nil(t, s.caller, "silent answerer dropped")
	require.Empty(t, r.completed, "no QSO logged for the dropped contact")
	sent := r.sentMsgs()
	require.Equal(t, "CQ 7Q5MLV KH78", sent[len(sent)-1], "resumed calling CQ")
}

// Dogfood 2026-07-17: when the worked answerer goes silent and the contact hits max
// repeats, another station answering us in the SAME slot is worked immediately — the
// abandon slot replies to them instead of wasting a CQ on a live pile-up.
func TestCallerSequencer_AbandonWorksLiveAnswererSameSlot(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)}) // DL9UW answers → reporting
	require.Equal(t, "DL9UW", s.caller.TheirCall)

	// DL9UW goes silent, but 9A4ZM keeps answering our CQ every slot.
	other := []goft8.DecodedMessage{dm("7Q5MLV 9A4ZM JN95", -6)}
	for i := int64(0); i < int64(s.maxRepeats)+1; i++ {
		driveTheir(s, 60+i*30, other)
	}

	require.True(t, s.Active())
	require.NotNil(t, s.caller, "abandon must pick up the live answerer, not drop to CQ")
	require.Equal(t, "9A4ZM", s.caller.TheirCall)
	sent := r.sentMsgs()
	require.Equal(t, "9A4ZM 7Q5MLV -06", sent[len(sent)-1],
		"the abandon slot replies to the live answerer instead of calling CQ")
	require.NotContains(t, sent, "CQ 7Q5MLV KH78",
		"no CQ wasted between the two contacts")

	// The new contact completes and logs normally.
	last := 60 + (int64(s.maxRepeats)+1)*30
	driveTheir(s, last+30, []goft8.DecodedMessage{dm("7Q5MLV 9A4ZM R-12", -6)})
	require.Len(t, r.completed, 1)
	require.Equal(t, "9A4ZM", r.completed[0].TheirCall)
	require.Equal(t, "JN95", r.completed[0].TheirGrid, "grid captured from the abandon-slot answer")
}

// review M2: an auto-first slot whose first answerer is a compound/portable call
// (our reply to it can't encode) must be SKIPPED, not abandon the whole pile-up —
// we fall through to the next encodable answerer in the same slot.
func TestCallerSequencer_AutoFirstSkipsUnencodableAnswerer(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)
	driveTheir(s, 30, []goft8.DecodedMessage{
		dm("7Q5MLV PJ4/K1ABC JO41", -5), // type-4 compound: our report won't encode → skip
		dm("7Q5MLV DL9UW JO41", -8),     // standard: work this one instead
	})
	require.NotNil(t, s.caller, "must pick the encodable answerer, not abandon the session")
	require.Equal(t, "DL9UW", s.caller.TheirCall)
	require.True(t, s.Active())
}

// onSlotCalling acts only in the answerers' (opposite-our-CQ) parity slots; a decode
// in our own parity slot is a no-op.
func TestCallerSequencer_OnlyActsInAnswerersParity(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s) // theirPeriod = even
	ref := SlotRefFromTime(time.Unix(45, 0).UTC())
	require.Equal(t, "odd", ref.Period) // our CQ parity
	s.OnSlot(ref, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)}, time.Unix(45+slotSeconds+1, 0).UTC())
	require.Empty(t, r.sentMsgs(), "must not transmit on our own parity slot")
	require.Nil(t, s.caller)
}

// Abandon stops a Call-CQ session.
func TestCallerSequencer_Abandon(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)
	require.True(t, s.Active())
	s.Abandon()
	require.False(t, s.Active())
	driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	require.Empty(t, r.sentMsgs(), "no transmit after Abandon")
}
