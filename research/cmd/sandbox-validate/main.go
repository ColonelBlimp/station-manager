// sandbox-validate exercises the research/sandbox/Channelizer on a
// labelled FT8 WAV and reports whether each known signal lights up
// in the baseband as expected.
//
// For every signal in the truth manifest, it:
//
//  1. Calls Channelizer.Extract at centerHz = signal.FreqHz with the
//     supplied bandwidth (default 200 Hz).
//  2. Forward-FFTs the resulting 3,200-sample complex baseband.
//  3. Reports the peak FFT bin, the in-band power (positive
//     frequencies up to FT8's 50 Hz tone span), and the
//     out-of-band power (the rest of the spectrum).
//
// As a control, it also extracts at a frequency known to be empty
// (default 2,500 Hz — well above the highest 10cq signal at 1,400 Hz)
// and prints the same numbers; expectation is that signal-band power
// ratios massively beat the empty-band ratio.
//
// Usage:
//
//	go run ./research/cmd/sandbox-validate -wav research/10cq_clean.wav
//	go run ./research/cmd/sandbox-validate -wav research/10cq_clean.wav -bw 200
//	go run ./research/cmd/sandbox-validate -wav research/10cq_clean.wav -peaks
//	go run ./research/cmd/sandbox-validate -wav research/10cq_clean.wav -scan
//
// -peaks prints the top 10 baseband FFT bins per signal for visual
// inspection of the 8-FSK tone pattern (tones live at multiples of
// 6.25 Hz = 100 baseband-FFT bins).
//
// -scan instead exercises the full candidate-finder pipeline:
// spectrogram → coarse FindCandidates → RefineCandidate per coarse
// hit. Scores detections against the truth manifest and shows the
// coarse-to-refined drift on each axis.
//
// Import rules: research code may use internal/audio + research/*
// (and stdlib) but MUST NOT import internal/ft8/* — sandbox work is
// independent of SM's FT8 stack.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/cmplx"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/sandbox"
	"github.com/ColonelBlimp/station-manager/research/sandbox/pfft"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

const (
	expectedSampleRate = 12000

	// ft8ToneSpanHz is the bandwidth occupied by the 8 FT8 tones
	// (8 tones × 6.25 Hz spacing = 50 Hz). Anything from the channel
	// centre to +50 Hz is "in-band" for an FT8 signal that uses
	// WSJT-X's tone-0 frequency convention.
	ft8ToneSpanHz = 50.0

	// ft8ToneSpacingHz is 6.25 Hz.
	ft8ToneSpacingHz = 6.25
)

func main() {
	wavPath := flag.String("wav", "research/10cq_clean.wav", "path to the WAV to validate")
	bandwidthHz := flag.Float64("bw", 200.0, "channelizer bandwidth in Hz")
	emptyHz := flag.Float64("empty", 2500.0, "control frequency where no signal is expected")
	showPeaks := flag.Bool("peaks", false, "print top-10 baseband FFT bins per signal")
	scan := flag.Bool("scan", false, "run FindCandidates + RefineCandidate instead of per-truth probing")
	threshold := flag.Float64("thresh", 3.0, "FindCandidates score threshold (scan mode)")
	dfsweep := flag.Float64("dfsweep", 0, "override RefineOptions.DFSweepRangeHz (Hz); 0 = default")
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

	fmt.Printf("=== %s ===\n", *wavPath)
	fmt.Printf("  %d samples @ %d Hz (%.2f s); %d truth signals; bandwidth=%.1f Hz\n\n",
		len(data.Samples), data.SampleRate,
		float64(len(data.Samples))/float64(data.SampleRate),
		len(manifest.Signals), *bandwidthHz)

	ch, err := sandbox.NewChannelizer()
	if err != nil {
		log.Fatalf("new channelizer: %v", err)
	}
	defer ch.Close()

	if err := ch.Prepare(data.Samples); err != nil {
		log.Fatalf("prepare: %v", err)
	}

	if *scan {
		runScan(ch, data.Samples, manifest, *threshold, *dfsweep)
		return
	}

	// One ComplexPlan for the per-signal baseband-FFT, sized to the
	// channelizer's output length for the requested bandwidth.
	// sliceN = round(bandwidth/binHz), rounded up to even.
	// For 200 Hz at 0.0625 Hz/bin that's 3,200.
	sliceN := bandwidthToSliceN(*bandwidthHz)
	bbPlan, err := pfft.NewComplexPlan(sliceN)
	if err != nil {
		log.Fatalf("baseband plan: %v", err)
	}
	defer bbPlan.Close()
	binHzBB := *bandwidthHz / float64(sliceN)

	// In-band span: [0, ft8ToneSpanHz] in baseband frequency. With the
	// WSJT-X tone-0 convention (truth.FreqHz is the lowest tone),
	// channelizing at exactly truth.FreqHz puts all 8 tones in the
	// positive half [0, +43.75 Hz].
	inBandBins := int(ft8ToneSpanHz / binHzBB) // = 800 for 200 Hz bandwidth

	header := "  signal                              center    peak  peakHz  inPwr      outPwr     ratio    verdict"
	fmt.Println(header)
	for _, sig := range manifest.Signals {
		printRow(ch, bbPlan, sig.Text, sig.FreqHz, *bandwidthHz, binHzBB, inBandBins, *showPeaks)
	}
	fmt.Println()
	fmt.Println("  -- control (no signal expected) --")
	printRow(ch, bbPlan, "(empty)", *emptyHz, *bandwidthHz, binHzBB, inBandBins, *showPeaks)
}

