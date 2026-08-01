package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Drive-watch STATE REPORTING — follow-up to the 2026-08-01 logging audit (B1).
//
// THE PROBLEM, stated from the operator's seat rather than the code's: the drive
// detector has three outcomes per transmission and reports only one of them. It
// arms, or it declines because the instrument is silent, or it declines because
// the meter is not on PO — and the two declines are silent early returns. So a
// station can transmit for an entire session with no transmitter-failure
// detection running at all, and nothing anywhere says so. Worse, silence from a
// declined watch is indistinguishable from silence from a healthy one, which is
// the shape the whole meter arc kept failing on.
//
// It cost real time on 2026-08-01: two drive alarms at 08:45 could be shown
// almost certainly false only by reconstructing evidence from a DIFFERENT log
// record, because the alarm line carries `code` and nothing else.
//
// ACCEPTANCE CRITERION (operator, 2026-08-01), given as a transition
// specification rather than a per-transmission one — deliberately, and the
// reasoning is his: this is a safety-monitoring DEGRADATION, not a confirmed
// transmitter failure, so it belongs at Warn and Error stays reserved for the
// actual no-output alarm. Transitions keep Warn visible at the normal `info`
// level (measured: the dogfood config is level=info, so Debug would not be
// emitted at all) without producing one line per transmission (measured: 691
// transmissions on 2026-08-01 alone, against 460 warns in the whole 15-day log).
//
//	unknown -> no_meter|meter_not_po ............ Warn once
//	armed   -> no_meter|meter_not_po ............ Warn once
//	change between dark reasons ................. Warn once, carrying old + new
//	no_meter|meter_not_po -> armed .............. Info once
//	repeated transmissions in the same state .... NO line
//	the existing meters record .................. always carries drive_watch
//	the existing Error alarm ..................... gains the evidence already at
//	                                               the call site
//
// TWO JUDGEMENT CALLS PUT TO THE OPERATOR RATHER THAN INFERRED (2026-08-01):
//
//  1. A FOURTH dark reason, `meter_moved_off_po`. All five transitions above are
//     evaluated at ARM time, but protection also disappears MID-transmission:
//     checkDriveSilence drops the watch when the meter is moved off PO while
//     keyed, which was a silent early return. Operator's call: report it, Warn,
//     once. Without it the slot where protection was actually lost is not the
//     slot that reports it — the next arm would, one transmission late.
//
//  2. What the meters record says for a transmission that never reached
//     armDriveWatch (a failed key write, or a teardown racing it). Operator's
//     call: OMIT the field. That matches withMeterContext, which omits gap_max_ms
//     when nothing was measured on the stated grounds that a zero "would read as
//     no silence at all, which is the opposite of the truth". An absent
//     drive_watch therefore means "never reached the arm point", which is
//     distinguishable from all four real outcomes.
//
// NOT LOGGED, and this is a finding rather than an omission: the operator asked
// for tainted-selection state on the Error alarm. It cannot be there — the
// alarm's own path returns early when driveSelTainted is set, so at the emit
// point it is ALWAYS false. A constant field is decoration. Decision 1 puts the
// taint on its own line at the moment it happens, which is where it is real.
//
// Rules assert on EMITTED LOG RECORDS, which is what the operator reads at
// 7Q8AC. That needs log capture, which this package has never had — meters.go
// says so in withMeterContext's comment, and routes both its branches through one
// helper precisely because nothing could catch them diverging. DW6 now can.
//
// THREE DEFECTS FOUND BY REVIEW AFTER THE FIRST GREEN, all of which the rules as
// first written would have shipped. Recorded because each one is a fixture
// failure, not a logic failure, and the shapes recur:
//
//   - THE FIXTURE EXCLUDED THE INTERVAL. DW8 sleeps long enough for the silence
//     timer, so it could only ever see a taint reported BY that timer. A taint
//     arriving inside the final interval — up to 3 s of a 12.6 s slot — was never
//     reported at all, and the meters record went on claiming armed. DW9 keys the
//     threshold long so the timer provably cannot run, and the fix reconciles the
//     transition where the taint is CREATED. Proof: removing that reconciliation
//     turns DW9 red while DW8 stays green.
//
//   - PRESENCE IS NOT A VALUE. DW7 asserted six field names existed. gap_max_ms
//     was emitted from the stored maximum without folding in the gap that was
//     still running — the one that had just tripped the alarm — so a totally
//     silent transmission reported gap_ms≈3000 beside gap_max_ms=0, on precisely
//     the case the alarm exists for. DW7 now asserts the relation between them.
//
//   - AN IDENTIFIER THAT IDENTIFIES NOTHING. DW7 claimed tx_gen joins the alarm to
//     the meters record; the meters record carried no generation, and the obvious
//     fix carries the WRONG one, because finishFt8Tx increments ft8TxGen before
//     flushing the summary. DW10 asserts the two records agree, and its proof uses
//     that off-by-one rather than a missing field: alarm tx_gen=1, meters tx_gen=2.
//
// A FOURTH, found in the round after those three: ONE FIX, TWO SITES, ONE PROOF.
// The taint is created in two places — observeMeter and the failed-unkey rollback
// in unsealMeterGapWindow — and both got the reconciliation, but only the first
// got a rule. Removing it from the unseal path alone left DW1-DW10 green while
// restoring both defects there. DW11 covers it, and drives the REAL release path
// (closing the fake makes tx_off fail) rather than calling seal/unseal by hand, so
// it pins the wiring too: proofs U1 (drop the state change) and U2 (drop the log
// call) each turn it red on their own. The lesson generalises past this file —
// when a fix touches N call sites, the proof must revert each of them separately,
// or N-1 of them are unguarded.

