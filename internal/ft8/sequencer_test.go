package ft8

import (
	stderrors "errors"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// seqRecorder captures the sequencer's side effects (transmits, published
// statuses, completions) for assertions.
type seqRecorder struct {
	mu          sync.Mutex
	sent        []string
	offsets     []float64
	dials       []float64
	statuses    []QsoStatus
	completed   []CompletedQso
	transmitErr error // non-nil → transmit returns it synchronously (onDone never fires)
	asyncFail   bool  // true → transmit "queues" (returns nil) but onDone(false): the
	// transmission started then failed on air (review H1 — the final-rung case).
	deferOnDone   bool          // true → transmit captures onDone instead of firing it
	pendingOnDone func(ok bool) // the last captured (deferred) onDone
}

// fireOnDone invokes the most recently captured deferred onDone, simulating the
// tx goroutine completing later. Used to test async-callback ordering (a session
// superseded before its final-rung callback returns). No-op if none pending.
func (r *seqRecorder) fireOnDone(ok bool) {
	r.mu.Lock()
	fn := r.pendingOnDone
	r.pendingOnDone = nil
	r.mu.Unlock()
	if fn != nil {
		fn(ok)
	}
}

// transmit mirrors Service.seqTransmit's contract: it returns synchronously
// ("queued"), then fires onDone with the on-air outcome. Tests fire onDone
// synchronously (the real path fires it from the tx goroutine); ok=true unless
// asyncFail is set, so the final-rung completion path is exercised either way.
// transmit matches the sequencer's injected shape; gen (the rung's session
// generation, checked by the real Service at commit) is irrelevant to these
// recorder-based tests and ignored.
func (r *seqRecorder) transmit(msg string, off, dialMHz float64, _ uint64, onDone func(ok bool)) error {
	r.mu.Lock()
	if r.transmitErr != nil {
		err := r.transmitErr
		r.mu.Unlock()
		return err
	}
	r.sent = append(r.sent, msg)
	r.offsets = append(r.offsets, off)
	r.dials = append(r.dials, dialMHz)
	fail := r.asyncFail
	if r.deferOnDone {
		r.pendingOnDone = onDone // fired later via fireOnDone
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	if onDone != nil {
		onDone(!fail)
	}
	return nil
}

func (r *seqRecorder) publish(st QsoStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, st)
}

func (r *seqRecorder) sentMsgs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

func (r *seqRecorder) lastStatus() QsoStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.statuses) == 0 {
		return QsoStatus{}
	}
	return r.statuses[len(r.statuses)-1]
}

func newTestSeq(r *seqRecorder) *Sequencer {
	s := newSequencer(r.transmit, r.publish, 0, nil) // 0 → defaultSeqMaxRepeats
	s.onComplete = func(c CompletedQso) {
		r.mu.Lock()
		r.completed = append(r.completed, c)
		r.mu.Unlock()
	}
	return s
}

// dm builds a decoded message with a given SNR.
func dm(text string, snr int) goft8.DecodedMessage {
	return goft8.DecodedMessage{Text: text, SNR: snr}
}

// driveTheir simulates the decode of the worked station's slot starting at
// `sec` (which must be an even slot — the worked station's parity here), firing
// OnSlot ~1 s into our following slot (a valid late-start dt).
func driveTheir(s *Sequencer, sec int64, msgs []goft8.DecodedMessage) {
	ref := SlotRefFromTime(time.Unix(sec, 0).UTC())
	now := time.Unix(sec+slotSeconds+1, 0).UTC() // 1 s into the current (our) slot
	s.OnSlot(ref, msgs, now)
}

