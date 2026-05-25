package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
	"github.com/ColonelBlimp/station-manager/research/ldpc"
	"github.com/ColonelBlimp/station-manager/research/truth"
	"github.com/ColonelBlimp/station-manager/research/unpack"
)

// slopeDiagRow captures one candidate's diagnostic record for the
// slope-vs-RMS / freq-refinement-vs-piecewise decision.
type slopeDiagRow struct {
	fixture     string
	freq        float64
	dt          float64
	hasTruth    bool   // any truth entry within (freq, dt) tolerance
	truthText   string // empty if !hasTruth
	decoded     bool   // CRC passed
	textMatched bool   // decoded AND unpacked text matches truthText
	slopeRad    float64
	absSlopeRad float64
	rmsRad      float64
	wins        int
	geo         float64
}

// runSlopeRMSDiag is the one-shot diagnostic that walks every WAV in
// dir, runs the full pipeline + Costas phase fit on every candidate,
// classifies it against the truth manifest, and prints per-candidate
// rows + summary buckets + a 3×3 slope-vs-RMS cross-tab.
//
// Purpose: decide between "phase-coherent freq refinement at the
// candidate stage" and "piecewise phase model" as the next coherent
// work. If the high-|slope| tail dominates real-signal candidates,
// freq refinement is the win. If low |slope| with high RMS dominates,
// piecewise is the win.
//
// Per the operator's directive (Session 90), this is a transient
// experiment: run once, classify the dominant failure mode, then
// build the corresponding fix. It is not integrated into the
// regression test suite.
func runSlopeRMSDiag(dir string) {
	entries, err := readWavDir(dir)
	if err != nil {
		fmt.Printf("read dir %q: %v\n", dir, err)
		return
	}
	if len(entries) == 0 {
		fmt.Printf("no .wav files found in %q\n", dir)
		return
	}

	var rows []slopeDiagRow
	for _, wavPath := range entries {
		data, err := audio.ReadWAV(wavPath)
		if err != nil {
			fmt.Printf("read %q: %v — skipping\n", wavPath, err)
			continue
		}
		if data.SampleRate != expectedSampleRate || data.Channels != 1 {
			continue
		}
		manifestPath := truth.PathFor(wavPath)
		manifest, _ := truth.Read(manifestPath)

		freqTol, dtTol := tolerancesFor(manifest)
		fixtureLabel := filepath.Base(wavPath)

		cands := candidates.Find(data.Samples)
		fmt.Printf("=== %s (%d candidates, %d truth) ===\n", fixtureLabel,
			len(cands), manifestSignalCount(manifest))

		for _, c := range cands {
			row := slopeDiagRow{
				fixture: fixtureLabel,
				freq:    c.Freq,
				dt:      c.DT,
			}
			if c.Verify != nil {
				row.wins = c.Verify.WinsTotal
				row.geo = c.Verify.GeoContrast
			}

			// Truth match by (freq, dt) only — we want to know whether
			// this candidate is at a real signal's location regardless
			// of whether the decoder recovered it.
			if manifest != nil {
				best := -1
				bestD := math.Inf(1)
				for ti, ts := range manifest.Signals {
					if math.Abs(c.Freq-ts.FreqHz) > freqTol || math.Abs(c.DT-ts.DTSec) > dtTol {
						continue
					}
					df := c.Freq - ts.FreqHz
					ddt := c.DT - ts.DTSec
					d := df*df + ddt*ddt
					if d < bestD {
						bestD = d
						best = ti
					}
				}
				if best >= 0 {
					row.hasTruth = true
					row.truthText = manifest.Signals[best].Text
				}
			}

			// Phase fit unconditional — same as DemodCoherent's first step.
			fit := demod.PhaseFitFor(data.Samples, c.Freq, c.DT)
			row.slopeRad = fit.Slope
			row.absSlopeRad = math.Abs(fit.Slope)
			row.rmsRad = fit.RMSResid

			// Incoherent decode (to know whether the current default
			// decodes this candidate). Coherent would be a separate
			// run; we already know coherent doesn't lift real captures.
			energies := demod.Demod(data.Samples, c.Freq, c.DT)
			llrs := demod.LLRs(energies)
			var input [174]float64
			for k := 0; k < 174; k++ {
				input[k] = llrs[k]
			}
			result, stats := ldpc.Decode(input)
			row.decoded = stats.ConvergedCRC
			if row.decoded {
				ur, uerr := unpack.Unpack(result.Info)
				if uerr == nil && row.hasTruth && ur.Text == row.truthText {
					row.textMatched = true
				}
			}

			rows = append(rows, row)
		}
		fmt.Println()
	}

	// Per-candidate rows for real-signal candidates only.
	fmt.Println("=== Per-candidate diagnostic rows (truth-matched candidates) ===")
	fmt.Printf("%-22s %8s %7s %4s %5s %5s %10s %8s %5s %6s\n",
		"fixture", "freq", "dt", "real", "dec", "text", "slopeRad", "rmsRad", "wins", "geo")
	for _, r := range rows {
		if !r.hasTruth {
			continue
		}
		fmt.Printf("%-22s %8.2f %+7.3f %4s %5s %5s %+10.4f %8s %5d %6.2f\n",
			r.fixture, r.freq, r.dt,
			boolTag(r.hasTruth), boolTag(r.decoded), boolTag(r.textMatched),
			r.slopeRad, formatRMS(r.rmsRad), r.wins, r.geo)
	}
	fmt.Println()

	// Summary over truth-matched (real-signal) candidates only.
	var realRows []slopeDiagRow
	for _, r := range rows {
		if r.hasTruth {
			realRows = append(realRows, r)
		}
	}
	fmt.Printf("=== Summary (%d real-signal candidates) ===\n\n", len(realRows))

	printAbsSlopeBuckets(realRows)
	fmt.Println()
	printRMSBuckets(realRows)
	fmt.Println()
	printCrossTab(realRows)
	fmt.Println()
	printDecodeBreakdownByBucket(realRows)
}

