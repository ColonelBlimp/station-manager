// Package demod is the research-stage FT8 symbol demodulator. Demod
// takes a 12 kHz mono FT8 audio buffer and a candidate's physical
// (freqHz, dtSec) and returns the 58 data-symbol × 8-tone energy
// matrix — the raw material the downstream LLR / LDPC / CRC stages
// will consume.
//
// Imports stdlib + internal/audio only — by rule the research tree
// must not depend on internal/ft8/*. FT8 protocol constants are
// re-declared here.
//
// This first cut is INCOHERENT: per-symbol Goertzel on |X|² with no
// phase tracking across symbols. The coherent variant (preserving
// complex amplitude phase across the 58 data symbols, expected
// ~2-3 dB gain at the marginal SNR end) is planned as the first
// SNR-improvement lever once end-to-end LLR → LDPC → CRC plumbing is
// in place and we can measure decode-parity rather than just
// per-symbol cleanliness.
package demod

import "math"

// FT8 protocol parameters per the QEX 2020 paper §4. Replicated here
// because the research tree is firewalled from internal/ft8/*.
const (
	fs   = 12000.0   // sample rate
	nsps = 1920      // samples per symbol (160 ms at fs)
	nn   = 79        // total channel symbols per transmission
	baud = fs / nsps // 6.25 Hz — symbol rate and 8-FSK tone spacing

	// ft8ToneCount is the 8-FSK alphabet size (tones 0..7).
	ft8ToneCount = 8

	// dataSymbolCount is the number of non-Costas channel symbols
	// carrying the 174 LDPC-coded bits (58 × 3 bits/symbol = 174).
	dataSymbolCount = 58

	// Costas-array layout per QEX paper §4 — three blocks of seven
	// known-tone symbols at fixed channel-symbol positions 0..6,
	// 36..42, 72..78.
	numCostasBlocks       = 3
	costasSymbolsPerBlock = 7
	costasBlockStride     = 36

	// synthSlotStartSec is the FT8 nominal TX start offset within the
	// 15-second slot, per QEX paper §4.
	synthSlotStartSec = 0.5
)

// Demod returns the per-symbol 8-tone energies for the 58 data
// symbols of an FT8 transmission at the given candidate position.
// out[i][j] = |X(freqHz + j·baud)|² over NSPS samples at data-symbol
// index i (0..57). Data symbols are the non-Costas symbols of the 79
// channel-symbol layout — i.e. channel symbols 7..35 and 43..71.
//
// (freqHz, dtSec) is the PHYSICAL-frame candidate position as
// produced by research/candidates: dtSec is the offset from the
// nominal 0.5 s TX start, so a physically-on-time signal is dtSec = 0.
//
// Slot-edge handling: any data symbol whose NSPS-sample window falls
// outside samples has its row left at zero. The caller can detect
// this by summing the 8 tones for that row — a row that is exactly
// zero never had its symbol window read. Returns the all-zero matrix
// when no data symbol fits (e.g. caller passed a buffer that is too
// short or dtSec that lies far outside the slot).
func Demod(samples []float32, freqHz, dtSec float64) [dataSymbolCount][ft8ToneCount]float64 {
	var out [dataSymbolCount][ft8ToneCount]float64

	// Precompute Goertzel coefficients c_k = 2·cos(2π·f_k/Fs) for
	// the 8 FT8 tones. Same coefficients across all 58 symbols —
	// only the audio window slides.
	var coeffs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		fk := freqHz + float64(k)*baud
		coeffs[k] = 2 * math.Cos(2*math.Pi*fk/fs)
	}

	txStart := int(math.Round((synthSlotStartSec + dtSec) * fs))

	dataIdx := 0
	for channelSym := 0; channelSym < nn; channelSym++ {
		if isCostas(channelSym) {
			continue
		}
		symStart := txStart + channelSym*nsps
		if symStart >= 0 && symStart+nsps <= len(samples) {
			out[dataIdx] = goertzelMulti(samples, symStart, nsps, coeffs)
		}
		dataIdx++
	}
	return out
}

// isCostas reports whether channelSym (0..78) is one of the 21
// Costas sync positions (0..6, 36..42, 72..78).
func isCostas(channelSym int) bool {
	if channelSym < costasSymbolsPerBlock {
		return true
	}
	if channelSym >= costasBlockStride && channelSym < costasBlockStride+costasSymbolsPerBlock {
		return true
	}
	if channelSym >= 2*costasBlockStride && channelSym < 2*costasBlockStride+costasSymbolsPerBlock {
		return true
	}
	return false
}
