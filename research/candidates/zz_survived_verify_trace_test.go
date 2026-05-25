package candidates

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

// TestTraceSurvivedVerify chases the genuine PASS_GATE finder-misses
// surfaced by the corrected on-lattice classification in
// TestDebugRealCaptureMisses. A signal whose best lattice point
// passes BOTH the stage-1 floor and the stage-2 gate cleanly, yet
// doesn't appear in Find()'s output, is either:
//
//   - NMS-suppressed by an adjacent stronger candidate
//   - Sort-and-cap-displaced (below the stage2MaxResults rank)
//   - Refined out of truth tolerance
//   - Lost in the truth-matcher's greedy assignment (a different truth
//     signal claimed the same Find()-output candidate first)
//
// Two cases were surfaced on the current corpus:
//
//	live_slot1.wav: PE1NPS <...> +02      at (2253.0, +0.100)
//	live_slot3.wav: <...> HG60IPA RR73    at (2253.0, +0.100)
//
// Both have a best lattice point near (2256.25, +0.2x) with healthy
// metrics (wins ≥ 13, geo ≥ 1.05). This trace walks each through
// the pipeline stages to identify the actual loss point.
func TestTraceSurvivedVerify(t *testing.T) {
	const capturesDir = "../../captures"

	cases := []struct {
		wav  string
		text string
		freq float64
		dt   float64
	}{
		{"live_slot1.wav", "PE1NPS <...> +02", 2253.0, +0.100},
		{"live_slot3.wav", "<...> HG60IPA RR73", 2253.0, +0.100},
	}

	for _, tc := range cases {
		t.Run(tc.wav+"_"+strings.ReplaceAll(tc.text, " ", "_"), func(t *testing.T) {
			tracePassGateMiss(t, filepath.Join(capturesDir, tc.wav), tc.text, tc.freq, tc.dt)
		})
	}
}

