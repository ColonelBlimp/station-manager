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

	// γ = 2π · baud · t_start(sym=0) — the coefficient of the
	// tone-only phase term inherited from the carrier being on for
	// t_start(0) seconds before sym 0's window opens.
	gamma := 2 * math.Pi * baud * (synthSlotStartSec + dtSec)

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
		corrected := raw - gamma*float64(tone)

		fit.rawPhases[i] = raw
		fit.correctedPhases[i] = corrected
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
