package cmd

import (
	"fmt"
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
	"github.com/spf13/cobra"
)

var decodeFlags struct {
	input         string
	maxCandidates int
	maxIterations int
	showAll       bool
	diagnose      bool
}

var decodeCmd = &cobra.Command{
	Use:   "decode",
	Short: "Stage 4: Full decode pipeline — WAV to messages",
	Long: `Reads a 12 kHz mono WAV file and runs the complete FT8 decode pipeline:

  1. Spectrogram  (SpectrogramFT8 — 3840-pt FFT, quarter-symbol step)
  2. Candidates   (FindCandidates — Costas sync correlation)
  3. Refine       (RefineCandidateAudio — Goertzel fine time/freq)
  4. Demodulate   (DemodulateAudio — 174 soft LLR values)
  5. LDPC decode  (codec.DecodeMessage — belief propagation + CRC-14)
  6. Unpack       (message.Unpack — 77-bit payload to callsigns/grid)

Use --diagnose for detailed per-candidate diagnostics showing where
each candidate succeeds or fails in the pipeline.

Comparable to WSJT-X's sync8 → ft8b → ft8_decode pipeline.`,
	Example: `  ft8test decode --input capture.wav
  ft8test decode --input capture.wav --diagnose
  ft8test decode --input capture.wav --show-all
  ft8test decode --input capture.wav --max-candidates 200 --max-iterations 50`,
	RunE: runDecode,
}

func init() {
	decodeCmd.Flags().StringVar(&decodeFlags.input, "input", "capture.wav",
		"input WAV file (12 kHz mono PCM)")
	decodeCmd.Flags().IntVar(&decodeFlags.maxCandidates, "max-candidates", 120,
		"maximum number of sync candidates to evaluate")
	decodeCmd.Flags().IntVar(&decodeFlags.maxIterations, "max-iterations", 40,
		"maximum LDPC belief-propagation iterations per candidate")
	decodeCmd.Flags().BoolVar(&decodeFlags.showAll, "show-all", false,
		"show all candidates including LDPC failures")
	decodeCmd.Flags().BoolVar(&decodeFlags.diagnose, "diagnose", false,
		"show detailed per-candidate diagnostics (refine, LLR stats, LDPC)")
	rootCmd.AddCommand(decodeCmd)
}

