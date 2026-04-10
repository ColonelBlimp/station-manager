package cmd

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/spf13/cobra"
)

var candidatesFlags struct {
	input         string
	maxCandidates int
}

var candidatesCmd = &cobra.Command{
	Use:   "candidates",
	Short: "Stage 3: Detect Costas sync candidates from a WAV file",
	Long: `Reads a 12 kHz mono WAV file, computes the FT8 spectrogram, and runs
Costas sync correlation to detect candidate signals.

This mirrors WSJT-X sync8.f90's candidate detection:
  - Spectrogram: 3840-pt FFT, quarter-symbol step (NSTEP=480)
  - Sync scoring: correlate 3×7 Costas pattern {3,1,4,0,6,5,2}
  - Bins per tone: 2 (nfos = NFFT1/NSPS)
  - Search band: 200–3000 Hz

Reports the top candidates sorted by sync score, with frequency and
time offset — comparable to sync8.f90's candidate0 output.`,
	Example: `  ft8test candidates --input capture.wav
  ft8test candidates --input capture.wav --max-candidates 200`,
	RunE: runCandidates,
}

func init() {
	candidatesCmd.Flags().StringVar(&candidatesFlags.input, "input", "capture.wav",
		"input WAV file (12 kHz mono PCM)")
	candidatesCmd.Flags().IntVar(&candidatesFlags.maxCandidates, "max-candidates", 120,
		"maximum number of candidates to return")
	rootCmd.AddCommand(candidatesCmd)
}

func runCandidates(_ *cobra.Command, _ []string) error {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Stage 3: Costas Sync Candidate Detection")
	fmt.Printf("  Input: %s  |  Max candidates: %d\n",
		candidatesFlags.input, candidatesFlags.maxCandidates)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	samples, sampleRate, err := readWAV(candidatesFlags.input)
	if err != nil {
		return fmt.Errorf("read WAV: %w", err)
	}
	fmt.Printf("  WAV: %d samples, %d Hz, %.2f s\n",
		len(samples), sampleRate, float64(len(samples))/float64(sampleRate))

	if sampleRate != dsp.SampleRate {
		fmt.Printf("  ⚠  Expected %d Hz, got %d Hz\n", dsp.SampleRate, sampleRate)
	}
	fmt.Println()

	// Stage 2: spectrogram.
	fmt.Println("  Computing spectrogram...")
	sg := dsp.SpectrogramFT8(samples)
	if sg == nil {
		fmt.Println("  ❌ SpectrogramFT8 returned nil — buffer too short")
		return nil
	}
	fmt.Printf("  Spectrogram: %d frames × %d bins\n", len(sg), len(sg[0]))
	fmt.Println()

	// Stage 3: candidate detection.
	const stepsPerSymbol = 4
	fmt.Printf("  Running Costas sync correlation (stepsPerSymbol=%d)...\n", stepsPerSymbol)
	candidates := dsp.FindCandidates(sg, candidatesFlags.maxCandidates, stepsPerSymbol)
	fmt.Printf("  Candidates found: %d (max %d)\n", len(candidates), candidatesFlags.maxCandidates)
	fmt.Println()

	if len(candidates) == 0 {
		fmt.Println("  ❌ No sync candidates found. Possible causes:")
		fmt.Println("     - No FT8 signals in the recording")
		fmt.Println("     - Audio level too low or too high")
		fmt.Println("     - Wrong sample rate or frequency band")
		return nil
	}

	// Print all candidates in a table matching WSJT-X sync8.f90 output style:
	// frequency (Hz), time offset (s), sync score.
	fmt.Printf("  %-5s  %9s  %9s  %9s\n", "RANK", "FREQ (Hz)", "TIME (s)", "SCORE")
	fmt.Println("  ─────  ─────────  ─────────  ─────────")
	for i, c := range candidates {
		fmt.Printf("  %-5d  %9.1f  %9.3f  %9.2f\n",
			i+1, c.Freq, c.TimeOff, c.Score)
	}
	fmt.Println()

	// Summary: frequency range of candidates.
	var minFreq, maxFreq float32
	minFreq = candidates[0].Freq
	maxFreq = candidates[0].Freq
	for _, c := range candidates {
		if c.Freq < minFreq {
			minFreq = c.Freq
		}
		if c.Freq > maxFreq {
			maxFreq = c.Freq
		}
	}
	fmt.Printf("  Candidate freq range: %.1f – %.1f Hz\n", minFreq, maxFreq)
	fmt.Printf("  Top score: %.2f  |  Bottom score: %.2f\n",
		candidates[0].Score, candidates[len(candidates)-1].Score)
	fmt.Println()
	fmt.Println("  Next step:")
	fmt.Printf("    ft8test decode --input %s\n", candidatesFlags.input)

	return nil
}