// syncBuf is a mutex-guarded buffer. logging.Service serialises its own writes,
// but a test reading while the detector's timer goroutine writes would still
// race — and these tests deliberately leave timers pending.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newDriveWatchService mirrors newCommandTestService but with a REAL logger
// writing to a buffer, so rules can assert on records rather than on fields the
// mechanism happens to carry.
func newDriveWatchService(t *testing.T) (*Service, *fakeSerial, *syncBuf) {
	t.Helper()
	buf := &syncBuf{}
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, logging.NewForWriter(buf))
	fake := newFakeSerial()
	s.mu.Lock()
	s.activeClient = fake
	s.identityConfirmed = true
	s.mu.Unlock()
	return s, fake, buf
}

// records parses the captured log into decoded records.
func records(t *testing.T, buf *syncBuf) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// matching returns the records whose message contains sub.
func matching(t *testing.T, buf *syncBuf, sub string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, rec := range records(t, buf) {
		if msg, _ := rec["message"].(string); strings.Contains(msg, sub) {
			out = append(out, rec)
		}
	}
	return out
}

// The message substrings the rules key on. Constants so a wording change breaks
// compilation of the tests rather than silently emptying their assertions.
const (
	wentDarkMsg   = "drive detection went dark"
	restoredMsg   = "drive detection restored"
	metersMsg     = "ft8 tx meters"
	driveNoOutMsg = "meter reports no output"
)

// slot runs one complete transmission. ms selects the rig meter ("MS0" = PO,
// "MS2" = ALC, "" = leave the current selection); rxAlive feeds the receive-time
// stream that is the instrument-alive evidence.
func slot(t *testing.T, s *Service, ms string, rxAlive bool) {
	t.Helper()
	if ms != "" {
		s.observeMeter(meterFrame(t, ms))
	}
	if rxAlive {
		rxMeterFlowing(t, s)
	}
	keyedTestSlot(t, s)
	s.finishFt8Tx()
}

