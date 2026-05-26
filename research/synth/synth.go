// Package synth generates FT8 audio waveforms from a 174-bit LDPC
// codeword, a centre frequency, and a TX-start DT offset. Used by
// the research-stage subtraction experiment: a successfully decoded
// signal can be re-synthesised and subtracted from the audio buffer,
// exposing previously-masked weaker signals in the residual.
//
// Clean-room from QEX paper §3 (Franke/Somerville/Taylor, "The FT4
// and FT8 Communication Protocols," QEX July/August 2020). NOT
// translated from WSJT-X Fortran (GPL) and NOT translated from
// kgoba/ft8_lib (MIT, but operator directive 2026-05-26 keeps
// third-party FT8 implementations out of the research tree for now).
//
// Imports stdlib only — by rule the research tree must not depend
// on internal/ft8/*. FT8 protocol constants are re-declared here.
package synth

import (
	"math"
)

// FT8 protocol parameters per the QEX 2020 paper §3-4. Replicated
// here because the research tree is firewalled from internal/ft8/*.
const (
	fs   = 12000.0 // sample rate (Hz)
	nsps = 1920    // samples per symbol (= T_sym × fs; T_sym = 0.16 s)
	nn   = 79      // total channel symbols per transmission

	// baud is the symbol rate AND the 8-FSK tone spacing in Hz.
	// At T_sym = 0.16 s, this is 6.25 Hz between adjacent tones.
	baud = fs / nsps

	// ft8ToneCount is the 8-FSK alphabet (tones 0..7).
	ft8ToneCount = 8

	// bitsPerSymbol = log2(ft8ToneCount). Each data symbol carries 3
	// LDPC-coded bits, gray-mapped to one of 8 tones.
	bitsPerSymbol = 3

	// codewordBits is the FT8 LDPC codeword length: 58 data symbols ×
	// 3 bits per symbol.
	codewordBits = 174

	// dataSymbolCount = codewordBits / bitsPerSymbol.
	dataSymbolCount = 58

	// Costas-array layout per QEX paper §4 — three blocks of seven
	// known-tone symbols at fixed channel-symbol positions 0..6,
	// 36..42, 72..78.
	numCostasBlocks       = 3
	costasSymbolsPerBlock = 7
	costasBlockStride     = 36

	// gfskBT is the bandwidth-time product of the Gaussian frequency-
	// shaping filter per QEX paper §3. BT=2.0 gives a relatively
	// localized pulse — most energy in ±1 symbol either side, with
	// negligible tail past ±2 symbols.
	gfskBT = 2.0

	// pulseHalfSpanSym is how many symbols either side of a symbol's
	// centre the GFSK pulse extends in our discretization. ±2 symbols
	// captures essentially all the energy at BT=2; truncation beyond
	// this contributes ≤ 1e-4 relative amplitude.
	pulseHalfSpanSym = 2

	// synthSlotStartSec is the FT8 nominal TX start offset within the
	// 15-second slot.
	synthSlotStartSec = 0.5
)

// icos7 is the FT8 7-tone Costas synchronisation pattern. The same
// sequence appears three times in every transmission: channel
// symbols 0-6, 36-42, 72-78.
var icos7 = [costasSymbolsPerBlock]uint8{3, 1, 4, 0, 6, 5, 2}

// grayUnmap maps tone index 0..7 to its 3-bit value per QEX paper
// Table 3. Identical convention to research/demod.GrayUnmap —
// re-declared here to keep the synth package stdlib-only. Source-
// of-truth is QEX Table 3 (NOT the standard reflected gray code;
// FT8's tones 4-7 use a different bit assignment).
var grayUnmap = [ft8ToneCount]uint8{0, 1, 3, 2, 6, 4, 5, 7}

// grayMap is the inverse of grayUnmap: it maps a 3-bit codeword
// value back to the tone index that encodes it. Built at init() so
// we don't search grayUnmap every time we encode a data symbol.
var grayMap [ft8ToneCount]uint8

func init() {
	for tone := 0; tone < ft8ToneCount; tone++ {
		grayMap[grayUnmap[tone]] = uint8(tone)
	}
}

