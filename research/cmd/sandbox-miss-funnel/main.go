// sandbox-miss-funnel walks each truth signal in the corpus through
// the decode pipeline stages and reports the last stage each truth
// survived to. The output answers the operator-named question:
//
//	for the 33 strict-mode misses at the 113/144 baseline, where
//	exactly does each truth die — finder, NMS, cap, refinement,
//	BP/OSD, or gate?
//
// Stages walked (top of pipeline → bottom):
//
//  1. raw          — FindCandidatesRaw produced a candidate
//     within (freqTol, dtTol) of the truth.
//  2. post-NMS     — survived SuppressOverlapsK2 with effectively
//     no result cap.
//  3. post-cap     — survived the MaxResults cap (default 200).
//  4. refined      — survived RefineCandidate (refinement didn't
//     drift outside the tolerance).
//  5. decoded      — BP/OSD produced a CRC-valid codeword.
//     Detected as "truth-near-decode in either
//     MultiPassResult.Decodes or .ShadowRejects".
//  6. gate-passed  — AcceptDecode admitted the decode. Detected as
//     "in Decodes but text differs" vs "in
//     ShadowRejects with matching text".
//  7. matched      — gate-passed AND text matches via
//     truth.NormalizeText.
//
// Each missing truth is labeled by the stage it last appeared in
// (e.g., a truth present in post-NMS but missing from post-cap is
// "cap-bound"). Each label points at one piece of pipeline machinery
// — the natural attack surface for that miss.
//
// Defaults match the strict baseline: symmetric channelizer,
// magnitude-domain LLRs (QEX § 6), BestOfN/APCQ/AP3 off, one
// CallsignHashTable threaded across the corpus in alphabetical
// fixture order.
//
// Usage:
//
//	go run ./research/cmd/sandbox-miss-funnel -dir captures
//
// Black-box-oracle posture: this is a research diagnostic. It
// doesn't decode independently — it relies on MultiPassDecodeFull
// for stages 5-7 and reproduces stages 1-4 directly from the
// finder helpers.
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
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/sandbox"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

const expectedSampleRate = 12000

// stage encodes the last pipeline stage a truth survived to.
type stage string

const (
	stageMatched      stage = "matched"
	stageGateBound    stage = "gate-bound"
	stageDecoderBound stage = "decoder-bound"
	stageRefineBound  stage = "refine-bound"
	stageStage2Bound  stage = "stage2-bound"
	stageCapBound     stage = "cap-bound"
	stageNMSBound     stage = "nms-bound"
	stageFinderMiss   stage = "finder-miss"
)

// reportOrder defines the printing order for the aggregate count
// table. Top-of-pipeline (finder-miss) appears last so the most
// "downstream" outcome (matched) appears first.
var reportOrder = []stage{
	stageMatched,
	stageGateBound,
	stageDecoderBound,
	stageRefineBound,
	stageStage2Bound,
	stageCapBound,
	stageNMSBound,
	stageFinderMiss,
}

type truthFate struct {
	capture string
	freqHz  float64
	dtSec   float64
	text    string
	stage   stage
	// nmsNote is populated for stageNMSBound entries: an attribution
	// string of the form "raw@(f,dt) → suppressed by kept@(f',dt')
	// [tight-box|K-cap]". Lets a reader see at a glance whether the
	// 6.25 Hz / 0.08 s tight box is firing (raw fell too close to a
	// kept neighbour) or the K cap is firing (3+ candidates in the
	// 12.5 Hz freq group).
	nmsNote string

	// hasRefined is true when a near-truth refined candidate was
	// found in the funnel walker; audioGeo/gridGeo carry that
	// candidate's measurements. Off-by-default for upstream stages
	// (finder-miss / nms-bound / cap-bound / stage2-bound /
	// refine-bound) — no refined candidate exists to measure.
	hasRefined bool

	// audioGeo is the audio-side Stage2 GeoContrast for the closest
	// refined candidate near this truth. Only valid when
	// hasRefined is true.
	audioGeo float64

	// gridGeo is the symbol-grid Costas GeoContrast for the same
	// refined candidate after Channelizer.Extract + ExtractSymbols.
	// Only valid when hasRefined is true and grid extraction
	// succeeded; gridGeo == 0 with hasRefined == true indicates
	// grid extraction failed (rare; dt placed symbols out of
	// baseband bounds).
	gridGeo float64
}

// traceEntry pairs a sandbox CandidateTrace with the capture it came
// from. The funnel walks all traces across the corpus to answer the
// Session-107 BP/OSD attribution questions; per-truth matching is by
// capture + nearest coordinate.
type traceEntry struct {
	capture string
	trace   sandbox.CandidateTrace
}

// extraEvidence records one accepted decode that did NOT match any
// truth in the same WAV's manifest — a false-positive that survived
// the gate. Carries the same audio/grid pair of GeoContrast values so
// the symbol-quality bucketing applies to extras as well as missed
// truths.
type extraEvidence struct {
	capture  string
	freqHz   float64
	dtSec    float64
	text     string
	audioGeo float64
	gridGeo  float64
}

// symbolQualityThreshold is the audio/grid GeoContrast cutoff used
// for the four-cell quadrant classification (matches the new strict
// Stage2 default at 0.70). Same threshold on both axes so the
// quadrant labels stay symmetric and the bucketing is interpretable
// without per-axis calibration.
const symbolQualityThreshold = 0.70

// candScore records one post-NMS candidate plus its truth-proximity
// label. Drives the score-audit summary that answers "does the
// matched-filter score actually separate real signals from aliases?"
//
// Carries both the sandbox's Sync (matched-filter score) and the
// candidates-package Stage2 verifier outputs (WinsTotal,
// GeoContrast, MinBlockContrast) so the audit can rank each metric's
// near-truth vs alias separation against the same post-NMS population.
type candScore struct {
	capture          string
	freqHz           float64
	dtSec            float64
	sync             float64
	nearTruth        bool
	winsTotal        int
	geoContrast      float64
	minBlockContrast float64
}

