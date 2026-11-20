package ft8

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// The operator's Abandon and an earned contact (invariant 1).
//
// The operator's ruling, 2026-11-20: "Log it. If the remote station has a QSO, we need to
// have it our end." Abandon ends the contact and cuts a transmission short, and leaves TX
// armed. Until this ruling it discarded a contact the partner had already rogered, where
// every other stop logged it. It now logs it, once, in whatever state our courtesy rung is:
// on the air, decided and not yet admitted, or deferred to a later slot. A contact the
// partner has not rogered is still discarded.

// abandon is the operator's Abandon, as the app that holds the arm asks for it.
func (f *replacedContact) abandon(t *testing.T) {
	t.Helper()
	require.NoError(t, f.s.OperatingAs(lifeA).AbandonQso())
}

// requireLoggedOnceAndQuiet: the contact is logged once, the exchange is over and TX is
// still armed. K1ABC, who never heard our 73, then rogers twice more under that arm:
// nothing is logged again, no rung is asked for and the rig is not keyed.
func (f *replacedContact) requireLoggedOnceAndQuiet(t *testing.T) {
	t.Helper()
	logged := f.loggedNow()
	require.Len(t, logged, 1, "K1ABC had rogered: the contact is logged, and once")
	require.Equal(t, "K1ABC", logged[0].TheirCall)
	require.InDelta(t, 14.074, logged[0].DialFreqMHz, 1e-9, "on the frequency of the exchange")
	require.Equal(t, int64(1), logged[0].LogbookID, "in the logbook the exchange was started in")
	require.False(t, f.s.seq.Active(), "the exchange outlived the abandon")
	require.True(t, f.s.armedNow(), "an abandon ends the contact, not the arm")

	sent, keys := len(f.sentNow()), f.keyer.keys()
	f.their(120, "G0XYZ K1ABC RR73", -11)
	f.their(150, "G0XYZ K1ABC RR73", -11)
	require.Len(t, f.loggedNow(), 1, "the abandoned contact was logged again")
	require.Len(t, f.sentNow(), sent, "a rung was asked for after the abandon")
	require.Equal(t, keys, f.keyer.keys(), "the rig was keyed after the abandon")
	require.False(t, f.s.seq.Active())
}

// Our closing 73 is keyed and playing when the operator abandons. The 73 is cut short and
// the contact is logged, by the abandon itself and once: the transmission's own completion,
// arriving afterwards, adds nothing.
func TestOperating_AnAbandonDuringTheClosingRungLogsTheContactOnce(t *testing.T) {
	f := newReplacedContact(t)
	f.toTheirReport(t)
	f.theyRoger(t)
	require.True(t, f.s.txInFlightNow(), "the fixture: the closing 73 is in flight")
	require.Empty(t, f.loggedNow(), "the fixture: the 73 has not finished, so nothing is logged yet")

	f.abandon(t)

	require.Len(t, f.loggedNow(), 1, "the abandon returned with the rogered contact not logged")
	f.s.txWg.Wait() // the cut transmission's goroutine, and its completion callback, have ended
	require.False(t, f.s.txInFlightNow(), "the abandon did not cut the 73 short")
	require.Positive(t, f.keyer.unkeys(), "the rig was left keyed")
	f.requireLoggedOnceAndQuiet(t)
}

// Our closing 73 is decided and the service has not been asked for it: the slot is between
// the sequencer's lock and the service's. The abandon logs the contact. The slot then asks
// for its transmission with TX still armed, and is refused: the 73 is not keyed.
func TestOperating_AnAbandonBeforeTheClosingRungIsAdmittedLogsTheContactOnce(t *testing.T) {
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

	f.abandon(t)
	require.Len(t, f.loggedNow(), 1, "the abandon returned with the rogered contact not logged")

	letGo()
	f.s.AwaitCompletionHandover()
	select {
	case <-slot:
	case <-time.After(2 * time.Second):
		t.Fatal("the slot never ended")
	}
	require.False(t, f.s.txInFlightNow(), "the closing 73 was admitted after the abandon")
	require.Zero(t, f.keyer.keys(), "the closing 73 was keyed after the abandon")
	f.requireLoggedOnceAndQuiet(t)
}

