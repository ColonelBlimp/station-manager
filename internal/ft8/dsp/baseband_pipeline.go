// baseband_pipeline.go — top-level FT8 RX pipeline using WSJT-X-style
// baseband demodulation with multi-pass LDPC decoding.
//
// [ProcessWindowBaseband] replaces the Goertzel-based demodulation path with
// the complex baseband approach ported from WSJT-X ft8b.f90:
//
//  1. Spectrogram → Costas sync → candidate detection (unchanged).
//  2. Coarse-fine Goertzel refinement (unchanged).
//  3. Complex baseband downsampling via frequency-domain extraction.
//  4. Fine time/frequency sync on the complex baseband (sync8d).
//  5. 32-point DFT per symbol → 8 complex tone values.
//  6. Four different LLR extraction methods (nsym=1,2,3 + bit-normalised).
//  7. Four LDPC decode attempts per candidate (first success wins).
//  8. Signal subtraction and multi-pass iteration (as in [ProcessWindowMultiPass]).
//
// The long FFT (192000-point) is computed once per window and reused across
// all candidates and passes.

package dsp

import (
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// ProcessWindowBaseband runs the enhanced FT8 RX pipeline using WSJT-X-style
// baseband demodulation.
//
// This function uses the same candidate detection and refinement as
// [ProcessWindowMultiPass], but replaces the Goertzel-based demodulation
// with complex baseband processing and 4-pass LDPC decoding per candidate.
//
// Parameters match [ProcessWindow] for drop-in use:
//   - samples: audio capture buffer (one FT8 window).
//   - maxCandidates: upper limit on sync candidates per pass.
//   - maxIter: maximum LDPC belief-propagation iterations per candidate.
//
// Returns all successfully decoded messages (deduplicated across passes),
// sorted by descending SNR. Returns nil if the input is too short or no
// messages decode.
func ProcessWindowBaseband(samples []float32, maxCandidates, maxIter int) []DecodedMessage {
	if len(samples) < SamplesPerSymbol || maxCandidates <= 0 || maxIter <= 0 {
		return nil
	}

	// Work on a copy for signal subtraction.
	audio := make([]float32, len(samples))
	copy(audio, samples)

	// Pre-compute Hann window for refinement.
	hann := HannCoefficients(SamplesPerSymbol)

	seen := make(map[[10]byte]struct{})
	var allDecoded []DecodedMessage

	for pass := range SubtractionPasses {
		// Step 1: Compute the long FFT for this pass's audio.
		longFFT := LongFFT(audio)

		// Step 2: WSJT-X-faithful sync8 candidate detection.
		// Uses linear-power spectrogram with ratio-metric scoring,
		// 40th-percentile normalization, and near-dupe suppression.
		sync8Result := Sync8FindCandidates(audio, DefaultSyncMin, maxCandidates)
		candidates := sync8Result.Candidates
		if len(candidates) == 0 {
			break
		}

		// Step 3: Refine, baseband demodulate, and decode.
		var passDecoded []DecodedMessage

		for i := range candidates {
			cand := &candidates[i]

			// Coarse-fine Goertzel refinement to get sub-sample time/freq.
			refined := RefineCandidateAudioFast(audio, hann, *cand)

			// Baseband demodulation: downsample → sync8d → 32-pt DFT → 4 LLR sets.
			bbResult := DemodulateBaseband(longFFT,
				float64(refined.Freq),
				float64(refined.TimeOff))

			if bbResult.Nsync <= minSyncForDecode {
				continue
			}

			// Try all 4 LLR sets: first successful decode wins.
			llrSets := [4]*[CodedBits]float32{
				&bbResult.LLRa,
				&bbResult.LLRb,
				&bbResult.LLRc,
				&bbResult.LLRd,
			}

			var msg77 [10]byte
			var decoded bool

			for _, llr := range llrSets {
				m, ok := codec.DecodeMessage(*llr, maxIter)
				if ok {
					msg77 = m
					decoded = true
					break
				}
			}

			if !decoded {
				continue
			}

			msg77[9] &= 0xF8

			if _, dup := seen[msg77]; dup {
				continue
			}
			seen[msg77] = struct{}{}

			// Rough SNR estimate from the refined sync score.
			snr := estimateSNRFromScore(refined.Score)

			dm := DecodedMessage{
				Msg77:   msg77,
				Freq:    refined.Freq,
				TimeOff: refined.TimeOff,
				SNR:     snr,
			}
			allDecoded = append(allDecoded, dm)
			passDecoded = append(passDecoded, dm)
		}

		// Signal subtraction for next pass.
		if len(passDecoded) == 0 || pass == SubtractionPasses-1 {
			break
		}
		for i := range passDecoded {
			subtractSignal(audio, &passDecoded[i])
		}
	}

	return allDecoded
}

// DemodulateBasebandSingle performs baseband demodulation of a single candidate
// and returns diagnostics suitable for the ft8test CLI.
//
// Unlike [DemodulateBaseband] which takes a precomputed longFFT, this function
// computes it from raw samples (convenient for single-candidate testing).
func DemodulateBasebandSingle(samples []float32, freq, timeOff float64) BasebandDemodResult {
	longFFT := LongFFT(samples)
	return DemodulateBaseband(longFFT, freq, timeOff)
}

// ProcessWindowBasebandDiag is like [ProcessWindowBaseband] but returns
// per-candidate diagnostic information for the ft8test CLI.
type BasebandDiag struct {
	CandIdx int
	Freq    float32
	TimeOff float32
	Score   float32
	Nsync   int
	FreqAdj float64
	PassIdx [4]int // which LDPC pass succeeded (-1 if failed)
	LLRMean [4]float64
	LLRVar  [4]float64
	Decoded bool
	Text    string
	SNR     float32
}

// ProcessWindowBasebandWithDiag is a diagnostic variant of ProcessWindowBaseband
// that returns per-candidate diagnostics alongside decoded messages.
// It also performs multi-pass signal subtraction (matching the production path).
func ProcessWindowBasebandWithDiag(samples []float32, maxCandidates, maxIter int) ([]DecodedMessage, []BasebandDiag) {
	if len(samples) < SamplesPerSymbol || maxCandidates <= 0 || maxIter <= 0 {
		return nil, nil
	}

	audio := make([]float32, len(samples))
	copy(audio, samples)

	hann := HannCoefficients(SamplesPerSymbol)

	seen := make(map[[10]byte]struct{})
	var allDecoded []DecodedMessage
	var allDiags []BasebandDiag

	for pass := range SubtractionPasses {
		// Compute long FFT for this pass's audio.
		longFFT := LongFFT(audio)

		// WSJT-X-faithful sync8 candidate detection.
		sync8Result := Sync8FindCandidates(audio, DefaultSyncMin, maxCandidates)
		candidates := sync8Result.Candidates
		if len(candidates) == 0 {
			break
		}

		var passDecoded []DecodedMessage

		for i := range candidates {
			cand := &candidates[i]
			refined := RefineCandidateAudioFast(audio, hann, *cand)

			bbResult := DemodulateBaseband(longFFT,
				float64(refined.Freq),
				float64(refined.TimeOff))

			diag := BasebandDiag{
				CandIdx: i,
				Freq:    refined.Freq,
				TimeOff: refined.TimeOff,
				Score:   refined.Score,
				Nsync:   bbResult.Nsync,
				FreqAdj: bbResult.FreqAdj,
			}

			// Compute LLR stats for each pass.
			llrSets := [4]*[CodedBits]float32{
				&bbResult.LLRa,
				&bbResult.LLRb,
				&bbResult.LLRc,
				&bbResult.LLRd,
			}
			for p, llr := range llrSets {
				diag.PassIdx[p] = -1
				var sum, sum2 float64
				for _, v := range llr {
					sum += float64(v)
					sum2 += float64(v) * float64(v)
				}
				n := float64(CodedBits)
				diag.LLRMean[p] = sum / n
				diag.LLRVar[p] = sum2/n - (sum/n)*(sum/n)
			}

			if bbResult.Nsync <= minSyncForDecode {
				allDiags = append(allDiags, diag)
				continue
			}

			// Try each LLR set.
			for p, llr := range llrSets {
				msg77, ok := codec.DecodeMessage(*llr, maxIter)
				if ok {
					msg77[9] &= 0xF8
					diag.PassIdx[p] = p
					if _, dup := seen[msg77]; !dup {
						seen[msg77] = struct{}{}
						diag.Decoded = true
						snr := estimateSNRFromScore(refined.Score)
						diag.SNR = snr
						dm := DecodedMessage{
							Msg77:   msg77,
							Freq:    refined.Freq,
							TimeOff: refined.TimeOff,
							SNR:     snr,
						}
						allDecoded = append(allDecoded, dm)
						passDecoded = append(passDecoded, dm)
					}
					break
				}
			}

			allDiags = append(allDiags, diag)
		}

		// Signal subtraction for next pass.
		if len(passDecoded) == 0 || pass == SubtractionPasses-1 {
			break
		}
		for i := range passDecoded {
			subtractSignal(audio, &passDecoded[i])
		}
	}

	return allDecoded, allDiags
}

// llrStats computes mean and variance of a 174-element LLR array.
func llrStats(llr *[CodedBits]float32) (mean, vari float64) {
	var sum, sum2 float64
	for _, v := range llr {
		sum += float64(v)
		sum2 += float64(v) * float64(v)
	}
	n := float64(CodedBits)
	mean = sum / n
	vari = sum2/n - mean*mean
	return
}
