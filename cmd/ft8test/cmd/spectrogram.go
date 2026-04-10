package cmd

import (
	"fmt"
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/spf13/cobra"
)

var spectrogramFlags struct {
	input string
}

var spectrogramCmd = &cobra.Command{
	Use:   "spectrogram",
	Short: "Stage 2: Compute spectrogram from a WAV file",
	Long: `Reads a 12 kHz mono WAV file (produced by 'capture'), computes the FT8
spectrogram using WSJT-X sync8.f90 parameters, and reports diagnostics.

WSJT-X spectrogram parameters:
  NSPS  = 1920  (samples per symbol at 12 kHz)
  NFFT1 = 3840  (2 × NSPS — zero-padded FFT)
  NSTEP = 480   (NSPS/4 — quarter-symbol step)
  NH1   = 1920  (NFFT1/2 — frequency bins, excl. DC)
  NHSYM = 372   (NMAX/NSTEP - 3 — number of time steps)
  df    = 3.125 Hz per bin (12000/3840)

This validates the spectrogram/FFT stage in isolation.`,
	Example: `  ft8test spectrogram --input capture.wav`,
	RunE:    runSpectrogram,
}

func init() {
	spectrogramCmd.Flags().StringVar(&spectrogramFlags.input, "input", "capture.wav",
		"input WAV file (12 kHz mono PCM, from 'capture' stage)")
	rootCmd.AddCommand(spectrogramCmd)
}

