package ft8

import (
	"context"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	The dial-guard SPECIFICATION, written before the implementation (2026-07-27).

	An FT8 exchange lives on ONE dial frequency. Everything here follows from that
	and from a decision the operator made deliberately: there is NO tolerance. A
	dial change is a statement of intent to leave, whether deliberate or accidental,
	and SM treats it as one.

	Why no tolerance, recorded so it is not re-litigated by the next threshold that
	looks reasonable: survivability depends on where the partner happens to sit in
	the passband and which way you moved, so there is no clean physical edge to pick.
	Every threshold tried before this spec existed was wrong — a 30 s quarantine that
	admitted the exact case it targeted, then a 45 s one. "The rig is where the
	session pinned it, or it is not" is a fact; "the partner is probably still
	copyable" is a guess. We act on the fact.

	The seven rules, each a test below:

	  1. A session is bound to the dial the daemon read at start.
	  2. ANY change to that dial ends the session — no tolerance.
	  3. It ends when the move is OBSERVED, not at the next rung.
	  4. An in-flight transmission is aborted; PTT drops.
	  5. TX is disarmed; the operator must re-arm. This holds with NO session too:
	     an arm is bound to a frequency exactly as a session is, and leaving TX armed
	     across a QSY is the setup for keying on a frequency nobody armed for.
	  6. A contact already rogered is still logged, on the PINNED frequency.
	  7. The operator is told what happened and why.

	Rule 6 is the one that must survive all the strictness above it: dropping TX
	must never drop a contact that already happened on the air.

	Rule 4's rationale corrects an earlier assumption. Letting the rung finish looked
	kinder — a complete, decodable message — but the rig's carrier moves WITH the
	dial, so the tail of the waveform is transmitted on the new frequency. What goes
	out is a signal that jumps frequency mid-transmission: undecodable to anyone and
	QRM wherever it lands. Aborting produces less garbage, not more.
*/

// dialGuardService builds an armed Service whose dial reads from *dial, so a test
// can move the rig by assigning to it. Returns the keyer so PTT is observable.
func dialGuardService(t *testing.T, dial *float64) (*Service, *fakeKeyer) {
	t.Helper()
	k := &fakeKeyer{}
	s := newTxTestService(k, newFakeTxPlayer(), nil)
	s.SetDialSource(func() (float64, bool) { return *dial, true })
	require.NoError(t, s.ArmTx(true))
	return s, k
}

// startCq starts a Call-CQ session and returns the statuses published from here on.
func startGuardCq(t *testing.T, s *Service) *[]QsoStatus {
	t.Helper()
	var published []QsoStatus
	s.seq.publish = func(st QsoStatus) { published = append(published, st) }
	require.NoError(t, s.StartCallCq("7Q5MLV", "IO91", 1500, 14.074, "", 1))
	require.True(t, s.seq.Active(), "fixture: a session must be running to test its end")
	return &published
}

// --- 1 + 2: bound to the start dial, and no tolerance ------------------------

// A session is bound to the dial read at start, and ANY departure from it ends the
// session. The table is the point: 1 Hz is treated exactly like a band change,
// because the rule is "moved", not "moved far".
func TestDialGuard_AnyChangeEndsTheSession(t *testing.T) {
	cases := []struct {
		name string
		to   float64
	}{
		{"one hertz", 14.074001},
		{"a hundred hertz", 14.0741},
		{"one kilohertz", 14.075},
		{"across the band edge", 7.074},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dial := 14.074
			s, _ := dialGuardService(t, &dial)
			startGuardCq(t, s)

			dial = tc.to
			s.onDialMoved()

			require.False(t, s.seq.Active(),
				"the rig left the frequency this session was bound to; there is no tolerance")
		})
	}

	t.Run("an unchanged dial leaves the session alone", func(t *testing.T) {
		dial := 14.074
		s, _ := dialGuardService(t, &dial)
		startGuardCq(t, s)

		s.onDialMoved() // spurious call, dial unchanged

		require.True(t, s.seq.Active(),
			"nothing moved — ending the session here would abandon every QSO on a stray report")
	})
}

// --- 3: ends on observation, not at the next rung ----------------------------

