package ft8

import (
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// --- H1: a completed QSO is logged only after the final rung TRANSMITS ------

// TestSequencer_FinalRungAsyncFailDoesNotLog: the 73 is queued (transmit returns
// nil) but the transmission then fails on air (onDone(false)). No QSO must be
// logged (review H1) — the session still ends.
func TestSequencer_FinalRungAsyncFailDoesNotLog(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)}) // 73 queued, fails on air

	require.Contains(t, r.sentMsgs(), "K1ABC G0XYZ 73", "the 73 was queued")
	require.Empty(t, r.completed, "a failed final 73 must NOT log a QSO (review H1)")
	// Follow-up M1: a failed final 73 stays active so the next slot retries it,
	// rather than silently ending the contact.
	require.True(t, s.Active(), "a failed final 73 stays active to retry")
}

// TestSequencer_FinalRungBadMessageAbandonsNoLog: a synchronous terminal error
// on the final rung (ErrTxBadMessage) must abandon and not log (review H1/M1).
func TestSequencer_FinalRungBadMessageAbandonsNoLog(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})

	r.mu.Lock()
	r.transmitErr = ErrTxBadMessage // the final 73 can't be sent
	r.mu.Unlock()
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

	require.Empty(t, r.completed, "a final rung that can't transmit must not log (review H1)")
	require.False(t, s.Active(), "ErrTxBadMessage is terminal (review M1)")
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

// A synchronous ErrTxInFlight on the final 73 must NOT end or log the QSO — the
// exchange stays in confirming so the next slot retries (review follow-up M1).
func TestSequencer_FinalRungInFlightRetries(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})

	r.mu.Lock()
	r.transmitErr = ErrTxInFlight // a prior TX is still running when the 73 fires
	r.mu.Unlock()
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
	require.Empty(t, r.completed, "ErrTxInFlight on the 73 must not log")
	require.True(t, s.Active(), "ErrTxInFlight on the final rung is retryable, not terminal")

	r.mu.Lock()
	r.transmitErr = nil // TX free now
	r.mu.Unlock()
	driveTheir(s, 120, nil) // next slot retries the 73
	require.Contains(t, r.sentMsgs(), "K1ABC G0XYZ 73")
	require.Len(t, r.completed, 1, "the retried 73 logs the QSO")
	require.False(t, s.Active())
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
	// A compound/portable call the standard encoder rejects.
	err := s.StartQso("G0XYZ", "IO91", "K1ABC/P", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC())
	require.ErrorIs(t, err, ErrTxBadMessage)
	require.False(t, s.Active(), "no session committed for an unencodable opening (review M1)")
	require.Empty(t, r.sentMsgs())
}

func TestCallerSequencer_StartCallCqRejectsUnencodableCq(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	err := s.StartCallCq("K1ABC/P", "FN42", 2700, 28.074, "auto_first", time.Unix(0, 0).UTC())
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

	err := s.StartCallCq("G0XYZ", "IO91", 1500, 14.074)
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
			_ = s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074)
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
