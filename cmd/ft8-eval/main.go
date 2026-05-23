package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

func main() {
	var (
		subPasses  = flag.Int("subtraction-passes", 0, "DecodeOptions.SubtractionPasses (0=fast baseline, 1=sensitive)")
		osdOrder   = flag.Int("osd-order", 0, "DecodeOptions.OSDOrder (0=default order-1, -1=disable BP-only, 2=order-2)")
		llrScale   = flag.Float64("llr-scale", 0, "DecodeOptions.LLRScale (0=default 1.0)")
		ldpcIters  = flag.Int("ldpc-iters", 0, "DecodeOptions.LDPCMaxIterations (0=default 50)")
		maxCand    = flag.Int("max-cand", 0, "DecodeOptions.Sync.MaxCand sync candidate cap (0=default 100)")
		syncHalf   = flag.Float64("sync-half-span", 0, "DecodeOptions.Sync.SearchHalfSpanSec time-offset search half-span in seconds (0=default 2.0)")
		minSync    = flag.Float64("min-sync", 0, "DecodeOptions.MinSyncPower floor (0=default 3.0, negative=no floor, positive=exact)")
		osdMaxDist = flag.Float64("osd-maxdist", 0, "DecodeOptions.OSDMaxNormDist gate (B2): OSD soft-distance ceiling in (0,1]. 0=default, negative=no gate")
		llrFloor   = flag.Float64("llr-floor", 0, "DecodeOptions.LLRQualityFloor: per-attempt mean(|LLR|) early-exit threshold. 0=default 0.2, negative=disabled, positive=exact")
		useHash    = flag.Bool("hashtable", false, "use a fresh per-file codec.HashTable (retains hash-bearing Type 1/2/3 messages)")
		oracle     = flag.Bool("oracle", false, "run jt9 -8 (WSJT-X) as black-box ground truth; show SM-vs-jt9 parity")
		msgs       = flag.Bool("msgs", false, "list each decoded message per file")
		diff       = flag.Bool("diff", false, "with -oracle: show jt9 decodes marked found/MISS by SM (sorted by SNR) + SM-only extras")
		cands      = flag.Bool("candidates", false, "dump the sync candidate list (freq/dt/power) per file — diagnoses sync vs demod misses")
		runs       = flag.Int("runs", 1, "decode each file N times; report the fastest wall time")
		mem        = flag.Bool("mem", false, "report heap bytes allocated per decode (first run, TotalAlloc delta)")
		jobs       = flag.Int("jobs", 1, "decode files concurrently with N workers (count-only sweeps; forced to 1 with -mem or -runs>1)")
		stats      = flag.Bool("stats", false, "collect + print per-file sync/decode diagnostics (cap displacement, coarse-vs-retry, wasted retry budget on failed candidates)")
	)
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}
	if *runs < 1 {
		*runs = 1
	}

	files, err := expandWavs(flag.Args())
	if err != nil {
		fatal("%v", err)
	}
	if len(files) == 0 {
		fatal("no .wav files found in arguments")
	}

	opts := ft8.DecodeOptions{
		SubtractionPasses: *subPasses,
		OSDOrder:          *osdOrder,
		LLRScale:          *llrScale,
		LDPCMaxIterations: *ldpcIters,
		MinSyncPower:      *minSync,
		OSDMaxNormDist:    *osdMaxDist,
		LLRQualityFloor:   *llrFloor,
		Sync: dsp.SyncOptions{
			MaxCand:           *maxCand,
			SearchHalfSpanSec: *syncHalf,
		},
	}

	oracleOK := *oracle
	if *oracle {
		if _, lookErr := exec.LookPath("jt9"); lookErr != nil {
			fmt.Fprintln(os.Stderr, "warning: -oracle requested but jt9 not on PATH; skipping oracle comparison")
			oracleOK = false
		}
	}

	// -mem and -runs timing demand a quiet CPU; force serial when either
	// is in play. Otherwise decode files concurrently — Decode is
	// single-threaded and CPU-bound, so count-only sweeps scale with cores.
	jobN := *jobs
	if jobN < 1 || *mem || *runs > 1 {
		jobN = 1
	}

	printConfig(opts, *runs, *useHash, jobN)
	printHeader(oracleOK, *mem)

	results := make([]fileResult, len(files))
	sem := make(chan struct{}, jobN)
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = evalFile(f, opts, oracleOK, *useHash, *runs, *mem, *stats)
		}(i, f)
	}
	wg.Wait()

	var smTotal, jt9Total, matchTotal int
	for _, r := range results {
		if r.skipped {
			continue
		}
		smTotal += len(r.decodes)
		if r.haveOracle {
			jt9Total += len(r.oracle)
			matchTotal += r.cmp.matched
		}
		printRow(r.name, len(r.decodes), len(r.oracle), r.cmp, r.best, r.allocBytes, r.haveOracle, *mem)
		if *msgs {
			printMessages(r.decodes)
		}
		if *diff && r.haveOracle {
			printDiff(r.decodes, r.oracle)
		}
		if *cands {
			printCandidates(r.samples, opts.Sync)
		}
	}

	printTotals(smTotal, jt9Total, matchTotal, oracleOK)

	if *stats {
		printStatsSummary(results)
	}
}

