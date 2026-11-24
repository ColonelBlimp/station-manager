package ft8

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// The DXpedition combined message (operator's report, 2026-11-24; dogfood inbox).
// A station working several callers at once sends one line with two meanings:
//
//	<call1> RR73; <call2> <SENDER> <report>
//
// a roger to call1 and a report to call2, with its own call hashed (the decoder
// prints it in angle brackets when it has resolved the hash, "<...>" when not).
// The daemon read neither half, so a contact the DX station had rogered and
// logged was abandoned here as "no answer" and never logged.
//
// All calls here are synthetic.

const (
	cmbUs    = "G0XYZ"
	cmbDx    = "K1ABC"
	cmbOther = "W9XYZ"
)

func TestParseCombined_TheWholeShapeOrNothing(t *testing.T) {
	c, ok := parseCombined("G0XYZ RR73; W9XYZ <K1ABC> -08")
	require.True(t, ok)
	require.Equal(t, "K1ABC", c.sender)
	require.Equal(t, message{kind: msgRoger, to: "G0XYZ", from: "K1ABC", rogerSignsOff: true}, c.roger)
	require.Equal(t, message{kind: msgReport, to: "W9XYZ", from: "K1ABC", report: -8}, c.report)

	// As decoded text arrives: any case, any spacing.
	c, ok = parseCombined("  g0xyz rr73;   w9xyz <k1abc> +04 ")
	require.True(t, ok)
	require.Equal(t, 4, c.report.report)

	// A compound sender, and a recipient the decoder gives hashed, are still this
	// message: the other half may be for somebody whose call we never resolved, and
	// that must not cost us our own half. A hashed recipient is the decoder's
	// unresolved placeholder or a call in angle brackets, in either position; it is
	// kept as printed, so it is never "us".
	c, ok = parseCombined("G0XYZ RR73; <...> <VP8/K1ABC> -08")
	require.True(t, ok)
	require.Equal(t, "VP8/K1ABC", c.sender)
	require.Equal(t, "G0XYZ", c.roger.to)
	require.Equal(t, "<...>", c.report.to)
	for _, text := range []string{
		"<...> RR73; G0XYZ <K1ABC> -08",
		"<PJ4/W9XYZ> RR73; G0XYZ <K1ABC> -08",
		"G0XYZ RR73; <PJ4/W9XYZ> <K1ABC> -08",
		"<...> RR73; <...> <K1ABC> -08",
	} {
		_, ok := parseCombined(text)
		require.True(t, ok, "%q is a combined message", text)
	}
	require.Equal(t, message{}, directedTo("<G0XYZ> RR73; W9XYZ <K1ABC> -08", cmbUs),
		"a hashed recipient was matched to us")

	for name, text := range map[string]string{
		"the sender is not resolved":          "G0XYZ RR73; W9XYZ <...> -08",
		"the sender is not in angle brackets": "G0XYZ RR73; W9XYZ K1ABC -08",
		"the sender is not a call":            "G0XYZ RR73; W9XYZ <HELLO> -08",
		"no semicolon: a plain roger":         "G0XYZ RR73 W9XYZ <K1ABC> -08",
		"RRR is not this message":             "G0XYZ RRR; W9XYZ <K1ABC> -08",
		"no report":                           "G0XYZ RR73; W9XYZ <K1ABC>",
		"not a report":                        "G0XYZ RR73; W9XYZ <K1ABC> FN42",
		"an R-report":                         "G0XYZ RR73; W9XYZ <K1ABC> R-08",
		"a word too many":                     "G0XYZ RR73; W9XYZ <K1ABC> -08 73",
		"the first recipient is not a call":   "HELLO RR73; W9XYZ <K1ABC> -08",
		"the second recipient is not a call":  "G0XYZ RR73; HELLO <K1ABC> -08",
		"the same recipient twice":            "G0XYZ RR73; G0XYZ <K1ABC> -08",
		// Angle brackets do not make a recipient: what is inside must be the
		// decoder's placeholder or a call (codex-243).
		"a bracketed word as the second recipient": "G0XYZ RR73; <HELLO> <K1ABC> -08",
		"bracketed noise as the second recipient":  "G0XYZ RR73; <!!!> <K1ABC> -08",
		"a bracketed word as the first recipient":  "<HELLO> RR73; G0XYZ <K1ABC> -08",
		"empty brackets as a recipient":            "G0XYZ RR73; <> <K1ABC> -08",
		"a plain roger":                            "G0XYZ K1ABC RR73",
		"a plain report":                           "G0XYZ K1ABC -08",
		"a CQ":                                     "CQ K1ABC FN42",
		"nothing":                                  "",
	} {
		_, ok := parseCombined(text)
		require.False(t, ok, "%s: %q was taken for a combined message", name, text)
	}
}

