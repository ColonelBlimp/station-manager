package ft8

import "math"

// GFSK modulation constants shared by every profile (ADR 0029 step b). The
// per-mode geometry — symbol length, tone count, Gaussian BT, edge ramp — lives
// on Profile (profile.go), which also owns Modulate / EncodeToSlot /
// EncodeWaveform. The whole encode→modulate chain is offline round-trip
// verifiable — the generated audio goes straight back through the shipped
// decoder (modulate_test.go, profile_test.go) — so a TX waveform is proven
// correct with ZERO RF before any audio device or PTT exists.

const (
	// txModIndex is the modulation index h. FT8 and FT4 both use h=1 (tone
	// spacing equals the symbol rate), so one tone step is exactly one bin.
	txModIndex = 1.0

	// txAmplitude scales the normalised [-1,1] waveform into int16, with
	// headroom below full scale so the ramped edges and any downstream mixing
	// don't clip.
	txAmplitude = 0.5
)

// truncateHead drops the first skip samples of a synchronised waveform — a late
// start (ADR 0032) — and re-ramps the new leading edge over nramp samples to
// suppress the key click a hard sample step would cause. skip ≤ 0 returns the
// waveform unchanged; skip at or past the end returns nil (nothing left to
// send). It mutates the returned slice's backing array, which the caller owns
// (a freshly-encoded per-transmission waveform).
func truncateHead(wave []int16, skip, nramp int) []int16 {
	if skip <= 0 {
		return wave
	}
	if skip >= len(wave) {
		return nil
	}
	out := wave[skip:]
	applyLeadingRamp(out, nramp)
	return out
}

// applyLeadingRamp fades in the first nramp samples of s with a raised cosine —
// the same edge taper Modulate applies to a full waveform — so a head-truncated
// transmission doesn't begin on a hard sample step.
func applyLeadingRamp(s []int16, nramp int) {
	if nramp > len(s) {
		nramp = len(s)
	}
	for i := 0; i < nramp; i++ {
		w := 0.5 * (1 - math.Cos(math.Pi*float64(i)/float64(nramp)))
		s[i] = int16(float64(s[i]) * w)
	}
}
