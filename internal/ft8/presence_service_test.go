package ft8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Presence in the FT8 scope (W-0027 C4b). With ownership required (the operating-client
// check on), an arm needs the current lifetime to have its FT8 event stream open, and is
// held only while it does: when that lifetime has had no stream for the grace, the daemon
// disarms as unattended, by the path every other stop takes. With ownership not required
// nothing changes from before C4.

// presentService is an owned service whose grace is on a clock the test owns.
func presentService(t *testing.T, current string) (*Service, *graceClock) {
	t.Helper()
	s := newOwnedService(t, current)
	clock := &graceClock{}
	s.presence = newOperatorPresence(func() time.Duration { return captureLinger }, clock.after, s.onPresenceLost)
	return s, clock
}

// graceNow is the helper's current grace, as a decision made now would name it.
func (p *operatorPresence) graceNow() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.graceN
}

// decided reports whether the helper has judged a grace to have run out and handed, or is
// about to hand, that decision on: the pending timer is gone while the hold remains.
func (p *operatorPresence) decided() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.timer == nil && p.held != (presenceHold{})
}

// stream opens the FT8 event stream as a lifetime ("" for a caller that named none).
func stream(t *testing.T, s *Service, lifetime string) (closeStream func()) {
	t.Helper()
	_, unsub, err := s.SubscribeModeAs("", lifetime)
	require.NoError(t, err)
	return unsub
}

func (s *Service) armGenNow() uint64 {
	gen, _ := s.armState()
	return gen
}

func (s *Service) disarmCauseNow() string {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	return s.txDisarmCause
}

func TestPresence_AnArmNeedsTheCurrentLifetimesStreamOpen(t *testing.T) {
	s, _ := presentService(t, lifeA)
	a := s.OperatingAs(lifeA)

	require.ErrorIs(t, a.ArmTx(true), ErrNotPresent, "armed with no FT8 stream open at all")
	stream(t, s, "") // a tool's stream, naming no lifetime
	require.ErrorIs(t, a.ArmTx(true), ErrNotPresent, "a stream with no lifetime stood in for the app's")
	stream(t, s, lifeB) // another lifetime's stream
	require.ErrorIs(t, a.ArmTx(true), ErrNotPresent, "another lifetime's stream stood in for the app's")
	require.False(t, s.armedNow())

	stream(t, s, lifeA)
	require.NoError(t, a.ArmTx(true))
	require.True(t, s.armedNow())
}

func TestPresence_LossForTheGraceDisarmsAsUnattended(t *testing.T) {
	s, clock := presentService(t, lifeA)
	stream(t, s, "") // a tool keeps reading throughout: it must not hold the arm
	closeA := stream(t, s, lifeA)
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))
	require.Empty(t, clock.pending())

	closeA() // the FT8 view is left, or the app is gone
	require.True(t, s.armedNow(), "disarmed before the grace had run")
	require.Len(t, clock.pending(), 1)
	require.Equal(t, captureLinger, clock.pending()[0].d, "the grace is the one ruled: the capture linger")

	clock.pending()[0].f()
	require.False(t, s.armedNow(), "the grace ran out and TX stayed armed")
	require.Equal(t, disarmUnattended, s.disarmCauseNow())
}

func TestPresence_AReturnInsideTheGraceKeepsTheArm(t *testing.T) {
	s, clock := presentService(t, lifeA)
	closeA := stream(t, s, lifeA)
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))
	closeA()
	pending := clock.pending()[0]

	stream(t, s, lifeA) // the same app, back inside the grace
	pending.f()         // the timer, fired late
	require.True(t, s.armedNow(), "a return inside the grace lost the arm")
	require.Empty(t, clock.pending())
}

func TestPresence_ARepeatedArmChangesNothingAndDoesNotExtendTheGrace(t *testing.T) {
	s, clock := presentService(t, lifeA)
	closeA := stream(t, s, lifeA)
	a := s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))
	closeA()
	pending := clock.pending()
	require.Len(t, pending, 1)

	// Already armed, and inside the grace with no stream: accepted, as before C4.
	require.NoError(t, a.ArmTx(true))
	require.Equal(t, pending, clock.pending(), "a repeated arm started the grace afresh")
	pending[0].f()
	require.False(t, s.armedNow(), "a repeated arm kept TX armed past the grace")
}