func main() {
	dir := flag.String("dir", "captures", "directory of .wav files with paired .truth.json manifests (sequential jt9 oracle order, alphabetical fixture order)")
	freqTol := flag.Float64("ftol", 5.0, "freq tolerance for stage matching (Hz). Matches sandbox-asym-ab default.")
	dtTol := flag.Float64("dttol", 0.5, "dt tolerance for stage matching (s). Matches sandbox-asym-ab default.")
	magnitudeLLR := flag.Bool("magnitude-llr", true, "QEX § 6 spec-aligned magnitude-domain demap. Default true matches the strict-mode 113/23 baseline.")
	nmsK := flag.Int("nms-k", 0, "override SearchOptions.K2MaxPerGroup (max kept per freq group in NMS). 0 = package default (2).")
	maxResults := flag.Int("max-results", 0, "override SearchOptions.MaxResults (post-NMS cap). 0 = package default (200). Raise to test whether raising NMS K is wasted by the cap biting downstream.")
	stage2Mode := flag.String("stage2-mode", "off", "post-NMS Costas verifier mode: off | observe | filter | rerank. Mirrors sandbox-asym-ab semantics so funnel deltas match decoder runs at the same options.")
	stage2Metric := flag.String("stage2-metric", "minblock", "Stage2 discriminator: minblock | geo | wins.")
	stage2Threshold := flag.Float64("stage2-threshold", 0, "Stage2 filter threshold (units depend on -stage2-metric).")
	flag.Parse()

	matches, err := filepath.Glob(filepath.Join(*dir, "*.wav"))
	if err != nil {
		log.Fatalf("glob %q: %v", *dir, err)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		log.Fatalf("no .wav files found in %q", *dir)
	}

	opts := sandbox.DefaultMultiPassOptions()
	opts.UseAsymmetricSlice = false
	opts.MagnitudeLLR = *magnitudeLLR
	opts.TraceCandidates = true
	if *nmsK > 0 {
		opts.Search.K2MaxPerGroup = *nmsK
	}
	if *maxResults > 0 {
		opts.Search.MaxResults = *maxResults
	}
	if mode, ok := parseStage2Mode(*stage2Mode); ok {
		opts.Stage2Mode = mode
	} else {
		log.Fatalf("invalid -stage2-mode %q (want off | observe | filter | rerank)", *stage2Mode)
	}
	if metric, ok := parseStage2Metric(*stage2Metric); ok {
		opts.Stage2Metric = metric
	} else {
		log.Fatalf("invalid -stage2-metric %q (want minblock | geo | wins)", *stage2Metric)
	}
	opts.Stage2Threshold = *stage2Threshold

	refineOpts := sandbox.DefaultRefineOptions()
	searchOpts := opts.Search

	// One hash table threaded across the corpus in alphabetical order
	// to match strict-mode semantics. Sequential hash carry can
	// affect Type 4 unpacking and any cross-slot hashed reference
	// (DG6JW/T → <DG6JW/T> in 20m_slot3).
	ht := sandbox.NewCallsignHashTable()

	var fates []truthFate
	var scores []candScore
	var extras []extraEvidence
	var allTraces []traceEntry
	var totalRaw, totalNMS, totalCap, totalStage2, totalRefined int
	for _, w := range matches {
		m, err := truth.Read(truth.PathFor(w))
		if err != nil {
			log.Printf("skip %q: read manifest: %v", w, err)
			continue
		}
		if m == nil {
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

		// Stages 1-4: walk the finder + refinement directly.
		spec := sandbox.Spectrogram(data.Samples)
		raw := sandbox.FindCandidatesRaw(spec, searchOpts)
		// Effectively no cap on NMS — feed K=2 medium tight/group
		// values that mirror DefaultSearchOptions K2Medium. We pass
		// a maxKeep slightly above len(raw) so the cap doesn't bite
		// here; the cap-bound stage applies it explicitly below.
		funnelK := opts.Search.K2MaxPerGroup
		if funnelK <= 0 {
			funnelK = 2
		}
		postNMS := sandbox.SuppressOverlapsK2(raw, 6.25, 0.08, 12.5, funnelK, len(raw)+1)
		postCap := postNMS
		if searchOpts.MaxResults > 0 && len(postCap) > searchOpts.MaxResults {
			postCap = postCap[:searchOpts.MaxResults]
		}
		// Stage2 verifier runs between post-cap and refined to match
		// MultiPassDecodeFull's placement. ApplyStage2 returns the
		// input unchanged for Stage2Off / Stage2Observe.
		postStage2 := sandbox.ApplyStage2(data.Samples, append([]sandbox.Candidate(nil), postCap...), opts)
		refined, grids := refineAndExtract(data.Samples, postStage2, refineOpts)

		totalRaw += len(raw)
		totalNMS += len(postNMS)
		totalCap += len(postCap)
		totalStage2 += len(postStage2)
		totalRefined += len(refined)

		// Stages 5-7: rely on MultiPassDecodeFull for accepted +
		// gate-rejected. Anything not surfaced by either reached
		// the decoder but BP/OSD failed. TraceCandidates is enabled
		// in opts so result.Traces carries per-candidate decoder
		// records for the BP/OSD attribution analysis.
		result := sandbox.MultiPassDecodeFull(data.Samples, opts, ht)

		for _, t := range result.Traces {
			allTraces = append(allTraces, traceEntry{
				capture: filepath.Base(w),
				trace:   t,
			})
		}

		// Build the audio/grid GeoContrast pair for each accepted
		// decode that doesn't match any truth — the false-positive
		// "extras" the gate let through. A dedicated channelizer +
		// per-decode grid extraction is the cheapest route; running
		// the extract is ~2 ms per decode at corpus scale and only
		// fires on the ~20-30 extras per corpus run.
		extraCh, err := sandbox.NewChannelizer()
		if err == nil {
			extraCh.SetAsymmetricFT8Slice(false)
			if err := extraCh.Prepare(data.Samples); err == nil {
				for _, d := range result.Decodes {
					if isAcceptedExtra(d, m.Signals, *freqTol, *dtTol) {
						ec := sandbox.Candidate{FreqHz: d.FreqHz, DtSec: d.DtSec, Sync: 0}
						audio := sandbox.VerifyCostasAt(data.Samples, d.FreqHz, d.DtSec, 0)
						gridG := 0.0
						if grid, err := sandbox.ExtractSymbols(extraCh, ec); err == nil {
							gridG = sandbox.VerifyCostasGrid(grid).GeoContrast
						}
						extras = append(extras, extraEvidence{
							capture:  filepath.Base(w),
							freqHz:   d.FreqHz,
							dtSec:    d.DtSec,
							text:     d.Text,
							audioGeo: audio.GeoContrast,
							gridGeo:  gridG,
						})
					}
				}
			}
			extraCh.Close()
		}

		for _, s := range m.Signals {
			tf := truthFate{
				capture: filepath.Base(w),
				freqHz:  s.FreqHz,
				dtSec:   s.DTSec,
				text:    s.Text,
				stage: classify(s, raw, postNMS, postCap, postStage2, refined,
					result.Decodes, result.ShadowRejects,
					*freqTol, *dtTol),
			}
			if tf.stage == stageNMSBound {
				tf.nmsNote = nmsAttribute(s, raw, postNMS, *freqTol, *dtTol)
			}
			// Symbol-quality measurement: when a near-truth refined
			// candidate exists, record its audio + grid GeoContrast
			// pair so the quadrant bucketing can attribute the loss
			// to channelizer/extract (audio good + grid bad) vs
			// BP/OSD/LLR (audio good + grid good + no decode) vs
			// finder-admitted-noise (both weak) vs lucky-refinement
			// (audio weak + grid good).
			if idx := closestRefinedIndex(refined, s.FreqHz, s.DTSec, *freqTol, *dtTol); idx >= 0 {
				tf.hasRefined = true
				c := refined[idx]
				tf.audioGeo = sandbox.VerifyCostasAt(data.Samples, c.FreqHz, c.DtSec, c.Sync).GeoContrast
				if grids != nil && idx < len(grids) && grids[idx] != nil {
					tf.gridGeo = sandbox.VerifyCostasGrid(grids[idx]).GeoContrast
				}
			}
			fates = append(fates, tf)
		}

		// Score audit: label every post-NMS candidate as near-truth or
		// alias and record (a) sandbox's matched-filter score
		// (Candidate.Sync) and (b) the candidates-package Stage2
		// verifier outputs (WinsTotal, GeoContrast, MinBlockContrast).
		// Aggregated across the corpus, the per-metric near-truth-vs-
		// alias distributions show which discriminator (if any) cleanly
		// separates real signals from aliases. Sandbox + candidates DT
		// frames agree (both: dt = 0 ⇒ TX start at QEX nominal 0.5 s
		// slot offset), so the sandbox coordinate feeds directly into
		// VerifyCostas without remap.
		for _, c := range postNMS {
			v := candidates.VerifyCostas(data.Samples, c.FreqHz, c.DtSec, c.Sync)
			scores = append(scores, candScore{
				capture:          filepath.Base(w),
				freqHz:           c.FreqHz,
				dtSec:            c.DtSec,
				sync:             c.Sync,
				nearTruth:        signalAnyNear(m.Signals, c.FreqHz, c.DtSec, *freqTol, *dtTol),
				winsTotal:        v.WinsTotal,
				geoContrast:      v.GeoContrast,
				minBlockContrast: v.MinBlockContrast,
			})
		}
	}

	printReport(fates, *magnitudeLLR, opts.Stage2Mode, opts.Stage2Metric, opts.Stage2Threshold)
	printSurvival(totalRaw, totalNMS, totalCap, totalStage2, totalRefined, opts.Stage2Mode)
	printSymbolQuality(fates, extras)
	printDecoderTrace(fates, extras, allTraces, *freqTol, *dtTol)
	printScoreAudit(scores)
}

// findTracesNearTruth returns every traceEntry whose (capture,
// FreqHz, DtSec) lies within (freqTol, dtTol) of the given truth.
// Used by printDecoderTrace to answer "what did the decoder try on
// this truth?" — multiple traces (one per pass) can match.
func findTracesNearTruth(allTraces []traceEntry, capture string, freqHz, dtSec, freqTol, dtTol float64) []sandbox.CandidateTrace {
	var out []sandbox.CandidateTrace
	for _, te := range allTraces {
		if te.capture != capture {
			continue
		}
		if near(te.trace.FreqHz, te.trace.DtSec, freqHz, dtSec, freqTol, dtTol) {
			out = append(out, te.trace)
		}
	}
	return out
}

// findTraceForExtra returns the trace (if any) whose coordinate
// matches an accepted extra. Used by printDecoderTrace to answer
// Q4/Q5 (BP vs OSD success / which LLR metric won). Tight tolerance
// (1 Hz, 0.05 s) because accepted extras come from the same refined
// coordinate the trace recorded.
func findTraceForExtra(allTraces []traceEntry, capture string, freqHz, dtSec float64) (sandbox.CandidateTrace, bool) {
	for _, te := range allTraces {
		if te.capture != capture {
			continue
		}
		if te.trace.Outcome != "accepted" {
			continue
		}
		if math.Abs(te.trace.FreqHz-freqHz) <= 1.0 && math.Abs(te.trace.DtSec-dtSec) <= 0.05 {
			return te.trace, true
		}
	}
	return sandbox.CandidateTrace{}, false
}

// printDecoderTrace consumes the per-candidate trace records from
// MultiPassResult.Traces and answers the five Session-107 attribution
// questions:
//
//  1. For each missed truth that reached the refine stage (audio+grid
//     measurable population), did any LLR metric produce a CRC-valid
//     codeword? Bucket: had_crc_valid vs all_failed.
//  2. If had_crc_valid: was the candidate gate-rejected, AP-guard
//     killed, unpack-failed, or accepted-but-text-mismatched (the
//     "wrong text at same grid" pattern flagged in the symbol-quality
//     finding).
//  3. If all_failed: across every attempt, was failure mostly
//     BP-non-convergent (syndrome never clean) or
//     syndrome-clean-CRC-invalid (BP found a wrong LDPC codeword
//     and OSD couldn't repair it)?
//  4. For accepted extras: how many came from BP success vs OSD
//     success? (BR.DecodeMethod prefix.)
//  5. For accepted extras: which LLR metric won most often?
//
// The 19 audio_good_grid_good missed truths from the
// symbol-quality stage are the population that drives the headline
// answer to Q1-Q3 — they have clean grid evidence so any decoder
// failure is squarely a BP/OSD/LLR problem.
func printDecoderTrace(fates []truthFate, extras []extraEvidence, allTraces []traceEntry, freqTol, dtTol float64) {
	fmt.Println()
	fmt.Println("=== BP/OSD DECODER TRACE (per-candidate observe-mode trace) ===")
	fmt.Printf("  total trace records (all passes, all candidates): %d\n", len(allTraces))

	// Q1+Q2: missed-truth disposition.
	type missedRow struct {
		fate                    truthFate
		traces                  []sandbox.CandidateTrace
		hadCRCOK                bool
		outcomes                []string
		anyAttemptsWithSynClean int
		anyAttemptsBPFail       int
		totalAttempts           int
	}
	var rows []missedRow
	for _, f := range fates {
		if f.stage == stageMatched {
			continue
		}
		if !f.hasRefined {
			continue
		}
		ts := findTracesNearTruth(allTraces, f.capture, f.freqHz, f.dtSec, freqTol, dtTol)
		row := missedRow{fate: f, traces: ts}
		for _, t := range ts {
			row.outcomes = append(row.outcomes, t.Outcome)
			for _, a := range t.Attempts {
				row.totalAttempts++
				if a.BR.OK {
					row.hadCRCOK = true
				} else if a.BR.SyndromeClean {
					row.anyAttemptsWithSynClean++
				} else {
					row.anyAttemptsBPFail++
				}
			}
		}
		rows = append(rows, row)
	}

	// Q1 / Q2 aggregation.
	hadCRC := 0
	allFailed := 0
	q2Buckets := map[string]int{}
	q3SyndromeClean := 0
	q3BPNonConverged := 0
	for _, r := range rows {
		if r.hadCRCOK {
			hadCRC++
			// Q2 sub-bucket: classify by outcome of the trace that had
			// the CRC-OK attempt. Use the first such outcome.
			for _, o := range r.outcomes {
				if o == "accepted" {
					q2Buckets["accepted_wrong_text"]++
					break
				} else if strings.HasPrefix(o, "gate_reject:") {
					q2Buckets["gate_reject"]++
					break
				} else if o == "ap_guard_fail" {
					q2Buckets["ap_guard_fail"]++
					break
				} else if o == "unpack_fail" {
					q2Buckets["unpack_fail"]++
					break
				}
			}
		} else {
			allFailed++
			q3SyndromeClean += r.anyAttemptsWithSynClean
			q3BPNonConverged += r.anyAttemptsBPFail
		}
	}

	fmt.Println()
	fmt.Printf("--- Q1: did any metric produce a CRC-valid codeword on the missed truths? ---\n")
	fmt.Printf("  measurable missed truths (refined near-truth candidate exists): %d\n", len(rows))
	fmt.Printf("    had_crc_valid (some metric returned CRC-OK):  %d\n", hadCRC)
	fmt.Printf("    all_failed   (no metric returned CRC-OK):     %d\n", allFailed)

	if hadCRC > 0 {
		fmt.Println()
		fmt.Println("--- Q2: for had_crc_valid, what killed the decode downstream? ---")
		for _, k := range []string{"accepted_wrong_text", "gate_reject", "ap_guard_fail", "unpack_fail"} {
			fmt.Printf("  %-22s %3d\n", k, q2Buckets[k])
		}
	}

	if allFailed > 0 {
		fmt.Println()
		fmt.Println("--- Q3: for all_failed, what was the BP/OSD failure mode breakdown? ---")
		fmt.Printf("  total attempts across all_failed truths: %d\n",
			q3SyndromeClean+q3BPNonConverged)
		fmt.Printf("    syndrome_clean_crc_invalid (BP found wrong LDPC codeword + OSD didn't repair): %d\n",
			q3SyndromeClean)
		fmt.Printf("    BP_nonconvergent (syndrome never clean + OSD didn't find one):                 %d\n",
			q3BPNonConverged)
	}

	// Per-truth detail for the all_failed bucket — the load-bearing
	// "what did each missed truth's decoder actually see" listing.
	if allFailed > 0 {
		fmt.Println()
		fmt.Println("--- per-missed-truth attempt detail (all_failed bucket) ---")
		fmt.Println("  capture            freq Hz   dt    text                                          attempts")
		var detailRows []missedRow
		for _, r := range rows {
			if !r.hadCRCOK {
				detailRows = append(detailRows, r)
			}
		}
		sort.Slice(detailRows, func(i, j int) bool {
			if detailRows[i].fate.capture != detailRows[j].fate.capture {
				return detailRows[i].fate.capture < detailRows[j].fate.capture
			}
			return detailRows[i].fate.freqHz < detailRows[j].fate.freqHz
		})
		for _, r := range detailRows {
			text := r.fate.text
			if len(text) > 42 {
				text = text[:39] + "..."
			}
			fmt.Printf("  %-16s  %7.1f  %+5.2f  %-44s",
				r.fate.capture, r.fate.freqHz, r.fate.dtSec, text)
			// Render attempts as "metric:method/iter/synclean/crcv,..."
			var parts []string
			for _, t := range r.traces {
				for _, a := range t.Attempts {
					tag := "BPfail"
					if a.BR.OK {
						tag = "OK"
					} else if a.BR.SyndromeClean {
						tag = "SynClean!CRC"
					}
					parts = append(parts, fmt.Sprintf("%s:%s/i%d", a.Metric, tag, a.BR.Iterations))
				}
			}
			fmt.Printf("  %s\n", strings.Join(parts, ", "))
		}
	}

	// Q4 + Q5: accepted extras attribution.
	bpCount := 0
	osdCount := 0
	otherCount := 0
	metricCounts := map[string]int{}
	var matched, unmatched int
	for _, e := range extras {
		t, ok := findTraceForExtra(allTraces, e.capture, e.freqHz, e.dtSec)
		if !ok {
			unmatched++
			continue
		}
		matched++
		// Winning attempt is the last entry (cascade short-circuits on
		// success); identify by Attempts[last].
		if len(t.Attempts) == 0 {
			continue
		}
		winning := t.Attempts[len(t.Attempts)-1]
		metricCounts[winning.Metric]++
		switch {
		case strings.HasPrefix(winning.BR.DecodeMethod, "BP"):
			bpCount++
		case strings.HasPrefix(winning.BR.DecodeMethod, "OSD"):
			osdCount++
		default:
			otherCount++
		}
	}
	fmt.Println()
	fmt.Println("--- Q4: accepted extras — BP success vs OSD success ---")
	fmt.Printf("  total extras: %d (matched to trace: %d, unmatched: %d)\n",
		len(extras), matched, unmatched)
	fmt.Printf("    BP success:    %3d\n", bpCount)
	fmt.Printf("    OSD success:   %3d\n", osdCount)
	if otherCount > 0 {
		fmt.Printf("    other/unknown: %3d\n", otherCount)
	}

	fmt.Println()
	fmt.Println("--- Q5: accepted extras by winning LLR metric ---")
	var metricsSorted []string
	for k := range metricCounts {
		metricsSorted = append(metricsSorted, k)
	}
	sort.Strings(metricsSorted)
	for _, k := range metricsSorted {
		fmt.Printf("    %-10s %3d\n", k, metricCounts[k])
	}
}

// qualityBucket labels one (audio, grid) GeoContrast pair into the
// four-quadrant classification the operator named:
//
//   - "audio_good_grid_bad"  → channelizer/refine/extract problem
//   - "audio_good_grid_good" → BP/OSD/LLR problem (missed) or
//     "convincing-shape extra" (accepted)
//   - "audio_weak_grid_weak" → finder admitted a marginal/noisy
//     candidate
//   - "audio_weak_grid_good" → interesting (metric mismatch or
//     lucky refinement)
//
// Threshold is symbolQualityThreshold on both axes.
func qualityBucket(audioGeo, gridGeo float64) string {
	audioGood := audioGeo >= symbolQualityThreshold
	gridGood := gridGeo >= symbolQualityThreshold
	switch {
	case audioGood && gridGood:
		return "audio_good_grid_good"
	case audioGood && !gridGood:
		return "audio_good_grid_bad"
	case !audioGood && gridGood:
		return "audio_weak_grid_good"
	default:
		return "audio_weak_grid_weak"
	}
}

var qualityBucketOrder = []string{
	"audio_good_grid_good",
	"audio_good_grid_bad",
	"audio_weak_grid_good",
	"audio_weak_grid_weak",
}

// printSymbolQuality emits the operator-named symbol-quality
// classification report:
//
//  1. Bucket counts for missed truths that had a refined near-truth
//     candidate (the only population for which symbol-quality is
//     measurable; upstream-died truths have no grid to evaluate).
//  2. Per-missed-truth detail list, sorted by bucket then capture.
//  3. Bucket counts for accepted extras (all extras have grids by
//     definition — they decoded).
//  4. Per-accepted-extra detail list, sorted by bucket then capture.
//
// Same audio/grid GeoContrast threshold (symbolQualityThreshold) is
// applied on both axes — keeps the four labels symmetric. Per the
// operator's framing the audio threshold matches the strict Stage2
// default at 0.70.
func printSymbolQuality(fates []truthFate, extras []extraEvidence) {
	fmt.Println()
	fmt.Println("=== SYMBOL-QUALITY CLASSIFICATION (audio vs grid GeoContrast) ===")
	fmt.Printf("  threshold (both axes): GeoContrast ≥ %.2f → \"good\", otherwise \"weak\"\n", symbolQualityThreshold)
	fmt.Println("  buckets:")
	fmt.Println("    audio_good_grid_good  — BP/OSD/LLR problem (missed) or convincing-shape extra (accepted)")
	fmt.Println("    audio_good_grid_bad   — channelizer/refine/extract degraded the grid")
	fmt.Println("    audio_weak_grid_good  — interesting (metric mismatch or lucky refinement)")
	fmt.Println("    audio_weak_grid_weak  — finder admitted a marginal/noisy candidate")

	// Missed truths with a refined near-truth candidate.
	missedByBucket := map[string][]truthFate{}
	noRefined := 0
	missedTotal := 0
	for _, f := range fates {
		if f.stage == stageMatched {
			continue
		}
		missedTotal++
		if !f.hasRefined {
			noRefined++
			continue
		}
		b := qualityBucket(f.audioGeo, f.gridGeo)
		missedByBucket[b] = append(missedByBucket[b], f)
	}
	fmt.Println()
	fmt.Printf("--- MISSED TRUTHS (n=%d total; n=%d with a refined near-truth candidate, n=%d upstream-died) ---\n",
		missedTotal, missedTotal-noRefined, noRefined)
	fmt.Println("  bucket                count")
	for _, b := range qualityBucketOrder {
		fmt.Printf("    %-22s %3d\n", b, len(missedByBucket[b]))
	}
	if noRefined > 0 {
		fmt.Printf("    %-22s %3d  (no refined near-truth candidate; symbol-quality n/a)\n",
			"upstream-died", noRefined)
	}
	for _, b := range qualityBucketOrder {
		entries := missedByBucket[b]
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].capture != entries[j].capture {
				return entries[i].capture < entries[j].capture
			}
			return entries[i].freqHz < entries[j].freqHz
		})
		fmt.Println()
		fmt.Printf("  ▸ %s (n=%d)\n", b, len(entries))
		for _, e := range entries {
			fmt.Printf("    %-16s  %7.1f Hz  dt=%+6.3f  audio=%6.2f  grid=%6.2f  stage=%s  %q\n",
				e.capture, e.freqHz, e.dtSec, e.audioGeo, e.gridGeo, e.stage, e.text)
		}
	}

	// Accepted extras (false-positive decodes).
	extrasByBucket := map[string][]extraEvidence{}
	for _, e := range extras {
		b := qualityBucket(e.audioGeo, e.gridGeo)
		extrasByBucket[b] = append(extrasByBucket[b], e)
	}
	fmt.Println()
	fmt.Printf("--- ACCEPTED EXTRAS (n=%d; false-positive decodes that survived the gate) ---\n", len(extras))
	fmt.Println("  bucket                count")
	for _, b := range qualityBucketOrder {
		fmt.Printf("    %-22s %3d\n", b, len(extrasByBucket[b]))
	}
	for _, b := range qualityBucketOrder {
		entries := extrasByBucket[b]
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].capture != entries[j].capture {
				return entries[i].capture < entries[j].capture
			}
			return entries[i].freqHz < entries[j].freqHz
		})
		fmt.Println()
		fmt.Printf("  ▸ %s (n=%d)\n", b, len(entries))
		for _, e := range entries {
			fmt.Printf("    %-16s  %7.1f Hz  dt=%+6.3f  audio=%6.2f  grid=%6.2f  %q\n",
				e.capture, e.freqHz, e.dtSec, e.audioGeo, e.gridGeo, e.text)
		}
	}
}

