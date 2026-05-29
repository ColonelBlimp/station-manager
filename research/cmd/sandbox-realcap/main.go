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
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

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
	osd3sweep := flag.Bool("osd3-sweep", false, "OSD order+budget sweep on real captures")
	coverage := flag.Bool("coverage", false, "candidate-coverage diagnostic: bucket each truth signal by why it was missed (single-pass only)")
	coverageMP := flag.Bool("coverage-mp", false, "multi-pass coverage with per-failed-candidate decoder diagnostics")
	maxResultsSweep := flag.Bool("maxresults-sweep", false, "sweep SearchOptions.MaxResults to test the bucket-B hypothesis")
	nmsDiag := flag.Bool("nms-diag", false, "NMS variant sweep (baseline + K=2 per-freq variants): pre-NMS raw count, truth survival, dropped-near-truth vs dropped-no-truth, same-freq-diff-dt truth pairs (uses -nms-cap)")
	nmsCap := flag.Int("nms-cap", 200, "MaxResults cap applied after NMS in -nms-diag (200 is the operational default; 500 = deep-search variant)")
	searchK2M := flag.Bool("search-k2m", false, "use K=2 medium NMS (tight 6.25Hz × 0.08s, group 12.5Hz, K=2) for the default / -maxresults-sweep decode paths")
	cpuProfile := flag.String("cpuprofile", "", "write CPU profile to file (default-mode runs only)")
	memProfile := flag.String("memprofile", "", "write heap profile to file at exit (default-mode runs only)")
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
	if *osd3sweep {
		runOSD3Sweep(wavs)
		return
	}
	if *coverage {
		runCoverage(wavs)
		return
	}
	if *coverageMP {
		runCoverageMultiPass(wavs)
		return
	}
	if *maxResultsSweep {
		runMaxResultsSweep(wavs, *searchK2M)
		return
	}
	if *nmsDiag {
		runNMSDiagnostic(wavs, *nmsCap)
		return
	}

	opts := sandbox.DefaultMultiPassOptions()
	if *singlePass {
		opts.MaxPasses = 1
	}
	if *searchK2M {
		opts.Search.K2Medium = true
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatalf("create cpuprofile: %v", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("start cpuprofile: %v", err)
		}
		defer pprof.StopCPUProfile()
	}
	if *memProfile != "" {
		defer func() {
			f, err := os.Create(*memProfile)
			if err != nil {
				log.Printf("create memprofile: %v", err)
				return
			}
			defer f.Close()
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Printf("write memprofile: %v", err)
			}
		}()
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

// runOSD3Sweep evaluates OSD order+budget configurations on the real-
// capture corpus. Holds gate at defaults; varies only OSD.Order and
// OSD.MaxCandidates. Reports matched / extras / OSD count / wall time
// per config so the operator can decide whether to ship OSD-3 (and
// at what budget) as the real-capture default.
func runOSD3Sweep(wavs []string) {
	type sweepCfg struct {
		Order int
		Cap   int // 0 = unlimited
		Label string
	}
	cfgs := []sweepCfg{
		{2, 0, "order=2 (current baseline)"},
		{3, 5000, "order=3, cap=5k"},
		{3, 10000, "order=3, cap=10k"},
		{3, 25000, "order=3, cap=25k"},
		{3, 50000, "order=3, cap=50k"},
		{3, 0, "order=3, no cap (~125k)"},
	}

	type sweepResult struct {
		Cfg          sweepCfg
		Matched      int
		Extras       int
		BPCount      int
		OSDCount     int
		Pass1, Pass2 int
		TotalTruth   int
		WallTime     time.Duration
	}

	fmt.Printf("=== OSD order/budget sweep: %d configs × %d captures ===\n\n",
		len(cfgs), len(wavs))
	var results []sweepResult
	for i, cfg := range cfgs {
		opts := sandbox.DefaultMultiPassOptions()
		opts.BP.OSD.Order = cfg.Order
		opts.BP.OSD.MaxCandidates = cfg.Cap

		ht := sandbox.NewCallsignHashTable()
		var agg sweepResult
		agg.Cfg = cfg
		start := time.Now()
		for _, w := range wavs {
			r := processCapture(w, opts, ht)
			agg.Matched += r.Matched
			agg.Extras += r.Extra
			agg.BPCount += r.BPCount
			agg.OSDCount += r.OSDCount
			agg.Pass1 += r.Pass1
			agg.Pass2 += r.Pass2
			agg.TotalTruth += r.Truth
		}
		agg.WallTime = time.Since(start)
		results = append(results, agg)
		fmt.Printf("  [%d/%d] %-30s  matched=%d extras=%d OSD=%d  %.2fs\n",
			i+1, len(cfgs), cfg.Label,
			agg.Matched, agg.Extras, agg.OSDCount, agg.WallTime.Seconds())
	}

	// Final summary table.
	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("%-30s  %-8s %-6s %-7s %-7s %-7s %-8s\n",
		"config", "matched", "FP", "BP", "OSD", "Pass2", "wall")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range results {
		fmt.Printf("%-30s  %3d/%-3d  %4d  %4d   %4d    %4d    %5.2fs\n",
			r.Cfg.Label, r.Matched, r.TotalTruth, r.Extras,
			r.BPCount, r.OSDCount, r.Pass2, r.WallTime.Seconds())
	}

	// Decision summary.
	base := results[0]
	fmt.Printf("\n=== Decision criteria (vs %s) ===\n", base.Cfg.Label)
	for _, r := range results[1:] {
		dMatched := r.Matched - base.Matched
		dExtras := r.Extras - base.Extras
		dWall := r.WallTime - base.WallTime
		perSlot := r.WallTime.Seconds() / float64(len(wavs))
		fmt.Printf("  %-30s  Δmatched=%+d  Δextras=%+d  Δwall=%+.1fs  per-slot=%.2fs%s\n",
			r.Cfg.Label, dMatched, dExtras, dWall.Seconds(), perSlot,
			budgetTag(perSlot))
	}
}

// budgetTag flags configs that exceed the 15-second per-slot budget.
func budgetTag(perSlot float64) string {
	if perSlot >= 15.0 {
		return "  ⚠ exceeds 15s slot budget"
	}
	if perSlot >= 10.0 {
		return "  (tight: > 10s per slot)"
	}
	return ""
}