// The worked station transmits in even slots (sec 0/30/60/90 → period "even");
// we answer in the odd slots between. driveTheir uses even `sec`.
func TestSequencer_HappyPath(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	// Answer K1ABC's CQ heard in the even slot at epoch 0.
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active())

	// Their re-CQ (or silence) → we send our call.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	// Their report to us (we measured them at -12) → we roger + report.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	// Their RR73 → we send 73 and the QSO completes.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

	require.Equal(t, []string{
		"K1ABC G0XYZ IO91",
		"K1ABC G0XYZ R-12",
		"K1ABC G0XYZ 73",
	}, r.sentMsgs())

	for _, o := range r.offsets {
		require.Equal(t, 1500.0, o)
	}
	require.False(t, s.Active(), "QSO should be idle after the 73")
	require.Len(t, r.completed, 1)
	require.Equal(t, "K1ABC", r.completed[0].TheirCall)
	require.Equal(t, "FN42", r.completed[0].TheirGrid)
	require.Equal(t, -10, r.completed[0].TheirReport) // they sent us -10
	require.Equal(t, -12, r.completed[0].OurReport)   // we sent R-12
	require.Equal(t, 14.074, r.completed[0].DialFreqMHz)
	require.Equal(t, 1500.0, r.completed[0].OffsetHz)
}

// TestSequencer_StartTheirPeriodNoRace guards the data race where a start method's
// info log read s.theirPeriod AFTER releasing s.mu, while a concurrent abandon-then-
// start wrote it under s.mu. Meaningful under `go test -race`: many goroutines each
// abandon then start, so a start's unguarded log read overlaps another's guarded
// write. The captured-under-lock fix must make this clean; the value is otherwise
// irrelevant to the assertions.
func TestSequencer_StartTheirPeriodNoRace(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Abandon()
			_ = s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now)
		}()
	}
	wg.Wait()
}

func TestSequencer_OnlyTransmitsOppositeParity(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// A slot of OUR parity (odd, sec=15) just decoded → nothing to send.
	ref := SlotRefFromTime(time.Unix(15, 0).UTC())
	s.OnSlot(ref, nil, time.Unix(15+slotSeconds+1, 0).UTC())
	require.Empty(t, r.sentMsgs(), "must not transmit off a our-parity slot")

	// A their-parity slot does trigger our call.
	driveTheir(s, 30, nil)
	require.Equal(t, []string{"K1ABC G0XYZ IO91"}, r.sentMsgs())
}

func TestSequencer_FiresOpeningRungImmediately(t *testing.T) {
	// Click lands early in our TX slot → the opening call goes out THIS slot without
	// waiting for the next OnSlot (the first-rung-delay fix). theirSlotUTC even →
	// theirPeriod even → we TX in odd slots; now = 1 s into the odd slot at unix 15.
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(16, 0).UTC()))
	require.Equal(t, []string{"K1ABC G0XYZ IO91"}, r.sentMsgs(),
		"opening call should fire on the start slot, not wait for OnSlot")
}

func TestSequencer_NoImmediateFireLateOrWrongParity(t *testing.T) {
	// Too late into our (odd) slot (6 s > 4.5 s) → wait for OnSlot, no immediate fire.
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(15+6, 0).UTC()))
	require.Empty(t, r.sentMsgs(), "a late start must wait for OnSlot")

	// Starting in the worked station's (even) slot → not ours to transmit in.
	r2 := &seqRecorder{}
	s2 := newTestSeq(r2)
	require.NoError(t, s2.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(2, 0).UTC()))
	require.Empty(t, r2.sentMsgs(), "must not transmit in the worked station's slot")
}

func TestSequencer_LateStartGuardSkips(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// Fire OnSlot too late into our slot (6 s in > txLateWindowSec 4.5 s) → skip.
	ref := SlotRefFromTime(time.Unix(30, 0).UTC())
	s.OnSlot(ref, nil, time.Unix(30+slotSeconds+6, 0).UTC())
	require.Empty(t, r.sentMsgs(), "a too-late slot must be skipped")
	require.True(t, s.Active(), "skipping a slot does not end the QSO")
}

func TestSequencer_AbandonsAfterMaxRepeats(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.maxRepeats = 2
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// No answer ever (empty their-slots): call, call, then abandon.
	driveTheir(s, 30, nil) // repeat 1
	driveTheir(s, 60, nil) // repeat 2
	driveTheir(s, 90, nil) // would be repeat 3 > max → abandon, no transmit
	require.Equal(t, []string{"K1ABC G0XYZ IO91", "K1ABC G0XYZ IO91"}, r.sentMsgs())
	require.False(t, s.Active(), "abandons after max unanswered repeats")
}

