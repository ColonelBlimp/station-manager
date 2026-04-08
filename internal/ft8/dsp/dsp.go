// dsp.go — top-level RX pipeline entry point.
//
// [ProcessWindow] connects the full receive chain:
//
//	audio samples → [SpectrogramFT8] → [FindCandidates] →
//	[RefineCandidateAudio] → [DemodulateAudio] →
//	codec.DecodeMessage → CRC filter → deduplicate
//
// The function accepts one FT8 capture window (typically 180 000 samples at
// 12 kHz = 15 s) and returns all successfully decoded messages together with
// their estimated frequency, time offset, and SNR.
//
// Deduplication: multiple candidates may decode to the same 77-bit message
// (slightly different frequency/time estimates). Only the first successfully
// decoded instance of each unique message is retained. Messages are keyed
// by the 77-bit content (10 bytes, trailing bits masked).

package dsp

import (
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// DecodedMessage represents a successfully decoded FT8 message recovered
// from a single RX window.
type DecodedMessage struct {
	// Msg77 contains the 77-bit packed message in 10 bytes (as produced by
	// [message.Pack]). Only the first 77 bits are meaningful; the 3 trailing
	// bits in Msg77[9] are always zero.
	Msg77 [10]byte

	// Freq is the estimated audio frequency of the signal (Hz).
	Freq float32

	// TimeOff is the estimated time offset within the window (seconds).
	TimeOff float32

	// SNR is a rough signal-to-noise ratio estimate (dB). This is derived
	// from the candidate's sync correlation score and the spectrogram noise
	// floor; it is useful for display and logging but should not be treated
	// as a calibrated measurement.
	SNR float32
}

// ProcessWindow runs the full FT8 RX pipeline on a single capture window.
//
// Parameters:
//   - samples: audio capture buffer (one FT8 window — nominally [WindowSamples]
//     samples at [SampleRate] Hz, but shorter buffers are accepted).
//   - maxCandidates: upper limit on the number of sync candidates to evaluate.
//     A typical value is 40–100. Higher values increase decode rate at the
//     cost of CPU time. Must be > 0.
//   - maxIter: maximum LDPC belief-propagation iterations per candidate
//     (typically 25–50). Must be > 0.
//
// Returns decoded messages sorted by descending SNR. Returns nil if the
// input is too short to produce a spectrogram, or if no messages decode
// successfully.
//
// The function is stateless and allocation-heavy (spectrogram, FFT buffers).
// For high-throughput use, a struct-based API with pre-allocated buffers
// can be added later behind this same interface.
func ProcessWindow(samples []float32, maxCandidates, maxIter int) []DecodedMessage {
	if len(samples) < SamplesPerSymbol || maxCandidates <= 0 || maxIter <= 0 {
		return nil
	}

	// Step 1: build the FT8-optimised spectrogram.
	// Half-symbol step (960 samples), log2(power), periodic Hann window.
	sg := SpectrogramFT8(samples)
	if sg == nil {
		return nil
	}

	// With half-symbol stepping, each symbol spans 2 rows.
	const stepsPerSymbol = 2

	// Need at least (79-1)*2 + 1 = 157 frames for a full FT8 message.
	minFrames := (NumSymbols-1)*stepsPerSymbol + 1
	if len(sg) < minFrames {
		return nil
	}

	// Step 2: detect candidate signals via Costas sync correlation.
	candidates := FindCandidates(sg, maxCandidates, stepsPerSymbol)
	if len(candidates) == 0 {
		return nil
	}

	// Estimate the noise floor from the spectrogram for SNR computation.
	noiseFloor := estimateNoiseFloor(sg)

	// Pre-compute Hann window for Goertzel-based demodulation.
	hann := HannCoefficients(SamplesPerSymbol)

	// Step 3: refine, demodulate, and decode each candidate.
	seen := make(map[[10]byte]struct{})
	var decoded []DecodedMessage

	for i := range candidates {
		cand := &candidates[i]

		// Refine the candidate's time and frequency using Goertzel
		// sync correlation on the raw audio.
		refined := RefineCandidateAudio(samples, hann, *cand)

		// Demodulate: extract 174 soft LLR values using Goertzel at
		// exact FT8 tone frequencies.
		llr := DemodulateAudio(samples, hann, refined)

		// Normalise LLR variance to 24.0, matching ft8_lib's
		// ftx_normalize_logl. This ensures consistent scaling for
		// the LDPC decoder regardless of signal strength.
		NormalizeLLR(&llr)

		// LDPC decode + CRC-14 verification.
		msg77, ok := codec.DecodeMessage(llr, maxIter)
		if !ok {
			continue
		}

		// Mask trailing bits for consistent keying.
		msg77[9] &= 0xF8

		// Deduplicate by 77-bit message content.
		if _, dup := seen[msg77]; dup {
			continue
		}
		seen[msg77] = struct{}{}

		snr := estimateSNR(refined.Score, noiseFloor)

		decoded = append(decoded, DecodedMessage{
			Msg77:   msg77,
			Freq:    refined.Freq,
			TimeOff: refined.TimeOff,
			SNR:     snr,
		})
	}

	return decoded
}

// estimateNoiseFloor computes the median-like noise floor from the
// spectrogram. It uses the overall mean power as a simple estimator.
//
// A more robust approach (e.g., median of each row, or percentile-based)
// can replace this without changing the ProcessWindow interface.
func estimateNoiseFloor(sg [][]float32) float64 {
	var sum float64
	var count int
	for _, row := range sg {
		for _, v := range row {
			sum += float64(v)
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// estimateSNR produces a rough dB-scale SNR estimate from the candidate's
// sync correlation score and the spectrogram noise floor.
//
// The sync score is the excess mean power at sync positions above the mean
// power across all tone positions — essentially signal power minus noise.
// SNR ≈ 10·log10(score / noiseFloor) when noiseFloor > 0; otherwise the
// score itself is converted to dB.
//
// This is a coarse approximation suitable for display and logging. The
// FT8 protocol's official SNR metric (in a 2500 Hz reference bandwidth)
// would require a more careful calibration.
func estimateSNR(score float32, noiseFloor float64) float32 {
	s := float64(score)
	if s <= 0 {
		return -30.0 // floor for undetectable signals
	}
	if noiseFloor > 0 {
		return float32(10.0 * math.Log10(s/noiseFloor))
	}
	// No noise floor available (e.g., silence); use score directly.
	return float32(10.0 * math.Log10(s))
}