// readWavDir lists wav files (top level only, sorted) under dir.
func readWavDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var wavs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".wav" {
			continue
		}
		wavs = append(wavs, filepath.Join(dir, e.Name()))
	}
	sort.Strings(wavs)
	return wavs, nil
}

func manifestSignalCount(m *truth.Manifest) int {
	if m == nil {
		return 0
	}
	return len(m.Signals)
}

func boolTag(b bool) string {
	if b {
		return "Y"
	}
	return "n"
}

func formatRMS(rms float64) string {
	if math.IsInf(rms, 1) {
		return "+Inf"
	}
	return fmt.Sprintf("%.4f", rms)
}

func printAbsSlopeBuckets(rows []slopeDiagRow) {
	low, mid, high := 0, 0, 0
	for _, r := range rows {
		switch {
		case r.absSlopeRad < 0.1:
			low++
		case r.absSlopeRad < 0.3:
			mid++
		default:
			high++
		}
	}
	fmt.Println("|slope| buckets (rad/sym):")
	fmt.Printf("  < 0.1          : %4d (%5.1f%%)\n", low, pct(low, len(rows)))
	fmt.Printf("  0.1 .. < 0.3   : %4d (%5.1f%%)\n", mid, pct(mid, len(rows)))
	fmt.Printf("  >= 0.3         : %4d (%5.1f%%)\n", high, pct(high, len(rows)))
}

func printRMSBuckets(rows []slopeDiagRow) {
	low, mid, high, infN := 0, 0, 0, 0
	for _, r := range rows {
		switch {
		case math.IsInf(r.rmsRad, 1):
			infN++
		case r.rmsRad < 0.2:
			low++
		case r.rmsRad < 0.4:
			mid++
		default:
			high++
		}
	}
	fmt.Println("RMSResid buckets (rad):")
	fmt.Printf("  < 0.2          : %4d (%5.1f%%)\n", low, pct(low, len(rows)))
	fmt.Printf("  0.2 .. < 0.4   : %4d (%5.1f%%)\n", mid, pct(mid, len(rows)))
	fmt.Printf("  >= 0.4         : %4d (%5.1f%%)\n", high, pct(high, len(rows)))
	if infN > 0 {
		fmt.Printf("  +Inf (fit fail): %4d (%5.1f%%)\n", infN, pct(infN, len(rows)))
	}
}

func printCrossTab(rows []slopeDiagRow) {
	// 3×3 cross-tab |slope| × rms.
	var grid [3][3]int
	for _, r := range rows {
		s := absSlopeBucket(r.absSlopeRad)
		m := rmsBucket(r.rmsRad)
		grid[s][m]++
	}
	fmt.Println("Cross-tab: |slope| (rows) × RMS (cols)")
	fmt.Printf("  %-15s %10s %15s %12s\n", "", "rms<0.2", "0.2<=rms<0.4", "rms>=0.4")
	fmt.Printf("  %-15s %10d %15d %12d   ← refinement vs LLR-scale\n",
		"|slope|<0.1", grid[0][0], grid[0][1], grid[0][2])
	fmt.Printf("  %-15s %10d %15d %12d\n",
		"0.1<=|slope|<0.3", grid[1][0], grid[1][1], grid[1][2])
	fmt.Printf("  %-15s %10d %15d %12d   ← high slope = freq refinement\n",
		"|slope|>=0.3", grid[2][0], grid[2][1], grid[2][2])
	fmt.Println("                                  ↑ piecewise candidate")
}

func printDecodeBreakdownByBucket(rows []slopeDiagRow) {
	// For each cross-tab cell, how many decoded? Tells us which
	// failure modes BP+OSD+incoherent can still salvage.
	var decoded, total [3][3]int
	for _, r := range rows {
		s := absSlopeBucket(r.absSlopeRad)
		m := rmsBucket(r.rmsRad)
		total[s][m]++
		if r.textMatched {
			decoded[s][m]++
		}
	}
	fmt.Println("Decode rate per cell (text-matched / cell-total):")
	fmt.Printf("  %-15s %10s %15s %12s\n", "", "rms<0.2", "0.2<=rms<0.4", "rms>=0.4")
	rowLabel := [3]string{"|slope|<0.1", "0.1<=|slope|<0.3", "|slope|>=0.3"}
	for s := 0; s < 3; s++ {
		fmt.Printf("  %-15s %10s %15s %12s\n",
			rowLabel[s],
			rate(decoded[s][0], total[s][0]),
			rate(decoded[s][1], total[s][1]),
			rate(decoded[s][2], total[s][2]),
		)
	}
}

func absSlopeBucket(s float64) int {
	switch {
	case s < 0.1:
		return 0
	case s < 0.3:
		return 1
	default:
		return 2
	}
}

func rmsBucket(rms float64) int {
	switch {
	case math.IsInf(rms, 1):
		return 2 // count infinity as high
	case rms < 0.2:
		return 0
	case rms < 0.4:
		return 1
	default:
		return 2
	}
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

func rate(num, denom int) string {
	if denom == 0 {
		return "  -/-  "
	}
	return fmt.Sprintf("%2d/%-3d %3.0f%%", num, denom, 100*float64(num)/float64(denom))
}