// TestSequencer_MaxRepeatsHonouredAndExposed: a custom cap (via newSequencer) is
// applied, and the published status advertises MaxRepeats on the capped rungs so
// the SPA can render "calls left" — but NOT on the one-shot 73 (where the cap does
// not apply). The exposed value is the SPA's countdown denominator.
func TestSequencer_MaxRepeatsHonouredAndExposed(t *testing.T) {
	r := &seqRecorder{}
	s := newSequencer(r.transmit, r.publish, 3, nil)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// Opening (calling) rung is capped → the cap is advertised; remaining = max-repeats.
	st := r.lastStatus()
	require.Equal(t, 3, st.MaxRepeats, "capped rung should advertise the cap")
	require.GreaterOrEqual(t, st.MaxRepeats-st.Repeats, 0, "remaining must not go negative")

	// 0 → the default cap (parity with the pre-config behaviour).
	r2 := &seqRecorder{}
	s2 := newSequencer(r2.transmit, r2.publish, 0, nil)
	require.Equal(t, defaultSeqMaxRepeats, s2.maxRepeats)
}

func TestSequencer_StartErrors(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)

	require.ErrorIs(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", slot, 0, 14.074, time.Unix(0, 0).UTC()), ErrNoOffset)
	require.Error(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", "not-a-time", 1500, 14.074, time.Unix(0, 0).UTC()))

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", slot, 1500, 14.074, time.Unix(0, 0).UTC()))
	require.ErrorIs(t, s.StartQso("G0XYZ", "IO91", "W2XYZ", "", slot, 1500, 14.074, time.Unix(0, 0).UTC()), ErrQsoInProgress)
}

func TestSequencer_Abandon(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	s.Abandon()
	require.False(t, s.Active())
	// Idle: a their-slot now does nothing.
	driveTheir(s, 30, nil)
	require.Empty(t, r.sentMsgs())
}

func TestSequencer_AbandonsWhenDisarmedMidQso(t *testing.T) {
	r := &seqRecorder{transmitErr: ErrTxNotArmed} // simulate TX disarmed under us
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, nil) // transmit returns ErrTxNotArmed → abandon
	require.False(t, s.Active(), "a not-armed transmit abandons the QSO")
	require.True(t, stderrors.Is(r.transmitErr, ErrTxNotArmed))
}

// TestSequencer_TransmitCarriesSessionDial pins the decode-log dial fix: the dial
// passed to transmit comes from the ACTIVE session's own state, and a rejected
// concurrent StartQso (ErrQsoInProgress) can never relabel an active rung with the
// rejected request's dial (the dial is threaded through transmit, not held in
// shared mutable Service state).
func TestSequencer_TransmitCarriesSessionDial(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// A second start with a DIFFERENT dial is rejected while a session is active.
	err := s.StartQso("G0XYZ", "IO91", "W1AW", "FN31",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 21.074, time.Unix(0, 0).UTC())
	require.ErrorIs(t, err, ErrQsoInProgress)

	// Drive a rung on the active session; every transmit carries 14.074, not 21.074.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})

	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.dials, "at least one rung transmitted")
	for _, d := range r.dials {
		require.Equal(t, 14.074, d, "transmit must carry the accepted session dial")
	}
}

// TestSequencer_SkipIfSilent_EndsWithoutRepeat — the operator's deferred Next,
// moved daemon-side (2026-07-13): armed mid-contact, a silent cycle on an
// already-sent rung ends the session with NO repeat transmission. (The prior
// SPA-side skip could only abandon a repeat the daemon had already keyed —
// an audible PTT tick and a fraction of a second of RF at a no-show.)
func TestSequencer_SkipIfSilent_EndsWithoutRepeat(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, nil) // opening transmitted (repeats=1)
	require.NoError(t, s.SetSkipIfSilent(true))
	require.True(t, r.lastStatus().SkipArmed, "arm must publish skip_armed (confirm-by-push)")

	driveTheir(s, 60, nil) // silent cycle → skip fires INSTEAD of the repeat
	require.Equal(t, []string{"K1ABC G0XYZ IO91"}, r.sentMsgs(), "no repeat transmission")
	require.False(t, s.Active(), "session ended by the skip")
	require.False(t, r.lastStatus().Active)
}

