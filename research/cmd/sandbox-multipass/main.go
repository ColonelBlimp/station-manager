// sandbox-multipass exercises MultiPassDecode on a labelled WAV
// fixture and reports pass-1 vs pass-2 decode coverage against the
// truth manifest. Acceptance harness for the multi-pass subtraction
// loop:
//
//   - single-signal fixtures: pass 2 must not add duplicates of
//     pass-1 decodes.
//   - crowded fixtures: pass 2 may recover decodes pass 1 missed
//     (overlap-unmasked signals).
//   - none: pass 2 must not introduce false text decodes.
//
// Usage:
//
//	go run ./research/cmd/sandbox-multipass -wav research/10cq_clean.wav
//	go run ./research/cmd/sandbox-multipass -wav research/10cq_snr-20dB.wav
//	go run ./research/cmd/sandbox-multipass -wav research/10cq_snr-20dB.wav -single  # pass-1 only baseline
//
// Import rules: research code may use internal/audio (and stdlib +
// research/*) but MUST NOT import internal/ft8/*.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/sandbox"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

const expectedSampleRate = 12000

func main() {
	wavPath := flag.String("wav", "research/10cq_clean.wav", "path to the WAV to decode")
	singlePass := flag.Bool("single", false, "disable multi-pass (single-pass baseline)")
	freqTol := flag.Float64("ftol", 5.0, "freq tolerance for truth matching (Hz)")
	dtTol := flag.Float64("dttol", 0.5, "dt tolerance for truth matching (s)")
	flag.Parse()

	data, err := audio.ReadWAV(*wavPath)
	if err != nil {
		log.Fatalf("read %q: %v", *wavPath, err)
	}
	if data.SampleRate != expectedSampleRate {
		log.Fatalf("%q: sample rate %d Hz, want %d", *wavPath, data.SampleRate, expectedSampleRate)
	}
	if data.Channels != 1 {
		log.Fatalf("%q: %d channels, want mono", *wavPath, data.Channels)
	}

	manifest, err := truth.Read(truth.PathFor(*wavPath))
	if err != nil {
		log.Fatalf("read truth manifest: %v", err)
	}
	if manifest == nil {
		log.Fatalf("no truth manifest beside %q", *wavPath)
	}

	opts := sandbox.DefaultMultiPassOptions()
	if *singlePass {
		opts.MaxPasses = 1
	}

	fmt.Printf("=== %s ===\n", *wavPath)
	fmt.Printf("  %d samples @ %d Hz; %d truth signals; max-passes=%d\n\n",
		len(data.Samples), data.SampleRate, len(manifest.Signals), opts.MaxPasses)

	ht := sandbox.NewCallsignHashTable()
	decodes := sandbox.MultiPassDecodeWithHashes(data.Samples, opts, ht)

	// Group by pass.
	byPass := map[int][]sandbox.DecodeRecord{}
	for _, d := range decodes {
		byPass[d.Pass] = append(byPass[d.Pass], d)
	}
	for _, p := range []int{1, 2} {
		fmt.Printf("  Pass %d: %d decodes\n", p, len(byPass[p]))
		recs := byPass[p]
		sort.Slice(recs, func(i, j int) bool { return recs[i].FreqHz < recs[j].FreqHz })
		for _, d := range recs {
			fmt.Printf("    %7.2f Hz  dt=%+6.3f s  %-12s  %s\n",
				d.FreqHz, d.DtSec, d.DecodeMethod, d.Text)
		}
		fmt.Println()
	}

	// Truth match.
	matched := 0
	missed := 0
	matchedTruth := make([]bool, len(manifest.Signals))
	for _, d := range decodes {
		for ti, sig := range manifest.Signals {
			if matchedTruth[ti] {
				continue
			}
			if math.Abs(d.FreqHz-sig.FreqHz) <= *freqTol &&
				math.Abs(d.DtSec-sig.DTSec) <= *dtTol &&
				d.Text == sig.Text {
				matchedTruth[ti] = true
				matched++
				break
			}
		}
	}
	for ti, m := range matchedTruth {
		if !m {
			missed++
			fmt.Printf("    MISS: %s (truth %7.2f Hz dt=%+5.3f)\n",
				manifest.Signals[ti].Text,
				manifest.Signals[ti].FreqHz, manifest.Signals[ti].DTSec)
		}
	}

	// Count duplicates (same text appearing multiple times in the result).
	textCounts := map[string]int{}
	for _, d := range decodes {
		textCounts[d.Text]++
	}
	duplicates := 0
	for _, c := range textCounts {
		if c > 1 {
			duplicates += c - 1
		}
	}

	// False positives: decodes whose text isn't in the truth manifest.
	falsePositives := 0
	truthTexts := map[string]bool{}
	for _, sig := range manifest.Signals {
		truthTexts[sig.Text] = true
	}
	for _, d := range decodes {
		if !truthTexts[d.Text] {
			falsePositives++
		}
	}

	fmt.Printf("\n  truth: %d matched / %d, %d missed; %d duplicates; %d false-positive text decodes\n",
		matched, len(manifest.Signals), missed, duplicates, falsePositives)
}