// K1ABC's roger is taken in too late for our 73 to go out in that slot, and the operator
// abandons before the next one.
func TestOperating_AnAbandonAfterADeferredClosingRungLogsTheContactOnce(t *testing.T) {
	f := newReplacedContact(t)
	f.toTheirReport(t)
	start := time.Unix(f.base.Unix()+90, 0).UTC()
	f.s.driveSequencer(ProfileFT8.SlotRefFromTime(start),
		[]goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)},
		start.Add(ProfileFT8.Slot+ProfileFT8.LateWindow+time.Second))
	require.True(t, f.s.seq.Active(), "the fixture: the exchange is live")
	require.NotContains(t, f.sentNow(), closingRung, "the fixture: the closing 73 was deferred")
	require.Empty(t, f.loggedNow(), "the fixture: nothing is logged yet")

	f.abandon(t)

	require.Zero(t, f.keyer.keys(), "the closing 73 was keyed")
	f.requireLoggedOnceAndQuiet(t)
}

// The other side of the line: K1ABC has NOT rogered when the operator abandons. Nothing is
// logged, then or when the roger arrives afterwards under the same arm.
func TestOperating_AnAbandonBeforeTheRogerLogsNothing(t *testing.T) {
	f := newReplacedContact(t)
	f.toTheirReport(t)

	f.abandon(t)

	require.False(t, f.s.seq.Active())
	require.True(t, f.s.armedNow(), "an abandon ends the contact, not the arm")
	require.Empty(t, f.loggedNow(), "a contact K1ABC had not rogered was logged by the abandon")
	sent := len(f.sentNow())
	f.their(90, "G0XYZ K1ABC RR73", -11)
	f.their(120, "G0XYZ K1ABC RR73", -11)
	require.Empty(t, f.loggedNow(), "the abandoned contact was logged when its roger arrived")
	require.Len(t, f.sentNow(), sent, "a rung was asked for after the abandon")
	require.Zero(t, f.keyer.keys())
}

// Stopping RF does not wait on logging (codex-206). The closing 73 is on the air and the
// log hand-over is held. The abandon retires the session, cuts the 73, and only then hands
// the contact over: the transmission ends while the hand-over is still held.
func TestOperating_AnAbandonCutsTheClosingRungBeforeTheContactIsHandedToTheLog(t *testing.T) {
	f := newReplacedContact(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	letThrough := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(letThrough) // a failed assertion must not leave the hand-over, and the abandon, waiting
	f.mu.Lock()
	f.holdLog = func() {
		enterOnce.Do(func() { close(entered) })
		<-release
	}
	f.mu.Unlock()

	f.toTheirReport(t)
	f.theyRoger(t)
	require.True(t, f.s.txInFlightNow(), "the fixture: the closing 73 is in flight")

	abandoned := make(chan error, 1)
	go func() { abandoned <- f.s.OperatingAs(lifeA).AbandonQso() }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fixture: the abandon never reached the log hand-over")
	}

	cut := make(chan struct{})
	go func() {
		f.s.txWg.Wait() // the transmission's goroutine has ended: unkeyed, cleaned up
		close(cut)
	}()
	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Fatal("the closing 73 was still on the air while the contact was being handed to the log")
	}
	require.Positive(t, f.keyer.unkeys(), "the rig was left keyed")
	require.False(t, f.s.txInFlightNow())
	require.Empty(t, f.loggedNow(), "the fixture: the hand-over is still held")
	select {
	case err := <-abandoned:
		t.Fatalf("the fixture: the abandon returned (%v) with the hand-over held", err)
	default:
	}

	letThrough()
	select {
	case err := <-abandoned:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the abandon never returned after the hand-over finished")
	}
	f.requireLoggedOnceAndQuiet(t)
}