// A reply after arming disarms the skip (they came back) — the contact
// continues, and a LATER silent cycle repeats normally (no stale skip).
func TestSequencer_SkipIfSilent_DisarmsOnReply(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, nil) // opening
	require.NoError(t, s.SetSkipIfSilent(true))
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)}) // they reply
	require.True(t, s.Active(), "reply keeps the contact")
	require.False(t, r.lastStatus().SkipArmed, "reply disarms the skip")

	driveTheir(s, 90, nil) // silent — but the skip is gone: normal repeat
	require.Equal(t, []string{
		"K1ABC G0XYZ IO91",
		"K1ABC G0XYZ R-12",
		"K1ABC G0XYZ R-12",
	}, r.sentMsgs())
	require.True(t, s.Active())
}

// Arming needs a skippable session; disarm is always accepted (idempotent).
func TestSequencer_SkipIfSilent_ArmRequiresSession(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.ErrorIs(t, s.SetSkipIfSilent(true), ErrNoActiveQso)
	require.NoError(t, s.SetSkipIfSilent(false))
}

// An arm placed before the opening ever transmits must not fire on the first
// their-slot (repeats=0 — there is nothing to skip yet): the opening goes out,
// THEN a silent cycle skips.
func TestSequencer_SkipIfSilent_NeverFiresBeforeFirstTransmit(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.NoError(t, s.SetSkipIfSilent(true))

	driveTheir(s, 30, nil) // first cycle: opening MUST transmit
	require.Equal(t, []string{"K1ABC G0XYZ IO91"}, r.sentMsgs())
	require.True(t, s.Active())

	driveTheir(s, 60, nil) // now a silent cycle on a sent rung → skip
	require.Equal(t, []string{"K1ABC G0XYZ IO91"}, r.sentMsgs())
	require.False(t, s.Active())
}

// TestSequencer_IsCurrentTracksAbandon pins the review 2026-07-20 #1 contract
// the Service's commit gate relies on: the generation captured at rung-decision
// time stays current until Abandon (or a new session) supersedes it.
func TestSequencer_IsCurrentTracksAbandon(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	s.mu.Lock()
	gen := s.sessionGen
	s.mu.Unlock()
	require.True(t, s.isCurrent(gen), "live session generation is current")

	s.Abandon()
	require.False(t, s.isCurrent(gen),
		"Abandon must supersede the generation so an in-gap rung is refused at commit")
}

// TestSequencer_ImmediateOpeningNotDoubleDriven pins review 2026-07-20 #2: the
// opening that fireOpening sent in the click's current slot must not be
// re-driven by the SAME slot's still-pending OnSlot — which would consume a
// second repeat and, at max_repeats=1, abandon the session while the opening
// transmission is still playing.
func TestSequencer_ImmediateOpeningNotDoubleDriven(t *testing.T) {
	r := &seqRecorder{}
	s := newSequencer(r.transmit, r.publish, 1, nil) // max_repeats=1 — the sharp case

	// CQ heard in the even slot at epoch 0; the operator clicks 1 s into OUR
	// odd slot (epoch 16) → fireOpening fires immediately in slot 15.
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(16, 0).UTC()))
	require.Len(t, r.sentMsgs(), 1, "opening fired immediately")
	require.True(t, s.Active())

	// The same slot's OnSlot was still pending when the session started (its
	// decode had just been published): identical slot, no reply from them yet.
	s.OnSlot(SlotRefFromTime(time.Unix(0, 0).UTC()),
		[]goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)}, time.Unix(16, 0).UTC())
	require.True(t, s.Active(),
		"same-slot OnSlot must not consume a second repeat and abandon (review 2026-07-20 #2)")
	require.Len(t, r.sentMsgs(), 1, "no second transmission in the same slot")

	// The next cycle proceeds normally: their report advances the exchange.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	require.True(t, s.Active())
	require.Len(t, r.sentMsgs(), 2, "roger-report transmitted on the following cycle")
}
