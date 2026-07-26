package ft8

import (
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// --- H1 (refined by the final-rung split): when a completed QSO is logged ----
//
// Review H1 established that a QSO logs only after its final rung TRANSMITS, so SM
// never records a contact it did not put on the air. The final-rung work split
// that by ladder (see finalrung.go): the rule still holds for GROUP B, where our
// closing RR73 is what completes the PARTNER's sequence and nothing is complete
// until it lands. It does NOT hold for GROUP A, where the partner already rogered
// — there the contact is finished on their side before we key at all, so
// withholding the log would leave the other operator holding a QSO we have no
// record of. Group A sends once and logs on either outcome; Group B retries under
// the repeat cap and logs only on success.
//
// H1's surviving half is asserted in every Group A test below: nothing logs before
// the final rung is reached.

// TestSequencer_ActiveCallsign covers ADR 0055 pin-at-arm: the active session
// exposes the callsign pinned at StartQso for the self-decode filter; idle (and
// after Abandon) it's empty, so nothing of ours is filtered.
func TestSequencer_ActiveCallsign(t *testing.T) {
	s := newTestSeq(&seqRecorder{})
	require.Equal(t, "", s.ActiveCallsign(), "idle sequencer has no active call")

	require.NoError(t, s.StartQso("7Q1XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.Equal(t, "7Q1XYZ", s.ActiveCallsign(), "active session exposes the pinned call")

	s.Abandon()
	require.Equal(t, "", s.ActiveCallsign(), "abandoned session has no active call")
}

// TestSequencer_FinalRungAsyncFailStillLogs: the 73 is queued (transmit returns
// nil) but the transmission then fails on air (onDone(false)). GROUP A — K1ABC
// already sent RR73, so the QSO IS logged, the session ends, and the 73 is never
// re-keyed.
func TestSequencer_FinalRungAsyncFailStillLogs(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	require.Empty(t, r.completed, "nothing logs before the final rung is reached (review H1)")

	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)}) // 73 queued, fails on air

	require.Contains(t, r.sentMsgs(), "K1ABC G0XYZ 73", "the 73 was queued")
	require.Len(t, r.completed, 1, "Group A: they already rogered, so the QSO logs anyway")
	require.False(t, s.Active(), "send-once: a failed Group A final rung ends the session")

	sentBefore := len(r.sentMsgs())
	driveTheir(s, 120, nil)
	require.Len(t, r.sentMsgs(), sentBefore, "a Group A final rung is never retried")
	require.Len(t, r.completed, 1, "and never logs twice")
}

// TestSequencer_FinalRungBadMessageStillLogs: a synchronous terminal error on the
// GROUP A final rung (ErrTxBadMessage — the 73 never reaches the air at all) still
// logs. TX going away does not un-make a contact the partner already rogered.
func TestSequencer_FinalRungBadMessageStillLogs(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	require.Empty(t, r.completed, "nothing logs before the final rung is reached (review H1)")

	r.mu.Lock()
	r.transmitErr = ErrTxBadMessage // the final 73 can't be sent
	r.mu.Unlock()
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

	require.NotContains(t, r.sentMsgs(), "K1ABC G0XYZ 73", "the 73 never went out")
	require.Len(t, r.completed, 1, "Group A logs even when the final 73 never keyed")
	require.False(t, s.Active(), "the session ends either way")
}

// TestCallerSequencer_FinalRR73AsyncFailDoesNotLog: caller-side equivalent — a
// queued-then-failed RR73 logs nothing (review H1).
func TestCallerSequencer_FinalRR73AsyncFailDoesNotLog(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)
	startCq(t, s)

	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)}) // RR73 queued, fails

	require.Contains(t, r.sentMsgs(), "DL9UW 7Q5MLV RR73", "the RR73 was queued")
	require.Empty(t, r.completed, "a failed RR73 must NOT log a QSO (review H1)")
}

