package demod

import (
	"math"
	"math/cmplx"
	"testing"
)

// TestDebug_ReproduceCase1 replicates the failing case 1
// (on_tone_0_zero_phase_start_0) outside the table-driven loop, so
// the issue (if any) shows directly. Uses synthesizeCosine exactly
// like the failing test path.
func TestDebug_ReproduceCase1(t *testing.T) {
	const fsHz = 12000.0
	const baseFreq = 1000.0
	const toneStep = 6.25
	var freqs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		freqs[k] = baseFreq + float64(k)*toneStep
	}
	samples := synthesizeCosine(nsps, 0, nsps, 1.0, 1000.0, 0, fsHz)
	t.Logf("samples[0]=%g samples[1]=%g samples[1919]=%g", samples[0], samples[1], samples[1919])
	for k := 0; k < ft8ToneCount; k++ {
		want := directDTFT(samples, 0, nsps, freqs[k], fsHz)
		t.Logf("tone %d freq=%g: DTFT=%v |X|=%g", k, freqs[k], want, cmplx.Abs(want))
	}
}

// TestDebug_DirectDTFTAndCosineSanity is a minimal sanity check on
// the test harness itself: a single on-bin cosine should produce
// near-zero DTFT at adjacent bins. If THIS fails, the bug is in
// directDTFT or synthesizeCosine, not in the Goertzel code under test.
func TestDebug_DirectDTFTAndCosineSanity(t *testing.T) {
	const (
		fsHz       = 12000.0
		signalFreq = 1000.0
		N          = 1920
	)

	samples := make([]float32, N)
	for i := 0; i < N; i++ {
		samples[i] = float32(math.Cos(2 * math.Pi * signalFreq * float64(i) / fsHz))
	}

	// Cosine value sanity.
	t.Logf("samples[0]=%g samples[1]=%g samples[12]=%g (period 12, expect 1, ~0.866, 1)",
		samples[0], samples[1], samples[12])

	gotSig := directDTFT(samples, 0, N, signalFreq, fsHz)
	t.Logf("DTFT at signal (%g Hz): %v |X|=%g (expect ~%g+0i, |X|≈960)",
		signalFreq, gotSig, cmplx.Abs(gotSig), float64(N)/2)

	gotAdj := directDTFT(samples, 0, N, signalFreq+6.25, fsHz)
	t.Logf("DTFT at signal+6.25: %v |X|=%g (expect ~0)", gotAdj, cmplx.Abs(gotAdj))

	gotFar := directDTFT(samples, 0, N, signalFreq+18.75, fsHz)
	t.Logf("DTFT at signal+18.75: %v |X|=%g (expect ~0)", gotFar, cmplx.Abs(gotFar))
}

// directDTFT computes the standard discrete-time Fourier transform at
// a single frequency by direct summation:
//
//	X(ω) = Σ_{n=0}^{N-1} x[start+n] · e^{-jωn}
//
// where ω = 2π·freq/Fs. Used in tests as the canonical reference the
// complex Goertzel kernel must match. Index 0 of the sum is the first
// sample of the windowed segment (start in original sample space).
func directDTFT(samples []float32, start, n int, freq, fsHz float64) complex128 {
	omega := 2 * math.Pi * freq / fsHz
	var sum complex128
	for i := 0; i < n; i++ {
		s := complex(float64(samples[start+i]), 0)
		e := cmplx.Exp(complex(0, -omega*float64(i)))
		sum += s * e
	}
	return sum
}

// makeUnitDelays builds the per-tone e^{+jω_k} array goertzelMultiComplex
// expects. Caller-of-test convenience.
func makeUnitDelays(freqs [ft8ToneCount]float64, fsHz float64) [ft8ToneCount]complex128 {
	var out [ft8ToneCount]complex128
	for k, f := range freqs {
		omega := 2 * math.Pi * f / fsHz
		out[k] = complex(math.Cos(omega), math.Sin(omega))
	}
	return out
}

// makeCoeffs builds the per-tone 2·cos(ω_k) array goertzelMultiComplex
// expects. Same convention as goertzelMulti's input.
func makeCoeffs(freqs [ft8ToneCount]float64, fsHz float64) [ft8ToneCount]float64 {
	var out [ft8ToneCount]float64
	for k, f := range freqs {
		out[k] = 2 * math.Cos(2*math.Pi*f/fsHz)
	}
	return out
}

// synthesizeCosine generates samples[start..start+n) holding
// A·cos(2π·freq·t/Fs + phase). t = sample index in the GLOBAL frame
// (the first sample of the buffer is t=0), so a non-zero start offset
// shifts where in the sinusoid the window begins — exactly the scenario
// the start-offset phase test pins.
func synthesizeCosine(bufLen, start, n int, amplitude, freq, phase, fsHz float64) []float32 {
	out := make([]float32, bufLen)
	omega := 2 * math.Pi * freq / fsHz
	for i := 0; i < n; i++ {
		t := float64(start + i)
		out[start+i] = float32(amplitude * math.Cos(omega*t+phase))
	}
	return out
}

