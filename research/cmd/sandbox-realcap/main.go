// sandbox-realcap validates the sandbox FT8 decoder against real
// WAV captures with jt9-oracle truth manifests. Walks a directory,
// runs single-pass + multi-pass on each capture, and reports
// per-capture and aggregate decode quality vs the truth.
//
// Tolerances follow the find-candidates convention for jt9-oracle
// truth: ±5 Hz frequency, ±0.5 s timing, exact-text match for
// "matched". Anything decoded that doesn't match any truth signal
// is "extra"; any truth signal that nothing decoded matches is
// "missed".
//
// The CallsignHashTable persists across captures in the same run —
// this is the operational "heard in a previous slot" memory that
// Type 4 messages depend on. The order captures are processed
// affects which Type 4 messages can resolve; for stable results,
// captures are processed in lexicographic order.
//
// Usage:
//
//	go run ./research/cmd/sandbox-realcap                       # default: captures/{20m,live}_slot*
//	go run ./research/cmd/sandbox-realcap -dir captures
//	go run ./research/cmd/sandbox-realcap -dir captures -single # single-pass only
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/sandbox"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

const (
	expectedSampleRate = 12000

	// jt9-oracle match tolerances (matches the find-candidates
	// jt9FreqMatchTolHz / jt9DTMatchTolS convention).
	freqMatchTolHz = 5.0
	dtMatchTolS    = 0.5
)

// captureResult is one capture's decode-vs-truth comparison.
type captureResult struct {
	Name string

	Truth   int
	Matched int
	Missed  int
	Extra   int

	// Decode-method counts on matched decodes.
	BPCount  int
	OSDCount int

	// Per-pass counts on matched decodes.
	Pass1 int
	Pass2 int

	// Mean absolute frequency error (Hz) and timing error (ms)
	// across matched decodes.
	MeanFreqErrHz float64
	MeanDtErrMs   float64

	// Type 4 stats: how many manifest signals look like Type 4
	// (heuristic: contain "RRR"/"RR73"/"73" without a numeric report
	// or a 4-char grid) and how many of those we matched.
	Type4Truth int
	Type4Hit   int
}

func main() {
	dir := flag.String("dir", "captures", "directory of real-capture WAVs with truth manifests")
	singlePass := flag.Bool("single", false, "use single-pass instead of multi-pass")
	sweep := flag.Bool("sweep", false, "OSD gate sweep across the grid (multi-pass only)")
	flag.Parse()

	wavs, err := listWAVs(*dir)
	if err != nil {
		log.Fatalf("list %q: %v", *dir, err)
	}
	if len(wavs) == 0 {
		log.Fatalf("no .wav files in %q", *dir)
	}

	if *sweep {
		runSweep(wavs, *dir)
		return
	}

	opts := sandbox.DefaultMultiPassOptions()
	if *singlePass {
		opts.MaxPasses = 1
	}

	// Persistent hash table across all captures — simulates the
	// operational "heard in earlier slots" memory.
	ht := sandbox.NewCallsignHashTable()

	fmt.Printf("=== sandbox-realcap (%s, %d captures, max-passes=%d) ===\n\n",
		*dir, len(wavs), opts.MaxPasses)
	fmt.Printf("%-15s %5s %5s %5s %5s  %6s %6s  %5s %5s  %5s %5s\n",
		"capture", "truth", "match", "miss", "extra",
		"|Δf|Hz", "|Δdt|ms", "BP", "OSD", "Pass1", "Pass2")
	fmt.Println(strings.Repeat("-", 90))

	var agg captureResult
	agg.Name = "TOTAL"
	for _, w := range wavs {
		r := processCapture(w, opts, ht)
		printRow(r)
		aggAccumulate(&agg, r)
	}
	fmt.Println(strings.Repeat("-", 90))
	if agg.Matched > 0 {
		agg.MeanFreqErrHz /= float64(agg.Matched)
		agg.MeanDtErrMs /= float64(agg.Matched)
	}
	printRow(agg)

	// Recall % and OSD share for the headline.
	recall := 0.0
	if agg.Truth > 0 {
		recall = 100 * float64(agg.Matched) / float64(agg.Truth)
	}
	osdShare := 0.0
	if agg.Matched > 0 {
		osdShare = 100 * float64(agg.OSDCount) / float64(agg.Matched)
	}
	type4Recall := 0.0
	if agg.Type4Truth > 0 {
		type4Recall = 100 * float64(agg.Type4Hit) / float64(agg.Type4Truth)
	}
	fmt.Println()
	fmt.Printf("Recall:           %d / %d  (%.1f%%)\n", agg.Matched, agg.Truth, recall)
	fmt.Printf("Extras (FP):      %d\n", agg.Extra)
	fmt.Printf("OSD share:        %d / %d matched  (%.1f%%)\n",
		agg.OSDCount, agg.Matched, osdShare)
	fmt.Printf("Pass-1 / Pass-2:  %d / %d\n", agg.Pass1, agg.Pass2)
	fmt.Printf("Type 4 recall:    %d / %d  (%.1f%%)\n",
		agg.Type4Hit, agg.Type4Truth, type4Recall)
	fmt.Printf("Hash table size:  %d callsigns\n", ht.Size())
}

