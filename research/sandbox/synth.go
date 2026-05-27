package sandbox

import (
	"math"
	"math/cmplx"
)

// FT8 GFSK modulation parameters per QEX paper §3:
//
//	gfskBT          = 2.0   bandwidth-time product of the Gaussian filter
//	ft8SymbolPeriod = 0.16  seconds per FT8 symbol (= 1 / 6.25 baud)
//
// The Gaussian filter's −3 dB bandwidth is B = gfskBT / ft8SymbolPeriod
// = 12.5 Hz. The filter standard deviation in time is
// σ = √(ln 2) / (2πB).
const (
	gfskBT          = 2.0
	ft8SymbolPeriod = 0.16
	gfskFilterB     = gfskBT / ft8SymbolPeriod // 12.5 Hz
)

// SynthesizeBaseband renders a 79-tone FT8 sequence as a complex
// baseband signal at bbRate (typically 200 Hz complex, matching the
// Channelizer's 200 Hz default bandwidth).
//
// Pipeline:
//
//  1. Build a piecewise-constant frequency trace f[n] = tones[n/spS]
//     × 6.25 Hz where spS = bbRate × ft8SymbolPeriod (32 samples per
//     symbol at 200 Hz).
//  2. Convolve f with a Gaussian filter of σ = √(ln 2)/(2πB),
//     B = 12.5 Hz. Filter length is truncated at ±3σ. Boundary
//     handling is edge-clamp (the first and last samples of f extend
//     outward) — gentler than zero-padding, which would inject a
//     spurious frequency jump at the edges.
//  3. Integrate the smoothed frequency: φ[n] = φ[n−1] + 2π f̃[n]/bbRate.
//  4. Emit the complex baseband sample s[n] = exp(i·φ[n]). Unit
//     amplitude, zero initial phase.
//
// Output length is exactly ft8SymbolCount × samplesPerSymbol — the
// caller is responsible for placing this within a larger baseband
// buffer at the candidate's dt offset.
//
// Clean-room derivation: the Gaussian impulse response in time is
// h(t) ∝ exp(−t²/(2σ²)) with σ above; this is the standard inverse
// Fourier transform of the −3 dB Gaussian frequency response
// H(f) = exp(−ln 2 · f²/(2B²)), which is the FT8/QEX spec.
func SynthesizeBaseband(tones [ft8SymbolCount]int, bbRate float64) []complex128 {
	samplesPerSymbol := int(math.Round(bbRate * ft8SymbolPeriod))
	totalSamples := ft8SymbolCount * samplesPerSymbol

	// Piecewise frequency trace.
	freq := make([]float64, totalSamples)
	for n := 0; n < totalSamples; n++ {
		freq[n] = float64(tones[n/samplesPerSymbol]) * refineToneSpacingHz
	}

	// Build the Gaussian filter, sample-domain.
	sigma := math.Sqrt(math.Ln2) / (2 * math.Pi * gfskFilterB) // seconds
	sigmaSamples := sigma * bbRate
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

	// Convolve freq with h, edge-clamping at the boundaries.
	smooth := make([]float64, totalSamples)
	for n := 0; n < totalSamples; n++ {
		acc := 0.0
		for k := 0; k <= 2*halfLen; k++ {
			idx := n + k - halfLen
			if idx < 0 {
				idx = 0
			} else if idx >= totalSamples {
				idx = totalSamples - 1
			}
			acc += h[k] * freq[idx]
		}
		smooth[n] = acc
	}

	// Integrate frequency to phase; emit unit-amplitude complex baseband.
	out := make([]complex128, totalSamples)
	phase := 0.0
	dPhase := 2 * math.Pi / bbRate
	for n := 0; n < totalSamples; n++ {
		phase += dPhase * smooth[n]
		out[n] = complex(math.Cos(phase), math.Sin(phase))
	}
	return out
}

// FitComplex returns the single complex scale factor c that minimises
// |observed − c·ref|² (least-squares closed form):
//
//	c = ⟨observed, ref⟩ / ⟨ref, ref⟩
//
// |c| is the amplitude that the unit-amplitude reference signal
// would need to match observed; arg(c) is the matching phase offset.
// Lengths must match.
//
// When ⟨ref, ref⟩ ≈ 0 (the reference is essentially zero), the fit
// is undefined and 0+0i is returned — the caller can treat this as
// "no signal energy to fit".
func FitComplex(observed, ref []complex128) complex128 {
	if len(observed) != len(ref) {
		panic("sandbox.FitComplex: length mismatch")
	}
	var num, den complex128
	for i, r := range ref {
		// ⟨observed, ref⟩ = Σ observed[i] · conj(ref[i])
		num += observed[i] * cmplx.Conj(r)
		den += r * cmplx.Conj(r)
	}
	if cmplx.Abs(den) == 0 {
		return 0
	}
	return num / den
}

