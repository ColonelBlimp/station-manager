package bridge

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// Per-transmission meter accumulation (follow-up (d), 2026-07-29).
//
// The FTdx10 marks RM READ METER as AI=O in its command index (CAT ref 2308-F
// p.5), so ALC/PO/SWR frames arrive UNPROMPTED while transmitting — the bridge
// listens rather than polling, which is why nothing here adds a CAT write to
// the key-down path.
//
// The rules below were written before the implementation. Each pins a state
// that the accumulator CREATES rather than only the happy path: readings must
// be attributable to one transmission, must not carry across transmissions,
// and — the load-bearing one — an ABSENCE of readings must be reported rather
// than being silent. The whole point of the first on-air run is to learn
// whether the rig pushes RM at all, and silence that produces no summary
// cannot be told apart from code that never ran.

// setFt8Keyed drives the flag the accumulator gates on, without going through
// the full key path (which would need a live client and the confirm cycle).
func setFt8Keyed(s *Service, keyed bool) {
	s.mu.Lock()
	s.ft8TxActive = keyed
	s.mu.Unlock()
}

func meterFrame(t *testing.T, line string) cat.Status {
	t.Helper()
	def, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("rigdef missing")
	}
	st, err := cat.Decode(def, []byte(line))
	if err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	return st
}

// R1 — a reading decoded while FT8 TX is keyed is accumulated against that
// transmission, and the summary reports its meter and value.
func TestObserveMeter_AccumulatesWhileKeyed(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, true)

	s.observeMeter(meterFrame(t, "RM5128000"))

	sum := s.flushFt8TxMeters()
	if len(sum.Samples) != 1 {
		t.Fatalf("got %d samples, want 1: %+v", len(sum.Samples), sum.Samples)
	}
	got := sum.Samples[0]
	if got.Tag != "PO" || got.Count != 1 || got.Last != 128 {
		t.Fatalf("sample = %+v, want PO count=1 last=128", got)
	}
}

// R1b — min/max track the RANGE across a transmission, not just the last
// value. A collapse PARTWAY through a transmission is the case the whole
// exercise exists to catch: last alone would miss a drop that recovered, and
// max alone would miss a transmission that never came up.
func TestObserveMeter_TracksRangeAcrossTransmission(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, true)

	for _, f := range []string{"RM5200000", "RM5000000", "RM5150000"} {
		s.observeMeter(meterFrame(t, f))
	}

	sum := s.flushFt8TxMeters()
	if len(sum.Samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(sum.Samples))
	}
	got := sum.Samples[0]
	if got.Count != 3 || got.Min != 0 || got.Max != 200 || got.Last != 150 {
		t.Fatalf("sample = %+v, want count=3 min=0 max=200 last=150", got)
	}
}

// R2 — readings decoded while NOT keyed stay out of the per-transmission
// SUMMARY. PO reads ~0 in receive, so accumulating them would peg every
// transmission's minimum at zero and make a real collapse indistinguishable
// from normal operation.
//
// This is the preservation half of R8: observation became unconditional, and
// this rule is the thing that refactor must NOT weaken.
func TestObserveMeter_ReceiveReadingsExcludedFromSummary(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, false)

	s.observeMeter(meterFrame(t, "RM5000000"))
	s.observeMeter(meterFrame(t, "RM4000000"))

	sum := s.flushFt8TxMeters()
	if len(sum.Samples) != 0 {
		t.Fatalf("readings taken outside a transmission were accumulated: %+v", sum.Samples)
	}
}

// R8 — OBSERVATION IS UNCONDITIONAL. A meter frame decoded in receive is still
// recorded as the rig's current reading.
//
// The gate belongs on the per-transmission diagnostic, not on observation
// itself: the rig pushing RM is rig state arriving like FA or MD0, and a
// future browser meter display must not need a transmission to come alive
// (operator, 2026-07-29). The original implementation returned early when not
// keyed and DESTROYED the reading, which would have made any such consumer
// blank until the operator transmitted.
func TestObserveMeter_RecordsLatestWhileReceiving(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, false)

	s.observeMeter(meterFrame(t, "RM6030000"))
	s.observeMeter(meterFrame(t, "RM5000000"))

	latest := s.latestMeters()
	byTag := map[string]int{}
	for _, r := range latest {
		byTag[r.Tag] = r.Value
	}
	if v, ok := byTag["SWR"]; !ok || v != 30 {
		t.Fatalf("SWR not observed in receive: latest=%+v", latest)
	}
	if v, ok := byTag["PO"]; !ok || v != 0 {
		t.Fatalf("PO not observed in receive: latest=%+v", latest)
	}
}