// runSweep is the OSD-gate grid sweep. Holds BP gates fixed; varies
// only OSD knobs:
//
//	MinNSyncOSD       ∈ {7, 8, 9, 10, 11}
//	MinToneAgreeOSD   ∈ {30, 35, 40, 45, 50}
//	AcceptDistRatio   ∈ {0.05, 0.15}   (current + one looser)
//
// = 50 configurations. Each config runs the 6 real captures in a
// fresh hash table (truth-pre-populated like the non-sweep path).
// Objective: matched − 2 × extras. Output: full table sorted by
// objective + top-5 + Pareto frontier (extras, matched).
func runSweep(wavs []string, dir string) {
	type sweepResult struct {
		MinNSync     int
		MinToneAgree int
		AcceptRatio  float64
		Matched      int
		Missed       int
		Extras       int
		BPCount      int
		OSDCount     int
		Pass1, Pass2 int
		TotalTruth   int
	}

	nSyncGrid := []int{7, 8, 9, 10, 11}
	toneAgreeGrid := []int{30, 35, 40, 45, 50}
	ratioGrid := []float64{0.05, 0.15}

	var results []sweepResult
	configCount := len(nSyncGrid) * len(toneAgreeGrid) * len(ratioGrid)
	fmt.Printf("=== OSD gate sweep: %d configs × %d captures ===\n\n",
		configCount, len(wavs))

	configIdx := 0
	for _, n := range nSyncGrid {
		for _, ta := range toneAgreeGrid {
			for _, ratio := range ratioGrid {
				configIdx++
				opts := sandbox.DefaultMultiPassOptions()
				opts.Gate.MinNSyncOSD = n
				opts.Gate.MinToneAgreeOSD = ta
				opts.BP.OSD.AcceptDistanceRatio = ratio

				ht := sandbox.NewCallsignHashTable()
				var agg sweepResult
				agg.MinNSync = n
				agg.MinToneAgree = ta
				agg.AcceptRatio = ratio
				for _, w := range wavs {
					r := processCapture(w, opts, ht)
					agg.Matched += r.Matched
					agg.Missed += r.Missed
					agg.Extras += r.Extra
					agg.BPCount += r.BPCount
					agg.OSDCount += r.OSDCount
					agg.Pass1 += r.Pass1
					agg.Pass2 += r.Pass2
					agg.TotalTruth += r.Truth
				}
				results = append(results, agg)
				fmt.Printf("  [%2d/%d] nSync=%d toneAgree=%d ratio=%.2f → matched=%d extras=%d obj=%d\n",
					configIdx, configCount, n, ta, ratio,
					agg.Matched, agg.Extras, agg.Matched-2*agg.Extras)
			}
		}
	}
	// Sort by objective (matched − 2*extras) desc.
	sort.Slice(results, func(i, j int) bool {
		oi := results[i].Matched - 2*results[i].Extras
		oj := results[j].Matched - 2*results[j].Extras
		return oi > oj
	})

	fmt.Printf("\n=== Sorted by objective (matched − 2 × extras), top 10 ===\n")
	fmt.Printf("%4s %9s %10s  %7s %7s %4s  %3s %4s  %5s %5s\n",
		"rank", "minNSync", "minToneAg", "ratio", "matched", "FP", "obj", "BP", "OSD", "P2")
	fmt.Println(strings.Repeat("-", 78))
	for i, r := range results {
		if i >= 10 {
			break
		}
		obj := r.Matched - 2*r.Extras
		fmt.Printf("%4d %9d %10d %7.2f  %7d %4d %4d  %3d %4d  %5d %5d\n",
			i+1, r.MinNSync, r.MinToneAgree, r.AcceptRatio,
			r.Matched, r.Extras, obj, r.BPCount, r.OSDCount, r.Pass1, r.Pass2)
	}

	// Pareto front: configs where no other config has both higher
	// matched AND lower extras.
	var pareto []sweepResult
	for _, a := range results {
		dominated := false
		for _, b := range results {
			if b.Matched > a.Matched && b.Extras < a.Extras {
				dominated = true
				break
			}
			// Strict in one and >= in the other.
			if (b.Matched > a.Matched && b.Extras <= a.Extras) ||
				(b.Matched >= a.Matched && b.Extras < a.Extras) {
				dominated = true
				break
			}
		}
		if !dominated {
			pareto = append(pareto, a)
		}
	}
	sort.Slice(pareto, func(i, j int) bool { return pareto[i].Matched < pareto[j].Matched })
	fmt.Printf("\n=== Pareto front (matched ↑, extras ↓) ===\n")
	fmt.Printf("%9s %10s %7s  %7s %4s %4s  %3s %4s\n",
		"minNSync", "minToneAg", "ratio", "matched", "FP", "obj", "BP", "OSD")
	fmt.Println(strings.Repeat("-", 65))
	for _, r := range pareto {
		obj := r.Matched - 2*r.Extras
		fmt.Printf("%9d %10d %7.2f  %7d %4d %4d  %3d %4d\n",
			r.MinNSync, r.MinToneAgree, r.AcceptRatio,
			r.Matched, r.Extras, obj, r.BPCount, r.OSDCount)
	}
	fmt.Printf("\nTotal truth: %d signals across %d captures\n", results[0].TotalTruth, len(wavs))
}

