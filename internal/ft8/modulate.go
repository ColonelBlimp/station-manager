package ft8

import (
	"math"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// FT8 GFSK modulator (ADR 0029 step b). go-ft8's EncodeStandardMessage stops at
// the 79-symbol tone sequence; this turns those tones into audio. The whole
// encode→modulate chain is offline round-trip-verifiable — feed the generated
// audio straight back through the shipped decoder (see modulate_test.go) — so
// the TX waveform is proven correct with ZERO RF before any audio device or PTT
// exists.
//
// Modulation is continuous-phase GFSK (Gaussian Frequency Shift Keying), the
// same scheme WSJT-X uses: each of the 8 tones is a frequency
// `offset + tone*6.25 Hz`, and transitions between symbols are smoothed by a
// Gaussian pulse (BT=2.0) rather than hard-switched, keeping the signal inside
// its ~50 Hz envelope. Phase is integrated sample-by-sample so the waveform is
// phase-continuous (no clicks at symbol boundaries), with a short raised-cosine
// ramp at the very start and end to suppress key clicks.

const (
	// txSamplesPerSymbol is the FT8 symbol length in 12 kHz samples. With
	// SampleRate=12000 this gives a 6.25 Hz tone spacing (= SampleRate/1920)
	// and a 0.16 s symbol. Mirrors go-ft8's internal ft8SamplesPerSymbol
	// (unexported); ADR 0029 notes go-ft8 should export the tone geometry so
	// RX occupancy + TX modulation share one source instead of both hardcoding.
	txSamplesPerSymbol = 1920

	// txGfskBT is the Gaussian pulse bandwidth-time product. 2.0 is the FT8
	// value (WSJT-X gen_ft8wave) — wider than the 0.3 of classic GFSK, chosen
	// for FT8's tight tone spacing.
	txGfskBT = 2.0

	// txModIndex is the modulation index h. FT8 uses h=1 (tone spacing equals
	// the symbol rate), so one tone step is exactly one 6.25 Hz bin.
	txModIndex = 1.0

	// txAmplitude scales the normalised [-1,1] waveform into int16, with
	// headroom below full scale so the ramped edges and any downstream mixing
	// don't clip.
	txAmplitude = 0.5
)

// Modulate turns an FT8 tone sequence (each value 0..7) into a normalised
// GFSK waveform in [-1, 1] at the given audio offset, sampled at
// goft8.SampleRate. The returned length is (len(tones)+2)*txSamplesPerSymbol:
// the Gaussian pulse spans three symbols, so the smoothed signal runs one
// symbol-width past each end of the nominal tone sequence.
//
// Pure and deterministic. offsetHz is the audio frequency of tone 0 (the base
// tone) — the same reference the decoder reports as FreqHz, so an offset chosen
// from the clear-offset picker round-trips back as the decoded frequency.
func Modulate(tones []uint8, offsetHz float64) []float32 {
	nsps := txSamplesPerSymbol
	nsym := len(tones)
	if nsym == 0 {
		return nil
	}
	nwave := (nsym + 2) * nsps
	fs := float64(goft8.SampleRate)

	// GFSK frequency pulse over three symbols. For a run of equal symbols the
	// overlapping pulses sum to unity, so a steady tone produces a constant
	// frequency offset; at transitions the sum slews smoothly between tones.
	pulse := make([]float64, 3*nsps)
	c := math.Pi * math.Sqrt(2.0/math.Ln2)
	for i := range pulse {
		tt := (float64(i) - 1.5*float64(nsps)) / float64(nsps)
		pulse[i] = 0.5 * (math.Erf(c*txGfskBT*(tt+0.5)) - math.Erf(c*txGfskBT*(tt-0.5)))
	}

	// Per-sample angular frequency: the carrier plus each symbol's pulse-shaped
	// tone contribution. dphiPeak is one tone-step of phase advance per sample.
	dphi := make([]float64, nwave)
	base := 2 * math.Pi * offsetHz / fs
	for k := range dphi {
		dphi[k] = base
	}
	dphiPeak := 2 * math.Pi * txModIndex / float64(nsps)
	for j := 0; j < nsym; j++ {
		ib := j * nsps
		tone := float64(tones[j])
		for i := 0; i < 3*nsps; i++ {
			dphi[ib+i] += dphiPeak * pulse[i] * tone
		}
	}

	// Integrate phase → samples.
	wave := make([]float32, nwave)
	phi := 0.0
	for k := 0; k < nwave; k++ {
		wave[k] = float32(math.Sin(phi))
		phi += dphi[k]
		if phi > 2*math.Pi {
			phi -= 2 * math.Pi
		}
	}

	// Raised-cosine ramp on the leading/trailing edges to suppress key clicks.
	nramp := nsps / 8
	for i := 0; i < nramp; i++ {
		w := float32(0.5 * (1 - math.Cos(math.Pi*float64(i)/float64(nramp))))
		wave[i] *= w
		wave[nwave-1-i] *= w
	}
	return wave
}

// EncodeToSlot encodes a standard FT8 message and lays its modulated waveform
// into a full 15-second slot buffer (SlotSamples int16 at goft8.SampleRate),
// starting dtSec into the slot with silence before and after. This is the
// offline building block the round-trip test uses and the shape the live TX
// path (steps c/d) will stream; offsetHz is the base-tone audio frequency.
//
// Errors only if the message isn't an encodable standard message
// (goft8.EncodeStandardMessage rejects free text / compound calls).
func EncodeToSlot(text string, offsetHz, dtSec float64) ([]int16, error) {
	const op errors.Op = "ft8.EncodeToSlot"
	enc, err := goft8.EncodeStandardMessage(text)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("encode %q", text)
	}
	wave := Modulate(enc.Tones[:], offsetHz)

	slot := make([]int16, SlotSamples)
	start := int(dtSec * float64(goft8.SampleRate))
	if start < 0 {
		start = 0
	}
	for i, v := range wave {
		k := start + i
		if k >= len(slot) {
			break // waveform runs past the slot end — truncate the silent tail
		}
		slot[k] = int16(float64(v) * txAmplitude * math.MaxInt16)
	}
	return slot, nil
}
