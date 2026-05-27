// unmask-probe asks the single load-bearing question for "option 3"
// of the 2026-05-26 subtraction direction: when we subtract real-
// capture signals with comparatively clean phase fits (RMSResid
// below some threshold), do any previously-masked weaker signals
// surface as new decodes in the residual?
//
// Per the synth-subtract-probe measurement that motivated this:
// real-capture subtractions with the single-(amp, phase, freq)
// calibration give only 25-50% energy reduction even on the
// cleanest candidates (RMS < 1.0). That's not the >90% reduction
// the full multi-pass story wants — but if even at 25-50%
// reduction we unmask ANY jt9-confirmed signal that didn't decode
// in pass 1, multi-pass subtraction has real upside on real
// captures and option 2 (per-symbol phase-tracked subtraction) is
// worth the engineering investment. If we unmask zero such signals
// across the 6-WAV corpus, option 3 is negative and the partial-
// subtraction route is closed; option 2 is the only remaining path.
//
// Algorithm per WAV:
//  1. Run candidates.Find + the full decode pipeline → records1
//  2. Filter records1 to clean decodes: CRC pass AND fit.RMSResid
//     below the supplied threshold (default 1.0 — admits ~20% of
//     our real-capture decodes per the synth-subtract-probe scan).
//  3. Up to N of the cleanest get synthesised + calibrated +
//     subtracted from a working audio copy.
//  4. Re-run candidates.Find + decode on the residual → records2.
//  5. Compare: which records2 entries are NEW (not in records1)?
//     Which records1 entries are LOST (not in records2 AND not
//     among the subtracted signals)?
//  6. Score new + lost against the truth manifest: oracle-matched
//     vs noise.
//
// Output: per-WAV table + corpus totals. The headline metric is
// "oracle-matched new decodes minus oracle-matched lost decodes."
// Positive = subtraction helps; zero = neutral; negative = harms.
//
// Usage:
//
//	go run ./research/cmd/unmask-probe                                 # captures/, defaults
//	go run ./research/cmd/unmask-probe -dir PATH -rms 1.0 -top 1       # explicit settings
//	go run ./research/cmd/unmask-probe -v                              # per-WAV detail
//
// Import rules: research code may use internal/audio (and stdlib +
// its own packages) but MUST NOT import internal/ft8/*.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/cmplx"
	"os"
	"path/filepath"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
	"github.com/ColonelBlimp/station-manager/research/ldpc"
	"github.com/ColonelBlimp/station-manager/research/synth"
	"github.com/ColonelBlimp/station-manager/research/truth"
	"github.com/ColonelBlimp/station-manager/research/unpack"
)

const (
	expectedSampleRate = 12000
	codewordBits       = 174
	tSym               = 1920.0 / 12000.0

	jt9FreqTolHz = 5.0
	jt9DTTolSec  = 0.3
)

// decoded is one successful decode plus the diagnostic info needed
// to (a) match it against truth, (b) potentially subtract it from
// the audio, (c) compare across passes for new/lost classification.
type decoded struct {
	freq        float64
	dt          float64
	text        string
	codeword    [codewordBits]uint8
	rmsResid    float64
	preciseFreq float64
}

// llrConfig collects the LLR-mode flags so they can be threaded
// through the probe entry points without exploding the parameter
// lists. At most one of `costas` / `soft` is true.
type llrConfig struct {
	soft      bool
	t1        float64
	t2        float64
	softClamp float64
	costas    bool
}

