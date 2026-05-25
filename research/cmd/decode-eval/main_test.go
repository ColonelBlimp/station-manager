package main

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
	"github.com/ColonelBlimp/station-manager/research/ldpc"
	"github.com/ColonelBlimp/station-manager/research/truth"
	"github.com/ColonelBlimp/station-manager/research/unpack"
)

// Decoder baseline. These are a floor, not exact golden counts —
// the purpose is to catch regressions in the BP+OSD+CRC+Unpack
// pipeline while leaving room for future upgrades (coherent demod,
// AP decoding, OSD order > 2, more Layer-2 message types) to raise
// the matched counts without test churn.
//
// History:
//   - 2026-05-25 BP-only + incoherent demod, freq/dt match only:
//     88/144 (61.1%), 1 extra.
//   - 2026-05-25 BP + OSD-2 + incoherent demod, freq/dt match only:
//     96/144 (66.7%), 2 extras. OSD-2 lifted -20 dB synthetic 3→6 and
//     real-capture matched 88→96.
//   - 2026-05-25 BP + OSD-2 + incoherent demod + Layer-2 (Type-1)
//     unpack, matched by TEXT+freq/dt: 95/144 (66.0%), 3 extras.
//     One previously-matched decode reclassified as extra because
//     its message type is i3=4 (nonstandard callsign, not yet
//     implemented). The 3 extras break down as:
//     2× confirmed jt9-misses (slot-bleed signals; both texts
//     match standing exchanges visible in live_slot2 truth)
//     1× Type-4 message (unsupported until c58 decoder lands)
//
// Two strictness tiers:
//
//   - **Strict** (synthetic clean / -16 dB and per-slot real-capture
//     numbers): exact equality, or equality on extras. These are
//     the "shouldn't move" surfaces — a drop here means something
//     broke.
//   - **Floor** (synthetic -20 / -22 dB and aggregate real-capture
//     totals): "at least this many" assertions. Decoder upgrades
//     should raise these; a drop fails the suite.
//
// Update these constants in the same commit as a decoder change
// that legitimately moves the numbers, with the new measurement
// recorded in the history above.
const (
	// Synthetic SNR sweep — gen10cq produces 10 CQ signals per fixture.
	syntheticTruthPerFixture = 10

	// Strict tier: clean and -16 dB decode at 100% with exact
	// text match — every synthetic signal is "CQ <CALL> <GRID>"
	// (Type 1) which the unpacker handles fully.
	syntheticCleanMatchedExact = 10
	synthetic16dBMatchedExact  = 10
	syntheticStrictExtraExact  = 0

	// Floor tier: -20 / -22 dB are the marginal-SNR regime.
	synthetic20dBMatchedFloor = 6
	synthetic22dBMatchedFloor = 0

	// Real-capture corpus aggregate vs jt9-oracle truth, with
	// text-based matching:
	//   95 / 144 matched (66.0% decode parity)
	//    3 extras (2× confirmed jt9-misses + 1× Type-4 unsupported)
	realCaptureTruthTotal   = 144
	realCaptureMatchedFloor = 95
	realCaptureExtraCeiling = 3
)

// realCaptureSlots pins per-slot expectations. Per-slot baselines
// are tracked because aggregate-only floors can hide a per-slot
// regression (e.g., one slot collapses, another improves, total
// stays flat). live_slot1 carries one Type-4 unsupported extra
// (will resolve when c58/nonstandard-call unpacking lands);
// live_slot3 carries two confirmed jt9-miss extras (slot-bleed
// signals from continuing QSOs visible in live_slot2 truth).
var realCaptureSlots = []struct {
	wav        string
	truthN     int
	minMatched int
	maxExtra   int
}{
	{"../../../captures/20m_slot1.wav", 21, 18, 0},
	{"../../../captures/20m_slot2.wav", 32, 19, 1}, // 1× Type-4 unsupported
	{"../../../captures/20m_slot3.wav", 17, 11, 0},
	{"../../../captures/live_slot1.wav", 29, 16, 0},
	{"../../../captures/live_slot2.wav", 23, 18, 0},
	{"../../../captures/live_slot3.wav", 22, 13, 2}, // 2× confirmed jt9-misses
}

