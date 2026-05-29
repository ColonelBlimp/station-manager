// sandbox-asym-ab runs the sandbox MultiPassDecode pipeline twice on
// the same WAV — once with the channelizer's default symmetric Extract
// mode and once with the FT8-tuned asymmetric mode toggled on via
// Channelizer.SetAsymmetricFT8Slice — and reports a side-by-side
// scoreboard so the A/B is interpretable in one glance.
//
// Used to validate the asymmetric-slice channelizer experiment per the
// 2026-05-28 Session 102 measurement plan. Operates on synthetic
// fixtures (clean / two-signal) where the expected decodes are known
// by construction; truth is supplied either via the WAV's truth
// manifest (when present) or via the inline -expect flag.
//
// Usage:
//
//	go run ./research/cmd/sandbox-asym-ab \
//	    -wav captures/synthetic/clean_SNR-6_seed1.wav \
//	    -expect "1500:0:CQ K1JT FN20"
//
//	go run ./research/cmd/sandbox-asym-ab \
//	    -wav captures/synthetic/eq_dF30_SNR-6_seed1.wav \
//	    -expect "1500:0:CQ K1JT FN20,1530:0:CQ G0ABC IO91"
//
// Import rules: research code may use internal/audio (stdlib + research)
// but MUST NOT import internal/ft8/*.
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

const expectedSampleRate = 12000

type expected struct {
	freqHz float64
	dtSec  float64
	text   string
}

type modeResult struct {
	label    string
	decodes  []sandbox.DecodeRecord
	matched  int
	extras   int
	methods  map[string]int
	matchMap []bool          // matched[i] = true if expected[i] was hit
	margins  []marginMetrics // per-expected[] margin diagnostic; len == len(expected)

	// llrMetricByTruth records the LLR metric ("N1", "N2", "N3", ...)
	// of the decode that matched expected[i]. Empty string if the truth
	// was not matched. Used by the corpus aggregate to surface which
	// metric uniquely recovers which truths.
	llrMetricByTruth []string

	// llrMetricCounts is a histogram of the LLR metric used by every
	// decode in this run, regardless of whether the decode matched a
	// truth. Useful for understanding cascade utilisation independent
	// of correctness.
	llrMetricCounts map[string]int

	// shadowRejects is the diagnostic channel from MultiPassDecodeFull:
	// every candidate that passed BPDecode + Unpack77 but failed the
	// post-decode quality gate. Used by the corpus audit to answer
	// "are oracle misses gate-rejects or true decode failures?".
	shadowRejects []sandbox.ShadowReject
}

// marginMetrics captures the per-truth LLR-margin signal at the known
// truth location, independent of whether the decoder's candidate
// scanner found and refined that signal end-to-end. The diagnostic
// answers the sub-parity question: "even if both modes decode, does
// asymmetric move the LLRs farther from / closer to the true codeword?"
//
//   - HardErrors: count of channel-LLR hard-decision bits that disagree
//     with the true codeword. Lower = better LLR quality at the truth.
//   - SoftDistance: weighted |LLR| mass on disagreeing bits, normalised
//     by total |LLR| mass. Captures wrong-bit *confidence*: a few
//     loud wrong bits is worse than many soft wrong bits. Range [0, 1];
//     lower = better.
//   - MedianAbsLLR: median |LLR| across all 174 bits. Context for
//     overall LLR scale; secondary signal.
//   - BPIterations: BPDecode loop iteration count on these LLRs. Lower
//     means BP converged faster, an indirect margin signal.
//   - DecodeMethod: "BP" / "OSD-N" / "fail". A drop from BP to OSD or
//     from OSD-1 to OSD-2 indicates margin loss even with parity tied.
//   - LLRComputed: false when the truth-location refinement or LLR path
//     failed; surfaces channelizer-side errors that would otherwise
//     hide in zero-valued metrics.
type marginMetrics struct {
	HardErrors   int
	SoftDistance float64
	MedianAbsLLR float64
	BPIterations int
	DecodeMethod string
	LLRComputed  bool
}

