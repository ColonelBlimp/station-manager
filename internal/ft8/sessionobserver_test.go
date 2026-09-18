package ft8

import (
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

type sessionCall struct {
	what    string // end | disarm
	cause   string
	partner string
	rung    string
	at      time.Time
}

type recordingSessionObserver struct {
	mu    sync.Mutex
	calls []sessionCall
}

func (r *recordingSessionObserver) ExchangeTerminated(cause, partner, rung string, at time.Time) {
	r.mu.Lock()
	r.calls = append(r.calls, sessionCall{what: "end", cause: cause, partner: partner, rung: rung, at: at})
	r.mu.Unlock()
}

func (r *recordingSessionObserver) TxDisarmed(cause string, at time.Time) {
	r.mu.Lock()
	r.calls = append(r.calls, sessionCall{what: "disarm", cause: cause, at: at})
	r.mu.Unlock()
}

func (r *recordingSessionObserver) snapshot() []sessionCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sessionCall(nil), r.calls...)
}

// requireOnly asserts the observer saw exactly the given calls (what/cause/partner).
func requireOnly(t *testing.T, r *recordingSessionObserver, want ...sessionCall) {
	t.Helper()
	got := r.snapshot()
	require.Len(t, got, len(want), "observer calls = %+v", got)
	for i := range want {
		require.Equal(t, want[i].what, got[i].what, "call %d = %+v", i, got[i])
		require.Equal(t, want[i].cause, got[i].cause, "call %d = %+v", i, got[i])
		require.Equal(t, want[i].partner, got[i].partner, "call %d = %+v", i, got[i])
		require.False(t, got[i].at.IsZero(), "call %d carries no stamp", i)
		if want[i].what == "end" {
			require.NotEmpty(t, got[i].rung, "a termination must name the rung the exchange was on")
		}
	}
}

// The seam is nil-safe by construction: an unwired service reports nil, and
// wiring is a plain injection under the TX lock.
func TestSessionObserver_UnwiredIsNilAndInjectionSticks(t *testing.T) {
	s := &Service{}
	if s.sessionObserverSnapshot() != nil {
		t.Fatal("a fresh service must have no session observer")
	}
	obs := &recordingSessionObserver{}
	s.SetSessionObserver(obs)
	if got := s.sessionObserverSnapshot(); got != SessionObserver(obs) {
		t.Fatalf("observer snapshot = %v, want the injected one", got)
	}
}

// ---- the sequencer's teardown boundary (AC4) ----

func observedSeq(t *testing.T) (*Sequencer, *seqRecorder, *recordingSessionObserver) {
	t.Helper()
	r := &seqRecorder{}
	s := newTestSeq(r)
	obs := &recordingSessionObserver{}
	s.onTerminated = obs.ExchangeTerminated
	return s, r, obs
}

// A rung that cannot key (tx_not_armed) or cannot encode (tx_bad_message)
// mid-exchange records ONE termination naming the partner and the rung.
func TestSessionObserver_RungDrivenRetirementsAreTerminations(t *testing.T) {
	for _, tc := range []struct {
		err   error
		cause string
	}{
		{ErrTxNotArmed, EndReasonTxNotArmed},
		{ErrTxBadMessage, EndReasonTxBadMessage},
	} {
		t.Run(tc.cause, func(t *testing.T) {
			s, r, obs := observedSeq(t)
			startedSession(t, s)
			r.transmitErr = tc.err
			driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})
			require.False(t, s.Active())
			requireOnly(t, obs, sessionCall{what: "end", cause: tc.cause, partner: "K1ABC"})
			require.Equal(t, "calling", obs.snapshot()[0].rung)
		})
	}
}

// An operator Abandon records nothing; a dial refusal through AbandonIfCurrent
// records one termination with the refusal's reason.
func TestSessionObserver_OperatorAbandonIsSilentButADialRefusalIsATermination(t *testing.T) {
	s, _, obs := observedSeq(t)
	startedSession(t, s)
	s.Abandon()
	requireOnly(t, obs)

	s, _, obs = observedSeq(t)
	startedSession(t, s)
	s.mu.Lock()
	gen := s.sessionGen
	s.mu.Unlock()
	require.True(t, s.AbandonIfCurrent(gen, EndReasonDialUnknown))
	requireOnly(t, obs, sessionCall{what: "end", cause: EndReasonDialUnknown, partner: "K1ABC"})
}