// The session must be over the moment the move is seen. Waiting for the next rung
// means up to 30 s on a CQ, during which the screen is unchanged and the operator
// cannot tell a deliberate stop from a hang — which is exactly how a WORKING guard
// was first read on air as "moving the dial does not stop TX" (dogfood 2026-07-27).
func TestDialGuard_EndsOnObservationNotAtTheNextRung(t *testing.T) {
	dial := 14.074
	s, _ := dialGuardService(t, &dial)
	startGuardCq(t, s)

	dial = 14.075
	s.onDialMoved()

	require.False(t, s.seq.Active(),
		"no further rung has been driven — the session must already be over")
}

// The scheduler is what actually notices: it samples the dial on every audio batch
// (~43 ms), so the guard fires within a slot rather than at the next rung.
func TestDialGuard_SchedulerReportsAMoveAsSoonAsItSeesOne(t *testing.T) {
	readings := []float64{14.074, 14.074, 14.075}
	n := 0
	sch := NewScheduler(make(chan []int16), nil)
	sch.SetDialSource(func() (float64, bool) {
		r := readings[n]
		if n < len(readings)-1 {
			n++
		}
		return r, true
	})

	moves := 0
	sch.SetOnDialMoved(func() { moves++ })

	for range readings {
		sch.observeDial()
	}

	require.Equal(t, 1, moves,
		"one move, reported once — from the per-batch sampling, not a slot boundary")
}

// --- 4: the in-flight transmission is aborted --------------------------------

// PTT comes down immediately. The carrier moves with the dial, so continuing would
// transmit the tail of the waveform on the NEW frequency: undecodable, and QRM
// where it lands.
func TestDialGuard_AbortsTheTransmissionInFlight(t *testing.T) {
	dial := 14.074
	s, k := dialGuardService(t, &dial)
	startGuardCq(t, s)

	// Get a rung genuinely KEYED and playing. Driven through the controller's
	// transmit directly rather than TransmitCurrentSlot, which would wait for slot
	// alignment — that is scaffolding, not spec: the assertions below are about the
	// real keying path (fakeTxPlayer.Play blocks, so the transmission stays in
	// flight until something cancels it).
	zeroTiming(t)
	gen := s.seq.currentGen()
	require.NoError(t, s.startTransmission("CQ 7Q5MLV KH78", 1500, 14.074,
		func() bool { return s.seq.isCurrent(gen) },
		func(ctx context.Context, ctrl *TxController) error {
			return ctrl.transmit(ctx, []int16{1, 2, 3}, time.Time{}, nil)
		}, nil, nil))
	require.Eventually(t, func() bool { return k.keys() > 0 }, time.Second, 10*time.Millisecond,
		"fixture: the rung must be keyed before the move, or there is nothing to abort")

	dial = 14.075
	s.onDialMoved()

	require.Eventually(t, func() bool { return k.unkeys() >= k.keys() }, time.Second, 10*time.Millisecond,
		"PTT must drop — the rig is transmitting on a frequency the session never pinned")
	require.False(t, s.txInFlightNow(), "and the transmission must be over, not merely disowned")
}

// --- 5: TX is disarmed ------------------------------------------------------

// A frequency change forces a deliberate re-arm. SM is attended-only by design, and
// leaving TX armed across a QSY is the setup for keying on a frequency nobody armed
// for. Costs a click per band change; that is the price of the guarantee.
func TestDialGuard_DisarmsTransmit(t *testing.T) {
	dial := 14.074
	s, _ := dialGuardService(t, &dial)
	startGuardCq(t, s)

	dial = 14.075
	s.onDialMoved()

	require.ErrorIs(t, s.TransmitNext("CQ 7Q5MLV IO91", 1500), ErrTxNotArmed,
		"TX must be disarmed by the move")
	require.ErrorIs(t, s.StartCallCq("7Q5MLV", "IO91", 1500, 14.075, "", 1), ErrTxNotArmed,
		"and a new session must not start until the operator re-arms")

	require.NoError(t, s.ArmTx(true), "re-arming is all it takes")
	require.NoError(t, s.StartCallCq("7Q5MLV", "IO91", 1500, 14.075, "", 1),
		"and then work continues on the new frequency")
	s.AbandonQso()
}

