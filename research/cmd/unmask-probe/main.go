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

func main() {
	dir := flag.String("dir", "captures", "directory containing .wav files (non-recursive)")
	rmsThresh := flag.Float64("rms", 1.0, "subtract decodes with phase-fit RMSResid below this threshold")
	topSubtract := flag.Int("top", 1, "subtract up to this many of the cleanest signals per WAV")
	verbose := flag.Bool("v", false, "print per-decode detail")
	flag.Parse()

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

	for _, wav := range wavs {
		fmt.Printf("=== %s ===\n", wav)
		newMatched, newExtra, lostMatched, subtracted := probeWAV(wav, *rmsThresh, *topSubtract, *verbose)
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
func probeWAV(wavPath string, rmsThresh float64, topSubtract int, verbose bool) (newMatched, newExtra, lostMatched, subtracted int) {
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

	// Pass 1.
	cands1 := candidates.Find(data.Samples)
	records1 := decodeAll(data.Samples, cands1)
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
		if !subtractInPlace(residual, c) {
			continue
		}
		subtracted++
	}

	// Pass 2 on residual.
	cands2 := candidates.Find(residual)
	records2 := decodeAll(residual, cands2)
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
func decodeAll(samples []float32, cands []candidates.Candidate) []decoded {
	var out []decoded
	for _, c := range cands {
		energies := demod.Demod(samples, c.Freq, c.DT)
		llrs := demod.LLRs(energies)
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