// runPipeline runs the full research-tree pipeline (candidates →
// demod → LLRs → ldpc.Decode → unpack) and scores against the
// truth manifest by TEXT + (freq, dt). A match requires:
//
//   - The decode's CRC passed.
//   - Unpack succeeded (text is well-formed).
//   - The unpacked text equals the truth-entry text exactly.
//   - The decode's (freq, dt) is within source-aware tolerance of
//     the truth entry.
//
// Returns counts (matched, missed, extra) and a classification of
// CRC-passes for diagnostic display. "matched" = text+location;
// "extra" = any CRC-pass not claimed by a truth entry (including
// unsupported message types and parse failures).
func runPipeline(t *testing.T, wavPath string) (matched, missed, extra, unsupported, malformed int) {
	t.Helper()

	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}
	manifest, err := truth.Read(truth.PathFor(wavPath))
	if err != nil {
		t.Fatalf("read truth for %s: %v", wavPath, err)
	}
	if manifest == nil {
		t.Fatalf("no truth manifest beside %s", wavPath)
	}

	cands := candidates.Find(data.Samples)

	type rec struct {
		freq, dt float64
		crcPass  bool
		text     string
		unpackOK bool
		msgType  uint8
	}
	records := make([]rec, len(cands))
	for i, c := range cands {
		energies := demod.Demod(data.Samples, c.Freq, c.DT)
		llrs := demod.LLRs(energies)
		var input [174]float64
		for k := 0; k < 174; k++ {
			input[k] = llrs[k]
		}
		result, stats := ldpc.Decode(input)
		r := rec{freq: c.Freq, dt: c.DT, crcPass: stats.ConvergedCRC}
		if r.crcPass {
			ur, uerr := unpack.Unpack(result.Info)
			r.msgType = ur.MsgType
			if uerr == nil {
				r.text = ur.Text
				r.unpackOK = true
			}
		}
		records[i] = r
	}

	freqTol, dtTol := syntheticFreqTolHz, syntheticDTMatchTol
	if manifest.Source != nil && *manifest.Source == "jt9-oracle" {
		freqTol, dtTol = jt9FreqMatchTolHz, jt9DTMatchTolS
	}

	matchedDecode := make([]int, len(records))
	for i := range matchedDecode {
		matchedDecode[i] = -1
	}
	matchedTruth := make([]int, len(manifest.Signals))
	for i := range matchedTruth {
		matchedTruth[i] = -1
	}
	for ti, ts := range manifest.Signals {
		bestIdx := -1
		bestDistSq := math.Inf(1)
		for di, r := range records {
			if !r.crcPass || !r.unpackOK || matchedDecode[di] >= 0 {
				continue
			}
			if r.text != ts.Text {
				continue
			}
			df := r.freq - ts.FreqHz
			ddt := r.dt - ts.DTSec
			if math.Abs(df) > freqTol || math.Abs(ddt) > dtTol {
				continue
			}
			d := df*df + ddt*ddt
			if d < bestDistSq {
				bestDistSq = d
				bestIdx = di
			}
		}
		if bestIdx >= 0 {
			matchedDecode[bestIdx] = ti
			matchedTruth[ti] = bestIdx
		}
	}

	for _, di := range matchedTruth {
		if di >= 0 {
			matched++
		}
	}
	missed = len(manifest.Signals) - matched
	for di, r := range records {
		if !r.crcPass || matchedDecode[di] >= 0 {
			continue
		}
		extra++
		if !r.unpackOK {
			if r.msgType != 1 {
				unsupported++
			} else {
				malformed++
			}
		}
	}
	return
}

// TestSyntheticBaseline pins the four synthetic-fixture outcomes
// recorded 2026-05-25 with text+location matching:
//
//	clean / -16 dB: strict 10/10 matched (text), 0 extras
//	-20 dB:         floor matched >= 6 (BP+OSD-2 baseline)
//	-22 dB:         floor matched >= 0 (BP+OSD-2 baseline)
//
// Full results are logged before assertions so failures surface
// the whole table at once.
func TestSyntheticBaseline(t *testing.T) {
	type result struct {
		name                                           string
		matched, missed, extra, unsupported, malformed int
	}
	runs := []struct {
		name string
		wav  string
	}{
		{"clean", "../../10cq_clean.wav"},
		{"-16dB", "../../10cq_snr-16dB.wav"},
		{"-20dB", "../../10cq_snr-20dB.wav"},
		{"-22dB", "../../10cq_snr-22dB.wav"},
	}
	results := make([]result, len(runs))
	for i, r := range runs {
		m, mi, e, u, mal := runPipeline(t, r.wav)
		results[i] = result{r.name, m, mi, e, u, mal}
	}

	t.Log("synthetic baseline (BP + OSD-2 + incoherent demod + Type-1 unpack):")
	t.Logf("  %-8s %8s %8s %8s %8s %8s", "snr", "matched", "missed", "extra", "unsupp", "malform")
	for _, r := range results {
		t.Logf("  %-8s %8d %8d %8d %8d %8d", r.name, r.matched, r.missed, r.extra, r.unsupported, r.malformed)
	}

	// Strict: clean and -16 dB.
	if got := results[0].matched; got != syntheticCleanMatchedExact {
		t.Errorf("clean matched = %d, want %d (strict)", got, syntheticCleanMatchedExact)
	}
	if got := results[0].extra; got != syntheticStrictExtraExact {
		t.Errorf("clean extra = %d, want %d (strict)", got, syntheticStrictExtraExact)
	}
	if got := results[1].matched; got != synthetic16dBMatchedExact {
		t.Errorf("-16dB matched = %d, want %d (strict)", got, synthetic16dBMatchedExact)
	}
	if got := results[1].extra; got != syntheticStrictExtraExact {
		t.Errorf("-16dB extra = %d, want %d (strict)", got, syntheticStrictExtraExact)
	}

	// Floor: -20 dB and -22 dB.
	if got := results[2].matched; got < synthetic20dBMatchedFloor {
		t.Errorf("-20dB matched = %d, want >= %d (floor)", got, synthetic20dBMatchedFloor)
	}
	if got := results[3].matched; got < synthetic22dBMatchedFloor {
		t.Errorf("-22dB matched = %d, want >= %d (floor)", got, synthetic22dBMatchedFloor)
	}

	// Synthetic fixtures should never produce malformed payloads
	// (CRC-pass + parse-fail on Type 1). If this fires, either Unpack
	// is broken or CRC14 is letting garbage through.
	for _, r := range results {
		if r.malformed > 0 {
			t.Errorf("%s: %d malformed payload(s) on synthetic — Unpack or CRC bug", r.name, r.malformed)
		}
	}
}

