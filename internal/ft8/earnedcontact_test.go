package ft8

import (
	"sync/atomic"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// An earned contact at a stop (invariant 1; codex-203 and codex-204).
//
// A Group A contact is earned when the partner's roger is taken in: they hold both halves
// of the exchange. Our closing rung is a courtesy. A stop of TX after that moment logs
// the contact, whatever state the courtesy is in: playing (operating_contact_test.go),
// decided and not yet admitted (the same file), or, here, not yet decided because the
// slot that took the roger in could not carry a rung.

// groupALadder is one of the three ladders whose closing rung is a courtesy.
type groupALadder struct {
	name    string
	their   string
	start   func(t *testing.T, s *Sequencer)
	toRoger func(s *Sequencer) // their slots before the roger, each in time
	rogerAt int64
	roger   string
	closing string
}

var groupALadders = []groupALadder{
	{
		name: "answer a CQ", their: "K1ABC",
		start: func(t *testing.T, s *Sequencer) {
			require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
				time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
		},
		toRoger: func(s *Sequencer) {
			driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
			driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
		},
		rogerAt: 90, roger: "G0XYZ K1ABC RR73", closing: "K1ABC G0XYZ 73",
	},
	{
		name: "work a caller, Field Day", their: "K7IOC",
		start: func(t *testing.T, s *Sequencer) {
			require.NoError(t, s.StartWorkCallerFd("G0XYZ", "1D", "DX", "K7IOC", "", "2A", "WWA", 2,
				time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
		},
		toRoger: func(s *Sequencer) { driveTheir(s, 30, nil) },
		rogerAt: 60, roger: "G0XYZ K7IOC RR73", closing: "K7IOC G0XYZ RR73",
	},
	{
		name: "answer a CQ, type 4", their: "PJ4/NA2AA",
		start: func(t *testing.T, s *Sequencer) {
			require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12,
				time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
		},
		toRoger: func(s *Sequencer) { driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ PJ4/NA2AA", -1)}) },
		rogerAt: 60, roger: "<...> PJ4/NA2AA RR73", closing: "PJ4/NA2AA 7Q5MLV 73",
	},
}

// deferral is one way the slot that takes the roger in cannot carry our rung.
type deferral struct {
	name  string
	drive func(s *Sequencer, sec int64, msgs []goft8.DecodedMessage)
}

var deferrals = []deferral{
	{"the roger is decoded too late into our slot", driveTheirLate},
	{"our slot has already carried a rung", func(s *Sequencer, sec int64, msgs []goft8.DecodedMessage) {
		s.mu.Lock()
		s.lastTxSlot = time.Unix(sec, 0).UTC().Add(ProfileFT8.Slot)
		s.mu.Unlock()
		driveTheir(s, sec, msgs)
	}},
}

// stopsOfASession are the two calls that end a session from outside its own ladder: the
// Service's disarm, and the operator's Abandon. Both log an earned contact (for the
// Abandon, the operator's ruling of 2026-11-20; earnedabandon_test.go).
var stopsOfASession = []struct {
	name string
	stop func(s *Sequencer)
}{
	{"a stop of TX", func(s *Sequencer) { s.abandonNamed("", disarmUnattended) }},
	{"the operator's Abandon", func(s *Sequencer) { s.Abandon() }},
}

// rogerDeferred runs the ladder to the partner's roger, taken in by a slot that defers our
// closing rung, and requires what the fixture claims: the exchange is live, the closing
// rung has not been sent and nothing is logged.
func rogerDeferred(t *testing.T, l groupALadder, d deferral) (*Sequencer, *seqRecorder) {
	t.Helper()
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)
	l.start(t, s)
	l.toRoger(s)
	require.Empty(t, r.completed, "the fixture: nothing is logged before their roger")
	d.drive(s, l.rogerAt, []goft8.DecodedMessage{dm(l.roger, -11)})
	require.True(t, s.Active(), "the fixture: the exchange is still live")
	require.Zero(t, countMsg(r.sentMsgs(), l.closing), "the fixture: the closing rung was deferred, not sent")
	require.Empty(t, r.completed, "the fixture: a deferred closing rung has logged nothing yet")
	return s, r
}

// The partner has rogered and our closing rung was deferred. A stop before the next slot
// logs the contact, once, and sends nothing afterwards.
func TestEarned_AStopAfterADeferredClosingRungLogsTheContact(t *testing.T) {
	for _, l := range groupALadders {
		for _, d := range deferrals {
			for _, by := range stopsOfASession {
				t.Run(l.name+": "+d.name+": "+by.name, func(t *testing.T) {
					s, r := rogerDeferred(t, l, d)

					by.stop(s)

					require.Len(t, r.completed, 1, "they had rogered: the stop logs the contact")
					require.Equal(t, l.their, r.completed[0].TheirCall)
					require.InDelta(t, 14.074, r.completed[0].DialFreqMHz, 1e-9)
					require.False(t, s.Active(), "the stop ends the session")

					driveTheir(s, l.rogerAt+30, []goft8.DecodedMessage{dm(l.roger, -11)})
					require.Len(t, r.completed, 1, "and never logs it twice")
					require.Zero(t, countMsg(r.sentMsgs(), l.closing), "nothing is sent for it after the stop")
				})
			}
		}
	}
}

// The other side of that line, in the same three ladders: the stop comes before the
// partner's roger. Nothing is logged, by either stop.
func TestEarned_AStopBeforeTheRogerLogsNothing(t *testing.T) {
	for _, l := range groupALadders {
		for _, by := range stopsOfASession {
			t.Run(l.name+": "+by.name, func(t *testing.T) {
				r := &seqRecorder{asyncFail: true}
				s := newTestSeq(r)
				l.start(t, s)
				l.toRoger(s)
				require.True(t, s.Active(), "the fixture: the exchange is live")

				by.stop(s)

				require.Empty(t, r.completed, "they had not rogered: a stop must not log the contact")
				require.False(t, s.Active())
				driveTheir(s, l.rogerAt, []goft8.DecodedMessage{dm(l.roger, -11)})
				require.Empty(t, r.completed, "the roger arriving after the stop logged the contact")
			})
		}
	}
}

// The control for the test above: with no stop, the next slot in time sends the closing
// rung once and the contact is logged, once. The deferral changed neither.
func TestEarned_ADeferredClosingRungIsStillSentOnceAndLogged(t *testing.T) {
	for _, l := range groupALadders {
		for _, d := range deferrals {
			t.Run(l.name+": "+d.name, func(t *testing.T) {
				s, r := rogerDeferred(t, l, d)

				driveTheir(s, l.rogerAt+30, nil)

				require.Equal(t, 1, countMsg(r.sentMsgs(), l.closing), "the closing rung goes out in the next slot")
				require.Len(t, r.completed, 1)
				require.False(t, s.Active())
				driveTheir(s, l.rogerAt+60, nil)
				require.Equal(t, 1, countMsg(r.sentMsgs(), l.closing), "never retried")
				require.Len(t, r.completed, 1, "never logged twice")
			})
		}
	}
}

// The other side of the line. In the Group B ladders the partner is still waiting on our
// RR73: nothing is earned until it reaches the air. With that final rung deferred, a stop
// logs nothing.
func TestEarned_AStopWithAGroupBFinalRungDeferredLogsNothing(t *testing.T) {
	at0 := time.Unix(0, 0).UTC()
	cases := []struct {
		name   string
		start  func(t *testing.T, s *Sequencer)
		before func(s *Sequencer)
		at     int64
		theirs goft8.DecodedMessage
		final  string
	}{
		{
			name: "work a caller",
			start: func(t *testing.T, s *Sequencer) {
				require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, at0.Format(time.RFC3339), 1500, 14.074, at0))
			},
			before: func(s *Sequencer) { driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)}) },
			at:     60, theirs: dm("G0XYZ K1ABC R-08", -11), final: "K1ABC G0XYZ RR73",
		},
		{
			name:   "call CQ",
			start:  func(t *testing.T, s *Sequencer) { startCq(t, s) },
			before: func(s *Sequencer) { driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)}) },
			at:     90, theirs: dm("7Q5MLV DL9UW R-15", -10), final: "DL9UW 7Q5MLV RR73",
		},
		{
			name: "answer a CQ, Field Day",
			start: func(t *testing.T, s *Sequencer) {
				require.NoError(t, s.StartQsoFd("G0XYZ", "1D", "DX", "K1ABC", "FN42", -12, at0.Format(time.RFC3339), 1500, 14.074, at0))
			},
			before: func(s *Sequencer) { driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ FD K1ABC FN42", -1)}) },
			at:     60, theirs: dm("G0XYZ K1ABC R 2A EMA", -12), final: "K1ABC G0XYZ RR73",
		},
		{
			name: "work a caller, type 4",
			start: func(t *testing.T, s *Sequencer) {
				require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3, at0.Format(time.RFC3339), 1500, 14.074, at0))
			},
			before: func(*Sequencer) {},
			at:     30, theirs: dm("<...> PJ4/NA2AA", 3), final: "PJ4/NA2AA 7Q5MLV RR73",
		},
	}
	for _, tc := range cases {
		for _, by := range stopsOfASession {
			t.Run(tc.name+": "+by.name, func(t *testing.T) {
				r := &seqRecorder{}
				s := newTestSeq(r)
				tc.start(t, s)
				tc.before(s)
				driveTheirLate(s, tc.at, []goft8.DecodedMessage{tc.theirs})
				require.True(t, s.Active(), "the fixture: the exchange is live")
				require.Zero(t, countMsg(r.sentMsgs(), tc.final), "the fixture: the final rung was deferred")

				by.stop(s)

				require.Empty(t, r.completed, "they never got our roger: a stop must not log the contact")
				require.False(t, s.Active())
			})
		}
	}
}