// An earlier rung's refusal arriving late (codex-206). A rung is accepted a slot before it
// is keyed, and the dial check beside the key can refuse it then, long after the request
// returned: the service ends that rung's session through AbandonIfCurrent. Here that rung
// is our R-report, which carries no completion of its own. While its refusal is still on
// its way, K1ABC's roger is taken in, too late for our 73: the contact is earned, in the
// same session. The refusal then ends the session, and the contact must be logged.
func TestOperating_ALateRefusalOfAnEarlierRungLogsTheContactEarnedMeanwhile(t *testing.T) {
	f := newReplacedContact(t)
	a := f.s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))
	f.base = ProfileFT8.slotStart(time.Now().UTC())
	require.NoError(t, a.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		f.base.Format(time.RFC3339), 1500, 14.074, 1, false, ""))
	f.their(30, "CQ K1ABC FN42", -1)

	// Our R-report goes through the service's real transmission. Its work is held and then
	// ends as the pre-key dial check ends it; the refusal callback is the one seqTransmit
	// registers for every rung.
	refuse := make(chan struct{})
	var once sync.Once
	letRefuse := func() { once.Do(func() { close(refuse) }) }
	t.Cleanup(letRefuse)
	var hadCompletion atomic.Bool
	real := f.s.seq.transmit
	f.s.seq.transmit = func(msg string, offsetHz, dialMHz float64, gen uint64, onDone func(bool)) error {
		if !strings.HasPrefix(msg, "K1ABC G0XYZ R") {
			return real(msg, offsetHz, dialMHz, gen, onDone)
		}
		f.mu.Lock()
		f.sent = append(f.sent, msg)
		f.mu.Unlock()
		hadCompletion.Store(onDone != nil)
		return f.s.startTransmission(msg, offsetHz, dialMHz,
			func() bool { return f.s.seq.isCurrent(gen) },
			func(context.Context, *TxController) error {
				<-refuse
				return ErrTxDialUnknown
			}, onDone,
			func(refusal error) { f.s.seq.AbandonIfCurrent(gen, endReasonForRefusal(refusal)) })
	}
	f.their(60, "G0XYZ K1ABC -10", -12)
	require.True(t, f.s.txInFlightNow(), "the fixture: our R-report's transmission is under way")
	require.False(t, hadCompletion.Load(), "the fixture: an R-report carries no completion of its own")

	start := time.Unix(f.base.Unix()+90, 0).UTC()
	f.s.driveSequencer(ProfileFT8.SlotRefFromTime(start),
		[]goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)},
		start.Add(ProfileFT8.Slot+ProfileFT8.LateWindow+time.Second))
	require.True(t, f.s.seq.Active(), "the fixture: the exchange is live")
	require.NotContains(t, f.sentNow(), closingRung, "the fixture: the closing 73 was deferred")
	require.Empty(t, f.loggedNow(), "the fixture: nothing is logged yet")

	letRefuse()
	f.s.txWg.Wait() // the refused transmission's goroutine, and its refusal callback, have ended

	require.False(t, f.s.seq.Active(), "a refused frequency confirmation ends the session")
	logged := f.loggedNow()
	require.Len(t, logged, 1, "K1ABC had rogered: the contact is logged, and once")
	require.Equal(t, "K1ABC", logged[0].TheirCall)
	require.InDelta(t, 14.074, logged[0].DialFreqMHz, 1e-9, "on the frequency of the exchange")
	require.Equal(t, int64(1), logged[0].LogbookID)
	require.Zero(t, f.keyer.keys(), "the rig was keyed")
}

// scopedAbandons are the two abandons that end ONE session generation and no other.
var scopedAbandons = []struct {
	name string
	end  func(s *Sequencer, gen uint64)
}{
	{"AbandonIfCurrent", func(s *Sequencer, gen uint64) { s.AbandonIfCurrent(gen, EndReasonDialMoved) }},
	{"abandonNamedIfCurrent", func(s *Sequencer, gen uint64) { s.abandonNamedIfCurrent(gen, EndReasonDialMoved, "") }},
}