func main() {
	dir := flag.String("dir", "captures", "directory containing .wav files (non-recursive)")
	rmsThresh := flag.Float64("rms", 1.0, "subtract decodes with phase-fit RMSResid below this threshold")
	topSubtract := flag.Int("top", 1, "subtract up to this many of the cleanest signals per WAV")
	perSymbol := flag.Bool("per-symbol", false, "use per-symbol phase-tracked calibration: 21 per-anchor (amp, phase) ratios, linearly interpolated. Measured-flat-or-negative vs single-c on this corpus — per-anchor SNR is too low for the interpolation to outperform global averaging.")
	perBlock := flag.Bool("per-block", false, "use per-block calibration: 3 weighted-LS coefficients (one per Costas block) interpolated across the TX window. Averages 7 anchors per block to suppress noise while still allowing block-level trajectory variation. Hypothesised middle ground between single-c and per-symbol.")
	coherentAdaptive := flag.Bool("coherent-adaptive", false, "use adaptive coherent cancellation: per-sample demod → Hann LPF → reconstruct → subtract, with timing refinement (±20 sample search). Generalises single-c / per-block / per-symbol — channel estimation bandwidth is set by the LPF length (N=12000 = ~1.4 Hz) rather than by anchor count. Per 2026-05-26 design discussion.")
	iterations := flag.Int("iterations", 1, "iterative detect-decode-subtract passes (1-3). Each iteration rebuilds residual fresh from original audio minus all decoded signals so far. Convergence: stop when no new decodes. Capped at 3 per operator directive.")
	calibrateCostas := flag.Bool("calibrate-costas", false, "scale LLRs using a Costas-anchor-derived noise level (research/demod.EstimateCostasCalibration) instead of the data-symbol-derived plain/Winsorized noise floor. Uses LLRsCalibrated. Falls back to standard LLRs when fewer than 3 Costas anchors are accessible.")
	llrSoft := flag.Bool("llr-soft", false, "apply surgical row-level softening (research/demod.LLRsSoftened): pathological symbol rows (ambiguous winner OR whole-row weak) get their LLRs capped at a lower threshold than the standard ±20. Mutually exclusive with -calibrate-costas in this first cut.")
	llrT1 := flag.Float64("llr-t1", 1.0, "ambiguous-winner threshold for -llr-soft. A row is pathological if (top1-top2)/noise < T1.")
	llrT2 := flag.Float64("llr-t2", 2.0, "weak-row threshold for -llr-soft. A row is also pathological if top1/noise < T2.")
	llrSoftClamp := flag.Float64("llr-soft-clamp", 5.0, "|LLR| ceiling applied to pathological rows by -llr-soft. Standard non-pathological clamp stays at ±20. Sweep candidates: {3, 5, 7, 10}.")
	refineMode := flag.String("refinement", "default", "candidate-refinement mode in research/candidates.FindWithGates. One of: 'default' (GeoContrast-only; Session-96 measurement kept this as the production default after gate-aware showed config-dependent trade-offs), 'gate-aware' (only accept refined positions that still pass the categorical gate; preserves invariant but trades baseline matched for production-config matched), 'strict-drop' (current refinement, drop candidate if refined Verify fails gate; A/B only).")
	verbose := flag.Bool("v", false, "print per-decode detail")
	flag.Parse()

	if *iterations < 1 || *iterations > 3 {
		log.Fatalf("-iterations %d outside allowed range 1-3", *iterations)
	}
	if *llrSoft && *calibrateCostas {
		log.Fatal("-llr-soft and -calibrate-costas are mutually exclusive in this first cut")
	}

	var refinementMode candidates.RefinementMode
	switch *refineMode {
	case "default":
		refinementMode = candidates.RefinementDefault
	case "gate-aware":
		refinementMode = candidates.RefinementGateAware
	case "strict-drop":
		refinementMode = candidates.RefinementStrictDrop
	default:
		log.Fatalf("-refinement %q: must be one of default / gate-aware / strict-drop", *refineMode)
	}

	entries, err := os.ReadDir(*dir)
	if err != nil {
		log.Fatalf("read dir %q: %v", *dir, err)
	}
	var wavs []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".wav" {
			continue
		}
		wavs = append(wavs, filepath.Join(*dir, e.Name()))
	}
	sort.Strings(wavs)
	if len(wavs) == 0 {
		log.Fatalf("no .wav files in %q", *dir)
	}

	fmt.Printf("unmask-probe: %d WAVs, rms-threshold=%.2f, top-subtract=%d\n\n",
		len(wavs), *rmsThresh, *topSubtract)

	var corpNewMatched, corpNewExtra, corpLostMatched, corpSubtracted int

	mode := "global single-c"
	modeCount := 0
	if *perSymbol {
		modeCount++
	}
	if *perBlock {
		modeCount++
	}
	if *coherentAdaptive {
		modeCount++
	}
	if modeCount > 1 {
		log.Fatal("specify at most one of -per-symbol / -per-block / -coherent-adaptive")
	}
	switch {
	case *perSymbol:
		mode = "per-symbol phase-tracked"
	case *perBlock:
		mode = "per-block (3-point) calibration"
	case *coherentAdaptive:
		mode = "coherent adaptive (LPF N=12000, ±20 timing search)"
	}
	fmt.Printf("subtraction mode: %s, iterations: %d\n\n", mode, *iterations)

	llrCfg := llrConfig{
		soft:      *llrSoft,
		t1:        *llrT1,
		t2:        *llrT2,
		softClamp: *llrSoftClamp,
		costas:    *calibrateCostas,
	}

	for _, wav := range wavs {
		fmt.Printf("=== %s ===\n", wav)
		var newMatched, newExtra, lostMatched, subtracted int
		if *iterations > 1 {
			newMatched, newExtra, lostMatched, subtracted = probeWAVIterative(wav, *rmsThresh, *topSubtract, *perSymbol, *perBlock, *coherentAdaptive, llrCfg, refinementMode, *iterations, *verbose)
		} else {
			newMatched, newExtra, lostMatched, subtracted = probeWAV(wav, *rmsThresh, *topSubtract, *perSymbol, *perBlock, *coherentAdaptive, llrCfg, refinementMode, *verbose)
		}
		corpNewMatched += newMatched
		corpNewExtra += newExtra
		corpLostMatched += lostMatched
		corpSubtracted += subtracted
		fmt.Println()
	}

	fmt.Printf("=== Corpus totals (rms < %.2f, top %d) ===\n", *rmsThresh, *topSubtract)
	fmt.Printf("  subtractions performed:       %d\n", corpSubtracted)
	fmt.Printf("  NEW oracle-matched decodes:   %d  (unmasked by subtraction)\n", corpNewMatched)
	fmt.Printf("  NEW text-extra decodes:       %d  (CRC-pass but no truth match)\n", corpNewExtra)
	fmt.Printf("  LOST oracle-matched decodes:  %d  (regression from subtraction)\n", corpLostMatched)
	net := corpNewMatched - corpLostMatched
	fmt.Printf("  Net matched delta:            %+d\n", net)
	switch {
	case net > 0:
		fmt.Println("  Verdict: subtraction has positive lift even at current calibration quality.")
	case net == 0 && corpNewExtra == 0:
		fmt.Println("  Verdict: subtraction is neutral on this corpus; no harm but no help.")
	case net == 0:
		fmt.Println("  Verdict: subtraction is neutral on matched but adds text-extras — net negative on parity.")
	default:
		fmt.Println("  Verdict: subtraction is net negative — losing more matched than it gains.")
	}
}

