package candidates

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

// fixtureMatchTolHz / fixtureMatchTolS define the (freq, dt) tolerance
// used to match a detected candidate against a ground-truth signal in
// the four-fixture regression. Same values the find-candidates CLI uses
// — pinning them here means a CLI tolerance change is visible as a
// test diff.
const (
	fixtureMatchTolHz = 2.0
	fixtureMatchTolS  = 0.1
)

// TestFind_FixtureCounts pins the (matched, missed, spurious) tuple for
// each committed synthetic fixture against the current Find pipeline.
//
// These numbers are the ones to maintain as we tune the stage-1/stage-2
// gate. A change here is a calibration drift signal — when intentional,
// update the expected counts in the same commit as the gate change so
// reviewers can see the matched/missed/spurious deltas line by line.
//
// Fixtures live at research/*.wav with their truth manifests at
// research/*.truth.json (committed alongside the WAVs). Tests run from
// the package directory so the relative path "../<name>" resolves.
func TestFind_FixtureCounts(t *testing.T) {
	fixtures := []struct {
		wav          string
		wantMatched  int
		wantMissed   int
		wantSpurious int
	}{
		// After step-2 recalibration (drop GeoContrast hard floor;
		// stage 1 threshold 1.0, topK 10000): recall reaches 10/10
		// across every fixture down to -22 dB. The spurious counts
		// are the FP tail the ranking puts below the reals — those
		// candidates exist by design (precision is now downstream's
		// job, not the gate's). Don't tighten these without revisiting
		// the recalibration plan.
		{"10cq_clean.wav", 10, 0, 36},
		{"10cq_snr-16dB.wav", 10, 0, 65},
		{"10cq_snr-20dB.wav", 10, 0, 60},
		{"10cq_snr-22dB.wav", 10, 0, 60},
	}

	for _, tc := range fixtures {
		t.Run(tc.wav, func(t *testing.T) {
			wavPath := filepath.Join("..", tc.wav)
			truthPath := truth.PathFor(wavPath)

			data, err := audio.ReadWAV(wavPath)
			if err != nil {
				t.Fatalf("read wav %q: %v", wavPath, err)
			}
			manifest, err := truth.Read(truthPath)
			if err != nil {
				t.Fatalf("read manifest %q: %v", truthPath, err)
			}
			if manifest == nil {
				t.Fatalf("no manifest at %q", truthPath)
			}

			cands := Find(data.Samples)
			matched, missed, spurious := scoreFixture(cands, manifest.Signals)

			if matched != tc.wantMatched || missed != tc.wantMissed || spurious != tc.wantSpurious {
				t.Errorf("scoring drift: got (matched=%d, missed=%d, spurious=%d), want (%d, %d, %d)",
					matched, missed, spurious,
					tc.wantMatched, tc.wantMissed, tc.wantSpurious)
				dumpCandidates(t, cands, manifest.Signals)
			}
		})
	}
}

// scoreFixture mirrors the find-candidates CLI's greedy matcher so the
// test counts match the operator-visible output. For each truth signal
// it picks the closest unmatched candidate within tolerance; any
// candidate left unmatched at the end is spurious; any truth signal
// without a matching candidate is missed.
func scoreFixture(cands []Candidate, signals []truth.Signal) (matched, missed, spurious int) {
	matchedDet := make([]int, len(cands)) // truth-index per detection; -1 = spurious
	for i := range matchedDet {
		matchedDet[i] = -1
	}
	matchedTruth := make([]int, len(signals)) // detection-index per truth; -1 = miss
	for i := range matchedTruth {
		matchedTruth[i] = -1
	}

	for ti, ts := range signals {
		bestIdx := -1
		bestDistSq := math.Inf(1)
		for di, c := range cands {
			if matchedDet[di] >= 0 {
				continue
			}
			df := c.Freq - ts.FreqHz
			ddt := c.DT - ts.DTSec
			if math.Abs(df) > fixtureMatchTolHz || math.Abs(ddt) > fixtureMatchTolS {
				continue
			}
			d := df*df + ddt*ddt
			if d < bestDistSq {
				bestDistSq = d
				bestIdx = di
			}
		}
		if bestIdx >= 0 {
			matchedDet[bestIdx] = ti
			matchedTruth[ti] = bestIdx
		}
	}

	for _, di := range matchedTruth {
		if di < 0 {
			missed++
		} else {
			matched++
		}
	}
	for _, ti := range matchedDet {
		if ti < 0 {
			spurious++
		}
	}
	return matched, missed, spurious
}

// dumpCandidates is the diagnostic-on-failure helper. Prints the full
// detection list with per-candidate match status — when a calibration
// change makes the counts shift, the dump shows which signals moved
// across the OK/MISS/FP boundary.
func dumpCandidates(t *testing.T, cands []Candidate, signals []truth.Signal) {
	t.Helper()
	t.Logf("detections (%d):", len(cands))
	for i, c := range cands {
		matched := ""
		for _, ts := range signals {
			if math.Abs(c.Freq-ts.FreqHz) <= fixtureMatchTolHz && math.Abs(c.DT-ts.DTSec) <= fixtureMatchTolS {
				matched = " -> " + ts.Text
				break
			}
		}
		var wins, geo string
		if c.Verify != nil {
			wins = "wins=" + itoa(c.Verify.WinsTotal) + "/21"
			geo = " geo=" + ftoa(c.Verify.GeoContrast, 3)
		}
		t.Logf("  %2d. f=%.2f dt=%+.3f s1=%.2f %s%s%s", i+1, c.Freq, c.DT, c.Score, wins, geo, matched)
	}
	t.Logf("expected truth signals (%d):", len(signals))
	for _, ts := range signals {
		t.Logf("  - %s at %.2f Hz dt=%+.3f", ts.Text, ts.FreqHz, ts.DTSec)
	}
}

// itoa/ftoa avoid pulling fmt into the test for compactness in the dump
// — keeps the failure log skimmable. Small private helpers.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func ftoa(x float64, decimals int) string {
	mult := math.Pow10(decimals)
	rounded := math.Round(x * mult)
	whole := int(rounded / mult)
	frac := int(math.Abs(rounded)) % int(mult)
	out := itoa(whole) + "."
	// Pad fractional part with leading zeros.
	fs := itoa(frac)
	for len(fs) < decimals {
		fs = "0" + fs
	}
	return out + fs
}
