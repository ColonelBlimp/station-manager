package synth

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/research/candidates"
)

// TestCodewordToSymbols_CostasAnchorsCorrect pins the Costas-block
// placement: regardless of the codeword bits, channel symbols 0-6,
// 36-42, 72-78 must contain icos7 = {3, 1, 4, 0, 6, 5, 2}. A
// failure here means the synth would never produce the Costas
// pattern, so candidate detection would silently fail.
func TestCodewordToSymbols_CostasAnchorsCorrect(t *testing.T) {
	// Two different codewords — all-zero and a random-ish pattern.
	// Costas tones must be identical between them.
	var cwZero [codewordBits]uint8
	var cwAlt [codewordBits]uint8
	for i := range cwAlt {
		cwAlt[i] = uint8(i % 2)
	}

	symZero := codewordToSymbols(cwZero)
	symAlt := codewordToSymbols(cwAlt)

	expectedCostas := icos7
	for block := 0; block < numCostasBlocks; block++ {
		base := block * costasBlockStride
		if block == 2 {
			// Block 2 starts at channel symbol 72, not 36*2 = 72 — they
			// happen to be the same because costasBlockStride = 36.
			base = 72
		}
		for i := 0; i < costasSymbolsPerBlock; i++ {
			want := expectedCostas[i]
			if symZero[base+i] != want {
				t.Errorf("zero-codeword block %d sym %d: got %d, want %d (icos7)",
					block, i, symZero[base+i], want)
			}
			if symAlt[base+i] != want {
				t.Errorf("alt-codeword block %d sym %d: got %d, want %d (icos7)",
					block, i, symAlt[base+i], want)
			}
		}
	}
}

// TestCodewordToSymbols_DataSymbolsRoundTrip pins the gray-code
// data-symbol mapping: for each 3-bit pattern (000..111), the
// produced tone index must round-trip back to the same bits via
// grayUnmap. Catches off-by-one or transposed-bit errors in
// codewordToSymbols's bit-packing.
func TestCodewordToSymbols_DataSymbolsRoundTrip(t *testing.T) {
	for bits := 0; bits < 8; bits++ {
		var cw [codewordBits]uint8
		// Put `bits` into data-symbol 0 (channel symbol 7 — the first
		// non-Costas position).
		cw[0] = uint8((bits >> 2) & 1)
		cw[1] = uint8((bits >> 1) & 1)
		cw[2] = uint8(bits & 1)

		sym := codewordToSymbols(cw)
		toneIdx := sym[7] // first data symbol
		recoveredBits := grayUnmap[toneIdx]
		if int(recoveredBits) != bits {
			t.Errorf("bits %03b → tone %d → bits %03b (expected %03b)",
				bits, toneIdx, recoveredBits, bits)
		}
	}
}

// TestGFSKPulse_PeakAndIntegral pins two structural properties of
// the GFSK shaping pulse: peak at t=0 is approximately 1/T_sym
// (i.e., ≈ 6.25 — same as the baud rate), and the integral over
// all t is approximately 1 (so each symbol contributes exactly one
// "tone unit" of frequency shift when its pulse is summed). These
// are the normalisation guarantees the rest of the synthesis depends
// on; a failure here means the synthesised tones are at wrong
// frequencies.
func TestGFSKPulse_PeakAndIntegral(t *testing.T) {
	peak := gfskPulse(0)
	// Expected ≈ 1 at t=0 (Gaussian-smoothed rect of unit height).
	// BT=2 leaves the peak essentially at 1 — at BT=2 the Gaussian
	// is wide enough that the rect's two transitions are well
	// separated and the central region is flat.
	if peak < 0.95 || peak > 1.05 {
		t.Errorf("pulse peak: got %.6f, expected ≈ 1.0 (unit-height rect smoothed)", peak)
	}

	// Integrate via Riemann sum over ±2 symbols at 1-sample resolution.
	// Expected ≈ T_sym = 0.16 s (the rect's integral, preserved by
	// Gaussian smoothing).
	const T = float64(nsps) / fs
	integral := 0.0
	const dt = 1.0 / fs
	for n := -pulseHalfSpanSym * nsps; n <= pulseHalfSpanSym*nsps; n++ {
		integral += gfskPulse(n) * dt
	}
	if integral < 0.99*T || integral > 1.01*T {
		t.Errorf("pulse integral: got %.6f, expected ≈ %.6f (T_sym)", integral, T)
	}
}