// An abandon scoped to the session that earned the contact logs it, and the session's
// last frame still says why it ended.
func TestEarned_AScopedAbandonOfItsOwnSessionLogsTheContact(t *testing.T) {
	for _, l := range groupALadders {
		for _, by := range scopedAbandons {
			t.Run(l.name+": "+by.name, func(t *testing.T) {
				s, r := rogerDeferred(t, l, deferrals[0])

				by.end(s, s.currentGen())

				require.Len(t, r.completed, 1, "they had rogered: the session's end logs the contact")
				require.Equal(t, l.their, r.completed[0].TheirCall)
				require.False(t, s.Active())
				r.mu.Lock()
				last := r.statuses[len(r.statuses)-1]
				r.mu.Unlock()
				require.False(t, last.Active)
				require.Equal(t, EndReasonDialMoved, last.EndReason, "the last frame lost the reason the session ended")

				driveTheir(s, l.rogerAt+30, []goft8.DecodedMessage{dm(l.roger, -11)})
				require.Len(t, r.completed, 1, "and never logs it twice")
				require.Zero(t, countMsg(r.sentMsgs(), l.closing), "nothing is sent for it afterwards")
			})
		}
	}
}

// An abandon scoped to an EARLIER session does nothing to the one that replaced it: the
// replacement's earned contact is neither logged by it nor ended by it.
func TestEarned_AScopedAbandonOfAnEarlierSessionLeavesTheNextOnesContact(t *testing.T) {
	for _, l := range groupALadders {
		for _, by := range scopedAbandons {
			t.Run(l.name+": "+by.name, func(t *testing.T) {
				r := &seqRecorder{asyncFail: true}
				s := newTestSeq(r)
				l.start(t, s)
				earlier := s.currentGen()
				s.Abandon()
				require.Empty(t, r.completed, "the fixture: the earlier session logged nothing")

				l.start(t, s)
				l.toRoger(s)
				driveTheirLate(s, l.rogerAt, []goft8.DecodedMessage{dm(l.roger, -11)})
				require.True(t, s.Active(), "the fixture: the next session is live, its contact earned")
				require.Empty(t, r.completed)

				by.end(s, earlier)

				require.Empty(t, r.completed, "an abandon of the earlier session logged the next one's contact")
				require.True(t, s.Active(), "an abandon of the earlier session ended the next one")
				driveTheir(s, l.rogerAt+30, nil)
				require.Len(t, r.completed, 1, "the next session goes on to send its closing rung and log")
			})
		}
	}
}

// The order inside the abandon: the session is retired BEFORE the transmission is cut,
// never after (the commit gate, review 2026-07-20 #1). The closing 73 is decided and held
// short of the service. The abandon is stopped where it stamps the contact, before it has
// retired anything; the 73 is then admitted, as the live session's rung, and goes on the
// air. When the abandon goes on it retires the session and THEN looks for a transmission
// to cut, so it finds this one. Cutting first would have found nothing to cut, and the 73
// would play on after the abandon returned.
func TestOperating_ARungAdmittedWhileAnAbandonIsClaimingIsStillCut(t *testing.T) {
	f := newReplacedContact(t)
	f.toTheirReport(t)

	stamping, resume := make(chan struct{}), make(chan struct{})
	var first, resumeOnce sync.Once
	goOn := func() { resumeOnce.Do(func() { close(resume) }) }
	t.Cleanup(goOn)
	real := f.s.seq.prepareComplete
	f.s.seq.prepareComplete = func(c *CompletedQso) {
		real(c)
		first.Do(func() {
			close(stamping)
			<-resume
		})
	}

	reached, letGo := f.heldBeforeTransmitting(t, func(msg string) bool { return msg == closingRung })
	slot := f.theirSlot(90, "G0XYZ K1ABC RR73", -11)
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("the fixture: the closing 73 was never decided")
	}

	abandoned := make(chan error, 1)
	go func() { abandoned <- f.s.OperatingAs(lifeA).AbandonQso() }()
	select {
	case <-stamping:
	case <-time.After(2 * time.Second):
		t.Fatal("the fixture: the abandon never reached the contact's stamp")
	}
	letGo()
	select {
	case <-slot:
	case <-time.After(2 * time.Second):
		t.Fatal("the slot never ended")
	}
	require.True(t, f.s.txInFlightNow(), "the fixture: the 73 was admitted while the abandon was claiming")

	goOn()
	select {
	case err := <-abandoned:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the abandon never returned")
	}
	cut := make(chan struct{})
	go func() {
		f.s.txWg.Wait()
		close(cut)
	}()
	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Fatal("the closing 73 was still on the air after the abandon returned")
	}
	require.False(t, f.s.txInFlightNow())
	require.Equal(t, f.keyer.keys(), f.keyer.unkeys(), "the rig was left keyed")
	f.requireLoggedOnceAndQuiet(t)
}