// Armed but idle is the same hazard with less warning: nothing on screen says the
// arm belongs to a frequency, so a band change followed by a click would key where
// the operator never armed. Costs a re-arm per band change while idle; agreed
// deliberately (2026-07-27).
func TestDialGuard_DisarmsEvenWithNoSessionRunning(t *testing.T) {
	dial := 14.074
	s, _ := dialGuardService(t, &dial)
	require.False(t, s.seq.Active(), "fixture: armed, but nothing running")

	dial = 14.075
	s.onDialMoved()

	require.ErrorIs(t, s.TransmitNext("CQ 7Q5MLV IO91", 1500), ErrTxNotArmed,
		"an arm is bound to a frequency too")
}

// --- 6: a rogered contact is still logged, on the pinned frequency -----------

// The invariant that must survive every rule above it. The partner has rogered, so
// the contact HAPPENED; dropping TX must not un-make it. It is logged on the dial
// the session was bound to — not the one the rig moved to — because that is where
// it took place.
func TestDialGuard_StillLogsAContactThatAlreadyHappened(t *testing.T) {
	dial := 14.074
	s, _ := dialGuardService(t, &dial)

	r := &seqRecorder{}
	stamp := s.seq.prepareComplete
	s.seq = newTestSeq(r)
	s.seq.prepareComplete = stamp
	var logged []CompletedQso
	s.seq.onComplete = func(c CompletedQso) { logged = append(logged, c) }

	require.NoError(t, s.sessionTxGate("test")) // pins the daemon's own dial
	require.NoError(t, s.seq.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// Work them to the point where they roger: the contact is now ours to log.
	driveTheir(s.seq, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s.seq, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})

	dial = 14.075 // QSY before the closing rung can go out
	driveTheir(s.seq, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

	require.Len(t, logged, 1, "a rogered contact is logged exactly once, whatever happens to TX")
	require.InDelta(t, 14.074, logged[0].DialFreqMHz, 1e-9,
		"and on the frequency it happened on, not the one we moved to")
}

// --- 7: the operator is told ------------------------------------------------

// The terminal frame carries WHY. A stop nobody can see is indistinguishable from a
// hang, and that is not a theoretical concern: it is how this behaviour was first
// read on the air.
func TestDialGuard_TellsTheOperatorWhy(t *testing.T) {
	dial := 14.074
	s, _ := dialGuardService(t, &dial)
	published := startGuardCq(t, s)

	dial = 14.075
	s.onDialMoved()

	require.NotEmpty(t, *published, "the session end must be published, not silent")
	last := (*published)[len(*published)-1]
	require.False(t, last.Active)
	require.Equal(t, EndReasonDialMoved, last.EndReason,
		"the operator must be able to tell a deliberate stop from a hang")
}

// --- the band buttons take the same path ------------------------------------

// SM's own band change is the same physical event as a hand on the VFO, so it ends
// the session and disarms too. Agreed deliberately (2026-07-27) rather than assumed:
// it means SM disarms itself in response to its own action.
func TestDialGuard_SmsOwnBandChangeIsNotSpecial(t *testing.T) {
	dial := 14.074
	s, _ := dialGuardService(t, &dial)
	startGuardCq(t, s)

	dial = 7.074 // as if set_band drove the rig here
	s.onDialMoved()

	require.False(t, s.seq.Active(), "a band button is a dial change like any other")
	require.ErrorIs(t, s.TransmitNext("CQ 7Q5MLV IO91", 1500), ErrTxNotArmed)
}

// --- the two halves are actually connected ----------------------------------

// The spec pins "the scheduler notices" and "the Service reacts" separately, so
// this covers the wire between them: a capture session must hand the scheduler a
// callback that reaches onDialMoved. Without it both halves pass and the guard
// never fires on air — which is precisely the shape of bug that survives a suite
// of green unit tests.
func TestDialGuard_CaptureSessionWiresTheSchedulerToTheService(t *testing.T) {
	dial := 14.074
	s, _ := dialGuardService(t, &dial)
	startGuardCq(t, s)

	// Stand up the scheduler exactly as startCaptureLocked does.
	sch := NewScheduler(make(chan []int16), nil)
	sch.SetDialSource(s.dialSource)
	sch.SetOnDialMoved(func() { s.onDialMoved() })

	sch.observeDial() // baseline
	dial = 14.075
	sch.observeDial() // the move

	require.False(t, s.seq.Active(),
		"a move seen by the scheduler must reach the guard — otherwise both halves "+
			"pass in isolation and nothing fires on the air")
	require.ErrorIs(t, s.TransmitNext("CQ 7Q5MLV IO91", 1500), ErrTxNotArmed)
}