// codewordToSymbols converts the 174-bit LDPC codeword into the
// 79-symbol channel sequence: Costas anchors at indices 0-6,
// 36-42, 72-78 (each block is icos7 verbatim), and data symbols
// at all other positions decoding 3 bits each via the gray map.
//
// Bit order within a data symbol is MSB-first: bits[0..2] of the
// data symbol's slice form the 3-bit value with bits[0] being the
// most significant. This matches the QEX paper's bit-packing
// convention (verified by round-trip against research/demod's
// LLR pipeline in synth_test.go).
func codewordToSymbols(codeword [codewordBits]uint8) [nn]uint8 {
	var symbols [nn]uint8

	// Place the three Costas blocks. All three are icos7 verbatim.
	for i := 0; i < costasSymbolsPerBlock; i++ {
		symbols[i] = icos7[i]
		symbols[costasBlockStride+i] = icos7[i]
		symbols[2*costasBlockStride+i] = icos7[i]
	}

	// Fill data symbols. The 58 data symbols occupy channel-symbol
	// positions 7..35 and 43..71 (everything not in a Costas block).
	dataIdx := 0
	for ch := 0; ch < nn; ch++ {
		if isCostas(ch) {
			continue
		}
		bits := (codeword[dataIdx*bitsPerSymbol+0] << 2) |
			(codeword[dataIdx*bitsPerSymbol+1] << 1) |
			(codeword[dataIdx*bitsPerSymbol+2])
		symbols[ch] = grayMap[bits&0x7]
		dataIdx++
	}
	return symbols
}

// isCostas reports whether channelSym is one of the 21 Costas
// anchor positions.
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

// gfskPulse returns the per-sample value of the GFSK frequency-
// shaping pulse, evaluated at sample offset `n` from the symbol
// centre.
//
// Per QEX paper §3, the pulse is the convolution of a rectangular
// pulse of width T_sym (the "instantaneous-tone window") with a
// Gaussian filter of BT=2.0. Closed form (Forney, "Digital
// Communications," eq. 3.4-12, equivalently Krzymien-Murch eq. 2):
//
//	g(t) = 0.5 × [erf((t + T_sym/2)·α) - erf((t - T_sym/2)·α)]
//
// where α = 2π·B / sqrt(ln 2) and B = BT/T_sym is the 3-dB
// bandwidth of the Gaussian.
//
// **Normalisation:** peak ≈ 1 at t=0, integral over all t ≈ T_sym.
// This matches the rectangular pulse it smooths — each symbol's
// pulse contributes peak amplitude 1 (representing "tone fully
// selected") during the symbol's centre, falling to ~0 outside.
// The synthesis equation `freq = f_centre + baud × Σ tone_n × g`
// then produces freq(symbol_n_centre) ≈ f_centre + baud × tone_n,
// the desired 8-FSK tone.
//
// For BT=2, T_sym=0.16 s, B=12.5 Hz, the pulse has peak 1 at t=0
// and decays past ±2·T_sym to <1e-4 of peak.
func gfskPulse(sampleOffset int) float64 {
	t := float64(sampleOffset) / fs
	const T = float64(nsps) / fs // symbol period in seconds
	const B = gfskBT / T         // 3-dB bandwidth in Hz
	scale := 2 * math.Pi * B / math.Sqrt(math.Ln2)
	a := scale * (t - T/2)
	b := scale * (t + T/2)
	return 0.5 * (math.Erf(b) - math.Erf(a))
}