// DW1 — UNKNOWN -> DARK reports once. The first transmission of a session that
// cannot arm is exactly the case the operator never learns about today: the
// station transmits with no protection and the log is identical to a healthy
// run.
func TestDriveWatch_UnknownToDark_WarnsOnce(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	slot(t, s, "", false) // no receive-time frames: instrument not known alive

	got := matching(t, buf, wentDarkMsg)
	if len(got) != 1 {
		t.Fatalf("went-dark records = %d, want exactly 1", len(got))
	}
	if lvl, _ := got[0]["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn — a monitoring degradation is not an Error, "+
			"which stays reserved for the actual no-output alarm", lvl)
	}
	if to, _ := got[0]["to"].(string); to != driveWatchNoMeter {
		t.Errorf("to = %q, want %q", to, driveWatchNoMeter)
	}
	if from, _ := got[0]["from"].(string); from != driveWatchUnknown {
		t.Errorf("from = %q, want %q", from, driveWatchUnknown)
	}
}

// DW2 — ARMED -> DARK reports once. Protection was running and stopped, which is
// the transition the operator most needs: nothing else in the log changes when
// the CAT link dies or the meter knob moves.
func TestDriveWatch_ArmedToDark_WarnsOnce(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	slot(t, s, "MS0", true) // armed
	slot(t, s, "MS2", true) // meter moved to ALC → dark

	got := matching(t, buf, wentDarkMsg)
	if len(got) != 1 {
		t.Fatalf("went-dark records = %d, want exactly 1", len(got))
	}
	if from, _ := got[0]["from"].(string); from != driveWatchArmed {
		t.Errorf("from = %q, want %q", from, driveWatchArmed)
	}
	if to, _ := got[0]["to"].(string); to != driveWatchMeterNotPO {
		t.Errorf("to = %q, want %q", to, driveWatchMeterNotPO)
	}
}

// DW3 — a change BETWEEN dark reasons reports once and carries both. Two
// different faults; a reader who saw only the first would go on believing the
// CAT link is the problem after the operator has fixed it and turned the knob.
func TestDriveWatch_DarkReasonChange_WarnsOnceWithOldAndNew(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	slot(t, s, "MS2", false) // no_meter wins: the instrument-alive check is first
	slot(t, s, "MS2", true)  // instrument alive now, but the meter is on ALC

	got := matching(t, buf, wentDarkMsg)
	if len(got) != 2 {
		t.Fatalf("went-dark records = %d, want 2 (one per distinct dark reason)", len(got))
	}
	if from, _ := got[1]["from"].(string); from != driveWatchNoMeter {
		t.Errorf("second record from = %q, want %q — the reason it REPLACED", from, driveWatchNoMeter)
	}
	if to, _ := got[1]["to"].(string); to != driveWatchMeterNotPO {
		t.Errorf("second record to = %q, want %q", to, driveWatchMeterNotPO)
	}
}

// DW4 — DARK -> ARMED reports once, at Info. Recovery is not a degradation, and
// an operator who was told protection vanished is owed the news that it is back.
func TestDriveWatch_DarkToArmed_InfosOnce(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	slot(t, s, "MS2", true) // dark
	slot(t, s, "MS0", true) // armed again

	got := matching(t, buf, restoredMsg)
	if len(got) != 1 {
		t.Fatalf("restored records = %d, want exactly 1", len(got))
	}
	if lvl, _ := got[0]["level"].(string); lvl != "info" {
		t.Errorf("level = %q, want info", lvl)
	}
	if from, _ := got[0]["from"].(string); from != driveWatchMeterNotPO {
		t.Errorf("from = %q, want %q", from, driveWatchMeterNotPO)
	}
}

// DW5 — THE RULE THAT MAKES THE DESIGN A TRANSITION MACHINE. Repeated
// transmissions in the same state emit nothing. This is the whole reason Warn is
// affordable: the operator measured 691 transmissions in one day against 460
// warns in the entire log, so a per-transmission Warn would invert what the level
// means. Run FOUR slots so a per-transmission implementation cannot pass by
// coincidence.
func TestDriveWatch_SameStateRepeated_EmitsNoTransitionLine(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	for i := 0; i < 4; i++ {
		slot(t, s, "MS2", true) // the same dark reason every time
	}

	if got := matching(t, buf, wentDarkMsg); len(got) != 1 {
		t.Errorf("went-dark records = %d across 4 identical slots, want exactly 1 — "+
			"a state that has not changed is not a transition", len(got))
	}
	if got := matching(t, buf, restoredMsg); len(got) != 0 {
		t.Errorf("restored records = %d, want 0", len(got))
	}
}