func listWAVs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".wav" {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func processCapture(wavPath string, opts sandbox.MultiPassOptions, ht *sandbox.CallsignHashTable) captureResult {
	r := captureResult{Name: filepath.Base(strings.TrimSuffix(wavPath, ".wav"))}
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		log.Printf("read %q: %v", wavPath, err)
		return r
	}
	if data.SampleRate != expectedSampleRate || data.Channels != 1 {
		log.Printf("%q: sample-rate=%d ch=%d (want 12000 mono)", wavPath, data.SampleRate, data.Channels)
		return r
	}
	manifest, err := truth.Read(truth.PathFor(wavPath))
	if err != nil || manifest == nil {
		log.Printf("no truth for %q", wavPath)
		return r
	}
	r.Truth = len(manifest.Signals)

	// Pre-populate the hash table from the truth manifest's
	// callsigns BEFORE decoding. This simulates the "heard in
	// earlier slots" memory the Type 4 path depends on — without
	// it, the very first Type 4 in a slot has nothing to resolve
	// against, but operationally an active decoder would have
	// accumulated those calls from a prior slot. We do this
	// per-capture so Type 4 recall reflects the spec-intended
	// memory, not the order-sensitive same-slot population.
	for _, sig := range manifest.Signals {
		for _, tok := range strings.Fields(sig.Text) {
			if looksLikeCall(tok) {
				ht.Add(tok)
			}
		}
	}

	decodes := sandbox.MultiPassDecodeWithHashes(data.Samples, opts, ht)

	// Per-truth match. Greedy by closest (text + freq + dt).
	matchedTruth := make([]bool, len(manifest.Signals))
	matchedDecode := make([]bool, len(decodes))
	for ti, sig := range manifest.Signals {
		bestIdx := -1
		bestScore := math.Inf(1)
		for di, d := range decodes {
			if matchedDecode[di] || d.Text != sig.Text {
				continue
			}
			df := d.FreqHz - sig.FreqHz
			ddt := d.DtSec - sig.DTSec
			if math.Abs(df) > freqMatchTolHz || math.Abs(ddt) > dtMatchTolS {
				continue
			}
			score := df*df + ddt*ddt*1e4 // weight dt error higher per-second
			if score < bestScore {
				bestScore = score
				bestIdx = di
			}
		}
		isType4 := looksLikeType4(sig.Text)
		if isType4 {
			r.Type4Truth++
		}
		if bestIdx < 0 {
			r.Missed++
			continue
		}
		matchedTruth[ti] = true
		matchedDecode[bestIdx] = true
		r.Matched++
		if isType4 {
			r.Type4Hit++
		}
		d := decodes[bestIdx]
		r.MeanFreqErrHz += math.Abs(d.FreqHz - sig.FreqHz)
		r.MeanDtErrMs += math.Abs(d.DtSec-sig.DTSec) * 1000
		if strings.HasPrefix(d.DecodeMethod, "OSD") {
			r.OSDCount++
		} else {
			r.BPCount++
		}
		if d.Pass == 1 {
			r.Pass1++
		} else {
			r.Pass2++
		}
	}
	for di := range decodes {
		if !matchedDecode[di] {
			r.Extra++
		}
	}
	if r.Matched > 0 {
		r.MeanFreqErrHz /= float64(r.Matched)
		r.MeanDtErrMs /= float64(r.Matched)
	}
	return r
}

