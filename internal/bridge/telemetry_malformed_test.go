package bridge

// L5 acceptance — malformed CAT telemetry must not leave safety-relevant state
// stale silently. Operator rulings 2026-08-15: OPEN-1(b) invalidate the
// attribution snapshot, OPEN-2 bound the logged raw value to 32 bytes.
//
// The confusable states these criteria separate:
//   - valid-unchanged  — the tag was simply absent this frame → keep last-known,
//     no log. (telemetryTagValid == false and no parse error.)
//   - malformed-new    — the rig pushed a NEW value that won't parse. Before L5
//     this was indistinguishable from valid-unchanged: the field dropped to zero,
//     read as "unchanged", and the stale value stayed apparently current on the
//     wire AND in CurrentDialMHz / CurrentPowerW, which stamp every logged FT8 QSO.
//
// Criteria (observable: the dial/power a logged QSO would carry, and smd.log):
//   G1/G2 — a garbled VFOAFREQ / VFOBFREQ / TXPWR yields a parse diagnostic (not a
//           silent drop); a valid value yields none.
//   G3    — after a garbled freq/power, CurrentDialMHz / CurrentPowerW report
//           UNKNOWN, not the pre-garble stale value (OPEN-1(b)).
//   G4    — one recovery line when a valid value for that tag resumes.
//   G5    — one Warn per episode (not per frame); the logged raw value is bounded.
//   Freeze — a malformed TXPWR arriving DURING a tune must NOT disturb lastPower,
//           the frozen pre-tune value StartTune restores to (never change TX flow).

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// feedStatus drives the exact readLoop per-frame sequence (pipeline.go ~937-946)
// against the real production methods: reconcile malformed telemetry, then — only
// for a populated frame — refresh the tune + dial snapshots. hub.publish is
// omitted (irrelevant to the dial/power assertions).
func feedStatus(s *Service, tr *malformedTelemetryTracker, status cat.Status) {
	p, hasFields, errs := mapStatusToPayload(status)
	s.observeMalformedTelemetry(tr, p, errs, "test-driver")
	if hasFields {
		s.captureTuneSnapshot(p)
		s.captureDialFreq(p)
	}
}

// newTelemetryTestService returns a buffer-logged, write-gate-satisfying service:
// activeClient + identityConfirmed make rigWritableLocked true so CurrentDialMHz /
// CurrentPowerW return their snapshot rather than a blanket "unknown".
func newTelemetryTestService(t *testing.T) (*Service, *syncBuf, *malformedTelemetryTracker) {
	t.Helper()
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")
	s.activeClient = newFakeSerial()
	s.identityConfirmed = true
	tr := newMalformedTelemetryTracker()
	return s, buf, &tr
}

// --- G1/G2: parse diagnostics (pure, no service) -------------------------------

func TestMapStatusToPayload_ReportsMalformedFreq(t *testing.T) {
	_, populated, errs := mapStatusToPayload(cat.Status{tagVfoAFreq: "not-a-freq"})
	if populated {
		t.Fatal("populated=true for a frame whose only value is malformed")
	}
	if len(errs) != 1 || errs[0].tag != tagVfoAFreq {
		t.Fatalf("parse errors = %+v, want one for %s", errs, tagVfoAFreq)
	}
	if !strings.Contains(errs[0].raw, "not-a-freq") {
		t.Errorf("raw = %q, want it to carry the offending value", errs[0].raw)
	}
}

func TestMapStatusToPayload_ReportsMalformedPower(t *testing.T) {
	_, _, errs := mapStatusToPayload(cat.Status{tagTxPwr: "??"})
	if len(errs) != 1 || errs[0].tag != tagTxPwr {
		t.Fatalf("parse errors = %+v, want one for %s", errs, tagTxPwr)
	}
}

func TestMapStatusToPayload_ValidValuesNoErrors(t *testing.T) {
	p, populated, errs := mapStatusToPayload(cat.Status{
		tagVfoAFreq: "014074000",
		tagVfoBFreq: "007074000",
		tagTxPwr:    "050",
	})
	if !populated || len(errs) != 0 {
		t.Fatalf("valid frame produced errs=%+v populated=%v; want no errors", errs, populated)
	}
	if p.VfoA != 14074000 || p.VfoB != 7074000 || p.Power != 50 {
		t.Errorf("valid values not parsed: %+v", p)
	}
}

func TestMapStatusToPayload_MalformedAlongsideValidReportsOnlyTheBadTag(t *testing.T) {
	p, _, errs := mapStatusToPayload(cat.Status{tagVfoAFreq: "junk", tagVfoBFreq: "007074000"})
	if len(errs) != 1 || errs[0].tag != tagVfoAFreq {
		t.Fatalf("parse errors = %+v, want exactly one for %s", errs, tagVfoAFreq)
	}
	if p.VfoB != 7074000 {
		t.Errorf("valid VFO-B dropped alongside a malformed VFO-A: %+v", p)
	}
}

