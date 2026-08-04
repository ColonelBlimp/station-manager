package ft8

/*
   Why a session ended — acceptance criteria.

   THE DEFECT (dogfood 2026-08-04, a 50-QSO FT8 run). Seven of eight
   "ft8 seq: session abandoned" records carried `reason: ""`. That is not one
   missing label: `Abandon()` is reached from TWELVE places, and only the two
   dial paths ever staged a reason. So a session that DIED — SM could no longer
   key the rig, or the rung's message will never encode — logged identically to
   one the operator deliberately stopped. Reading the log back, the two are
   indistinguishable, which is the whole reason the field exists.

   THE THREE FAMILIES, and why they are not treated alike:
     · The operator pressed Abandon (Service.AbandonQso).
     · The operator disarmed TX, and a session was still running (disarmTx ×2).
     · A rung could not transmit — ErrTxNotArmed or ErrTxBadMessage, at eight
       sites across the five ladders.
   Only the third is NOT the operator's own doing, and CLAUDE.md invariant 5 is
   explicit that the operator must be able to see a session end and why WHENEVER
   they did not cause it ("a safety stop nobody can see is indistinguishable
   from a hang"). Those eight therefore get a frame reason as well as a log one.
   The first two stay ABSENT from the frame — the const block's standing rule is
   "absent for a NORMAL end … because the operator caused it", and toasting a
   disarm would tell the operator what they just did. Operator's ruling
   2026-08-04, choosing this over log-only and over toasting everything.

   SO THE TWO CARRIERS DIVERGE DELIBERATELY, and that is the thing to get right:
   the LOG must never be blank (R5), while the FRAME must stay blank for an
   operator-caused end (R3, R4). A fallback that leaked into the frame would
   turn every Abandon into a toast; a frame reason that failed to reach the log
   would leave the original defect in place. R6 pins that a staged reason still
   wins over the call site's fallback — the idiom AbandonIfCurrent already uses,
   reused here rather than invented twice.

   THE NEAREST CONFUSABLE STATES:
     · R1/R2 vs R3 — "SM could not key the rig" vs "the operator stopped it".
       The entire point. R1 and R2 also separate the two KEYING failures from
       each other, because they lead to different operator actions: TX went
       un-armed (re-arm) versus a message that will never encode (a compound
       callsign SM cannot answer).
     · R4 — the operator pressing Abandon vs disarming TX. Both are theirs, both
       legitimately silent on the frame, and they were previously the same empty
       string in the log.

   NOT COVERED HERE, stated rather than left implied: that the two disarmTx call
   sites pass causeTxDisarmed is verified by reading, not by a test — driving it
   needs a whole Service. R4 pins the mechanism they use.
*/

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// logSink captures real log records — logging.NewForWriter, the same seam
// internal/bridge/drivewatch_test.go uses. Asserting on the JSON a record
// actually carries is the observable; a faked Logger would only prove the
// builder was called.
type logSink struct {
	buf *bytes.Buffer
	w   *bufio.Writer
}

func newLogSink() (*logSink, logging.Logger) {
	buf := &bytes.Buffer{}
	w := bufio.NewWriter(buf)
	return &logSink{buf: buf, w: w}, logging.NewForWriter(w)
}

// abandonReason returns the `reason` field of the session-abandoned record, and
// whether such a record was written at all.
func (l *logSink) abandonReason(t *testing.T) (string, bool) {
	t.Helper()
	require.NoError(t, l.w.Flush())
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if !strings.Contains(line, "session abandoned") {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line is not JSON: %s", line)
		reason, _ := rec["reason"].(string)
		return reason, true
	}
	return "", false
}

// startedSession puts the sequencer into an active answer-a-CQ session whose next
// rung will attempt to transmit. Mirrors the fixture the other sequencer tests use.
func startedSession(t *testing.T, s *Sequencer) {
	t.Helper()
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))
	require.True(t, s.Active(), "fixture must actually start a session")
}

func TestAbandonCause_TxNotArmedIsNamedAndVisible(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s)

	// R1: the rig can no longer be keyed. Not the operator's doing, so it must be
	// named in the log AND explained on the frame.
	r.transmitErr = ErrTxNotArmed
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})

	reason, found := sink.abandonReason(t)
	require.True(t, found, "a session that died must say so")
	require.Equal(t, EndReasonTxNotArmed, reason)
	require.Equal(t, EndReasonTxNotArmed, r.lastStatus().EndReason,
		"invariant 5: an end the operator did not cause must be visible to them")
}

