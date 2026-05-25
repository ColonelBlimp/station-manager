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
	// text-based matching. Each unmatched CRC-pass is classified
	// into exactly one of textExtra / unsupported / malformed —
	// these are disjoint counters tracked independently so the
	// next decoder change (coherent demod) can be A/B'd without
	// either category masking the other.
	//
	//   95 / 144 matched (66.0% decode parity)
	//    2 textExtra   — confirmed jt9-misses on live_slot3
	//                    ("DN9GLA SV2AJX KN10", "9A2KS SK7WS RR73");
	//                    both are real continuing exchanges from
	//                    QSOs visible in live_slot2 truth.
	//    1 unsupported — 1× Type-4 (nonstandard call, i3=4) on 20m_slot2;
	//                    will become matched once c58 unpacker lands.
	//                    NOT a decoder failure.
	//    0 malformed   — strictly enforced; non-zero would indicate
	//                    CRC false-accept on supported type or an
	//                    Unpack regression.
	realCaptureTruthTotal         = 144
	realCaptureMatchedFloor       = 95
	realCaptureTextExtraCeiling   = 2
	realCaptureUnsupportedCeiling = 1
	// malformed must be exactly 0 — asserted directly, no constant needed.
)

// realCaptureSlots pins per-slot expectations. Per-slot baselines
// are tracked because aggregate-only floors can hide a per-slot
// regression (e.g., one slot collapses, another improves, total
// stays flat). textExtra and unsupported are tracked separately
// — coherent demod work shouldn't be allowed to grow textExtra
// silently even if unsupported drops, or vice versa.
//
// Known classifications in the current corpus:
//   - 20m_slot2: 1× Type-4 unsupported (jt9 reports the call as
//     a nonstandard form; c58 decoder will resolve this once landed)
//   - live_slot3: 2× confirmed jt9-misses, both real signals
//     observable as continuations of QSOs in live_slot2's truth
var realCaptureSlots = []struct {
	wav            string
	truthN         int
	minMatched     int
	maxTextExtra   int
	maxUnsupported int
}{
	{"../../../captures/20m_slot1.wav", 21, 18, 0, 0},
	{"../../../captures/20m_slot2.wav", 32, 19, 0, 1}, // 1× Type-4 unsupported
	{"../../../captures/20m_slot3.wav", 17, 11, 0, 0},
	{"../../../captures/live_slot1.wav", 29, 16, 0, 0},
	{"../../../captures/live_slot2.wav", 23, 18, 0, 0},
	{"../../../captures/live_slot3.wav", 22, 13, 2, 0}, // 2× confirmed jt9-misses
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
// Returns disjoint counters: matched / missed / textExtra /
// unsupported / malformed. Every CRC-pass falls into exactly one
// of matched / textExtra / unsupported / malformed (sum equals
// CRC-pass count). Independent counters so future decoder changes
// can be A/B'd without one category masking another.
func runPipeline(t *testing.T, wavPath string) (matched, missed, textExtra, unsupported, malformed int) {
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
		if !r.unpackOK {
			if r.msgType != 1 {
				unsupported++
			} else {
				malformed++
			}
		} else {
			textExtra++
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
		name                                               string
		matched, missed, textExtra, unsupported, malformed int
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
		m, mi, te, u, mal := runPipeline(t, r.wav)
		results[i] = result{r.name, m, mi, te, u, mal}
	}

	t.Log("synthetic baseline (BP + OSD-2 + incoherent demod + Type-1 unpack):")
	t.Logf("  %-8s %8s %8s %10s %8s %8s", "snr", "matched", "missed", "textExtra", "unsupp", "malform")
	for _, r := range results {
		t.Logf("  %-8s %8d %8d %10d %8d %8d", r.name, r.matched, r.missed, r.textExtra, r.unsupported, r.malformed)
	}

	// Strict: clean and -16 dB. All synthetic signals are Type 1, so
	// any unsupported/malformed/textExtra at these SNRs is a bug.
	for i, snr := range []string{"clean", "-16dB"} {
		exact := syntheticCleanMatchedExact
		if i == 1 {
			exact = synthetic16dBMatchedExact
		}
		r := results[i]
		if r.matched != exact {
			t.Errorf("%s matched = %d, want %d (strict)", snr, r.matched, exact)
		}
		if r.textExtra+r.unsupported+r.malformed != syntheticStrictExtraExact {
			t.Errorf("%s total extras = %d (text=%d unsupp=%d malf=%d), want %d (strict)",
				snr, r.textExtra+r.unsupported+r.malformed, r.textExtra, r.unsupported, r.malformed, syntheticStrictExtraExact)
		}
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
		name                                               string
		truthN                                             int
		matched, missed, textExtra, unsupported, malformed int
		minMatched                                         int
		maxTextExtra                                       int
		maxUnsupported                                     int
	}
	results := make([]result, len(realCaptureSlots))
	for i, fx := range realCaptureSlots {
		m, mi, te, u, mal := runPipeline(t, fx.wav)
		results[i] = result{
			name:           filepath.Base(fx.wav),
			truthN:         fx.truthN,
			matched:        m,
			missed:         mi,
			textExtra:      te,
			unsupported:    u,
			malformed:      mal,
			minMatched:     fx.minMatched,
			maxTextExtra:   fx.maxTextExtra,
			maxUnsupported: fx.maxUnsupported,
		}
	}

	totalMatched, totalTextExtra, totalUnsupp, totalMalformed := 0, 0, 0, 0
	for _, r := range results {
		totalMatched += r.matched
		totalTextExtra += r.textExtra
		totalUnsupp += r.unsupported
		totalMalformed += r.malformed
	}

	t.Log("real-capture baseline (BP + OSD-2 + incoherent demod + Type-1 unpack):")
	t.Logf("  %-22s %6s %8s %7s %10s %7s %8s   %-10s %-10s %-10s",
		"slot", "truth", "matched", "missed", "textExtra", "unsupp", "malform", "minMatch", "maxTextEx", "maxUnsupp")
	for _, r := range results {
		t.Logf("  %-22s %6d %8d %7d %10d %7d %8d   %-10d %-10d %-10d",
			r.name, r.truthN, r.matched, r.missed, r.textExtra, r.unsupported, r.malformed,
			r.minMatched, r.maxTextExtra, r.maxUnsupported)
	}
	t.Logf("  %-22s %6d %8d %7s %10d %7d %8d   %-10d %-10d %-10s",
		"TOTAL", realCaptureTruthTotal, totalMatched, "—", totalTextExtra, totalUnsupp, totalMalformed,
		realCaptureMatchedFloor, realCaptureTextExtraCeiling, "—")

	// Per-slot assertions — floor on matched, ceiling on each
	// extras category separately so a regression in one can't be
	// masked by an improvement in another.
	for _, r := range results {
		if r.matched < r.minMatched {
			t.Errorf("%s: matched = %d, want >= %d", r.name, r.matched, r.minMatched)
		}
		if r.textExtra > r.maxTextExtra {
			t.Errorf("%s: textExtra = %d, want <= %d", r.name, r.textExtra, r.maxTextExtra)
		}
		if r.unsupported > r.maxUnsupported {
			t.Errorf("%s: unsupported = %d, want <= %d", r.name, r.unsupported, r.maxUnsupported)
		}
		if r.malformed > 0 {
			t.Errorf("%s: malformed = %d, want 0 (CRC false-accept or Unpack bug)", r.name, r.malformed)
		}
	}

	// Aggregate assertions — each category tracked independently.
	if totalMatched < realCaptureMatchedFloor {
		t.Errorf("total matched = %d, want >= %d", totalMatched, realCaptureMatchedFloor)
	}
	if totalTextExtra > realCaptureTextExtraCeiling {
		t.Errorf("total textExtra = %d, want <= %d (likely CRC false-accept or coherent-demod regression)",
			totalTextExtra, realCaptureTextExtraCeiling)
	}
	if totalUnsupp > realCaptureUnsupportedCeiling {
		t.Errorf("total unsupported = %d, want <= %d (a new message type appeared in the corpus?)",
			totalUnsupp, realCaptureUnsupportedCeiling)
	}
	if totalMalformed > 0 {
		t.Errorf("total malformed = %d, want 0 (CRC false-accept on supported type or Unpack regression)",
			totalMalformed)
	}
}