func runDecode(_ *cobra.Command, _ []string) error {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Stage 4: Full Decode Pipeline")
	fmt.Printf("  Input: %s\n", decodeFlags.input)
	fmt.Printf("  Max candidates: %d  |  Max LDPC iterations: %d\n",
		decodeFlags.maxCandidates, decodeFlags.maxIterations)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Load WAV.
	samples, sampleRate, err := readWAV(decodeFlags.input)
	if err != nil {
		return fmt.Errorf("read WAV: %w", err)
	}
	fmt.Printf("  WAV: %d samples, %d Hz, %.2f s\n",
		len(samples), sampleRate, float64(len(samples))/float64(sampleRate))
	if sampleRate != dsp.SampleRate {
		fmt.Printf("  ⚠  Expected %d Hz, got %d Hz\n", dsp.SampleRate, sampleRate)
	}
	fmt.Println()

	// Step 1: Spectrogram.
	fmt.Println("  ── Step 1: Spectrogram ──")
	sg := dsp.SpectrogramFT8(samples)
	if sg == nil {
		fmt.Println("  ❌ SpectrogramFT8 returned nil")
		return nil
	}
	fmt.Printf("  %d frames × %d bins\n", len(sg), len(sg[0]))
	fmt.Println()

	// Step 2: Candidate detection.
	fmt.Println("  ── Step 2: Candidate detection ──")
	const stepsPerSymbol = 4
	candidates := dsp.FindCandidates(sg, decodeFlags.maxCandidates, stepsPerSymbol)
	fmt.Printf("  %d candidates found\n", len(candidates))
	if len(candidates) == 0 {
		fmt.Println("  ❌ No sync candidates — nothing to decode")
		return nil
	}
	fmt.Println()

	// Steps 3–6: Refine, demodulate, LDPC decode, unpack — per candidate.
	fmt.Println("  ── Steps 3–6: Refine → Demodulate → LDPC → Unpack ──")
	fmt.Println()

	hann := dsp.HannCoefficients(dsp.SamplesPerSymbol)
	seen := make(map[[10]byte]struct{})

	var decoded, ldpcFail, dupCount int

	type result struct {
		rank    int
		freq    float32
		timeOff float32
		snr     float32
		text    string
	}
	var results []result

	noiseFloor := estimateNoiseFloorSimple(sg)

	for i := range candidates {
		cand := &candidates[i]
		diag := decodeFlags.diagnose

		if diag {
			fmt.Printf("  [%3d] Coarse: %7.1f Hz  t=%.3f s  score=%.2f\n",
				i+1, cand.Freq, cand.TimeOff, cand.Score)
		}

		// Step 3: Refine.
		refined := dsp.RefineCandidateAudio(samples, hann, *cand)

		if diag {
			fmt.Printf("        Refined: %7.1f Hz  t=%.3f s  score=%.2f",
				refined.Freq, refined.TimeOff, refined.Score)
			freqShift := refined.Freq - cand.Freq
			timeShift := refined.TimeOff - cand.TimeOff
			fmt.Printf("  (Δf=%+.1f Hz  Δt=%+.3f s)\n", freqShift, timeShift)
		}

		// Step 4: Demodulate.
		llr := dsp.DemodulateAudio(samples, hann, refined)
		dsp.NormalizeLLR(&llr)

		if diag {
			// LLR statistics.
			var llrMin, llrMax float32
			var llrSum, llrSumSq float64
			llrMin = llr[0]
			llrMax = llr[0]
			var zeroCount int
			for _, v := range llr {
				if v < llrMin {
					llrMin = v
				}
				if v > llrMax {
					llrMax = v
				}
				llrSum += float64(v)
				llrSumSq += float64(v) * float64(v)
				if v == 0 {
					zeroCount++
				}
			}
			llrMean := llrSum / float64(len(llr))
			llrVar := llrSumSq/float64(len(llr)) - llrMean*llrMean
			fmt.Printf("        LLR: min=%.1f max=%.1f mean=%.2f var=%.2f zeros=%d\n",
				llrMin, llrMax, llrMean, llrVar, zeroCount)
		}

		// Step 5: LDPC decode + CRC-14.
		msg77, ok := codec.DecodeMessage(llr, decodeFlags.maxIterations)
		if !ok {
			ldpcFail++
			if diag {
				fmt.Printf("        ❌ LDPC/CRC fail\n\n")
			} else if decodeFlags.showAll {
				fmt.Printf("  [%3d] %8.1f Hz  t=%+.3f s  score=%.2f  ❌ LDPC/CRC fail\n",
					i+1, refined.Freq, refined.TimeOff, refined.Score)
			}
			continue
		}

		// Deduplicate.
		msg77[9] &= 0xF8
		if _, dup := seen[msg77]; dup {
			dupCount++
			if diag {
				fmt.Printf("        ⊘ Duplicate (already decoded)\n\n")
			}
			continue
		}
		seen[msg77] = struct{}{}

		// Step 6: Unpack.
		decoded++
		snr := estimateSNRSimple(refined.Score, noiseFloor)

		msg, unpackErr := message.Unpack(msg77)
		var text string
		if unpackErr != nil {
			text = fmt.Sprintf("(unpack error: %v)", unpackErr)
		} else {
			text = msg.String()
		}

		if diag {
			fmt.Printf("        ✓ Decoded: %s  (SNR %+.1f)\n\n", text, snr)
		}

		results = append(results, result{
			rank:    i + 1,
			freq:    refined.Freq,
			timeOff: refined.TimeOff,
			snr:     snr,
			text:    text,
		})
	}

	// Print decoded messages.
	if len(results) > 0 {
		fmt.Println()
		fmt.Printf("  %-5s %-9s %6s %8s  %s\n", "RANK", "TIME (s)", "SNR", "FREQ", "MESSAGE")
		fmt.Println("  ───── ───────── ────── ────────  ──────────────────────────────────")
		for _, r := range results {
			fmt.Printf("  %-5d %+8.3f  %+5.1f  %7.1f  %s\n",
				r.rank, r.timeOff, r.snr, r.freq, r.text)
		}
	}

	// Summary.
	fmt.Println()
	fmt.Println("  ── Summary ──")
	fmt.Printf("  Candidates evaluated : %d\n", len(candidates))
	fmt.Printf("  LDPC+CRC pass        : %d\n", decoded)
	fmt.Printf("  LDPC+CRC fail        : %d\n", ldpcFail)
	fmt.Printf("  Duplicates removed   : %d\n", dupCount)
	fmt.Printf("  Unique messages      : %d\n", len(results))
	fmt.Println()

	if decoded == 0 {
		fmt.Println("  ❌ No messages decoded. Candidates were found but LDPC")
		fmt.Println("     decode failed on all of them. Try --diagnose to see details.")
	} else {
		fmt.Printf("  ✓ %d message(s) decoded\n", len(results))
	}

	return nil
}

// estimateNoiseFloorSimple computes the mean spectrogram power as a simple
// noise floor estimate (matches dsp.estimateNoiseFloor).
func estimateNoiseFloorSimple(sg [][]float32) float64 {
	var sum float64
	var count int
	for _, row := range sg {
		for _, v := range row {
			sum += float64(v)
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// estimateSNRSimple produces a rough dB SNR from sync score and noise floor
// (matches dsp.estimateSNR).
func estimateSNRSimple(score float32, noiseFloor float64) float32 {
	s := float64(score)
	if s <= 0 {
		return -30.0
	}
	if noiseFloor > 0 {
		return float32(10.0 * math.Log10(s/noiseFloor))
	}
	return float32(10.0 * math.Log10(s))
}