// probeWAV runs the pass-1 decode, picks subtraction targets,
// builds the residual, runs the pass-2 decode, and classifies the
// new/lost decodes against the truth manifest. Returns the four
// corpus-aggregate counters.
func probeWAV(wavPath string, rmsThresh float64, topSubtract int, perSymbol, perBlock, coherentAdaptive bool, llr llrConfig, refinementMode candidates.RefinementMode, verbose bool) (newMatched, newExtra, lostMatched, subtracted int) {
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		log.Printf("  read wav: %v — skipping", err)
		return
	}
	if data.SampleRate != expectedSampleRate || data.Channels != 1 {
		log.Printf("  rate=%d channels=%d — skipping", data.SampleRate, data.Channels)
		return
	}

	manifest, _ := truth.Read(truth.PathFor(wavPath))

	findGates := candidates.DefaultGates
	findGates.RefinementMode = refinementMode

	// Pass 1.
	cands1 := candidates.FindWithGates(data.Samples, findGates)
	records1 := decodeAll(data.Samples, cands1, llr)
	fmt.Printf("  pass 1: %d candidates → %d CRC-pass decodes\n", len(cands1), len(records1))

	// Filter to clean decodes.
	var clean []decoded
	for _, r := range records1 {
		if r.rmsResid < rmsThresh {
			clean = append(clean, r)
		}
	}
	sort.Slice(clean, func(i, j int) bool { return clean[i].rmsResid < clean[j].rmsResid })
	if len(clean) > topSubtract {
		clean = clean[:topSubtract]
	}
	if len(clean) == 0 {
		fmt.Printf("  no decodes meet rms < %.2f — nothing to subtract\n", rmsThresh)
		return
	}
	fmt.Printf("  subtracting %d clean decode(s) (rms < %.2f):\n", len(clean), rmsThresh)
	for _, c := range clean {
		fmt.Printf("    %q at freq=%.2f, dt=%+.3f, rms=%.3f\n", c.text, c.freq, c.dt, c.rmsResid)
	}

	// Build residual by subtracting calibrated synth for each clean decode.
	residual := make([]float32, len(data.Samples))
	copy(residual, data.Samples)
	for _, c := range clean {
		ok := dispatchSubtract(residual, c, perSymbol, perBlock, coherentAdaptive)
		if !ok {
			continue
		}
		subtracted++
	}

	// Pass 2 on residual.
	cands2 := candidates.FindWithGates(residual, findGates)
	records2 := decodeAll(residual, cands2, llr)
	fmt.Printf("  pass 2: %d candidates → %d CRC-pass decodes\n", len(cands2), len(records2))

	// Classify diff: new (in pass 2 not pass 1), lost (in pass 1 not pass 2).
	// Match by text + (freq, dt) proximity. The subtracted signals are
	// expected to be "lost" — we exclude them from the lost-decode set.
	subtractedSet := make(map[string]bool, len(clean))
	for _, c := range clean {
		subtractedSet[recordKey(c.text, c.freq, c.dt)] = true
	}

	newDecodes := diffNew(records2, records1)
	lostDecodes := diffNew(records1, records2)
	// Strip subtracted from "lost."
	{
		filtered := lostDecodes[:0]
		for _, d := range lostDecodes {
			if !subtractedSet[recordKey(d.text, d.freq, d.dt)] {
				filtered = append(filtered, d)
			}
		}
		lostDecodes = filtered
	}

	// Truth match.
	for _, d := range newDecodes {
		if matchesTruth(d, manifest) {
			newMatched++
			if verbose {
				fmt.Printf("    NEW MATCHED: %q at freq=%.2f, dt=%+.3f\n", d.text, d.freq, d.dt)
			}
		} else {
			newExtra++
			if verbose {
				fmt.Printf("    NEW EXTRA:   %q at freq=%.2f, dt=%+.3f\n", d.text, d.freq, d.dt)
			}
		}
	}
	for _, d := range lostDecodes {
		if matchesTruth(d, manifest) {
			lostMatched++
			if verbose {
				fmt.Printf("    LOST MATCHED: %q at freq=%.2f, dt=%+.3f\n", d.text, d.freq, d.dt)
			}
		}
	}

	fmt.Printf("  outcome: %d new-matched, %d new-extra, %d lost-matched\n",
		newMatched, newExtra, lostMatched)

	return newMatched, newExtra, lostMatched, subtracted
}