// bandwidthToSliceN mirrors Channelizer.Extract's slice-length rule:
// sliceN = round(bandwidthHz / channelizerBinHz), rounded up to even.
// channelizerBinHz = 0.0625 Hz (12 kHz / 192 kHz FFT).
func bandwidthToSliceN(bandwidthHz float64) int {
	const binHz = 12000.0 / 192000.0
	n := int(bandwidthHz/binHz + 0.5)
	if n%2 != 0 {
		n++
	}
	return n
}

func printRow(
	ch *sandbox.Channelizer,
	bbPlan *pfft.ComplexPlan,
	label string,
	centerHz, bandwidthHz, binHzBB float64,
	inBandBins int,
	showPeaks bool,
) {
	bb, err := ch.Extract(centerHz, bandwidthHz)
	if err != nil {
		fmt.Printf("  %-36s %7.2f  Extract failed: %v\n", label, centerHz, err)
		return
	}
	if len(bb) != bbPlan.Length() {
		fmt.Printf("  %-36s %7.2f  unexpected baseband length %d vs %d\n",
			label, centerHz, len(bb), bbPlan.Length())
		return
	}

	work := make([]complex128, len(bb))
	copy(work, bb)
	if err := bbPlan.Forward(work); err != nil {
		fmt.Printf("  %-36s %7.2f  baseband FFT failed: %v\n", label, centerHz, err)
		return
	}

	// In-band: positive baseband bins [0, inBandBins). Out-of-band:
	// the remaining [inBandBins, len) — these are higher positive
	// freqs + all negative freqs, where no signal energy should sit
	// when channelized at the correct centre.
	var inPwr, outPwr float64
	peakBin := 0
	peakMag := 0.0
	for i, v := range work {
		m := cmplx.Abs(v)
		p := m * m
		if i < inBandBins {
			inPwr += p
			if m > peakMag {
				peakMag = m
				peakBin = i
			}
		} else {
			outPwr += p
		}
	}
	peakHz := float64(peakBin) * binHzBB

	// "verdict": signal-band-ratio in dB. Positive = signal-band
	// dominates; near-zero / negative = no excess signal energy.
	// Truth signals are expected to be 20+ dB above the empty
	// control; the empty control should sit near 0 dB or negative.
	ratioDB := 10.0 * log10ish(inPwr/outPwr)

	verdict := "ok"
	if ratioDB < 10 {
		verdict = "WEAK"
	}
	if ratioDB < 3 {
		verdict = "FAIL"
	}

	fmt.Printf("  %-36s %7.2f  %5d  %6.2f  %.2e  %.2e  %+6.1f  %s\n",
		label, centerHz, peakBin, peakHz, inPwr, outPwr, ratioDB, verdict)

	if showPeaks {
		printTopBins(work, binHzBB, 10)
	}
}

