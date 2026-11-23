package ft8

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The read of FT8's state (W-0027 A3, section I): what the desktop app asks when
// a close is asked for, so that its question says what the daemon knows and not
// what a page last heard. It is one observation: it stops nothing and promises
// nothing about the moment after.

// recentSlot is the start of the slot just heard: a start refuses a decode too
// old to work.
func recentSlot() string {
	return time.Now().UTC().Truncate(15 * time.Second).Format(time.RFC3339)
}

// Nothing armed and nothing running: nothing would stop.
func TestOperatingState_IdleIsNothingToStop(t *testing.T) {
	s := newOwnedService(t, lifeA)

	st := s.OperatingState()
	require.Equal(t, OperatingState{}, st)
	require.False(t, st.WouldStop())
}

// Armed with nothing running is something a stop would stop (as it is busy for
// an archive switch: operator ruling 2026-09-22).
func TestOperatingState_ArmedWithNothingRunning(t *testing.T) {
	s := newOwnedService(t, lifeA)
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))

	st := s.OperatingState()
	require.Equal(t, OperatingState{TxArmed: true}, st)
	require.True(t, st.WouldStop())
}

// A manual send waiting for its slot is in flight.
func TestOperatingState_AManualSendWaitingForItsSlot(t *testing.T) {
	s := newOwnedService(t, lifeA)
	a := s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))
	require.NoError(t, a.TransmitNext("CQ G0XYZ IO91", 1500))

	st := s.OperatingState()
	require.True(t, st.TxInFlight, "the send waiting for its slot was not reported")
	require.True(t, st.WouldStop())
}

// A Call-CQ run and an exchange are each a run under way.
func TestOperatingState_ARunUnderWay(t *testing.T) {
	starts := map[string]func(o Operating) error{
		"call cq": func(o Operating) error {
			return o.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, "")
		},
		"an exchange": func(o Operating) error {
			return o.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", recentSlot(), 1500, 14.074, 1, false, "")
		},
	}
	for name, start := range starts {
		t.Run(name, func(t *testing.T) {
			s := newOwnedService(t, lifeA)
			a := s.OperatingAs(lifeA)
			require.NoError(t, a.ArmTx(true))
			require.NoError(t, start(a))

			st := s.OperatingState()
			require.True(t, st.RunActive, "the run was not reported")
			require.True(t, st.TxArmed)
			require.True(t, st.WouldStop())
		})
	}
}

// A run waiting between contacts has no exchange under way, and a stop still
// ends it (ADR 0059, ADR 0067): it is something that would stop.
func TestOperatingState_ARunWaitingBetweenContacts(t *testing.T) {
	r := &seqRecorder{}
	seq := newTestSeq(r)
	listedCaller(t, seq, r) // a pick run whose seeding contact is complete
	require.False(t, seq.Active(), "fixture: no exchange is under way")

	active, waiting := seq.operatingState()
	require.False(t, active)
	require.True(t, waiting, "the run waiting between contacts was not reported")

	// And through the service's read: on its own it is something to stop.
	s := newOwnedService(t, lifeA)
	s.seq = seq
	st := s.OperatingState()
	require.Equal(t, OperatingState{AutoWorkArmed: true}, st)
	require.True(t, st.WouldStop(), "a run waiting between contacts must count as something to stop")
}

// The read is passive. Asked any number of times of a daemon that is armed and
// running, it leaves the arm, the run, the subscribers, what the stream would
// replay and the presence hold exactly as they were.
func TestOperatingState_ChangesNothing(t *testing.T) {
	s := newOwnedService(t, lifeA)
	a := s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))
	require.NoError(t, a.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, ""))

	type seen struct {
		armGen     uint64
		armed      bool
		active     bool
		subs       int
		lastTx     *hubEvent
		lastQso    *hubEvent
		lastDecode *hubEvent
		held       presenceHold
		streams    int
		operating  string
	}
	look := func() seen {
		var v seen
		v.armGen, v.armed = s.armState()
		v.active = s.seq.Active()
		s.hub.mu.Lock()
		v.subs, v.lastTx, v.lastQso, v.lastDecode = len(s.hub.subs), s.hub.lastTx, s.hub.lastQso, s.hub.lastDecode
		s.hub.mu.Unlock()
		s.presence.mu.Lock()
		v.held, v.streams = s.presence.held, len(s.presence.streams)
		s.presence.mu.Unlock()
		s.seqGate.Lock()
		v.operating = s.operatingLifetime
		s.seqGate.Unlock()
		return v
	}

	before := look()
	for range 3 {
		require.True(t, s.OperatingState().WouldStop())
	}
	require.Equal(t, before, look(), "the read changed something")
}

// The states are read together, under the order the package already uses
// (seqGate, then txMu, then the sequencer's lock: SealTxAdmission), so the answer
// is never one half from before a start or a disarm and the other from after.
// Shown by holding txMu: the read must then be found holding seqGate.
func TestOperatingState_IsReadUnderTheEstablishedOrder(t *testing.T) {
	s := newOwnedService(t, lifeA)

	s.txMu.Lock()
	// Released on every way out: the fixture's own cleanup disarms, and takes txMu.
	release := sync.OnceFunc(s.txMu.Unlock)
	t.Cleanup(release)
	done := make(chan OperatingState, 1)
	go func() { done <- s.OperatingState() }()

	require.Eventually(t, func() bool {
		if s.seqGate.TryLock() {
			s.seqGate.Unlock()
			return false
		}
		return true
	}, 5*time.Second, time.Millisecond, "the read did not take seqGate before txMu")
	select {
	case st := <-done:
		t.Fatalf("the read answered %+v while txMu was held", st)
	default:
	}
	release()
	require.Equal(t, OperatingState{}, <-done)
}

// An arm that lands after an idle answer is not seen by that answer: the read is
// one observation, not a hold (section I, 5). What the app then does is its own
// rule (the disarm is sent whatever the read said).
func TestOperatingState_IsOneObservation(t *testing.T) {
	s := newOwnedService(t, lifeA)

	idle := s.OperatingState()
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))

	require.False(t, idle.WouldStop(), "the earlier answer was idle")
	require.True(t, s.OperatingState().WouldStop(), "and the arm that followed it stands")
}