// printSurvival reports total candidate counts at each pipeline
// stage summed across the corpus. Lets a reader see at a glance how
// far the Stage2 verifier (when active) is trimming the post-NMS
// candidate set vs the baseline Off / Observe behaviour.
func printSurvival(raw, nms, capped, stage2, refined int, mode sandbox.Stage2Mode) {
	fmt.Println()
	fmt.Println("=== CANDIDATE SURVIVAL (totals across corpus) ===")
	fmt.Printf("  raw:        %6d\n", raw)
	fmt.Printf("  post-NMS:   %6d\n", nms)
	fmt.Printf("  post-cap:   %6d\n", capped)
	fmt.Printf("  post-Stage2:%6d  (Stage2 mode: %s)\n", stage2, modeName(mode))
	fmt.Printf("  refined:    %6d\n", refined)
}

// modeName renders a Stage2Mode value as a short human-readable
// label for survival-report headers.
func modeName(m sandbox.Stage2Mode) string {
	switch m {
	case sandbox.Stage2Observe:
		return "observe"
	case sandbox.Stage2Filter:
		return "filter"
	case sandbox.Stage2Rerank:
		return "rerank"
	default:
		return "off"
	}
}

// metricName renders a Stage2Metric value as a short human-readable
// label for report headers.
func metricName(m sandbox.Stage2Metric) string {
	switch m {
	case sandbox.Stage2MetricGeo:
		return "GeoContrast"
	case sandbox.Stage2MetricWins:
		return "WinsTotal"
	default:
		return "MinBlockContrast"
	}
}