func TestAbandonCause_BadMessageIsDistinctFromTxGone(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s)

	// R2: a different failure needing a different operator response — a message
	// that will NEVER encode, versus TX that merely went un-armed.
	r.transmitErr = ErrTxBadMessage
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})

	reason, _ := sink.abandonReason(t)
	require.Equal(t, EndReasonTxBadMessage, reason)
	require.NotEqual(t, EndReasonTxNotArmed, reason, "the two keying failures must not collapse")
	require.Equal(t, EndReasonTxBadMessage, r.lastStatus().EndReason)
}

func TestAbandonCause_OperatorAbandonNamedInLogButSilentOnTheFrame(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s)

	s.Abandon()

	// R3: the log stops being blank …
	reason, found := sink.abandonReason(t)
	require.True(t, found)
	require.Equal(t, causeOperator, reason, "the log must distinguish this from a session that died")
	// … while the frame stays silent, because the operator already knows.
	require.Empty(t, r.lastStatus().EndReason,
		"a fallback that reached the frame would toast the operator about their own click")
}

func TestAbandonCause_DisarmIsDistinctFromAbandon(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s)

	// R4: both are the operator's own doing, both silent on the frame — and they
	// were the SAME empty string in the log before this.
	s.abandonNamed("", causeTxDisarmed)

	reason, _ := sink.abandonReason(t)
	require.Equal(t, causeTxDisarmed, reason)
	require.NotEqual(t, causeOperator, reason)
	require.Empty(t, r.lastStatus().EndReason)
}

func TestAbandonCause_LogLineIsNeverBlank(t *testing.T) {
	// R5: the rule the defect violated, stated directly over every cause a call
	// site can supply plus the staged ones.
	for _, cause := range []string{
		causeOperator, causeTxDisarmed,
		EndReasonTxNotArmed, EndReasonTxBadMessage,
		EndReasonDialMoved, EndReasonDialUnknown, EndReasonBandChange,
	} {
		sink, log := newLogSink()
		r := &seqRecorder{}
		s := newTestSeqLogged(r, log)
		startedSession(t, s)

		s.abandonNamed("", cause)

		reason, found := sink.abandonReason(t)
		require.True(t, found, "cause %q wrote no record", cause)
		require.NotEmpty(t, reason, "cause %q logged blank", cause)
	}
}

func TestAbandonCause_StagedReasonWinsOverTheCallSiteFallback(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s)

	// R6: the dial guard stages its reason and THEN the teardown runs through a
	// path with its own fallback. The specific explanation must survive — losing
	// it would report a dial-guard stop as a routine disarm. Mirrors the rule
	// AbandonIfCurrent already applies.
	s.setPendingEndReason(EndReasonDialMoved)
	s.abandonNamed("", causeTxDisarmed)

	reason, _ := sink.abandonReason(t)
	require.Equal(t, EndReasonDialMoved, reason)
	require.Equal(t, EndReasonDialMoved, r.lastStatus().EndReason,
		"a staged reason is operator-visible; the fallback it beat is not")
}

// R7: a terminal transmit failure must NOT go through the shared staging slot.
//
// codex 3531e1ed P2, and a regression I introduced. The eight rung sites staged
// their reason under one lock hold and abandoned under a second, so a teardown
// landing between the two consumed it — the operator's own Abandon reported "SM
// could not transmit". The same shared slot is what the DIAL GUARD stages into,
// which gives the defect a deterministic and worse face: a rung failure racing a
// dial-guard teardown OVERWRITES the guard's explanation, and a safety stop is
// then reported as a transmit failure. R6 says a staged reason wins; this says
// the rung must not be staging in the first place.
func TestAbandonCause_TxFailureDoesNotClobberAStagedReason(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s)

	// The dial guard has staged its reason; the rung then fails on the same session.
	s.setPendingEndReason(EndReasonDialMoved)
	r.transmitErr = ErrTxNotArmed
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})

	reason, found := sink.abandonReason(t)
	require.True(t, found)
	require.Equal(t, EndReasonDialMoved, reason,
		"the dial guard's explanation must survive a rung failure, not be overwritten by it")
	require.Equal(t, EndReasonDialMoved, r.lastStatus().EndReason,
		"a safety stop reported as a transmit failure sends the operator after the wrong fault")
}