// decodeAll runs the full decode pipeline (Find result → demod →
// LLRs → ldpc.Decode → unpack) and returns one `decoded` per
// CRC-passing candidate with non-empty unpack text.
func decodeAll(samples []float32, cands []candidates.Candidate, llr llrConfig) []decoded {
	var out []decoded
	for _, c := range cands {
		energies := demod.Demod(samples, c.Freq, c.DT)
		var llrs [codewordBits]float64
		switch {
		case llr.soft:
			llrs = demod.LLRsSoftened(energies, llr.t1, llr.t2, llr.softClamp)
		case llr.costas:
			// Costas-anchor-derived noise scale. Falls back to the
			// data-symbol Winsorized estimate when fewer than 3
			// anchors are accessible (noiseLevel returns 0).
			_, noiseLevel := demod.EstimateCostasCalibration(samples, c.Freq, c.DT)
			if noiseLevel > 0 {
				llrs = demod.LLRsCalibrated(energies, noiseLevel)
			} else {
				llrs = demod.LLRs(energies)
			}
		default:
			llrs = demod.LLRs(energies)
		}
		var input [codewordBits]float64
		for k := 0; k < codewordBits; k++ {
			input[k] = llrs[k]
		}
		result, stats := ldpc.Decode(input)
		if !stats.ConvergedCRC {
			continue
		}
		ur, uerr := unpack.Unpack(result.Info)
		if uerr != nil {
			continue
		}
		fit := demod.PhaseFitFor(samples, c.Freq, c.DT)
		preciseFreq := c.Freq
		if !math.IsInf(fit.RMSResid, 1) {
			slopeUser := wrapPi(fit.Slope - 2*math.Pi*c.Freq*tSym)
			preciseFreq = c.Freq + slopeUser/(2*math.Pi*tSym)
		}
		out = append(out, decoded{
			freq:        c.Freq,
			dt:          c.DT,
			text:        ur.Text,
			codeword:    result.Codeword,
			rmsResid:    fit.RMSResid,
			preciseFreq: preciseFreq,
		})
	}
	return out
}

// subtractInPlace synthesises the calibrated waveform for `d` and
// subtracts it from `audio`. Returns false if calibration failed
// (insufficient accessible anchors).
func subtractInPlace(audio []float32, d decoded) bool {
	probe := synth.Synthesize(d.codeword, d.preciseFreq, d.dt, len(audio), 1.0, 0.0)
	xReal, accReal := demod.CostasAnchorAmplitudes(audio, d.preciseFreq, d.dt)
	xSynth, accSynth := demod.CostasAnchorAmplitudes(probe, d.preciseFreq, d.dt)

	var num complex128
	var den float64
	accessible := 0
	for i := 0; i < len(xReal); i++ {
		if !accReal[i] || !accSynth[i] {
			continue
		}
		num += xReal[i] * cmplx.Conj(xSynth[i])
		den += real(xSynth[i])*real(xSynth[i]) + imag(xSynth[i])*imag(xSynth[i])
		accessible++
	}
	if accessible < 7 || den == 0 {
		return false
	}
	cCalib := num / complex(den, 0)
	amp := cmplx.Abs(cCalib)
	phase := cmplx.Phase(cCalib)

	calibrated := synth.Synthesize(d.codeword, d.preciseFreq, d.dt, len(audio), amp, phase)
	for k := range audio {
		audio[k] -= calibrated[k]
	}
	return true
}