// parseStage2Mode mirrors sandbox-asym-ab's parser so the funnel and
// the decoder harness accept the same flag vocabulary.
func parseStage2Mode(s string) (sandbox.Stage2Mode, bool) {
	switch s {
	case "off", "":
		return sandbox.Stage2Off, true
	case "observe":
		return sandbox.Stage2Observe, true
	case "filter":
		return sandbox.Stage2Filter, true
	case "rerank":
		return sandbox.Stage2Rerank, true
	}
	return sandbox.Stage2Off, false
}

// parseStage2Metric mirrors sandbox-asym-ab's parser.
func parseStage2Metric(s string) (sandbox.Stage2Metric, bool) {
	switch s {
	case "minblock", "":
		return sandbox.Stage2MetricMinBlock, true
	case "geo":
		return sandbox.Stage2MetricGeo, true
	case "wins":
		return sandbox.Stage2MetricWins, true
	}
	return sandbox.Stage2MetricMinBlock, false
}

// signalAnyNear is the truth-list flavour of anyNear: returns true
// if any truth.Signal in the slice lies within (freqTol, dtTol) of
// the given (freqHz, dtSec) coordinate.
func signalAnyNear(signals []truth.Signal, freqHz, dtSec, freqTol, dtTol float64) bool {
	for _, s := range signals {
		if near(freqHz, dtSec, s.FreqHz, s.DTSec, freqTol, dtTol) {
			return true
		}
	}
	return false
}