// R8b — the latest reading tracks the most recent frame, in receive or keyed.
// A display showing a stale value would be worse than showing none.
func TestObserveMeter_LatestTracksMostRecent(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, false)

	s.observeMeter(meterFrame(t, "RM5010000"))
	s.observeMeter(meterFrame(t, "RM5200000"))

	// Assert PRESENCE then value: a bare range-and-compare passes vacuously on
	// an empty slice, which is exactly how this test slipped past the stub.
	latest := s.latestMeters()
	var po *meterReading
	for i := range latest {
		if latest[i].Tag == "PO" {
			po = &latest[i]
		}
	}
	if po == nil {
		t.Fatalf("PO absent from latest readings: %+v", latest)
	}
	if po.Value != 200 {
		t.Fatalf("latest PO = %d, want 200 (most recent frame)", po.Value)
	}
}

// R10 — the first meter frame of a pipeline lifecycle is announced ONCE.
//
// Without it, a rig that pushes nothing is indistinguishable from a wiring bug
// on our side, and answering that question would need a transmission — the
// very coupling R8 removes. Silent thereafter, because the push rate is
// unknown and could be tens per second.
func TestMarkMeterSeen_AnnouncesOnlyOnce(t *testing.T) {
	s, _ := newCommandTestService(t)

	s.mu.Lock()
	first := s.markMeterSeenLocked()
	second := s.markMeterSeenLocked()
	s.mu.Unlock()

	if !first {
		t.Fatal("the first meter frame must announce")
	}
	if second {
		t.Fatal("a second meter frame must NOT re-announce (the push rate is unbounded)")
	}
}

// R10b — a new pipeline re-answers the question. Scoped per-pipeline like
// identityVerified: a reconnect may be a different rig, or the same rig with
// AI mode no longer armed (the FTdx10 clears AI on power-off), so the previous
// instance's answer does not carry over.
func TestResetMeterObservation_ReArmsAnnouncement(t *testing.T) {
	s, _ := newCommandTestService(t)

	s.mu.Lock()
	_ = s.markMeterSeenLocked()
	s.mu.Unlock()

	s.resetMeterObservation()

	s.mu.Lock()
	again := s.markMeterSeenLocked()
	s.mu.Unlock()
	if !again {
		t.Fatal("a new pipeline lifecycle must re-announce whether this rig pushes meters")
	}
}

// R10c — a reconnect must not leave the previous rig's readings on display.
func TestResetMeterObservation_ClearsLatest(t *testing.T) {
	s, _ := newCommandTestService(t)
	s.observeMeter(meterFrame(t, "RM5200000"))

	s.resetMeterObservation()

	if latest := s.latestMeters(); len(latest) != 0 {
		t.Fatalf("readings survived a pipeline teardown: %+v", latest)
	}
}

// R3/R4 — ending a transmission consumes the accumulator, and a transmission
// during which the rig pushed NOTHING still produces a summary. Absence has to
// be reportable: distinguishing "this rig does not push RM" from "the end-of-TX
// path never ran" is the first question the on-air run has to answer.
func TestFinishFt8Tx_ConsumesAccumulatorAndReportsAbsence(t *testing.T) {
	s, _ := newCommandTestService(t)

	// A transmission with no meter pushes at all.
	setFt8Keyed(s, true)
	s.finishFt8Tx()
	if sum := s.lastFt8TxMeters(); !sum.Present {
		t.Fatal("a transmission with no meter data produced no summary — silence would be unattributable")
	} else if len(sum.Samples) != 0 {
		t.Fatalf("no frames were pushed, yet samples = %+v", sum.Samples)
	}

	// A transmission WITH pushes: the summary must reflect them.
	setFt8Keyed(s, true)
	s.observeMeter(meterFrame(t, "RM4090000"))
	s.finishFt8Tx()
	sum := s.lastFt8TxMeters()
	if !sum.Present || len(sum.Samples) != 1 || sum.Samples[0].Tag != "ALC" {
		t.Fatalf("summary = %+v, want one ALC sample", sum)
	}
}

// R5 — the accumulator resets between transmissions. Without this, a collapse
// in transmission 1 would keep re-appearing as transmission 2's minimum and
// every later transmission would look faulty.
func TestObserveMeter_DoesNotCarryAcrossTransmissions(t *testing.T) {
	s, _ := newCommandTestService(t)

	setFt8Keyed(s, true)
	s.observeMeter(meterFrame(t, "RM5000000")) // TX 1: collapsed
	s.finishFt8Tx()

	setFt8Keyed(s, true)
	s.observeMeter(meterFrame(t, "RM5200000")) // TX 2: healthy
	s.finishFt8Tx()

	sum := s.lastFt8TxMeters()
	if len(sum.Samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(sum.Samples))
	}
	if got := sum.Samples[0]; got.Min != 200 || got.Count != 1 {
		t.Fatalf("transmission 2 = %+v, want min=200 count=1 (transmission 1 must not carry over)", got)
	}
}