// log10ish is math.Log10 with -inf/+inf clamped to ±200 so the table
// stays readable when one side is essentially zero.
func log10ish(x float64) float64 {
	if x <= 0 {
		return -200
	}
	return math.Log10(x)
}

type peakEntry struct {
	bin int
	mag float64
}

func printTopBins(work []complex128, binHzBB float64, topN int) {
	peaks := make([]peakEntry, len(work))
	for i, v := range work {
		peaks[i] = peakEntry{bin: i, mag: cmplx.Abs(v)}
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].mag > peaks[j].mag })
	if topN > len(peaks) {
		topN = len(peaks)
	}
	fmt.Printf("      top %d bins:\n", topN)
	for i := 0; i < topN; i++ {
		p := peaks[i]
		// Express bin in signed Hz from baseband DC: bins >= len/2
		// are negative frequencies.
		hz := float64(p.bin) * binHzBB
		if p.bin >= len(work)/2 {
			hz = float64(p.bin-len(work)) * binHzBB
		}
		toneIdx := -1
		// Closest FT8 tone (only meaningful for in-band positive bins).
		if hz >= -ft8ToneSpacingHz/2 && hz < ft8ToneSpanHz+ft8ToneSpacingHz/2 {
			toneIdx = int((hz + ft8ToneSpacingHz/2) / ft8ToneSpacingHz)
		}
		toneTag := ""
		if toneIdx >= 0 && toneIdx < 8 {
			toneTag = fmt.Sprintf("  ~tone%d", toneIdx)
		}
		fmt.Printf("        bin %4d  %+7.2f Hz  mag=%.2e%s\n", p.bin, hz, p.mag, toneTag)
	}
}

