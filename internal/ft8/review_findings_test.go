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
	require.False(t, s.Active())
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

// --- M2: the scheduler skips slots whose boundary it was delayed past --------

func TestStaleTarget(t *testing.T) {
	target := time.Unix(60, 0).UTC()

	// On time / still within the slot that started at target → emit (not stale).
	require.False(t, staleTarget(target, target))
	require.False(t, staleTarget(target.Add(5*time.Second), target))
	require.False(t, staleTarget(target.Add(SlotDuration-time.Millisecond), target))

	// Delayed to/past the next boundary → stale (one slot and many slots late).
	require.True(t, staleTarget(target.Add(SlotDuration), target))
	require.True(t, staleTarget(target.Add(3*SlotDuration+2*time.Second), target))
}
