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

	// Per-symbol freq lookup (79 entries instead of materializing a
	// length-signalLen array). freq[n] in the original is segFreq[n/sps].
	var segFreq [ft8SymbolCount]float64
	for s := 0; s < ft8SymbolCount; s++ {
		segFreq[s] = carrierHz + float64(tones[s])*refineToneSpacingHz
	}

	// Gaussian kernel (same BT=2.0 as SynthesizeBaseband, sampled at
	// audio rate). σ = √(ln 2) / (2πB), B = 12.5 Hz.
	sigma := math.Sqrt(math.Ln2) / (2 * math.Pi * gfskFilterB)
	sigmaSamples := sigma * audioRate
	halfLen := int(math.Ceil(3 * sigmaSamples))
	if halfLen < 1 {
		halfLen = 1
	}
	kernelLen := 2*halfLen + 1
	h := make([]float64, kernelLen)
	hSum := 0.0
	for k := 0; k < kernelLen; k++ {
		x := float64(k-halfLen) / sigmaSamples
		h[k] = math.Exp(-0.5 * x * x)
		hSum += h[k]
	}
	for k := range h {
		h[k] /= hSum
	}
	// cumH[k] = Σ h[0..k-1]; cumH[0] = 0, cumH[kernelLen] = 1.
	// Gives the kernel mass over taps [a, b) as cumH[b] - cumH[a].
	cumH := make([]float64, kernelLen+1)
	for k := 0; k < kernelLen; k++ {
		cumH[k+1] = cumH[k] + h[k]
	}

	// The naive smooth[n] = Σ_k h[k] · freq[clamp(n+k-halfLen)] is
	// O(signalLen × kernelLen). But freq is piecewise-constant in
	// segments of width sps (79 segments total), so the convolution
	// reduces to a sum over only the segments the kernel overlaps —
	// at most 2 (kernelLen=763 < sps=1920) plus optional edge-clamp
	// contributions. The exact identity is:
	//
	//   smooth[n] = Σ_s segFreq[s] · (cumH[overlapHi_s] - cumH[overlapLo_s])
	//             + (edge-clamp contributions from segFreq[0] / segFreq[end])
	//
	// Each kernel tap that maps to signal index < 0 uses segFreq[0]
	// (edge clamp); each tap that maps to >= signalLen uses
	// segFreq[end]. Their masses are cumH[lowClampCount] and
	// cumH[highClampCount] respectively.
	phase := 0.0
	dPhase := 2 * math.Pi / audioRate
	lastSegIdx := ft8SymbolCount - 1
	for n := 0; n < signalLen; n++ {
		// Signal-domain range covered by the kernel (before clamping).
		sigLo := n - halfLen     // inclusive
		sigHi := n + halfLen + 1 // exclusive

		smoothN := 0.0
		kStart := 0 // first non-clamped kernel tap index
		if sigLo < 0 {
			clampLen := -sigLo
			smoothN += segFreq[0] * cumH[clampLen]
			sigLo = 0
			kStart = clampLen
		}
		kEnd := kernelLen // exclusive; last non-clamped tap index + 1
		if sigHi > signalLen {
			clampLen := sigHi - signalLen
			smoothN += segFreq[lastSegIdx] * cumH[clampLen]
			sigHi = signalLen
			kEnd = kernelLen - clampLen
		}
		// Active region: signal indices [sigLo, sigHi) ↔ kernel taps
		// [kStart, kEnd). Iterate segments that overlap [sigLo, sigHi).
		sStart := sigLo / sps
		sEnd := (sigHi - 1) / sps
		for s := sStart; s <= sEnd; s++ {
			segLo := s * sps
			segHi := segLo + sps
			if segLo < sigLo {
				segLo = sigLo
			}
			if segHi > sigHi {
				segHi = sigHi
			}
			// Map segment's [segLo, segHi) signal indices back to
			// kernel-tap indices: tap = signalIdx - (n-halfLen).
			kLo := segLo - (n - halfLen)
			kHi := segHi - (n - halfLen)
			// Sanity-clip to the active tap window (no-op in practice
			// once the active region is right, but defensive).
			if kLo < kStart {
				kLo = kStart
			}
			if kHi > kEnd {
				kHi = kEnd
			}
			if kHi > kLo {
				smoothN += segFreq[s] * (cumH[kHi] - cumH[kLo])
			}
		}

		phase += dPhase * smoothN
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