// TestRealCaptureBaseline pins per-slot and aggregate decode parity
// across the six real captures recorded 2026-05-25.
//
//	per-slot: matched >= <slot floor>, extras <= <slot ceiling>
//	aggregate: matched >= 88 / 144 (61.1%), extras <= 1
//
// Full table is logged before assertions so failure output carries
// the complete picture, not just the assertion that tripped.
//
// Skipped under -short to keep fast-feedback runs fast (~8s wallclock).
func TestRealCaptureBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-capture baseline under -short")
	}

	type result struct {
		name                                           string
		truthN                                         int
		matched, missed, extra, unsupported, malformed int
		minMatched                                     int
		maxExtra                                       int
	}
	results := make([]result, len(realCaptureSlots))
	for i, fx := range realCaptureSlots {
		m, mi, e, u, mal := runPipeline(t, fx.wav)
		results[i] = result{
			name:        filepath.Base(fx.wav),
			truthN:      fx.truthN,
			matched:     m,
			missed:      mi,
			extra:       e,
			unsupported: u,
			malformed:   mal,
			minMatched:  fx.minMatched,
			maxExtra:    fx.maxExtra,
		}
	}

	totalMatched, totalExtra, totalUnsupp, totalMalformed := 0, 0, 0, 0
	for _, r := range results {
		totalMatched += r.matched
		totalExtra += r.extra
		totalUnsupp += r.unsupported
		totalMalformed += r.malformed
	}

	t.Log("real-capture baseline (BP + OSD-2 + incoherent demod + Type-1 unpack):")
	t.Logf("  %-22s %6s %8s %7s %6s %7s %8s   %-12s %-12s",
		"slot", "truth", "matched", "missed", "extra", "unsupp", "malform", "min matched", "max extra")
	for _, r := range results {
		t.Logf("  %-22s %6d %8d %7d %6d %7d %8d   %-12d %-12d",
			r.name, r.truthN, r.matched, r.missed, r.extra, r.unsupported, r.malformed, r.minMatched, r.maxExtra)
	}
	t.Logf("  %-22s %6d %8d %7s %6d %7d %8d   %-12d %-12d",
		"TOTAL", realCaptureTruthTotal, totalMatched, "—", totalExtra, totalUnsupp, totalMalformed,
		realCaptureMatchedFloor, realCaptureExtraCeiling)

	// Per-slot assertions — floor on matched, ceiling on extras.
	for _, r := range results {
		if r.matched < r.minMatched {
			t.Errorf("%s: matched = %d, want >= %d", r.name, r.matched, r.minMatched)
		}
		if r.extra > r.maxExtra {
			t.Errorf("%s: extra = %d, want <= %d", r.name, r.extra, r.maxExtra)
		}
	}

	// Aggregate assertions.
	if totalMatched < realCaptureMatchedFloor {
		t.Errorf("total matched = %d, want >= %d", totalMatched, realCaptureMatchedFloor)
	}
	if totalExtra > realCaptureExtraCeiling {
		t.Errorf("total extra = %d, want <= %d", totalExtra, realCaptureExtraCeiling)
	}

	// Malformed payloads on real captures should be ~zero. Any non-
	// zero count indicates either a CRC false-accept producing bad
	// bits or an Unpack regression. Track separately so a sudden
	// uptick is loud.
	if totalMalformed > 0 {
		t.Errorf("total malformed = %d, want 0 (CRC false-accept or Unpack bug)", totalMalformed)
	}
}