// --- Follow-up M1: final-rung state survives transmit acceptance -------------

// A transient ErrTxInFlight on a GROUP A final 73 no longer retries: the rung is
// send-once, so the QSO logs and the session ends. The courtesy 73 is lost (the
// partner will repeat their RR73 a few times and give up), but both logs hold the
// contact — which is the trade the split was decided on. Group B keeps the retry;
// see TestCallerSequencer_FinalRR73InFlightRetries below.
func TestSequencer_FinalRungInFlightStillLogsOnce(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	require.Empty(t, r.completed, "nothing logs before the final rung is reached (review H1)")

	r.mu.Lock()
	r.transmitErr = ErrTxInFlight // a prior TX is still running when the 73 fires
	r.mu.Unlock()
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
	require.Len(t, r.completed, 1, "Group A logs on the single attempt")
	require.False(t, s.Active(), "and ends rather than holding the rig for a retry")

	r.mu.Lock()
	r.transmitErr = nil // TX free again
	r.mu.Unlock()
	driveTheir(s, 120, nil)
	require.NotContains(t, r.sentMsgs(), "K1ABC G0XYZ 73", "no retry once the session ended")
	require.Len(t, r.completed, 1, "and no second log")
}

// Caller equivalent: ErrTxInFlight on RR73 keeps the contact in cqRogering and
// retries RR73 — it must not silently drop back to CQ (review follow-up M1).
func TestCallerSequencer_FinalRR73InFlightRetries(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)
	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})

	r.mu.Lock()
	r.transmitErr = ErrTxInFlight
	r.mu.Unlock()
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)}) // RR73 attempt fails
	require.Empty(t, r.completed, "ErrTxInFlight on RR73 must not log")

	r.mu.Lock()
	r.transmitErr = nil
	r.mu.Unlock()
	driveTheir(s, 120, nil) // retry RR73 (not a CQ)
	sent := r.sentMsgs()
	require.Equal(t, "DL9UW 7Q5MLV RR73", sent[len(sent)-1], "retried RR73, did not drop to CQ")
	require.Len(t, r.completed, 1, "the retried RR73 logs the QSO")
}

// The session stays active until the final 73 completes; on success the
// deferred callback logs and goes idle (review follow-up M1 — keep-active model).
func TestSequencer_DeferredFinal73LogsOnSuccess(t *testing.T) {
	r := &seqRecorder{deferOnDone: true}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)}) // 73 queued, callback deferred

	require.True(t, s.Active(), "session stays active while the 73 is in flight")
	require.Empty(t, r.completed, "not logged until the 73 actually completes")

	r.fireOnDone(true)
	require.False(t, s.Active(), "idle once the 73 completes")
	require.Len(t, r.completed, 1, "logged on completion")
}

// Abandon while the final 73 is in flight bumps the generation, so the later
// (now-stale) success callback must neither log nor publish over the new state
// (review follow-up M1 — generation guard).
func TestSequencer_AbandonDuringFinal73SuppressesStaleCallback(t *testing.T) {
	r := &seqRecorder{deferOnDone: true}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)}) // 73 queued, deferred

	// A new QSO can't start while we're still keying the 73.
	require.ErrorIs(t, s.StartQso("G0XYZ", "IO91", "W2DEF", "EM12",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()),
		ErrQsoInProgress)

	s.Abandon() // supersedes the session (bumps generation)
	require.False(t, s.Active())

	r.fireOnDone(true) // the stale 73 callback finally fires
	require.Empty(t, r.completed, "a superseded session's callback must not log (gen guard)")
}

// --- M1: a session is not committed unless its opening message encodes -------

