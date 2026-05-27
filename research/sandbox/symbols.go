package sandbox

import (
	"fmt"
	"math"

	"github.com/ColonelBlimp/station-manager/research/sandbox/pfft"
)

const (
	// ft8SymbolCount is the symbol count of an FT8 message:
	// 7 Costas + 29 data + 7 Costas + 29 data + 7 Costas = 79.
	ft8SymbolCount = 79

	// ft8TonesPerSymbol is the 8-FSK alphabet size — each symbol
	// carries one of 8 tones spaced at 6.25 Hz.
	ft8TonesPerSymbol = 8
)

// SymbolGrid is the symbol-domain output of a 32-point complex FFT
// per FT8 symbol, restricted to the 8 in-band tone bins.
//
//	Tones[s][m] = |FFT[s][m]|² (power of tone m at symbol s)
//	Amps[s][m]  = FFT[s][m]   (complex amplitude of tone m at symbol s)
//
// At refineBandwidthHz=200 Hz complex sample rate and 32-sample
// symbols, the 32-point FFT bin spacing is exactly 6.25 Hz — one FT8
// tone per bin — so bin m maps directly to tone m for m in [0, 8).
// Bins 8..31 carry out-of-band content (negative-frequency wrap and
// the residual high-positive-frequency tail) and are dropped.
//
// Both Tones and Amps are retained: Tones supports the
// argmax-style hard-decision Costas sync gate, Amps preserves
// phase for later soft demodulation.
type SymbolGrid struct {
	Tones [ft8SymbolCount][ft8TonesPerSymbol]float64
	Amps  [ft8SymbolCount][ft8TonesPerSymbol]complex128
}

// ExtractSymbols computes the SymbolGrid for a refined candidate.
//
// Steps:
//
//  1. Channelize at the candidate's refined freq (200 Hz bandwidth →
//     3200 baseband samples @ 200 Hz complex, 32 samples/symbol).
//  2. Compute the symbol-0 start offset as
//     round((c.DtSec + nominalStartSec) × bbRate). Slice 79
//     consecutive 32-sample symbols from there.
//  3. For each symbol, run a 32-point complex FFT and keep bins 0..7
//     (the 8 FT8 tones).
//
// Returns an error if the channelizer fails or the candidate's dt
// places any symbol outside the 3200-sample baseband. The supplied
// channelizer must already have had Prepare called on the source
// audio.
//
// Plan-per-call is used for the 32-point FFT; at N=32 the CGO entry
// cost dominates the FFT work, but the absolute scale (~200 ns/call)
// is too small to be worth caching until profiling identifies it as
// a hot spot.
func ExtractSymbols(ch *Channelizer, c Candidate) (*SymbolGrid, error) {
	bb, err := ch.Extract(c.FreqHz, refineBandwidthHz)
	if err != nil {
		return nil, fmt.Errorf("ExtractSymbols: channelize: %w", err)
	}
	bbRate := refineBandwidthHz

	startSample := int(math.Round((c.DtSec + nominalStartSec) * bbRate))
	endSample := startSample + ft8SymbolCount*refineSymbolSamples
	if startSample < 0 || endSample > len(bb) {
		return nil, fmt.Errorf("ExtractSymbols: dt=%.3f s places symbols out of bounds (start=%d, end=%d, baseband=%d)",
			c.DtSec, startSample, endSample, len(bb))
	}

	plan, err := pfft.NewComplexPlan(refineSymbolSamples)
	if err != nil {
		return nil, fmt.Errorf("ExtractSymbols: plan: %w", err)
	}
	defer plan.Close()

	grid := &SymbolGrid{}
	work := make([]complex128, refineSymbolSamples)
	for s := 0; s < ft8SymbolCount; s++ {
		symStart := startSample + s*refineSymbolSamples
		copy(work, bb[symStart:symStart+refineSymbolSamples])
		if err := plan.Forward(work); err != nil {
			return nil, fmt.Errorf("ExtractSymbols: FFT(symbol %d): %w", s, err)
		}
		for m := 0; m < ft8TonesPerSymbol; m++ {
			grid.Amps[s][m] = work[m]
			re, im := real(work[m]), imag(work[m])
			grid.Tones[s][m] = re*re + im*im
		}
	}
	return grid, nil
}

// HardSyncScore counts the Costas anchor symbols where the strongest
// of the 8 in-band tones in the grid equals the expected Costas tone.
// Maximum possible score is 21 (3 blocks × 7 symbols each).
//
// This is the post-refinement "is this candidate worth feeding to
// the decoder?" gate per the QEX paper §3 / WSJT-X convention: truth
// candidates should produce scores near 21 on clean fixtures; spurious
// candidates (matched-filter aliases, noise peaks) drop sharply
// because their Costas positions don't align with strong tones in
// the underlying audio.
//
// Decision rule is plain argmax per Costas symbol — no soft info,
// no normalisation. Score is integer-valued and directly interpretable.
func HardSyncScore(grid *SymbolGrid) int {
	n := 0
	for _, blockStart := range costasBlockStarts {
		for k := 0; k < 7; k++ {
			s := blockStart + k
			expected := costasArray[k]
			maxTone := 0
			maxP := grid.Tones[s][0]
			for m := 1; m < ft8TonesPerSymbol; m++ {
				if grid.Tones[s][m] > maxP {
					maxP = grid.Tones[s][m]
					maxTone = m
				}
			}
			if maxTone == expected {
				n++
			}
		}
	}
	return n
}