func main() {
	wavPath := flag.String("wav", "", "single WAV to decode (use -dir for the corpus mode)")
	dirPath := flag.String("dir", "", "directory of real-capture WAVs with .truth.json manifests (corpus A/B mode)")
	expectStr := flag.String("expect", "", `expected decodes for single-WAV mode "freq:dt:text,..." (ignored with -dir; manifest is used)`)
	freqTol := flag.Float64("ftol", 5.0, "freq tolerance for truth matching (Hz)")
	dtTol := flag.Float64("dttol", 0.5, "dt tolerance for truth matching (s)")
	singlePass := flag.Bool("single", false, "single-pass mode (MaxPasses=1) — disables subtract+redecode loop")
	verbose := flag.Bool("v", false, "print per-WAV detail in corpus mode (otherwise summary only)")
	dumpExtras := flag.Bool("dump-extras", false, "in corpus mode, dump all asymmetric-only extras (decodes that don't match any truth and weren't produced by symmetric) with capture/freq/dt/text/method")
	dumpShadow := flag.Bool("dump-shadow-rejects", false, "in corpus mode, dump every CRC/unpack-valid candidate the post-decode gate rejected (per-capture, by mode), with NSync/ToneAgree/HardErrors/SNR/Reason/LLRMetric/Method; also surfaces per-(reason, metric) histograms and per-truth would-recover audit")
	maxHardErrors := flag.Int("max-hard-errors", -1, "override AcceptDecodeOptions.MaxHardErrors (default -1 = use sandbox default of 36)")
	enableAPCQ := flag.Bool("enable-apcq", false, "enable AP-CQ a priori decoding pass at end of cascade (QEX § 7 AP2 specialised to c28_1=CQ token)")
	apcqMag := flag.Float64("apcq-mag", 0, "override AP-CQ pinning magnitude (0 = package default ~10)")
	enableAP3 := flag.Bool("enable-ap3", false, "enable AP3 (QEX § 7 AP3) — pin both c28_1 and c28_2 from CallsignHashTable; runs after AP-CQ failure")
	ap3Mag := flag.Float64("ap3-mag", 0, "override AP3 pinning magnitude (0 = package default ~10)")
	ap3MaxK := flag.Int("ap3-max-k", 0, "AP3 hash-callsign cap per side (0 = default 8). Worst-case BP runs per failed candidate = (1+K)×K.")
	dumpMissed := flag.Bool("dump-missed", false, "in corpus mode, list every truth not matched by accepted decodes in either mode, with text + capture (used to answer 'which oracle misses are CQ-shaped?')")
	strict := flag.Bool("strict", false, "strict-parity mode (clean-room report § Behavioral Findings): forces all experimental decode-recovery features off (AP-CQ, AP3, BestOfN). Scoring is against the sequential jt9-default truth manifests; deep-mode recoveries are intentionally not admitted. Calibration knobs (sync gate, candidate-search breadth) keep their defaults at this scaffold stage.")
	searchThreshold := flag.Float64("search-threshold", 0, "override SearchOptions.Threshold (matched-filter score floor at candidate finder). 0 = package default (3.0). Clean-room report calibrates `near 1.8` for the six-file oracle corpus; measure before adopting.")
	magnitudeLLR := flag.Bool("magnitude-llr", false, "explicitly enable QEX § 6 magnitude-domain demap in non-strict mode (in strict mode it's the default). LLR = max|C|_{x=0} − max|C|_{x=1}, the paper-prescribed form. Has no effect when -legacy-power-llr is also set (legacy wins).")
	legacyPowerLLR := flag.Bool("legacy-power-llr", false, "force the legacy power-domain demap (LLR = max|C|²_{x=0} − max|C|²_{x=1}) even in strict mode. Use for A/B comparison against the pre-2026-05-29 baseline. Off-spec per QEX § 6.")
	nmsK := flag.Int("nms-k", 0, "override SearchOptions.K2MaxPerGroup (max kept candidates per freq group in K=2-medium NMS). 0 = package default (2). Session 105 funnel surfaced 7 NMS-suppressed truths at K=2; raise to measure recovery vs alias growth.")
	maxResults := flag.Int("max-results", 0, "override SearchOptions.MaxResults (post-NMS cap). 0 = package default (200). Pairs with -nms-k: raising K admits more candidates at NMS but the cap may then bite downstream; raising both together is the controlled test.")
	flag.Parse()

	if *wavPath == "" && *dirPath == "" {
		log.Fatalf("either -wav or -dir is required")
	}
	if *wavPath != "" && *dirPath != "" {
		log.Fatalf("-wav and -dir are mutually exclusive")
	}

	opts := sandbox.DefaultMultiPassOptions()
	if *singlePass {
		opts.MaxPasses = 1
	}
	if *maxHardErrors >= 0 {
		opts.Gate.MaxHardErrors = *maxHardErrors
	}
	if *enableAPCQ {
		opts.EnableAPCQ = true
	}
	if *apcqMag > 0 {
		opts.APCQMag = *apcqMag
	}
	if *enableAP3 {
		opts.EnableAP3 = true
	}
	if *ap3Mag > 0 {
		opts.AP3Mag = *ap3Mag
	}
	if *ap3MaxK > 0 {
		opts.AP3MaxCallsigns = *ap3MaxK
	}
	if *searchThreshold > 0 {
		opts.Search.Threshold = *searchThreshold
	}
	if *nmsK > 0 {
		opts.Search.K2MaxPerGroup = *nmsK
	}
	if *maxResults > 0 {
		opts.Search.MaxResults = *maxResults
	}
	// LLR domain precedence (clearest case wins):
	//   -legacy-power-llr  → force power (off-spec; A/B against pre-2026-05-29 baseline)
	//   -magnitude-llr     → force magnitude
	//   -strict (no flags) → magnitude (QEX § 6 spec-aligned; baseline 113/144)
	//   no flags at all    → power (preserve historical non-strict default)
	switch {
	case *legacyPowerLLR:
		opts.MagnitudeLLR = false
	case *magnitudeLLR:
		opts.MagnitudeLLR = true
	case *strict:
		opts.MagnitudeLLR = true
	default:
		opts.MagnitudeLLR = false
	}
	if *strict && *legacyPowerLLR {
		fmt.Fprintln(os.Stderr, "note: -strict + -legacy-power-llr → power-domain demap (A/B mode; off-spec per QEX § 6)")
	}

	if *strict {
		// Strict mode forces all experimental decode-recovery features
		// off regardless of how the per-feature flags were set. This is
		// the discipline: -strict cannot be subverted by also passing
		// -enable-apcq etc. Notify when an override is taking effect so
		// the user isn't surprised.
		if opts.EnableAPCQ {
			fmt.Fprintln(os.Stderr, "note: -strict overrides -enable-apcq (AP-CQ disabled)")
		}
		if opts.EnableAP3 {
			fmt.Fprintln(os.Stderr, "note: -strict overrides -enable-ap3 (AP3 disabled)")
		}
		if opts.EnableBestOfN {
			fmt.Fprintln(os.Stderr, "note: -strict overrides EnableBestOfN (BestOfN disabled)")
		}
		opts.EnableAPCQ = false
		opts.EnableAP3 = false
		opts.EnableBestOfN = false
		printStrictBanner(opts.MagnitudeLLR)
	}

	if *dirPath != "" {
		runCorpus(*dirPath, opts, *freqTol, *dtTol, *verbose, *dumpExtras, *dumpShadow, *dumpMissed, *strict)
		return
	}
	runSingle(*wavPath, *expectStr, opts, *freqTol, *dtTol, *strict)
}

// printStrictBanner emits the strict-mode header explaining what the
// scaffold currently enforces. Kept terse but explicit so the
// invariant is visible in every strict-mode run's transcript — future
// readers diffing strict-mode scoreboards across commits can see at a
// glance which knobs were in scope.
//
// Asymmetric-channelizer reasoning (interpretation A locked in
// 2026-05-29): the FT8-tuned asymmetric slice admits decodes that
// symmetric does not (e.g. `JY5IB EA3DYI JN11` on `20m_slot2`,
// observed via pass=2 OSD-2 at 1386 Hz). That recovery is
// behaviourally a deeper-mode search, so it falls into the
// "experimental decode recovery" bucket per the clean-room report
// and is excluded from strict scoring. `-strict` therefore runs
// symmetric only; without `-strict`, the A/B comparison still
// surfaces the asym deltas as deep-mode data.
func printStrictBanner(magnitudeMode bool) {
	fmt.Println("=== STRICT-PARITY MODE ===")
	fmt.Println("  channelizer: symmetric only (asymmetric = deep-mode recovery)")
	fmt.Println("  experimental knobs disabled: AP-CQ, AP3, BestOfN")
	if magnitudeMode {
		fmt.Println("  LLR domain: magnitude (QEX § 6 spec-aligned; -legacy-power-llr to A/B)")
	} else {
		fmt.Println("  LLR domain: POWER (legacy/off-spec; -legacy-power-llr override active)")
	}
	fmt.Println("  scoring: sequential jt9-default truth manifests")
	fmt.Println("  calibration knobs (sync gate, candidate-search breadth): defaults — TBD")
	fmt.Println()
}