// heldWriter is a log writer that stops at the first line containing match, until let go.
type heldWriter struct {
	match   string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *heldWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.match) {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	return len(p), nil
}

// heldLogger is a real logger whose writer stops at the line containing match.
func heldLogger(t *testing.T, match string) (log logging.Logger, entered <-chan struct{}, letGo func()) {
	t.Helper()
	w := &heldWriter{match: match, entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	letGo = func() { once.Do(func() { close(w.release) }) }
	t.Cleanup(letGo)
	return logging.NewForWriter(w), w.entered, letGo
}

// withHeldLogLine gives the sequencer such a logger. Call it before anything is started.
func (f *replacedContact) withHeldLogLine(t *testing.T, match string) (entered <-chan struct{}, letGo func()) {
	t.Helper()
	log, entered, letGo := heldLogger(t, match)
	f.s.seq.log = log
	return entered, letGo
}

// withHeldEvictionLine gives the service's real hub such a logger, stopped at the record
// of an evicted subscriber. Call it before anything is started.
func (f *replacedContact) withHeldEvictionLine(t *testing.T) (entered <-chan struct{}, letGo func()) {
	t.Helper()
	log, entered, letGo := heldLogger(t, "subscriber evicted")
	f.s.hub.logger = log
	return entered, letGo
}

// aFullSubscriber subscribes to the service's hub and fills its buffer, so that the next
// event published evicts it. Call it when nothing else is about to publish.
func (f *replacedContact) aFullSubscriber(t *testing.T) <-chan hubEvent {
	t.Helper()
	ch, unsubscribe := f.s.hub.subscribe()
	t.Cleanup(unsubscribe)
	for len(ch) < cap(ch) {
		f.s.hub.publish(hubEvent{name: "test-fill"})
	}
	return ch
}

// requireEvicted: the subscriber's stream was ended by the hub.
func requireEvicted(t *testing.T, ch <-chan hubEvent) {
	t.Helper()
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the fixture: the full subscriber was never evicted")
		}
	}
}

// onTheAir sends the rungs that match through the service's real transmission, as the
// fixture already sends the closing 73.
func (f *replacedContact) onTheAir(match func(msg string) bool) {
	real := f.s.seq.transmit
	f.s.seq.transmit = func(msg string, offsetHz, dialMHz float64, gen uint64, onDone func(bool)) error {
		if !match(msg) {
			return real(msg, offsetHz, dialMHz, gen, onDone)
		}
		f.mu.Lock()
		f.sent = append(f.sent, msg)
		f.mu.Unlock()
		return f.s.startTransmission(msg, offsetHz, dialMHz,
			func() bool { return f.s.seq.isCurrent(gen) },
			func(ctx context.Context, ctrl *TxController) error {
				return ctrl.transmit(ctx, []int16{1, 2, 3}, time.Time{}, nil)
			}, onDone, nil)
	}
}

// requireCutWhile: the abandon is under way and stopped at heldAt; the transmission must
// already have been cut, and ends without the abandon being let go.
func (f *replacedContact) requireCutWhile(t *testing.T, heldAt <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-heldAt:
	case <-time.After(2 * time.Second):
		t.Fatalf("the fixture: the abandon never reached %s", what)
	}
	cut := make(chan struct{})
	go func() {
		f.s.txWg.Wait() // the transmission's goroutine has ended: unkeyed, cleaned up
		close(cut)
	}()
	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Fatalf("the transmission was still on the air while the abandon was held at %s", what)
	}
	require.Positive(t, f.keyer.unkeys(), "the rig was left keyed")
	require.False(t, f.s.txInFlightNow())
}