func runSpectrogram(_ *cobra.Command, _ []string) error {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Stage 2: Spectrogram")
	fmt.Printf("  Input: %s\n", spectrogramFlags.input)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	samples, sampleRate, err := readWAV(spectrogramFlags.input)
	if err != nil {
		return fmt.Errorf("read WAV: %w", err)
	}
	fmt.Printf("  WAV: %d samples, %d Hz, %.2f s\n",
		len(samples), sampleRate, float64(len(samples))/float64(sampleRate))

	if sampleRate != dsp.SampleRate {
		fmt.Printf("  ⚠  Expected %d Hz, got %d Hz — results may be incorrect\n",
			dsp.SampleRate, sampleRate)
	}
	fmt.Println()

	// Print WSJT-X reference parameters for comparison.
	nfft1 := 2 * dsp.SamplesPerSymbol    // 3840
	nstep := dsp.SamplesPerSymbol / 4    // 480
	nh1 := nfft1 / 2                     // 1920 (WSJT-X excludes DC bin)
	nhsym := dsp.WindowSamples/nstep - 3 // 372 (WSJT-X NHSYM)
	df := float64(dsp.SampleRate) / float64(nfft1)
	binsPerTone := dsp.ToneSpacing / df

	fmt.Println("  WSJT-X reference parameters:")
	fmt.Printf("    NSPS  = %d  (samples per symbol)\n", dsp.SamplesPerSymbol)
	fmt.Printf("    NFFT1 = %d  (FFT size = 2 × NSPS)\n", nfft1)
	fmt.Printf("    NSTEP = %d  (quarter-symbol step)\n", nstep)
	fmt.Printf("    NH1   = %d  (freq bins, excl. DC)\n", nh1)
	fmt.Printf("    NHSYM = %d  (time steps for full 15 s window)\n", nhsym)
	fmt.Printf("    df    = %.3f Hz/bin\n", df)
	fmt.Printf("    Bins/tone = %.1f  (tone spacing %.2f Hz / df %.3f Hz)\n",
		binsPerTone, dsp.ToneSpacing, df)
	fmt.Println()

	// Compute spectrogram.
	fmt.Println("  Computing spectrogram...")
	sg := dsp.SpectrogramFT8(samples)
	if sg == nil {
		fmt.Println("  ❌ SpectrogramFT8 returned nil — buffer too short?")
		fmt.Printf("     Got %d samples, need at least %d (one symbol period)\n",
			len(samples), dsp.SamplesPerSymbol)
		return nil
	}

	numFrames := len(sg)
	numBins := 0
	if numFrames > 0 {
		numBins = len(sg[0])
	}

	fmt.Printf("  Result: %d frames × %d bins\n", numFrames, numBins)
	fmt.Printf("  Our nBins = NFFT1/2 + 1 = %d  (WSJT-X NH1 = %d, excludes DC)\n",
		nfft1/2+1, nh1)
	fmt.Println()

	// Compare our frame count with WSJT-X NHSYM.
	fmt.Println("  Frame count comparison:")
	fmt.Printf("    Our nFrames : %d\n", numFrames)
	fmt.Printf("    WSJT-X NHSYM: %d  (NMAX/NSTEP - 3)\n", nhsym)
	if numFrames == nhsym {
		fmt.Println("    ✓ Exact match")
	} else {
		diff := numFrames - nhsym
		fmt.Printf("    Δ = %+d frames (our formula: (N-NSPS)/NSTEP + 1)\n", diff)
		fmt.Println("    Note: small difference is expected — WSJT-X subtracts 3")
		fmt.Println("    guard frames; our formula counts all frames that fit.")
	}
	fmt.Println()

	// Power range statistics (log2 domain).
	var sgMin, sgMax float32
	var sgSum float64
	var count int
	sgMin = sg[0][0]
	sgMax = sg[0][0]
	for _, row := range sg {
		for _, v := range row {
			if v < sgMin {
				sgMin = v
			}
			if v > sgMax {
				sgMax = v
			}
			sgSum += float64(v)
			count++
		}
	}
	sgMean := sgSum / float64(count)

	fmt.Println("  Log2-power statistics:")
	fmt.Printf("    Min : %.2f\n", sgMin)
	fmt.Printf("    Max : %.2f\n", sgMax)
	fmt.Printf("    Mean: %.2f\n", sgMean)
	fmt.Printf("    Dynamic range: %.2f (max - min)\n", sgMax-sgMin)
	fmt.Println()

	// Note: WSJT-X sync8.f90 uses linear power s(i,j) = re² + im²,
	// NOT log2. Our SpectrogramFT8 uses log2(power) for sync scoring.
	// The linear-to-log2 transform preserves relative ordering of
	// candidates but changes the numeric scoring scale.
	fmt.Println("  Note: SpectrogramFT8 outputs log2(power). WSJT-X sync8.f90")
	fmt.Println("  uses linear power for sync scoring. The log2 transform is")
	fmt.Println("  applied here for robust candidate detection.")
	fmt.Println()

	// FT8 signal band check: most FT8 signals fall in 200–3000 Hz.
	// Report mean power in-band vs out-of-band as a sanity check.
	binLow := int(math.Round(200.0 / df))
	binHigh := int(math.Round(3000.0 / df))
	if binHigh >= numBins {
		binHigh = numBins - 1
	}
	var inBandSum, outBandSum float64
	var inBandN, outBandN int
	for _, row := range sg {
		for b, v := range row {
			if b >= binLow && b <= binHigh {
				inBandSum += float64(v)
				inBandN++
			} else {
				outBandSum += float64(v)
				outBandN++
			}
		}
	}
	if inBandN > 0 && outBandN > 0 {
		inMean := inBandSum / float64(inBandN)
		outMean := outBandSum / float64(outBandN)
		fmt.Printf("  Band power (200–3000 Hz): mean log2 = %.2f  (%d bins)\n", inMean, binHigh-binLow+1)
		fmt.Printf("  Out-of-band power       : mean log2 = %.2f\n", outMean)
		if inMean > outMean+1.0 {
			fmt.Println("  ✓ In-band energy is elevated — signals likely present")
		} else {
			fmt.Println("  ⚠  In-band energy similar to out-of-band — weak or no signals")
		}
	}
	fmt.Println()

	// Minimum frames check for full FT8 message.
	const stepsPerSymbol = 4
	minFrames := (dsp.NumSymbols-1)*stepsPerSymbol + 1
	if numFrames >= minFrames {
		fmt.Printf("  ✓ %d frames ≥ %d minimum for FT8 decode (79 symbols × 4 steps)\n",
			numFrames, minFrames)
	} else {
		fmt.Printf("  ❌ %d frames < %d minimum — not enough data for FT8\n",
			numFrames, minFrames)
	}
	fmt.Println()
	fmt.Println("  Next step:")
	fmt.Printf("    ft8test candidates --input %s\n", spectrogramFlags.input)

	return nil
}