// R11 — the meter the rig actually PUSHES (RM0) is accumulated per
// transmission. Measured 2026-07-29 on the dogfood FTdx10: RM0 is the ONLY
// meter prefix the rig sends unprompted (RM4/5/6 are query-only), so without
// this the per-transmission summary is empty on real hardware — which is
// exactly what the first two on-air transmissions reported.
func TestObserveMeter_AccumulatesPushedMeter(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, true)

	for _, f := range []string{"RM0180000", "RM0000000", "RM0210000"} {
		s.observeMeter(meterFrame(t, f))
	}

	sum := s.flushFt8TxMeters()
	var m *meterSample
	for i := range sum.Samples {
		if sum.Samples[i].Tag == "METER" {
			m = &sum.Samples[i]
		}
	}
	if m == nil {
		t.Fatalf("pushed RM0 readings absent from the summary: %+v", sum.Samples)
	}
	if m.Count != 3 || m.Min != 0 || m.Max != 210 || m.Last != 210 {
		t.Fatalf("METER = %+v, want count=3 min=0 max=210 last=210", m)
	}
}

// R12 — the meter SELECTION is recorded, because a pushed RM0 value is
// uninterpretable on its own. The rig reports the value (RM0) and which meter
// it is (MS) as separate frames; storing only the number would leave the log
// saying "the meter read 210" with no way to know whether that is power,
// ALC or drain voltage. MS is in the READ burst so the answer arrives at
// pipeline start rather than being inferred.
func TestObserveMeter_RecordsMeterSelection(t *testing.T) {
	s, _ := newCommandTestService(t)

	if got := s.meterSelection(); got != "" {
		t.Fatalf("selection before any MS frame = %q, want empty (unknown, not guessed)", got)
	}
	s.observeMeter(meterFrame(t, "MS00"))
	if got := s.meterSelection(); got != "PO" {
		t.Fatalf("selection = %q, want PO", got)
	}
	// The operator can change it mid-session from the front panel; the rig
	// pushes the new selection and the reading's meaning changes with it.
	s.observeMeter(meterFrame(t, "MS50"))
	if got := s.meterSelection(); got != "SWR" {
		t.Fatalf("selection after front-panel change = %q, want SWR", got)
	}
}

// R12b — the selection is reported WITH the transmission's summary, so a log
// line is self-describing rather than needing correlation against a separate
// line that may be thousands of frames earlier. The label belongs to the
// READING, not to the summary: see R13.
func TestFlushFt8TxMeters_CarriesSelection(t *testing.T) {
	s, _ := newCommandTestService(t)
	s.observeMeter(meterFrame(t, "MS00"))
	setFt8Keyed(s, true)
	s.observeMeter(meterFrame(t, "RM0200000"))

	sum := s.flushFt8TxMeters()
	if len(sum.Samples) != 1 {
		t.Fatalf("got %d samples, want 1: %+v", len(sum.Samples), sum.Samples)
	}
	if sum.Samples[0].Sel != "PO" {
		t.Fatalf("sample Sel = %q, want PO", sum.Samples[0].Sel)
	}
}

// R13 — readings taken under DIFFERENT meter selections are never merged, and
// each carries the selection that was in force when it was taken (c49e12f2
// review P2).
//
// The pushed RM0 frame does not say which meter it is, so the selection is the
// only thing that gives a number meaning. Accumulating across a change and
// labelling the lot with whatever was selected last produces a min/max that
// spans two different physical quantities — power and SWR share nothing but a
// 0-255 scale. Mislabelled diagnostics are worse than none, which is the
// standard this file already sets for the unknown-selection case.
func TestObserveMeter_SelectionChangeSplitsSamples(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, true)

	s.observeMeter(meterFrame(t, "MS00")) // PO
	s.observeMeter(meterFrame(t, "RM0100000"))
	s.observeMeter(meterFrame(t, "MS50")) // operator switches to SWR mid-transmission
	s.observeMeter(meterFrame(t, "RM0200000"))

	sum := s.flushFt8TxMeters()
	bySel := map[string]meterSample{}
	for _, m := range sum.Samples {
		bySel[m.Sel] = m
	}
	if len(bySel) != 2 {
		t.Fatalf("readings under two selections were merged: %+v", sum.Samples)
	}
	if po := bySel["PO"]; po.Max != 100 || po.Count != 1 {
		t.Fatalf("PO sample = %+v, want max=100 count=1", po)
	}
	if swr := bySel["SWR"]; swr.Max != 200 || swr.Count != 1 {
		t.Fatalf("SWR sample = %+v, want max=200 count=1", swr)
	}
}

