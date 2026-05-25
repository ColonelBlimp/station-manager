package candidates

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

// TestDebugRealCaptureMisses walks each jt9-truth manifest under
// captures/, finds signals our pipeline misses, and classifies each
// miss by evaluating the actual pipeline lattice within the
// truth-match tolerance window.
//
// **Important — lattice vs continuous coordinate.** The pipeline
// sweeps a spectrogram grid of 3.125 Hz freq bins × 40 ms time
// steps. jt9 reports continuous (freq, dt) values that may sit
// off-grid by up to half a cell (1.56 Hz, 20 ms). Evaluating
// `verifyCostas` at jt9's exact coordinates over-reports the
// pipeline's accessible quality, because the closest grid point can
// have very different metrics.
//
// **Pick by truth-nearest bin, not by best-metric bin.** Earlier
// versions of this probe ranked all in-tolerance lattice points
// and picked the one with the best gate metrics. That breaks for
// weak signals adjacent to strong ones: a strong neighbour's
// sidelobe inside the tolerance window will outscore the real
// signal at its own bin, producing a fake "PASS_GATE" classification
// when the genuine miss is WINS_LOW at the truth-true bin. Session
// 92 hit this on live_slot1 PE1NPS and live_slot3 HG60IPA — both
// classified PASS_GATE but actually WINS_LOW at the real-signal
// bin 721 (the strong OM3DX at bin 723 leaked into bin 722).
//
// Current policy: choose the freq bin closest to the truth freq,
// then within that bin pick the dt-step with the best stage-2
// metrics. This honestly characterises the real-signal bin, not a
// sidelobe. A sidelobe-risk flag is set when a different in-tolerance
// bin has materially better metrics than the picked bin (≥2 more
// wins) — the operator may want to investigate those separately.
//
// Per-miss output reports the truth coordinate, the best lattice
// coordinate, the (df, ddt) offset, stage-1 score at the lattice
// point, wins/block/geo, the gate result, and the sidelobe flag.
func TestDebugRealCaptureMisses(t *testing.T) {
	const capturesDir = "../../captures"

	type miss struct {
		wav  string
		text string
		freq float64
		dt   float64
	}

	wavs, err := filepath.Glob(filepath.Join(capturesDir, "*.wav"))
	if err != nil {
		t.Fatalf("glob captures: %v", err)
	}
	if len(wavs) == 0 {
		t.Skipf("no .wav files in %q — skipping miss probe", capturesDir)
	}
	sort.Strings(wavs)

	var allMisses []miss
	for _, wavPath := range wavs {
		data, err := audio.ReadWAV(wavPath)
		if err != nil {
			t.Logf("read %q: %v — skipping", wavPath, err)
			continue
		}
		manifest, err := truth.Read(truth.PathFor(wavPath))
		if err != nil || manifest == nil {
			continue
		}

		cands := Find(data.Samples)
		const (
			freqTol = 5.0
			dtTol   = 0.3
		)
		matchedTruth := make(map[int]bool)
		matchedDet := make(map[int]bool)
		for ti, ts := range manifest.Signals {
			bestIdx := -1
			bestDistSq := math.Inf(1)
			for di, c := range cands {
				if matchedDet[di] {
					continue
				}
				df := c.Freq - ts.FreqHz
				ddt := c.DT - ts.DTSec
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
				matchedDet[bestIdx] = true
				matchedTruth[ti] = true
			}
		}

		for ti, ts := range manifest.Signals {
			if matchedTruth[ti] {
				continue
			}
			allMisses = append(allMisses, miss{
				wav: filepath.Base(wavPath), text: ts.Text,
				freq: ts.FreqHz, dt: ts.DTSec,
			})
		}
	}

	if len(allMisses) == 0 {
		t.Log("no misses across all real captures")
		return
	}

	const (
		freqTol         = 5.0
		dtTol           = 0.3
		searchHalfSpanS = 2.5 // match the pipeline (Find uses 2.5, not 2.0)
		df              = fs / nfft1
		tstep           = float64(nstep) / fs
	)

	// Reused for stage-1 evaluation at each grid point.
	nominalStartStep := int(math.Floor(0.5 / tstep))

	t.Logf("Probing %d misses across %d captures (lattice: %.4f Hz × %.0f ms):",
		len(allMisses), len(wavs), df, tstep*1000)
	t.Logf("")
	t.Logf("%-22s | %-26s | %-15s | %-15s | %-10s | %-7s | %-22s | gate",
		"slot", "text", "truth (f, dt)", "best grid", "Δf,Δdt", "s1", "wins/[b0,b1,b2]/geo")
	t.Logf("%s", strings.Repeat("-", 175))

	// Cache spectrograms per file (re-read across misses).
	specCache := make(map[string][][]float64)
	samplesCache := make(map[string][]float32)

	for _, m := range allMisses {
		// DT out-of-range vs the pipeline's search span.
		if math.Abs(m.dt) > searchHalfSpanS {
			t.Logf("%-22s | %-26s | %7.1f %+7.3f | %-15s | %-10s | %-7s | %-22s | DT_OUT_OF_RANGE (|dt|=%.3f > %.1f)",
				m.wav, truncate(m.text, 26), m.freq, m.dt, "-", "-", "-", "-",
				math.Abs(m.dt), searchHalfSpanS)
			continue
		}

		// Load samples + spectrogram once per file.
		samples, ok := samplesCache[m.wav]
		if !ok {
			data, err := audio.ReadWAV(filepath.Join(capturesDir, m.wav))
			if err != nil {
				t.Logf("read %q: %v — skipping miss", m.wav, err)
				continue
			}
			samples = data.Samples
			samplesCache[m.wav] = samples
			specCache[m.wav] = spectrogram(samples)
		}
		spec := specCache[m.wav]

		// Enumerate every grid point inside the (freq, dt) tolerance window.
		binCentre := int(math.Round(m.freq / df))
		binRadius := int(math.Floor(freqTol/df)) + 1
		stepRadius := int(math.Floor(dtTol/tstep)) + 1

		type gridEval struct {
			centreBin  int
			rawDtSteps int
			freq       float64
			dt         float64
			s1         float64
			v          CostasVerify
			passGate   bool
			whyFailed  string
		}
		var evals []gridEval
		for bin := binCentre - binRadius; bin <= binCentre+binRadius; bin++ {
			freq := float64(bin) * df
			if math.Abs(freq-m.freq) > freqTol {
				continue
			}
			for steps := -stepRadius; steps <= stepRadius; steps++ {
				// rawDtSteps is in pre-physical frame; physical dt is
				// rawDtSteps*tstep + dtPhysicalOffsetSec.
				rawDtSteps := int(math.Round((m.dt-dtPhysicalOffsetSec)/tstep)) + steps
				physicalDT := float64(rawDtSteps)*tstep + dtPhysicalOffsetSec
				if math.Abs(physicalDT-m.dt) > dtTol {
					continue
				}

				s1 := costasScore(spec, bin, rawDtSteps, nominalStartStep)
				v := verifyCostas(samples, freq, physicalDT, s1)

				passGate := true
				why := ""
				if v.WinsTotal < sanityWinsTotal {
					passGate = false
					why = fmt.Sprintf("WINS_LOW (wins=%d)", v.WinsTotal)
				} else {
					for b := 0; b < numCostasBlocks; b++ {
						if v.AccessibleBlock[b] == 0 {
							continue
						}
						if v.WinsBlock[b] < sanityWinsPerBlock {
							passGate = false
							why = fmt.Sprintf("BLOCK_LOW (blk%d=%d)", b, v.WinsBlock[b])
							break
						}
					}
				}
				// Stage-1 failure is reported on the best grid point's
				// stage-1 score below; the gate logic above mirrors the
				// pipeline's stage-2 gate exactly.

				evals = append(evals, gridEval{
					centreBin: bin, rawDtSteps: rawDtSteps,
					freq: freq, dt: physicalDT,
					s1: s1, v: v, passGate: passGate, whyFailed: why,
				})
			}
		}

		if len(evals) == 0 {
			t.Logf("%-22s | %-26s | %7.1f %+7.3f | %-15s | %-10s | %-7s | %-22s | NO_GRID_POINTS_IN_TOLERANCE",
				m.wav, truncate(m.text, 26), m.freq, m.dt, "-", "-", "-", "-")
			continue
		}

		// Group evals by freq bin and pick the bin closest to truth.
		// Within that bin, pick the dt-step with the best stage-2
		// metrics (wins desc, geo desc).
		binByDist := make(map[int][]gridEval)
		for _, e := range evals {
			binByDist[e.centreBin] = append(binByDist[e.centreBin], e)
		}
		var targetBin int
		bestBinDist := math.Inf(1)
		for bin := range binByDist {
			binFreq := float64(bin) * df
			d := math.Abs(binFreq - m.freq)
			if d < bestBinDist {
				bestBinDist = d
				targetBin = bin
			}
		}
		binEvals := binByDist[targetBin]
		sort.Slice(binEvals, func(i, j int) bool {
			a, b := binEvals[i], binEvals[j]
			if a.v.WinsTotal != b.v.WinsTotal {
				return a.v.WinsTotal > b.v.WinsTotal
			}
			return a.v.GeoContrast > b.v.GeoContrast
		})
		best := binEvals[0]

		// Sidelobe-risk annotation: any other bin in the tolerance
		// window with materially better wins than the picked bin?
		// Threshold of +2 wins is conservative — small differences
		// can be noise jitter, but a 2-win advantage suggests another
		// signal contribution.
		sidelobeRisk := ""
		for bin, others := range binByDist {
			if bin == targetBin {
				continue
			}
			topWins := 0
			topGeo := 0.0
			for _, e := range others {
				if e.v.WinsTotal > topWins ||
					(e.v.WinsTotal == topWins && e.v.GeoContrast > topGeo) {
					topWins = e.v.WinsTotal
					topGeo = e.v.GeoContrast
				}
			}
			if topWins >= best.v.WinsTotal+2 {
				binFreq := float64(bin) * df
				sidelobeRisk = fmt.Sprintf(" SIDELOBE_RISK(bin %.2f wins=%d>%d)",
					binFreq, topWins, best.v.WinsTotal)
				break
			}
		}

		var gateStr string
		switch {
		case best.passGate && best.s1 < stage1ScoreThreshold:
			// Passes gate at stage-2 metrics but stage-1 would have
			// filtered. The pipeline runs stage-1 first, so this means
			// the lattice point's stage-2 metrics looked OK but the
			// candidate wouldn't even reach stage-2 from the pipeline's
			// own enumeration. Annotate.
			gateStr = fmt.Sprintf("STAGE1_LOW (s1=%.2f < %.1f) | s2-would-PASS",
				best.s1, stage1ScoreThreshold)
		case best.passGate:
			gateStr = "PASS_GATE"
		default:
			gateStr = fmt.Sprintf("FAIL: %s", best.whyFailed)
		}

		t.Logf("%-22s | %-26s | %7.1f %+7.3f | %7.2f %+7.3f | %+5.2f,%+5.3f | %7.3f | %2d/[%d,%d,%d] geo=%5.3f | %s%s",
			m.wav, truncate(m.text, 26),
			m.freq, m.dt,
			best.freq, best.dt,
			best.freq-m.freq, best.dt-m.dt,
			best.s1,
			best.v.WinsTotal, best.v.WinsBlock[0], best.v.WinsBlock[1], best.v.WinsBlock[2],
			best.v.GeoContrast,
			gateStr, sidelobeRisk,
		)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