// Every disarm ends the hold, whatever its cause: once TX is disarmed, the app's stream
// can close without any grace being run for an arm that is gone.
func TestPresence_ADisarmEndsTheHold(t *testing.T) {
	s, clock := presentService(t, lifeA)
	closeA := stream(t, s, lifeA)
	a := s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))

	require.NoError(t, a.ArmTx(false))
	closeA()
	require.Empty(t, clock.pending(), "a grace was started for an arm that had been disarmed")
}

// The grace's decision is tied to the arm it was made for: it is checked again under the
// gate, and a late one never ends a later arm (codex-124, answer 3).
func TestPresence_ALateDecisionNeverEndsALaterArm(t *testing.T) {
	s, clock := presentService(t, lifeA)
	a := s.OperatingAs(lifeA)
	closeA := stream(t, s, lifeA)
	require.NoError(t, a.ArmTx(true))
	firstArm := s.armGenNow()
	closeA()
	late := clock.pending()[0]

	require.NoError(t, a.ArmTx(false)) // the operator disarms; the hold ends
	closeAgain := stream(t, s, lifeA)
	require.NoError(t, a.ArmTx(true))
	require.NotEqual(t, firstArm, s.armGenNow())

	late.f()
	require.True(t, s.armedNow(), "the first arm's timer ended the second arm")

	// Past the helper, the service checks each part of the decision for itself.
	// A decision for this arm, made while its stream is open again, ends nothing.
	s.onPresenceLost(lifeA, s.armGenNow(), s.presence.graceNow())
	require.True(t, s.armedNow(), "the arm was ended although its lifetime is present")
	// A decision for the EARLIER arm, arriving while the later arm has no stream and its
	// own grace has not run, ends nothing: it is not this arm's decision.
	closeAgain()
	require.False(t, s.presence.has(lifeA))
	s.onPresenceLost(lifeA, firstArm, s.presence.graceNow())
	require.True(t, s.armedNow(), "a decision for an earlier arm ended a later one")
	// The later arm's own decision, for its own grace, with its lifetime still absent,
	// does end it.
	s.onPresenceLost(lifeA, s.armGenNow(), s.presence.graceNow())
	require.False(t, s.armedNow(), "the arm's own decision, with no stream, ended nothing")
}

// codex-134: a decision is for ONE absence. The helper judges a grace to have run out and
// hands the decision to the service, which may have to wait for the gate. If the app
// returns and leaves again meanwhile, a new grace has started for the SAME arm, and the
// old decision must not end the arm before that grace has run. The gate is held by the
// test, so the order is fixed without any waiting on the clock.
func TestPresence_AQueuedDecisionIsForOneAbsenceOnly(t *testing.T) {
	// queued arms, loses presence, and lets the grace run out with the gate held: the
	// decision is then waiting to be acted on. It returns the way to let it through.
	queued := func(t *testing.T) (s *Service, clock *graceClock, acted chan struct{}, letThrough func()) {
		s, clock = presentService(t, lifeA)
		closeA := stream(t, s, lifeA)
		require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))
		closeA()
		grace := clock.pending()[0]

		s.seqGate.Lock()
		acted = make(chan struct{})
		go func() {
			defer close(acted)
			grace.f() // the helper decides, then waits here for the gate
		}()
		// The gate is let go when the test ends even if it fails first: a failed test
		// that still held it would hang in its own clean-up.
		held := true
		letThrough = func() {
			if held {
				held = false
				s.seqGate.Unlock()
				<-acted
			}
		}
		t.Cleanup(letThrough)
		require.Eventually(t, s.presence.decided, 5*time.Second, time.Millisecond,
			"the helper never reached its decision")
		return s, clock, acted, letThrough
	}

	t.Run("control: the absence stands, and the decision disarms", func(t *testing.T) {
		s, _, _, letThrough := queued(t)
		letThrough()
		require.False(t, s.armedNow(), "an absence that stood was not acted on")
	})

	t.Run("control: the app returned and stayed, and the arm stands", func(t *testing.T) {
		s, _, _, letThrough := queued(t)
		stream(t, s, lifeA)
		letThrough()
		require.True(t, s.armedNow())
	})

	t.Run("the app returned and left again: the new grace runs in full", func(t *testing.T) {
		s, clock, _, letThrough := queued(t)
		arm := s.armGenNow()
		before := len(clock.timers)
		stream(t, s, lifeA)() // back, and gone again: a new absence of the same arm
		require.Len(t, clock.timers, before+1, "the new absence started no grace of its own")
		newGrace := clock.timers[before]
		letThrough()
		require.Equal(t, arm, s.armGenNow(), "the fixture: it is still the same arm")
		require.True(t, s.armedNow(),
			"the earlier absence's decision ended the arm before the new grace had run")

		newGrace.f() // the new grace, at its own end
		require.False(t, s.armedNow(), "the new grace ran out and the arm stands")
		require.Equal(t, disarmUnattended, s.disarmCauseNow())
	})
}