// parseMessage itself still reads a combined line as nothing: its many callers
// keep their meaning, and only the readers below are taught the two halves.
func TestParseMessage_ACombinedLineIsStillNothingToIt(t *testing.T) {
	require.Equal(t, message{}, parseMessage("G0XYZ RR73; W9XYZ <K1ABC> -08"))
}

func TestDirectedTo_OnlyTheHalfAddressedToUs(t *testing.T) {
	roger := directedTo("G0XYZ RR73; W9XYZ <K1ABC> -08", cmbUs)
	require.Equal(t, message{kind: msgRoger, to: cmbUs, from: cmbDx, rogerSignsOff: true}, roger)

	report := directedTo("W9XYZ RR73; G0XYZ <K1ABC> -08", cmbUs)
	require.Equal(t, message{kind: msgReport, to: cmbUs, from: cmbDx, report: -8}, report)

	// Neither half is ours: nothing, and never the first half by default.
	require.Equal(t, message{}, directedTo("W9XYZ RR73; N0CALL <K1ABC> -08", cmbUs))
	// Not a valid combined line: nothing.
	require.Equal(t, message{}, directedTo("G0XYZ RR73; W9XYZ <...> -08", cmbUs))
	require.Equal(t, message{}, directedTo("G0XYZ RR73; G0XYZ <K1ABC> -08", cmbUs))
	// A plain line is read exactly as before.
	for _, plain := range []string{"G0XYZ K1ABC RR73", "G0XYZ K1ABC -08", "CQ K1ABC FN42", "K1ABC W9XYZ FN42", "TNX 73 GL"} {
		require.Equal(t, parseMessage(plain), directedTo(plain, cmbUs), plain)
	}
}

// Each half means exactly what its plain form means, on every rung of both
// ladders: whatever the plain line does to an exchange, the combined line does.
func TestCombined_EachHalfIsExactlyItsPlainForm(t *testing.T) {
	const (
		plainRoger     = "G0XYZ K1ABC RR73"
		combinedRoger  = "G0XYZ RR73; W9XYZ <K1ABC> -03"
		plainReport    = "G0XYZ K1ABC -08"
		combinedReport = "W9XYZ RR73; G0XYZ <K1ABC> -08"
	)
	for _, state := range []txState{txCalling, txReporting, txConfirming, txDone} {
		e := Exchange{OurCall: cmbUs, OurGrid: "IO91", TheirCall: cmbDx, State: state}
		for _, pair := range [][2]string{{plainRoger, combinedRoger}, {plainReport, combinedReport}} {
			wantEx, wantOK := e.Advance(pair[0], -12)
			gotEx, gotOK := e.Advance(pair[1], -12)
			require.Equal(t, wantOK, gotOK, "answer ladder, rung %s, %q", state.label(), pair[1])
			require.Equal(t, wantEx, gotEx, "answer ladder, rung %s, %q", state.label(), pair[1])
		}
	}
	for _, state := range []callerState{cqReporting, cqRogering, cqDone} {
		c := NewCallerExchange(cmbUs, cmbDx, "FN42", -12)
		c.State = state
		for _, pair := range [][2]string{{plainRoger, combinedRoger}, {plainReport, combinedReport}} {
			wantEx, wantOK := c.Advance(pair[0])
			gotEx, gotOK := c.Advance(pair[1])
			require.Equal(t, wantOK, gotOK, "caller ladder, rung %d, %q", state, pair[1])
			require.Equal(t, wantEx, gotEx, "caller ladder, rung %d, %q", state, pair[1])
		}
	}
}