// candidateFate records what happened to one coarse candidate across
// the decode pipeline. Used by the coverage diagnostic to classify
// each truth signal into a bucket explaining why we missed it.
type candidateFate struct {
	Rank         int     // 1-indexed position in the pre-NMS sorted list
	FreqHz       float64 // refined freq if decoded, else coarse
	DtSec        float64 // refined dt if decoded, else coarse
	Sync         float64
	InTopN       bool // rank ≤ defaultMaxResults
	SurvivedNMS  bool // appeared in the default-NMS post list
	BPDecoded    bool // BP+CRC succeeded (regardless of method)
	GateOK       bool // accepted by AcceptDecode
	DecodeMethod string
	Text         string // unpacked text on successful decode; empty otherwise
}

// truthBucket tags one truth signal by the reason it was missed (or
// MATCHED). See runCoverage for the bucket definitions.
type truthBucket int

const (
	bucketMATCHED truthBucket = iota
	bucketA1                  // no candidate within LOOSE
	bucketA2                  // candidate within LOOSE but not NORMAL
	bucketB                   // candidate within NORMAL but pre-NMS rank > MaxResults
	bucketC                   // candidate within NORMAL and in top-N, but killed by NMS
	bucketD                   // candidate survived NMS but BP+OSD failed
	bucketE                   // BP-OK but gate rejected
)

func (b truthBucket) String() string {
	switch b {
	case bucketMATCHED:
		return "MATCHED"
	case bucketA1:
		return "A1 (no cand near truth)"
	case bucketA2:
		return "A2 (cand only at loose tol)"
	case bucketB:
		return "B (cand rank > MaxResults)"
	case bucketC:
		return "C (cand killed by NMS)"
	case bucketD:
		return "D (BP+OSD failed)"
	case bucketE:
		return "E (decoded but gate rejected)"
	}
	return "?"
}

// candidateDiag holds the per-candidate decoder diagnostics that
// feed the D/E bucket analysis. Populated whenever a candidate
// reaches BPDecode.
type candidateDiag struct {
	Rank         int // rank in the pre-NMS sorted list
	Pass         int // 1 or 2
	FreqHz       float64
	DtSec        float64
	Sync         float64
	InTopN       bool
	SurvivedNMS  bool
	BPDecoded    bool   // BP+CRC succeeded (any method)
	GateOK       bool   // accepted by AcceptDecode
	DecodeMethod string // "BP", "OSD-N", "fail"
	BPIterations int
	NSync        int
	ToneAgree    int
	HardErrors   int
	MedianAbsLLR float64
	Text         string // unpacked text if BPDecoded && GateOK
}

// runCoverageMultiPass is the multi-pass-aware coverage diagnostic.
// Runs both passes of the standard pipeline with instrumentation;
// classifies each truth by its eventual fate (MATCHED-pass1,
// MATCHED-pass2, or one of the miss buckets).
//
// For each candidate that reaches BPDecode, records per-candidate
// diagnostics (nsync, toneAgree, hardErrors, etc.) so we can read
// out the D/E populations' decoder-side properties.
func runCoverageMultiPass(wavs []string) {
	const (
		looseFreqHz  = 20.0
		looseDtSec   = 1.0
		normalFreqHz = 10.0
		normalDtSec  = 0.5
	)

	fmt.Printf("=== Multi-pass coverage @ MaxResults=%d ===\n",
		sandbox.DefaultSearchOptions().MaxResults)
	fmt.Printf("Tolerances: normal ±%.0f Hz / ±%.2f s, loose ±%.0f Hz / ±%.2f s\n\n",
		normalFreqHz, normalDtSec, looseFreqHz, looseDtSec)

	type bucketCounts struct {
		MatchedPass1 int
		MatchedPass2 int
		A1, A2, B, C int
		D            int // BP+OSD failed
		Egate        int // BP-OK but gate rejected (true gate-tuning candidate)
		Eother       int // BP-OK + gate-OK but decoded text matched neither this truth nor any other (extra-near-truth)
		Eoverlap     int // BP-OK + gate-OK + decoded text matches a DIFFERENT truth
		Extras       int
	}
	var totals bucketCounts
	totalTruth := 0

	// Aggregate D/E diagnostics for histogram output.
	var allDDiag, allEDiag []candidateDiag

	for _, wavPath := range wavs {
		name := filepath.Base(strings.TrimSuffix(wavPath, ".wav"))
		data, err := audio.ReadWAV(wavPath)
		if err != nil {
			log.Printf("read %q: %v", wavPath, err)
			continue
		}
		manifest, err := truth.Read(truth.PathFor(wavPath))
		if err != nil || manifest == nil {
			log.Printf("no truth for %q", wavPath)
			continue
		}
		totalTruth += len(manifest.Signals)

		bc, dDiag, eDiag := analyseCaptureMP(data.Samples, manifest, looseFreqHz, looseDtSec, normalFreqHz, normalDtSec)
		allDDiag = append(allDDiag, dDiag...)
		allEDiag = append(allEDiag, eDiag...)
		totals.MatchedPass1 += bc.MatchedPass1
		totals.MatchedPass2 += bc.MatchedPass2
		totals.A1 += bc.A1
		totals.A2 += bc.A2
		totals.B += bc.B
		totals.C += bc.C
		totals.D += bc.D
		totals.Egate += bc.Egate
		totals.Eother += bc.Eother
		totals.Eoverlap += bc.Eoverlap
		totals.Extras += bc.Extras

		totalE := bc.Egate + bc.Eother + bc.Eoverlap
		fmt.Printf("%-12s truth=%3d  M1=%3d M2=%2d  A1=%2d A2=%2d  B=%2d C=%2d  D=%2d E=%2d(g%d/o%d/x%d)  extras=%2d\n",
			name, len(manifest.Signals),
			bc.MatchedPass1, bc.MatchedPass2,
			bc.A1, bc.A2, bc.B, bc.C, bc.D, totalE,
			bc.Egate, bc.Eoverlap, bc.Eother, bc.Extras)
	}
	fmt.Println(strings.Repeat("-", 95))
	totalE := totals.Egate + totals.Eother + totals.Eoverlap
	fmt.Printf("%-12s truth=%3d  M1=%3d M2=%2d  A1=%2d A2=%2d  B=%2d C=%2d  D=%2d E=%2d(g%d/o%d/x%d)  extras=%2d\n",
		"TOTAL", totalTruth,
		totals.MatchedPass1, totals.MatchedPass2,
		totals.A1, totals.A2, totals.B, totals.C, totals.D, totalE,
		totals.Egate, totals.Eoverlap, totals.Eother, totals.Extras)

	totalMatched := totals.MatchedPass1 + totals.MatchedPass2
	totalMissed := totals.A1 + totals.A2 + totals.B + totals.C + totals.D + totalE
	fmt.Println()
	fmt.Printf("Recall: %d / %d  (%.1f%%)\n", totalMatched, totalTruth,
		100*float64(totalMatched)/float64(totalTruth))
	fmt.Printf("  MATCHED-pass1: %3d  (%.1f%% of truth)\n",
		totals.MatchedPass1, 100*float64(totals.MatchedPass1)/float64(totalTruth))
	fmt.Printf("  MATCHED-pass2: %3d  (%.1f%% of truth)\n",
		totals.MatchedPass2, 100*float64(totals.MatchedPass2)/float64(totalTruth))
	fmt.Printf("Missed: %d / %d  (%.1f%% of truth)\n", totalMissed, totalTruth,
		100*float64(totalMissed)/float64(totalTruth))
	if totalMissed > 0 {
		for _, b := range []struct {
			name  string
			count int
		}{
			{"A1 (no candidate near truth)", totals.A1},
			{"A2 (cand at LOOSE not NORMAL)", totals.A2},
			{"B (rank > MaxResults)", totals.B},
			{"C (NMS-killed)", totals.C},
			{"D (BP+OSD failed)", totals.D},
			{"E-gate (BP-OK, gate rejected)", totals.Egate},
			{"E-overlap (decoded to OTHER truth)", totals.Eoverlap},
			{"E-other (decoded to non-truth text)", totals.Eother},
		} {
			fmt.Printf("  %-33s  %3d  (%.1f%% of missed)\n",
				b.name, b.count, 100*float64(b.count)/float64(totalMissed))
		}
	}

	// D and E diagnostics: per-candidate decoder properties on the
	// failed truth-near candidates.
	fmt.Printf("\n=== Diagnostics on bucket D (BP+OSD failed at decode) ===\n")
	printDiagHistogram(allDDiag)
	fmt.Printf("\n=== Diagnostics on bucket E (decoded but gate rejected) ===\n")
	printDiagHistogram(allEDiag)
}

