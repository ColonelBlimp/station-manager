// synth-subtract-probe is the Session-85 sanity check redone with
// sub-bin freq estimation: load a real-capture WAV, decode each of
// the top-N candidates, re-synthesise the decoded signal at its
// precisely-estimated freq + phase + amplitude, subtract from the
// audio buffer, and measure how much of the original signal's
// energy remains at the Costas-anchor expected tones.
//
// **The load-bearing measurement**: Session 85's lattice-snapped
// subtraction left ~98.6% of the energy in place (1.4% reduction)
// — that's what killed the multi-pass subtraction approach. The
// hypothesis now is that the precise-freq estimate from
// research/demod.PhaseFitFor (via the slope→df convention bridge)
// + a per-anchor complex-amplitude calibration via
// CostasAnchorAmplitudes will drive the residual energy below 10%.
// Below 10% means subtraction is viable as a research lever and
// the multi-pass loop in decode-eval becomes worth building. Above
// 10% means there's another modelling gap (likely amplitude drift
// or per-symbol phase tracking) and we need to refine before going
// further.
//
// Usage:
//
//	go run ./research/cmd/synth-subtract-probe -wav captures/live_slot1.wav
//	go run ./research/cmd/synth-subtract-probe -wav PATH -top 5 -v
//
// Output: per-candidate energy ratio + a corpus summary line.
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
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
	"github.com/ColonelBlimp/station-manager/research/ldpc"
	"github.com/ColonelBlimp/station-manager/research/synth"
)

const (
	expectedSampleRate = 12000
	codewordBits       = 174
	// tSym is the FT8 symbol period in seconds (= 1920 / 12000).
	// Used in the slope→df convention bridge.
	tSym = 1920.0 / 12000.0
)

