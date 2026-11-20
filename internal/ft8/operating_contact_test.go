package ft8

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/testbarrier"
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
	s := newBareTxTestService(f.keyer, f.player, nil)
	f.s = s
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
	// Keyed AND playing: the controller keys first and plays after, so a wait for the key
	// alone lets a replacement land before the play has begun (commit review of b4c6f4b7).
	require.Eventually(t, func() bool { return f.keyer.keys() > 0 && f.player.plays() > 0 },
		2*time.Second, 5*time.Millisecond, "the fixture: the closing 73 must be keyed and playing")
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
	waitsForTheTransmission := testbarrier.Expect(t, testbarrier.AtWaitGroup, "(*Service).disarmTxLocked")
	go func() {
		defer close(replaced)
		f.s.RetireOperating()
	}()
	require.Eventually(t, func() bool { return !f.s.armedNow() }, 2*time.Second, 5*time.Millisecond,
		"the fixture: the replacement must have begun its stop")
	// The replacement is seen parked waiting for the transmission's goroutine, which is
	// in the hand-over, and has not returned.
	testbarrier.Waiting(waitsForTheTransmission,
		"the replacement, with the contact still being handed to the log", replaced)

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

// A confirmed exit of the app waits for completion logging already earned before it
// closes the session (ADR 0100 §3; W-0029 part 2, E). A contact reaches its logging job
// from one of two places. From the transmission's own goroutine, when the closing 73
// played or was cut short: RetireOperating waits for that (the tests above). Or from the
// decode loop itself, when the closing 73 was refused before any transmission started
// (the rig not ready, the dial moved): the contact is earned and is handed over there,
// after the sequencer has let go of its lock, where no stop waits for it. A stop landing
// in that gap returns with the hand-over still to come, and a wait for the logging jobs
// taken then finds none. AwaitCompletionHandover is the wait for that gap.
func TestOperating_TheExitWaitsForAContactHandedOverFromTheDecodeLoop(t *testing.T) {
	f := newReplacedContact(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	letThrough := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(letThrough)
	f.mu.Lock()
	f.holdLog = func() {
		enterOnce.Do(func() { close(entered) })
		<-release
	}
	f.mu.Unlock()

	f.toTheirReport(t)
	// The closing 73 is refused before a transmission starts: K1ABC's roger earns the
	// contact, and the decode loop hands it over.
	real := f.s.seq.transmit
	f.s.seq.transmit = func(msg string, offsetHz, dialMHz float64, gen uint64, onDone func(bool)) error {
		if msg == closingRung {
			f.mu.Lock()
			f.sent = append(f.sent, msg)
			f.mu.Unlock()
			return ErrTxNotReady
		}
		return real(msg, offsetHz, dialMHz, gen, onDone)
	}
	slot := make(chan struct{})
	go func() { // the decode loop's slot
		defer close(slot)
		f.s.driveSequencer(ProfileFT8.SlotRefFromTime(time.Unix(f.base.Unix()+90, 0).UTC()),
			[]goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)},
			time.Unix(f.base.Unix()+90, 0).UTC().Add(ProfileFT8.Slot+time.Second))
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fixture: the refused closing 73 never handed the contact over")
	}
	require.False(t, f.s.txInFlightNow(), "the fixture: no transmission carries this hand-over")
	require.Empty(t, f.loggedNow(), "the fixture: the hand-over is in progress, not finished")

	// The exit's stop returns: nothing it waits for is running.
	f.s.RetireOperating()

	type drained struct{ logged int }
	done := make(chan drained, 1)
	waitsForTheSlot := testbarrier.Expect(t, testbarrier.AtMutex, "(*Service).AwaitCompletionHandover")
	go func() {
		f.s.AwaitCompletionHandover()
		done <- drained{logged: len(f.loggedNow())}
	}()
	// The wait is seen parked at the slot's mark, having returned nothing.
	testbarrier.Waiting(waitsForTheSlot, "the wait, with the contact still being handed over", done)
	letThrough()
	select {
	case d := <-done:
		require.Equal(t, 1, d.logged, "when the wait returned the contact had not been handed over")
	case <-time.After(2 * time.Second):
		t.Fatal("the wait never returned after the hand-over finished")
	}
	<-slot
	require.False(t, f.s.seq.Active())

	// With no slot in progress the wait returns at once.
	f.s.AwaitCompletionHandover()
}

// theirSlot drives one of K1ABC's slots as the decode loop does, on a goroutine of its own.
func (f *replacedContact) theirSlot(afterSec int64, text string, snr int) <-chan struct{} {
	done := make(chan struct{})
	start := time.Unix(f.base.Unix()+afterSec, 0).UTC()
	go func() {
		defer close(done)
		f.s.driveSequencer(ProfileFT8.SlotRefFromTime(start),
			[]goft8.DecodedMessage{dm(text, snr)}, start.Add(ProfileFT8.Slot+time.Second))
	}()
	return done
}

// heldBeforeTransmitting stops the first matching rung at the sequencer's call to
// transmit: the rung is decided, the sequencer has let go of its lock, and the service has
// not been asked yet. No transmission exists there for a stop to wait for.
func (f *replacedContact) heldBeforeTransmitting(t *testing.T, match func(msg string) bool) (reached <-chan struct{}, letGo func()) {
	t.Helper()
	real := f.s.seq.transmit
	at, gate := make(chan struct{}), make(chan struct{})
	var first, release sync.Once
	f.s.seq.transmit = func(msg string, offsetHz, dialMHz float64, gen uint64, onDone func(bool)) error {
		if match(msg) {
			hold := false
			first.Do(func() { hold = true })
			if hold {
				close(at)
				<-gate
			}
		}
		return real(msg, offsetHz, dialMHz, gen, onDone)
	}
	letGo = func() { release.Do(func() { close(gate) }) }
	t.Cleanup(letGo)
	return at, letGo
}

