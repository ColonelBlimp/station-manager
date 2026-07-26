package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// Final-rung policy regressions — see finalrung.go for the group split.
//
// Before the split, EVERY ladder left a failed final rung in place and retried it
// forever: the repeat cap and the skip-if-silent off-ramp both sat behind
// `if !confirming`, and nothing else bounded the rung, so a contact whose closing
// message kept failing re-keyed the rig every cycle until the operator noticed.
// These pin both halves of the fix. Each uses asyncFail, so every transmission
// starts and then fails on air — the shape that produced the unbounded loop.
//
// The standard answer-a-CQ ladder (Group A) is covered in review_findings_test.go,
// where the H1 tests it refines already live.

// countMsg reports how many times want was transmitted.
func countMsg(sent []string, want string) int {
	n := 0
	for _, m := range sent {
		if m == want {
			n++
		}
	}
	return n
}

// --- GROUP B: retry under the cap, then abandon WITHOUT logging --------------

// Work-a-caller: our RR73 is what completes the ANSWERER's sequence, so a failure
// is retried — but bounded, and a roger they never received is not a QSO.
func TestWorkCaller_FinalRR73IsCappedAndDoesNotLog(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)
	s.maxRepeats = 2

	require.NoError(t, s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)}) // our report
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R-08", -11)}) // their roger → RR73 attempt 1
	require.True(t, s.Active(), "Group B retries a failed RR73 — the caller is waiting on it")

	driveTheir(s, 90, nil) // attempt 2 — the cap
	require.True(t, s.Active())

	driveTheir(s, 120, nil) // would be attempt 3
	require.False(t, s.Active(), "the final RR73 retry is bounded")
	require.Empty(t, r.completed, "they never got the roger, so neither side has a QSO")
	require.Equal(t, 2, countMsg(r.sentMsgs(), "K1ABC G0XYZ RR73"), "exactly maxRepeats attempts")
}

// Call CQ: the worst of the seven, because the contact only clears on success — an
// unbounded retry froze the whole pile-up loop on one station. At the cap the
// CONTACT is dropped (not the session) and CQ resumes.
func TestCallerSequencer_FinalRR73CapDropsContactAndResumesCq(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)
	s.maxRepeats = 2
	startCq(t, s)

	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})  // answers → we report
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)}) // roger → RR73 attempt 1
	driveTheir(s, 120, nil)                                                 // attempt 2 — the cap
	require.Equal(t, 2, countMsg(r.sentMsgs(), "DL9UW 7Q5MLV RR73"))

	driveTheir(s, 150, nil) // would be attempt 3
	require.Equal(t, 2, countMsg(r.sentMsgs(), "DL9UW 7Q5MLV RR73"), "no third attempt")
	require.Empty(t, r.completed, "a roger they never received is not a QSO")
	require.True(t, s.Active(), "the CQ session survives — only the contact is dropped")

	sent := r.sentMsgs()
	require.Equal(t, "CQ 7Q5MLV KH78", sent[len(sent)-1], "back to calling CQ")
	require.Contains(t, s.stalledCalls, "DL9UW",
		"parked for the round so the rescan can't walk straight back into the same wall")
}