// And the fixture above is not vacuous: on the rungs that wait for them, the
// combined halves do advance.
func TestCombined_TheHalvesAdvanceTheAnswerLadder(t *testing.T) {
	e := Exchange{OurCall: cmbUs, OurGrid: "IO91", TheirCall: cmbDx, State: txCalling}

	e, ok := e.Advance("W9XYZ RR73; G0XYZ <K1ABC> -08", -12)
	require.True(t, ok, "the report half did not advance the call")
	require.Equal(t, txReporting, e.State)
	require.Equal(t, -8, e.RcvdReport)
	require.Equal(t, -12, e.SendSnr)

	e, ok = e.Advance("G0XYZ RR73; W9XYZ <K1ABC> -03", -11)
	require.True(t, ok, "the roger half did not advance the report")
	require.Equal(t, txConfirming, e.State)
}

// Attribution: only the station being worked, and only when the decoder
// resolved its call. Nothing else advances anything.
func TestCombined_OnlyTheStationBeingWorkedAndOnlyWhenResolved(t *testing.T) {
	calling := Exchange{OurCall: cmbUs, TheirCall: cmbDx, State: txCalling}
	reporting := Exchange{OurCall: cmbUs, TheirCall: cmbDx, State: txReporting}
	for name, text := range map[string]string{
		"another station's roger to us":  "G0XYZ RR73; W9XYZ <N0CALL> -08",
		"an unresolved sender's roger":   "G0XYZ RR73; W9XYZ <...> -08",
		"the roger is for somebody else": "W9XYZ RR73; N0CALL <K1ABC> -08",
		"our call in both halves":        "G0XYZ RR73; G0XYZ <K1ABC> -08",
	} {
		_, ok := reporting.Advance(text, -12)
		require.False(t, ok, "%s advanced the report rung: %q", name, text)
	}
	for name, text := range map[string]string{
		"another station's report to us": "W9XYZ RR73; G0XYZ <N0CALL> -08",
		"an unresolved sender's report":  "W9XYZ RR73; G0XYZ <...> -08",
	} {
		_, ok := calling.Advance(text, -12)
		require.False(t, ok, "%s advanced the calling rung: %q", name, text)
	}
}

