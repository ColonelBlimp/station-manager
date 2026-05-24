// find-candidates loads every .wav file directly under the supplied
// directory (default: research/), each into its own buffer, and runs
// the research-stage candidates.Find on each. For each input it
// prints the detected candidate list.
//
// If a ground-truth manifest sits next to the WAV (foo.wav →
// foo.truth.json), each detection is scored as matched (close to a
// known signal), spurious (no nearby known signal), or missed
// (known signal with no nearby detection). Match tolerances:
// freqMatchTolHz Hz frequency, dtMatchTolS seconds timing.
//
// Usage:
//
//	go run ./research/cmd/find-candidates              # walks research/
//	go run ./research/cmd/find-candidates -dir PATH    # walks PATH/
//
// Only top-level .wav files in the directory are scanned (no recursion).
// Files that aren't 12 kHz mono are skipped with a warning.
//
// Import rules: research code may use internal/audio (and stdlib +
// its own packages) but MUST NOT import internal/ft8/* — the
// candidate-finder work iterates independently of SM's FT8 stack.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

// expectedSampleRate is the FT8 canonical 12 kHz. Hardcoded here
// rather than imported from internal/ft8/dsp per the research-tree
// import rule (no internal/ft8/* dependencies).
const expectedSampleRate = 12000

// Ground-truth match tolerances. A detected candidate is considered
// a match for a truth signal if both axes are within bounds. Sized
// generously vs the sync grid (3.125 Hz freq, 40 ms time): half-bin
// quantisation alone could put a perfect detection up to 1.56 Hz /
// 20 ms off the truth value, so ±2 Hz / ±0.1 s leaves comfortable
// headroom for the basic detector without admitting genuinely
// wrong-position detections.
const (
	freqMatchTolHz = 2.0
	dtMatchTolS    = 0.1
)

func main() {
	dir := flag.String("dir", "research", "directory containing .wav files (non-recursive)")
	flag.Parse()

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("read dir %q: %v", *dir, err)
	}

	var wavs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".wav" {
			continue
		}
		wavs = append(wavs, filepath.Join(*dir, e.Name()))
	}
	sort.Strings(wavs)

	if len(wavs) == 0 {
		log.Fatalf("no .wav files found in %q", *dir)
	}

	fmt.Printf("Found %d .wav file(s) in %s/\n\n", len(wavs), *dir)

	for _, wavPath := range wavs {
		data, err := audio.ReadWAV(wavPath)
		if err != nil {
			log.Printf("read %q: %v — skipping", wavPath, err)
			continue
		}
		if data.SampleRate != expectedSampleRate {
			log.Printf("%q: sample rate %d Hz, want %d — skipping",
				wavPath, data.SampleRate, expectedSampleRate)
			continue
		}
		if data.Channels != 1 {
			log.Printf("%q: %d channels, want mono — skipping",
				wavPath, data.Channels)
			continue
		}

		fmt.Printf("=== %s ===\n", wavPath)
		fmt.Printf("  %d samples @ %d Hz (%.2f s)\n",
			len(data.Samples), data.SampleRate,
			float64(len(data.Samples))/float64(data.SampleRate))

		// Soft-load any ground-truth manifest sitting next to the WAV.
		// Missing file is fine — Read returns (nil, nil) in that case.
		truthPath := truth.PathFor(wavPath)
		manifest, err := truth.Read(truthPath)
		if err != nil {
			log.Printf("  read truth %q: %v — scoring skipped", truthPath, err)
		} else if manifest != nil {
			fmt.Printf("  truth manifest: %s (%d signals)\n", truthPath, len(manifest.Signals))
		} else {
			fmt.Println("  truth manifest: (none)")
		}

		cands := candidates.Find(data.Samples)
		printCandidates(cands, manifest)
		fmt.Println()
	}
}

// printCandidates renders the detection list and, if a manifest was
// supplied, annotates each detection as OK / FP and lists any MISS.
func printCandidates(cands []candidates.Candidate, manifest *truth.Manifest) {
	if len(cands) == 0 {
		fmt.Println("  candidates: (none)")
	} else {
		fmt.Printf("  candidates (%d):\n", len(cands))
	}

	// Without a manifest, just print the raw list.
	if manifest == nil {
		for i, c := range cands {
			fmt.Printf("    %2d. freq=%8.2f Hz  dt=%+.3f s  score=%.3f\n",
				i+1, c.Freq, c.DT, c.Score)
		}
		return
	}

	// Greedy match: for each truth signal walk the detection list and
	// pick the closest unmatched one within tolerance. Squared
	// (df, ddt) is fine for ranking since both axes share the same
	// tolerance shape.
	matchedDetection := make([]int, len(cands)) // truth-index per detection; -1 for spurious
	for i := range matchedDetection {
		matchedDetection[i] = -1
	}
	matchedTruth := make([]int, len(manifest.Signals)) // detection-index per truth; -1 for miss
	for i := range matchedTruth {
		matchedTruth[i] = -1
	}
	for ti, ts := range manifest.Signals {
		bestIdx := -1
		bestDistSq := math.Inf(1)
		for di, c := range cands {
			if matchedDetection[di] >= 0 {
				continue
			}
			df := c.Freq - ts.FreqHz
			ddt := c.DT - ts.DTSec
			if math.Abs(df) > freqMatchTolHz || math.Abs(ddt) > dtMatchTolS {
				continue
			}
			d := df*df + ddt*ddt
			if d < bestDistSq {
				bestDistSq = d
				bestIdx = di
			}
		}
		if bestIdx >= 0 {
			matchedDetection[bestIdx] = ti
			matchedTruth[ti] = bestIdx
		}
	}

	for i, c := range cands {
		verifyStr := "(no verify)"
		if c.Verify != nil {
			v := c.Verify
			verifyStr = fmt.Sprintf("wins=%d/21 [b0:%d b1:%d b2:%d] geo=%.2f minBlk=%.2f",
				v.WinsTotal, v.WinsBlock[0], v.WinsBlock[1], v.WinsBlock[2],
				v.GeoContrast, v.MinBlockContrast)
		}
		if ti := matchedDetection[i]; ti >= 0 {
			ts := manifest.Signals[ti]
			df := c.Freq - ts.FreqHz
			ddt := c.DT - ts.DTSec
			fmt.Printf("    %2d. freq=%8.2f Hz  dt=%+.3f s  s1=%.2f  %s  OK  %s (df=%+.2f Hz ddt=%+.3f s)\n",
				i+1, c.Freq, c.DT, c.Score, verifyStr, ts.Text, df, ddt)
		} else {
			fmt.Printf("    %2d. freq=%8.2f Hz  dt=%+.3f s  s1=%.2f  %s  FP  no match within %.1f Hz x %.2f s\n",
				i+1, c.Freq, c.DT, c.Score, verifyStr, freqMatchTolHz, dtMatchTolS)
		}
	}

	// Missed-truth listing.
	var missed []int
	for ti, di := range matchedTruth {
		if di < 0 {
			missed = append(missed, ti)
		}
	}
	if len(missed) > 0 {
		fmt.Printf("  missed (%d):\n", len(missed))
		for _, ti := range missed {
			ts := manifest.Signals[ti]
			fmt.Printf("    - %s at %.2f Hz dt=%+.3f s\n", ts.Text, ts.FreqHz, ts.DTSec)
		}
	}

	matched := len(manifest.Signals) - len(missed)
	spurious := 0
	for _, ti := range matchedDetection {
		if ti < 0 {
			spurious++
		}
	}
	fmt.Printf("  truth: %d matched, %d missed, %d spurious\n", matched, len(missed), spurious)
}