// TestGoertzelMultiComplex_MatchesDirectDTFT is the load-bearing
// validation: across a grid of (freq, phase, start) combinations,
// the complex Goertzel output must agree with the direct DTFT sum
// to numerical precision. Any phase-convention or sign-of-ω error
// fails this test loudly.
//
// Uses 8 tones at FT8 spacing (6.25 Hz) starting at 1000 Hz, NSPS
// samples per window, fs = 12 kHz — the live use case.
func TestGoertzelMultiComplex_MatchesDirectDTFT(t *testing.T) {
	const (
		fsHz     = 12000.0
		baseFreq = 1000.0
		toneStep = 6.25 // FT8 8-FSK spacing
	)
	var freqs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		freqs[k] = baseFreq + float64(k)*toneStep
	}
	coeffs := makeCoeffs(freqs, fsHz)
	unitDelays := makeUnitDelays(freqs, fsHz)

	cases := []struct {
		name        string
		signalFreq  float64
		signalPhase float64
		bufLen      int
		start       int
	}{
		{"on_tone_0_zero_phase_start_0", 1000.0, 0, nsps, 0},
		{"on_tone_3_quarter_pi_start_0", 1018.75, math.Pi / 4, nsps, 0},
		{"on_tone_7_half_pi_start_0", 1043.75, math.Pi / 2, nsps, 0},
		{"on_tone_0_zero_phase_start_500", 1000.0, 0, nsps + 500, 500},
		{"on_tone_4_random_phase_start_1234", 1025.0, 1.7, nsps + 1234, 1234},
		{"off_bin_below_tone_2_zero_phase", 1010.0, 0, nsps, 0}, // between tones 1 and 2
		{"off_bin_above_tone_5_phase_-pi/3", 1035.0, -math.Pi / 3, nsps, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			samples := synthesizeCosine(c.bufLen, c.start, nsps, 1.0, c.signalFreq, c.signalPhase, fsHz)
			got := goertzelMultiComplex(samples, c.start, nsps, coeffs, unitDelays)
			for k := 0; k < ft8ToneCount; k++ {
				want := directDTFT(samples, c.start, nsps, freqs[k], fsHz)
				diff := got[k] - want
				if cmplx.Abs(diff) > 1e-6*cmplx.Abs(want)+1e-9 {
					t.Errorf("tone %d freq=%.2f: got %v, want %v (|diff|=%g, |want|=%g)",
						k, freqs[k], got[k], want, cmplx.Abs(diff), cmplx.Abs(want))
				}
			}
		})
	}
}

// TestGoertzelMultiComplex_OnBinPhase pins the closed-form for a
// single on-bin cosine: x[n] = A·cos(ω·n + φ), n=0..N-1 → DTFT at ω
// equals A·N/2 · e^{jφ}. So the Goertzel output at that tone must
// have magnitude A·N/2 and phase φ (within numerical precision).
//
// Catches a class of bugs (sign of ω, half-vs-full amplitude factor)
// that the directDTFT cross-check might mask if both implementations
// share the same convention error.
func TestGoertzelMultiComplex_OnBinPhase(t *testing.T) {
	const (
		fsHz       = 12000.0
		signalFreq = 1018.75 // exactly on FT8 tone 3 of base 1000
	)
	var freqs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		freqs[k] = 1000.0 + float64(k)*6.25
	}
	coeffs := makeCoeffs(freqs, fsHz)
	unitDelays := makeUnitDelays(freqs, fsHz)

	// Wait — on a finite-window DTFT, even an "on-bin" frequency only
	// hits the closed-form A·N/2·e^{jφ} EXACTLY when the bin spacing
	// equals 1/(NSPS/fs) = fs/NSPS = 6.25 Hz AND the signal frequency
	// is an exact multiple of that spacing. Our tones at 1000.0+k*6.25
	// are multiples of 6.25, so they hit on-bin only if 1000.0 is a
	// multiple of 6.25 — which it is (1000/6.25 = 160). Good.
	for _, phase := range []float64{0, math.Pi / 6, math.Pi / 3, math.Pi / 2, -math.Pi / 4, -math.Pi / 2} {
		samples := synthesizeCosine(nsps, 0, nsps, 1.0, signalFreq, phase, fsHz)
		got := goertzelMultiComplex(samples, 0, nsps, coeffs, unitDelays)

		// Tone 3 should pick up the full signal.
		x3 := got[3]
		wantMag := float64(nsps) / 2.0
		gotMag := cmplx.Abs(x3)
		if math.Abs(gotMag-wantMag)/wantMag > 1e-6 {
			t.Errorf("phase=%g: |X[3]| = %g, want %g", phase, gotMag, wantMag)
		}
		gotPhase := cmplx.Phase(x3)
		// Phase must equal φ modulo 2π.
		dp := math.Mod(gotPhase-phase+3*math.Pi, 2*math.Pi) - math.Pi
		if math.Abs(dp) > 1e-6 {
			t.Errorf("phase=%g: angle(X[3]) = %g, want %g (Δ=%g)", phase, gotPhase, phase, dp)
		}

		// Other tones should have near-zero magnitude (on-bin orthogonality).
		for k := 0; k < ft8ToneCount; k++ {
			if k == 3 {
				continue
			}
			if cmplx.Abs(got[k]) > 1e-6*float64(nsps) {
				t.Errorf("phase=%g tone %d: |X[%d]| = %g, want ~0 (on-bin orthogonality)",
					phase, k, k, cmplx.Abs(got[k]))
			}
		}
	}
}

