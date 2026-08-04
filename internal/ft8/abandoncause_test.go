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

// record returns the first log record whose message contains `msg`, decoded,
// and whether one was written at all. Used by every rule that asserts on what
// smd.log actually carries rather than on a builder having been called.
func (l *logSink) record(t *testing.T, msg string) (map[string]any, bool) {
	t.Helper()
	require.NoError(t, l.w.Flush())
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if !strings.Contains(line, msg) {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line is not JSON: %s", line)
		return rec, true
	}
	return nil, false
}

// abandonReason returns the `reason` field of the session-abandoned record, and
// whether such a record was written at all.
func (l *logSink) abandonReason(t *testing.T) (string, bool) {
	t.Helper()
	rec, ok := l.record(t, "session abandoned")
	if !ok {
		return "", false
	}
	reason, _ := rec["reason"].(string)
	return reason, true
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

// R8: a rung whose transmit failed must not end a session that REPLACED the one
// it belonged to.
//
// codex ea0c91a5 P1. `transmit` returning a terminal error is not instantaneous —
// the handler logs, then abandons — and ErrTxBadMessage comes from ENCODING
// (servicetx.go), so TX stays armed and the operator can abandon and start a
// fresh session inside that window. An unconditional teardown then kills the
// replacement and stamps the dead rung's failure on it as an end reason. This is
// the hazard CLAUDE.md invariant 5 names ("anything that retires a session must
// be generation-scoped … an unconditional abandon killed a valid session started
// on the new dial in the meantime") — the same trap, reached down a new path.
//
// Deterministic: beforeErr performs the operator's abandon-and-restart INSIDE the
// transmit call, which is exactly the window.
func TestAbandonCause_StaleTxFailureCannotEndAReplacementSession(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s)

	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	r.transmitErr = ErrTxBadMessage
	r.beforeErr = func() {
		r.beforeErr = nil // the replacement's own opening rung must not re-enter
		r.transmitErr = nil
		s.Abandon()
		// A DIFFERENT partner, so the surviving session is unmistakably the new one.
		_ = s.StartQso("G0XYZ", "IO91", "W1XYZ", "FN31", slot, 1500, 14.074, time.Unix(0, 0).UTC())
	}
	driveTheir(s, 30, []goft8.DecodedMessage{dm("G0XYZ K1ABC FN42", -12)})

	require.True(t, s.Active(),
		"the replacement session must survive a stale rung's failure")
	require.Equal(t, "W1XYZ", s.statusForTest().TheirCall,
		"and it must still be the REPLACEMENT, not a revived corpse")
	require.NotEqual(t, EndReasonTxBadMessage, r.lastStatus().EndReason,
		"a dead rung's failure must never be stamped on the session that replaced it")

	// The operator's own abandon of the ORIGINAL is what the log should carry.
	reason, found := sink.abandonReason(t)
	require.True(t, found)
	require.Equal(t, causeOperator, reason)
}

/*
   R21-R23 — the four ways a session ends are distinguishable, and the abandon
   line says WHICH CONTACT was lost.

   ft8-logging-gaps findings 2 and 3. `disarmTx` reached one `ft8 tx: disarmed`
   line from five causes, and the abandon beneath it carried no callsign. So the
   ONE automatic safety teardown SM performs — the linger expiry enforcing the
   open-view presence check, the only presence guarantee in the system — was
   byte-identical to an operator pressing a button, and neither said which
   station was being worked when it happened.

   The review's own instruction is the shape of these rules: "a test that
   asserts only 'a line was emitted' is weaker than the rule; assert that the
   two confusable states produce DISTINGUISHABLE output." So R21 pairs the two
   causes rather than checking one in isolation.
*/

func TestAbandonCause_R21_TheAutomaticTeardownIsDistinguishableFromAButtonPress(t *testing.T) {
	// The pair that matters: an operator disarm and the unattended safety stop.
	seen := map[string]string{}
	for _, cause := range []string{disarmOperator, disarmUnattended} {
		sink, log := newLogSink()
		r := &seqRecorder{}
		s := newTestSeqLogged(r, log)
		startedSession(t, s)

		s.abandonNamed("", cause)

		reason, found := sink.abandonReason(t)
		require.True(t, found, "%s wrote no record", cause)
		seen[cause] = reason
	}
	require.NotEqual(t, seen[disarmOperator], seen[disarmUnattended],
		"an operator disarm and the enforced presence stop must not read alike")
	require.Equal(t, disarmUnattended, seen[disarmUnattended],
		"the only automatic safety teardown must name itself")
}

func TestAbandonCause_R22_TheAbandonLineNamesTheContactThatWasLost(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s) // works K1ABC

	s.Abandon()

	rec, ok := sink.record(t, "session abandoned")
	require.True(t, ok)
	require.Equal(t, "K1ABC", rec["their_call"],
		"a session ended and the log cannot say which contact went with it")
}

func TestAbandonCause_R23_EveryDisarmCauseIsItsOwnValue(t *testing.T) {
	// Guards the enumeration against two causes being given the same string —
	// which would restore the defect while every individual rule still passed.
	all := []string{
		disarmOperator, disarmUnattended, disarmCatLost,
		disarmShutdown, disarmBandChange, disarmDialMoved,
	}
	seen := map[string]bool{}
	for _, c := range all {
		require.NotEmpty(t, c, "a blank cause is the defect this closes")
		require.False(t, seen[c], "duplicate disarm cause %q", c)
		seen[c] = true
	}
}