// subtractPerSymbolInPlace synthesises the codeword and subtracts
// it from `audio` using per-symbol phase-tracked calibration. Per-
// anchor (amp, phase) ratios at the 21 Costas-anchor positions are
// linearly interpolated across the TX window, then applied sample-
// by-sample to the complex synth envelope before subtracting.
//
// This handles non-linear phase trajectories that the global
// single-c calibration in subtractInPlace can't model. Real captures
// typically have RMSResid of the linear-phase fit between 1 and 2
// radians; the per-symbol approach decouples the calibration from
// any single-fit assumption.
//
// Returns true iff at least 7 of the 21 Costas anchors were
// accessible in both audio and synth (otherwise the interpolation
// has too few support points to be meaningful).
func subtractPerSymbolInPlace(audio []float32, d decoded) bool {
	// Step 1: complex synth at unit amp, zero initial phase.
	zSynth := synth.SynthesizeComplex(d.codeword, d.preciseFreq, d.dt, len(audio), 1.0, 0.0)

	// Step 2: per-anchor calibration ratios. CostasAnchorAmplitudes
	// runs on a real-valued buffer; we feed it imag(zSynth) so the
	// synth side matches the real-valued audio convention. The
	// resulting xSynth is the synth's complex amplitude at each
	// anchor's expected tone — same shape as xReal.
	synthReal := make([]float32, len(zSynth))
	for k := range zSynth {
		synthReal[k] = float32(imag(zSynth[k]))
	}
	xReal, accReal := demod.CostasAnchorAmplitudes(audio, d.preciseFreq, d.dt)
	xSynth, accSynth := demod.CostasAnchorAmplitudes(synthReal, d.preciseFreq, d.dt)

	type anchorCalib struct {
		sampleCentre int
		amp          float64
		phase        float64 // wrapped; unwrapped in pass 2
	}
	var points []anchorCalib

	txStartSample := int(math.Round((0.5 + d.dt) * float64(expectedSampleRate)))
	const nsps = 1920
	for i := 0; i < len(xReal); i++ {
		if !accReal[i] || !accSynth[i] {
			continue
		}
		c := xReal[i] / xSynth[i]
		points = append(points, anchorCalib{
			sampleCentre: txStartSample + costasSymPos(i)*nsps + nsps/2,
			amp:          cmplx.Abs(c),
			phase:        cmplx.Phase(c),
		})
	}
	if len(points) < 7 {
		return false
	}
	// Anchors are produced in costasSym order, which is monotonic in
	// channel-symbol index — so points are already sorted by sample.
	// Unwrap phases.
	for i := 1; i < len(points); i++ {
		diff := points[i].phase - points[i-1].phase
		for diff > math.Pi {
			points[i].phase -= 2 * math.Pi
			diff -= 2 * math.Pi
		}
		for diff <= -math.Pi {
			points[i].phase += 2 * math.Pi
			diff += 2 * math.Pi
		}
	}

	// Step 3: interpolate (amp, phase) to a per-sample calibration,
	// then apply c_k · z_synth[k] sample-by-sample and subtract.
	//
	// imag(c · z) where c = amp·e^(jφ_c) and z = re+j·im
	//   = amp · (cos(φ_c)·im + sin(φ_c)·re)
	first := points[0]
	last := points[len(points)-1]
	cursor := 0 // running index into points[]
	for k := 0; k < len(audio); k++ {
		// Find amp(k), phase(k).
		var amp, phase float64
		switch {
		case k <= first.sampleCentre:
			amp = first.amp
			phase = first.phase
		case k >= last.sampleCentre:
			amp = last.amp
			phase = last.phase
		default:
			// Advance cursor until points[cursor+1].sampleCentre > k.
			for cursor+1 < len(points) && points[cursor+1].sampleCentre <= k {
				cursor++
			}
			a, b := points[cursor], points[cursor+1]
			frac := float64(k-a.sampleCentre) / float64(b.sampleCentre-a.sampleCentre)
			amp = a.amp + frac*(b.amp-a.amp)
			phase = a.phase + frac*(b.phase-a.phase)
		}
		cosP := math.Cos(phase)
		sinP := math.Sin(phase)
		calibrated := amp * (cosP*imag(zSynth[k]) + sinP*real(zSynth[k]))
		audio[k] -= float32(calibrated)
	}
	return true
}