// runSingle is the original single-WAV path: optional inline -expect
// list, full per-truth margin diagnostic, decode-list dump. When
// strict is true, the asymmetric channelizer is skipped entirely (see
// printStrictBanner for the framing).
func runSingle(wavPath, expectStr string, opts sandbox.MultiPassOptions, freqTol, dtTol float64, strict bool) {
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		log.Fatalf("read %q: %v", wavPath, err)
	}
	if data.SampleRate != expectedSampleRate {
		log.Fatalf("%q: sample rate %d Hz, want %d", wavPath, data.SampleRate, expectedSampleRate)
	}
	if data.Channels != 1 {
		log.Fatalf("%q: %d channels, want mono", wavPath, data.Channels)
	}

	exp, err := parseExpected(expectStr)
	if err != nil {
		log.Fatalf("parse -expect: %v", err)
	}

	fmt.Printf("=== %s ===\n", wavPath)
	fmt.Printf("  %d samples @ %d Hz; %d expected decode(s)\n\n",
		len(data.Samples), data.SampleRate, len(exp))

	symOpts := opts
	symOpts.UseAsymmetricSlice = false
	// Single-WAV mode: each run starts with an empty hash table.
	// There's no prior fixture to carry calls from.
	sym := runOnce("symmetric", data.Samples, symOpts, exp, freqTol, dtTol, sandbox.NewCallsignHashTable())

	if strict {
		// Strict mode: symmetric only. Print sym detail; suppress the
		// A/B scoreboard (no asym row to compare against).
		printRun(sym)
		fmt.Println("--- scoreboard (strict / sym-only) ---")
		fmt.Printf("  %-18s  %7d  %6d  %5d\n", "mode", "matched", "extras", "total")
		fmt.Printf("  %-18s  %7d  %6d  %5d\n", sym.label, sym.matched, sym.extras, len(sym.decodes))
		fmt.Println()
		return
	}

	asymOpts := opts
	asymOpts.UseAsymmetricSlice = true
	asym := runOnce("asymmetric-FT8", data.Samples, asymOpts, exp, freqTol, dtTol, sandbox.NewCallsignHashTable())

	printRun(sym)
	printRun(asym)
	printScoreboard(sym, asym, exp)
}

// runCorpus iterates all *.wav files in dir that have a sibling
// .truth.json manifest, runs the paired A/B on each, and emits a
// per-capture scoreboard plus a corpus-level aggregate at the end.
// This is the step-3 entry point for the asymmetric-channelizer
// experiment against the real-capture corpus.
// extraDump records an asym-only extra: capture name, decode metadata,
// and whether it sits near any truth-position frequency (within ±20 Hz).
// "near-truth" extras are candidates for plausible-jt9-miss signals;
// far-from-truth extras with low-quality decode methods are more likely
// CRC-lottery or gate failures.
type extraDump struct {
	capture     string
	decode      sandbox.DecodeRecord
	nearTruth   bool
	nearestText string
	nearestHz   float64
}

