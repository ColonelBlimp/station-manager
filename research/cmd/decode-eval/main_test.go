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
)

// Decoder baseline. These are a floor, not exact golden counts —
// the purpose is to catch regressions in the BP+OSD+CRC pipeline
// while leaving room for future upgrades (coherent demod, AP
// decoding, OSD order > 2) to raise the matched counts without
// test churn.
//
// History:
//   - 2026-05-25 BP-only + incoherent demod: 88/144 (61.1%), 1 extra.
//   - 2026-05-25 BP + OSD-2 + incoherent demod: 96/144 (66.7%), 2 extras.
//     -20 dB synthetic went 3→6; real-capture matched went 88→96.
//     One additional extra on live_slot3 (now 2 total). The extras
//     remain inconclusive without Layer-2 unpacking — could be
//     slot-bleed signals jt9 didn't decode, or rare CRC false accepts.
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

	// Strict tier: clean and -16 dB decode at 100% — both BP-only
	// and BP+OSD-2 land here trivially.
	syntheticCleanMatchedExact = 10
	synthetic16dBMatchedExact  = 10
	syntheticStrictExtraExact  = 0

	// Floor tier: -20 / -22 dB are the marginal-SNR regime.
	// BP-only got 3/10 at -20 dB; BP+OSD-2 reaches 6/10. -22 dB
	// is still 0/10 — below OSD-2's reach without further upgrades
	// (order >2, AP, or coherent demod).
	synthetic20dBMatchedFloor = 6
	synthetic22dBMatchedFloor = 0

	// Real-capture corpus aggregate vs jt9-oracle truth:
	//   96 / 144 matched (66.7% decode parity) under BP + OSD-2
	//    2 extras (live_slot3 — inconclusive, see history above)
	realCaptureTruthTotal   = 144
	realCaptureMatchedFloor = 96
	realCaptureExtraCeiling = 2
)

// realCaptureSlots pins per-slot expectations. Per-slot baselines
// are tracked because aggregate-only floors can hide a per-slot
// regression (e.g., one slot collapses, another improves, total
// stays flat). The extras ceiling is 0 everywhere except live_slot3
// where the observed extras are the known outlier (BP-only saw 1;
// BP+OSD-2 sees 2 — see the history at the top of the file).
var realCaptureSlots = []struct {
	wav        string
	truthN     int
	minMatched int
	maxExtra   int
}{
	{"../../../captures/20m_slot1.wav", 21, 18, 0},
	{"../../../captures/20m_slot2.wav", 32, 20, 0},
	{"../../../captures/20m_slot3.wav", 17, 11, 0},
	{"../../../captures/live_slot1.wav", 29, 16, 0},
	{"../../../captures/live_slot2.wav", 23, 18, 0},
	{"../../../captures/live_slot3.wav", 22, 13, 2},
}

// runPipeline is the test-side mirror of main.go's decodeAll +
// scoring. Kept inline (rather than calling decodeAll directly)
// because decodeAll prints, which would flood test output. Match
// logic mirrors score() but returns counts instead of printing.
func runPipeline(t *testing.T, wavPath string) (matched, missed, extra int) {
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
	}
	records := make([]rec, len(cands))
	for i, c := range cands {
		energies := demod.Demod(data.Samples, c.Freq, c.DT)
		llrs := demod.LLRs(energies)
		var input [174]float64
		for k := 0; k < 174; k++ {
			input[k] = llrs[k]
		}
		_, stats := ldpc.Decode(input)
		records[i] = rec{freq: c.Freq, dt: c.DT, crcPass: stats.ConvergedCRC}
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
			if !r.crcPass || matchedDecode[di] >= 0 {
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
		if r.crcPass && matchedDecode[di] < 0 {
			extra++
		}
	}
	return matched, missed, extra
}

// TestSyntheticBaseline pins the four synthetic-fixture outcomes
// recorded 2026-05-25 (BP-only + incoherent demod).
//
//	clean / -16 dB: strict 10/10 matched, 0 extras
//	-20 dB:         floor matched >= 3 (BP-only baseline)
//	-22 dB:         floor matched >= 0 (BP-only baseline; OSD-2 should raise)
//
// Full results are logged before assertions so failures surface
// the whole table at once (Go's testing.T buffers t.Logf and
// flushes on test failure).
func TestSyntheticBaseline(t *testing.T) {
	type result struct {
		name                   string
		matched, missed, extra int
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
		m, mi, e := runPipeline(t, r.wav)
		results[i] = result{r.name, m, mi, e}
	}

	// Always log the table — visible with -v and on any failure.
	t.Log("synthetic baseline (BP + OSD-2 + incoherent demod):")
	t.Logf("  %-8s %8s %8s %8s", "snr", "matched", "missed", "extra")
	for _, r := range results {
		t.Logf("  %-8s %8d %8d %8d", r.name, r.matched, r.missed, r.extra)
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
		name                   string
		truthN                 int
		matched, missed, extra int
		minMatched             int
		maxExtra               int
	}
	results := make([]result, len(realCaptureSlots))
	for i, fx := range realCaptureSlots {
		m, mi, e := runPipeline(t, fx.wav)
		results[i] = result{
			name:       filepath.Base(fx.wav),
			truthN:     fx.truthN,
			matched:    m,
			missed:     mi,
			extra:      e,
			minMatched: fx.minMatched,
			maxExtra:   fx.maxExtra,
		}
	}

	totalMatched, totalExtra := 0, 0
	for _, r := range results {
		totalMatched += r.matched
		totalExtra += r.extra
	}

	// Always log the table — visible with -v and on any failure.
	t.Log("real-capture baseline (BP + OSD-2 + incoherent demod):")
	t.Logf("  %-22s %6s %8s %7s %6s   %-12s %-12s",
		"slot", "truth", "matched", "missed", "extra", "min matched", "max extra")
	for _, r := range results {
		t.Logf("  %-22s %6d %8d %7d %6d   %-12d %-12d",
			r.name, r.truthN, r.matched, r.missed, r.extra, r.minMatched, r.maxExtra)
	}
	t.Logf("  %-22s %6d %8d %7s %6d   %-12d %-12d",
		"TOTAL", realCaptureTruthTotal, totalMatched, "—", totalExtra,
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
}