// runScan runs the full sandbox candidate-detection pipeline on the
// supplied audio: spectrogram → FindCandidates → RefineCandidate per
// coarse hit. Matches every coarse and refined candidate against the
// truth manifest and prints a per-signal table showing the drift the
// refinement step applied.
//
// Match tolerances: coarse ±5 Hz / ±0.1 s (loose enough to absorb the
// 3.125 Hz × 40 ms grid snapping); refined ±1 Hz / ±0.05 s (tight
// enough that the refinement step has to actually improve over coarse
// to count).
func runScan(ch *sandbox.Channelizer, samples []float32, manifest *truth.Manifest, threshold, dfsweepOverride float64) {
	const (
		coarseFreqTol  = 5.0
		coarseDtTol    = 0.1
		refinedFreqTol = 1.0
		refinedDtTol   = 0.05
	)

	spec := sandbox.Spectrogram(samples)
	if spec == nil {
		log.Fatal("spectrogram returned no frames")
	}
	fmt.Printf("  spectrogram: %d frames × %d bins\n", len(spec), len(spec[0]))

	findOpts := sandbox.DefaultSearchOptions()
	findOpts.Threshold = threshold
	cands := sandbox.FindCandidates(spec, findOpts)
	fmt.Printf("  coarse candidates (thresh=%.2f): %d\n", threshold, len(cands))
	for i, c := range cands {
		if i >= 15 {
			fmt.Printf("    ... %d more\n", len(cands)-15)
			break
		}
		fmt.Printf("    %2d. freq=%8.2f Hz  dt=%+.3f s  sync=%.2f\n", i+1, c.FreqHz, c.DtSec, c.Sync)
	}
	fmt.Println()

	refineOpts := sandbox.DefaultRefineOptions()
	if dfsweepOverride > 0 {
		refineOpts.DFSweepRangeHz = dfsweepOverride
	}
	fmt.Printf("  refine: df sweep ±%.2f Hz step %.2f Hz\n\n", refineOpts.DFSweepRangeHz, refineOpts.DFSweepStepHz)
	refined := make([]sandbox.Candidate, len(cands))
	nsyncs := make([]int, len(cands))
	medianAbsLLRs := make([]float64, len(cands))
	decisiveBits := make([]int, len(cands))
	bpResults := make([]sandbox.BPResult, len(cands))
	bpOpts := sandbox.DefaultBPOptions()
	for i, c := range cands {
		r, err := sandbox.RefineCandidate(ch, c, refineOpts)
		if err != nil {
			log.Printf("    refine failed @ coarse=%.2f Hz / %.3f s: %v", c.FreqHz, c.DtSec, err)
			refined[i] = c
			continue
		}
		refined[i] = r
		grid, err := sandbox.ExtractSymbols(ch, r)
		if err != nil {
			log.Printf("    symbols failed @ refined=%.2f Hz / %.3f s: %v", r.FreqHz, r.DtSec, err)
			continue
		}
		nsyncs[i] = sandbox.HardSyncScore(grid)
		llrs := sandbox.SoftLLRs(grid)
		medianAbsLLRs[i], decisiveBits[i] = summariseLLRs(llrs[:])
		bpResults[i] = sandbox.BPDecode(llrs, bpOpts)
	}

	// Run Unpack77 on every BP-OK candidate so we can compare the
	// decoded text to the truth-manifest string. Also run the encoder
	// round-trip: EncodeLDPC(cw[0:91]) must equal cw exactly, which
	// validates generator-matrix interpretation, systematic-code
	// orientation, and CRC placement all in one comparison.
	decodedText := make([]string, len(cands))
	roundTripOK := make([]bool, len(cands))
	toneMatchOK := make([]bool, len(cands))
	snrMetrics := make([]sandbox.SNRMetrics, len(cands))
	// SNR measurement uses a narrower extraction than refinement:
	// 100 Hz isolates the 8 FT8 tones (which span 50 Hz starting at
	// tone 0) and excludes any signal 100 Hz away — important on
	// dense fixtures like 10cq where adjacent signals at ±100 Hz
	// would otherwise leak into the residual and floor the SNR.
	const snrBandwidthHz = 100.0
	for i, br := range bpResults {
		if !br.OK {
			continue
		}
		var payload [sandbox.LDPCPayloadBits]uint8
		copy(payload[:], br.Message91[:sandbox.LDPCPayloadBits])
		ur := sandbox.Unpack77(payload)
		if ur.OK {
			decodedText[i] = ur.Text
		} else {
			decodedText[i] = "[unpack: " + ur.Detail + "]"
		}

		// Acceptance test 1: cw' = EncodeLDPC(cw[0:91]) must equal cw.
		var info [sandbox.LDPCInfoBits]uint8
		copy(info[:], br.Codeword[:sandbox.LDPCInfoBits])
		reCw := sandbox.EncodeLDPC(info)
		roundTripOK[i] = reCw == br.Codeword

		// Acceptance test 2 (data positions) + (Costas positions): the
		// re-encoded tones from the BP-decoded codeword should match
		// the SymbolGrid argmax. Costas positions should match always
		// (BP-OK ⇒ nsync=21 plus encoder writes literal Costas tones);
		// data-symbol positions may drift on noisy fixtures where BP
		// corrected channel-induced errors.
		reTones := sandbox.CodewordToTones(reCw)
		grid, err := sandbox.ExtractSymbols(ch, refined[i])
		if err == nil {
			toneMatchOK[i] = countToneMismatches(reTones, grid) == 0
		}

		// M2: synthesize the clean reference, channelize the audio at
		// the refined freq using the SNR-specific narrower bandwidth,
		// slice the symbol-aligned window, fit / subtract / SNR.
		//
		// Centering the slice at the candidate's freq puts tone 0 at
		// baseband DC; the 8 FT8 tones occupy baseband [0, +43.75].
		// A 100 Hz slice covers [−50, +50] — fully captures the tones
		// and excludes ±100 Hz neighbours (relevant on dense fixtures).
		ref := sandbox.SynthesizeBaseband(reTones, snrBandwidthHz)
		bb, err := ch.Extract(refined[i].FreqHz, snrBandwidthHz)
		if err != nil {
			continue
		}
		startSample := int(math.Round((refined[i].DtSec + 0.5) * snrBandwidthHz))
		endSample := startSample + len(ref)
		if startSample < 0 || endSample > len(bb) {
			continue
		}
		// Per-symbol fit absorbs accumulated phase drift across the
		// 12.6-second slot — a single complex scale would be killed
		// by even a 0.05 Hz freq residual (>180° over the slot).
		samplesPerSymbol := int(math.Round(snrBandwidthHz * 0.16))
		snrMetrics[i] = sandbox.MeasureSNRPerSymbol(
			bb[startSample:endSample], ref, samplesPerSymbol, snrBandwidthHz,
		)
	}

	// Truth-vs-detection table.
	fmt.Printf("  %-30s  %-10s  %-8s  %-7s  %-14s  %-8s  %s\n",
		"truth signal", "refined Hz", "nsync/21", "BP iter", "BP result", "SNR(dB)", "decoded text")
	hits, missed := 0, 0
	usedCoarse := make([]bool, len(cands))
	for _, sig := range manifest.Signals {
		best := -1
		for i, r := range refined {
			if usedCoarse[i] {
				continue
			}
			if absF(r.CoarseFreqHz-sig.FreqHz) <= coarseFreqTol &&
				absF(r.CoarseDtSec-sig.DTSec) <= coarseDtTol {
				if best < 0 || r.Sync > refined[best].Sync {
					best = i
				}
			}
		}
		if best < 0 {
			fmt.Printf("  %-30s  %-10s  %-8s  %-7s  %-14s  %-8s  MISS\n",
				sig.Text, "—", "—", "—", "—", "—")
			missed++
			continue
		}
		r := refined[best]
		usedCoarse[best] = true
		_ = refinedDtTol
		_ = refinedFreqTol
		bpr := bpResults[best]
		bpStatus := bpResultLabel(bpr)
		text := decodedText[best]
		match := ""
		if bpr.OK {
			if text == sig.Text {
				match = "✓"
			} else {
				match = "✗ MISMATCH"
			}
		}
		snrStr := "—"
		if bpr.OK {
			snrStr = fmt.Sprintf("%+6.1f", snrMetrics[best].SNR2500DB)
		}
		fmt.Printf("  %-30s  %9.2f   %4d/21   %4d    %-14s  %-8s  %s %s\n",
			sig.Text, r.FreqHz, nsyncs[best], bpr.Iterations, bpStatus, snrStr, text, match)
		hits++
	}

	// Unmatched detections (spurious): how many fail BP outright vs
	// reach a clean syndrome but miss CRC vs decode successfully.
	spurious := 0
	spuriousNsyncBuckets := [22]int{}
	bpStats := struct{ syndromeFail, syndromeOnly, ok int }{}
	for i, used := range usedCoarse {
		if used {
			continue
		}
		spurious++
		spuriousNsyncBuckets[nsyncs[i]]++
		switch {
		case bpResults[i].OK:
			bpStats.ok++
		case bpResults[i].SyndromeClean:
			bpStats.syndromeOnly++
		default:
			bpStats.syndromeFail++
		}
	}
	fmt.Printf("\n  truth: %d matched, %d missed, %d spurious\n", hits, missed, spurious)

	// Histogram of spurious nsync.
	fmt.Printf("\n  spurious nsync histogram:\n")
	for n := 21; n >= 0; n-- {
		if spuriousNsyncBuckets[n] == 0 {
			continue
		}
		fmt.Printf("    %2d/21: %d\n", n, spuriousNsyncBuckets[n])
	}

	// BP outcomes on spurious. Healthy: most should be syndrome-fail.
	fmt.Printf("\n  spurious BP outcomes: %d syndrome-fail, %d syndrome-clean-CRC-fail, %d OK\n",
		bpStats.syndromeFail, bpStats.syndromeOnly, bpStats.ok)

	// Encoder round-trip across all BP-OK candidates. The "load-bearing"
	// acceptance test of the encoder milestone: EncodeLDPC(cw[0:91])
	// must equal cw exactly for every successful decode.
	rtOK, fullToneOK, rtTotal := 0, 0, 0
	totalCostasMismatch, totalDataMismatch := 0, 0
	for i, br := range bpResults {
		if !br.OK {
			continue
		}
		rtTotal++
		if roundTripOK[i] {
			rtOK++
		}
		if toneMatchOK[i] {
			fullToneOK++
		}
		// Per-candidate Costas vs data mismatch breakdown.
		var info [sandbox.LDPCInfoBits]uint8
		copy(info[:], br.Codeword[:sandbox.LDPCInfoBits])
		reTones := sandbox.CodewordToTones(sandbox.EncodeLDPC(info))
		grid, err := sandbox.ExtractSymbols(ch, refined[i])
		if err == nil {
			cm, dm := splitToneMismatches(reTones, grid)
			totalCostasMismatch += cm
			totalDataMismatch += dm
		}
	}
	fmt.Printf("\n  encoder round-trip:\n")
	fmt.Printf("    cw' == cw:                 %d/%d\n", rtOK, rtTotal)
	fmt.Printf("    full 79-tone match:        %d/%d\n", fullToneOK, rtTotal)
	fmt.Printf("    Costas mismatches (≤21):   %d total across %d candidates\n",
		totalCostasMismatch, rtTotal)
	fmt.Printf("    data mismatches (≤58):     %d total across %d candidates (BP-corrected bits)\n",
		totalDataMismatch, rtTotal)

	_ = medianAbsLLRs
	_ = decisiveBits
}