func runCorpus(dir string, opts sandbox.MultiPassOptions, freqTol, dtTol float64, verbose, dumpExtras, dumpShadow, dumpMissed, strict bool) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.wav"))
	if err != nil {
		log.Fatalf("glob %q: %v", dir, err)
	}
	sort.Strings(matches)

	var (
		corpusSymMatched, corpusAsymMatched int
		corpusSymExtras, corpusAsymExtras   int
		corpusTotalTruths                   int
		transMissToMatch, transMatchToMiss  int
		asymOnlyExtras                      []extraDump
		// Per-LLR-metric attribution: count matched truths whose decode
		// succeeded via each metric. Indexed by mode label → metric →
		// count. Captured during the per-capture loop and totalled here
		// for the corpus aggregate.
		symMetricMatched  = map[string]int{}
		asymMetricMatched = map[string]int{}
		// Per-metric truths recovered ONLY by N=k (where lower-N
		// variants would have failed). Records the truth text + freq
		// for the writeup-quality table.
		symUniqueByMetric  = map[string][]string{}
		asymUniqueByMetric = map[string][]string{}

		// Shadow-reject audit accumulators. Populated regardless of
		// the -dump-shadow-rejects flag: the per-(reason, metric)
		// histograms and would-recover audit are always interesting
		// when the corpus runs, even if the per-reject dump is
		// expensive enough to gate on a flag.
		symShadowTotal, asymShadowTotal     int
		symShadowByReason                   = map[string]int{}
		asymShadowByReason                  = map[string]int{}
		symShadowByMetric                   = map[string]int{}
		asymShadowByMetric                  = map[string]int{}
		symWouldRecover, asymWouldRecover   []wouldRecoverEntry
		symShadowEntries, asymShadowEntries []shadowDumpEntry

		// Missed-truth accumulator (per-truth, per-mode boolean) for
		// the -dump-missed audit.
		missedSym, missedAsym []missedTruthEntry
	)

	// One hash table per mode, threaded across the entire corpus in
	// alphabetical filename order. Matches `jt9 -8 file1 file2 …`
	// behaviour: callsigns introduced by a decode in an earlier
	// fixture become available for hash resolution in later fixtures
	// (e.g. DG6JW/T introduced in 20m_slot2 → `<DG6JW/T> SV0TPN +01`
	// resolves in 20m_slot3). Symmetric and asymmetric modes have
	// independent tables so each mode's accumulated state reflects
	// only its own decoder's prior output.
	symHT := sandbox.NewCallsignHashTable()
	asymHT := sandbox.NewCallsignHashTable()

	for _, w := range matches {
		manifestPath := truth.PathFor(w)
		m, err := truth.Read(manifestPath)
		if err != nil {
			log.Printf("skip %q: read manifest: %v", w, err)
			continue
		}
		if m == nil {
			// No manifest: skip silently. Real-capture corpus always has
			// manifests; missing one is intentional (e.g. unrelated WAVs).
			continue
		}

		data, err := audio.ReadWAV(w)
		if err != nil {
			log.Printf("skip %q: %v", w, err)
			continue
		}
		if data.SampleRate != expectedSampleRate || data.Channels != 1 {
			log.Printf("skip %q: sample rate or channel count wrong", w)
			continue
		}

		exp := manifestToExpected(m)
		symOpts := opts
		symOpts.UseAsymmetricSlice = false
		sym := runOnce("symmetric", data.Samples, symOpts, exp, freqTol, dtTol, symHT)

		// In strict mode the asymmetric channelizer is skipped entirely
		// (it's classified as deep-mode recovery per interpretation A).
		// asym stays as the zero-value modeResult so downstream output
		// blocks gated by `if !strict` don't dereference live data.
		var asym modeResult
		if !strict {
			asymOpts := opts
			asymOpts.UseAsymmetricSlice = true
			asym = runOnce("asymmetric-FT8", data.Samples, asymOpts, exp, freqTol, dtTol, asymHT)
		}

		fmt.Printf("=== %s (%d truths) ===\n", filepath.Base(w), len(exp))
		fmt.Printf("  sym  matched=%2d extras=%2d total=%2d\n", sym.matched, sym.extras, len(sym.decodes))
		if !strict {
			fmt.Printf("  asym matched=%2d extras=%2d total=%2d\n", asym.matched, asym.extras, len(asym.decodes))

			// Per-truth transitions (A/B-only signal; meaningless in strict).
			captureMissToMatch, captureMatchToMiss := 0, 0
			for i := range exp {
				switch {
				case !sym.matchMap[i] && asym.matchMap[i]:
					captureMissToMatch++
					transMissToMatch++
					if verbose {
						fmt.Printf("    MISS→MATCH: %.1f Hz dt=%+.3f \"%s\"\n", exp[i].freqHz, exp[i].dtSec, exp[i].text)
					}
				case sym.matchMap[i] && !asym.matchMap[i]:
					captureMatchToMiss++
					transMatchToMiss++
					if verbose {
						fmt.Printf("    MATCH→MISS: %.1f Hz dt=%+.3f \"%s\"\n", exp[i].freqHz, exp[i].dtSec, exp[i].text)
					}
				}
			}
			if captureMissToMatch > 0 || captureMatchToMiss > 0 {
				fmt.Printf("  transitions: +%d miss→match, +%d match→miss\n", captureMissToMatch, captureMatchToMiss)
			}
		}
		fmt.Println()

		corpusSymMatched += sym.matched
		corpusSymExtras += sym.extras
		if !strict {
			corpusAsymMatched += asym.matched
			corpusAsymExtras += asym.extras
		}
		corpusTotalTruths += len(exp)

		// Per-metric attribution. The "unique to N=k" lists capture
		// truths recovered ONLY by N=k that lower-N variants didn't
		// crack — the load-bearing signal for "did adding N=k earn
		// its keep". We don't have a separate "would-have-failed-at-
		// N=1" run, so we approximate: a truth is "uniquely recovered
		// by N=k" if its winning decode's LLRMetric was N=k. This
		// double-counts cases where N=1 would have also worked under
		// a different candidate refinement (rare in practice).
		captureBase := filepath.Base(w)
		for i := range exp {
			if sym.matchMap[i] && sym.llrMetricByTruth[i] != "" {
				symMetricMatched[sym.llrMetricByTruth[i]]++
				if sym.llrMetricByTruth[i] != sandbox.LLRMetricN1 {
					symUniqueByMetric[sym.llrMetricByTruth[i]] = append(
						symUniqueByMetric[sym.llrMetricByTruth[i]],
						fmt.Sprintf("%s %.1f Hz %q", captureBase, exp[i].freqHz, exp[i].text),
					)
				}
			}
			if !strict && asym.matchMap[i] && asym.llrMetricByTruth[i] != "" {
				asymMetricMatched[asym.llrMetricByTruth[i]]++
				if asym.llrMetricByTruth[i] != sandbox.LLRMetricN1 {
					asymUniqueByMetric[asym.llrMetricByTruth[i]] = append(
						asymUniqueByMetric[asym.llrMetricByTruth[i]],
						fmt.Sprintf("%s %.1f Hz %q", captureBase, exp[i].freqHz, exp[i].text),
					)
				}
			}
		}

		if dumpExtras && !strict {
			// Collect asym-only extras: asym decodes that (i) don't match
			// any truth, AND (ii) don't appear in the symmetric decode set
			// (so we isolate decodes that asymmetric mode produced and
			// symmetric did not — the +5 net delta).
			for _, ad := range asym.decodes {
				if decodeMatchesAnyTruth(ad, exp, freqTol, dtTol) {
					continue
				}
				if decodeMatchesAny(ad, sym.decodes, freqTol, dtTol) {
					continue // also present in symmetric extras; not asym-only
				}
				e := extraDump{capture: filepath.Base(w), decode: ad}
				e.nearTruth, e.nearestText, e.nearestHz = findNearestTruth(ad, exp, 20.0)
				asymOnlyExtras = append(asymOnlyExtras, e)
			}
		}

		// Shadow-reject audit. Always accumulates by-reason / by-metric
		// histograms (cheap); records would-recover entries (truths
		// missed by accepted decodes but carried by a shadow-reject in
		// the same mode); records full per-reject dump entries when the
		// -dump-shadow-rejects flag is set.
		symShadowTotal += len(sym.shadowRejects)
		for _, r := range sym.shadowRejects {
			symShadowByReason[categorizeReason(r.Reason)]++
			symShadowByMetric[r.LLRMetric]++
		}
		if !strict {
			asymShadowTotal += len(asym.shadowRejects)
			for _, r := range asym.shadowRejects {
				asymShadowByReason[categorizeReason(r.Reason)]++
				asymShadowByMetric[r.LLRMetric]++
			}
		}
		for i, e := range exp {
			if !sym.matchMap[i] {
				if r, ok := findShadowRejectForTruth(sym.shadowRejects, e, freqTol, dtTol); ok {
					symWouldRecover = append(symWouldRecover, wouldRecoverEntry{
						capture: captureBase, truth: e, reject: r,
					})
				}
			}
			if !strict && !asym.matchMap[i] {
				if r, ok := findShadowRejectForTruth(asym.shadowRejects, e, freqTol, dtTol); ok {
					asymWouldRecover = append(asymWouldRecover, wouldRecoverEntry{
						capture: captureBase, truth: e, reject: r,
					})
				}
			}
		}
		if dumpShadow {
			for _, r := range sym.shadowRejects {
				symShadowEntries = append(symShadowEntries, shadowDumpEntry{
					capture: captureBase, reject: r,
					nearTruth: shadowIsNearTruth(r, exp, freqTol, dtTol),
				})
			}
			if !strict {
				for _, r := range asym.shadowRejects {
					asymShadowEntries = append(asymShadowEntries, shadowDumpEntry{
						capture: captureBase, reject: r,
						nearTruth: shadowIsNearTruth(r, exp, freqTol, dtTol),
					})
				}
			}
		}
		if dumpMissed {
			for i, e := range exp {
				if !sym.matchMap[i] {
					missedSym = append(missedSym, missedTruthEntry{capture: captureBase, truth: e})
				}
				if !strict && !asym.matchMap[i] {
					missedAsym = append(missedAsym, missedTruthEntry{capture: captureBase, truth: e})
				}
			}
		}
	}

	fmt.Println("=== CORPUS AGGREGATE ===")
	fmt.Printf("  total truths: %d across %d captures\n", corpusTotalTruths, len(matches))
	fmt.Printf("  %-18s  matched     extras\n", "mode")
	fmt.Printf("  %-18s  %3d/%3d  %5d\n", "symmetric", corpusSymMatched, corpusTotalTruths, corpusSymExtras)
	if !strict {
		fmt.Printf("  %-18s  %3d/%3d  %5d\n", "asymmetric-FT8", corpusAsymMatched, corpusTotalTruths, corpusAsymExtras)

		dMatched := corpusAsymMatched - corpusSymMatched
		dExtras := corpusAsymExtras - corpusSymExtras
		fmt.Printf("  delta vs symmetric:  matched %+d   extras %+d\n", dMatched, dExtras)
		fmt.Printf("  transitions:  miss→match %d   match→miss %d   net %+d\n",
			transMissToMatch, transMatchToMiss, transMissToMatch-transMatchToMiss)

		switch {
		case dMatched > 0 && dExtras <= 0:
			fmt.Println("  verdict: ASYMMETRIC WIN (+matched, no extras growth)")
		case dMatched < 0:
			fmt.Println("  verdict: ASYMMETRIC REGRESSION (lost truths)")
		case dMatched == 0 && dExtras > 0:
			fmt.Println("  verdict: ASYMMETRIC LOSS (extras grew, no truths gained)")
		case dMatched > 0 && dExtras > 0:
			fmt.Println("  verdict: MIXED (truths gained but extras grew too)")
		default:
			fmt.Println("  verdict: NEUTRAL (no parity change)")
		}
	}

	// Per-LLR-metric attribution table. Surfaces which block-N variant
	// recovered which truths, and which truths were uniquely cracked
	// by higher-N variants (i.e. the cascade was load-bearing for those
	// truths). N=1 = primary; N=2/N=3 are the cascade-recovered set.
	fmt.Println()
	fmt.Println("  per-LLR-metric matched truth count:")
	fmt.Printf("    %-16s  %-6s  %-6s  %-6s  %-7s  %-8s  %-6s  %-6s\n", "mode", "N=1", "N=2", "N=3", "N1Norm", "BestOfN", "APCQ", "AP3")
	fmt.Printf("    %-16s  %6d  %6d  %6d  %7d  %8d  %6d  %6d\n", "symmetric",
		symMetricMatched[sandbox.LLRMetricN1],
		symMetricMatched[sandbox.LLRMetricN2],
		symMetricMatched[sandbox.LLRMetricN3],
		symMetricMatched[sandbox.LLRMetricN1Norm],
		symMetricMatched[sandbox.LLRMetricBestOfN],
		symMetricMatched[sandbox.LLRMetricAPCQ],
		symMetricMatched[sandbox.LLRMetricAP3])
	if !strict {
		fmt.Printf("    %-16s  %6d  %6d  %6d  %7d  %8d  %6d  %6d\n", "asymmetric-FT8",
			asymMetricMatched[sandbox.LLRMetricN1],
			asymMetricMatched[sandbox.LLRMetricN2],
			asymMetricMatched[sandbox.LLRMetricN3],
			asymMetricMatched[sandbox.LLRMetricN1Norm],
			asymMetricMatched[sandbox.LLRMetricBestOfN],
			asymMetricMatched[sandbox.LLRMetricAPCQ],
			asymMetricMatched[sandbox.LLRMetricAP3])
	}

	for _, metric := range []string{sandbox.LLRMetricN2, sandbox.LLRMetricN3, sandbox.LLRMetricN1Norm, sandbox.LLRMetricBestOfN, sandbox.LLRMetricAPCQ, sandbox.LLRMetricAP3} {
		hasSym := len(symUniqueByMetric[metric]) > 0
		hasAsym := !strict && len(asymUniqueByMetric[metric]) > 0
		if hasSym || hasAsym {
			fmt.Printf("\n  truths recovered via %s (cascade-load-bearing):\n", metric)
			if hasSym {
				fmt.Printf("    symmetric (n=%d):\n", len(symUniqueByMetric[metric]))
				for _, t := range symUniqueByMetric[metric] {
					fmt.Printf("      %s\n", t)
				}
			}
			if hasAsym {
				fmt.Printf("    asymmetric (n=%d):\n", len(asymUniqueByMetric[metric]))
				for _, t := range asymUniqueByMetric[metric] {
					fmt.Printf("      %s\n", t)
				}
			}
		}
	}

	if dumpExtras && len(asymOnlyExtras) > 0 {
		fmt.Println()
		fmt.Printf("=== ASYMMETRIC-ONLY EXTRAS (n=%d) ===\n", len(asymOnlyExtras))
		fmt.Println("  (decodes asymmetric produced that symmetric did not, and that don't match any truth)")
		fmt.Printf("  %-16s  %8s  %7s  %5s  %-7s  %-10s  %-40s  near-truth\n",
			"capture", "freq(Hz)", "dt(s)", "pass", "metric", "method", "text")
		near := 0
		byMetric := map[string]int{}
		for _, e := range asymOnlyExtras {
			tag := "—"
			if e.nearTruth {
				tag = fmt.Sprintf("≈ %.0f Hz: %s", e.nearestHz, e.nearestText)
				near++
			}
			byMetric[e.decode.LLRMetric]++
			fmt.Printf("  %-16s  %8.2f  %+7.3f  %5d  %-7s  %-10s  %-40s  %s\n",
				e.capture, e.decode.FreqHz, e.decode.DtSec, e.decode.Pass,
				e.decode.LLRMetric, e.decode.DecodeMethod, truncate(e.decode.Text, 40), tag)
		}
		fmt.Printf("\n  near-truth extras: %d / %d (within ±20 Hz of a truth position)\n",
			near, len(asymOnlyExtras))
		fmt.Printf("  extras by LLR metric:")
		for _, m := range []string{sandbox.LLRMetricN1, sandbox.LLRMetricN2, sandbox.LLRMetricN3, sandbox.LLRMetricN1Norm, sandbox.LLRMetricBestOfN, sandbox.LLRMetricAPCQ, sandbox.LLRMetricAP3} {
			if byMetric[m] > 0 {
				fmt.Printf(" %s=%d", m, byMetric[m])
			}
		}
		fmt.Println()
	}

	// Shadow-reject audit block. Always emitted (the counts are
	// always interesting); -dump-shadow-rejects extends it with the
	// full per-capture per-reject dump.
	printShadowAudit(
		symShadowTotal, asymShadowTotal,
		symShadowByReason, asymShadowByReason,
		symShadowByMetric, asymShadowByMetric,
		symWouldRecover, asymWouldRecover,
	)
	if dumpShadow {
		printShadowDump("symmetric", symShadowEntries)
		if !strict {
			printShadowDump("asymmetric-FT8", asymShadowEntries)
		}
	}
	if dumpMissed {
		printMissedTruths("symmetric", missedSym)
		if !strict {
			printMissedTruths("asymmetric-FT8", missedAsym)
		}
	}
}