// dispatchSubtract routes to the appropriate per-mode subtraction
// function. At most one of the boolean flags is true per the upstream
// mutual-exclusion check. The coherent-adaptive path's returned bestDelta
// is discarded here; callers that want to cache it for iter 2+ should
// invoke cancelCoherentAdaptiveInPlace directly.
func dispatchSubtract(audio []float32, d decoded, perSymbol, perBlock, coherentAdaptive bool) bool {
	switch {
	case coherentAdaptive:
		_, ok := cancelCoherentAdaptiveInPlace(audio, d)
		return ok
	case perSymbol:
		return subtractPerSymbolInPlace(audio, d)
	case perBlock:
		return subtractPerBlockInPlace(audio, d)
	default:
		return subtractInPlace(audio, d)
	}
}

// dispatchSubtractIterative is the iterative-aware variant: for the
// coherent-adaptive path, it consults a per-signal best-delta cache —
// broad search in iter 1, tight search around the cached delta in iter
// 2+. Other modes ignore the cache.
//
// The cache key is the decoded text (sufficient since each signal has
// unique text within a slot under FT8 semantics). Maps callers can pass
// the same cache across iterations to track best-delta convergence.
func dispatchSubtractIterative(audio []float32, d decoded, perSymbol, perBlock, coherentAdaptive bool, deltaCache map[string]int) bool {
	if !coherentAdaptive {
		return dispatchSubtract(audio, d, perSymbol, perBlock, coherentAdaptive)
	}
	if cached, ok := deltaCache[d.text]; ok {
		newDelta, ok := cancelCoherentAdaptiveAroundDelta(audio, d, cached)
		if !ok {
			return false
		}
		deltaCache[d.text] = newDelta
		return true
	}
	newDelta, ok := cancelCoherentAdaptiveInPlace(audio, d)
	if !ok {
		return false
	}
	deltaCache[d.text] = newDelta
	return true
}