// DW6 — THE PER-TRANSMISSION FACT, on the record that already exists. The
// transition lines say when the state CHANGED; this says what it WAS for the
// transmission in front of you, which is the question asked when reading back a
// specific slot.
//
// Both meters branches are covered on purpose. logFt8TxMeters has a
// no-samples branch and a with-samples branch, and withMeterContext's own comment
// says one shared path exists because "this package has no log capture, so a
// second copy of this emit is exactly where the two branches could silently
// diverge". The no_meter case takes the first branch and the other two take the
// second, so this table exercises both.
func TestDriveWatch_MetersRecordCarriesTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ms      string
		rxAlive bool
		want    string
	}{
		{"armed", "MS0", true, driveWatchArmed},
		{"instrument silent", "", false, driveWatchNoMeter},
		{"meter not on PO", "MS2", true, driveWatchMeterNotPO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, fake, buf := newDriveWatchService(t)
			shortDriveSilence(s)
			t.Cleanup(answerTxStatusQueries(s, fake))

			slot(t, s, tc.ms, tc.rxAlive)

			got := matching(t, buf, metersMsg)
			if len(got) != 1 {
				t.Fatalf("meters records = %d, want exactly 1", len(got))
			}
			if dw, _ := got[0]["drive_watch"].(string); dw != tc.want {
				t.Errorf("drive_watch = %q, want %q — without it a reader cannot tell a "+
					"transmission that was WATCHED and silent from one that was never watched",
					dw, tc.want)
			}
		})
	}
}

// DW7 — THE ALARM CARRIES ITS OWN EVIDENCE. This is the rule with a measured
// cost behind it: on 2026-08-01 two alarms could only be judged by finding a
// separate meters record, because the alarm line carries `code` and nothing else.
// Every field below is already in hand at the emit point and was being discarded
// — the same shape as the meterGapAtUnkey finding of 2026-07-30.
func TestDriveWatch_AlarmCarriesTheEvidenceAtTheCallSite(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s) // instrument alive, so the watch arms
	keyedTestSlot(t, s)
	time.Sleep(3 * silence) // silent throughout: the collapse being hunted
	s.finishFt8Tx()

	got := matching(t, buf, driveNoOutMsg)
	if len(got) != 1 {
		t.Fatalf("drive alarm records = %d, want exactly 1", len(got))
	}
	rec := got[0]
	if lvl, _ := rec["level"].(string); lvl != "error" {
		t.Errorf("level = %q, want error — the confirmed no-output alarm keeps Error", lvl)
	}
	for _, field := range []string{
		"meter_sel",    // which meter the silence was measured on
		"meter_n",      // how many frames arrived at all
		"meter_po_max", // whether any output was ever seen
		"gap_ms",       // the silence that tripped it
		"gap_max_ms",   // the widest silence in the window
		"tx_gen",       // which transmission, so it joins to the meters record
	} {
		if _, ok := rec[field]; !ok {
			t.Fatalf("alarm record is missing %q — judging this alarm then needs a "+
				"different record, which is what cost the time on 2026-08-01", field)
		}
	}

	// PRESENCE IS NOT ENOUGH, and the first version of this rule stopped there.
	// The gap that TRIPS the alarm is still running when it fires, so it is not yet
	// in meterGapMax — noteMeterGap folds an interval in only when the next frame
	// arrives, and on a totally silent transmission none ever does. Reporting the
	// stored maximum alone gave gap_ms≈3000 beside gap_max_ms=0 on exactly the
	// worst case this alarm exists for. Two numbers that contradict each other are
	// worse than one number, because a reader has to decide which lied.
	gapMs, _ := rec["gap_ms"].(float64)
	gapMaxMs, _ := rec["gap_max_ms"].(float64)
	if gapMs < float64(silence.Milliseconds()) {
		t.Errorf("gap_ms = %v, want >= the %v threshold that raised the alarm", gapMs, silence)
	}
	if gapMaxMs < gapMs {
		t.Errorf("gap_max_ms = %v < gap_ms = %v — the widest silence cannot be narrower "+
			"than the one that tripped the alarm", gapMaxMs, gapMs)
	}
}

