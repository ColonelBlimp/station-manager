package dsp

import (
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// FT8 waveform synthesizer — generates the time-domain audio that
// an FT8 transmitter would produce for a given 77-bit message. Used
// by iterative signal subtraction (decode → synthesize that signal
// → subtract from audio → re-decode the residual) and as a
// reference for testing.
//
// **Spec sources (clean-room — not derived from WSJT-X
// gen_ft8wave.f90):**
//
//   - QEX paper §4 "Channel Symbols and Modulation" — the 79-symbol
//     transmission layout, the 8-FSK tone spacing of 6.25 Hz, the
//     Costas array {3,1,4,0,6,5,2} at symbol positions 0..6, 36..42,
//     72..78, and the BT=2 Gaussian frequency-shaping requirement.
//   - Murota & Hirade, "GMSK Modulation for Digital Mobile Radio
//     Telephony," IEEE Trans. Communications COM-29(7), July 1981 —
//     the standard formulation of the Gaussian filter for
//     continuous-phase FSK: Gaussian impulse response with
//     σ = sqrt(ln 2) / (2π · B), where B = BT/T is the 3 dB
//     filter bandwidth.

// FT8 modulation constants per QEX paper §4.
const (
	// GFSKBandwidthTimeProduct is the BT product specifying the
	// Gaussian filter's bandwidth-time product. Per QEX §4, FT8
	// uses BT=2 — wider than typical GMSK (BT=0.3 in GSM) for
	// stronger inter-symbol smoothing in the narrow 50 Hz FT8
	// channel. **Spec-mandated; do not override.**
	GFSKBandwidthTimeProduct = 2.0

	// SynthSlotStartSeconds is the nominal TX start offset within
	// the 15-second FT8 slot. Operational convention shared across
	// FT8 implementations — matches dsp.nominalTXStartSeconds used
	// by the sync detector + demodulator. **Convention-mandated for
	// interop; do not override.**
	SynthSlotStartSeconds = 0.5
)

// GFSKGaussianTruncationSigma controls how far (in standard
// deviations) the Gaussian impulse response extends before being
// truncated to zero. Wider truncation = more precise frequency
// transitions at the cost of more samples per symbol contributing
// to each output. 3σ covers ~99.7% of the Gaussian's energy and is
// the default; 2.5σ shows measurable boundary ringing in test
// signals, 4σ buys negligible precision over 3σ.
//
// Exposed as a package var so power-users (or sensitivity tuning
// experiments) can adjust. Mutate at program start before any
// synthesis call.
var GFSKGaussianTruncationSigma = 3.0

// Synthesize generates the FT8 time-domain audio for a 77-bit
// message at the given centre frequency f0 (Hz) and time offset dt
// (seconds, relative to the nominal slot start at
// SynthSlotStartSeconds).
//
// Output: NMAX (= 180000) float32 audio samples at Fs (= 12000 Hz),
// covering the full 15-second slot. The TX active region is ~12.64
// seconds (79 channel symbols × NSPS/Fs = 79 × 160 ms); samples
// outside that window are zero.
//
// Output amplitude is unit-peak. Callers performing signal
// subtraction must scale by the amplitude estimated from the
// received audio (e.g. via complex correlation in the candidate's
// band) before subtracting — the synth output alone is not
// energy-matched to the actual received signal.
//
// Returns nil if msgBits is the wrong length or if LDPC encoding
// rejects the input.
func Synthesize(msgBits []byte, f0, dt float64) []float32 {
	if len(msgBits) != codec.MessageBits {
		return nil
	}

	// 1. Build the full 91-bit info word: 77-bit message + 14-bit CRC.
	info := make([]byte, codec.InfoBits)
	copy(info[:codec.MessageBits], msgBits)
	crc := codec.CRC14(msgBits)
	for i := 0; i < codec.CRCBits; i++ {
		info[codec.MessageBits+i] = byte((crc >> (codec.CRCBits - 1 - i)) & 1)
	}

	// 2. LDPC encode: 91 info bits → 174 codeword bits.
	codeword := codec.LDPCEncode(info)

	// 3. Build the 79 channel symbols.
	//    Layout per QEX §4:
	//      [Costas 0..6]  [data 0..28]  [Costas 36..42]
	//      [data 29..57]  [Costas 72..78]
	//    Data symbols carry 3 codeword bits each via Gray code.
	tones := buildChannelSymbols(codeword)

	// 4. Convert the symbol sequence to a per-sample instantaneous
	//    frequency, apply Gaussian shaping, integrate to phase, emit
	//    sin samples.
	return synthesizeWaveform(tones, f0, dt)
}

// buildChannelSymbols maps a 174-bit codeword to the 79 channel
// symbols of an FT8 transmission. Returns the tone index (0..7)
// for each symbol position 0..78.
func buildChannelSymbols(codeword []byte) [NN]uint8 {
	var tones [NN]uint8

	// Three Costas-array blocks at known positions. Icos7 is the
	// 7-tone array defined in params.go (per QEX §4: {3,1,4,0,6,5,2}).
	for k := 0; k < CostasTonesPerBlock; k++ {
		tones[k] = Icos7[k]
		tones[CostasBlockStrideSymbols+k] = Icos7[k]
		tones[2*CostasBlockStrideSymbols+k] = Icos7[k]
	}

	// 58 data symbols sandwiched between Costas blocks.
	codewordIdx := 0
	for _, span := range [2][2]int{
		{SymbolDataStart1, SymbolDataEnd1},
		{SymbolDataStart2, SymbolDataEnd2},
	} {
		for chanSym := span[0]; chanSym <= span[1]; chanSym++ {
			var bits uint8
			for j := 0; j < DemodBitsPerSymbol; j++ {
				bits = (bits << 1) | codeword[codewordIdx]
				codewordIdx++
			}
			tones[chanSym] = GrayMap[bits]
		}
	}
	return tones
}

// synthesizeWaveform generates the time-domain audio for a given
// tone sequence at carrier frequency f0 (Hz) and time offset dt
// (seconds relative to SynthSlotStartSeconds).
//
// **Q-function shortcut (vs brute-force convolution):** The instantaneous
// frequency from convolving a piecewise-constant symbol-frequency
// train with a Gaussian filter is mathematically identical to the
// superposition of Gaussian step responses at each symbol boundary.
// Each step response is:
//
//	f_step(t) = Δf · Φ((t - τ_boundary) / σ)
//
// where Φ is the standard cumulative Gaussian (`0.5 · (1 + erf(x/√2))`).
// Φ saturates to 0 below -3σ and 1 above +3σ, so the transition is
// localised to a ±truncationSigma·σ zone around each boundary. With
// FT8's symbol duration T=160ms and σ≈10.6ms, the transition zones
// don't overlap (±32ms < ±80ms half-symbol), so we can fill the
// audio in two simple passes:
//
//  1. Precompute the Φ shape once at sample-offset resolution.
//  2. Build freqTraj[s] for s in [0, txDuration) by:
//     - filling steady-state segments with constant freq[n]
//     - filling transition zones with the precomputed Φ shape
//     scaled by Δf at each of 78 symbol boundaries
//  3. Integrate freqTraj to phase, emit sin samples in the TX window.
//
// Operation count: ~150K total (vs ~114M for brute-force convolution
// — roughly 1000× cheaper in the convolution stage). Standard GMSK
// efficiency technique; not a port.
func synthesizeWaveform(tones [NN]uint8, f0, dt float64) []float32 {
	const symbolDurationSec = float64(NSPS) / Fs // 0.16 s
	const filterBandwidth = GFSKBandwidthTimeProduct / symbolDurationSec
	gaussianSigma := math.Sqrt(math.Ln2) / (2.0 * math.Pi * filterBandwidth)
	sigmaSamples := gaussianSigma * Fs
	truncSamples := int(math.Ceil(GFSKGaussianTruncationSigma * sigmaSamples))

	// Precompute Φ((k - 0)/σ_samples) for k = -truncSamples..+truncSamples.
	// phiShape[k+truncSamples] gives Φ at sample offset k from a
	// boundary's centre. Φ(x) = 0.5·(1 + erf(x/√2)) via the stdlib.
	phiShape := make([]float64, 2*truncSamples+1)
	for k := -truncSamples; k <= truncSamples; k++ {
		x := float64(k) / sigmaSamples
		phiShape[k+truncSamples] = 0.5 * (1 + math.Erf(x/math.Sqrt2))
	}

	// Per-symbol target frequency: freq[n] = f0 + tones[n]·Baud.
	// Baud (=6.25 Hz) is both the symbol rate and the tone spacing
	// (FSK-like, h=1 modulation index).
	var freq [NN]float64
	for n := 0; n < NN; n++ {
		freq[n] = f0 + float64(tones[n])*Baud
	}

	// Build freqTraj for the full TX window. Pass 1: fill segments
	// between transition zones with constant freq[n]. Pass 2: fill
	// transition zones using the precomputed Φ shape.
	txDurationSamples := NN * NSPS // 79 × 1920 = 151,680 samples ≈ 12.64 s
	freqTraj := make([]float64, txDurationSamples)

	// Steady-state segment before the first boundary (s in [0, NSPS - truncSamples))
	// stays at freq[0]. After each boundary n (at sample n·NSPS), the
	// steady-state segment runs from boundary+truncSamples+1 to the
	// next boundary - truncSamples. Last segment runs to end of TX.
	for s := 0; s < min(NSPS-truncSamples, txDurationSamples); s++ {
		freqTraj[s] = freq[0]
	}
	for n := 1; n < NN; n++ {
		// Transition zone around boundary at n·NSPS.
		boundary := n * NSPS
		deltaFreq := freq[n] - freq[n-1]
		baseFreq := freq[n-1]
		zoneStart := boundary - truncSamples
		zoneEnd := boundary + truncSamples
		for s := zoneStart; s <= zoneEnd; s++ {
			if s < 0 || s >= txDurationSamples {
				continue
			}
			freqTraj[s] = baseFreq + deltaFreq*phiShape[s-zoneStart]
		}
		// Steady-state segment from end of this transition zone to
		// start of next boundary's transition zone (or end of TX).
		segStart := zoneEnd + 1
		var segEnd int
		if n+1 < NN {
			segEnd = (n+1)*NSPS - truncSamples
		} else {
			segEnd = txDurationSamples
		}
		if segEnd > txDurationSamples {
			segEnd = txDurationSamples
		}
		for s := segStart; s < segEnd; s++ {
			if s < 0 {
				continue
			}
			freqTraj[s] = freq[n]
		}
	}

	// Phase integration + sin emission.
	txStartSample := int(math.Round((SynthSlotStartSeconds + dt) * Fs))
	out := make([]float32, NMAX)
	phase := 0.0
	for s := 0; s < txDurationSamples; s++ {
		phase += 2.0 * math.Pi * freqTraj[s] / Fs
		// Keep phase bounded to avoid float64 precision loss over
		// the 12.64-second integration.
		if phase > 2.0*math.Pi {
			phase -= 2.0 * math.Pi
		} else if phase < -2.0*math.Pi {
			phase += 2.0 * math.Pi
		}

		outIdx := txStartSample + s
		if outIdx >= 0 && outIdx < NMAX {
			out[outIdx] = float32(math.Sin(phase))
		}
	}

	return out
}