// metricSpec describes one discriminator metric to audit. The
// audit emits the same stats + histogram + overlap analysis for each
// metric so the four numbers can be compared side by side on the
// same post-NMS population.
type metricSpec struct {
	name     string
	colLabel string
	binWidth float64
	maxBin   float64
	extract  func(candScore) float64
}

// printScoreAudit answers the question "which front-end discriminator
// (Sync, WinsTotal, GeoContrast, MinBlockContrast) actually separates
// signal-bearing candidates from aliases?" Two populations:
//
//   - near-truth: post-NMS candidates within (freqTol, dtTol) of any
//     truth signal in the same WAV's manifest
//   - alias: everything else (post-NMS candidates with no truth in
//     proximity)
//
// The Sync section is the sandbox finder's matched-filter ranking
// (the audit's Session-105 baseline). The Stage2 sections come from
// the candidates package's per-anchor Costas verifier — same audio,
// same coordinates, different discrimination algebra. If a Stage2
// metric cleanly separates the populations where Sync overlaps, that
// metric is a candidate for porting into the sandbox finder.
func printScoreAudit(scores []candScore) {
	if len(scores) == 0 {
		return
	}

	metrics := []metricSpec{
		{name: "Candidate.Sync (sandbox)", colLabel: "sync range", binWidth: 2.0, maxBin: 50.0, extract: func(s candScore) float64 { return s.sync }},
		{name: "WinsTotal (candidates Stage2, 0..21)", colLabel: "wins range", binWidth: 1.0, maxBin: 21.0, extract: func(s candScore) float64 { return float64(s.winsTotal) }},
		{name: "GeoContrast (candidates Stage2)", colLabel: "geo range", binWidth: 0.5, maxBin: 8.0, extract: func(s candScore) float64 { return s.geoContrast }},
		{name: "MinBlockContrast (candidates Stage2)", colLabel: "minblock range", binWidth: 0.5, maxBin: 8.0, extract: func(s candScore) float64 { return s.minBlockContrast }},
	}

	fmt.Println()
	fmt.Println("=== FRONT-END SCORE AUDIT (post-NMS, four metrics side-by-side) ===")
	fmt.Printf("  scope:     all post-NMS candidates across the corpus (n=%d)\n", len(scores))
	fmt.Printf("  labelling: near-truth = within (ftol, dttol) of any manifest signal\n")
	for _, m := range metrics {
		printOneMetricAudit(scores, m)
	}

	printDiscriminationSummary(scores, metrics)
}