// Field Day, ANSWERING side: FD inverts the standard roles — the answerer sends
// the closing RR73, so this is Group B where the standard answer ladder is Group A.
func TestFieldDay_AnswerFinalRR73IsCappedAndDoesNotLog(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)
	s.maxRepeats = 2

	require.NoError(t, s.StartQsoFd("G0XYZ", "1D", "DX", "K1ABC", "FN42", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ FD K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R 2A EMA", -12)}) // attempt 1
	require.True(t, s.Active(), "in FD the ANSWERING side sends the closing RR73 — Group B")

	driveTheir(s, 90, nil)  // attempt 2 — the cap
	driveTheir(s, 120, nil) // would be attempt 3
	require.False(t, s.Active())
	require.Empty(t, r.completed, "the CQ-FD station never got the roger")
	require.Equal(t, 2, countMsg(r.sentMsgs(), "K1ABC G0XYZ RR73"))
}

// Type-4 work-a-caller: the whole ladder is ONE terminal rung, so there was no
// pre-final rung carrying a cap to bypass — the bound had to be added outright.
func TestType4Work_FinalRR73IsCappedAndDoesNotLog(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)
	s.maxRepeats = 2

	require.NoError(t, s.StartWorkCallerT4("7Q5MLV", "PJ4/NA2AA", "", 3,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, []goft8.DecodedMessage{dm("<...> PJ4/NA2AA", 3)}) // attempt 1
	require.True(t, s.Active())
	driveTheir(s, 60, nil) // attempt 2 — the cap
	require.True(t, s.Active())

	driveTheir(s, 90, nil) // would be attempt 3
	require.False(t, s.Active(), "the sole terminal rung carries its own bound")
	require.Empty(t, r.completed)
	require.Equal(t, 2, countMsg(r.sentMsgs(), "PJ4/NA2AA 7Q5MLV RR73"))
}

// A Group A completion must be idempotent. Because it now completes on EITHER
// outcome, one contact can reach the callback twice — the transmit error branch
// fires it when the goroutine never started, and a still-running earlier
// transmission of the same rung fires its own later. Logging twice would mean a
// duplicate QSO row AND a duplicate upload to every forwarder.
func TestSequencer_Final73CompletionIsIdempotent(t *testing.T) {
	r := &seqRecorder{deferOnDone: true} // capture onDone instead of firing it
	s := newTestSeq(r)

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)}) // 73 queued, onDone held

	// Hold the callback itself so it can be invoked twice, as the two independent
	// completion routes would.
	r.mu.Lock()
	done := r.pendingOnDone
	r.mu.Unlock()
	require.NotNil(t, done, "the final rung wired a completion callback")

	done(false) // the error-branch completion
	require.Len(t, r.completed, 1, "the first callback logs the QSO")
	require.False(t, s.Active())

	done(true) // the in-flight transmission finishing afterwards
	require.Len(t, r.completed, 1, "a second completion for the same contact must not log again")
}

// --- GROUP A: send once, log on either outcome ------------------------------

// Field Day, WORK side: their RR73 is what advanced us here, so they are finished
// and our RR73 is the courtesy — the mirror of the answering side above.
func TestFieldDayWork_FinalRR73SendsOnceAndLogs(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)

	require.NoError(t, s.StartWorkCallerFd("G0XYZ", "1D", "DX", "K7IOC", "", "2A", "WWA", 2,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, nil) // our R+exchange
	require.Empty(t, r.completed, "nothing logs before the final rung is reached")

	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K7IOC RR73", -12)}) // their RR73 → our RR73 fails
	require.Len(t, r.completed, 1, "Group A: they already rogered, so the QSO logs anyway")
	require.Equal(t, "K7IOC", r.completed[0].TheirCall)
	require.False(t, s.Active(), "send-once ends the session")

	driveTheir(s, 90, nil)
	require.Equal(t, 1, countMsg(r.sentMsgs(), "K7IOC G0XYZ RR73"), "never retried")
	require.Len(t, r.completed, 1, "and never logged twice")
}

// Type-4 answer: their RR73 advanced us to the closing 73, so they are finished.
func TestType4Answer_Final73SendsOnceAndLogs(t *testing.T) {
	r := &seqRecorder{asyncFail: true}
	s := newTestSeq(r)

	require.NoError(t, s.StartQsoT4("7Q5MLV", "PJ4/NA2AA", "", -12,
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ PJ4/NA2AA", -1)}) // our bare-call opening
	require.Empty(t, r.completed, "nothing logs before the final rung is reached")

	driveTheir(s, 60, []goft8.DecodedMessage{dm("<...> PJ4/NA2AA RR73", -11)}) // their roger → our 73 fails
	require.Len(t, r.completed, 1, "Group A: they already rogered, so the QSO logs anyway")
	require.Equal(t, "PJ4/NA2AA", r.completed[0].TheirCall)
	require.False(t, s.Active(), "send-once ends the session")

	driveTheir(s, 90, nil)
	require.Equal(t, 1, countMsg(r.sentMsgs(), "PJ4/NA2AA 7Q5MLV 73"), "never retried")
	require.Len(t, r.completed, 1, "and never logged twice")
}
