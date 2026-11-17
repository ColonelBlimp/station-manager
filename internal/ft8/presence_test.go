package ft8

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The operator's presence in a scope (W-0027 C4b). The helper knows nothing of FT8: it
// counts the open operating subscriptions of each app lifetime, holds an arm for the
// lifetime that made it, and says when that lifetime has had no subscription for the
// grace. These tests drive it with a clock the test owns.

// graceClock stands in for time.AfterFunc: timers are recorded, never run by themselves,
// and the test fires or ignores each one. A stopped timer can still be fired, as a real
// one that had already begun to run would be.
type graceClock struct {
	timers []*graceTimer
}

type graceTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

func (t *graceTimer) Stop() bool {
	was := !t.stopped
	t.stopped = true
	return was
}

func (c *graceClock) after(d time.Duration, f func()) presenceTimer {
	t := &graceTimer{d: d, f: f}
	c.timers = append(c.timers, t)
	return t
}

// pending is the timers that have not been stopped.
func (c *graceClock) pending() []*graceTimer {
	var out []*graceTimer
	for _, t := range c.timers {
		if !t.stopped {
			out = append(out, t)
		}
	}
	return out
}

type lostCall struct {
	lifetime string
	arm      uint64
}

func newTestPresence() (*operatorPresence, *graceClock, *[]lostCall) {
	clock := &graceClock{}
	var lost []lostCall
	p := newOperatorPresence(
		func() time.Duration { return 5 * time.Second },
		clock.after,
		func(lifetime string, arm, _ uint64) { lost = append(lost, lostCall{lifetime, arm}) },
	)
	return p, clock, &lost
}

func TestPresence_CountsTheOpenStreamsOfEachLifetime(t *testing.T) {
	p, clock, lost := newTestPresence()
	require.False(t, p.has(lifeA))

	closeA1 := p.open(lifeA)
	closeA2 := p.open(lifeA)
	closeB := p.open(lifeB)
	require.True(t, p.has(lifeA))
	require.True(t, p.has(lifeB))

	closeA1()
	require.True(t, p.has(lifeA), "one of two streams closed and the lifetime is gone")
	closeA1() // closing twice counts once
	require.True(t, p.has(lifeA), "a stream closed twice was counted twice")
	closeA2()
	require.False(t, p.has(lifeA))
	require.True(t, p.has(lifeB), "one lifetime's close touched another's count")
	closeB()

	// A request that named no lifetime is never presence.
	p.open("")()
	require.False(t, p.has(""))
	require.Empty(t, clock.timers, "with no arm held there is nothing to time")
	require.Empty(t, *lost)
}

func TestPresence_TheGraceRunsWhenTheHoldersLastStreamCloses(t *testing.T) {
	p, clock, lost := newTestPresence()
	closeA := p.open(lifeA)
	p.hold(lifeA, 7)
	require.Empty(t, clock.timers, "a held arm with a stream open is not timed")

	closeA()
	require.Len(t, clock.pending(), 1)
	require.Equal(t, 5*time.Second, clock.pending()[0].d)
	require.Empty(t, *lost, "the loss was reported before the grace ran out")

	clock.pending()[0].f()
	require.Equal(t, []lostCall{{lifeA, 7}}, *lost)
}

func TestPresence_AReturnInsideTheGraceChangesNothing(t *testing.T) {
	p, clock, lost := newTestPresence()
	closeA := p.open(lifeA)
	p.hold(lifeA, 1)
	closeA()
	first := clock.timers[0]

	closeAgain := p.open(lifeA) // back inside the grace
	require.True(t, first.stopped, "the pending grace was not cancelled by the return")
	// A timer that had already begun to run when it was stopped reports nothing.
	first.f()
	require.Empty(t, *lost)

	// And the next loss runs a grace of its own, in full.
	closeAgain()
	require.Len(t, clock.pending(), 1)
	first.f() // the earlier timer, fired late: it is not this grace
	require.Empty(t, *lost, "a stale timer ended a later grace early")
	clock.pending()[0].f()
	require.Equal(t, []lostCall{{lifeA, 1}}, *lost)
}

func TestPresence_OnlyTheHoldersStreamsCount(t *testing.T) {
	p, clock, lost := newTestPresence()
	closeA := p.open(lifeA)
	p.open(lifeB) // another lifetime's stream stays open throughout
	p.open("")
	p.hold(lifeA, 3)

	closeA()
	require.Len(t, clock.pending(), 1, "another lifetime's stream stood in for the holder's")
	clock.pending()[0].f()
	require.Equal(t, []lostCall{{lifeA, 3}}, *lost)
}