// TestSynthesize_CandidateDetected is the end-to-end smoke test:
// synthesise a signal at a known (freq, dt), feed it into the
// real candidate finder, and confirm a candidate appears within
// tolerance of the synthesised position. This is the load-bearing
// "does the synth audio look like FT8 to the rest of the pipeline"
// check — if it fails, subtraction won't work.
//
// We don't validate the codeword's data bits round-trip via
// demod/LDPC here — that test would also exercise the decoder's
// behaviour on a synthetic input and is best handled in a separate
// integration test. The Costas pattern detection is sufficient to
// confirm the synthesiser is producing recognisable FT8 structure.
func TestSynthesize_CandidateDetected(t *testing.T) {
	const (
		freq      = 1500.0 // mid-band, far from sweep edges
		dt        = 0.0    // physically on-time
		nsamples  = 180000 // 15-second slot
		amplitude = 0.3    // typical real-capture amplitude
		freqTolHz = 5.0    // generous; on-grid signal should land within ~1 Hz
		dtTolSec  = 0.1
	)

	// Use a pseudo-random but deterministic codeword. The Costas
	// pattern doesn't depend on the codeword; the data symbols
	// just fill the gaps with arbitrary tones.
	var cw [codewordBits]uint8
	for i := range cw {
		cw[i] = uint8((i*37 + 11) % 2)
	}

	audio := Synthesize(cw, freq, dt, nsamples, amplitude, 0.0)
	if len(audio) != nsamples {
		t.Fatalf("synth length %d, want %d", len(audio), nsamples)
	}

	// Quick sanity: audio should not be all zeros.
	var maxAbs float32
	for _, s := range audio {
		if a := float32(math.Abs(float64(s))); a > maxAbs {
			maxAbs = a
		}
	}
	if maxAbs < 0.1 {
		t.Fatalf("synth audio peak %.4f is suspiciously low; expected ≈ %.2f", maxAbs, amplitude)
	}

	cands := candidates.Find(audio)
	if len(cands) == 0 {
		t.Fatal("candidates.Find returned 0 candidates on synthesised audio — the synth isn't producing FT8 structure")
	}

	// At least one candidate must land near (freq, dt).
	for _, c := range cands {
		if math.Abs(c.Freq-freq) <= freqTolHz && math.Abs(c.DT-dt) <= dtTolSec {
			t.Logf("synthesised at (freq=%.2f, dt=%.3f); finder reported (freq=%.2f, dt=%.3f); top candidate ranked %.2f stage-1 score",
				freq, dt, c.Freq, c.DT, c.Score)
			return
		}
	}

	// Diagnostic: dump the top 3 candidates we DID find, so a failure
	// tells us whether it was off-frequency, off-dt, or absent entirely.
	t.Errorf("no candidate within (±%.1f Hz, ±%.3f s) of synthesised (%.2f Hz, %.3f s)",
		freqTolHz, dtTolSec, freq, dt)
	limit := len(cands)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		t.Logf("  top-%d: freq=%.2f dt=%.3f stage1=%.2f", i+1, cands[i].Freq, cands[i].DT, cands[i].Score)
	}
}

// TestSynthesizeComplex_ImagMatchesReal pins the contract that
// SynthesizeComplex's imaginary part equals Synthesize's float32
// output sample-for-sample (modulo float32 rounding). The two
// functions share the same instFreq + phase integration, so this
// is a regression guard against the two emitters drifting apart.
func TestSynthesizeComplex_ImagMatchesReal(t *testing.T) {
	const (
		freq         = 1500.0
		dt           = 0.0
		nsamples     = 180000
		amplitude    = 0.3
		initialPhase = 0.7 // arbitrary non-zero to exercise the phase path
		tol          = 1e-6
	)
	var cw [codewordBits]uint8
	for i := range cw {
		cw[i] = uint8((i*37 + 11) % 2)
	}
	real := Synthesize(cw, freq, dt, nsamples, amplitude, initialPhase)
	cmplx := SynthesizeComplex(cw, freq, dt, nsamples, amplitude, initialPhase)
	if len(real) != len(cmplx) {
		t.Fatalf("length mismatch: real=%d complex=%d", len(real), len(cmplx))
	}
	mismatches := 0
	for k := range real {
		got := float32(imag(cmplx[k]))
		want := real[k]
		diff := math.Abs(float64(got - want))
		if diff > tol {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("sample %d: imag=%.8f, real=%.8f, diff=%.2e",
					k, got, want, diff)
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d/%d samples mismatched", mismatches, len(real))
	}
}

// TestSynthesizeComplex_MagnitudeAtCarrier verifies the complex
// envelope's magnitude approximates the configured amplitude
// inside the TX window. The envelope's modulus is |amp · e^(jφ)|
// = amp regardless of phase — sample-by-sample. A failure here
// would point at a cos/sin sign mismatch or amplitude scaling bug.
func TestSynthesizeComplex_MagnitudeAtCarrier(t *testing.T) {
	const (
		freq         = 1500.0
		dt           = 0.0
		nsamples     = 180000
		amplitude    = 0.3
		initialPhase = 0.0
	)
	var cw [codewordBits]uint8
	for i := range cw {
		cw[i] = uint8((i*37 + 11) % 2)
	}
	z := SynthesizeComplex(cw, freq, dt, nsamples, amplitude, initialPhase)

	// Sample magnitudes in the interior of the TX window (avoid
	// pulse-edge taper). Symbol 40 sits at sample 0.5*fs + 40*nsps
	// = 6000 + 76800 = 82800. We'll check a 1920-sample symbol's
	// worth of samples there.
	for k := 82800; k < 82800+nsps; k++ {
		mag := math.Sqrt(real(z[k])*real(z[k]) + imag(z[k])*imag(z[k]))
		if mag < 0.99*amplitude || mag > 1.01*amplitude {
			t.Errorf("sample %d: |z|=%.6f, want ≈ %.4f", k, mag, amplitude)
			return
		}
	}
}

// TestSynthesize_DTShifts pins the dt-handling: synthesising at
// different DT offsets should produce candidates at the corresponding
// physical-DT positions in the slot. Catches sign errors in the
// txStartSample calculation.
func TestSynthesize_DTShifts(t *testing.T) {
	const (
		freq      = 1500.0
		nsamples  = 180000
		amplitude = 0.3
		freqTolHz = 5.0
		dtTolSec  = 0.1
	)

	var cw [codewordBits]uint8
	for i := range cw {
		cw[i] = uint8((i*37 + 11) % 2)
	}

	cases := []struct {
		name string
		dt   float64
	}{
		{"on-time", 0.0},
		{"early", -0.4},
		{"late", +0.4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			audio := Synthesize(cw, freq, tc.dt, nsamples, amplitude, 0.0)
			cands := candidates.Find(audio)
			found := false
			for _, c := range cands {
				if math.Abs(c.Freq-freq) <= freqTolHz && math.Abs(c.DT-tc.dt) <= dtTolSec {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no candidate near (freq=%.2f, dt=%.3f) for case %q", freq, tc.dt, tc.name)
			}
		})
	}
}