// probeWAVIterative runs the operator's 2026-05-26 iterative algorithm:
// max N passes of (Find on fresh residual → decode each candidate →
// interleaved subtract if clean), with convergence on "no new decodes
// this pass." Reports new vs lost matched against the truth manifest
// over all passes combined.
//
// Fresh-residual rebuild each pass: residual is rebuilt from the original
// audio minus the entire decoded-set-so-far, NOT cumulatively-subtracted
// across passes. This prevents subtraction errors from compounding.
//
// Within-pass interleaving: as each candidate decodes successfully, it is
// immediately subtracted from the residual so subsequent candidates in
// the same pass see the cleaner version. The operator-proposed structure.
func probeWAVIterative(wavPath string, rmsThresh float64, topSubtract int, perSymbol, perBlock, coherentAdaptive bool, llr llrConfig, refinementMode candidates.RefinementMode, maxIter int, verbose bool) (newMatched, newExtra, lostMatched, subtracted int) {
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		log.Printf("  read wav: %v — skipping", err)
		return
	}
	if data.SampleRate != expectedSampleRate || data.Channels != 1 {
		log.Printf("  rate=%d channels=%d — skipping", data.SampleRate, data.Channels)
		return
	}
	manifest, _ := truth.Read(truth.PathFor(wavPath))

	findGates := candidates.DefaultGates
	findGates.RefinementMode = refinementMode

	// Pass-1 baseline: decode without any subtraction. Used to classify
	// new-vs-lost against the final iterative decoded set.
	pass1Cands := candidates.FindWithGates(data.Samples, findGates)
	pass1Records := decodeAll(data.Samples, pass1Cands, llr)
	fmt.Printf("  baseline pass 1: %d CRC-pass decodes\n", len(pass1Records))

	// Iterative loop.
	type key struct {
		text string
	}
	allDecoded := map[key]decoded{}
	for _, r := range pass1Records {
		allDecoded[key{r.text}] = r
	}

	// deltaCache holds the best-delta found during iter-1's broad timing
	// search per signal text, so iter 2+ can do a tight ±2 sample search
	// around the cached value instead of repeating the full ±20 broad
	// search. Cache is per-WAV (signals are unique within a slot in FT8).
	// Used only by the coherent-adaptive path; other modes ignore it.
	deltaCache := map[string]int{}

	var lastIterNew int
	for iter := 1; iter <= maxIter; iter++ {
		// Rebuild residual fresh from original audio minus all decoded so far.
		residual := make([]float32, len(data.Samples))
		copy(residual, data.Samples)
		for _, d := range allDecoded {
			if d.rmsResid >= rmsThresh {
				continue
			}
			dispatchSubtractIterative(residual, d, perSymbol, perBlock, coherentAdaptive, deltaCache)
			subtracted++
		}

		// Find + decode on this residual.
		cands := candidates.FindWithGates(residual, findGates)
		records := decodeAll(residual, cands, llr)

		// Identify and add new decodes; interleave-subtract as we go for
		// any newly-decoded clean signals so later candidates in this pass
		// see the cleaner version.
		newThisIter := 0
		for _, r := range records {
			if _, exists := allDecoded[key{r.text}]; exists {
				continue
			}
			allDecoded[key{r.text}] = r
			newThisIter++
			if r.rmsResid < rmsThresh {
				dispatchSubtractIterative(residual, r, perSymbol, perBlock, coherentAdaptive, deltaCache)
			}
		}
		fmt.Printf("  iter %d: %d candidates, %d decodes, %d new\n",
			iter, len(cands), len(records), newThisIter)
		lastIterNew = newThisIter
		if newThisIter == 0 {
			break
		}
	}
	_ = lastIterNew

	// Classify allDecoded against pass1 (for new) and against truth (for matched).
	for _, d := range allDecoded {
		_, inPass1 := containsByKey(pass1Records, d)
		matched := matchesTruth(d, manifest)
		switch {
		case inPass1 && matched:
			// Already counted as part of baseline — neither new nor lost.
		case inPass1 && !matched:
			// Pass-1 had it but truth doesn't — pass-1 text-extra, not from iteration.
		case !inPass1 && matched:
			newMatched++
			if verbose {
				fmt.Printf("    NEW MATCHED: %q at freq=%.2f, dt=%+.3f\n", d.text, d.freq, d.dt)
			}
		case !inPass1 && !matched:
			newExtra++
			if verbose {
				fmt.Printf("    NEW EXTRA:   %q at freq=%.2f, dt=%+.3f\n", d.text, d.freq, d.dt)
			}
		}
	}
	// Lost = pass1-matched signals NOT in allDecoded. Should always be 0
	// since allDecoded starts with pass1Records and grows.
	pass1Texts := map[string]bool{}
	for _, r := range pass1Records {
		pass1Texts[r.text] = true
	}
	for _, r := range pass1Records {
		if !matchesTruth(r, manifest) {
			continue
		}
		if _, exists := allDecoded[key{r.text}]; !exists {
			lostMatched++
		}
	}

	fmt.Printf("  outcome: %d new-matched, %d new-extra, %d lost-matched\n",
		newMatched, newExtra, lostMatched)
	return newMatched, newExtra, lostMatched, subtracted
}

// containsByKey reports whether a `decoded` matching d (by text + freq + dt
// proximity) exists in records.
func containsByKey(records []decoded, d decoded) (decoded, bool) {
	for _, r := range records {
		if r.text == d.text &&
			math.Abs(r.freq-d.freq) <= jt9FreqTolHz &&
			math.Abs(r.dt-d.dt) <= jt9DTTolSec {
			return r, true
		}
	}
	return decoded{}, false
}