// countToneMismatches returns the number of positions (0..78) where
// the re-encoded tone differs from the SymbolGrid argmax.
func countToneMismatches(reTones [79]int, grid *sandbox.SymbolGrid) int {
	n := 0
	for s := 0; s < 79; s++ {
		max := 0
		maxP := grid.Tones[s][0]
		for m := 1; m < 8; m++ {
			if grid.Tones[s][m] > maxP {
				maxP = grid.Tones[s][m]
				max = m
			}
		}
		if max != reTones[s] {
			n++
		}
	}
	return n
}

// splitToneMismatches separates the mismatch count into Costas
// positions (21 anchors at blocks 0/36/72) and data positions (the
// remaining 58). Costas mismatches on a BP-OK candidate would
// indicate an encoder/Costas placement bug; data mismatches reflect
// channel-induced bit errors that BP corrected during decode.
func splitToneMismatches(reTones [79]int, grid *sandbox.SymbolGrid) (costas, data int) {
	costasSet := map[int]bool{}
	for _, blockStart := range [3]int{0, 36, 72} {
		for k := 0; k < 7; k++ {
			costasSet[blockStart+k] = true
		}
	}
	for s := 0; s < 79; s++ {
		max := 0
		maxP := grid.Tones[s][0]
		for m := 1; m < 8; m++ {
			if grid.Tones[s][m] > maxP {
				maxP = grid.Tones[s][m]
				max = m
			}
		}
		if max != reTones[s] {
			if costasSet[s] {
				costas++
			} else {
				data++
			}
		}
	}
	return
}