func tracePassGateMiss(t *testing.T, wavPath, truthText string, truthFreq, truthDT float64) {
	const (
		freqTolHz = 5.0
		dtTolSec  = 0.3
	)

	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Skipf("read %q: %v", wavPath, err)
	}
	manifestPath := truth.PathFor(wavPath)
	manifest, _ := truth.Read(manifestPath)

	near := func(freq, dt float64) bool {
		return math.Abs(freq-truthFreq) <= freqTolHz && math.Abs(dt-truthDT) <= dtTolSec
	}

	t.Logf("=== Target: \"%s\" at (%.1f Hz, %+.3f s) in %s ===", truthText, truthFreq, truthDT, filepath.Base(wavPath))

	if len(data.Samples) < nmax {
		t.Fatalf("buffer too short: %d < %d", len(data.Samples), nmax)
	}
	spec := spectrogram(data.Samples)
	if len(spec) == 0 {
		t.Fatalf("empty spectrogram")
	}

	const (
		freqLowHz       = 200.0
		freqHighHz      = 2900.0
		searchHalfSpanS = 2.5
		df              = fs / nfft1
		tstep           = float64(nstep) / fs
	)

	nominalStartStep := int(math.Floor(0.5 / tstep))
	halfSpanSteps := int(math.Round(searchHalfSpanS / tstep))
	dtStepsMin := -halfSpanSteps - dtPhysicalOffsetSteps
	dtStepsMax := halfSpanSteps - dtPhysicalOffsetSteps

	halfFFT := len(spec[0])
	binLow := int(math.Round(freqLowHz / df))
	if binLow < 0 {
		binLow = 0
	}
	binHigh := int(math.Round(freqHighHz / df))
	if binHigh+freqOversample*maxToneIdx >= halfFFT {
		binHigh = halfFFT - freqOversample*maxToneIdx - 1
	}

	// ---- Stage 1: enumerate every grid point, sort by score ----
	var stage1 []rawCandidate
	for centreBin := binLow; centreBin <= binHigh; centreBin++ {
		for dtSteps := dtStepsMin; dtSteps <= dtStepsMax; dtSteps++ {
			score := costasScore(spec, centreBin, dtSteps, nominalStartStep)
			if score >= stage1ScoreThreshold {
				stage1 = append(stage1, rawCandidate{
					centreBin:  centreBin,
					rawDtSteps: dtSteps,
					score:      score,
				})
			}
		}
	}
	sort.Slice(stage1, func(i, j int) bool {
		return stage1[i].score > stage1[j].score
	})

	// Locate the "best lattice point" by repeating the corrected-probe's
	// selection (passing gate preferred, then geo→wins→minBlock→s1).
	type latticeEval struct {
		idx      int // index in stage1 (descending score order)
		raw      rawCandidate
		freq     float64
		dt       float64
		v        CostasVerify
		passGate bool
	}
	var hits []latticeEval
	for i, c := range stage1 {
		freq := float64(c.centreBin) * df
		dt := float64(c.rawDtSteps)*tstep + dtPhysicalOffsetSec
		if !near(freq, dt) {
			continue
		}
		v := verifyCostas(data.Samples, freq, dt, c.score)
		passGate := v.WinsTotal >= sanityWinsTotal
		if passGate {
			for b := 0; b < numCostasBlocks; b++ {
				if v.AccessibleBlock[b] == 0 {
					continue
				}
				if v.WinsBlock[b] < sanityWinsPerBlock {
					passGate = false
					break
				}
			}
		}
		hits = append(hits, latticeEval{idx: i, raw: c, freq: freq, dt: dt, v: v, passGate: passGate})
	}
	// Rank exactly like the corrected probe.
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.passGate != b.passGate {
			return a.passGate
		}
		if a.passGate {
			if a.v.GeoContrast != b.v.GeoContrast {
				return a.v.GeoContrast > b.v.GeoContrast
			}
			if a.v.WinsTotal != b.v.WinsTotal {
				return a.v.WinsTotal > b.v.WinsTotal
			}
			if a.v.MinBlockContrast != b.v.MinBlockContrast {
				return a.v.MinBlockContrast > b.v.MinBlockContrast
			}
			return a.raw.score > b.raw.score
		}
		if a.v.WinsTotal != b.v.WinsTotal {
			return a.v.WinsTotal > b.v.WinsTotal
		}
		return a.v.GeoContrast > b.v.GeoContrast
	})
	if len(hits) == 0 {
		t.Logf("no lattice points found within tolerance")
		return
	}
	best := hits[0]
	t.Logf("Best lattice point: freq=%.2f dt=%+.3f  s1=%.3f wins=%d/[%d,%d,%d] geo=%.3f gate=%v",
		best.freq, best.dt, best.raw.score, best.v.WinsTotal,
		best.v.WinsBlock[0], best.v.WinsBlock[1], best.v.WinsBlock[2],
		best.v.GeoContrast, best.passGate)

	// Per-bin scan: for each freq bin within tolerance, find the
	// best (any dt-step) metrics. Reveals whether the "best lattice
	// point" picked by ranking is a genuine signal or a sidelobe
	// from a stronger adjacent signal.
	t.Logf("Per-bin scan (best dt-step per bin):")
	t.Logf("  %-5s %-7s %-7s %-7s %-7s %-7s", "bin", "freq", "dt", "s1", "wins", "geo")
	binToBest := make(map[int]latticeEval)
	for _, h := range hits {
		cur, ok := binToBest[h.raw.centreBin]
		if !ok || h.v.WinsTotal > cur.v.WinsTotal ||
			(h.v.WinsTotal == cur.v.WinsTotal && h.v.GeoContrast > cur.v.GeoContrast) {
			binToBest[h.raw.centreBin] = h
		}
	}
	bins := make([]int, 0, len(binToBest))
	for b := range binToBest {
		bins = append(bins, b)
	}
	sort.Ints(bins)
	for _, b := range bins {
		h := binToBest[b]
		marker := ""
		if h.raw.centreBin == best.raw.centreBin && h.raw.rawDtSteps == best.raw.rawDtSteps {
			marker = " ← picked as best"
		}
		t.Logf("  %5d %7.2f %+7.3f %7.3f %7d %7.3f%s",
			h.raw.centreBin, h.freq, h.dt, h.raw.score, h.v.WinsTotal, h.v.GeoContrast, marker)
	}
	t.Logf("Stage-1 rank of best lattice point: %d / %d (threshold s1=%.2f, topK=%d)",
		best.idx+1, len(stage1), stage1ScoreThreshold, stage1TopK)
	if best.idx+1 > stage1TopK {
		t.Logf("  → would be CUT by stage1TopK cap")
	} else {
		t.Logf("  → survives stage1TopK cap")
	}

	// ---- Run the full Find pipeline alongside, and locate our candidate ----
	if len(stage1) > stage1TopK {
		stage1 = stage1[:stage1TopK]
	}

	type verifiedHit struct {
		raw      rawCandidate
		v        CostasVerify
		freq     float64
		dt       float64
		isTarget bool
	}

	verified := make([]rawCandidate, 0, len(stage1))
	var targetVerified *verifiedHit // pointer into verified[]; we'll resolve to index after sort
	for _, c := range stage1 {
		freq := float64(c.centreBin) * df
		physicalDT := float64(c.rawDtSteps)*tstep + dtPhysicalOffsetSec
		v := verifyCostas(data.Samples, freq, physicalDT, c.score)

		passGate := v.WinsTotal >= sanityWinsTotal
		if passGate {
			for b := 0; b < numCostasBlocks; b++ {
				if v.AccessibleBlock[b] == 0 {
					continue
				}
				if v.WinsBlock[b] < sanityWinsPerBlock {
					passGate = false
					break
				}
			}
		}
		if !passGate {
			continue
		}
		vCopy := v
		c.verify = &vCopy
		verified = append(verified, c)

		// Mark our best-lattice candidate (same centreBin + rawDtSteps).
		if c.centreBin == best.raw.centreBin && c.rawDtSteps == best.raw.rawDtSteps {
			h := verifiedHit{raw: c, v: vCopy, freq: freq, dt: physicalDT, isTarget: true}
			targetVerified = &h
		}
	}
	if targetVerified == nil {
		t.Logf("Best lattice point did NOT enter the verified set — its gate evaluation differs (unexpected)")
		return
	}
	t.Logf("Verified set size: %d (post stage-2 gate)", len(verified))

	// Sort by the pipeline's rank key.
	sort.Slice(verified, func(i, j int) bool {
		a, b := verified[i].verify, verified[j].verify
		if a.GeoContrast != b.GeoContrast {
			return a.GeoContrast > b.GeoContrast
		}
		if a.WinsTotal != b.WinsTotal {
			return a.WinsTotal > b.WinsTotal
		}
		if a.MinBlockContrast != b.MinBlockContrast {
			return a.MinBlockContrast > b.MinBlockContrast
		}
		return a.Stage1Score > b.Stage1Score
	})

	targetIdx := -1
	for i, c := range verified {
		if c.centreBin == best.raw.centreBin && c.rawDtSteps == best.raw.rawDtSteps {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		t.Fatalf("target lost after sort — impossible")
	}
	t.Logf("Verified rank of target: %d / %d", targetIdx+1, len(verified))

	// ---- NMS ----
	const (
		freqSuppressBins  = 2
		timeSuppressSteps = 3
	)
	keep := make([]bool, len(verified))
	suppressor := make([]int, len(verified))
	for i := range keep {
		keep[i] = true
		suppressor[i] = -1
	}
	for i := 0; i < len(verified); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(verified); j++ {
			if !keep[j] {
				continue
			}
			dbin := verified[i].centreBin - verified[j].centreBin
			if dbin < 0 {
				dbin = -dbin
			}
			dstep := verified[i].rawDtSteps - verified[j].rawDtSteps
			if dstep < 0 {
				dstep = -dstep
			}
			if dbin <= freqSuppressBins && dstep <= timeSuppressSteps {
				keep[j] = false
				suppressor[j] = i
			}
		}
	}

	if keep[targetIdx] {
		t.Logf("NMS: target SURVIVED")
	} else {
		supIdx := suppressor[targetIdx]
		supC := verified[supIdx]
		supFreq := float64(supC.centreBin) * df
		supDT := float64(supC.rawDtSteps)*tstep + dtPhysicalOffsetSec
		t.Logf("NMS: target SUPPRESSED by rank %d (freq=%.2f dt=%+.3f wins=%d/[%d,%d,%d] geo=%.3f s1=%.3f)",
			supIdx+1, supFreq, supDT,
			supC.verify.WinsTotal,
			supC.verify.WinsBlock[0], supC.verify.WinsBlock[1], supC.verify.WinsBlock[2],
			supC.verify.GeoContrast, supC.verify.Stage1Score)

		t.Logf("  Suppressor distance to target truth: df=%+.2f Hz ddt=%+.3f s",
			supFreq-truthFreq, supDT-truthDT)

		// Distance to every OTHER truth signal in the slot.
		if manifest != nil {
			t.Logf("  Suppressor distance to other truth signals in slot:")
			for _, ts := range manifest.Signals {
				if ts.Text == truthText {
					continue
				}
				df1 := supFreq - ts.FreqHz
				ddt1 := supDT - ts.DTSec
				if math.Abs(df1) <= freqTolHz*2 && math.Abs(ddt1) <= dtTolSec*2 {
					inTol := math.Abs(df1) <= freqTolHz && math.Abs(ddt1) <= dtTolSec
					tag := ""
					if inTol {
						tag = " ← within tolerance"
					}
					t.Logf("    \"%s\" at (%.1f, %+.3f): df=%+.2f ddt=%+.3f%s",
						ts.Text, ts.FreqHz, ts.DTSec, df1, ddt1, tag)
				}
			}
		}
	}

	// ---- Refinement (only on survivors) + final cap ----
	cands := Find(data.Samples)
	t.Logf("Find() returned %d candidates total", len(cands))

	// Locate our target in the final output (or note absence) and run
	// the truth-matcher exactly as the probe does so we can see who
	// (if anyone) claims our truth signal vs. the lattice-best
	// candidate.
	type matchTrace struct {
		di       int
		freq, dt float64
		dist     float64
	}

	// Per-truth claimant tracking via greedy match.
	matchedDet := make(map[int]bool)
	matchedTruth := make(map[int]int) // truth index → det index
	if manifest != nil {
		for ti, ts := range manifest.Signals {
			bestIdx := -1
			bestDistSq := math.Inf(1)
			for di, c := range cands {
				if matchedDet[di] {
					continue
				}
				df1 := c.Freq - ts.FreqHz
				ddt := c.DT - ts.DTSec
				if math.Abs(df1) > freqTolHz || math.Abs(ddt) > dtTolSec {
					continue
				}
				d := df1*df1 + ddt*ddt
				if d < bestDistSq {
					bestDistSq = d
					bestIdx = di
				}
			}
			if bestIdx >= 0 {
				matchedDet[bestIdx] = true
				matchedTruth[ti] = bestIdx
			}
		}
	}

	// Find the index of our specific truth signal.
	targetTruthIdx := -1
	if manifest != nil {
		for ti, ts := range manifest.Signals {
			if ts.Text == truthText && math.Abs(ts.FreqHz-truthFreq) < 0.1 && math.Abs(ts.DTSec-truthDT) < 0.05 {
				targetTruthIdx = ti
				break
			}
		}
	}
	if targetTruthIdx >= 0 {
		di, claimed := matchedTruth[targetTruthIdx]
		if claimed {
			c := cands[di]
			t.Logf("Truth-matcher: target truth CLAIMED det idx=%d (freq=%.2f dt=%+.3f) — shouldn't be a miss",
				di, c.Freq, c.DT)
		} else {
			t.Logf("Truth-matcher: target truth UNCLAIMED — confirms miss")

			// Any output candidate inside the target's tolerance?
			var nearby []matchTrace
			for di, c := range cands {
				df1 := c.Freq - truthFreq
				ddt := c.DT - truthDT
				if math.Abs(df1) <= freqTolHz && math.Abs(ddt) <= dtTolSec {
					nearby = append(nearby, matchTrace{di: di, freq: c.Freq, dt: c.DT, dist: math.Sqrt(df1*df1 + ddt*ddt)})
				}
			}
			t.Logf("  Find() output candidates within target tolerance: %d", len(nearby))
			for _, mt := range nearby {
				c := cands[mt.di]
				claimedBy := -1
				for ti, di2 := range matchedTruth {
					if di2 == mt.di {
						claimedBy = ti
						break
					}
				}
				if claimedBy >= 0 {
					ts := manifest.Signals[claimedBy]
					t.Logf("    det idx=%d (freq=%.2f dt=%+.3f) — CLAIMED-BY truth \"%s\" at (%.1f, %+.3f)",
						mt.di, c.Freq, c.DT, ts.Text, ts.FreqHz, ts.DTSec)
				} else {
					t.Logf("    det idx=%d (freq=%.2f dt=%+.3f) — unclaimed",
						mt.di, c.Freq, c.DT)
				}
			}
		}
	}
}