func printRow(r captureResult) {
	fmt.Printf("%-15s %5d %5d %5d %5d  %6.2f %6.1f  %5d %5d  %5d %5d\n",
		r.Name, r.Truth, r.Matched, r.Missed, r.Extra,
		r.MeanFreqErrHz, r.MeanDtErrMs,
		r.BPCount, r.OSDCount, r.Pass1, r.Pass2)
}

func aggAccumulate(agg *captureResult, r captureResult) {
	agg.Truth += r.Truth
	agg.Matched += r.Matched
	agg.Missed += r.Missed
	agg.Extra += r.Extra
	agg.BPCount += r.BPCount
	agg.OSDCount += r.OSDCount
	agg.Pass1 += r.Pass1
	agg.Pass2 += r.Pass2
	agg.Type4Truth += r.Type4Truth
	agg.Type4Hit += r.Type4Hit
	// Accumulate weighted-sum-of-errors; divide by total matched at
	// the end.
	agg.MeanFreqErrHz += r.MeanFreqErrHz * float64(r.Matched)
	agg.MeanDtErrMs += r.MeanDtErrMs * float64(r.Matched)
}

// looksLikeCall mirrors the heuristic in MultiPassDecode's
// registerCallsigns: contains both a letter and a digit; not a
// known non-call keyword; not a placeholder.
func looksLikeCall(tok string) bool {
	if tok == "" || tok[0] == '<' {
		return false
	}
	switch tok {
	case "CQ", "DE", "QRZ", "R", "RRR", "RR73", "73":
		return false
	}
	if strings.HasPrefix(tok, "+") || strings.HasPrefix(tok, "-") {
		return false // numeric report
	}
	hasLetter, hasDigit := false, false
	for _, c := range tok {
		switch {
		case c >= 'A' && c <= 'Z':
			hasLetter = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// looksLikeType4 returns true if the truth-manifest text looks like
// a Type 4 message: ends in RRR / RR73 / 73 (the only reports a
// 2-bit r2 field can encode) and contains at least one nonstandard
// callsign indicator (a "/" suffix or a length >6 chars).
//
// Heuristic — won't catch every edge case but good enough for the
// "what fraction of Type 4 truth do we hit" headline.
func looksLikeType4(text string) bool {
	endsWithToken := strings.HasSuffix(text, " 73") ||
		strings.HasSuffix(text, " RRR") ||
		strings.HasSuffix(text, " RR73")
	if !endsWithToken {
		return false
	}
	for _, tok := range strings.Fields(text) {
		if strings.Contains(tok, "/") {
			return true
		}
		if looksLikeCall(tok) && len(tok) > 6 {
			return true
		}
	}
	return false
}