func printDiagHistogram(diags []candidateDiag) {
	if len(diags) == 0 {
		fmt.Println("  (no candidates in this bucket)")
		return
	}
	fmt.Printf("  count = %d\n", len(diags))
	// Decode method distribution.
	methods := map[string]int{}
	for _, d := range diags {
		methods[d.DecodeMethod]++
	}
	fmt.Printf("  decode methods:\n")
	for m, n := range methods {
		if m == "" {
			m = "(not attempted)"
		}
		fmt.Printf("    %-14s %d\n", m, n)
	}
	// Quick stats: nsync, hardErrors, BPIterations.
	var nsyncs, hardErrs, bpIters []int
	var medLLRs []float64
	for _, d := range diags {
		nsyncs = append(nsyncs, d.NSync)
		hardErrs = append(hardErrs, d.HardErrors)
		bpIters = append(bpIters, d.BPIterations)
		medLLRs = append(medLLRs, d.MedianAbsLLR)
	}
	fmt.Printf("  nsync         min=%d median=%d max=%d\n", minInt(nsyncs), medianInt(nsyncs), maxInt(nsyncs))
	fmt.Printf("  hardErrors    min=%d median=%d max=%d\n", minInt(hardErrs), medianInt(hardErrs), maxInt(hardErrs))
	fmt.Printf("  BP iters      min=%d median=%d max=%d\n", minInt(bpIters), medianInt(bpIters), maxInt(bpIters))
	fmt.Printf("  median|LLR|   min=%.2e median=%.2e max=%.2e\n", minFloat(medLLRs), medianFloat(medLLRs), maxFloat(medLLRs))
}

func minInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs {
		if x < m {
			m = x
		}
	}
	return m
}
func maxInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}
func medianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := make([]int, len(xs))
	copy(s, xs)
	sort.Ints(s)
	return s[len(s)/2]
}
func minFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs {
		if x < m {
			m = x
		}
	}
	return m
}
func maxFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}
func medianFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := make([]float64, len(xs))
	copy(s, xs)
	sort.Float64s(s)
	return s[len(s)/2]
}