// What is running when presence is lost is stopped by the one stop path: a run between
// contacts, and a transmission waiting for its slot.
func TestPresence_LossStopsARunAndATransmissionInFlight(t *testing.T) {
	t.Run("a Call-CQ run between contacts", func(t *testing.T) {
		s, clock := presentService(t, lifeA)
		closeA := stream(t, s, lifeA)
		a := s.OperatingAs(lifeA)
		require.NoError(t, a.ArmTx(true))
		require.NoError(t, a.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, ""))
		require.True(t, s.seq.Active())

		closeA()
		clock.pending()[0].f()
		require.False(t, s.seq.Active(), "the run outlived the loss of presence")
		require.False(t, s.txInFlightNow())
		require.False(t, s.armedNow())
	})
	t.Run("a manual send queued for the next slot", func(t *testing.T) {
		s, clock := presentService(t, lifeA)
		closeA := stream(t, s, lifeA)
		a := s.OperatingAs(lifeA)
		require.NoError(t, a.ArmTx(true))
		require.NoError(t, a.TransmitNext("CQ G0XYZ IO91", 1500))
		require.True(t, s.txInFlightNow(), "the fixture: a transmission is waiting for its slot")

		closeA()
		clock.pending()[0].f()
		require.False(t, s.txInFlightNow(), "the queued transmission outlived the loss of presence")
		require.False(t, s.armedNow())
	})
}

// A replacement ends the hold with the arm. The replaced lifetime's stream, still open,
// is not the new lifetime's presence; and a stream opened before its lifetime was
// admitted counts once it is.
func TestPresence_FollowsTheCurrentLifetime(t *testing.T) {
	s, clock := presentService(t, lifeA)
	stream(t, s, lifeA) // stays open across the replacement
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))
	early := stream(t, s, lifeB) // opened before B is admitted
	_ = early

	s.RetireOperating()
	require.False(t, s.armedNow())
	require.Empty(t, clock.pending(), "a replacement left a grace running for an arm that is gone")
	s.PublishOperating(lifeB)

	require.NoError(t, s.OperatingAs(lifeB).ArmTx(true), "a stream opened before admission did not count once admitted")
	require.ErrorIs(t, s.OperatingAs(lifeA).ArmTx(true), ErrLifetimeNotCurrent)

	// Without a stream of its own the new lifetime is not present, whatever A holds open.
	s2, _ := presentService(t, lifeA)
	stream(t, s2, lifeA)
	s2.RetireOperating()
	s2.PublishOperating(lifeB)
	require.ErrorIs(t, s2.OperatingAs(lifeB).ArmTx(true), ErrNotPresent,
		"the replaced lifetime's open stream was taken as the new one's presence")
}