// SubtractFitted returns observed − scale·ref. Lengths must match.
// The returned slice is freshly allocated; observed and ref are not
// modified.
func SubtractFitted(observed, ref []complex128, scale complex128) []complex128 {
	if len(observed) != len(ref) {
		panic("sandbox.SubtractFitted: length mismatch")
	}
	out := make([]complex128, len(observed))
	for i, o := range observed {
		out[i] = o - scale*ref[i]
	}
	return out
}

// SNRMetrics groups the per-candidate signal-vs-residual measurement.
type SNRMetrics struct {
	// SignalPower is |c|² · ⟨ref, ref⟩ — the energy of the fitted
	// reference signal (i.e., the energy of c·ref).
	SignalPower float64

	// ResidualPower is Σ |observed − c·ref|² — the energy left after
	// subtracting the fitted reference.
	ResidualPower float64

	// SNRBasebandDB is 10·log₁₀(SignalPower / ResidualPower) measured
	// in the baseband bandwidth used by the channelizer (typically
	// 200 Hz complex).
	SNRBasebandDB float64

	// SNR2500DB is SNRBasebandDB normalised to the WSJT-X convention
	// (2500 Hz noise reference bandwidth). The conversion subtracts
	// 10·log₁₀(2500 / basebandBW) because widening the noise BW
	// proportionally increases the integrated noise power.
	SNR2500DB float64
}

// MeasureSNR runs FitComplex + SubtractFitted on the supplied
// reference and observed baseband slices and reports the resulting
// signal/residual power ratio in both raw baseband-bandwidth and
// WSJT-X 2500-Hz-equivalent dB.
//
// basebandBWHz is the bandwidth the observed signal occupies (matches
// the channelizer's Extract bandwidth).
func MeasureSNR(observed, ref []complex128, basebandBWHz float64) SNRMetrics {
	c := FitComplex(observed, ref)
	residual := SubtractFitted(observed, ref, c)

	cAbs2 := real(c)*real(c) + imag(c)*imag(c)
	var refEnergy float64
	for _, r := range ref {
		refEnergy += real(r)*real(r) + imag(r)*imag(r)
	}
	signal := cAbs2 * refEnergy

	var resEnergy float64
	for _, x := range residual {
		resEnergy += real(x)*real(x) + imag(x)*imag(x)
	}

	return finishSNR(signal, resEnergy, basebandBWHz)
}

// MeasureSNRPerSymbol is the per-symbol-fit version of MeasureSNR. It
// splits observed and ref into samplesPerSymbol-length blocks and
// fits a complex scale independently per symbol, then sums signal and
// residual energy across all symbols.
//
// Per-symbol fit absorbs:
//
//   - per-symbol phase variation (i.e. small frequency drift across
//     the 12.6 s message — even 0.05 Hz error accumulates >180° of
//     phase across the slot, destroying a single-c fit),
//   - per-symbol amplitude variation (slow fading), and
//   - synth-vs-transmitter pulse-shape mismatch at the symbol
//     boundaries (the boundary energy is absorbed into the adjacent
//     symbols' c values rather than appearing as residual).
//
// What it does NOT absorb is content that doesn't have the right tone
// at the right symbol position — exactly the property we want for
// subtraction (we want true channel noise + interferers to stay in
// the residual).
//
// observed and ref must have lengths equal to ft8SymbolCount *
// samplesPerSymbol.
func MeasureSNRPerSymbol(observed, ref []complex128, samplesPerSymbol int, basebandBWHz float64) SNRMetrics {
	if len(observed) != len(ref) {
		panic("sandbox.MeasureSNRPerSymbol: length mismatch")
	}
	nSyms := len(observed) / samplesPerSymbol
	var signal, resEnergy float64
	for s := 0; s < nSyms; s++ {
		start := s * samplesPerSymbol
		end := start + samplesPerSymbol
		obsSym := observed[start:end]
		refSym := ref[start:end]
		c := FitComplex(obsSym, refSym)
		var refE float64
		for _, r := range refSym {
			refE += real(r)*real(r) + imag(r)*imag(r)
		}
		cAbs2 := real(c)*real(c) + imag(c)*imag(c)
		signal += cAbs2 * refE
		// Residual energy = Σ |obs - c·ref|².
		for i, o := range obsSym {
			d := o - c*refSym[i]
			resEnergy += real(d)*real(d) + imag(d)*imag(d)
		}
	}
	return finishSNR(signal, resEnergy, basebandBWHz)
}

// finishSNR converts raw signal/residual power into the SNRMetrics
// struct, including the WSJT-X 2500 Hz noise-BW normalisation.
func finishSNR(signal, resEnergy, basebandBWHz float64) SNRMetrics {
	var snrBB, snr2500 float64
	if resEnergy > 0 && signal > 0 {
		snrBB = 10 * math.Log10(signal/resEnergy)
		snr2500 = snrBB - 10*math.Log10(2500/basebandBWHz)
	}
	return SNRMetrics{
		SignalPower:   signal,
		ResidualPower: resEnergy,
		SNRBasebandDB: snrBB,
		SNR2500DB:     snr2500,
	}
}
