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

	"github.com/ColonelBlimp/station-manager/internal/audio"
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
}

func main() {
	dir := flag.String("dir", "captures", "directory of .wav files with paired .truth.json manifests (sequential jt9 oracle order, alphabetical fixture order)")
	freqTol := flag.Float64("ftol", 5.0, "freq tolerance for stage matching (Hz). Matches sandbox-asym-ab default.")
	dtTol := flag.Float64("dttol", 0.5, "dt tolerance for stage matching (s). Matches sandbox-asym-ab default.")
	magnitudeLLR := flag.Bool("magnitude-llr", true, "QEX § 6 spec-aligned magnitude-domain demap. Default true matches the strict-mode 113/23 baseline.")
	nmsK := flag.Int("nms-k", 0, "override SearchOptions.K2MaxPerGroup (max kept per freq group in NMS). 0 = package default (2).")
	maxResults := flag.Int("max-results", 0, "override SearchOptions.MaxResults (post-NMS cap). 0 = package default (200). Raise to test whether raising NMS K is wasted by the cap biting downstream.")
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
	if *nmsK > 0 {
		opts.Search.K2MaxPerGroup = *nmsK
	}
	if *maxResults > 0 {
		opts.Search.MaxResults = *maxResults
	}

	refineOpts := sandbox.DefaultRefineOptions()
	searchOpts := opts.Search

	// One hash table threaded across the corpus in alphabetical order
	// to match strict-mode semantics. Sequential hash carry can
	// affect Type 4 unpacking and any cross-slot hashed reference
	// (DG6JW/T → <DG6JW/T> in 20m_slot3).
	ht := sandbox.NewCallsignHashTable()

	var fates []truthFate
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
		refined := refineCandidates(data.Samples, postCap, refineOpts)

		// Stages 5-7: rely on MultiPassDecodeFull for accepted +
		// gate-rejected. Anything not surfaced by either reached
		// the decoder but BP/OSD failed.
		result := sandbox.MultiPassDecodeFull(data.Samples, opts, ht)

		for _, s := range m.Signals {
			tf := truthFate{
				capture: filepath.Base(w),
				freqHz:  s.FreqHz,
				dtSec:   s.DTSec,
				text:    s.Text,
				stage: classify(s, raw, postNMS, postCap, refined,
					result.Decodes, result.ShadowRejects,
					*freqTol, *dtTol),
			}
			if tf.stage == stageNMSBound {
				tf.nmsNote = nmsAttribute(s, raw, postNMS, *freqTol, *dtTol)
			}
			fates = append(fates, tf)
		}
	}

	printReport(fates, *magnitudeLLR)
}

// refineCandidates runs RefineCandidate over a slice of post-cap
// candidates and returns the successfully refined set. Skipping the
// per-candidate channelizer.Extract failure path is intentional: the
// funnel only cares whether refinement *produces* a candidate, not
// downstream extraction failures.
func refineCandidates(samples []float32, cands []sandbox.Candidate, refineOpts sandbox.RefineOptions) []sandbox.Candidate {
	ch, err := sandbox.NewChannelizer()
	if err != nil {
		return nil
	}
	defer ch.Close()
	ch.SetAsymmetricFT8Slice(false)
	if err := ch.Prepare(samples); err != nil {
		return nil
	}
	refined := make([]sandbox.Candidate, 0, len(cands))
	for _, c := range cands {
		rc, err := sandbox.RefineCandidate(ch, c, refineOpts)
		if err != nil {
			continue
		}
		refined = append(refined, rc)
	}
	return refined
}

// classify walks the pipeline outcome inputs from downstream (matched)
// to upstream (finder-miss). Returns the first stage label that
// applies; this is the LAST stage the truth survived to before
// failure.
func classify(
	t truth.Signal,
	raw, postNMS, postCap, refined []sandbox.Candidate,
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
	// Stage 4: refine-bound. Cap-survivor was near truth but
	// refinement drifted outside tolerance.
	if anyNear(postCap, t.FreqHz, t.DTSec, freqTol, dtTol) {
		return stageRefineBound
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

func printReport(fates []truthFate, magnitudeMode bool) {
	domain := "magnitude (QEX § 6 spec-aligned)"
	if !magnitudeMode {
		domain = "POWER (legacy/off-spec)"
	}
	fmt.Println("=== PER-TRUTH MISS-STAGE FUNNEL ===")
	fmt.Printf("  LLR domain:     %s\n", domain)
	fmt.Printf("  channelizer:    symmetric only (strict-aligned)\n")
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