// analyseCaptureMP runs the multi-pass pipeline with full
// instrumentation and classifies each truth signal.
//
// Returns: bucket counts (with E split into gate / overlap / other),
// diagnostic records for buckets D and E (combined) so we can
// characterise the decode-failure modes.
func analyseCaptureMP(audio []float32, manifest *truth.Manifest,
	looseFreqHz, looseDtSec, normalFreqHz, normalDtSec float64,
) (bc struct {
	MatchedPass1, MatchedPass2 int
	A1, A2, B, C, D            int
	Egate, Eother, Eoverlap    int
	Extras                     int
}, dDiag, eDiag []candidateDiag) {

	const coverageMaxResults = 10000
	defaultMaxResults := sandbox.DefaultSearchOptions().MaxResults

	ch, err := sandbox.NewChannelizer()
	if err != nil {
		return
	}
	defer ch.Close()

	// Pre-populate hash table from truth manifest (mirrors
	// processCapture).
	ht := sandbox.NewCallsignHashTable()
	for _, sig := range manifest.Signals {
		for _, tok := range strings.Fields(sig.Text) {
			if looksLikeCall(tok) {
				ht.Add(tok)
			}
		}
	}

	rOpts := sandbox.DefaultRefineOptions()
	bpOpts := sandbox.DefaultBPOptions()
	gate := sandbox.DefaultAcceptDecodeOptions()

	working := make([]float32, len(audio))
	copy(working, audio)

	// Cumulative diagnostic records across passes.
	allFates := []candidateDiag{}
	matchedPass1Set := map[int]bool{} // fate-index of matched-pass1 candidates
	matchedPass2Set := map[int]bool{}

	type decodeOut struct {
		text   string
		freqHz float64
		dtSec  float64
		cw     [sandbox.LDPCCodewordBits]uint8
		pass   int
	}
	var allDecodes []decodeOut

	for pass := 1; pass <= 2; pass++ {
		if err := ch.Prepare(working); err != nil {
			break
		}
		spec := sandbox.Spectrogram(working)
		if spec == nil {
			break
		}
		// Pre-NMS list with deep ranking (for B / C / A bucket
		// assignment). Records every candidate's pre-NMS rank.
		rawOpts := sandbox.DefaultSearchOptions()
		rawOpts.MaxResults = coverageMaxResults
		rawOpts.NMSFreqHz = 0.0001
		rawOpts.NMSDTSec = 0.0001
		rawOpts.Threshold = 0.01
		rawCands := sandbox.FindCandidates(spec, rawOpts)
		preRank := map[[2]float64]int{}
		for i, c := range rawCands {
			preRank[[2]float64{c.FreqHz, c.DtSec}] = i + 1
		}
		// Post-NMS top-N — what the operational pipeline actually
		// decodes. Pre-NMS rank can be > MaxResults for some of
		// these (NMS pruning brings up lower-ranked survivors), so
		// we decode this list regardless of pre-NMS rank.
		postOpts := sandbox.DefaultSearchOptions()
		postOpts.MaxResults = defaultMaxResults
		postCands := sandbox.FindCandidates(spec, postOpts)
		postSet := map[[2]float64]bool{}
		for _, c := range postCands {
			postSet[[2]float64{c.FreqHz, c.DtSec}] = true
		}

		// Record fates for the pre-NMS list (rank info for ALL
		// candidates), without decoding the ones that won't enter
		// the actual pipeline.
		for i, c := range rawCands {
			f := candidateDiag{
				Rank:        i + 1,
				Pass:        pass,
				FreqHz:      c.FreqHz,
				DtSec:       c.DtSec,
				Sync:        c.Sync,
				InTopN:      i < defaultMaxResults,
				SurvivedNMS: postSet[[2]float64{c.FreqHz, c.DtSec}],
			}
			// Only candidates that will enter the actual decode
			// pipeline get instrumented for D/E. Others are pre-
			// classified as rank-only.
			if !f.SurvivedNMS {
				allFates = append(allFates, f)
				continue
			}
			r, err := sandbox.RefineCandidate(ch, c, rOpts)
			if err != nil {
				allFates = append(allFates, f)
				continue
			}
			grid, err := sandbox.ExtractSymbols(ch, r)
			if err != nil {
				allFates = append(allFates, f)
				continue
			}
			llrs := sandbox.SoftLLRs(grid, false)
			br := sandbox.BPDecode(llrs, bpOpts)
			f.DecodeMethod = br.DecodeMethod
			f.BPIterations = br.Iterations
			f.NSync = sandbox.HardSyncScore(grid)
			f.MedianAbsLLR = medianAbsLLR(llrs[:])
			if !br.OK {
				f.HardErrors = sandbox.HardErrorsCount(br.Codeword, llrs)
				allFates = append(allFates, f)
				continue
			}
			f.BPDecoded = true
			f.HardErrors = sandbox.HardErrorsCount(br.Codeword, llrs)
			f.ToneAgree = sandbox.ToneAgreementCount(br.Codeword, grid)
			var payload [sandbox.LDPCPayloadBits]uint8
			copy(payload[:], br.Message91[:sandbox.LDPCPayloadBits])
			ur := sandbox.Unpack77WithHashes(payload, ht)
			if !ur.OK {
				allFates = append(allFates, f)
				continue
			}
			// Coverage diagnostic path: uses LLRMetric=N1 since this
			// codepath only runs the N=1 LLR set. The cascade-aware
			// tightening lives in MultiPassDecode.
			gateOK, _ := sandbox.AcceptDecode(br.DecodeMethod, sandbox.LLRMetricN1, f.NSync, grid, br.Codeword,
				f.HardErrors, 1000.0, gate)
			f.GateOK = gateOK
			if gateOK {
				f.Text = ur.Text
				f.FreqHz = r.FreqHz
				f.DtSec = r.DtSec
				allDecodes = append(allDecodes, decodeOut{
					text:   ur.Text,
					freqHz: r.FreqHz,
					dtSec:  r.DtSec,
					cw:     br.Codeword,
					pass:   pass,
				})
				// Register decoded callsigns for Type 4 / hash use.
				for _, tok := range strings.Fields(ur.Text) {
					if looksLikeCall(tok) {
						ht.Add(tok)
					}
				}
			}
			allFates = append(allFates, f)
		}
		// Between passes: subtract pass-N decodes from working audio.
		if pass < 2 {
			for _, d := range allDecodes {
				if d.pass != pass {
					continue
				}
				tones := sandbox.CodewordToTones(d.cw)
				cosSynth, sinSynth, sigStart, sigLen := sandbox.SynthesizeAudio(
					tones, d.freqHz, d.dtSec, 12000, len(working))
				sps := int(12000 * 0.16)
				working = sandbox.FitAndSubtractAudio(working, cosSynth, sinSynth, sigStart, sigLen, sps)
			}
		}
	}

	// Classify each truth.
	for _, sig := range manifest.Signals {
		// Is the truth MATCHED in pass 1 or pass 2?
		matchedPass := 0
		for _, d := range allDecodes {
			if d.text != sig.Text {
				continue
			}
			if math.Abs(d.freqHz-sig.FreqHz) <= looseFreqHz &&
				math.Abs(d.dtSec-sig.DTSec) <= looseDtSec {
				matchedPass = d.pass
				break
			}
		}
		if matchedPass == 1 {
			bc.MatchedPass1++
			continue
		}
		if matchedPass == 2 {
			bc.MatchedPass2++
			continue
		}
		// Not matched — classify by best-fate near candidate.
		// Collect all pass-1 candidates within LOOSE tolerance, then
		// filter to NORMAL. Pick the one with the highest-priority
		// fate (closest-to-success):
		//   priority E > D > C > B
		// Records the chosen candidate's diagnostics in dDiag/eDiag
		// for the bucket histogram.
		var anyLoose, anyNormal bool
		bestPriority := -1
		var bestFate candidateDiag
		bestBucket := 0
		for _, f := range allFates {
			if f.Pass != 1 {
				continue
			}
			df := math.Abs(f.FreqHz - sig.FreqHz)
			ddt := math.Abs(f.DtSec - sig.DTSec)
			if df > looseFreqHz || ddt > looseDtSec {
				continue
			}
			anyLoose = true
			if df > normalFreqHz || ddt > normalDtSec {
				continue
			}
			anyNormal = true
			// Determine this candidate's bucket.
			var thisBucket, thisPriority int
			switch {
			case !f.SurvivedNMS && !f.InTopN:
				thisBucket = 4 // B
				thisPriority = 1
			case !f.SurvivedNMS && f.InTopN:
				thisBucket = 5 // C
				thisPriority = 2
			case f.SurvivedNMS && !f.BPDecoded:
				thisBucket = 6 // D
				thisPriority = 3
			default: // BP-OK but didn't match truth → gate rejected for this truth
				thisBucket = 7 // E
				thisPriority = 4
			}
			if thisPriority > bestPriority {
				bestPriority = thisPriority
				bestFate = f
				bestBucket = thisBucket
			}
		}
		if !anyLoose {
			bc.A1++
			continue
		}
		if !anyNormal {
			bc.A2++
			continue
		}
		switch bestBucket {
		case 4:
			bc.B++
		case 5:
			bc.C++
		case 6:
			bc.D++
			dDiag = append(dDiag, bestFate)
		case 7:
			// Split E by why the decode didn't yield this truth's text.
			eDiag = append(eDiag, bestFate)
			switch {
			case !bestFate.GateOK:
				// Decoder reached a CRC-valid codeword but the gate
				// rejected it. The truly actionable gate-tuning case.
				bc.Egate++
			case textMatchesAnyTruth(bestFate.Text, manifest, sig.Text, looseFreqHz, looseDtSec, bestFate.FreqHz, bestFate.DtSec):
				// Decoded to a DIFFERENT truth's text — overlap, not
				// a real "missed" decode. Truth was masked by an
				// adjacent station the decoder grabbed instead.
				bc.Eoverlap++
			default:
				// Decoded to non-truth text — extra-near-truth (the
				// candidate is a real decode of something not in the
				// truth manifest, or a spurious codeword).
				bc.Eother++
			}
		}
	}

	// Extras: decoded candidates that don't match any truth.
	for _, d := range allDecodes {
		matched := false
		for _, sig := range manifest.Signals {
			if d.text == sig.Text &&
				math.Abs(d.freqHz-sig.FreqHz) <= looseFreqHz &&
				math.Abs(d.dtSec-sig.DTSec) <= looseDtSec {
				matched = true
				break
			}
		}
		if !matched {
			bc.Extras++
		}
	}

	_ = matchedPass1Set
	_ = matchedPass2Set
	return bc, dDiag, eDiag
}