func TestPresence_AnArmHeldWithNoStreamIsTimedAtOnce(t *testing.T) {
	p, clock, _ := newTestPresence()
	p.hold(lifeA, 1)
	require.Len(t, clock.pending(), 1)
}

func TestPresence_ReleasingTheArmEndsTheWatch(t *testing.T) {
	p, clock, lost := newTestPresence()
	closeA := p.open(lifeA)
	p.hold(lifeA, 1)
	closeA()
	timer := clock.timers[0]

	p.release() // the arm was disarmed for some other reason
	require.True(t, timer.stopped)
	timer.f() // fired late
	require.Empty(t, *lost, "a released arm was reported lost")

	// With nothing held, streams come and go untimed.
	p.open(lifeA)()
	require.Len(t, clock.timers, 1)
}

// codex-124, answer 3: a timer that fires late never ends a LATER arm. The decision is
// tied to the arm it was made for.
func TestPresence_ALateTimerNeverEndsALaterArm(t *testing.T) {
	p, clock, lost := newTestPresence()
	closeA := p.open(lifeA)
	p.hold(lifeA, 1)
	closeA()
	old := clock.timers[0]

	p.release()
	p.hold(lifeA, 2) // a new arm by the same lifetime, with no stream yet: its own grace
	require.Len(t, clock.pending(), 1)
	old.f()
	require.Empty(t, *lost, "the earlier arm's timer ended the later arm")

	clock.pending()[0].f()
	require.Equal(t, []lostCall{{lifeA, 2}}, *lost)

	// The same for an arm by another lifetime.
	p.release()
	p.hold(lifeB, 3)
	for _, tm := range clock.timers[:2] {
		tm.f()
	}
	require.Len(t, *lost, 1, "an earlier lifetime's timer ended another lifetime's arm")
}

func TestPresence_TheGraceIsReadWhenItStarts(t *testing.T) {
	clock := &graceClock{}
	grace := 5 * time.Second
	p := newOperatorPresence(func() time.Duration { return grace }, clock.after, func(string, uint64, uint64) {})
	p.hold(lifeA, 1)
	grace = 50 * time.Millisecond
	p.release()
	p.hold(lifeA, 2)
	require.Equal(t, 5*time.Second, clock.timers[0].d)
	require.Equal(t, 50*time.Millisecond, clock.timers[1].d)
}

// A decision names the grace it was made for, and the helper can say whether that loss
// still stands: the same hold, the same grace, and the holder still without a stream
// (codex-134).
func TestPresence_ALossStandsOnlyForTheGraceItWasDecidedIn(t *testing.T) {
	clock := &graceClock{}
	type decision struct {
		lifetime   string
		arm, grace uint64
	}
	var decisions []decision
	p := newOperatorPresence(func() time.Duration { return time.Second }, clock.after,
		func(lifetime string, arm, grace uint64) {
			decisions = append(decisions, decision{lifetime, arm, grace})
		})

	closeA := p.open(lifeA)
	p.hold(lifeA, 1)
	closeA()
	clock.pending()[0].f()
	require.Len(t, decisions, 1)
	d := decisions[0]
	require.True(t, p.lossStands(d.lifetime, d.arm, d.grace), "a loss just decided does not stand")
	require.False(t, p.lossStands(d.lifetime, d.arm+1, d.grace), "it stands for another arm")
	require.False(t, p.lossStands(lifeB, d.arm, d.grace), "it stands for another lifetime")

	back := p.open(lifeA)
	require.False(t, p.lossStands(d.lifetime, d.arm, d.grace), "it stands although the holder is back")
	back() // gone again: a new absence, with a grace of its own
	require.False(t, p.lossStands(d.lifetime, d.arm, d.grace),
		"an earlier absence's decision stands for a later absence")

	clock.timers[len(clock.timers)-1].f() // the new grace, at its own end
	require.Len(t, decisions, 2)
	require.True(t, p.lossStands(decisions[1].lifetime, decisions[1].arm, decisions[1].grace))

	p.release()
	require.False(t, p.lossStands(decisions[1].lifetime, decisions[1].arm, decisions[1].grace),
		"a loss stands for an arm that has been released")
}
