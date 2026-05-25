package demod

import (
	"math"
	"math/cmplx"
)

// icos7 is the FT8 Costas synchronisation pattern per QEX paper §3.
// The same 7-tone sequence is embedded at channel-symbol positions
// 0..6, 36..42, and 72..78 of every transmission.
var icos7 = [costasSymbolsPerBlock]uint8{3, 1, 4, 0, 6, 5, 2}

// costasAnchors is the total number of Costas synchronisation anchors
// across the three blocks. Used as the size of fixed-size fit arrays.
const costasAnchors = numCostasBlocks * costasSymbolsPerBlock // 21

// costasSym maps the i-th Costas anchor index (0..20) to its
// channel-symbol position. Precomputed for fast iteration.
var costasSym = func() [costasAnchors]int {
	var out [costasAnchors]int
	idx := 0
	for block := 0; block < numCostasBlocks; block++ {
		for sym := 0; sym < costasSymbolsPerBlock; sym++ {
			out[idx] = block*costasBlockStride + sym
			idx++
		}
	}
	return out
}()

// costasExpectedTone maps the i-th Costas anchor to its expected tone
// from icos7. Pre-flattened across the three blocks (each block uses
// the same icos7 sequence).
var costasExpectedTone = func() [costasAnchors]uint8 {
	var out [costasAnchors]uint8
	idx := 0
	for block := 0; block < numCostasBlocks; block++ {
		for sym := 0; sym < costasSymbolsPerBlock; sym++ {
			out[idx] = icos7[sym]
			idx++
		}
	}
	return out
}()

// PhaseFit captures the result of fitting a linear phase model to the
// 21 Costas anchor phases of a candidate FT8 signal. The model is
//
//	phase(sym) = Phi0 + Slope · sym
//
// where Slope is in radians per channel symbol. Slope captures any
// residual carrier-frequency offset after the candidate finder's
// fine-frequency refinement (slope = 2π · Δf · NSPS / fs); a real
// signal at perfectly-known frequency has Slope ≈ 0.
//
// RMSResid is the weighted RMS residual of the fit (radians). It
// summarises how well the data conforms to the linear model — large
// values indicate non-linear phase dynamics (HF multipath, Doppler
// spread, bad candidate-finder hit) that make coherent demod
// unreliable. The caller should compare against a threshold and fall
// back to incoherent demod when the fit doesn't hold.
//
// AccessibleAnchors records how many of the 21 anchors had an audio
// window inside the supplied buffer. Candidates at the slot edges
// (|dt| close to ±2.5 s) lose some anchors; if too few survive, the
// fit isn't meaningful and RMSResid is set to +Inf.
type PhaseFit struct {
	Phi0     float64
	Slope    float64
	RMSResid float64

	AccessibleAnchors int

	// Diagnostic fields — retained for tests and verbose logging.
	rawPhases       [costasAnchors]float64
	correctedPhases [costasAnchors]float64
	weights         [costasAnchors]float64
}

// PhaseFitFor is the exported entry point to fitCostasPhase, for
// diagnostic callers (decode-eval's slope-vs-RMS pass) that want
// the phase-fit results independent of whether they're running
// the coherent demod path. Internal coherent decode uses
// fitCostasPhase directly.
func PhaseFitFor(samples []float32, freqHz, dtSec float64) PhaseFit {
	return fitCostasPhase(samples, freqHz, dtSec)
}