// textMatchesAnyTruth returns true if decodeText matches a truth
// signal in the manifest OTHER THAN excludeText (used to identify
// "decoded a different truth" — overlap rather than real failure).
func textMatchesAnyTruth(decodeText string, manifest *truth.Manifest, excludeText string,
	looseFreqHz, looseDtSec, candFreqHz, candDtSec float64,
) bool {
	for _, sig := range manifest.Signals {
		if sig.Text == excludeText {
			continue
		}
		if sig.Text != decodeText {
			continue
		}
		// Decoded text matches a different truth signal; check the
		// candidate is also near that truth (otherwise the match is
		// coincidental — same text but different freq/dt).
		if math.Abs(candFreqHz-sig.FreqHz) <= looseFreqHz &&
			math.Abs(candDtSec-sig.DTSec) <= looseDtSec {
			return true
		}
	}
	return false
}

func medianAbsLLR(llrs []float64) float64 {
	abs := make([]float64, len(llrs))
	for i, l := range llrs {
		a := l
		if a < 0 {
			a = -a
		}
		abs[i] = a
	}
	sort.Float64s(abs)
	return abs[len(abs)/2]
}

// runMaxResultsSweep tests the bucket-B-dominant hypothesis from the
// coverage diagnostic: if 80% of truth signals have a candidate within
// ±10 Hz / ±0.5 s but their score ranks below the top-50 cap, raising
// MaxResults should lift recall substantially.
//
// Sweep over MaxResults values; record matched / extras / runtime.
// Decision: if matched scales with MaxResults at acceptable runtime,
// ship a higher default. If matched plateaus, bucket B candidates
// don't actually decode and we need scanner score-quality work.
func runMaxResultsSweep(wavs []string, k2Medium bool) {
	caps := []int{50, 100, 200, 500, 1000}
	mode := "standard NMS"
	if k2Medium {
		mode = "K=2 medium NMS"
	}
	fmt.Printf("=== MaxResults sweep (%s): %d configs × %d captures ===\n\n",
		mode, len(caps), len(wavs))

	type res struct {
		MaxResults int
		Matched    int
		Extras     int
		Total      int
		Wall       time.Duration
	}
	var results []res

	for _, m := range caps {
		opts := sandbox.DefaultMultiPassOptions()
		opts.Search.MaxResults = m
		opts.Search.K2Medium = k2Medium
		ht := sandbox.NewCallsignHashTable()
		start := time.Now()
		matched, extras, total := 0, 0, 0
		for _, w := range wavs {
			r := processCapture(w, opts, ht)
			matched += r.Matched
			extras += r.Extra
			total += r.Truth
		}
		results = append(results, res{m, matched, extras, total, time.Since(start)})
		fmt.Printf("  MaxResults=%-4d  matched=%3d/%-3d  extras=%2d  wall=%.2fs (%.2fs/slot)\n",
			m, matched, total, extras, time.Since(start).Seconds(),
			time.Since(start).Seconds()/float64(len(wavs)))
	}

	fmt.Println()
	base := results[0]
	for _, r := range results[1:] {
		fmt.Printf("  Δ vs MaxResults=50: matched %+d, extras %+d, wall %+.1fs%s\n",
			r.Matched-base.Matched, r.Extras-base.Extras,
			(r.Wall - base.Wall).Seconds(),
			budgetTag(r.Wall.Seconds()/float64(len(wavs))))
	}
}

// aggBuckets is a per-capture or aggregate count of truth signals by
// bucket. Index by truthBucket enum value.
type aggBuckets struct {
	Counts [7]int
}