// subtractPerBlockInPlace is the middle ground between subtractInPlace
// (global single-c, 21-anchor weighted average) and subtractPerSymbol
// (per-anchor calibration, 21 noisy points). It computes a separate
// weighted-LS calibration per Costas block (each averaging 7 anchors,
// √7 SNR improvement vs per-anchor), then linearly interpolates
// between the 3 block centres across the TX window.
//
// Same complex-multiplier model + sample-by-sample application as
// subtractPerSymbolInPlace; the only difference is the number and
// noise level of calibration points (3 cleaner ones vs 21 noisier).
//
// Returns false if fewer than 2 blocks have at least 3 accessible
// anchors each (insufficient for the interpolation).
func subtractPerBlockInPlace(audio []float32, d decoded) bool {
	zSynth := synth.SynthesizeComplex(d.codeword, d.preciseFreq, d.dt, len(audio), 1.0, 0.0)
	synthReal := make([]float32, len(zSynth))
	for k := range zSynth {
		synthReal[k] = float32(imag(zSynth[k]))
	}
	xReal, accReal := demod.CostasAnchorAmplitudes(audio, d.preciseFreq, d.dt)
	xSynth, accSynth := demod.CostasAnchorAmplitudes(synthReal, d.preciseFreq, d.dt)

	type blockCalib struct {
		sampleCentre int
		amp          float64
		phase        float64
	}
	var blocks []blockCalib

	txStartSample := int(math.Round((0.5 + d.dt) * float64(expectedSampleRate)))
	const (
		nsps      = 1920
		blockSize = 7 // anchors per Costas block
	)
	for block := 0; block < 3; block++ {
		var num complex128
		var den float64
		accessible := 0
		for j := 0; j < blockSize; j++ {
			i := block*blockSize + j
			if !accReal[i] || !accSynth[i] {
				continue
			}
			num += xReal[i] * cmplx.Conj(xSynth[i])
			den += real(xSynth[i])*real(xSynth[i]) + imag(xSynth[i])*imag(xSynth[i])
			accessible++
		}
		if accessible < 3 || den == 0 {
			continue
		}
		c := num / complex(den, 0)
		// Block centre at its midpoint anchor (sym 3 within the block).
		midAnchor := costasSymPos(block*blockSize + 3)
		blocks = append(blocks, blockCalib{
			sampleCentre: txStartSample + midAnchor*nsps + nsps/2,
			amp:          cmplx.Abs(c),
			phase:        cmplx.Phase(c),
		})
	}
	if len(blocks) < 2 {
		return false
	}
	// Unwrap phases between adjacent blocks.
	for i := 1; i < len(blocks); i++ {
		diff := blocks[i].phase - blocks[i-1].phase
		for diff > math.Pi {
			blocks[i].phase -= 2 * math.Pi
			diff -= 2 * math.Pi
		}
		for diff <= -math.Pi {
			blocks[i].phase += 2 * math.Pi
			diff += 2 * math.Pi
		}
	}

	first := blocks[0]
	last := blocks[len(blocks)-1]
	cursor := 0
	for k := 0; k < len(audio); k++ {
		var amp, phase float64
		switch {
		case k <= first.sampleCentre:
			amp = first.amp
			phase = first.phase
		case k >= last.sampleCentre:
			amp = last.amp
			phase = last.phase
		default:
			for cursor+1 < len(blocks) && blocks[cursor+1].sampleCentre <= k {
				cursor++
			}
			a, b := blocks[cursor], blocks[cursor+1]
			frac := float64(k-a.sampleCentre) / float64(b.sampleCentre-a.sampleCentre)
			amp = a.amp + frac*(b.amp-a.amp)
			phase = a.phase + frac*(b.phase-a.phase)
		}
		cosP := math.Cos(phase)
		sinP := math.Sin(phase)
		calibrated := amp * (cosP*imag(zSynth[k]) + sinP*real(zSynth[k]))
		audio[k] -= float32(calibrated)
	}
	return true
}

// costasSymPos maps anchor index 0..20 to its channel-symbol
// position (0..78). Anchor blocks are 7 symbols each, blocks
// stride 36 channel symbols apart per the FT8 protocol.
func costasSymPos(anchorIdx int) int {
	const (
		costasSymbolsPerBlock = 7
		costasBlockStride     = 36
	)
	block := anchorIdx / costasSymbolsPerBlock
	symInBlock := anchorIdx % costasSymbolsPerBlock
	return block*costasBlockStride + symInBlock
}

// diffNew returns entries in `a` that have no matching entry in `b`
// — matched by text + (freq, dt) tolerance. Used for both
// new-decode and lost-decode classification.
func diffNew(a, b []decoded) []decoded {
	var out []decoded
	for _, ai := range a {
		matched := false
		for _, bj := range b {
			if ai.text == bj.text &&
				math.Abs(ai.freq-bj.freq) <= jt9FreqTolHz &&
				math.Abs(ai.dt-bj.dt) <= jt9DTTolSec {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, ai)
		}
	}
	return out
}

// matchesTruth returns true iff `d` corresponds to a jt9-oracle
// entry in the manifest (text + freq/dt match within tolerance).
func matchesTruth(d decoded, manifest *truth.Manifest) bool {
	if manifest == nil {
		return false
	}
	for _, ts := range manifest.Signals {
		if d.text != ts.Text {
			continue
		}
		if math.Abs(d.freq-ts.FreqHz) <= jt9FreqTolHz &&
			math.Abs(d.dt-ts.DTSec) <= jt9DTTolSec {
			return true
		}
	}
	return false
}

// recordKey is a stable identifier for a decode used by the
// "exclude subtracted from lost" filter in probeWAV.
func recordKey(text string, freq, dt float64) string {
	return fmt.Sprintf("%q|%.2f|%.3f", text, freq, dt)
}

// wrapPi wraps x to (-π, +π].
func wrapPi(x float64) float64 {
	for x > math.Pi {
		x -= 2 * math.Pi
	}
	for x <= -math.Pi {
		x += 2 * math.Pi
	}
	return x
}
