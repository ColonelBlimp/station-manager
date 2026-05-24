package candidates

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// TestDebugStage1Distribution counts how many stage-1 candidates
// pass at each s1 threshold across the synthetic SNR sweep + real
// captures. This is the budget number — every candidate that passes
// stage 1 costs ~210 µs in stage 2, so the cumulative count IS the
// verify budget.
func TestDebugStage1Distribution(t *testing.T) {
	fixtures := []struct {
		path string
	}{
		{"../10cq_clean.wav"},
		{"../10cq_snr-16dB.wav"},
		{"../10cq_snr-20dB.wav"},
		{"../10cq_snr-22dB.wav"},
	}
	captures, _ := filepath.Glob("../../captures/*.wav")
	sort.Strings(captures)
	for _, p := range captures {
		fixtures = append(fixtures, struct{ path string }{p})
	}

	const (
		df    = fs / nfft1
		tstep = float64(nstep) / fs

		freqLowHz       = 200.0
		freqHighHz      = 2900.0
		searchHalfSpanS = 2.5
	)

	thresholds := []float64{1.0, 1.1, 1.2, 1.3, 1.4, 1.5, 1.7, 2.0}

	t.Logf("%-30s |%s", "fixture", " counts at s1 >= each threshold")
	header := fmt.Sprintf("%-30s |", "")
	for _, th := range thresholds {
		header += fmt.Sprintf(" %5.1f ", th)
	}
	t.Log(header)

	for _, fx := range fixtures {
		data, err := audio.ReadWAV(fx.path)
		if err != nil {
			continue
		}
		spec := spectrogram(data.Samples)
		if len(spec) == 0 {
			continue
		}

		halfFFT := len(spec[0])
		binLow := int(math.Round(freqLowHz / df))
		binHigh := int(math.Round(freqHighHz / df))
		if binHigh+freqOversample*maxToneIdx >= halfFFT {
			binHigh = halfFFT - freqOversample*maxToneIdx - 1
		}
		halfSpanSteps := int(math.Round(searchHalfSpanS / tstep))
		dtStepsMin := -halfSpanSteps - dtPhysicalOffsetSteps
		dtStepsMax := halfSpanSteps - dtPhysicalOffsetSteps
		nominalStartStep := int(math.Floor(0.5 / tstep))

		var scores []float64
		for centreBin := binLow; centreBin <= binHigh; centreBin++ {
			for dtSteps := dtStepsMin; dtSteps <= dtStepsMax; dtSteps++ {
				s := costasScore(spec, centreBin, dtSteps, nominalStartStep)
				if s >= 1.0 {
					scores = append(scores, s)
				}
			}
		}

		counts := make([]int, len(thresholds))
		for _, s := range scores {
			for i, th := range thresholds {
				if s >= th {
					counts[i]++
				}
			}
		}
		row := fmt.Sprintf("%-30s |", filepath.Base(fx.path))
		for _, c := range counts {
			row += fmt.Sprintf(" %5d ", c)
		}
		t.Log(row)
	}
}