// The morning of 2026-11-24, in order, with synthetic calls: the report arrives
// combined, then plain; the roger arrives combined. One contact, logged once, at
// the frequency the session was pinned to.
func TestSequencer_ACombinedRogerCompletesAndLogsTheContact(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso(cmbUs, "IO91", cmbDx, "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.090, time.Unix(0, 0).UTC()))

	// Their report to us, inside a roger to somebody else.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("W9XYZ RR73; G0XYZ <K1ABC> -06", -12)})
	// They repeat it plain (they had not yet heard our roger).
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -06", -12)})
	// Their roger to us, inside a report to the next station.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ RR73; N0CALL <K1ABC> -08", -12)})

	require.Equal(t, []string{
		"K1ABC G0XYZ R-12", // on the combined report
		"K1ABC G0XYZ R-12", // again: their plain repeat changes nothing
		"K1ABC G0XYZ 73",   // on the combined roger
	}, r.sentMsgs())
	require.False(t, s.Active(), "the contact should be complete after the 73")
	require.Len(t, r.completed, 1, "the rogered contact was not logged exactly once")
	q := r.completed[0]
	require.Equal(t, cmbDx, q.TheirCall)
	require.Equal(t, -6, q.TheirReport)
	require.Equal(t, -12, q.OurReport)
	require.Equal(t, 14.090, q.DialFreqMHz)
	require.Equal(t, 1500.0, q.OffsetHz)

	// The roger sent again (they did not hear our 73) logs nothing more.
	driveTheir(s, 120, []goft8.DecodedMessage{dm("G0XYZ RR73; N0CALL <K1ABC> -08", -12)})
	require.Len(t, r.completed, 1, "a repeated roger logged the contact again")
}

// Once the roger is taken in, the contact is earned: it logs though our 73 never
// reaches the air (Group A; invariant 1), exactly as for a plain roger.
func TestSequencer_ACombinedRogerEarnsTheContactThoughThe73IsRefused(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso(cmbUs, "IO91", cmbDx, "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.090, time.Unix(0, 0).UTC()))
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -06", -12)})

	r.mu.Lock()
	r.transmitErr = ErrTxInFlight // the 73 is refused
	r.mu.Unlock()
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ RR73; N0CALL <K1ABC> -08", -12)})

	require.Len(t, r.completed, 1, "a contact rogered by a combined message was not logged when the 73 was refused")
	require.Equal(t, 14.090, r.completed[0].DialFreqMHz)
	require.False(t, s.Active())
}

// A combined line is never a caller: it adds nobody to the list a run works from.
func TestCollectAnswerers_ACombinedLineIsNeverACaller(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	listedCaller(t, s, r) // a pick run; DL9UW is listed
	before := len(r.lastStatus().Answerers)

	driveTheir(s, 120, []goft8.DecodedMessage{
		dm("G0XYZ RR73; W9XYZ <K1ABC> -08", -9),
		dm("W9XYZ RR73; G0XYZ <K1ABC> -08", -9),
	})
	st := r.lastStatus()
	require.Len(t, st.Answerers, before, "a combined line was listed as a caller")
	for _, a := range st.Answerers {
		require.NotEqual(t, cmbDx, a.Call)
	}
	require.Nil(t, s.caller, "a combined line started a contact")
}

// Field Day reads no report exchange out of a combined line.
func TestFieldDay_ACombinedLineChangesNothing(t *testing.T) {
	for _, text := range []string{"G0XYZ RR73; W9XYZ <K1ABC> -08", "W9XYZ RR73; G0XYZ <K1ABC> -08"} {
		for _, state := range []fdState{fdCalling, fdRogering} {
			e := FdExchange{OurCall: cmbUs, TheirCall: cmbDx, State: state}
			got, ok := e.Advance(text)
			require.False(t, ok, "Field Day, rung %d, advanced on %q", state, text)
			require.Equal(t, e, got)
		}
	}
}

// Who transmitted a combined line is its resolved sender: for a reception spot
// (no grid: the line carries none), and for dropping our own transmissions.
func TestCombined_TheTransmitterIsTheResolvedSender(t *testing.T) {
	call, grid, ok := SpotFrom("G0XYZ RR73; W9XYZ <K1ABC> -08")
	require.True(t, ok)
	require.Equal(t, "K1ABC", call)
	require.Equal(t, "", grid)

	_, _, ok = SpotFrom("G0XYZ RR73; W9XYZ <...> -08")
	require.False(t, ok, "an unresolved sender was spotted")

	msgs := []goft8.DecodedMessage{
		dm("W9XYZ RR73; N0CALL <G0XYZ> -08", -1), // our own, heard back
		dm("G0XYZ RR73; W9XYZ <K1ABC> -08", -9),  // theirs, to us
		dm("W9XYZ RR73; N0CALL <...> -08", -9),   // nobody's, as far as we know
	}
	kept := dropOwnTransmissions(msgs, cmbUs)
	require.Len(t, kept, 2, "our own combined line was not dropped")
	require.Equal(t, "G0XYZ RR73; W9XYZ <K1ABC> -08", kept[0].Text)
}

// The diagnostic line says what the ladder acted on: a combined line that
// advances the exchange is named as heard from the station being worked. (Before
// the fix that line was absent for exactly the slot that mattered.)
func TestSequencer_ACombinedLineIsNamedInTheDiagnosticLine(t *testing.T) {
	sink, log := newLogSink()
	r := &seqRecorder{}
	s := newTestSeqLogged(r, log)
	startedSession(t, s) // G0XYZ answering K1ABC

	const line = "W9XYZ RR73; G0XYZ <K1ABC> -06"
	driveTheir(s, 60, []goft8.DecodedMessage{dm(line, -12)})

	rec, found := sink.record(t, "ft8 seq: decode from worked station")
	require.True(t, found, "a combined line from the station being worked was not reported as heard")
	require.Equal(t, line, rec["from_worked"])
	require.Equal(t, true, rec["advanced"])
}

// reportedHeard says whether the log carries the diagnostic record `msg` naming
// `line` as heard from the station being worked.
func reportedHeard(t *testing.T, sink *logSink, msg, line string) bool {
	t.Helper()
	require.NoError(t, sink.w.Flush())
	for _, raw := range strings.Split(sink.buf.String(), "\n") {
		if !strings.Contains(raw, msg) {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &rec))
		if rec["from_worked"] == line {
			return true
		}
	}
	return false
}

// The same for the two ladders on the side that is called: the Call-CQ run and
// working a caller. Their diagnostic lines name a combined line they acted on.
func TestCallerLadders_ACombinedLineIsNamedInTheirDiagnosticLines(t *testing.T) {
	t.Run("call cq", func(t *testing.T) {
		sink, log := newLogSink()
		r := &seqRecorder{}
		s := newTestSeqLogged(r, log)
		startCq(t, s)
		driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})

		const line = "7Q5MLV RR73; W9XYZ <DL9UW> -05"
		driveTheir(s, 90, []goft8.DecodedMessage{dm(line, -10)})
		require.True(t, reportedHeard(t, sink, "ft8 seq: caller decode from worked station", line),
			"the Call-CQ ladder did not report the combined line as heard")
		require.Len(t, r.completed, 1, "fixture: the combined roger completed the contact")
	})
	t.Run("working a caller", func(t *testing.T) {
		sink, log := newLogSink()
		r := &seqRecorder{}
		s := newTestSeqLogged(r, log)
		require.NoError(t, s.StartWorkCaller(cmbUs, cmbDx, "FN42", -12,
			time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

		const line = "G0XYZ RR73; W9XYZ <K1ABC> -05"
		driveTheir(s, 30, []goft8.DecodedMessage{dm(line, -10)})
		require.True(t, reportedHeard(t, sink, "ft8 seq: working caller — decode from worked station", line),
			"the work ladder did not report the combined line as heard")
	})
}

// The hold after our RR73 reads a combined line with the same reader as the
// ladder. The case where reading it matters (a line that goes unread looks just
// like silence, which also releases the hold): the partner's roger was a BARE
// RR73, so an RR73 from them again is a plea, not a sign-off, and our RR73 must
// go out once more (finalrung_test.go, "still resends when the roger was
// bare"). It is the same plea when it arrives inside a combined message.
func TestCallerSequencer_ConfirmHoldReadsACombinedRR73AsItsPlainForm(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	startCq(t, s)

	driveTheir(s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
	driveTheir(s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW RR73", -10)}) // a bare roger
	require.Len(t, r.completed, 1)
	require.NotNil(t, s.confirmHold)
	require.False(t, s.confirmHold.rogeredWithReport)

	before := countMsg(r.sentMsgs(), "DL9UW 7Q5MLV RR73")
	driveTheir(s, 120, []goft8.DecodedMessage{dm("7Q5MLV RR73; W9XYZ <DL9UW> -05", -10)})
	sent := r.sentMsgs()
	require.Equal(t, "DL9UW 7Q5MLV RR73", sent[len(sent)-1],
		"a combined RR73 from a partner still asking was taken for silence")
	require.Equal(t, before+1, countMsg(sent, "DL9UW 7Q5MLV RR73"))
	require.NotNil(t, s.confirmHold, "the hold was released on a plea")
	require.Len(t, r.completed, 1, "the contact was logged again")
}

// What reaches the logging sink is the same value whether the partner's report
// and roger came plain or combined: every field of the completed contact. So
// whatever is shown downstream of the sink for a plain roger (the stored row and
// its upload row, cmd/smd's W-0028 tests; the ft8-logged frame) holds for a
// combined one. No single test crosses from a decode to a row: the sink lives in
// cmd/smd, and nothing exported feeds a decode to a Service.
func TestCombined_TheSinkIsHandedExactlyWhatAPlainExchangeHandsIt(t *testing.T) {
	run := func(report, roger string) CompletedQso {
		r := &seqRecorder{}
		s := newTestSeq(r)
		require.NoError(t, s.StartQso(cmbUs, "IO91", cmbDx, "FN42",
			time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.090, time.Unix(0, 0).UTC()))
		driveTheir(s, 30, []goft8.DecodedMessage{dm(report, -12)})
		driveTheir(s, 60, []goft8.DecodedMessage{dm(roger, -11)})
		require.Len(t, r.completed, 1, "%q then %q did not complete one contact", report, roger)
		return r.completed[0]
	}
	plain := run("G0XYZ K1ABC -06", "G0XYZ K1ABC RR73")
	require.Equal(t, plain, run("W9XYZ RR73; G0XYZ <K1ABC> -06", "G0XYZ RR73; N0CALL <K1ABC> -08"))
	require.Equal(t, plain, run("G0XYZ K1ABC -06", "G0XYZ RR73; N0CALL <K1ABC> -08"))
	require.Equal(t, plain, run("W9XYZ RR73; G0XYZ <K1ABC> -06", "G0XYZ K1ABC RR73"))
	require.Equal(t, 14.090, plain.DialFreqMHz)
	require.Equal(t, cmbDx, plain.TheirCall)
}

// Through the Service's own transmission and completion (the armed check, the
// single flight, the controller's key and play, the sequencer's callback), with
// the fake keyer and player: a combined roger puts our 73 on the air and hands
// the logging sink one contact, on the frequency and in the logbook the exchange
// was started with. A repeat of that roger hands it nothing more.
func TestService_ACombinedRogerHandsTheLoggingSinkOneContact(t *testing.T) {
	f := newReplacedContact(t)
	f.toTheirReport(t) // G0XYZ answering K1ABC, our R-report sent

	f.their(90, "G0XYZ RR73; W9XYZ <K1ABC> -05", -11)
	// The roger put our 73 on the air: keyed and playing, as for a plain roger.
	require.Eventually(t, func() bool { return f.keyer.keys() > 0 && f.player.plays() > 0 },
		2*time.Second, 5*time.Millisecond, "the combined roger did not put our 73 on the air")
	require.Contains(t, f.sentNow(), closingRung)
	require.Empty(t, f.loggedNow(), "the fixture: the 73 has not finished, so nothing is logged yet")
	close(f.player.done) // the 73 plays to its end
	require.Eventually(t, func() bool { return len(f.loggedNow()) == 1 }, 2*time.Second, 5*time.Millisecond,
		"a contact rogered by a combined message never reached the logging sink")
	logged := f.loggedNow()[0]
	require.Equal(t, "K1ABC", logged.TheirCall)
	require.InDelta(t, 14.074, logged.DialFreqMHz, 1e-9, "on the frequency of the exchange")
	require.Equal(t, int64(1), logged.LogbookID)
	require.False(t, f.s.seq.Active())

	f.their(120, "G0XYZ RR73; W9XYZ <K1ABC> -05", -11)
	f.their(150, "G0XYZ K1ABC RR73", -11)
	require.Never(t, func() bool { return len(f.loggedNow()) != 1 }, 100*time.Millisecond, 10*time.Millisecond,
		"a repeated roger handed the sink the contact again")
}

// The type-4 ladder has its own parser and its own attribution rules (ADR 0048),
// and reads nothing out of a combined line.
func TestType4_ACombinedLineChangesNothing(t *testing.T) {
	for _, text := range []string{
		"7Q5MLV RR73; W9XYZ <PJ4/NA2AA> -08",
		"W9XYZ RR73; 7Q5MLV <PJ4/NA2AA> -08",
		"<...> RR73; W9XYZ <PJ4/NA2AA> -08",
	} {
		require.Equal(t, type4msg{}, parseType4(text), "the type-4 parser read %q", text)
		for _, state := range []t4State{t4Calling, t4Confirming, t4Done} {
			e := T4Exchange{OurCall: "7Q5MLV", TheirCall: "PJ4/NA2AA", State: state}
			got, ok := e.Advance(text)
			require.False(t, ok, "type 4, rung %d, advanced on %q", state, text)
			require.Equal(t, e, got)
		}
	}
}
