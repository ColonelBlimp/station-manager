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
// captures/, finds signals our pipeline misses, and probes the
// verifier directly at jt9's reported (freq, dt). The output
// categorises each miss by which stage rejected it:
//
//   - "DT out of range" — jt9's dt is outside Find's search window
//   - "wins below gate" — verifier ran but WinsTotal/WinsBlock failed
//   - "stage-1 below floor" — matched-filter score < stage1ScoreThreshold
//   - "in output but outside match tolerance" — we found it but
//     it landed > 5 Hz or > 0.3 s from jt9's value
//   - "in output as spurious" — same candidate is in our output
//     but the truth matcher's greedy assignment picked a closer one
func TestDebugRealCaptureMisses(t *testing.T) {
	// Find the captures/ directory relative to the package dir.
	// research/candidates/ -> repo root -> captures/
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

	// For each miss, run the diagnostic.
	const (
		searchHalfSpanS = 2.0
		df              = fs / nfft1
		tstep           = float64(nstep) / fs
	)

	t.Logf("Probing %d misses across %d captures:", len(allMisses), len(wavs))
	t.Logf("")
	t.Logf("%-22s | %-26s | %-7s | %-7s | diagnosis", "slot", "text", "freq", "dt")
	t.Logf("%s", strings.Repeat("-", 110))

	for _, m := range allMisses {
		// 1. Check DT in search range.
		if math.Abs(m.dt) > searchHalfSpanS {
			t.Logf("%-22s | %-26s | %7.1f | %+7.3f | DT_OUT_OF_RANGE (|dt|=%.3f > %.1f)",
				m.wav, truncate(m.text, 26), m.freq, m.dt, math.Abs(m.dt), searchHalfSpanS)
			continue
		}

		// 2. Run verifier at jt9's reported (freq, dt).
		data, _ := audio.ReadWAV(filepath.Join(capturesDir, m.wav))
		v := verifyCostas(data.Samples, m.freq, m.dt, 0)

		// 3. Compute stage-1 score at nearest grid point.
		spec := spectrogram(data.Samples)
		var s1 float64
		if len(spec) > 0 {
			centreBin := int(math.Round(m.freq / df))
			rawDtSteps := int(math.Round((m.dt - dtPhysicalOffsetSec) / tstep))
			nominalStartStep := int(math.Floor(0.5 / tstep))
			s1 = costasScore(spec, centreBin, rawDtSteps, nominalStartStep)
		}

		// 4. Diagnose.
		var diagnosis string
		switch {
		case s1 < stage1ScoreThreshold:
			diagnosis = fmt.Sprintf("STAGE1_LOW (s1=%.2f < %.1f)", s1, stage1ScoreThreshold)
		case v.WinsTotal < sanityWinsTotal:
			diagnosis = fmt.Sprintf("WINS_LOW (wins=%d < %d)", v.WinsTotal, sanityWinsTotal)
		case v.WinsBlock[0] < sanityWinsPerBlock || v.WinsBlock[1] < sanityWinsPerBlock || v.WinsBlock[2] < sanityWinsPerBlock:
			diagnosis = fmt.Sprintf("BLOCK_LOW (wins=%d [%d,%d,%d])",
				v.WinsTotal, v.WinsBlock[0], v.WinsBlock[1], v.WinsBlock[2])
		default:
			// Passes all gates — must have lost to NMS or refinement.
			diagnosis = fmt.Sprintf("SURVIVED_VERIFY (s1=%.2f wins=%d/21 geo=%.2f) — likely NMS/refinement",
				s1, v.WinsTotal, v.GeoContrast)
		}

		t.Logf("%-22s | %-26s | %7.1f | %+7.3f | %s",
			m.wav, truncate(m.text, 26), m.freq, m.dt, diagnosis)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