// runCoverage runs the candidate-coverage diagnostic on the supplied
// real-capture corpus. For each truth signal, identifies which
// pipeline stage was responsible for missing it.
//
// The diagnostic runs FindCandidates with NMS effectively disabled
// (NMSFreqHz=0, NMSDTSec=0) AND with the default MaxResults bumped
// to 500 — this gives us the pre-NMS sorted candidate list with
// enough depth to see bucket B (truth-near candidates ranked below
// MaxResults). It then applies the default NMS manually to identify
// which candidates would have survived in the standard pipeline.
// Surviving candidates are decoded as in normal multi-pass; the
// resulting text + gate status feed the per-truth bucket assignment.
func runCoverage(wavs []string) {
	const (
		looseFreqHz  = 20.0
		looseDtSec   = 1.0
		normalFreqHz = 10.0
		normalDtSec  = 0.5
		// Default MaxResults in DefaultSearchOptions = 200 (new default
		// after the 2026-05-27 calibration); bucket B counts truth-
		// near candidates whose pre-NMS rank exceeds this.
		defaultMaxResults = 200
		// We sweep up to this many candidates per slot when building
		// the diagnostic list. Set high so even low-scored truth-near
		// candidates appear — the goal here is to find out IF the
		// matched filter sees them at all, not to budget runtime.
		coverageMaxResults = 10000
	)

	fmt.Printf("=== Coverage diagnostic across %d captures ===\n", len(wavs))
	fmt.Printf("Tolerances: tight ±5/±0.25s, normal ±%.0f/±%.2fs, loose ±%.0f/±%.2fs\n\n",
		normalFreqHz, normalDtSec, looseFreqHz, looseDtSec)

	var totals aggBuckets
	totalTruth := 0

	for _, wavPath := range wavs {
		name := filepath.Base(strings.TrimSuffix(wavPath, ".wav"))
		data, err := audio.ReadWAV(wavPath)
		if err != nil {
			log.Printf("read %q: %v", wavPath, err)
			continue
		}
		manifest, err := truth.Read(truth.PathFor(wavPath))
		if err != nil || manifest == nil {
			log.Printf("no truth manifest beside %q", wavPath)
			continue
		}
		totalTruth += len(manifest.Signals)

		fates := gatherCandidateFates(data.Samples, manifest, coverageMaxResults, defaultMaxResults)

		// Pre-build a map of decoded text → fate index so the
		// "already-decoded" check is O(1) per truth.
		decodedByText := map[string][]int{}
		for i, f := range fates {
			if f.GateOK && f.Text != "" {
				decodedByText[f.Text] = append(decodedByText[f.Text], i)
			}
		}

		var caps aggBuckets
		for _, sig := range manifest.Signals {
			b := classifyTruth(sig, fates, decodedByText,
				looseFreqHz, looseDtSec, normalFreqHz, normalDtSec, defaultMaxResults)
			caps.Counts[b]++
			totals.Counts[b]++
		}
		printBucketRow(name, len(manifest.Signals), caps)
	}
	fmt.Println(strings.Repeat("-", 70))
	printBucketRow("TOTAL", totalTruth, totals)

	// Headline summary.
	fmt.Println()
	for b := bucketMATCHED; b <= bucketE; b++ {
		pct := 0.0
		if totalTruth > 0 {
			pct = 100 * float64(totals.Counts[b]) / float64(totalTruth)
		}
		fmt.Printf("  %-32s  %4d  %5.1f%%\n", b.String(), totals.Counts[b], pct)
	}
}

func printBucketRow(name string, truthCount int, ab aggBuckets) {
	fmt.Printf("%-12s truth=%3d  M=%3d  A1=%2d  A2=%2d  B=%2d  C=%2d  D=%2d  E=%2d\n",
		name, truthCount,
		ab.Counts[bucketMATCHED], ab.Counts[bucketA1], ab.Counts[bucketA2],
		ab.Counts[bucketB], ab.Counts[bucketC], ab.Counts[bucketD], ab.Counts[bucketE])
}