// missedTruthEntry pairs an unmatched truth with its capture name.
type missedTruthEntry struct {
	capture string
	truth   expected
}

// printMissedTruths emits the per-mode list of unmatched truths plus a
// CQ-format count. The CQ count answers "is AP-CQ relevant?": if zero
// missed truths are CQ-format, AP-CQ cannot help on this corpus
// regardless of magnitude or implementation detail.
func printMissedTruths(label string, entries []missedTruthEntry) {
	if len(entries) == 0 {
		return
	}
	cqCount := 0
	for _, e := range entries {
		if strings.HasPrefix(strings.TrimSpace(e.truth.text), "CQ ") {
			cqCount++
		}
	}
	fmt.Println()
	fmt.Printf("=== MISSED TRUTHS (%s, n=%d; CQ-format=%d) ===\n", label, len(entries), cqCount)
	for _, e := range entries {
		mark := " "
		if strings.HasPrefix(strings.TrimSpace(e.truth.text), "CQ ") {
			mark = "*"
		}
		fmt.Printf("  %s %-16s  %7.2f Hz  dt=%+.3f  %q\n",
			mark, e.capture, e.truth.freqHz, e.truth.dtSec, e.truth.text)
	}
}

// wouldRecoverEntry is a truth that wasn't matched by any accepted
// decode in this mode but whose Text and (FreqHz, DtSec) line up with
// a ShadowReject in the same mode. These are the load-bearing audit
// hits: each one is a truth the gate threw away that the decoder
// otherwise found.
type wouldRecoverEntry struct {
	capture string
	truth   expected
	reject  sandbox.ShadowReject
}

// shadowDumpEntry pairs a ShadowReject with its capture name and a
// flag noting whether it sits on top of a known truth position
// (within freqTol/dtTol AND matches truth text). Used for the
// -dump-shadow-rejects per-mode dump.
type shadowDumpEntry struct {
	capture   string
	reject    sandbox.ShadowReject
	nearTruth bool
}