// Stopping RF does not wait on a log line either (codex-207). The writer of the daemon's
// log can be held up as the hand-over can. With an earned contact, the line that says the
// contact was completed is written after the cut.
func TestOperating_AnAbandonCutsTheClosingRungBeforeItsLogLineIsWritten(t *testing.T) {
	f := newReplacedContact(t)
	atLine, letGo := f.withHeldLogLine(t, "QSO logged anyway")
	f.toTheirReport(t)
	f.theyRoger(t)
	require.True(t, f.s.txInFlightNow(), "the fixture: the closing 73 is in flight")

	abandoned := make(chan error, 1)
	go func() { abandoned <- f.s.OperatingAs(lifeA).AbandonQso() }()
	f.requireCutWhile(t, atLine, "the completed contact's log line")

	letGo()
	select {
	case err := <-abandoned:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the abandon never returned after the log line was written")
	}
	f.requireLoggedOnceAndQuiet(t)
}

// The same for a contact that is not earned: our R-report is on the air when the operator
// abandons. The line that says the session was abandoned is written after the cut.
func TestOperating_AnAbandonCutsAnEarlierRungBeforeItsLogLineIsWritten(t *testing.T) {
	f := newReplacedContact(t)
	atLine, letGo := f.withHeldLogLine(t, "session abandoned")
	f.onTheAir(func(msg string) bool { return strings.HasPrefix(msg, "K1ABC G0XYZ R") })
	f.toTheirReport(t)
	require.Eventually(t, func() bool { return f.keyer.keys() > 0 && f.player.plays() > 0 },
		2*time.Second, 5*time.Millisecond, "the fixture: our R-report must be keyed and playing")
	require.True(t, f.s.txInFlightNow())

	abandoned := make(chan error, 1)
	go func() { abandoned <- f.s.OperatingAs(lifeA).AbandonQso() }()
	f.requireCutWhile(t, atLine, "the abandoned session's log line")

	letGo()
	select {
	case err := <-abandoned:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the abandon never returned after the log line was written")
	}
	require.Empty(t, f.loggedNow(), "a contact K1ABC had not rogered was logged by the abandon")
	require.False(t, f.s.seq.Active())
	require.True(t, f.s.armedNow(), "an abandon ends the contact, not the arm")
}

// Nor on the hub's record of an evicted subscriber (codex-208). The frame that says the
// session is over is published before the cut, as it must be: it is the hub that then
// writes a line, for a subscriber too slow to take the frame. That line is written after
// the cut, for an earned contact and for a contact that is not.
func TestOperating_AnAbandonCutsBeforeAnEvictedSubscriberIsReported(t *testing.T) {
	cases := []struct {
		name   string
		onAir  func(t *testing.T, f *replacedContact)
		logged int
	}{
		{"the closing rung, the contact earned", func(t *testing.T, f *replacedContact) {
			f.toTheirReport(t)
			f.theyRoger(t)
		}, 1},
		{"an earlier rung, nothing earned", func(t *testing.T, f *replacedContact) {
			f.onTheAir(func(msg string) bool { return strings.HasPrefix(msg, "K1ABC G0XYZ R") })
			f.toTheirReport(t)
			require.Eventually(t, func() bool { return f.keyer.keys() > 0 && f.player.plays() > 0 },
				2*time.Second, 5*time.Millisecond, "the fixture: our R-report must be keyed and playing")
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplacedContact(t)
			atLine, letGo := f.withHeldEvictionLine(t)
			tc.onAir(t, f)
			require.True(t, f.s.txInFlightNow(), "the fixture: a transmission is in flight")
			slow := f.aFullSubscriber(t)

			abandoned := make(chan error, 1)
			go func() { abandoned <- f.s.OperatingAs(lifeA).AbandonQso() }()
			f.requireCutWhile(t, atLine, "the evicted subscriber's log line")

			letGo()
			select {
			case err := <-abandoned:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("the abandon never returned after the log line was written")
			}
			requireEvicted(t, slow)
			require.Len(t, f.loggedNow(), tc.logged)
			require.False(t, f.s.seq.Active())
		})
	}
}