// gatherCandidateFates runs the full pipeline with NMS disabled +
// expanded MaxResults, recording every candidate's fate (rank,
// NMS-survival, decode result). Returned slice is sorted by pre-NMS
// rank (descending by Sync).
func gatherCandidateFates(samples []float32, manifest *truth.Manifest,
	maxResults, defaultMax int) []candidateFate {
	_ = manifest // currently unused but reserved for per-truth-aware diagnostics

	ch, err := sandbox.NewChannelizer()
	if err != nil {
		return nil
	}
	defer ch.Close()
	if err := ch.Prepare(samples); err != nil {
		return nil
	}
	spec := sandbox.Spectrogram(samples)
	if spec == nil {
		return nil
	}

	// Pre-NMS: run FindCandidates with NMS effectively disabled,
	// expanded MaxResults, AND a very low score threshold — so any
	// real signal the matched filter can detect makes it into the
	// list even if it scores below the default 3.0 cutoff. Bucket A1
	// then truly means "the matched-filter score at this freq/dt is
	// indistinguishable from noise."
	rawOpts := sandbox.DefaultSearchOptions()
	rawOpts.MaxResults = maxResults
	rawOpts.NMSFreqHz = 0.0001 // not exactly zero (avoid false coincidence)
	rawOpts.NMSDTSec = 0.0001
	rawOpts.Threshold = 0.01 // near-zero — find anything detectable
	rawCands := sandbox.FindCandidates(spec, rawOpts)

	// Post-NMS: run again with the default settings to identify which
	// candidates survived. We compare by (FreqHz, DtSec) which should
	// match exactly since both calls use the same spectrogram.
	postOpts := sandbox.DefaultSearchOptions()
	postOpts.MaxResults = defaultMax
	postCands := sandbox.FindCandidates(spec, postOpts)
	survivedSet := map[[2]float64]bool{}
	for _, c := range postCands {
		survivedSet[[2]float64{c.FreqHz, c.DtSec}] = true
	}

	// For each pre-NMS candidate, run the decode pipeline and record
	// its fate. Use the same gates as multipass to make the bucket
	// assignment consistent with the normal run.
	rOpts := sandbox.DefaultRefineOptions()
	bpOpts := sandbox.DefaultBPOptions()
	gate := sandbox.DefaultAcceptDecodeOptions()
	ht := sandbox.NewCallsignHashTable()
	// Pre-populate the hash table with truth-manifest callsigns so
	// Type 4 references can resolve (mirrors processCapture).
	for _, sig := range manifest.Signals {
		for _, tok := range strings.Fields(sig.Text) {
			if looksLikeCall(tok) {
				ht.Add(tok)
			}
		}
	}

	fates := make([]candidateFate, len(rawCands))
	for i, c := range rawCands {
		f := candidateFate{
			Rank:        i + 1,
			FreqHz:      c.FreqHz,
			DtSec:       c.DtSec,
			Sync:        c.Sync,
			InTopN:      i < defaultMax,
			SurvivedNMS: survivedSet[[2]float64{c.FreqHz, c.DtSec}],
		}
		// Only run decode on candidates that would have survived in
		// the normal pipeline. Saves runtime on the tail-of-rank
		// candidates that wouldn't be decoded anyway.
		if !f.SurvivedNMS || !f.InTopN {
			fates[i] = f
			continue
		}
		r, err := sandbox.RefineCandidate(ch, c, rOpts)
		if err != nil {
			fates[i] = f
			continue
		}
		grid, err := sandbox.ExtractSymbols(ch, r)
		if err != nil {
			fates[i] = f
			continue
		}
		llrs := sandbox.SoftLLRs(grid, false)
		br := sandbox.BPDecode(llrs, bpOpts)
		f.DecodeMethod = br.DecodeMethod
		if !br.OK {
			fates[i] = f
			continue
		}
		f.BPDecoded = true
		var payload [sandbox.LDPCPayloadBits]uint8
		copy(payload[:], br.Message91[:sandbox.LDPCPayloadBits])
		ur := sandbox.Unpack77WithHashes(payload, ht)
		if !ur.OK {
			fates[i] = f
			continue
		}
		// Quality gate.
		nsync := sandbox.HardSyncScore(grid)
		hardErrs := sandbox.HardErrorsCount(br.Codeword, llrs)
		// Skip the SNR check in coverage mode for speed; pass +inf
		// so MinSNR2500DB doesn't reject. (The other 4 checks remain.)
		// LLRMetric=N1 since this codepath only computes the N=1 LLR
		// set; the cascade-aware tightening applies in MultiPassDecode.
		gateOK, _ := sandbox.AcceptDecode(br.DecodeMethod, sandbox.LLRMetricN1, nsync, grid, br.Codeword,
			hardErrs, 1000.0, gate)
		f.GateOK = gateOK
		if gateOK {
			f.Text = ur.Text
			f.FreqHz = r.FreqHz
			f.DtSec = r.DtSec
		}
		fates[i] = f
	}
	return fates
}

func classifyTruth(sig truth.Signal, fates []candidateFate,
	decodedByText map[string][]int,
	looseFreqHz, looseDtSec, normalFreqHz, normalDtSec float64,
	defaultMaxResults int) truthBucket {
	// MATCHED: any decoded fate with matching text within LOOSE tolerance.
	if idxs, ok := decodedByText[sig.Text]; ok {
		for _, i := range idxs {
			f := fates[i]
			if math.Abs(f.FreqHz-sig.FreqHz) <= looseFreqHz &&
				math.Abs(f.DtSec-sig.DTSec) <= looseDtSec {
				return bucketMATCHED
			}
		}
	}
	// Find nearest pre-NMS candidate within LOOSE.
	bestIdx := -1
	bestScore := math.Inf(1)
	for i, f := range fates {
		df := f.FreqHz - sig.FreqHz
		ddt := f.DtSec - sig.DTSec
		if math.Abs(df) > looseFreqHz || math.Abs(ddt) > looseDtSec {
			continue
		}
		score := df*df + ddt*ddt*1e4
		if score < bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return bucketA1
	}
	best := fates[bestIdx]
	if math.Abs(best.FreqHz-sig.FreqHz) > normalFreqHz ||
		math.Abs(best.DtSec-sig.DTSec) > normalDtSec {
		return bucketA2
	}
	if !best.InTopN {
		return bucketB
	}
	if !best.SurvivedNMS {
		return bucketC
	}
	if !best.BPDecoded {
		return bucketD
	}
	// BP-OK but gate rejected (or wrong text).
	return bucketE
}

