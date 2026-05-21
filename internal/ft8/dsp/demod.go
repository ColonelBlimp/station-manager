package dsp

import (
	"math"
	"math/cmplx"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// Soft demodulator producing 174 log-likelihood ratios (LLRs) from
// 200-Hz complex baseband. Implements the L_j formula from Taylor
// 2020 §6 ("Soft demapping") plus the Gray mapping from §4
// (Table 3).
//
// **Spec-level inputs (QEX paper):**
//
//   - 8-FSK modulation, 3 bits per channel symbol (§4).
//   - 79 channel symbols per transmission; 58 data symbols at
//     indices 7..35 and 43..71 (the slots between the start/middle
//     and middle/end Costas arrays) (§4).
//   - Gray mapping `{0,1,3,2,5,6,4,7}` between binary index and
//     tone number (§4 Table 3, == GrayMap in params.go).
//   - L_j soft-demap formula (§6):
//     L_j = K · (max{|C_i| : x_j=1} − max{|C_i| : x_j=0})
//
// **LLR sign convention.** Our LDPCDecode uses the LDPC-literature
// convention: positive LLR ⟹ bit-is-0. The paper's formula gives
// positive when bit-is-1 is favoured. We negate the formula here so
// our demodulator output feeds LDPCDecode without a sign swap:
//
//	L_j = K · (max{|C_i| : x_j=0} − max{|C_i| : x_j=1})
//	     (positive ⟹ bit-is-0)
//
// **K scaling.** Per the paper, K is "adjusted empirically".
// First-cut uses K=1.0 — fine for sensitivity testing; LDPC BP is
// somewhat tolerant of LLR scaling at the price of slightly more
// iterations on marginal signals. Tune later if needed.
//
// **Symbol extraction.** At 200 Hz baseband, each FT8 channel
// symbol spans NSPS2 = 32 samples (NSPS/NDOWN = 1920/60). A 32-
// point FFT per symbol gives 32 bins at 200/32 = 6.25 Hz spacing
// — exactly one bin per FSK tone. The 8 useful tones land at FFT
// bins 0..7; bins 8..31 are out-of-band and ignored.

const (
	// DemodSymbols is the count of FT8 data symbols per
	// transmission. 58 = NN (79 total) − 21 sync (3 Costas blocks
	// × 7 symbols each).
	DemodSymbols = 58

	// DemodBitsPerSymbol is the 8-FSK bit count per symbol.
	DemodBitsPerSymbol = 3

	// DemodLLRCount is the total LLR output length: 58 symbols ×
	// 3 bits = 174, exactly matching the LDPC(174,91) codeword
	// length so the output feeds LDPCDecode directly.
	DemodLLRCount = DemodSymbols * DemodBitsPerSymbol

	// SymbolFFTSize is the per-symbol FFT length at baseband.
	// NSPS / NDOWN = 1920 / 60 = 32 samples per symbol at 200 Hz.
	SymbolFFTSize = NSPS / NDOWN

	// SymbolDataStart1 / End1 / Start2 / End2 mark the channel-
	// symbol index ranges that carry data (i.e. NOT Costas sync).
	// Block 1 data: symbols 7..35 (29 symbols).
	// Block 2 data: symbols 43..71 (29 symbols).
	SymbolDataStart1 = 7
	SymbolDataEnd1   = 35 // inclusive
	SymbolDataStart2 = 43
	SymbolDataEnd2   = 71 // inclusive

	// DemodScale is the empirical K constant from Taylor 2020 §6.
	// First-cut value 1.0 — tune later if LDPC sensitivity needs it.
	DemodScale = 1.0
)

// Demodulate computes 174 LLRs for one Sync candidate from its
// 200-Hz complex baseband signal. The output feeds directly into
// LDPCDecode (matching LDPC-literature sign convention: positive
// ⟹ bit-is-0).
//
// baseband: complex samples at Fs2=200 Hz from Downsample. Must
// be at least long enough to cover all 58 data symbols at the
// requested time offset.
//
// dt: candidate time offset in seconds from nominal TX start
// (Candidate.DT from Sync). 0 = perfectly aligned; positive =
// signal arrived late, negative = early.
//
// Returns a fresh 174-element LLR slice. Returns nil if the
// baseband is too short for all symbol extractions at the given
// dt (a programmer-side bug — the upstream pipeline should never
// hand a too-short baseband to the demodulator).
//
// Builds an ad-hoc audio.Plan internally for the 58 per-symbol
// FFTs of size SymbolFFTSize=32. Callers running many Demodulate
// calls per slot (fine-timing retries × many candidates) should
// use DemodulateWithPlan with a shared Plan to amortise the
// twiddle-table construction.
func Demodulate(baseband []complex128, dt float64) []float64 {
	return DemodulateWithPlan(baseband, dt, nil)
}

// DemodulateWithPlan is the Plan-reuse variant of Demodulate. If
// symPlan is non-nil it must have been constructed for size
// SymbolFFTSize (Plan.FFT panics on a size mismatch). If nil, an
// ad-hoc plan is built per call.
func DemodulateWithPlan(baseband []complex128, dt float64, symPlan *audio.Plan) []float64 {
	// Symbol start samples in baseband: each channel symbol n
	// begins at sample nominalStart + dtSamples + n*SymbolFFTSize.
	const nominalStartSamples = int(nominalTXStartSeconds * Fs2) // 100
	dtSamples := int(math.Round(dt * Fs2))

	out := make([]float64, DemodLLRCount)
	llrIdx := 0

	// Iterate the two data-symbol ranges. Channel-symbol index ↔
	// LLR triplet: first data symbol → LLRs [0..2], second → [3..5],
	// etc.
	for _, span := range [2][2]int{
		{SymbolDataStart1, SymbolDataEnd1},
		{SymbolDataStart2, SymbolDataEnd2},
	} {
		for chanSym := span[0]; chanSym <= span[1]; chanSym++ {
			symStart := nominalStartSamples + dtSamples + chanSym*SymbolFFTSize
			if symStart < 0 || symStart+SymbolFFTSize > len(baseband) {
				return nil
			}

			// Per-symbol FFT.
			window := make([]complex128, SymbolFFTSize)
			copy(window, baseband[symStart:symStart+SymbolFFTSize])
			var spectrum []complex128
			if symPlan != nil {
				spectrum = symPlan.FFT(window)
			} else {
				spectrum = audio.FFT(window)
			}

			// Tone magnitudes at bins 0..7 (one per FSK tone).
			var mag [TonesPerSymbol]float64
			for tone := 0; tone < TonesPerSymbol; tone++ {
				mag[tone] = cmplx.Abs(spectrum[tone])
			}

			// Per-bit max-tone reduction.
			// For each bit position j in 0..2 (j=0 is MSB per
			// codeword-bit-order convention):
			//   max0 = max over tones t where GrayUnmap[t] has bit (2-j) clear
			//   max1 = max over tones t where GrayUnmap[t] has bit (2-j) set
			//   LLR_j = K · (max0 − max1)
			// The (2-j) inversion maps "bit j of the triplet" (MSB-
			// first as the LDPC codeword orders them) to "bit
			// position in the 3-bit GrayUnmap value" (LSB-numbered
			// internally).
			for j := 0; j < DemodBitsPerSymbol; j++ {
				bitMask := uint8(1) << (DemodBitsPerSymbol - 1 - j) // 4, 2, 1 for j=0,1,2
				var max0, max1 float64
				for tone := 0; tone < TonesPerSymbol; tone++ {
					if GrayUnmap[tone]&bitMask != 0 {
						if mag[tone] > max1 {
							max1 = mag[tone]
						}
					} else {
						if mag[tone] > max0 {
							max0 = mag[tone]
						}
					}
				}
				out[llrIdx] = DemodScale * (max0 - max1)
				llrIdx++
			}
		}
	}

	return out
}