// R13b — an MS frame arriving AFTER the last reading must not relabel it. The
// selection can change in the TX→RX tail, which needs no operator action at
// all, so this is reachable without anyone touching the front panel
// mid-transmission.
func TestObserveMeter_LateSelectionDoesNotRelabel(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, true)

	s.observeMeter(meterFrame(t, "MS00")) // PO
	s.observeMeter(meterFrame(t, "RM0150000"))
	s.observeMeter(meterFrame(t, "MS50")) // arrives after the last reading

	sum := s.flushFt8TxMeters()
	if len(sum.Samples) != 1 {
		t.Fatalf("got %d samples, want 1: %+v", len(sum.Samples), sum.Samples)
	}
	if got := sum.Samples[0].Sel; got != "PO" {
		t.Fatalf("sample Sel = %q, want PO — a later MS frame must not relabel a reading already taken", got)
	}
}

// R7 — a transmission BEGINS with an empty accumulator (codex review of
// bd60f178, P1).
//
// KeyFt8Tx sets ft8TxActive before writing tx_on, so meter frames decoded in
// the window between the two are accumulated. If that write then fails, the
// rollback clears the TX flags and the backstop timer but NOT the accumulator,
// and all three of its return paths exit without finishFt8Tx — so the aborted
// attempt's readings merge into whatever transmits next, corrupting the
// per-transmission attribution this whole feature provides.
//
// The review offered two fixes: clear on the failed-key rollback, or clear
// when beginning every key. This pins the SECOND deliberately. "A transmission
// starts empty" is one invariant at the single point that defines a
// transmission's start; "every failure path remembers to clean up" is a
// property of N exit paths that any future early return silently evades. The
// rollback path is where the bug was found, not where the rule belongs.
//
// The leftover state is constructed directly rather than raced against a
// failing write: the fake's onWrite hook only fires on writes that succeed, so
// the real window cannot be hit deterministically.
func TestKeyFt8Tx_StartsWithEmptyMeterAccumulator(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	s.lastMode = "USB"
	s.tuneRestoreSettle = 0

	// State a failed key attempt leaves behind: readings banked while
	// ft8TxActive was briefly true, then the flag rolled back without a flush.
	setFt8Keyed(s, true)
	s.observeMeter(meterFrame(t, "RM5250000"))
	setFt8Keyed(s, false)

	// A real transmission now runs to completion.
	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("KeyFt8Tx: %v", err)
	}
	s.observeMeter(meterFrame(t, "RM5100000"))
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	sum := s.lastFt8TxMeters()
	if len(sum.Samples) != 1 {
		t.Fatalf("got %d samples, want 1: %+v", len(sum.Samples), sum.Samples)
	}
	got := sum.Samples[0]
	if got.Count != 1 || got.Max != 100 {
		t.Fatalf("summary = %+v, want count=1 max=100 — the aborted key attempt's "+
			"reading (250) must not be merged into this transmission", got)
	}
}

// R6 — a disconnect while keyed clears the accumulator too. The rig vanishing
// mid-transmission must not leave readings to be attributed to whatever
// transmits next after the supervisor reconnects.
func TestClearFt8TxOnDisconnect_ResetsMeters(t *testing.T) {
	s, _ := newCommandTestService(t)

	setFt8Keyed(s, true)
	s.observeMeter(meterFrame(t, "RM5000000"))
	s.clearFt8TxOnDisconnect()

	setFt8Keyed(s, true)
	sum := s.flushFt8TxMeters()
	if len(sum.Samples) != 0 {
		t.Fatalf("readings survived a disconnect: %+v", sum.Samples)
	}
}

// A tag the summary does not model must not appear in it. The load-bearing
// guard is the allowlist in flushFt8TxMetersLocked, NOT the matching one in
// observeMeter: with only the accumulate-side filter removed this test still
// passes, because flush screens the tag a second time. Verified by reverting
// both (2026-07-29) — the pair had to go before the test went red.
//
// What it protects: a future rigdef state (S-meter, IDD, VDD) joining the
// per-transmission summary silently and changing what the operator is reading.
func TestObserveMeter_IgnoresUnmodelledTags(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, true)

	s.observeMeter(cat.Status{"VFOAFREQ": "014074000", "SMETER": "015"})

	if sum := s.flushFt8TxMeters(); len(sum.Samples) != 0 {
		t.Fatalf("unmodelled tags were accumulated: %+v", sum.Samples)
	}
}