// runNMSDiagnostic measures how NMS variants interact with the
// pre-NMS candidate pool: how many cells survive each variant's
// suppression, how many truth signals retain a kept candidate, and
// how often the corpus contains two truths at the same nominal
// frequency but different dt (the case per-freq K=2 timing-peak
// selection would buy).
//
// Approach: for each capture, run FindCandidatesRaw once (sorted by
// score, no NMS, no cap). For each variant — two standard NMS box
// sizes plus three K=2 per-freq-group variants — re-run the
// corresponding suppressor on the same raw list with MaxResults =
// nmsCap and tally:
//
//   - post-NMS count (after cap)
//   - truth signals whose nearest surviving candidate is within
//     ±5 Hz × ±0.5 s
//   - dropped candidates within truth tolerance of some truth signal
//   - dropped candidates not within tolerance of any truth signal
//
// Plus a corpus-wide count of same-freq-different-dt truth pairs
// (|Δf| < 3.125 Hz AND |Δdt| > 0.2 s).
//
// K=2 variants admit up to 2 candidates per freq group, suppressing
// only tight near-duplicates of already-kept candidates — the design
// probe for "two timing peaks per freq bin, tight near-dupe
// suppression only".
func runNMSDiagnostic(wavs []string, nmsCap int) {
	type variant struct {
		name  string
		apply func(raw []sandbox.Candidate, capN int) []sandbox.Candidate
	}
	variants := []variant{
		{
			name: "nms baseline   ±12.5Hz × ±0.12s",
			apply: func(raw []sandbox.Candidate, capN int) []sandbox.Candidate {
				return sandbox.SuppressOverlaps(raw, 12.5, 0.12, capN)
			},
		},
		{
			name: "nms F-tight    ±6.25Hz × ±0.12s",
			apply: func(raw []sandbox.Candidate, capN int) []sandbox.Candidate {
				return sandbox.SuppressOverlaps(raw, 6.25, 0.12, capN)
			},
		},
		{
			name: "nms F-narrow   ±3.125Hz × ±0.12s",
			apply: func(raw []sandbox.Candidate, capN int) []sandbox.Candidate {
				return sandbox.SuppressOverlaps(raw, 3.125, 0.12, capN)
			},
		},
		{
			name: "K=2 tight  d=3.125×0.04 grp=6.25",
			apply: func(raw []sandbox.Candidate, capN int) []sandbox.Candidate {
				return sandbox.SuppressOverlapsK2(raw, 3.125, 0.04, 6.25, 2, capN)
			},
		},
		{
			name: "K=2 medium d=6.25×0.08 grp=12.5",
			apply: func(raw []sandbox.Candidate, capN int) []sandbox.Candidate {
				return sandbox.SuppressOverlapsK2(raw, 6.25, 0.08, 12.5, 2, capN)
			},
		},
	}
	labels := []string{"base", "F-t ", "F-n ", "k2-t", "k2-m"}

	type varAgg struct {
		postNMS          int
		truthSurvived    int
		droppedNearTruth int
		droppedNoTruth   int
	}
	aggs := make([]varAgg, len(variants))

	var totalRaw int
	var totalTruth int
	var totalPairs int

	fmt.Printf("=== NMS variant diagnostic on %d captures (MaxResults=%d) ===\n\n", len(wavs), nmsCap)
	fmt.Printf("%-15s %7s %5s", "capture", "raw", "truth")
	for _, lbl := range labels {
		fmt.Printf("  %4s/%-3d %4s-srv", lbl, nmsCap, lbl)
	}
	fmt.Println()
	fmt.Println(strings.Repeat("-", 15+1+7+1+5+1+(14)*len(variants)))

	for _, w := range wavs {
		data, err := audio.ReadWAV(w)
		if err != nil || data.SampleRate != expectedSampleRate || data.Channels != 1 {
			log.Printf("skip %q", w)
			continue
		}
		manifest, err := truth.Read(truth.PathFor(w))
		if err != nil || manifest == nil {
			log.Printf("no truth for %q", w)
			continue
		}

		spec := sandbox.Spectrogram(data.Samples)
		if spec == nil {
			continue
		}
		opts := sandbox.DefaultSearchOptions()
		raw := sandbox.FindCandidatesRaw(spec, opts)
		totalRaw += len(raw)
		totalTruth += len(manifest.Signals)

		// Count same-freq-different-dt truth pairs in this capture.
		pairs := countSameFreqDiffDtTruth(manifest.Signals, 3.125, 0.2)
		totalPairs += pairs

		fmt.Printf("%-15s %7d %5d",
			strings.TrimSuffix(filepath.Base(w), ".wav"),
			len(raw), len(manifest.Signals))

		for i, v := range variants {
			kept := v.apply(raw, nmsCap)
			aggs[i].postNMS += len(kept)

			keptSet := make(map[float64]bool, len(kept))
			for _, k := range kept {
				keptSet[k.Sync*1e7+k.FreqHz*1e3+k.DtSec] = true
			}

			survived := 0
			for _, sig := range manifest.Signals {
				for _, k := range kept {
					if math.Abs(k.FreqHz-sig.FreqHz) <= freqMatchTolHz &&
						math.Abs(k.DtSec-sig.DTSec) <= dtMatchTolS {
						survived++
						break
					}
				}
			}
			aggs[i].truthSurvived += survived

			for _, c := range raw {
				key := c.Sync*1e7 + c.FreqHz*1e3 + c.DtSec
				if keptSet[key] {
					continue
				}
				nearTruth := false
				for _, sig := range manifest.Signals {
					if math.Abs(c.FreqHz-sig.FreqHz) <= freqMatchTolHz &&
						math.Abs(c.DtSec-sig.DTSec) <= dtMatchTolS {
						nearTruth = true
						break
					}
				}
				if nearTruth {
					aggs[i].droppedNearTruth++
				} else {
					aggs[i].droppedNoTruth++
				}
			}

			fmt.Printf(" %8d %8d", len(kept), survived)
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("-", 15+1+7+1+5+1+(14)*len(variants)))
	fmt.Printf("\nTotals: raw=%d, truth=%d, same-freq-diff-dt truth pairs=%d (Δf<3.125Hz, Δdt>0.2s)\n",
		totalRaw, totalTruth, totalPairs)

	fmt.Printf("\n%-40s %10s %14s %14s %14s\n",
		"variant", "post-NMS", "truth-surv", "dropped-near", "dropped-clean")
	fmt.Println(strings.Repeat("-", 100))
	for i, v := range variants {
		fmt.Printf("%-40s %10d  %4d / %3d   %14d %14d\n",
			v.name, aggs[i].postNMS, aggs[i].truthSurvived, totalTruth,
			aggs[i].droppedNearTruth, aggs[i].droppedNoTruth)
	}

	fmt.Println()
	fmt.Println("Reading the table:")
	fmt.Println("  post-NMS      = candidates surviving NMS+cap (capped at nmsCap)")
	fmt.Println("  truth-surv    = # of jt9-truth signals with a surviving candidate in ±5Hz×±0.5s tolerance")
	fmt.Println("  dropped-near  = candidates suppressed within truth tolerance (alias OR lost real signal)")
	fmt.Println("  dropped-clean = candidates suppressed with no truth nearby (correct alias/noise suppression)")
	fmt.Println()
	fmt.Println("K=2 variants admit up to 2 candidates per freq group; tight box only suppresses true near-dupes.")
	fmt.Println("If K=2 truth-surv > baseline truth-surv: the per-freq quota recovered real signal masked by NMS.")
	fmt.Println("If K=2 dropped-clean ≪ baseline: K=2 is admitting alias clusters baseline correctly dropped.")
}

// countSameFreqDiffDtTruth counts truth-pair occurrences where two
// signals share a frequency within freqTolHz AND are separated in dt
// by more than dtMinSec. This is the case where per-freq top-K timing-
// peak selection (with K ≥ 2) would buy something.
func countSameFreqDiffDtTruth(signals []truth.Signal, freqTolHz, dtMinSec float64) int {
	n := 0
	for i := 0; i < len(signals); i++ {
		for j := i + 1; j < len(signals); j++ {
			if math.Abs(signals[i].FreqHz-signals[j].FreqHz) < freqTolHz &&
				math.Abs(signals[i].DTSec-signals[j].DTSec) > dtMinSec {
				n++
			}
		}
	}
	return n
}