// Synthesize generates a 12 kHz mono float32 audio buffer of an
// FT8 transmission for the supplied codeword at the given centre
// frequency and DT offset.
//
//   - codeword: 174-bit LDPC-coded info+CRC+parity, hard-decided
//     to {0,1}. For subtraction use cases the codeword comes from
//     a successful BP+OSD-2 decode.
//   - freq: signal centre frequency in Hz (the tone-0 frequency
//     in standard FT8 nomenclature; tone k sits at freq + k·baud).
//   - dt: TX-start offset from the nominal 0.5 s slot start, in
//     seconds. Positive = late arrival, negative = early.
//   - nsamples: length of the output buffer. Samples outside the
//     TX window are zero. Typically 180000 (= 15 s × fs).
//   - amplitude: peak amplitude of the synthesised sinusoid. For
//     subtraction this is estimated from the audio (e.g., from the
//     Costas-anchor mean energy in research/demod.fitCostasPhase's
//     Ahat output).
//   - initialPhase: phase offset (radians) added to the synth's
//     sinusoid. For subtraction this is calibrated against the
//     real signal's Costas-anchor phase so the synth aligns with
//     the audio's instantaneous phase trajectory. Zero is fine for
//     standalone generation (test fixtures, candidate-finder
//     smoke tests) where absolute phase doesn't matter.
//
// Returns a float32 slice of length nsamples. The signal is
// phase-continuous across the entire TX window — the GFSK shaping
// produces smooth frequency transitions between symbols.
//
// Implementation: builds an instantaneous-frequency array by
// summing the per-symbol Gaussian shaping pulses, integrates to
// get phase, takes the sin to get audio. O(nsamples × pulse_width
// × 79) but with a fixed pulse width (~4 symbols at BT=2) this
// runs in well under 1 ms per call.
func Synthesize(codeword [codewordBits]uint8, freq, dt float64, nsamples int, amplitude, initialPhase float64) []float32 {
	symbols := codewordToSymbols(codeword)

	// TX window placement in the output buffer.
	txStartSample := int(math.Round((synthSlotStartSec + dt) * fs))

	// Each symbol's pulse extends ±pulseHalfSpanSym symbols around
	// the symbol's centre. Total signal duration in samples
	// includes the tail beyond the nominal nn-symbol window.
	pulseHalfSpanSamples := pulseHalfSpanSym * nsps

	// Pre-compute the pulse over its full support so we don't
	// re-call erfc for every sample. The pulse is symmetric so we
	// only store the non-negative half plus the value at 0.
	pulseSpanSamples := 2*pulseHalfSpanSamples + 1
	pulse := make([]float64, pulseSpanSamples)
	for n := -pulseHalfSpanSamples; n <= pulseHalfSpanSamples; n++ {
		pulse[n+pulseHalfSpanSamples] = gfskPulse(n)
	}

	// Instantaneous frequency offset from `freq` at each sample,
	// in units of `baud` Hz. Build by accumulating per-symbol
	// contributions weighted by the shaping pulse.
	//
	// instFreq[i] = Σ_n symbol[n] × pulse(i - n·nsps - nsps/2)
	//
	// Note the +nsps/2 offset — each symbol's pulse is centred at
	// its midpoint, not its start. Without this offset the
	// transmitted frequency would be biased by half a symbol.
	signal := make([]float32, nsamples)

	// Working range in the OUTPUT buffer: TX window plus the
	// pulse-tail span on either side. Clamp to [0, nsamples).
	rangeStart := txStartSample - pulseHalfSpanSamples
	rangeEnd := txStartSample + nn*nsps + pulseHalfSpanSamples
	if rangeStart < 0 {
		rangeStart = 0
	}
	if rangeEnd > nsamples {
		rangeEnd = nsamples
	}
	if rangeStart >= rangeEnd {
		return signal
	}

	// Compute instantaneous frequency offset (in tone units, i.e.,
	// multiples of `baud`) for every sample in the working range.
	instFreqTones := make([]float64, rangeEnd-rangeStart)
	for symIdx := 0; symIdx < nn; symIdx++ {
		toneVal := float64(symbols[symIdx])
		symCentreSample := txStartSample + symIdx*nsps + nsps/2

		// Compute the range of output samples this symbol's pulse
		// covers, clamped to the working range.
		pulseStart := symCentreSample - pulseHalfSpanSamples
		pulseEnd := symCentreSample + pulseHalfSpanSamples
		if pulseStart < rangeStart {
			pulseStart = rangeStart
		}
		if pulseEnd > rangeEnd-1 {
			pulseEnd = rangeEnd - 1
		}
		for s := pulseStart; s <= pulseEnd; s++ {
			offset := s - symCentreSample + pulseHalfSpanSamples
			instFreqTones[s-rangeStart] += toneVal * pulse[offset]
		}
	}

	// Integrate frequency to phase. Sample-rate-normalized:
	// phase[k] - phase[k-1] = 2π × (freq + baud × instFreqTones[k]) / fs.
	// Initial phase comes from `initialPhase` — subtraction callers
	// calibrate this against the real signal's Costas-anchor phase
	// so the synth aligns with the audio's phase trajectory. Standalone
	// callers (tests, candidate detection) pass 0.
	phase := initialPhase
	dPhaseCentre := 2 * math.Pi * freq / fs
	dPhasePerTone := 2 * math.Pi * baud / fs
	for k := 0; k < rangeEnd-rangeStart; k++ {
		phase += dPhaseCentre + dPhasePerTone*instFreqTones[k]
		signal[rangeStart+k] = float32(amplitude * math.Sin(phase))
	}

	return signal
}

// SynthesizeFromInfo is a convenience wrapper that LDPC-encodes a
// 91-bit info word into a 174-bit codeword, then synthesises.
// Useful for tests that start from a known message; not used by
// the subtraction path (which already has a codeword in hand).
//
// LDPC encoding is the linear matrix multiply codeword[0:91] = info,
// codeword[91:174] = G × info over GF(2), where G is the 83×91
// systematic generator submatrix. The G matrix is loaded from
// generator.dat (NOT BUILT YET — this stub returns an error). For
// the current subtraction experiment we don't need encoding; the
// codeword always comes from a successful decode.
//
// Returns an error if the LDPC encoder isn't available. This is a
// deliberate scaffold for future test-fixture work.
func SynthesizeFromInfo(info [91]uint8, freq, dt float64, nsamples int, amplitude, initialPhase float64) ([]float32, error) {
	return nil, errLDPCEncoderNotImplemented
}

// errLDPCEncoderNotImplemented signals that SynthesizeFromInfo
// can't be used until the LDPC encoder is wired up. The subtraction
// experiment doesn't need it; tests that round-trip from message
// text will eventually require it.
var errLDPCEncoderNotImplemented = errInternal("synth: LDPC encoder not implemented; supply a pre-encoded codeword to Synthesize() instead")

type errInternal string

func (e errInternal) Error() string { return string(e) }