// A Call-CQ run with no station being worked has no partner exchange to
// terminate: the sequencer's teardown records nothing, whatever the cause.
func TestSessionObserver_ARunWithoutAPartnerIsNotAnExchange(t *testing.T) {
	s, _, obs := observedSeq(t)
	require.NoError(t, s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "operator_pick", "", time.Unix(0, 0).UTC(), ""))
	require.True(t, s.Active())
	s.mu.Lock()
	gen := s.sessionGen
	s.mu.Unlock()
	require.True(t, s.AbandonIfCurrent(gen, EndReasonDialMoved))
	requireOnly(t, obs)
}

// ---- the service's disarm boundary (AC3, AC4; ruling 2) ----

func observedService(t *testing.T) (*Service, *recordingSessionObserver) {
	t.Helper()
	dial := 14.074
	s, _ := dialGuardService(t, &dial) // armed, dial source wired
	obs := &recordingSessionObserver{}
	s.SetSessionObserver(obs)
	return s, obs
}

func partnerExchange(t *testing.T, s *Service) {
	t.Helper()
	startedSession(t, s.seq)
}

// Closing the browser mid-exchange (linger expiry, cause unattended) records
// exactly one termination — the partner, the rung — and NO disarm row.
func TestSessionObserver_UnattendedMidExchangeIsOneTerminationAndNoDisarmRow(t *testing.T) {
	s, obs := observedService(t)
	partnerExchange(t, s)
	s.disarmTx(disarmUnattended)
	requireOnly(t, obs, sessionCall{what: "end", cause: "unattended", partner: "K1ABC"})
}

// Closing the browser on an idle armed session — including an armed Call-CQ
// run waiting between contacts — is routine lifecycle: nothing is recorded.
func TestSessionObserver_UnattendedWhileIdleOrBetweenContactsRecordsNothing(t *testing.T) {
	s, obs := observedService(t)
	s.disarmTx(disarmUnattended)
	requireOnly(t, obs)

	s, obs = observedService(t)
	startGuardCq(t, s) // a run, no partner
	s.disarmTx(disarmUnattended)
	requireOnly(t, obs)
}

// A CAT loss while idle records tx.disarmed; mid-exchange it records the
// termination instead, never both.
func TestSessionObserver_CatLostIsADisarmWhenIdleAndATerminationMidExchange(t *testing.T) {
	s, obs := observedService(t)
	s.disarmTx(disarmCatLost)
	requireOnly(t, obs, sessionCall{what: "disarm", cause: "cat_lost"})

	s, obs = observedService(t)
	partnerExchange(t, s)
	s.disarmTx(disarmCatLost)
	requireOnly(t, obs, sessionCall{what: "end", cause: "cat_lost", partner: "K1ABC"})
}

// The dial guard: a move during a Call-CQ run (no partner) is a disarm row; a
// move mid-exchange is one termination with cause dial_moved and no disarm row.
func TestSessionObserver_DialMovedIsADisarmOnARunAndATerminationMidExchange(t *testing.T) {
	s, obs := observedService(t)
	startGuardCq(t, s)
	s.onDialMoved(14.074, 14.075)
	requireOnly(t, obs, sessionCall{what: "disarm", cause: "dial_moved"})

	s, obs = observedService(t)
	partnerExchange(t, s)
	s.onDialMoved(14.074, 14.075)
	requireOnly(t, obs, sessionCall{what: "end", cause: "dial_moved", partner: "K1ABC"})
}

// Operator causes never record, armed or not; an idle re-disarm records nothing.
func TestSessionObserver_OperatorCausesAndIdleRedisarmRecordNothing(t *testing.T) {
	for _, cause := range []string{disarmOperator, disarmBandChange, disarmShutdown} {
		s, obs := observedService(t)
		partnerExchange(t, s)
		s.disarmTx(cause)
		requireOnly(t, obs)
	}
	s, obs := observedService(t)
	s.disarmTx(disarmOperator) // now idle
	s.disarmTx(disarmCatLost)  // an idle re-disarm: nothing was armed to lose
	requireOnly(t, obs)
}
