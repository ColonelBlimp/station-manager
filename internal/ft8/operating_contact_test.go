package ft8

import (
	"context"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// A contact at a replacement (W-0027 C4, acceptance criterion 6: "A contact the old app
// was logging when it was replaced is logged, once").
//
// A replacement stops the scope by the path every other stop takes (RetireOperating), and
// invariant 1 says a rogered contact is logged exactly once whatever stops TX. That
// invariant had tests for a dial move and for a refused rung, and none for a replacement
// (the dossier, C4a, limit 4). These are that test, through the real pieces: the service
// under the operating-client rule, its own sequencer and completion policy, the real
// single-flight transmission and its cancellation, and the injected log hand-over. Only
// the waveform and the wait for a slot are left out.
//
// The moment that cannot be made by hand at a station: the partner has rogered, so the
// contact is earned; our closing 73 has not finished, so it is not yet logged; and a new
// app starts.

// closingRung is our courtesy 73 to K1ABC: the Group A final rung.
const closingRung = "K1ABC G0XYZ 73"

// replacedContact is lifetime A's answer to K1ABC's CQ, and what was logged and sent.
type replacedContact struct {
	s      *Service
	keyer  *fakeKeyer
	player *fakeTxPlayer
	base   time.Time // the start of the slot of K1ABC's that A answered

	mu      sync.Mutex
	logged  []CompletedQso
	sent    []string // every rung the sequencer asked for, in order
	holdLog func()   // runs inside the log hand-over, before the contact is recorded
}

func newReplacedContact(t *testing.T) *replacedContact {
	t.Helper()
	zeroTiming(t)
	f := &replacedContact{keyer: &fakeKeyer{}, player: newFakeTxPlayer()}
	s := newTxTestService(f.keyer, f.player, nil)
	f.s = s
	s.RequireOperatingLifetime()
	s.PublishOperating(lifeA)
	s.presence.open(lifeA) // presence is C4b's rule, with its own tests: A's stream is open
	t.Cleanup(func() { _ = s.ArmTx(false) })

	s.SetQsoLogger(func(_ context.Context, c CompletedQso) {
		f.mu.Lock()
		hold := f.holdLog
		f.mu.Unlock()
		if hold != nil {
			hold()
		}
		f.mu.Lock()
		f.logged = append(f.logged, c)
		f.mu.Unlock()
	})

	// The sequencer's rungs. The closing 73 goes through the service's real transmission:
	// armed check, single flight, the controller's key and play, cancellation, and the
	// sequencer's own completion callback. The earlier rungs are taken as sent, as the
	// sequencer tests' recorder takes them.
	s.seq.transmit = func(msg string, offsetHz, dialMHz float64, gen uint64, onDone func(bool)) error {
		f.mu.Lock()
		f.sent = append(f.sent, msg)
		f.mu.Unlock()
		if msg != closingRung {
			if onDone != nil {
				onDone(true)
			}
			return nil
		}
		return s.startTransmission(msg, offsetHz, dialMHz,
			func() bool { return s.seq.isCurrent(gen) },
			func(ctx context.Context, ctrl *TxController) error {
				return ctrl.transmit(ctx, []int16{1, 2, 3}, time.Time{}, nil)
			}, onDone, nil)
	}
	return f
}

func (f *replacedContact) loggedNow() []CompletedQso {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CompletedQso(nil), f.logged...)
}

func (f *replacedContact) sentNow() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

// their is a decode of K1ABC's slot, afterSec seconds after the one A answered.
func (f *replacedContact) their(afterSec int64, text string, snr int) {
	driveTheir(f.s.seq, f.base.Unix()+afterSec, []goft8.DecodedMessage{dm(text, snr)})
}

// toTheirReport arms as lifetime A, answers K1ABC through the service's own start, and
// works the exchange until we have sent our R-report. K1ABC has not rogered yet.
func (f *replacedContact) toTheirReport(t *testing.T) {
	t.Helper()
	a := f.s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))
	f.base = ProfileFT8.slotStart(time.Now().UTC())
	require.NoError(t, a.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		f.base.Format(time.RFC3339), 1500, 14.074, 1, false, ""))
	f.their(30, "CQ K1ABC FN42", -1)
	f.their(60, "G0XYZ K1ABC -10", -12)
	require.True(t, f.s.seq.Active(), "the fixture: the exchange is under way")
	require.Empty(t, f.loggedNow(), "the fixture: nothing is logged before they roger")
}

// theyRoger is K1ABC's RR73, which earns the contact and puts our closing 73 on the air.
func (f *replacedContact) theyRoger(t *testing.T) {
	t.Helper()
	f.their(90, "G0XYZ K1ABC RR73", -11)
	require.Eventually(t, func() bool { return f.keyer.keys() > 0 }, 2*time.Second, 5*time.Millisecond,
		"the fixture: the closing 73 must be keyed")
	require.Contains(t, f.sentNow(), closingRung)
}

// newAppArms is the third step of the replacement and the new app's first act.
func (f *replacedContact) newAppArms(t *testing.T) {
	t.Helper()
	f.s.PublishOperating(lifeB)
	f.s.presence.open(lifeB)
	require.NoError(t, f.s.OperatingAs(lifeB).ArmTx(true), "the new app could not arm after the replacement")
}