// stopsOfTx are two of the stops that go through the service's disarm.
var stopsOfTx = map[string]func(t *testing.T, f *replacedContact){
	"the app is replaced or exits": func(_ *testing.T, f *replacedContact) { f.s.RetireOperating() },
	"the operator disarms": func(t *testing.T, f *replacedContact) {
		require.NoError(t, f.s.OperatingAs(lifeA).ArmTx(false))
	},
}

// The earlier boundary (codex-203). K1ABC's roger has been taken in and our closing 73
// decided, but the transmission has not been admitted: the slot is between the
// sequencer's lock and the service's. A stop landing there finds no transmission to wait
// for. The contact is earned all the same (invariant 1), so the stop must not retire it
// unlogged; and the 73 must not go out after the stop.
func TestOperating_AContactEarnedIsLoggedWhenAStopLandsBeforeItsClosingRungIsAdmitted(t *testing.T) {
	for name, stop := range stopsOfTx {
		t.Run(name, func(t *testing.T) {
			f := newReplacedContact(t)
			f.toTheirReport(t)
			reached, letGo := f.heldBeforeTransmitting(t, func(msg string) bool { return msg == closingRung })
			slot := f.theirSlot(90, "G0XYZ K1ABC RR73", -11)
			select {
			case <-reached:
			case <-time.After(2 * time.Second):
				t.Fatal("the fixture: the closing 73 was never decided")
			}
			require.False(t, f.s.txInFlightNow(), "the fixture: no transmission has been admitted")
			require.Empty(t, f.loggedNow(), "the fixture: nothing is logged yet")

			stop(t, f)
			require.False(t, f.s.armedNow())
			require.False(t, f.s.seq.Active(), "the exchange outlived the stop")

			// The slot goes on to ask for its transmission, and the exit's wait sees it out.
			letGo()
			f.s.AwaitCompletionHandover()
			select {
			case <-slot:
			case <-time.After(2 * time.Second):
				t.Fatal("the slot never ended")
			}
			logged := f.loggedNow()
			require.Len(t, logged, 1, "K1ABC had rogered: the contact is logged, and once")
			require.Equal(t, "K1ABC", logged[0].TheirCall)
			require.InDelta(t, 14.074, logged[0].DialFreqMHz, 1e-9, "on the frequency of the exchange")
			require.Equal(t, int64(1), logged[0].LogbookID, "in the logbook the old app answered in")
			require.Zero(t, f.keyer.keys(), "the closing 73 was keyed after the stop")
			require.False(t, f.s.txInFlightNow())

			f.newAppArms(t)
			f.requireNothingMoreUnderTheNewApp(t, 1)
		})
	}
}

// The other side of that line: the rung held at the same point is our R-report, so K1ABC
// has not rogered. The stop ends the exchange and nothing is logged, then or later.
func TestOperating_AContactNotEarnedIsNotLoggedWhenAStopLandsBeforeItsRungIsAdmitted(t *testing.T) {
	f := newReplacedContact(t)
	a := f.s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))
	f.base = ProfileFT8.slotStart(time.Now().UTC())
	require.NoError(t, a.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		f.base.Format(time.RFC3339), 1500, 14.074, 1, false, ""))
	f.their(30, "CQ K1ABC FN42", -1)
	reached, letGo := f.heldBeforeTransmitting(t, func(msg string) bool {
		return strings.HasPrefix(msg, "K1ABC G0XYZ R")
	})
	slot := f.theirSlot(60, "G0XYZ K1ABC -10", -12)
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("the fixture: our R-report was never decided")
	}

	f.s.RetireOperating()
	require.False(t, f.s.seq.Active())

	letGo()
	f.s.AwaitCompletionHandover()
	select {
	case <-slot:
	case <-time.After(2 * time.Second):
		t.Fatal("the slot never ended")
	}
	require.Empty(t, f.loggedNow(), "a contact K1ABC had not rogered was logged by the stop")
	require.Zero(t, f.keyer.keys())

	f.newAppArms(t)
	f.requireNothingMoreUnderTheNewApp(t, 0)
}

// Structure, so the wait above keeps covering the decode loop: the package's own code
// reaches the sequencer's slot handler only inside driveSequencer, which marks the slot.
// Any mention of OnSlot elsewhere fails, a call or a method value alike; the method's own
// declaration is not a mention.
func TestSource_TheSequencerSlotIsDrivenOnlyThroughTheMarkedCall(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	fset := token.NewFileSet()
	inside, files := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		files++
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			owner := ""
			if ok {
				owner = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "OnSlot" {
					return true
				}
				if owner == "driveSequencer" {
					inside++
					return true
				}
				t.Errorf("%s: OnSlot is reached in %q, outside driveSequencer: a contact handed over from that slot is not waited for",
					fset.Position(sel.Pos()), owner)
				return true
			})
		}
	}
	require.Greater(t, files, 20, "the guard is looking at too little")
	require.Equal(t, 1, inside, "driveSequencer must drive the slot, once")
}
