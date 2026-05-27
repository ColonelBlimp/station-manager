package sandbox

import (
	"math"
)

// audioRateHz is the FT8 audio sample rate per the project convention
// (12 kHz mono float32). Synthesis at this rate is what's needed for
// in-place subtraction from the slot's audio buffer.
const audioRateHz = 12000.0

// SynthesizeAudio renders the 79 FT8 tones as a real-valued audio
// signal at audioRateHz, placing it at offset dtSec (WSJT-X
// nominal-start convention: dtSec = 0 means "signal starts at the
// 0.5 s nominal slot start"). carrierHz is the audio-domain tone-0
// frequency (so tones occupy [carrierHz, carrierHz + 43.75] Hz).
//
// Returns two unit-amplitude reference signals:
//
//	cosOut[n] = cos(carrier_phase(n) + gfsk_phase(n))
//	sinOut[n] = sin(carrier_phase(n) + gfsk_phase(n))
//
// where carrier_phase + gfsk_phase is the integrated instantaneous
// frequency f(t) = carrierHz + tones[symbol(t)] × 6.25, with GFSK
// BT=2.0 Gaussian smoothing applied to the symbol-rate frequency
// trace (same kernel as SynthesizeBaseband — clean-room from QEX §3).
//
// Both buffers are audioLen samples long. Outside the signal's
// time window [signalStart, signalStart + 79·sps) the samples are
// zero. signalStart and signalLen are returned so callers know
// exactly which slice to fit/subtract over.
//
// cos and sin are the I/Q components needed by the audio-domain LSQ
// fit: real-audio[n] ≈ a·cos[n] + b·sin[n] gives a 2-variable system
// for the per-symbol amplitude/phase pair (a, b).
func SynthesizeAudio(
	tones [ft8SymbolCount]int,
	carrierHz, dtSec, audioRate float64,
	audioLen int,
) (cosOut, sinOut []float32, signalStart, signalLen int) {
	sps := int(math.Round(audioRate * ft8SymbolPeriod))
	signalLen = ft8SymbolCount * sps
	signalStart = int(math.Round((dtSec + nominalStartSec) * audioRate))

	cosOut = make([]float32, audioLen)
	sinOut = make([]float32, audioLen)

	// Piecewise instantaneous-frequency trace at audio rate.
	freq := make([]float64, signalLen)
	for n := 0; n < signalLen; n++ {
		freq[n] = carrierHz + float64(tones[n/sps])*refineToneSpacingHz
	}

	// Gaussian filter (same BT=2.0 kernel as SynthesizeBaseband, just
	// re-sampled to audio rate). σ = √(ln 2) / (2πB), B = 12.5 Hz.
	sigma := math.Sqrt(math.Ln2) / (2 * math.Pi * gfskFilterB)
	sigmaSamples := sigma * audioRate
	halfLen := int(math.Ceil(3 * sigmaSamples))
	if halfLen < 1 {
		halfLen = 1
	}
	h := make([]float64, 2*halfLen+1)
	hSum := 0.0
	for k := 0; k <= 2*halfLen; k++ {
		x := float64(k-halfLen) / sigmaSamples
		h[k] = math.Exp(-0.5 * x * x)
		hSum += h[k]
	}
	for k := range h {
		h[k] /= hSum
	}

	// Convolve freq with h, edge-clamping at signal boundaries.
	smooth := make([]float64, signalLen)
	for n := 0; n < signalLen; n++ {
		acc := 0.0
		for k := 0; k <= 2*halfLen; k++ {
			idx := n + k - halfLen
			if idx < 0 {
				idx = 0
			} else if idx >= signalLen {
				idx = signalLen - 1
			}
			acc += h[k] * freq[idx]
		}
		smooth[n] = acc
	}

	// Integrate phase, emit cos+sin at audio rate within the signal
	// time window.
	phase := 0.0
	dPhase := 2 * math.Pi / audioRate
	for n := 0; n < signalLen; n++ {
		phase += dPhase * smooth[n]
		idx := signalStart + n
		if idx < 0 || idx >= audioLen {
			continue
		}
		cosOut[idx] = float32(math.Cos(phase))
		sinOut[idx] = float32(math.Sin(phase))
	}
	return cosOut, sinOut, signalStart, signalLen
}

// FitAndSubtractAudio performs the per-symbol 2-variable LSQ fit on
// audio (real-valued) against the cos/sin synth references and
// returns the residual audio. For each of the 79 symbol windows
// (sps samples each), it solves:
//
//	minimize Σ (audio[n] − a·cos[n] − b·sin[n])²
//
// via the closed-form 2×2 normal-equations system:
//
//	[⟨c,c⟩ ⟨c,s⟩] [a]   [⟨a,c⟩]
//	[⟨c,s⟩ ⟨s,s⟩] [b] = [⟨a,s⟩]
//
// where ⟨x,y⟩ is the sum-of-products over the symbol's audio range.
// Per-symbol fits absorb any accumulated phase drift across the
// slot (same reason MeasureSNRPerSymbol uses per-symbol fits).
//
// audio is not modified; the returned residual is a fresh allocation
// equal to audio - Σ(a·cos + b·sin) inside the signal range. Outside,
// it's a verbatim copy of audio.
//
// Singular symbol windows (≈zero synth energy) leave audio unchanged.
func FitAndSubtractAudio(
	audio []float32,
	cosSynth, sinSynth []float32,
	signalStart, signalLen, samplesPerSymbol int,
) []float32 {
	residual := make([]float32, len(audio))
	copy(residual, audio)
	if signalLen <= 0 || signalStart < 0 ||
		signalStart+signalLen > len(audio) {
		return residual
	}
	nSyms := signalLen / samplesPerSymbol
	for s := 0; s < nSyms; s++ {
		start := signalStart + s*samplesPerSymbol
		end := start + samplesPerSymbol

		// Build the 2×2 system + RHS over the symbol's audio range.
		var cc, ss, cs, ac, as float64
		for n := start; n < end; n++ {
			c := float64(cosSynth[n])
			si := float64(sinSynth[n])
			x := float64(audio[n])
			cc += c * c
			ss += si * si
			cs += c * si
			ac += x * c
			as += x * si
		}
		// Solve via cofactor formula: det = cc·ss − cs².
		det := cc*ss - cs*cs
		if det <= 0 || (cc == 0 && ss == 0) {
			continue
		}
		a := (ss*ac - cs*as) / det
		b := (cc*as - cs*ac) / det

		// Subtract the fitted signal from the residual over this
		// symbol's range.
		for n := start; n < end; n++ {
			residual[n] = float32(float64(audio[n]) -
				a*float64(cosSynth[n]) -
				b*float64(sinSynth[n]))
		}
	}
	return residual
}