func TestMapStatusToPayload_BoundsRawValue(t *testing.T) {
	long := strings.Repeat("9x", 200) // 400 bytes, non-numeric
	_, _, errs := mapStatusToPayload(cat.Status{tagVfoAFreq: long})
	if len(errs) != 1 {
		t.Fatalf("parse errors = %+v, want one", errs)
	}
	if len(errs[0].raw) > maxTelemetryRawLen {
		t.Errorf("raw len = %d, want <= %d", len(errs[0].raw), maxTelemetryRawLen)
	}
}

// --- G3: invalidation, observed via CurrentDialMHz / CurrentPowerW -------------

func TestMalformedFreq_InvalidatesDialSnapshot(t *testing.T) {
	s, buf, tr := newTelemetryTestService(t)

	feedStatus(s, tr, cat.Status{"SELECT": "VFO-A", tagVfoAFreq: "014074000"})
	if mhz, ok := s.CurrentDialMHz(); !ok || mhz < 14.0 || mhz > 14.1 {
		t.Fatalf("seed dial = (%v, %v); want ~14.074 MHz known", mhz, ok)
	}

	feedStatus(s, tr, cat.Status{tagVfoAFreq: "garbage"})
	if mhz, ok := s.CurrentDialMHz(); ok {
		t.Fatalf("dial still reported (%v) after a malformed freq; want unknown (stale must not read as fresh)", mhz)
	}
	if recs := matching(t, buf, "malformed rig telemetry"); len(recs) != 1 {
		t.Fatalf("malformed-warn lines = %d, want 1; log:\n%s", len(recs), buf.String())
	} else if tag, _ := recs[0]["tag"].(string); tag != tagVfoAFreq {
		t.Errorf("warn tag = %q, want %s", tag, tagVfoAFreq)
	}
}

func TestMalformedPower_InvalidatesPowerSnapshot(t *testing.T) {
	s, buf, tr := newTelemetryTestService(t)

	feedStatus(s, tr, cat.Status{tagTxPwr: "050"})
	if w := s.CurrentPowerW(); w != 50 {
		t.Fatalf("seed power = %d, want 50", w)
	}

	feedStatus(s, tr, cat.Status{tagTxPwr: "not-watts"})
	if w := s.CurrentPowerW(); w != 0 {
		t.Fatalf("power still %d after a malformed TXPWR; want 0 (unknown)", w)
	}
	if recs := matching(t, buf, "malformed rig telemetry"); len(recs) != 1 {
		t.Fatalf("malformed-warn lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
}

// Freeze — the load-bearing TX-safety criterion: a malformed TXPWR during a tune
// must not clear lastPower, or StartTune's restore target is corrupted.
func TestMalformedPower_DuringTune_DoesNotCorruptRestoreSnapshot(t *testing.T) {
	s, _, tr := newTelemetryTestService(t)

	feedStatus(s, tr, cat.Status{tagTxPwr: "050"}) // pre-tune power known
	s.mu.Lock()
	s.tuneActive = true
	s.mu.Unlock()

	feedStatus(s, tr, cat.Status{tagTxPwr: "garbage"})

	s.mu.Lock()
	got := s.lastPower
	s.mu.Unlock()
	if got != 50 {
		t.Fatalf("lastPower = %d during tune after a malformed TXPWR; want 50 (frozen restore snapshot must survive)", got)
	}
}

// --- G4/G5: episodic warn + recovery ------------------------------------------

func TestMalformedFreq_WarnsOncePerEpisodeAndRecovers(t *testing.T) {
	s, buf, tr := newTelemetryTestService(t)

	feedStatus(s, tr, cat.Status{"SELECT": "VFO-A", tagVfoAFreq: "014074000"})
	for i := 0; i < 3; i++ {
		feedStatus(s, tr, cat.Status{tagVfoAFreq: "garbage"})
	}
	if recs := matching(t, buf, "malformed rig telemetry"); len(recs) != 1 {
		t.Fatalf("malformed-warn lines = %d after 3 garbled frames, want 1 (once per episode); log:\n%s", len(recs), buf.String())
	}

	// Valid value resumes → one recovery, dial known again.
	feedStatus(s, tr, cat.Status{tagVfoAFreq: "014074000"})
	if recs := matching(t, buf, "rig telemetry recovered"); len(recs) != 1 {
		t.Fatalf("recovery lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	if _, ok := s.CurrentDialMHz(); !ok {
		t.Fatal("dial still unknown after a valid freq resumed")
	}

	// A fresh garble opens a new episode → a second warn.
	feedStatus(s, tr, cat.Status{tagVfoAFreq: "garbage"})
	if recs := matching(t, buf, "malformed rig telemetry"); len(recs) != 2 {
		t.Fatalf("malformed-warn lines = %d after a new episode, want 2; log:\n%s", len(recs), buf.String())
	}
}