func TestSequencer_StartQsoRejectsUnencodableCall(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	// A type-4 compound call the standard encoder still rejects (the standard /P
	// variant encodes on go-ft8 ≥ v0.3.5, so /P is no longer an "unencodable" example).
	err := s.StartQso("G0XYZ", "IO91", "PJ4/K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC())
	require.ErrorIs(t, err, ErrTxBadMessage)
	require.False(t, s.Active(), "no session committed for an unencodable opening (review M1)")
	require.Empty(t, r.sentMsgs())
}

func TestCallerSequencer_StartCallCqRejectsUnencodableCq(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	err := s.StartCallCq("PJ4/K1ABC", "FN42", 2700, 28.074, "auto_first", "", time.Unix(0, 0).UTC())
	require.ErrorIs(t, err, ErrTxBadMessage)
	require.False(t, s.Active(), "no CQ session committed for an unencodable CQ (review M1)")
	require.Empty(t, r.sentMsgs())
}

// --- H2: operator_pick is rejected at start until the stack exists -----------

func TestStartCallCq_RejectsOperatorPick(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	s.cfg.TX.CallerAnswerMode = types.Ft8CallerAnswerOperatorPick
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	err := s.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", 1)
	require.ErrorIs(t, err, ErrCallerAnswerModeUnsupported,
		"operator_pick must be rejected, not silently auto-picked (review H2)")
	require.False(t, s.seq.Active(), "no session for an unsupported answer mode")
}

// --- M3: disarm and QSO-start cannot leave an active session after disarm ----

func TestService_StartDisarmRace(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)

	for i := 0; i < 100; i++ {
		require.NoError(t, s.ArmTx(true))

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, 1)
		}()
		go func() {
			defer wg.Done()
			_ = s.ArmTx(false)
		}()
		wg.Wait()

		// Invariant (review M3): once disarmed, no session may be left active.
		// ArmTx(false) ran, so TX is disarmed — the sequencer must be idle.
		s.txMu.Lock()
		armed := s.txArmed
		s.txMu.Unlock()
		require.False(t, armed, "ArmTx(false) ran; TX must be disarmed")
		require.False(t, s.seq.Active(), "active session left after disarm (review M3)")
	}
}

// AbandonQso must serialize through seqGate like the Start*/disarm paths, so it
// is atomic w.r.t. a start's armed-check → session-commit window. Without the
// gate, Abandon could slip into that window, find an idle sequencer + nil
// txCancel, do nothing, and let the start commit a fresh-generation session and
// transmit AFTER Abandon returned (High finding). Deterministic: hold seqGate as
// an in-progress start would and require AbandonQso to block behind it.
func TestAbandonQso_SerializesThroughSeqGate(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	defer func() { _ = s.ArmTx(false) }()

	s.seqGate.Lock() // stand in for a Start* holding the gate across its commit

	done := make(chan struct{})
	go func() {
		s.AbandonQso()
		close(done)
	}()

	// The gate is held, so AbandonQso must NOT complete. The old code took no
	// gate and would return immediately here.
	select {
	case <-done:
		s.seqGate.Unlock()
		t.Fatal("AbandonQso completed while seqGate was held — it does not serialize " +
			"against a concurrent Start*, so a start can transmit after Abandon returns")
	case <-time.After(50 * time.Millisecond):
	}

	s.seqGate.Unlock() // release: AbandonQso may now proceed
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("AbandonQso did not complete after seqGate was released")
	}
}

// --- M2: the scheduler skips slots serviced beyond the lateness budget -------

func TestSlotTooLate(t *testing.T) {
	target := time.Unix(60, 0).UTC()

	// On time / small delay (within the 2 s budget) → emit.
	require.False(t, slotTooLate(target, target))
	require.False(t, slotTooLate(target.Add(100*time.Millisecond), target))
	require.False(t, slotTooLate(target.Add(maxSlotLateness), target)) // exactly at the budget

	// Beyond the budget → skip (a large sub-slot delay AND a multi-slot delay).
	require.True(t, slotTooLate(target.Add(5*time.Second), target))
	require.True(t, slotTooLate(target.Add(SlotDuration), target))
	require.True(t, slotTooLate(target.Add(3*SlotDuration+2*time.Second), target))
}