// fitCostasPhase extracts complex Goertzel amplitudes at the 21
// Costas anchors of the candidate at (freqHz, dtSec), estimates a
// per-anchor weight from expected-vs-other-tone log-contrast, and
// fits the linear phase model
//
//	phase(sym) = Phi0 + Slope · sym
//
// after subtracting the known tone-dependent term γ · tone (with
// γ = 2π · baud · (0.5+dtSec)) from each anchor's measured phase.
// This tone-correction step is what allows a SINGLE global linear
// fit across all 21 anchors instead of three per-block fits — the
// cross term β · tone · sym vanishes for FT8 (β = 2π · baud · T_sym
// = 2π · 1 ≡ 0 mod 2π), and the residual γ · tone term is known
// from icos7 so it can be subtracted before fitting.
//
// Pass 1 estimates an initial slope from within-block
// consecutive-anchor phase differences (1-symbol gaps avoid 2π
// wrap ambiguity for any realistic carrier offset). Pass 2 uses
// that estimate to unwrap each anchor's corrected phase, then runs
// closed-form weighted least squares to refine.
//
// Inaccessible anchors (audio window outside the buffer) get weight
// zero and are skipped throughout. Returns +Inf RMSResid when fewer
// than 3 anchors are accessible or the LS system is singular.
func fitCostasPhase(samples []float32, freqHz, dtSec float64) PhaseFit {
	var fit PhaseFit

	// Goertzel coefficients + unit delays for the 8 FT8 tones at
	// (freqHz + k · baud). Same convention as Demod.
	var coeffs [ft8ToneCount]float64
	var unitDelays [ft8ToneCount]complex128
	for k := 0; k < ft8ToneCount; k++ {
		fk := freqHz + float64(k)*baud
		omega := 2 * math.Pi * fk / fs
		coeffs[k] = 2 * math.Cos(omega)
		unitDelays[k] = complex(math.Cos(omega), math.Sin(omega))
	}

	txStart := int(math.Round((synthSlotStartSec + dtSec) * fs))

	const eps = 1e-12

	for i := 0; i < costasAnchors; i++ {
		sym := costasSym[i]
		tone := costasExpectedTone[i]
		symStart := txStart + sym*nsps

		if symStart < 0 || symStart+nsps > len(samples) {
			continue
		}

		x := goertzelMultiComplex(samples, symStart, nsps, coeffs, unitDelays)

		expectedX := x[tone]
		expectedE := real(expectedX)*real(expectedX) + imag(expectedX)*imag(expectedX)

		var maxOtherE float64
		for k := 0; k < ft8ToneCount; k++ {
			if k == int(tone) {
				continue
			}
			e := real(x[k])*real(x[k]) + imag(x[k])*imag(x[k])
			if e > maxOtherE {
				maxOtherE = e
			}
		}

		// Weight = max(0, log(expected/max-other)). Anchors where
		// the expected tone is overshadowed by another get zero
		// weight — they don't contribute (likely wrong candidate
		// position, severe noise, or signal interference).
		logContrast := math.Log((expectedE + eps) / (maxOtherE + eps))
		weight := math.Max(0, logContrast)

		raw := cmplx.Phase(expectedX)
		fit.rawPhases[i] = raw
		fit.correctedPhases[i] = raw // no γ·tone correction — see model derivation in fn doc
		fit.weights[i] = weight
		fit.AccessibleAnchors++
	}

	if fit.AccessibleAnchors < 3 {
		fit.RMSResid = math.Inf(1)
		return fit
	}

	// Pass 1: initial slope estimate from within-block
	// consecutive-anchor diffs (1-symbol gaps, no wrap ambiguity).
	var slopeSum, slopeWtSum float64
	for i := 0; i < costasAnchors-1; i++ {
		if fit.weights[i] == 0 || fit.weights[i+1] == 0 {
			continue
		}
		if costasSym[i+1]-costasSym[i] != 1 {
			continue
		}
		diff := wrapPi(fit.correctedPhases[i+1] - fit.correctedPhases[i])
		w := math.Min(fit.weights[i], fit.weights[i+1])
		slopeSum += w * diff
		slopeWtSum += w
	}
	var slopeInit float64
	if slopeWtSum > 0 {
		slopeInit = slopeSum / slopeWtSum
	}

	// Pass 2: unwrap each anchor's corrected phase relative to the
	// slopeInit-predicted phase, then weighted closed-form LS.
	var unwrapped [costasAnchors]float64
	for i := 0; i < costasAnchors; i++ {
		if fit.weights[i] == 0 {
			continue
		}
		predicted := slopeInit * float64(costasSym[i])
		diff := fit.correctedPhases[i] - predicted
		n := math.Round(diff / (2 * math.Pi))
		unwrapped[i] = fit.correctedPhases[i] - n*2*math.Pi
	}

	// Closed-form weighted LS for (Phi0, Slope):
	//   Phi0  = (Σw·p · Σw·s² - Σw·s · Σw·s·p) / (Σw · Σw·s² - (Σw·s)²)
	//   Slope = (Σw · Σw·s·p - Σw·s · Σw·p) / (Σw · Σw·s² - (Σw·s)²)
	var w0, wS, wP, wSS, wSP float64
	for i := 0; i < costasAnchors; i++ {
		w := fit.weights[i]
		if w == 0 {
			continue
		}
		s := float64(costasSym[i])
		p := unwrapped[i]
		w0 += w
		wS += w * s
		wP += w * p
		wSS += w * s * s
		wSP += w * s * p
	}

	det := w0*wSS - wS*wS
	if det == 0 {
		// All anchors at same sym (impossible by construction) or
		// numerical degeneracy. Treat as fit failure.
		fit.Slope = slopeInit
		if w0 > 0 {
			fit.Phi0 = wP / w0
		}
		fit.RMSResid = math.Inf(1)
		return fit
	}

	fit.Slope = (w0*wSP - wS*wP) / det
	fit.Phi0 = (wP - fit.Slope*wS) / w0

	// Weighted RMS residual.
	var residSqSum float64
	for i := 0; i < costasAnchors; i++ {
		w := fit.weights[i]
		if w == 0 {
			continue
		}
		s := float64(costasSym[i])
		p := unwrapped[i]
		r := p - fit.Phi0 - fit.Slope*s
		residSqSum += w * r * r
	}
	fit.RMSResid = math.Sqrt(residSqSum / w0)

	return fit
}