func main() {
	wavPath := flag.String("wav", "", "path to the .wav file (required)")
	topN := flag.Int("top", 3, "how many top-scoring candidates to probe")
	verbose := flag.Bool("v", false, "print per-anchor calibration detail")
	flag.Parse()

	if *wavPath == "" {
		fmt.Fprintln(os.Stderr, "synth-subtract-probe: -wav PATH is required")
		os.Exit(2)
	}

	data, err := audio.ReadWAV(*wavPath)
	if err != nil {
		log.Fatalf("read wav %q: %v", *wavPath, err)
	}
	if data.SampleRate != expectedSampleRate || data.Channels != 1 {
		log.Fatalf("%q: rate=%d channels=%d — want %d Hz mono",
			*wavPath, data.SampleRate, data.Channels, expectedSampleRate)
	}

	fmt.Printf("=== %s (%.2f s, %d samples) ===\n",
		*wavPath, float64(len(data.Samples))/float64(data.SampleRate), len(data.Samples))

	cands := candidates.Find(data.Samples)
	if len(cands) == 0 {
		log.Fatal("no candidates found")
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	if len(cands) > *topN {
		cands = cands[:*topN]
	}

	probed := 0
	hits := 0
	for i, c := range cands {
		fmt.Printf("\n--- Candidate %d: freq=%.2f Hz, dt=%+.3f s, stage-1=%.2f ---\n",
			i+1, c.Freq, c.DT, c.Score)

		ok := probeCandidate(data.Samples, c, *verbose)
		probed++
		if ok {
			hits++
		}
	}

	fmt.Printf("\n=== probed %d / decode-and-subtract OK %d ===\n", probed, hits)
}

// probeCandidate runs the full decode→precise-freq→synth→subtract
// pipeline on one candidate, reporting energy reduction. Returns
// true iff the candidate decoded cleanly (CRC pass) AND subtraction
// drove residual energy below 10% of the original.
func probeCandidate(samples []float32, c candidates.Candidate, verbose bool) bool {
	// Decode.
	energies := demod.Demod(samples, c.Freq, c.DT)
	llrs := demod.LLRs(energies)
	var input [codewordBits]float64
	for k := 0; k < codewordBits; k++ {
		input[k] = llrs[k]
	}
	result, stats := ldpc.Decode(input)
	if !stats.ConvergedCRC {
		fmt.Println("  decode: BP+OSD-2 failed CRC — skipping")
		return false
	}
	fmt.Printf("  decode: CRC pass (iters=%d, usedOSD=%v)\n", stats.Iterations, stats.UsedOSD)

	// Precise frequency via the slope→df convention bridge.
	fit := demod.PhaseFitFor(samples, c.Freq, c.DT)
	if math.IsInf(fit.RMSResid, 1) {
		fmt.Println("  phase-fit: RMSResid = +Inf, fit unreliable — skipping")
		return false
	}
	// slope is 2π·f_signal·T_sym mod 2π. The fit measures it at f_demod
	// so slope_user = wrap(Slope - 2π·f_demod·T_sym) is 2π·(f_sig - f_demod)·T_sym.
	slopeUser := wrapPi(fit.Slope - 2*math.Pi*c.Freq*tSym)
	df := slopeUser / (2 * math.Pi * tSym)
	preciseFreq := c.Freq + df
	fmt.Printf("  phase-fit: RMSResid=%.3f rad, slope=%+.3f, df=%+.3f Hz → precise freq %.4f\n",
		fit.RMSResid, fit.Slope, df, preciseFreq)

	// Pass 1 — unit-amplitude synth at the precise freq, zero initial
	// phase. We'll use its Costas-anchor amplitudes to calibrate.
	probe := synth.Synthesize(result.Codeword, preciseFreq, c.DT, len(samples), 1.0, 0.0)

	// Measure complex Costas-anchor amplitudes in both audio and probe.
	xReal, accReal := demod.CostasAnchorAmplitudes(samples, preciseFreq, c.DT)
	xSynth, accSynth := demod.CostasAnchorAmplitudes(probe, preciseFreq, c.DT)

	// Weighted least-squares complex calibration: find c such that
	//   c × xSynth[i] ≈ xReal[i] for all accessible i.
	// Closed form: c = Σ(xReal·conj(xSynth)) / Σ|xSynth|².
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
		fmt.Printf("  calibration: only %d accessible anchors / den=%.3e — skipping\n",
			accessible, den)
		return false
	}
	cCalib := num / complex(den, 0)
	amp := cmplx.Abs(cCalib)
	phase := cmplx.Phase(cCalib)
	fmt.Printf("  calibration (over %d anchors): amp=%.4f, phase=%+.3f rad\n",
		accessible, amp, phase)

	if verbose {
		fmt.Println("  per-anchor x_real / x_synth ratio (mag, phase):")
		for i := 0; i < len(xReal); i++ {
			if !accReal[i] || !accSynth[i] {
				continue
			}
			ratio := xReal[i] / xSynth[i]
			fmt.Printf("    anchor %2d: |%.3f|, phase=%+.3f rad\n",
				i, cmplx.Abs(ratio), cmplx.Phase(ratio))
		}
	}

	// Pass 2 — calibrated synth with the recovered amplitude + phase.
	calibrated := synth.Synthesize(result.Codeword, preciseFreq, c.DT, len(samples), amp, phase)

	// Build the residual (audio - calibrated synth). Use a fresh
	// buffer; never mutate the caller's `samples`.
	residual := make([]float32, len(samples))
	for k := range samples {
		residual[k] = samples[k] - calibrated[k]
	}

	// Measure Costas-anchor energy at expected tones before vs after.
	originalEnergy := costasTotalEnergy(samples, preciseFreq, c.DT)
	residualEnergy := costasTotalEnergy(residual, preciseFreq, c.DT)
	ratio := residualEnergy / originalEnergy
	reductionPct := 100 * (1 - ratio)
	fmt.Printf("  energy at expected Costas tones:\n")
	fmt.Printf("    original: %.3e\n", originalEnergy)
	fmt.Printf("    residual: %.3e  (%.1f%% reduction)\n", residualEnergy, reductionPct)

	// Operational check: does candidates.Find still detect a candidate
	// at this position in the residual?
	resCands := candidates.Find(residual)
	stillDetected := false
	for _, rc := range resCands {
		if math.Abs(rc.Freq-c.Freq) <= 5.0 && math.Abs(rc.DT-c.DT) <= 0.3 {
			stillDetected = true
			fmt.Printf("    finder on residual: STILL DETECTED at freq=%.2f, dt=%+.3f, stage-1=%.2f\n",
				rc.Freq, rc.DT, rc.Score)
			break
		}
	}
	if !stillDetected {
		fmt.Println("    finder on residual: signal gone — subtraction succeeded operationally")
	}

	return ratio < 0.1
}

// costasTotalEnergy returns the sum of |X(f_expected)|² across the
// 21 Costas anchors of the signal at (freqHz, dtSec), computed via
// research/demod's complex Goertzel helper. The probe uses this
// before/after subtraction to quantify residual energy.
func costasTotalEnergy(samples []float32, freqHz, dtSec float64) float64 {
	amps, accessible := demod.CostasAnchorAmplitudes(samples, freqHz, dtSec)
	var total float64
	for i := range amps {
		if !accessible[i] {
			continue
		}
		total += real(amps[i])*real(amps[i]) + imag(amps[i])*imag(amps[i])
	}
	return total
}

// wrapPi wraps x to the (-π, +π] half-open interval. Used in the
// slope→df bridge so the inferred frequency offset is the smallest-
// magnitude consistent value.
func wrapPi(x float64) float64 {
	for x > math.Pi {
		x -= 2 * math.Pi
	}
	for x <= -math.Pi {
		x += 2 * math.Pi
	}
	return x
}