// With ownership required the count that drives audio capture decides capture only: its
// linger no longer disarms. With ownership not required it disarms, as before C4.
func TestPresence_TheCaptureLingerNoLongerDecidesPresence(t *testing.T) {
	s, _ := presentService(t, lifeA)
	hold := s.presence.open(lifeA) // presence, with no capture subscriber at all
	defer hold()
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))
	s.onLingerExpired()
	require.True(t, s.armedNow(), "the capture linger disarmed although the lifetime is present")

	off := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	t.Cleanup(func() { _ = off.ArmTx(false) })
	require.NoError(t, off.ArmTx(true), "not required: an arm with no subscriber at all")
	off.onLingerExpired()
	require.False(t, off.armedNow(), "not required: the linger must disarm, as before C4")
	require.Equal(t, disarmUnattended, off.disarmCauseNow())
}

// With ownership not required a stream's lifetime is not recorded and holds nothing.
func TestPresence_NotRequired_StreamsAreNotCounted(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	t.Cleanup(func() { _ = s.ArmTx(false) })
	closeA := stream(t, s, lifeA)
	require.False(t, s.presence.has(lifeA))
	closeA()
}

// A stream that is refused is not presence, and closing a stream twice counts once.
func TestPresence_OnlyAnAdmittedStreamCounts(t *testing.T) {
	s, _ := presentService(t, lifeA)
	_, _, err := s.SubscribeModeAs("ft4", lifeA) // the active profile is FT8
	require.ErrorIs(t, err, ErrProfileMismatch)
	require.False(t, s.presence.has(lifeA), "a refused stream was counted as presence")

	closeOne := stream(t, s, lifeA)
	stream(t, s, lifeA)
	closeOne()
	closeOne()
	require.True(t, s.presence.has(lifeA), "one of two streams, closed twice, ended presence")
}

// The stream handler takes the lifetime from the request by the accessor it is given, and
// counts it for as long as the request is served.
func TestPresence_TheStreamHandlerCountsTheRequestsLifetime(t *testing.T) {
	s, _ := presentService(t, lifeA)
	s.SetStreamLifetime(func(r *http.Request) string { return r.Header.Get("X-Test-Lifetime") })

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/v1/ft8/events", nil).WithContext(ctx)
	req.Header.Set("X-Test-Lifetime", lifeA)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.HTTPHandler(make(chan struct{})).ServeHTTP(httptest.NewRecorder(), req)
	}()
	require.Eventually(t, func() bool { return s.presence.has(lifeA) }, 5*time.Second, 5*time.Millisecond)
	cancel()
	<-done
	require.False(t, s.presence.has(lifeA), "the stream ended and its lifetime is still counted")

	// With no accessor given, a stream names no lifetime.
	bare, _ := presentService(t, lifeA)
	ctx2, cancel2 := context.WithCancel(context.Background())
	req2 := httptest.NewRequest(http.MethodGet, "/v1/ft8/events", nil).WithContext(ctx2)
	req2.Header.Set("X-Test-Lifetime", lifeA)
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		bare.HTTPHandler(make(chan struct{})).ServeHTTP(httptest.NewRecorder(), req2)
	}()
	require.Eventually(t, func() bool {
		bare.mu.Lock()
		defer bare.mu.Unlock()
		return bare.subCount == 1
	}, 5*time.Second, 5*time.Millisecond)
	require.False(t, bare.presence.has(lifeA))
	cancel2()
	<-done2
}

// The service's own helper, on real timers: the grace it uses is the capture linger, and
// its end disarms. (The other tests here give the helper a clock of their own.)
func TestPresence_TheServicesOwnGraceIsTheCaptureLinger(t *testing.T) {
	saved := captureLinger
	captureLinger = 40 * time.Millisecond
	t.Cleanup(func() { captureLinger = saved })

	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	s.RequireOperatingLifetime()
	s.PublishOperating(lifeA)
	t.Cleanup(func() { _ = s.ArmTx(false) })
	closeA := stream(t, s, lifeA)
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))

	closeA()
	require.True(t, s.armedNow(), "disarmed at once, with no grace")
	require.Eventually(t, func() bool { return !s.armedNow() }, 5*time.Second, 5*time.Millisecond,
		"the service's own grace never disarmed")
	require.Equal(t, disarmUnattended, s.disarmCauseNow())
}