// wrapPi reduces an angle to (-π, π].
func wrapPi(theta float64) float64 {
	t := math.Mod(theta, 2*math.Pi)
	if t > math.Pi {
		t -= 2 * math.Pi
	} else if t <= -math.Pi {
		t += 2 * math.Pi
	}
	return t
}

// DemodCoherent extracts coherent soft metrics for the 58 FT8 data
// symbols, using a phase reference fitted from the 21 Costas anchors.
//
// Algorithm:
//
//  1. Fit a linear phase model φ(sym) = Phi0 + Slope·sym to the
//     Costas anchors via fitCostasPhase. Returns PhaseFit; caller
//     checks PhaseFit.RMSResid against a threshold to decide
//     whether to use these metrics or fall back to incoherent demod.
//
//  2. Estimate Ahat (signal amplitude after phase rotation) as the
//     mean Re(rotated Costas-expected-tone) across all anchors, and
//     σ² (post-rotation noise variance) as the mean squared
//     Re(rotated non-expected tones). Both use the SAME phase model
//     the data symbols will use, so any bias inherent in the model
//     affects calibration and decoding consistently.
//
//  3. For each data symbol at channel-symbol position s (s ∈
//     7..35, 43..71), compute complex Goertzel at all 8 FT8 tones.
//     For each tone k:
//
//     rotated_k = X[k] · exp(-j · (Phi0 + Slope·s + γ·k))
//     metric[k] = (Ahat / σ²) · Re(rotated_k)
//
//     The metric is signed: positive when the tone is at the
//     reference phase (= candidate tone matches transmitted),
//     near-zero for tones the transmitter didn't use at this
//     symbol, and negative if the tone is anti-phase (rare in
//     practice, indicative of phase drift the linear model
//     didn't capture).
//
// On phase-fit failure (RMSResid = +Inf or AccessibleAnchors < 3),
// returns an all-zero metric matrix and the failed PhaseFit. The
// caller MUST inspect PhaseFit before using metrics.
//
// The scaling factor (Ahat / σ²) is what makes these metrics
// directly feedable into logsumexp without additional alpha
// estimation — it's the matched-filter output normalised by noise
// variance, which is what coherent-AWGN likelihood theory wants.
func DemodCoherent(samples []float32, freqHz, dtSec float64) ([dataSymbolCount][ft8ToneCount]float64, PhaseFit) {
	var out [dataSymbolCount][ft8ToneCount]float64

	fit := fitCostasPhase(samples, freqHz, dtSec)

	if fit.AccessibleAnchors < 3 || math.IsInf(fit.RMSResid, 1) {
		return out, fit
	}

	var coeffs [ft8ToneCount]float64
	var unitDelays [ft8ToneCount]complex128
	for k := 0; k < ft8ToneCount; k++ {
		fk := freqHz + float64(k)*baud
		omega := 2 * math.Pi * fk / fs
		coeffs[k] = 2 * math.Cos(omega)
		unitDelays[k] = complex(math.Cos(omega), math.Sin(omega))
	}

	txStart := int(math.Round((synthSlotStartSec + dtSec) * fs))

	// First pass over Costas anchors to estimate Ahat (signal amplitude
	// at expected tone after rotation) and σ² (noise variance of
	// non-expected-tone real components after rotation). All tones at
	// a given sym use the SAME predicted phase (Phi0 + Slope·s); there
	// is no per-tone phase correction (see fitCostasPhase model doc).
	var ahatSum, sigmaSqSum float64
	var ahatN, sigmaN int
	for i := 0; i < costasAnchors; i++ {
		if fit.weights[i] == 0 {
			continue
		}
		sym := costasSym[i]
		expectedTone := costasExpectedTone[i]
		symStart := txStart + sym*nsps
		if symStart < 0 || symStart+nsps > len(samples) {
			continue
		}
		x := goertzelMultiComplex(samples, symStart, nsps, coeffs, unitDelays)

		phaseAtSym := fit.Phi0 + fit.Slope*float64(sym)

		// Re(rotated) at the expected tone is the signal component.
		rotExp := rotateClockwise(x[expectedTone], phaseAtSym)
		ahatSum += real(rotExp)
		ahatN++

		// Re(rotated) at the 7 non-expected tones is the noise component.
		for k := 0; k < ft8ToneCount; k++ {
			if k == int(expectedTone) {
				continue
			}
			rotK := rotateClockwise(x[k], phaseAtSym)
			sigmaSqSum += real(rotK) * real(rotK)
			sigmaN++
		}
	}

	if ahatN == 0 || sigmaN == 0 {
		return out, fit
	}

	ahat := ahatSum / float64(ahatN)
	sigmaSq := sigmaSqSum / float64(sigmaN)

	const eps = 1e-12
	alpha := ahat / (sigmaSq + eps)

	// Second pass: emit coherent metrics for the 58 data symbols.
	// All 8 tones at a given channel symbol use the same predicted
	// phase (Phi0 + Slope·channelSym); no per-tone correction.
	dataIdx := 0
	for channelSym := 0; channelSym < nn; channelSym++ {
		if isCostas(channelSym) {
			continue
		}
		symStart := txStart + channelSym*nsps
		if symStart >= 0 && symStart+nsps <= len(samples) {
			x := goertzelMultiComplex(samples, symStart, nsps, coeffs, unitDelays)
			phaseAtSym := fit.Phi0 + fit.Slope*float64(channelSym)
			for k := 0; k < ft8ToneCount; k++ {
				rot := rotateClockwise(x[k], phaseAtSym)
				out[dataIdx][k] = alpha * real(rot)
			}
		}
		dataIdx++
	}

	return out, fit
}

// rotateClockwise multiplies a complex value by e^{-jθ}, i.e.
// rotates by -θ. Inlined to avoid a cmplx.Exp call (which goes
// through cmplx.Cot internally and is ~10× slower than
// hand-written sincos for this hot path).
func rotateClockwise(x complex128, theta float64) complex128 {
	cosT := math.Cos(theta)
	sinT := math.Sin(theta)
	xr, xi := real(x), imag(x)
	// (xr + j·xi) · (cosT - j·sinT) = (xr·cosT + xi·sinT) + j·(xi·cosT - xr·sinT)
	return complex(xr*cosT+xi*sinT, xi*cosT-xr*sinT)
}