// fileResult is one file's evaluation, computed (possibly concurrently)
// then printed in input order.
type fileResult struct {
	name       string
	samples    []float32
	decodes    []ft8.DecodedMessage
	oracle     []oracleDecode
	cmp        cmp
	haveOracle bool
	best       time.Duration
	allocBytes uint64
	skipped    bool
	stats      *ft8.DecodeStats
}

func evalFile(f string, opts ft8.DecodeOptions, oracleOK, useHash bool, runs int, mem, collectStats bool) fileResult {
	data, err := audio.ReadWAV(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: read %s: %v\n", f, err)
		return fileResult{name: displayName(f), skipped: true}
	}

	r := fileResult{name: displayName(f), samples: data.Samples, best: time.Duration(1<<63 - 1)}
	for run := 0; run < runs; run++ {
		var before runtime.MemStats
		if mem && run == 0 {
			runtime.GC()
			runtime.ReadMemStats(&before)
		}
		// Fresh DecodeStats per run when -stats is set; only the
		// first run's stats are retained on the fileResult (decodes
		// are deterministic over runs, so run-0 is representative).
		runOpts := withHashTable(opts, useHash)
		var localStats *ft8.DecodeStats
		if collectStats {
			localStats = &ft8.DecodeStats{}
			runOpts.Stats = localStats
		}
		t0 := time.Now()
		res := ft8.Decode(data.Samples, runOpts)
		elapsed := time.Since(t0)
		if mem && run == 0 {
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			r.allocBytes = after.TotalAlloc - before.TotalAlloc
		}
		if elapsed < r.best {
			r.best = elapsed
		}
		if run == 0 {
			r.decodes = res
			r.stats = localStats
		}
	}

	if oracleOK {
		if od := runOracle(f); od != nil {
			r.oracle = od
			r.cmp = compareToOracle(r.decodes, od)
			r.haveOracle = true
		}
	}
	return r
}

// withHashTable returns opts with a fresh single-slot HashTable attached
// when requested. A per-file fresh table is the honest single-slot
// measure; the daemon reuses one table across slots (cross-slot hash
// resolution), but for corpus evaluation each file stands alone.
func withHashTable(opts ft8.DecodeOptions, use bool) ft8.DecodeOptions {
	if use {
		opts.HashTable = codec.NewHashTable(0) // 0 → default capacity
	}
	return opts
}

// oracleDecode is one decoded message parsed from jt9 -8 stdout.
type oracleDecode struct {
	snr  int
	freq int
	msg  string
}