// categorizeReason buckets the AcceptDecode reason string into one
// of a small set of named categories. The reason strings carry
// variable thresholds (e.g. "BP nsync 7 < 8") so bucket on the prefix
// before the numerics for the histogram. "other" is the catch-all for
// any reason that doesn't match a known prefix (signals an
// AcceptDecode change that should add a new category here).
func categorizeReason(reason string) string {
	switch {
	case strings.HasPrefix(reason, "hard-errors"):
		return "hard-errors"
	case strings.HasPrefix(reason, "snr"):
		return "snr"
	case strings.HasPrefix(reason, "BP nsync"):
		return "BP nsync"
	case strings.HasPrefix(reason, "OSD nsync"):
		return "OSD nsync"
	case strings.HasPrefix(reason, "OSD tone-agree"):
		return "OSD tone-agree"
	default:
		return "other"
	}
}

// findShadowRejectForTruth searches rejects for one whose (FreqHz,
// DtSec) sits within freqTol/dtTol of e and whose Text normalises to
// the truth text. Returns the first match. Mirrors the matcher in
// runOnce / decodeMatchesAnyTruth so the audit definition of "would
// recover" is consistent with the accepted-decode definition of
// "matched".
func findShadowRejectForTruth(rejects []sandbox.ShadowReject, e expected, freqTol, dtTol float64) (sandbox.ShadowReject, bool) {
	targetText := truth.NormalizeText(e.text)
	for _, r := range rejects {
		if math.Abs(r.FreqHz-e.freqHz) <= freqTol &&
			math.Abs(r.DtSec-e.dtSec) <= dtTol &&
			truth.NormalizeText(r.Text) == targetText {
			return r, true
		}
	}
	return sandbox.ShadowReject{}, false
}

// shadowIsNearTruth reports whether r lies within freqTol/dtTol of
// any truth in exp AND its text normalises to the truth text. Same
// shape as decodeMatchesAnyTruth but for shadow rejects.
func shadowIsNearTruth(r sandbox.ShadowReject, exp []expected, freqTol, dtTol float64) bool {
	rText := truth.NormalizeText(r.Text)
	for _, e := range exp {
		if math.Abs(r.FreqHz-e.freqHz) <= freqTol &&
			math.Abs(r.DtSec-e.dtSec) <= dtTol &&
			rText == truth.NormalizeText(e.text) {
			return true
		}
	}
	return false
}

// printShadowAudit emits the always-on shadow-reject summary: totals
// per mode, per-reason histogram, per-metric histogram, and the
// would-recover audit (truths missed by accepted decodes but
// carried by a shadow-reject — the load-bearing answer to "are
// oracle misses gate-rejects or true decode failures?").
func printShadowAudit(
	symTotal, asymTotal int,
	symByReason, asymByReason map[string]int,
	symByMetric, asymByMetric map[string]int,
	symWouldRecover, asymWouldRecover []wouldRecoverEntry,
) {
	fmt.Println()
	fmt.Println("=== SHADOW-REJECT AUDIT ===")
	fmt.Printf("  total shadow rejects:  sym=%d   asym=%d\n", symTotal, asymTotal)

	reasonKeys := []string{"hard-errors", "snr", "BP nsync", "OSD nsync", "OSD tone-agree", "other"}
	fmt.Println("  by reason:")
	fmt.Printf("    %-18s  %6s  %6s\n", "reason", "sym", "asym")
	for _, k := range reasonKeys {
		if symByReason[k] == 0 && asymByReason[k] == 0 {
			continue
		}
		fmt.Printf("    %-18s  %6d  %6d\n", k, symByReason[k], asymByReason[k])
	}

	metricKeys := []string{
		sandbox.LLRMetricN1, sandbox.LLRMetricN2, sandbox.LLRMetricN3,
		sandbox.LLRMetricN1Norm, sandbox.LLRMetricBestOfN, sandbox.LLRMetricAPCQ,
		sandbox.LLRMetricAP3,
	}
	fmt.Println("  by LLR metric:")
	fmt.Printf("    %-10s  %6s  %6s\n", "metric", "sym", "asym")
	for _, k := range metricKeys {
		if symByMetric[k] == 0 && asymByMetric[k] == 0 {
			continue
		}
		fmt.Printf("    %-10s  %6d  %6d\n", k, symByMetric[k], asymByMetric[k])
	}

	fmt.Printf("  would-recover (truth missed but shadow-reject carries truth text + freq):\n")
	fmt.Printf("    sym:  %d truth(s) the gate dropped\n", len(symWouldRecover))
	for _, w := range symWouldRecover {
		fmt.Printf("      %-16s  %7.2f Hz  dt=%+.3f  metric=%-7s  method=%-6s  reason=%q  truth=%q\n",
			w.capture, w.truth.freqHz, w.truth.dtSec, w.reject.LLRMetric, w.reject.Method,
			w.reject.Reason, w.truth.text)
	}
	fmt.Printf("    asym: %d truth(s) the gate dropped\n", len(asymWouldRecover))
	for _, w := range asymWouldRecover {
		fmt.Printf("      %-16s  %7.2f Hz  dt=%+.3f  metric=%-7s  method=%-6s  reason=%q  truth=%q\n",
			w.capture, w.truth.freqHz, w.truth.dtSec, w.reject.LLRMetric, w.reject.Method,
			w.reject.Reason, w.truth.text)
	}
}

// printShadowDump emits the full per-mode dump of every captured
// shadow-reject (only called when -dump-shadow-rejects is set). Sort
// by capture then freq for a stable order.
func printShadowDump(label string, entries []shadowDumpEntry) {
	if len(entries) == 0 {
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].capture != entries[j].capture {
			return entries[i].capture < entries[j].capture
		}
		return entries[i].reject.FreqHz < entries[j].reject.FreqHz
	})
	fmt.Println()
	fmt.Printf("=== SHADOW REJECTS (%s, n=%d) ===\n", label, len(entries))
	fmt.Printf("  %-16s  %8s  %7s  %5s  %-7s  %-7s  %5s  %5s  %5s  %7s  %-40s  %s\n",
		"capture", "freq(Hz)", "dt(s)", "pass", "metric", "method",
		"nsync", "tone", "hErr", "snr(dB)", "text", "reason")
	for _, e := range entries {
		flag := ""
		if e.nearTruth {
			flag = " [truth]"
		}
		fmt.Printf("  %-16s  %8.2f  %+7.3f  %5d  %-7s  %-7s  %5d  %5d  %5d  %7.1f  %-40s  %s%s\n",
			e.capture, e.reject.FreqHz, e.reject.DtSec, e.reject.Pass,
			e.reject.LLRMetric, e.reject.Method,
			e.reject.NSync, e.reject.ToneAgree, e.reject.HardErrors, e.reject.SNR2500DB,
			truncate(e.reject.Text, 40), e.reject.Reason, flag)
	}
}