// requireNothingMoreUnderTheNewApp: K1ABC, who never heard our 73, rogers again under the
// new app's arm. The old app's contact is not logged a second time, and the new app's arm
// sends nothing for it.
func (f *replacedContact) requireNothingMoreUnderTheNewApp(t *testing.T, wantLogged int) {
	t.Helper()
	sent := len(f.sentNow())
	f.their(120, "G0XYZ K1ABC RR73", -11)
	f.their(150, "G0XYZ K1ABC RR73", -11)
	require.Len(t, f.loggedNow(), wantLogged, "the old app's contact was logged again under the new app")
	require.Len(t, f.sentNow(), sent, "the new app's arm sent a rung for the old app's contact")
	require.False(t, f.s.seq.Active(), "the old app's exchange is running under the new app")
	require.False(t, f.s.txInFlightNow())
}

// The closing 73 is keyed and playing when the new app starts. The replacement cuts it
// short and the contact is logged, once: K1ABC had already rogered.
func TestOperating_AContactEarnedBeforeAReplacementIsLoggedOnce(t *testing.T) {
	f := newReplacedContact(t)
	f.toTheirReport(t)
	f.theyRoger(t)
	require.True(t, f.s.txInFlightNow(), "the fixture: the closing 73 is in flight")
	require.Empty(t, f.loggedNow(), "the fixture: the 73 has not finished, so nothing is logged yet")
	f.keyer.setNotReady(true) // a keyed rig reports not ready; the plain fake never does

	f.s.RetireOperating()

	require.False(t, f.s.txInFlightNow(), "the replacement returned with the 73 still in flight")
	require.Positive(t, f.keyer.unkeys(), "the replacement returned with the rig keyed")
	require.False(t, f.s.armedNow())
	require.False(t, f.s.seq.Active(), "the old app's exchange outlived the replacement")
	logged := f.loggedNow()
	require.Len(t, logged, 1, "K1ABC had rogered: the contact is logged, and once")
	require.Equal(t, "K1ABC", logged[0].TheirCall)
	require.InDelta(t, 14.074, logged[0].DialFreqMHz, 1e-9, "on the frequency of the exchange")
	require.Equal(t, int64(1), logged[0].LogbookID, "in the logbook the old app answered in")

	f.keyer.setNotReady(false) // unkeyed
	f.newAppArms(t)
	f.requireNothingMoreUnderTheNewApp(t, 1)
}

// The closing 73 has played out and the contact is being handed to the log at the moment
// the new app starts. The replacement waits for the hand-over and does not repeat it.
func TestOperating_AContactBeingLoggedAtAReplacementIsLoggedOnce(t *testing.T) {
	f := newReplacedContact(t)

	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	letThrough := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(letThrough) // a failed assertion must not leave the hand-over, and the stop, waiting
	f.mu.Lock()
	f.holdLog = func() {
		enterOnce.Do(func() { close(entered) })
		<-release
	}
	f.mu.Unlock()

	f.toTheirReport(t)
	f.theyRoger(t)
	close(f.player.done) // the 73 plays to its end: it completed on the air
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fixture: the completed contact never reached the log hand-over")
	}
	require.Empty(t, f.loggedNow(), "the fixture: the hand-over is in progress, not finished")

	replaced := make(chan struct{})
	go func() {
		defer close(replaced)
		f.s.RetireOperating()
	}()
	require.Eventually(t, func() bool { return !f.s.armedNow() }, 2*time.Second, 5*time.Millisecond,
		"the fixture: the replacement must have begun its stop")
	// Nothing observable says "it is waiting", so this is a bounded look, not a barrier: a
	// replacement that does not wait returns within microseconds of clearing the arm.
	select {
	case <-replaced:
		t.Fatal("the replacement returned while the contact was still being handed to the log")
	case <-time.After(250 * time.Millisecond):
	}

	letThrough()
	select {
	case <-replaced:
	case <-time.After(2 * time.Second):
		t.Fatal("the replacement never returned after the hand-over finished")
	}
	logged := f.loggedNow()
	require.Len(t, logged, 1, "the contact being logged at the replacement is logged, and once")
	require.Equal(t, "K1ABC", logged[0].TheirCall)
	require.False(t, f.s.seq.Active())

	f.newAppArms(t)
	f.requireNothingMoreUnderTheNewApp(t, 1)
}

// The other side of the line, so that the tests above cannot pass by logging whatever is
// running: K1ABC has NOT rogered when the new app starts. Nothing is logged, then or when
// the roger arrives under the new app. The replacement ends an unfinished exchange as any
// other stop does (the brief: "A contact already earned still finishes logging").
func TestOperating_AContactNotEarnedBeforeAReplacementIsNotLogged(t *testing.T) {
	f := newReplacedContact(t)
	f.toTheirReport(t)

	f.s.RetireOperating()

	require.False(t, f.s.seq.Active())
	require.False(t, f.s.armedNow())
	require.Empty(t, f.loggedNow(), "a contact K1ABC had not rogered was logged by the replacement")

	f.newAppArms(t)
	f.requireNothingMoreUnderTheNewApp(t, 0)
}