// runOracle runs `jt9 -8 <wav>` in a throwaway working directory and
// returns the decoded messages (lines carrying the ` ~ ` decode marker;
// the trailing <DecodeFinished> line is excluded). Returns nil on
// failure. jt9 stdout lines look like:
//
//	000000  11  0.8 1353 ~  F1RCQ YO3OBB RR73
//	HHMMSS snr  dt  freq ~  message...
func runOracle(wav string) []oracleDecode {
	abs, err := filepath.Abs(wav)
	if err != nil {
		return nil
	}
	tmp, err := os.MkdirTemp("", "ft8-eval-jt9-")
	if err != nil {
		return nil
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "jt9", "-8", abs)
	cmd.Dir = tmp // keep jt9's side-effect files out of the repo
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: jt9 on %s: %v\n", filepath.Base(wav), err)
		return nil
	}

	var decodes []oracleDecode
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		marker := strings.Index(line, " ~ ")
		if marker < 0 {
			continue
		}
		fields := strings.Fields(line[:marker])
		d := oracleDecode{snr: 0, freq: 0}
		if len(fields) >= 4 {
			d.snr = atoiOr(fields[1], 0)
			d.freq = atoiOr(fields[3], 0)
		}
		d.msg = strings.TrimSpace(line[marker+len(" ~ "):])
		decodes = append(decodes, d)
	}
	return decodes
}

// printDiff aligns SM's decodes with jt9's by message key (the first two
// tokens — the two callsigns / "CQ CALL"), the stable identifier for a
// transmission across the report/grid noise that differs between
// decoders. jt9's decodes are listed weakest-SNR-first so the
// sensitivity story (are misses all weak, or spread?) is obvious;
// SM-only extras follow.
func printDiff(sm []ft8.DecodedMessage, oracle []oracleDecode) {
	smKeys := map[string]bool{}
	for _, r := range sm {
		smKeys[msgKey(r.Text)] = true
	}
	oracleKeys := map[string]bool{}
	for _, d := range oracle {
		oracleKeys[msgKey(d.msg)] = true
	}

	sorted := append([]oracleDecode(nil), oracle...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].snr < sorted[j].snr })

	miss := 0
	for _, d := range sorted {
		mark := "  ✓ "
		if !smKeys[msgKey(d.msg)] {
			mark = "✗ MISS"
			miss++
		}
		fmt.Printf("      %s  snr=%4d  %5d Hz   %q\n", mark, d.snr, d.freq, d.msg)
	}
	for _, r := range sm {
		if !oracleKeys[msgKey(r.Text)] {
			fmt.Printf("      SM+    sync=%5.2f  %4.0f Hz   %q\n", r.SyncPower, r.Freq, r.Text)
		}
	}
	fmt.Printf("      (jt9=%d, SM-missed=%d, SM-only=%d)\n", len(oracle), miss, countSMOnly(sm, oracleKeys))
}

// msgKey is the first two whitespace tokens, uppercased — the two
// callsigns (or "CQ CALL") that identify a transmission. Robust to
// trailing report/grid differences between decoders. Falls back to the
// whole normalized string for non-standard (free-text/telemetry) forms.
func msgKey(s string) string {
	f := strings.Fields(strings.ToUpper(s))
	switch {
	case len(f) >= 2:
		return f[0] + " " + f[1]
	case len(f) == 1:
		return f[0]
	default:
		return ""
	}
}