// TestGoertzelMultiComplex_StartOffsetRotatesPhase pins the convention
// that the Goertzel output's phase is interpreted at the FIRST sample
// of the window. If we shift the window's starting sample by Δ within
// the same continuous sinusoid, the recovered phase must rotate by
// ω·Δ.
//
// This is the property phase interpolation between Costas anchors
// relies on: the phase reference at symbol s is the angle of the
// signal observed within that symbol's window, which is comparable
// across symbols as a linear function of (symbol·NSPS).
func TestGoertzelMultiComplex_StartOffsetRotatesPhase(t *testing.T) {
	const (
		fsHz       = 12000.0
		signalFreq = 1025.0 // on-bin (tone 4 of base 1000)
		basePhase  = math.Pi / 5
	)
	var freqs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		freqs[k] = 1000.0 + float64(k)*6.25
	}
	coeffs := makeCoeffs(freqs, fsHz)
	unitDelays := makeUnitDelays(freqs, fsHz)

	const bufLen = 3 * nsps
	samples := synthesizeCosine(bufLen, 0, bufLen, 1.0, signalFreq, basePhase, fsHz)

	// Take the Goertzel output for three consecutive NSPS windows
	// starting at samples 0, NSPS, 2*NSPS.
	var phases [3]float64
	for w := 0; w < 3; w++ {
		got := goertzelMultiComplex(samples, w*nsps, nsps, coeffs, unitDelays)
		phases[w] = cmplx.Phase(got[4]) // tone 4 carries the signal
	}

	// Expected phase at window starting at sample s = basePhase + ω·s.
	omega := 2 * math.Pi * signalFreq / fsHz
	for w := 0; w < 3; w++ {
		expected := basePhase + omega*float64(w*nsps)
		// Wrap to (-π, π].
		expectedWrapped := math.Mod(expected+math.Pi, 2*math.Pi) - math.Pi
		if expectedWrapped < -math.Pi {
			expectedWrapped += 2 * math.Pi
		}
		gotWrapped := math.Mod(phases[w]+math.Pi, 2*math.Pi) - math.Pi
		if gotWrapped < -math.Pi {
			gotWrapped += 2 * math.Pi
		}
		diff := math.Mod(gotWrapped-expectedWrapped+3*math.Pi, 2*math.Pi) - math.Pi
		if math.Abs(diff) > 1e-6 {
			t.Errorf("window %d (start=%d): got phase %g, want %g (Δ=%g)",
				w, w*nsps, gotWrapped, expectedWrapped, diff)
		}
	}
}

// TestGoertzelMultiComplex_MagnitudeMatchesIncoherent confirms the
// new complex kernel and the existing |X|² kernel agree on tone
// energies. |goertzelMultiComplex|² should equal goertzelMulti for
// the same input — a sanity check that the new path doesn't drift
// the magnitude.
func TestGoertzelMultiComplex_MagnitudeMatchesIncoherent(t *testing.T) {
	const fsHz = 12000.0
	var freqs [ft8ToneCount]float64
	for k := 0; k < ft8ToneCount; k++ {
		freqs[k] = 1000.0 + float64(k)*6.25
	}
	coeffs := makeCoeffs(freqs, fsHz)
	unitDelays := makeUnitDelays(freqs, fsHz)

	// Multi-tone synthetic — three tones at different amplitudes,
	// arbitrary phases — to exercise more than one bin at once.
	samples := make([]float32, nsps)
	for i := 0; i < nsps; i++ {
		t := float64(i)
		samples[i] = float32(
			1.0*math.Cos(2*math.Pi*1000.0*t/fsHz+0.3) +
				0.5*math.Cos(2*math.Pi*1018.75*t/fsHz+1.1) +
				0.2*math.Cos(2*math.Pi*1043.75*t/fsHz-0.7),
		)
	}

	complexX := goertzelMultiComplex(samples, 0, nsps, coeffs, unitDelays)
	realE := goertzelMulti(samples, 0, nsps, coeffs)

	for k := 0; k < ft8ToneCount; k++ {
		gotMagSq := real(complexX[k])*real(complexX[k]) + imag(complexX[k])*imag(complexX[k])
		wantMagSq := realE[k]
		diff := math.Abs(gotMagSq - wantMagSq)
		// Allow relative epsilon — both paths accumulate in float64
		// but through slightly different arithmetic, so equality is
		// approximate not exact.
		tol := 1e-6*wantMagSq + 1e-9
		if diff > tol {
			t.Errorf("tone %d: |X|² complex=%g, incoherent=%g, diff=%g (tol=%g)",
				k, gotMagSq, wantMagSq, diff, tol)
		}
	}
}