// DW9 — THE LATE TAINT. The meter moves off PO inside the FINAL silence interval,
// so the transmission ends before the watch timer ticks again and
// disarmDriveWatch cancels it. DW8 sleeps long enough for that timer and
// therefore cannot see this at all — the fixture excluded the very interval where
// the two implementations differ.
//
// Two things go wrong when the transition is only reconciled at the timer: no
// Warn is emitted, and the meters record still claims drive_watch=armed for a
// window that had stopped measuring RF. The second is the more dangerous, because
// "armed and silent" is the log's positive evidence that output was normal.
//
// The silence threshold is set LONG here so the timer provably cannot fire —
// making this deterministic rather than a race the fixture might win by luck.
func TestDriveWatch_TaintLateInTheWindow_StillReportsBeforeTheSummary(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	s.driveSilence = 30 * time.Second // no tick can occur inside this test
	t.Cleanup(answerTxStatusQueries(s, fake))

	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s) // armed

	s.observeMeter(meterFrame(t, "MS2")) // knob turned...
	s.finishFt8Tx()                      // ...and the slot ends before any timer tick

	got := matching(t, buf, wentDarkMsg)
	if len(got) != 1 {
		t.Fatalf("went-dark records = %d, want exactly 1 — protection was lost and the "+
			"timer that used to report it never ran", len(got))
	}
	if to, _ := got[0]["to"].(string); to != driveWatchMovedOffPO {
		t.Errorf("to = %q, want %q", to, driveWatchMovedOffPO)
	}

	meters := matching(t, buf, metersMsg)
	if len(meters) != 1 {
		t.Fatalf("meters records = %d, want exactly 1", len(meters))
	}
	if dw, _ := meters[0]["drive_watch"].(string); dw != driveWatchMovedOffPO {
		t.Errorf("meters record drive_watch = %q, want %q — a tainted window must not "+
			"read as armed, which is the log's positive evidence that output was normal",
			dw, driveWatchMovedOffPO)
	}
}

// DW11 — THE OTHER TAINT SITE. A failed tx_off reopens the keyed window, and
// unsealMeterGapWindow answers the selection question afresh from whatever the
// meter is on NOW. That is a second, independent way the watch goes dark, and
// DW9 does not reach it: DW9 exercises observeMeter's site, so removing the
// reconciliation from the unseal path alone left DW1-DW10 all green while
// restoring both defects — no Warn, and drive_watch=armed on a window that had
// stopped measuring RF.
//
// The REAL release path runs here rather than seal/unseal being called by hand
// (the precedent D19 set, when the fake could not fail a write): closing the fake
// makes WriteCommandBytes return ErrClosed, so tx_off genuinely fails. That
// matters because it covers the WIRING as well as the state change — a test that
// called unsealMeterGapWindow and logged the result itself would pass even if
// nothing in production ever logged it.
//
// meterSel is set directly to model the sealed-window discard: observeMeter
// records the selection unconditionally but skips the taint decision while
// sealed, which is exactly the state a knob turned mid-write leaves behind. The
// long threshold keeps any silence timer out of it.
func TestDriveWatch_TaintOnFailedUnkey_ReportsAndLabelsTheSummary(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	s.driveSilence = 30 * time.Second // no timer tick can occur inside this test
	t.Cleanup(answerTxStatusQueries(s, fake))

	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s) // armed on PO

	// The knob moves while the unkey write is pending: the selection is recorded,
	// the taint deliberately is not (the window is sealed by then).
	s.mu.Lock()
	s.meterSel = "ALC"
	tainted := s.driveSelTainted
	s.mu.Unlock()
	if tainted {
		t.Fatal("fixture: the taint must NOT be set yet, or this proves the wrong site")
	}

	// Make tx_off fail for real, so the release path takes its rollback branch.
	fake.Close()
	if err := s.UnkeyFt8Tx(context.Background()); err == nil {
		t.Fatal("fixture: the tx_off write must fail, or the unseal path never runs")
	}

	got := matching(t, buf, wentDarkMsg)
	if len(got) != 1 {
		t.Fatalf("went-dark records = %d, want exactly 1 — the window reopened on a "+
			"selection that is no longer PO, so the watch is dark from here", len(got))
	}
	if to, _ := got[0]["to"].(string); to != driveWatchMovedOffPO {
		t.Errorf("to = %q, want %q", to, driveWatchMovedOffPO)
	}

	s.finishFt8Tx()
	meters := matching(t, buf, metersMsg)
	if len(meters) != 1 {
		t.Fatalf("meters records = %d, want exactly 1", len(meters))
	}
	if dw, _ := meters[0]["drive_watch"].(string); dw != driveWatchMovedOffPO {
		t.Errorf("meters record drive_watch = %q, want %q — after a failed unkey the rig "+
			"is still keyed with its meter off PO, so the window cannot read as armed",
			dw, driveWatchMovedOffPO)
	}
}

