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

// R2 — readings decoded while NOT keyed are discarded. PO reads ~0 in receive,
// so accumulating them would peg every transmission's minimum at zero and make
// a real collapse indistinguishable from normal operation.
func TestObserveMeter_IgnoredWhileNotKeyed(t *testing.T) {
	s, _ := newCommandTestService(t)
	setFt8Keyed(s, false)

	s.observeMeter(meterFrame(t, "RM5000000"))
	s.observeMeter(meterFrame(t, "RM4000000"))

	sum := s.flushFt8TxMeters()
	if len(sum.Samples) != 0 {
		t.Fatalf("readings taken outside a transmission were accumulated: %+v", sum.Samples)
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