func printOneMetricAudit(scores []candScore, m metricSpec) {
	var nearVals, aliasVals []float64
	for _, s := range scores {
		v := m.extract(s)
		if s.nearTruth {
			nearVals = append(nearVals, v)
		} else {
			aliasVals = append(aliasVals, v)
		}
	}

	fmt.Println()
	fmt.Printf("--- %s ---\n", m.name)
	fmt.Printf("  population        count    min     p25     p50     p75     max\n")
	printStats("near-truth", nearVals)
	printStats("alias", aliasVals)
	fmt.Println()

	histo := buildHisto(nearVals, aliasVals, m.binWidth, m.maxBin)
	fmt.Printf("  histogram (bin width = %.1f):\n", m.binWidth)
	fmt.Printf("    %-15s  near-truth   alias\n", m.colLabel)
	for _, row := range histo {
		fmt.Printf("    %-15s  %10d   %5d\n", row.label, row.near, row.alias)
	}

	if len(nearVals) > 0 && len(aliasVals) > 0 {
		sortedNear := append([]float64(nil), nearVals...)
		sort.Float64s(sortedNear)
		nearP25 := sortedNear[len(sortedNear)/4]
		nearP50 := sortedNear[len(sortedNear)/2]
		aboveP25 := 0
		aboveP50 := 0
		for _, v := range aliasVals {
			if v >= nearP25 {
				aboveP25++
			}
			if v >= nearP50 {
				aboveP50++
			}
		}
		fmt.Println("  overlap summary:")
		fmt.Printf("    aliases scoring ≥ near-truth p25 (%.2f): %d / %d (%.0f%%)\n",
			nearP25, aboveP25, len(aliasVals), 100*float64(aboveP25)/float64(len(aliasVals)))
		fmt.Printf("    aliases scoring ≥ near-truth p50 (%.2f): %d / %d (%.0f%%)\n",
			nearP50, aboveP50, len(aliasVals), 100*float64(aboveP50)/float64(len(aliasVals)))
	}
}

// printDiscriminationSummary tallies one number per metric: the
// fraction of aliases that score above near-truth's median. Smaller
// is better — it means alias mass sits below the signal median, so
// thresholding on that metric trims aliases without killing real
// signals. Side-by-side numbers make the four metrics directly
// comparable; the smallest wins.
func printDiscriminationSummary(scores []candScore, metrics []metricSpec) {
	fmt.Println()
	fmt.Println("=== DISCRIMINATION SUMMARY (smaller = better separator) ===")
	fmt.Println("  metric                                  alias≥near-p50  alias≥near-p25")
	for _, m := range metrics {
		var nearVals, aliasVals []float64
		for _, s := range scores {
			v := m.extract(s)
			if s.nearTruth {
				nearVals = append(nearVals, v)
			} else {
				aliasVals = append(aliasVals, v)
			}
		}
		if len(nearVals) == 0 || len(aliasVals) == 0 {
			continue
		}
		sortedNear := append([]float64(nil), nearVals...)
		sort.Float64s(sortedNear)
		nearP25 := sortedNear[len(sortedNear)/4]
		nearP50 := sortedNear[len(sortedNear)/2]
		aboveP25 := 0
		aboveP50 := 0
		for _, v := range aliasVals {
			if v >= nearP25 {
				aboveP25++
			}
			if v >= nearP50 {
				aboveP50++
			}
		}
		fmt.Printf("  %-38s  %5.1f%% (%4d)   %5.1f%% (%4d)\n",
			m.name,
			100*float64(aboveP50)/float64(len(aliasVals)), aboveP50,
			100*float64(aboveP25)/float64(len(aliasVals)), aboveP25)
	}
}