// DW10 — THE JOIN ACTUALLY WORKS. DW7 asserts the alarm carries tx_gen; this
// asserts the identity leads somewhere. It has to be the meters record, because a
// state that has not changed emits no transition line, so on the ordinary case
// there IS no other record carrying the transmission.
//
// This is also the rule that catches an off-by-one, which the obvious
// implementation has: finishFt8Tx increments ft8TxGen BEFORE flushing the
// summary, so reading the live counter at flush time labels the record with the
// NEXT transmission's generation and the join silently points at the wrong slot.
func TestDriveWatch_AlarmAndMetersRecordShareTheGeneration(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	time.Sleep(3 * silence)
	s.finishFt8Tx()

	alarm := matching(t, buf, driveNoOutMsg)
	meters := matching(t, buf, metersMsg)
	if len(alarm) != 1 || len(meters) != 1 {
		t.Fatalf("alarm records = %d, meters records = %d, want 1 of each", len(alarm), len(meters))
	}
	alarmGen, ok := alarm[0]["tx_gen"].(float64)
	if !ok {
		t.Fatal("alarm carries no tx_gen")
	}
	metersGen, ok := meters[0]["tx_gen"].(float64)
	if !ok {
		t.Fatal("the meters record carries no tx_gen, so the alarm's identity leads nowhere")
	}
	if alarmGen != metersGen {
		t.Errorf("alarm tx_gen = %v but meters tx_gen = %v — the two records describe the "+
			"SAME transmission and must agree, or the join points at the wrong slot",
			alarmGen, metersGen)
	}
}

// DW8 — the operator's added fourth reason. The meter is moved off PO WHILE
// KEYED, so the watch is dropped for the rest of the transmission. Arm-time
// transitions cannot see this: arming already happened and succeeded. Without
// this rule the slot that actually lost protection is silent and the NEXT one
// reports it, one transmission late.
func TestDriveWatch_MeterMovedOffPoWhileKeyed_WarnsOnce(t *testing.T) {
	s, fake, buf := newDriveWatchService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))

	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s) // armed

	s.observeMeter(meterFrame(t, "MS2")) // knob turned mid-slot → taint
	time.Sleep(3 * silence)              // let the watch timer reach its taint branch
	s.finishFt8Tx()

	got := matching(t, buf, wentDarkMsg)
	if len(got) != 1 {
		t.Fatalf("went-dark records = %d, want exactly 1", len(got))
	}
	if from, _ := got[0]["from"].(string); from != driveWatchArmed {
		t.Errorf("from = %q, want %q", from, driveWatchArmed)
	}
	if to, _ := got[0]["to"].(string); to != driveWatchMovedOffPO {
		t.Errorf("to = %q, want %q", to, driveWatchMovedOffPO)
	}
	// The alarm must NOT fire: the silence stopped being evidence the moment the
	// meter left PO, which is the whole reason the taint branch exists.
	if alarms := matching(t, buf, driveNoOutMsg); len(alarms) != 0 {
		t.Errorf("drive alarms = %d, want 0 — a tainted window cannot support the claim", len(alarms))
	}
}
