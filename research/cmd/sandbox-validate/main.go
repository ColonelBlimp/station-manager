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
//
// -peaks prints the top 10 baseband FFT bins per signal for visual
// inspection of the 8-FSK tone pattern (tones live at multiples of
// 6.25 Hz = 100 baseband-FFT bins).
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