// decodeMatchesAnyTruth reports whether d falls within freqTol/dtTol of
// any truth in exp AND matches the truth's text under
// truth.NormalizeText. Mirrors the scoring condition in runOnce so
// extras classification stays consistent with matched-count scoring.
func decodeMatchesAnyTruth(d sandbox.DecodeRecord, exp []expected, freqTol, dtTol float64) bool {
	decodeText := truth.NormalizeText(d.Text)
	for _, e := range exp {
		if math.Abs(d.FreqHz-e.freqHz) <= freqTol &&
			math.Abs(d.DtSec-e.dtSec) <= dtTol &&
			decodeText == truth.NormalizeText(e.text) {
			return true
		}
	}
	return false
}

// decodeMatchesAny reports whether d is the same physical decode as any
// in others, judged by NormalizeText-equality + freq/dt tolerance.
// Used to separate "asym-only" extras from "extras both modes agree
// on" — both decodes come from the sandbox so they share emit
// formatting, but routing through NormalizeText keeps the comparison
// shape identical to the truth matcher.
func decodeMatchesAny(d sandbox.DecodeRecord, others []sandbox.DecodeRecord, freqTol, dtTol float64) bool {
	dText := truth.NormalizeText(d.Text)
	for _, o := range others {
		if math.Abs(d.FreqHz-o.FreqHz) <= freqTol &&
			math.Abs(d.DtSec-o.DtSec) <= dtTol &&
			dText == truth.NormalizeText(o.Text) {
			return true
		}
	}
	return false
}