// Through the service and its real stop: K1ABC's roger is taken in too late for our 73 to
// go out in that slot, and the stop comes before the next one.
func TestOperating_AContactEarnedIsLoggedWhenAStopFollowsADeferredClosingRung(t *testing.T) {
	for name, stop := range stopsOfTx {
		t.Run(name, func(t *testing.T) {
			f := newReplacedContact(t)
			f.toTheirReport(t)
			start := time.Unix(f.base.Unix()+90, 0).UTC()
			f.s.driveSequencer(ProfileFT8.SlotRefFromTime(start),
				[]goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)},
				start.Add(ProfileFT8.Slot+ProfileFT8.LateWindow+time.Second))
			require.True(t, f.s.seq.Active(), "the fixture: the exchange is live")
			require.NotContains(t, f.sentNow(), closingRung, "the fixture: the closing 73 was deferred")
			require.Empty(t, f.loggedNow(), "the fixture: nothing is logged yet")

			stop(t, f)
			f.s.AwaitCompletionHandover()

			logged := f.loggedNow()
			require.Len(t, logged, 1, "K1ABC had rogered: the contact is logged, and once")
			require.Equal(t, "K1ABC", logged[0].TheirCall)
			require.InDelta(t, 14.074, logged[0].DialFreqMHz, 1e-9, "on the frequency of the exchange")
			require.Equal(t, int64(1), logged[0].LogbookID, "in the logbook the old app answered in")
			require.Zero(t, f.keyer.keys(), "the closing 73 was keyed")
			require.False(t, f.s.seq.Active())

			f.newAppArms(t)
			f.requireNothingMoreUnderTheNewApp(t, 1)
		})
	}
}