func countSMOnly(sm []ft8.DecodedMessage, oracleKeys map[string]bool) int {
	n := 0
	for _, r := range sm {
		if !oracleKeys[msgKey(r.Text)] {
			n++
		}
	}
	return n
}

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// expandWavs turns the argument list (files and/or directories) into a
// sorted, de-duplicated list of *.wav paths. Directories are walked one
// level deep — enough for testdata/ and captures/, not a recursive find.
func expandWavs(args []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", a, err)
		}
		if !info.IsDir() {
			if strings.EqualFold(filepath.Ext(a), ".wav") {
				add(a)
			}
			continue
		}
		entries, err := os.ReadDir(a)
		if err != nil {
			return nil, fmt.Errorf("read dir %s: %w", a, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".wav") {
				add(filepath.Join(a, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func displayName(p string) string {
	// Keep the parent dir for disambiguation (cap1.wav vs slot1.wav live
	// in different dirs) but drop deeper path noise.
	dir := filepath.Base(filepath.Dir(p))
	return filepath.Join(dir, filepath.Base(p))
}

func printConfig(opts ft8.DecodeOptions, runs int, useHash bool, jobs int) {
	fmt.Printf("DecodeOptions: subtraction_passes=%d osd_order=%d osd_maxdist=%g llr_scale=%g llr_floor=%g ldpc_iters=%d min_sync=%g sync_half_span=%g hashtable=%v  (runs=%d jobs=%d)\n\n",
		opts.SubtractionPasses, opts.OSDOrder, opts.OSDMaxNormDist, opts.LLRScale, opts.LLRQualityFloor, opts.LDPCMaxIterations, opts.MinSyncPower, opts.Sync.SearchHalfSpanSec, useHash, runs, jobs)
}

// printStatsSummary prints the per-file diagnostic table when -stats
// is set. Columns:
//
//	sync_raw / sync_ded / cap : pipeline funnel through the sync stage
//	dec / @coarse / @retry    : decoded candidates split by where success landed
//	failed / fail_attempts    : retry budget burned on candidates that never decoded
//
// "@coarse" = decode landed at (FreqIdxUsed=0, TimeIdxUsed=0); "@retry"
// = decode needed at least one fine retry on either axis.
func printStatsSummary(results []fileResult) {
	fmt.Println()
	fmt.Println("=== Decode-pipeline diagnostics (per-file) ===")
	fmt.Printf("%-28s %8s %8s %5s | %5s %8s %7s | %7s %16s\n",
		"file", "sync_raw", "sync_ded", "cap", "dec", "@coarse", "@retry", "failed", "fail_attempts(avg)")
	fmt.Println(strings.Repeat("-", 110))

	var totalRaw, totalDed, totalCap, totalDec, totalCoarse, totalRetry, totalFailed, totalFailAttempts int

	for _, r := range results {
		if r.skipped || r.stats == nil {
			continue
		}
		s := r.stats
		var dec, coarse, retry, failed, failAttemptsSum int
		for _, c := range s.Candidates {
			if c.Decoded {
				dec++
				if c.FreqIdxUsed == 0 && c.TimeIdxUsed == 0 {
					coarse++
				} else {
					retry++
				}
			} else {
				failed++
				failAttemptsSum += c.Attempts
			}
		}
		avgFail := 0.0
		if failed > 0 {
			avgFail = float64(failAttemptsSum) / float64(failed)
		}
		fmt.Printf("%-28s %8d %8d %5d | %5d %8d %7d | %7d %16.1f\n",
			r.name, s.RawSyncCandidates, s.AfterDedup, s.AfterCap,
			dec, coarse, retry, failed, avgFail)

		totalRaw += s.RawSyncCandidates
		totalDed += s.AfterDedup
		totalCap += s.AfterCap
		totalDec += dec
		totalCoarse += coarse
		totalRetry += retry
		totalFailed += failed
		totalFailAttempts += failAttemptsSum
	}

	fmt.Println(strings.Repeat("-", 110))
	avgFailTotal := 0.0
	if totalFailed > 0 {
		avgFailTotal = float64(totalFailAttempts) / float64(totalFailed)
	}
	fmt.Printf("%-28s %8d %8d %5d | %5d %8d %7d | %7d %16.1f\n",
		"TOTAL", totalRaw, totalDed, totalCap,
		totalDec, totalCoarse, totalRetry, totalFailed, avgFailTotal)
	fmt.Printf("\nDedup collapse: %d raw → %d deduped (%.1f%% suppressed); cap drops: %d deduped → %d capped\n",
		totalRaw, totalDed, percentSuppressed(totalRaw, totalDed), totalDed, totalCap)
	fmt.Printf("Retry economics: %d decoded total; %d at coarse (%.0f%%), %d needed retry (%.0f%%); failed candidates burned %d attempts total (avg %.1f / failed)\n",
		totalDec, totalCoarse, pct(totalCoarse, totalDec), totalRetry, pct(totalRetry, totalDec),
		totalFailAttempts, avgFailTotal)

	printLLRDistribution(results)
	printBPDistribution(results)
}

// printBPDistribution shows BP-exit syndrome-weight distributions for
// decoded vs failed candidates and the BP-converged-but-CRC-failed
// rate. The big question this answers: are failed candidates failing
// because BP never gets close (high min syndrome weight) or because
// BP converges to wrong codewords (CRC failures despite convergence)?
// That choice determines what a BP-internal early-exit should look
// like.
func printBPDistribution(results []fileResult) {
	var decodedMinSW, failedMinSW []float64
	var totalBPAttempts, totalConverged, totalCRCValid, totalOSDInvoked int
	for _, r := range results {
		if r.skipped || r.stats == nil {
			continue
		}
		for _, c := range r.stats.Candidates {
			if c.MinBPSyndromeWeight >= 0 {
				if c.Decoded {
					decodedMinSW = append(decodedMinSW, float64(c.MinBPSyndromeWeight))
				} else {
					failedMinSW = append(failedMinSW, float64(c.MinBPSyndromeWeight))
				}
			}
			totalBPAttempts += c.BPAttempts
			totalConverged += c.BPConvergeCount
			totalCRCValid += c.BPCRCValidCount
			totalOSDInvoked += c.OSDInvokedCount
		}
	}
	if len(decodedMinSW) == 0 && len(failedMinSW) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("=== BP-exit syndrome weight distribution (per candidate's best attempt) ===")
	fmt.Printf("%-24s %7s %10s %10s %10s %10s %10s %10s\n",
		"population", "n", "min", "p10", "p50", "p90", "max", "mean")
	fmt.Println(strings.Repeat("-", 100))
	printLLRRow("decoded candidates", decodedMinSW)
	printLLRRow("failed candidates", failedMinSW)

	fmt.Println()
	fmt.Printf("BP/OSD path counts (across all retry attempts):\n")
	fmt.Printf("  total BP attempts:           %d\n", totalBPAttempts)
	fmt.Printf("  BP converged:                %d  (%.1f%% of attempts)\n", totalConverged, pct(totalConverged, totalBPAttempts))
	fmt.Printf("  BP converged + CRC valid:    %d  (%.1f%% of attempts; %.1f%% of converged)\n",
		totalCRCValid, pct(totalCRCValid, totalBPAttempts), pct(totalCRCValid, totalConverged))
	fmt.Printf("  OSD invoked:                 %d  (%.1f%% of attempts)\n", totalOSDInvoked, pct(totalOSDInvoked, totalBPAttempts))
	fmt.Printf("  BP-only (no OSD needed):     %d  (%.1f%% of attempts)\n",
		totalBPAttempts-totalOSDInvoked, pct(totalBPAttempts-totalOSDInvoked, totalBPAttempts))

	// What fraction of failed candidates EVER got BP to converge?
	// And of those, what fraction had ≥ 1 CRC-valid attempt? "BP
	// converged but CRC failed" is the signature of close-but-wrong
	// alignment (often near-aliased copies of a real decode).
	if len(failedMinSW) > 0 {
		failedConvergedCount := 0
		for _, v := range failedMinSW {
			if v == 0 {
				failedConvergedCount++
			}
		}
		fmt.Printf("\nAmong %d FAILED candidates: %d (%.1f%%) had BP converge at least once (always to a CRC-invalid codeword).\n",
			len(failedMinSW), failedConvergedCount, pct(failedConvergedCount, len(failedMinSW)))
	}
}

// printLLRDistribution shows the BestMeanAbsLLR distribution for
// decoded vs failed candidates. The separation between the two
// distributions is what determines whether an LLR-quality early-exit
// gate is viable, and at what threshold. We want failed-candidate
// p90 << decoded-candidate p10 for a clean gate.
func printLLRDistribution(results []fileResult) {
	var decodedLLRs, failedLLRs []float64
	for _, r := range results {
		if r.skipped || r.stats == nil {
			continue
		}
		for _, c := range r.stats.Candidates {
			if c.Decoded {
				decodedLLRs = append(decodedLLRs, c.BestMeanAbsLLR)
			} else {
				failedLLRs = append(failedLLRs, c.BestMeanAbsLLR)
			}
		}
	}
	if len(decodedLLRs) == 0 && len(failedLLRs) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("=== LLR-quality distribution (mean(|LLR|) per candidate's best attempt) ===")
	fmt.Printf("%-24s %7s %10s %10s %10s %10s %10s %10s\n",
		"population", "n", "min", "p10", "p50", "p90", "max", "mean")
	fmt.Println(strings.Repeat("-", 100))
	printLLRRow("decoded candidates", decodedLLRs)
	printLLRRow("failed candidates", failedLLRs)

	// Headline separation read: if failed-p90 < decoded-p10, an
	// early-exit gate at any value in that gap would skip BP+OSD on
	// most failed attempts without losing any real decodes.
	if len(decodedLLRs) > 0 && len(failedLLRs) > 0 {
		decodedSorted := append([]float64(nil), decodedLLRs...)
		sort.Float64s(decodedSorted)
		failedSorted := append([]float64(nil), failedLLRs...)
		sort.Float64s(failedSorted)
		failedP90 := percentile(failedSorted, 0.90)
		decodedP10 := percentile(decodedSorted, 0.10)
		decodedMin := decodedSorted[0]
		gap := decodedP10 - failedP90
		fmt.Printf("\nSeparation: failed_p90=%.3f  decoded_p10=%.3f  gap=%.3f  decoded_min=%.3f\n",
			failedP90, decodedP10, gap, decodedMin)
		if gap > 0 {
			fmt.Printf("→ Clean gate exists at any threshold in (%.3f, %.3f); use just below decoded_min=%.3f to preserve every observed decode\n",
				failedP90, decodedP10, decodedMin)
		} else {
			fmt.Println("→ Distributions overlap; an absolute mean(|LLR|) gate would cost real decodes. Need a normalised statistic or different invariant.")
		}

		// How many failed candidates fall below the decoded_min?
		// That's the "obviously skippable" set — the lowest hanging
		// fruit for any LLR-based gate.
		var belowMin int
		for _, v := range failedSorted {
			if v < decodedMin {
				belowMin++
			} else {
				break
			}
		}
		fmt.Printf("Lowest hanging fruit: %d failed candidates (%.1f%%) have mean(|LLR|) < decoded_min=%.3f — these could be skipped pre-BP at zero real-decode cost\n",
			belowMin, pct(belowMin, len(failedSorted)), decodedMin)
	}
}

func printLLRRow(label string, values []float64) {
	if len(values) == 0 {
		fmt.Printf("%-24s %7d  (no samples)\n", label, 0)
		return
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	fmt.Printf("%-24s %7d %10.3f %10.3f %10.3f %10.3f %10.3f %10.3f\n",
		label, len(values),
		sorted[0],
		percentile(sorted, 0.10),
		percentile(sorted, 0.50),
		percentile(sorted, 0.90),
		sorted[len(sorted)-1],
		mean)
}

// percentile returns the p-th percentile (p in [0,1]) of an already-
// sorted slice via nearest-rank. Falls back to min/max at the ends.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

func percentSuppressed(raw, ded int) float64 {
	if raw == 0 {
		return 0
	}
	return float64(raw-ded) * 100.0 / float64(raw)
}

func pct(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) * 100.0 / float64(denom)
}

// cmp is the honest comparison of an SM decode set against the oracle:
// matched = jt9 decodes SM also found, missed = jt9 decodes SM didn't,
// smOnly = SM decodes jt9 didn't (false-positive proxy).
type cmp struct{ matched, missed, smOnly int }

// compareToOracle matches by msgKey (first two tokens — the callsigns).
func compareToOracle(sm []ft8.DecodedMessage, oracle []oracleDecode) cmp {
	smKeys := map[string]bool{}
	for _, r := range sm {
		smKeys[msgKey(r.Text)] = true
	}
	oracleKeys := map[string]bool{}
	for _, d := range oracle {
		oracleKeys[msgKey(d.msg)] = true
	}
	var c cmp
	for _, d := range oracle {
		if smKeys[msgKey(d.msg)] {
			c.matched++
		} else {
			c.missed++
		}
	}
	for _, r := range sm {
		if !oracleKeys[msgKey(r.Text)] {
			c.smOnly++
		}
	}
	return c
}

func printHeader(oracle, mem bool) {
	// With the oracle, "match" (real decodes matched to jt9) is the
	// honest score; "sm" stays visible so false-positive inflation
	// (sm > match) is obvious. parity = match / jt9.
	line := fmt.Sprintf("%-26s %5s", "file", "sm")
	if oracle {
		line += fmt.Sprintf(" %5s %5s %5s %6s %7s", "jt9", "match", "miss", "extra", "parity")
	}
	line += fmt.Sprintf(" %9s", "time")
	if mem {
		line += fmt.Sprintf(" %9s", "alloc")
	}
	fmt.Println(line)
	fmt.Println(strings.Repeat("-", len(line)))
}

func printRow(name string, sm, jt9 int, c cmp, dur time.Duration, allocBytes uint64, oracle, mem bool) {
	line := fmt.Sprintf("%-26s %5d", name, sm)
	if oracle {
		par := "n/a"
		if jt9 > 0 {
			par = fmt.Sprintf("%d%%", c.matched*100/jt9)
		}
		line += fmt.Sprintf(" %5d %5d %5d %6d %7s", jt9, c.matched, c.missed, c.smOnly, par)
	}
	line += fmt.Sprintf(" %9s", dur.Round(time.Millisecond))
	if mem {
		line += fmt.Sprintf(" %8.1fM", float64(allocBytes)/(1<<20))
	}
	fmt.Println(line)
}

// printCandidates dumps the sync stage's output (the same Spectrogram +
// Sync that Decode runs first), sorted by frequency. Lets us see whether
// a missed signal even produced a candidate (sync gap) or produced one
// that the demod/LDPC stage then failed to decode (downstream gap).
func printCandidates(samples []float32, opts dsp.SyncOptions) {
	spec := dsp.Spectrogram(samples)
	cs := dsp.Sync(spec, opts)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Freq < cs[j].Freq })
	fmt.Printf("      sync candidates: %d\n", len(cs))
	for _, c := range cs {
		fmt.Printf("        %7.2f Hz  %+.2f s  power=%6.2f\n", c.Freq, c.DT, c.SyncPower)
	}
}

func printMessages(results []ft8.DecodedMessage) {
	for i, r := range results {
		fmt.Printf("      [%2d] %7.2f Hz  %+.2f s  sync=%5.2f  %q\n",
			i, r.Freq, r.DT, r.SyncPower, r.Text)
	}
}

func printTotals(sm, jt9, match int, oracle bool) {
	fmt.Println()
	if oracle && jt9 > 0 {
		fmt.Printf("TOTAL  sm=%d  jt9=%d  match=%d  parity=%d%%  (sm-only/false+=%d)\n",
			sm, jt9, match, match*100/jt9, sm-match)
		return
	}
	fmt.Printf("TOTAL  sm=%d\n", sm)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: ft8-eval [flags] <wav-or-dir>...\n\n")
	fmt.Fprintf(os.Stderr, "Runs ft8.Decode on each WAV and reports decode count + timing.\n\n")
	flag.PrintDefaults()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