// bpResultLabel formats a BPResult into a compact status string for
// the per-candidate table.
func bpResultLabel(r sandbox.BPResult) string {
	switch {
	case r.OK:
		return "OK (CRC ✓)"
	case r.SyndromeClean:
		return "synd ✓ CRC ✗"
	default:
		return "synd ✗"
	}
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// summariseLLRs returns the median of |LLR| and the count of "decisive"
// bits where |LLR| exceeds half of the candidate's own maximum |LLR|.
// Both metrics are scale-invariant in opposite directions: the median
// is in raw LLR units (varying across SNR), the decisive-bit count is
// a fraction of the candidate's own peak (comparable across SNR).
func summariseLLRs(llrs []float64) (float64, int) {
	if len(llrs) == 0 {
		return 0, 0
	}
	absVals := make([]float64, len(llrs))
	maxAbs := 0.0
	for i, l := range llrs {
		a := l
		if a < 0 {
			a = -a
		}
		absVals[i] = a
		if a > maxAbs {
			maxAbs = a
		}
	}
	sort.Float64s(absVals)
	median := absVals[len(absVals)/2]
	decisive := 0
	if maxAbs > 0 {
		thresh := maxAbs * 0.5
		for _, a := range absVals {
			if a >= thresh {
				decisive++
			}
		}
	}
	return median, decisive
}