// findNearestTruth returns whether any truth's freq is within nearHz of
// d.FreqHz and, if so, the nearest truth's text + freq. Used to flag
// extras that look like jt9-misses (real signals on a frequency where
// jt9 didn't decode but a different decoder did).
func findNearestTruth(d sandbox.DecodeRecord, exp []expected, nearHz float64) (bool, string, float64) {
	best := nearHz + 1
	var bestText string
	var bestHz float64
	for _, e := range exp {
		diff := math.Abs(d.FreqHz - e.freqHz)
		if diff < best {
			best = diff
			bestText = e.text
			bestHz = e.freqHz
		}
	}
	if best <= nearHz {
		return true, bestText, bestHz
	}
	return false, "", 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// manifestToExpected converts a truth manifest's Signals into the
// per-WAV expected-list shape the rest of the tool uses.
func manifestToExpected(m *truth.Manifest) []expected {
	out := make([]expected, len(m.Signals))
	for i, s := range m.Signals {
		out[i] = expected{
			freqHz: s.FreqHz,
			dtSec:  s.DTSec,
			text:   s.Text,
		}
	}
	return out
}

// runOnce executes the decode pipeline once with the given options
// (whose UseAsymmetricSlice flag selects the channelizer mode) and
// scores its decodes against the expected truth list. The caller
// supplies the hash table: single-WAV mode passes a fresh one per
// run, corpus mode threads one per channelizer-mode across all
// fixtures in alphabetical order so hash references can resolve
// across files the same way `jt9 -8 file1 file2 …` does.
func runOnce(
	label string,
	audioSamples []float32,
	opts sandbox.MultiPassOptions,
	exp []expected,
	freqTol, dtTol float64,
	ht *sandbox.CallsignHashTable,
) modeResult {
	res := modeResult{
		label:            label,
		methods:          map[string]int{},
		matchMap:         make([]bool, len(exp)),
		llrMetricByTruth: make([]string, len(exp)),
		llrMetricCounts:  map[string]int{},
	}
	full := sandbox.MultiPassDecodeFull(audioSamples, opts, ht)
	res.decodes = full.Decodes
	res.shadowRejects = full.ShadowRejects
	res.margins = computeMargins(audioSamples, opts.UseAsymmetricSlice, exp)

	// Truth match. Compare text via truth.NormalizeText on both sides
	// so manifest-formatting variance (jt9-oracle trailing annotations
	// like "a1", and the "R-09" vs "R -09" report convention) doesn't
	// produce phantom mismatches. The raw decode/truth text is
	// preserved for the report dump.
	for _, d := range res.decodes {
		res.methods[d.DecodeMethod]++
		res.llrMetricCounts[d.LLRMetric]++
		matched := false
		decodeText := truth.NormalizeText(d.Text)
		for i, e := range exp {
			if res.matchMap[i] {
				continue
			}
			if math.Abs(d.FreqHz-e.freqHz) <= freqTol &&
				math.Abs(d.DtSec-e.dtSec) <= dtTol &&
				decodeText == truth.NormalizeText(e.text) {
				res.matchMap[i] = true
				res.llrMetricByTruth[i] = d.LLRMetric
				res.matched++
				matched = true
				break
			}
		}
		if !matched {
			res.extras++
		}
	}
	return res
}

func printRun(r modeResult) {
	fmt.Printf("--- %s mode ---\n", r.label)
	fmt.Printf("  decodes: %d  (matched=%d, extras=%d)\n", len(r.decodes), r.matched, r.extras)
	if len(r.methods) > 0 {
		methods := make([]string, 0, len(r.methods))
		for m := range r.methods {
			methods = append(methods, m)
		}
		sort.Strings(methods)
		fmt.Printf("  methods:")
		for _, m := range methods {
			fmt.Printf(" %s=%d", m, r.methods[m])
		}
		fmt.Println()
	}
	sort.Slice(r.decodes, func(i, j int) bool { return r.decodes[i].FreqHz < r.decodes[j].FreqHz })
	for _, d := range r.decodes {
		fmt.Printf("    %7.2f Hz  dt=%+6.3f s  pass=%d  %-8s  %s\n",
			d.FreqHz, d.DtSec, d.Pass, d.DecodeMethod, d.Text)
	}
	fmt.Println()
}

func printScoreboard(sym, asym modeResult, exp []expected) {
	fmt.Println("--- scoreboard ---")
	fmt.Printf("  %-18s  matched  extras  total\n", "mode")
	fmt.Printf("  %-18s  %7d  %6d  %5d\n", sym.label, sym.matched, sym.extras, len(sym.decodes))
	fmt.Printf("  %-18s  %7d  %6d  %5d\n", asym.label, asym.matched, asym.extras, len(asym.decodes))
	fmt.Println()

	dMatched := asym.matched - sym.matched
	dExtras := asym.extras - sym.extras
	verdict := "neutral"
	switch {
	case dMatched > 0 && dExtras <= 0:
		verdict = "ASYMMETRIC WIN (+matched, no extras growth)"
	case dMatched < 0:
		verdict = "ASYMMETRIC REGRESSION (lost truths)"
	case dExtras > 0 && dMatched == 0:
		verdict = "ASYMMETRIC LOSS (extras grew, no truths gained)"
	case dMatched > 0 && dExtras > 0:
		verdict = "MIXED (truths gained but extras grew too)"
	}
	fmt.Printf("  delta vs symmetric:  matched %+d   extras %+d   verdict: %s\n",
		dMatched, dExtras, verdict)

	// Per-truth match status.
	if len(exp) > 0 {
		fmt.Println()
		fmt.Println("  per-truth match status:")
		fmt.Printf("    %-30s  sym  asym\n", "truth")
		for i, e := range exp {
			tag := fmt.Sprintf("%.1f Hz dt=%+.3f \"%s\"", e.freqHz, e.dtSec, e.text)
			if len(tag) > 30 {
				tag = tag[:27] + "..."
			}
			fmt.Printf("    %-30s  %3s  %4s\n", tag,
				boolTag(sym.matchMap[i]), boolTag(asym.matchMap[i]))
		}
	}

	// Per-truth LLR-margin diagnostic. Two-mode side-by-side: any
	// change in HardErrors / SoftDistance reveals sub-parity LLR
	// quality shifts that decode counts hide.
	if len(exp) > 0 {
		fmt.Println()
		fmt.Println("  per-truth LLR margin (truth-location refinement):")
		fmt.Printf("    %-22s  %-15s  %-8s  %-12s  %-9s  %-6s\n",
			"truth", "mode", "hardErr", "softDist", "median|L|", "BP it")
		for i, e := range exp {
			tag := fmt.Sprintf("%.0fHz \"%s\"", e.freqHz, e.text)
			if len(tag) > 22 {
				tag = tag[:19] + "..."
			}
			sm, am := sym.margins[i], asym.margins[i]
			printMarginLine(tag, "symmetric", sm)
			printMarginLine("", "asymmetric-FT8", am)
			if sm.LLRComputed && am.LLRComputed {
				dHE := am.HardErrors - sm.HardErrors
				dSD := am.SoftDistance - sm.SoftDistance
				dMed := am.MedianAbsLLR - sm.MedianAbsLLR
				dIt := am.BPIterations - sm.BPIterations
				fmt.Printf("    %-22s  %-15s  %+8d  %+12.4f  %+9.2f  %+6d\n",
					"", "Δ (asym-sym)", dHE, dSD, dMed, dIt)
			}
		}
	}
}

func printMarginLine(tag, mode string, m marginMetrics) {
	if !m.LLRComputed {
		fmt.Printf("    %-22s  %-15s  %-8s  %-12s  %-9s  %-6s  (no LLR)\n",
			tag, mode, "—", "—", "—", "—")
		return
	}
	fmt.Printf("    %-22s  %-15s  %8d  %12.4f  %9.2f  %6d  %s\n",
		tag, mode, m.HardErrors, m.SoftDistance, m.MedianAbsLLR, m.BPIterations, m.DecodeMethod)
}

func boolTag(ok bool) string {
	if ok {
		return "yes"
	}
	return "—"
}

// computeMargins runs the truth-location margin diagnostic for each
// expected decode in exp. Builds a Channelizer with the requested
// mode, refines a candidate seeded at each truth's freq/dt, extracts
// the symbol grid, computes channel LLRs, and compares against the
// codeword the truth message encodes to. Returns one marginMetrics
// per truth in input order.
//
// The diagnostic uses the same RefineCandidate / ExtractSymbols /
// SoftLLRs pipeline the production decoder uses, so margin numbers
// are directly comparable to what BP sees during the actual decode.
// The only intentional deviation is that we seed the candidate at
// the known truth coordinates rather than letting FindCandidates
// surface it — the goal is to isolate channelizer-mode effects from
// scanner-noise effects.
func computeMargins(audioSamples []float32, asym bool, exp []expected) []marginMetrics {
	out := make([]marginMetrics, len(exp))
	if len(exp) == 0 {
		return out
	}
	ch, err := sandbox.NewChannelizer()
	if err != nil {
		return out
	}
	defer ch.Close()
	ch.SetAsymmetricFT8Slice(asym)
	if err := ch.Prepare(audioSamples); err != nil {
		return out
	}
	rOpts := sandbox.DefaultRefineOptions()
	bpOpts := sandbox.DefaultBPOptions()

	for i, e := range exp {
		trueCW, err := encodeTruthCodeword(e.text)
		if err != nil {
			continue
		}
		seed := sandbox.Candidate{
			FreqHz:       e.freqHz,
			DtSec:        e.dtSec,
			CoarseFreqHz: e.freqHz,
			CoarseDtSec:  e.dtSec,
		}
		refined, err := sandbox.RefineCandidate(ch, seed, rOpts)
		if err != nil {
			continue
		}
		grid, err := sandbox.ExtractSymbols(ch, refined)
		if err != nil {
			continue
		}
		llrs := sandbox.SoftLLRs(grid, false)
		out[i] = scoreMargin(llrs, trueCW, bpOpts)
	}
	return out
}

// scoreMargin computes the per-truth margin metrics from channel LLRs
// against a known true codeword. Sign convention (per
// research/sandbox/bp.go): positive LLR favours bit 0; negative favours
// bit 1. Hard error occurs when sign disagrees with the true bit.
func scoreMargin(llrs [174]float64, trueCW [174]uint8, bpOpts sandbox.BPOptions) marginMetrics {
	var (
		hardErrors   int
		softDisagree float64
		softTotal    float64
	)
	abs := make([]float64, 174)
	for i := 0; i < 174; i++ {
		a := math.Abs(llrs[i])
		abs[i] = a
		softTotal += a
		hardBit := uint8(0)
		if llrs[i] < 0 {
			hardBit = 1
		}
		if hardBit != trueCW[i]&1 {
			hardErrors++
			softDisagree += a
		}
	}
	sort.Float64s(abs)
	median := abs[len(abs)/2]
	softDist := 0.0
	if softTotal > 0 {
		softDist = softDisagree / softTotal
	}
	br := sandbox.BPDecode(llrs, bpOpts)
	return marginMetrics{
		HardErrors:   hardErrors,
		SoftDistance: softDist,
		MedianAbsLLR: median,
		BPIterations: br.Iterations,
		DecodeMethod: br.DecodeMethod,
		LLRComputed:  true,
	}
}

// encodeTruthCodeword builds the 174-bit LDPC codeword for a known
// truth message text. Supports the Type-1 standard layout used by
// every fixture in captures/synthetic/ (CQ <call> <grid> form).
//
// Returns an error for messages that aren't space-separated three-
// token Type-1 form — the diagnostic skips those truths rather than
// returning a garbage codeword.
func encodeTruthCodeword(text string) ([174]uint8, error) {
	var cw [174]uint8
	tokens := strings.Fields(text)
	if len(tokens) != 3 {
		return cw, fmt.Errorf("truth %q: expected 3 tokens for Type-1, got %d", text, len(tokens))
	}
	payload, err := sandbox.PackType1(tokens[0], tokens[1], tokens[2])
	if err != nil {
		return cw, fmt.Errorf("truth %q: PackType1: %w", text, err)
	}
	info := sandbox.PayloadToInfo91(payload)
	encoded := sandbox.EncodeLDPC(info)
	copy(cw[:], encoded[:])
	return cw, nil
}

func parseExpected(s string) ([]expected, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]expected, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields := strings.SplitN(p, ":", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("entry %q: want freq:dt:text", p)
		}
		var e expected
		var err error
		if _, err = fmt.Sscanf(fields[0], "%f", &e.freqHz); err != nil {
			return nil, fmt.Errorf("entry %q: freq: %w", p, err)
		}
		if _, err = fmt.Sscanf(fields[1], "%f", &e.dtSec); err != nil {
			return nil, fmt.Errorf("entry %q: dt: %w", p, err)
		}
		e.text = strings.TrimSpace(fields[2])
		out = append(out, e)
	}
	return out, nil
}