func printStats(label string, vals []float64) {
	if len(vals) == 0 {
		fmt.Printf("    %-15s  %5d   (no values)\n", label, 0)
		return
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	min := sorted[0]
	max := sorted[len(sorted)-1]
	p25 := sorted[len(sorted)/4]
	p50 := sorted[len(sorted)/2]
	p75 := sorted[3*len(sorted)/4]
	fmt.Printf("    %-15s  %5d   %5.1f   %5.1f   %5.1f   %5.1f   %5.1f\n",
		label, len(vals), min, p25, p50, p75, max)
}

type histoRow struct {
	label string
	near  int
	alias int
}

// buildHisto bins near-truth and alias scores into the same set of
// bins for side-by-side comparison.
func buildHisto(near, alias []float64, binWidth, maxBin float64) []histoRow {
	if binWidth <= 0 {
		return nil
	}
	nBins := int(maxBin/binWidth) + 1 // last bin is the overflow tail
	rows := make([]histoRow, nBins)
	for i := 0; i < nBins-1; i++ {
		lo := float64(i) * binWidth
		hi := lo + binWidth
		rows[i].label = fmt.Sprintf("%4.1f-%4.1f", lo, hi)
	}
	rows[nBins-1].label = fmt.Sprintf("    ≥%4.1f", maxBin)

	for _, v := range near {
		idx := int(v / binWidth)
		if idx >= nBins-1 {
			idx = nBins - 1
		}
		if idx < 0 {
			idx = 0
		}
		rows[idx].near++
	}
	for _, v := range alias {
		idx := int(v / binWidth)
		if idx >= nBins-1 {
			idx = nBins - 1
		}
		if idx < 0 {
			idx = 0
		}
		rows[idx].alias++
	}
	return rows
}

// refineAndExtract runs RefineCandidate over the supplied candidate
// slice and pairs each survivor with its SymbolGrid (via
// ExtractSymbols). The two return slices are index-aligned: grids[i]
// is the grid for refined[i] (or nil if grid extraction failed for
// that refined candidate — symbol windows outside the baseband bound
// are the typical cause; the candidate stays in refined regardless
// so the funnel's stage counts don't shift).
//
// Sharing one Channelizer across refine + extract for an entire WAV
// is the cheap path: Prepare runs once, the cached 192k FFT is
// reused for every Refine and every Extract.
func refineAndExtract(samples []float32, cands []sandbox.Candidate, refineOpts sandbox.RefineOptions) ([]sandbox.Candidate, []*sandbox.SymbolGrid) {
	ch, err := sandbox.NewChannelizer()
	if err != nil {
		return nil, nil
	}
	defer ch.Close()
	ch.SetAsymmetricFT8Slice(false)
	if err := ch.Prepare(samples); err != nil {
		return nil, nil
	}
	refined := make([]sandbox.Candidate, 0, len(cands))
	grids := make([]*sandbox.SymbolGrid, 0, len(cands))
	for _, c := range cands {
		rc, err := sandbox.RefineCandidate(ch, c, refineOpts)
		if err != nil {
			continue
		}
		refined = append(refined, rc)
		if g, err := sandbox.ExtractSymbols(ch, rc); err == nil {
			grids = append(grids, g)
		} else {
			grids = append(grids, nil)
		}
	}
	return refined, grids
}

// closestRefinedIndex finds the refined candidate within (freqTol,
// dtTol) of (truthFreq, truthDt) with the smallest combined
// distance, and returns its slice index. Returns -1 if none match.
//
// "Closest" combines freq + dt by treating them on a comparable
// scale (1 s of dt drift = 8 Hz of freq drift, matching the
// nmsAttribute helper). The constants only matter for argmin, not
// for thresholding.
func closestRefinedIndex(refined []sandbox.Candidate, truthFreq, truthDt, freqTol, dtTol float64) int {
	best := -1
	bestSq := math.Inf(1)
	for i := range refined {
		c := &refined[i]
		if !near(c.FreqHz, c.DtSec, truthFreq, truthDt, freqTol, dtTol) {
			continue
		}
		dF := c.FreqHz - truthFreq
		dD := c.DtSec - truthDt
		dist := dF*dF + (dD*8)*(dD*8)
		if dist < bestSq {
			bestSq = dist
			best = i
		}
	}
	return best
}

// isAcceptedExtra returns true when an accepted decode does NOT
// match any truth in the manifest — i.e., a false-positive that
// passed BP/OSD + CRC + gate. Same definition the harness uses for
// its "extras" count: no near-truth coordinate AND matching text.
func isAcceptedExtra(d sandbox.DecodeRecord, signals []truth.Signal, freqTol, dtTol float64) bool {
	dText := truth.NormalizeText(d.Text)
	for _, s := range signals {
		if near(d.FreqHz, d.DtSec, s.FreqHz, s.DTSec, freqTol, dtTol) &&
			dText == truth.NormalizeText(s.Text) {
			return false
		}
	}
	return true
}

// classify walks the pipeline outcome inputs from downstream (matched)
// to upstream (finder-miss). Returns the first stage label that
// applies; this is the LAST stage the truth survived to before
// failure.
func classify(
	t truth.Signal,
	raw, postNMS, postCap, postStage2, refined []sandbox.Candidate,
	accepted []sandbox.DecodeRecord,
	shadow []sandbox.ShadowReject,
	freqTol, dtTol float64,
) stage {
	truthText := truth.NormalizeText(t.Text)

	// Stage 7: matched against an accepted decode.
	for _, d := range accepted {
		if near(d.FreqHz, d.DtSec, t.FreqHz, t.DTSec, freqTol, dtTol) &&
			truth.NormalizeText(d.Text) == truthText {
			return stageMatched
		}
	}
	// Stage 6: present in shadow rejects with matching text. This is
	// a "gate killed a real decode" outcome — distinct from "decoder
	// produced a different decode at the same freq" (which falls
	// through to decoder-bound below).
	for _, r := range shadow {
		if near(r.FreqHz, r.DtSec, t.FreqHz, t.DTSec, freqTol, dtTol) &&
			truth.NormalizeText(r.Text) == truthText {
			return stageGateBound
		}
	}
	// Stage 5: decoder-bound. Refined candidate near truth exists,
	// but BP/OSD produced no truth-matching codeword in either the
	// accepted or shadow-rejected populations.
	if anyNear(refined, t.FreqHz, t.DTSec, freqTol, dtTol) {
		return stageDecoderBound
	}
	// Stage 4b: refine-bound. Stage2 survivor was near truth but
	// refinement drifted outside tolerance.
	if anyNear(postStage2, t.FreqHz, t.DTSec, freqTol, dtTol) {
		return stageRefineBound
	}
	// Stage 4a: stage2-bound. Cap-survivor was near truth but the
	// Stage2 verifier filter dropped it (or, in rerank mode with a
	// binding cap, sorted it below the cap line). Only fires when
	// Stage2 is active; in Off/Observe modes postStage2 == postCap
	// so this branch is never reached.
	if anyNear(postCap, t.FreqHz, t.DTSec, freqTol, dtTol) {
		return stageStage2Bound
	}
	// Stage 3: cap-bound. Survived NMS but got dropped by the
	// MaxResults cap before reaching refinement.
	if anyNear(postNMS, t.FreqHz, t.DTSec, freqTol, dtTol) {
		return stageCapBound
	}
	// Stage 2: NMS-bound. Raw candidate near truth existed but NMS
	// removed it (suppressed by a higher-scoring neighbour).
	if anyNear(raw, t.FreqHz, t.DTSec, freqTol, dtTol) {
		return stageNMSBound
	}
	// Stage 1: finder didn't produce any candidate near truth.
	return stageFinderMiss
}

// nmsAttribute walks the suppression mechanics for one NMS-bound
// truth: find the closest raw candidate to truth (the would-be kept),
// then identify the kept post-NMS candidate that suppressed it. The
// K=2-medium NMS suppresses a raw candidate when either:
//
//   - tight-box: some kept candidate is within (6.25 Hz, 0.08 s)
//     of it (near-duplicate dedup)
//   - K-cap: K or more kept candidates already exist within 12.5 Hz
//     of it on the freq axis (group cap)
//
// We can't replay SuppressOverlapsK2's exact decision (input order
// matters; we'd need to re-run it) but we can identify which kept
// neighbour is most likely responsible by checking proximity.
func nmsAttribute(t truth.Signal, raw, postNMS []sandbox.Candidate, freqTol, dtTol float64) string {
	// Closest raw candidate to truth — this is the one that would
	// have been kept if NMS hadn't suppressed it.
	var rawWinner *sandbox.Candidate
	rawBestSq := math.Inf(1)
	for i := range raw {
		c := &raw[i]
		if !near(c.FreqHz, c.DtSec, t.FreqHz, t.DTSec, freqTol, dtTol) {
			continue
		}
		dF := c.FreqHz - t.FreqHz
		dD := c.DtSec - t.DTSec
		// Normalised distance: freq in Hz, dt scaled to comparable Hz
		// (8 Hz per second of dt drift) for ranking; only used for
		// argmin so the constants don't affect the labelling.
		distSq := dF*dF + (dD*8)*(dD*8)
		if distSq < rawBestSq {
			rawBestSq = distSq
			rawWinner = c
		}
	}
	if rawWinner == nil {
		return "(no raw match — funnel misclassification)"
	}

	// Find the kept post-NMS candidate that's closest to the raw
	// winner — likely the suppressor. Check tight-box first (most
	// common); fall back to K-cap detection.
	const tightFreqHz = 6.25
	const tightDtSec = 0.08
	const groupFreqHz = 12.5

	// Tight-box check: any kept candidate within (6.25 Hz, 0.08 s)?
	for i := range postNMS {
		k := &postNMS[i]
		dF := math.Abs(k.FreqHz - rawWinner.FreqHz)
		dD := math.Abs(k.DtSec - rawWinner.DtSec)
		if dF <= tightFreqHz && dD <= tightDtSec {
			return fmt.Sprintf("tight-box (raw@%.1fHz,%+.3fs → kept@%.1fHz,%+.3fs, Δf=%.2f Δdt=%+.3f)",
				rawWinner.FreqHz, rawWinner.DtSec, k.FreqHz, k.DtSec, k.FreqHz-rawWinner.FreqHz, k.DtSec-rawWinner.DtSec)
		}
	}
	// K-cap check: count kept candidates within groupFreqHz on freq.
	groupCount := 0
	var groupNeighbours []sandbox.Candidate
	for _, k := range postNMS {
		if math.Abs(k.FreqHz-rawWinner.FreqHz) <= groupFreqHz {
			groupCount++
			groupNeighbours = append(groupNeighbours, k)
		}
	}
	if groupCount >= 2 {
		// Format the kept group neighbours' freqs for visibility.
		fs := make([]string, 0, len(groupNeighbours))
		for _, n := range groupNeighbours {
			fs = append(fs, fmt.Sprintf("%.1f", n.FreqHz))
		}
		return fmt.Sprintf("K-cap (raw@%.1fHz,%+.3fs in freq group with %d kept: [%s])",
			rawWinner.FreqHz, rawWinner.DtSec, groupCount, joinStrings(fs, ", "))
	}
	return fmt.Sprintf("(unclassified — raw@%.1fHz,%+.3fs has no nearby kept)", rawWinner.FreqHz, rawWinner.DtSec)
}

// joinStrings is a stdlib-free wrapper to keep this tool's import
// list lean (matches the rest of the funnel's style).
func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

func anyNear(cands []sandbox.Candidate, fHz, dtSec, freqTol, dtTol float64) bool {
	for _, c := range cands {
		if near(c.FreqHz, c.DtSec, fHz, dtSec, freqTol, dtTol) {
			return true
		}
	}
	return false
}

func near(aF, aD, bF, bD, freqTol, dtTol float64) bool {
	return math.Abs(aF-bF) <= freqTol && math.Abs(aD-bD) <= dtTol
}

func printReport(fates []truthFate, magnitudeMode bool, stage2Mode sandbox.Stage2Mode, stage2Metric sandbox.Stage2Metric, stage2Threshold float64) {
	domain := "magnitude (QEX § 6 spec-aligned)"
	if !magnitudeMode {
		domain = "POWER (legacy/off-spec)"
	}
	fmt.Println("=== PER-TRUTH MISS-STAGE FUNNEL ===")
	fmt.Printf("  LLR domain:     %s\n", domain)
	fmt.Printf("  channelizer:    symmetric only (strict-aligned)\n")
	if stage2Mode == sandbox.Stage2Off {
		fmt.Printf("  Stage2 verifier: off\n")
	} else {
		fmt.Printf("  Stage2 verifier: mode=%s metric=%s threshold=%.3f\n",
			modeName(stage2Mode), metricName(stage2Metric), stage2Threshold)
	}
	fmt.Printf("  total truths:   %d\n\n", len(fates))

	counts := map[stage]int{}
	byStage := map[stage][]truthFate{}
	for _, f := range fates {
		counts[f.stage]++
		byStage[f.stage] = append(byStage[f.stage], f)
	}

	fmt.Println("  stage              count")
	for _, s := range reportOrder {
		fmt.Printf("    %-16s  %3d\n", string(s), counts[s])
	}
	fmt.Println()

	// Per-stage detail for everything that isn't matched. Sorted by
	// (capture, freq) so the same fixture's misses cluster together.
	for _, s := range reportOrder {
		if s == stageMatched {
			continue
		}
		entries := byStage[s]
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].capture != entries[j].capture {
				return entries[i].capture < entries[j].capture
			}
			return entries[i].freqHz < entries[j].freqHz
		})
		fmt.Printf("--- %s (n=%d) ---\n", s, len(entries))
		for _, e := range entries {
			fmt.Printf("  %-16s  %7.1f Hz  dt=%+6.3f  %q\n",
				e.capture, e.freqHz, e.dtSec, e.text)
			if e.nmsNote != "" {
				fmt.Printf("                       %s\n", e.nmsNote)
			}
		}
		fmt.Println()
	}

	// Sanity: total counts add up.
	var sum int
	for _, c := range counts {
		sum += c
	}
	if sum != len(fates) {
		fmt.Fprintf(os.Stderr, "warning: stage counts %d != total truths %d\n", sum, len(fates))
	}
}
