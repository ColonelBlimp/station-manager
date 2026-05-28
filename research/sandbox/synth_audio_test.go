package sandbox

import (
	"math"
	"math/rand"
	"testing"
)

// referenceSynthesizeAudio is the naive O(signalLen × kernelLen)
// implementation that was the original port. Retained inline as the
// equivalence reference for the fast analytical version.
// Do not call from production code — slow.
func referenceSynthesizeAudio(
	tones [ft8SymbolCount]int,
	carrierHz, dtSec, audioRate float64,
	audioLen int,
) (cosOut, sinOut []float32, signalStart, signalLen int) {
	sps := int(math.Round(audioRate * ft8SymbolPeriod))
	signalLen = ft8SymbolCount * sps
	signalStart = int(math.Round((dtSec + nominalStartSec) * audioRate))

	cosOut = make([]float32, audioLen)
	sinOut = make([]float32, audioLen)

	freq := make([]float64, signalLen)
	for n := 0; n < signalLen; n++ {
		freq[n] = carrierHz + float64(tones[n/sps])*refineToneSpacingHz
	}

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

// TestSynthesizeAudio_MatchesReference pins the fast SynthesizeAudio
// against the preserved naive convolution reference across random tone
// sequences and dtSec offsets. Tolerance is loose — phase accumulates
// over ~150k samples and the cumulative-kernel formulation differs in
// float ordering, so per-sample float32 mismatches of a few ULPs are
// expected; we require that the L∞ error stays below 1e-3, which is
// well below the level the downstream fit + subtract is sensitive to.
func TestSynthesizeAudio_MatchesReference(t *testing.T) {
	const audioRate = 12000.0
	const audioLen = 180000

	rng := rand.New(rand.NewSource(20260528))
	cases := 5
	for c := 0; c < cases; c++ {
		var tones [ft8SymbolCount]int
		for i := range tones {
			tones[i] = rng.Intn(8)
		}
		carrierHz := 200.0 + rng.Float64()*2400.0
		dtSec := -0.5 + rng.Float64()*1.0

		cosRef, sinRef, ssRef, lenRef := referenceSynthesizeAudio(tones, carrierHz, dtSec, audioRate, audioLen)
		cosGot, sinGot, ssGot, lenGot := SynthesizeAudio(tones, carrierHz, dtSec, audioRate, audioLen)

		if ssRef != ssGot {
			t.Errorf("case %d signalStart mismatch: ref=%d got=%d", c, ssRef, ssGot)
		}
		if lenRef != lenGot {
			t.Errorf("case %d signalLen mismatch: ref=%d got=%d", c, lenRef, lenGot)
		}
		if len(cosRef) != len(cosGot) || len(sinRef) != len(sinGot) {
			t.Fatalf("case %d output length mismatch", c)
		}
		maxCos, maxSin := 0.0, 0.0
		for i := range cosRef {
			d := math.Abs(float64(cosRef[i]) - float64(cosGot[i]))
			if d > maxCos {
				maxCos = d
			}
			d = math.Abs(float64(sinRef[i]) - float64(sinGot[i]))
			if d > maxSin {
				maxSin = d
			}
		}
		if maxCos > 1e-3 || maxSin > 1e-3 {
			t.Errorf("case %d (carrier=%.1f dt=%.3f): L∞ cos=%.6f sin=%.6f (tol 1e-3)", c, carrierHz, dtSec, maxCos, maxSin)
		}
	}
}

// BenchmarkSynthesizeAudio measures the per-call cost of the production
// SynthesizeAudio. Used to confirm the analytical-convolution version
// landed the expected order-of-magnitude speedup.
func BenchmarkSynthesizeAudio(b *testing.B) {
	const audioRate = 12000.0
	const audioLen = 180000
	rng := rand.New(rand.NewSource(1))
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = rng.Intn(8)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = SynthesizeAudio(tones, 1500.0, 0.0, audioRate, audioLen)
	}
}

// BenchmarkReferenceSynthesizeAudio benches the naive convolution
// reference for the speedup-ratio comparison.
func BenchmarkReferenceSynthesizeAudio(b *testing.B) {
	const audioRate = 12000.0
	const audioLen = 180000
	rng := rand.New(rand.NewSource(1))
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = rng.Intn(8)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = referenceSynthesizeAudio(tones, 1500.0, 0.0, audioRate, audioLen)
	}
}