// The completion can be reached by two callers at once: the stop, and the slot whose
// closing rung was refused. Only one of them logs (the generation guard), but each
// prepares the contact first, stamping the antenna path and the frequency that are live
// at that moment. The one that logs must log what IT stamped. Here the two are made to
// overlap in a fixed order: the first stamps and waits; the live antenna path changes; the
// second stamps and waits for the contact to be logged; the first goes on, and logs. With
// one shared contact between them the first logs the second's stamp (and the race
// detector reports the two touching it; codex-204).
func TestOperating_TheCallerThatLogsAnEarnedContactLogsItsOwnStamp(t *testing.T) {
	overlapped := func(t *testing.T, f *replacedContact) (firstStamped <-chan struct{}) {
		t.Helper()
		real := f.s.seq.prepareComplete
		var calls atomic.Int32
		first, second := make(chan struct{}), make(chan struct{})
		f.s.seq.prepareComplete = func(c *CompletedQso) {
			switch calls.Add(1) {
			case 1:
				real(c)
				close(first)
				select {
				case <-second:
				case <-time.After(5 * time.Second):
				}
			case 2:
				<-first
				f.s.txMu.Lock()
				f.s.exchPath = antPathLong // the live selection moves on after the first stamp
				f.s.exchPathGen++
				f.s.txMu.Unlock()
				real(c)
				close(second)
				deadline := time.Now().Add(5 * time.Second)
				for len(f.loggedNow()) == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
			default:
				real(c)
			}
		}
		return first
	}
	ended := func(t *testing.T, f *replacedContact, calls ...<-chan struct{}) {
		t.Helper()
		for _, ch := range calls {
			select {
			case <-ch:
			case <-time.After(10 * time.Second):
				t.Fatal("a caller of the completion never returned")
			}
		}
		logged := f.loggedNow()
		require.Len(t, logged, 1, "the contact is logged, and once")
		require.Equal(t, antPathShort, logged[0].AntPath,
			"the contact was logged with the path stamped by the caller that did not log it")
		require.False(t, f.s.seq.Active())
	}

	t.Run("the stop prepares first, the refused slot second", func(t *testing.T) {
		f := newReplacedContact(t)
		f.toTheirReport(t)
		firstStamped := overlapped(t, f)
		reached, letGo := f.heldBeforeTransmitting(t, func(msg string) bool { return msg == closingRung })
		slot := f.theirSlot(90, "G0XYZ K1ABC RR73", -11)
		select {
		case <-reached:
		case <-time.After(2 * time.Second):
			t.Fatal("the fixture: the closing 73 was never decided")
		}
		stopped := make(chan struct{})
		go func() { defer close(stopped); f.s.RetireOperating() }()
		select {
		case <-firstStamped:
		case <-time.After(5 * time.Second):
			t.Fatal("the fixture: the stop never reached the completion")
		}
		letGo() // the slot is refused for want of an arm and calls the completion too
		ended(t, f, stopped, slot)
	})
	t.Run("the refused slot prepares first, the stop second", func(t *testing.T) {
		f := newReplacedContact(t)
		f.toTheirReport(t)
		firstStamped := overlapped(t, f)
		f.keyer.setNotReady(true) // the closing 73 is refused before any transmission starts
		slot := f.theirSlot(90, "G0XYZ K1ABC RR73", -11)
		select {
		case <-firstStamped:
		case <-time.After(5 * time.Second):
			t.Fatal("the fixture: the refused slot never reached the completion")
		}
		stopped := make(chan struct{})
		go func() { defer close(stopped); f.s.RetireOperating() }()
		ended(t, f, stopped, slot)
	})
}
