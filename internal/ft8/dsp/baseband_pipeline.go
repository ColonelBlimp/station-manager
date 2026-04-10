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

// maxHardErrors is the WSJT-X nharderrors threshold (ft8b.f90 line 422).
// After a successful CRC-14 decode, nharderrors counts how many of the 174
// coded bits disagree with the hard decisions from the input LLRs. For a
// legitimate decode, the LDPC code corrects a modest number of errors
// (typically < 30). For false decodes from noise, roughly half the bits
// disagree (nharderrors ≈ 87). Rejecting nharderrors > 36 eliminates
// nearly all CRC-14 false alarms.
const maxHardErrors = 36

// ProcessWindowBaseband runs the enhanced FT8 RX pipeline using WSJT-X-style
// baseband demodulation.
//
// This function uses the same candidate detection and refinement as
// [ProcessWindowMultiPass], but replaces the Goertzel-based demodulation
// with complex baseband processing and 4-pass LDPC decoding per candidate.
//
// When an [APContext] is provided, up to 4 additional AP decode passes
// are attempted after the 4 regular LLR passes fail, matching the WSJT-X
// ft8b.f90 AP decoding strategy.
//
// Parameters match [ProcessWindow] for drop-in use:
//   - samples: audio capture buffer (one FT8 window).
//   - maxCandidates: upper limit on sync candidates per pass.
//   - maxIter: maximum LDPC belief-propagation iterations per candidate.
//   - ap: optional AP context for a priori decoding (nil to disable).
//
// Returns all successfully decoded messages (deduplicated across passes),
// sorted by descending SNR. Returns nil if the input is too short or no
// messages decode.
func ProcessWindowBaseband(samples []float32, maxCandidates, maxIter int, ap *APContext) []DecodedMessage {
	if len(samples) < SamplesPerSymbol || maxCandidates <= 0 || maxIter <= 0 {
		return nil
	}

	// Work on a copy for signal subtraction.
	audio := make([]float32, len(samples))
	copy(audio, samples)

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

		// Step 3: Baseband demodulate and decode.
		// WSJT-X passes sync8 candidates directly to ft8b — no
		// intermediate Goertzel refinement. Sync8d inside
		// DemodulateBaseband performs its own fine time/freq search.
		var passDecoded []DecodedMessage

		for i := range candidates {
			cand := &candidates[i]

			// Baseband demodulation: downsample → sync8d → 32-pt DFT → LLR sets.
			bbResult := DemodulateBaseband(longFFT,
				float64(cand.Freq),
				float64(cand.TimeOff))

			if bbResult.Nsync <= minSyncForDecode {
				continue
			}

			// Try 3 LLR sets: first successful decode wins.
			// Matches WSJT-X ft8b.f90 ipass=1-3 (llra, llrb, llrc).
			// bmetd (nsym=1, bit-normalised) is omitted to match WSJT-X's
			// 3-pass approach and reduce false alarm probability.
			llrSets := [3]*[CodedBits]float32{
				&bbResult.LLRa,
				&bbResult.LLRb,
				&bbResult.LLRc,
			}

			var msg77 [10]byte
			var decoded bool

			for p, llr := range llrSets {
				// Primary LLR (bmeta) gets full OSD order-2 for maximum
				// sensitivity. Secondary LLRs (bmetb, bmetc) use OSD order-1
				// to reduce false alarm probability (45× fewer codeword tests).
				var m [10]byte
				var ok bool
				if p == 0 {
					m, ok = codec.DecodeMessage(*llr, maxIter)
				} else {
					m, ok = codec.DecodeMessageShallow(*llr, maxIter)
				}
				if ok && codec.ValidateMsg77(m) {
					// Hard error check (WSJT-X ft8b.f90 line 422).
					if countHardErrors(m, llr) > maxHardErrors {
						continue
					}
					msg77 = m
					decoded = true
					break
				}
			}

			// If regular passes failed, try AP passes.
			if !decoded && ap != nil {
				m, apOK := tryAPPasses(&bbResult.LLRa, maxIter, ap)
				if apOK && codec.ValidateMsg77(m) {
					if countHardErrors(m, &bbResult.LLRa) <= maxHardErrors {
						msg77 = m
						decoded = true
					}
				}
			}

			if !decoded {
				continue
			}

			msg77[9] &= 0xF8

			// Post-decode SNR computation using the per-symbol s8 array
			// and re-encoded tone sequence (WSJT-X ft8b.f90 lines 438–452).
			itone := msg77ToTones(msg77)
			snr := computePostDecodeSNR(&bbResult.S8, &itone)

			// SNR+sync sanity check: reject likely false decodes.
			// Matches WSJT-X ft8b.f90 line 456.
			if bbResult.Nsync <= 10 && snr < -24.0 {
				continue
			}

			if _, dup := seen[msg77]; dup {
				continue
			}
			seen[msg77] = struct{}{}

			dm := DecodedMessage{
				Msg77:   msg77,
				Freq:    cand.Freq,
				TimeOff: cand.TimeOff,
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
	CandIdx   int
	Freq      float32
	TimeOff   float32
	Score     float32
	Nsync     int
	FreqAdj   float64
	PassIdx   [4]int // which LDPC pass succeeded (-1 if failed)
	LLRMean   [4]float64
	LLRVar    [4]float64
	Decoded   bool
	APType    int // AP type that decoded (0 = no AP, >0 = AP type)
	Text      string
	SNR       float32
	Is1       int     // sync hits in Costas block 1
	Is2       int     // sync hits in Costas block 2
	Is3       int     // sync hits in Costas block 3
	IBest     int     // refined baseband sample index
	ValidSyms int     // symbols within NP2 bound (0–79)
	RawSigma  float64 // raw bmeta σ before normalization
}

// ProcessWindowBasebandWithDiag is a diagnostic variant of ProcessWindowBaseband
// that returns per-candidate diagnostics alongside decoded messages.
// It also performs multi-pass signal subtraction (matching the production path).
func ProcessWindowBasebandWithDiag(samples []float32, maxCandidates, maxIter int, ap *APContext) ([]DecodedMessage, []BasebandDiag) {
	if len(samples) < SamplesPerSymbol || maxCandidates <= 0 || maxIter <= 0 {
		return nil, nil
	}

	audio := make([]float32, len(samples))
	copy(audio, samples)

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

			// WSJT-X passes sync8 candidates directly to ft8b — no Goertzel refinement.
			bbResult := DemodulateBaseband(longFFT,
				float64(cand.Freq),
				float64(cand.TimeOff))

			diag := BasebandDiag{
				CandIdx:   i,
				Freq:      cand.Freq,
				TimeOff:   cand.TimeOff,
				Score:     cand.Score,
				Nsync:     bbResult.Nsync,
				FreqAdj:   bbResult.FreqAdj,
				Is1:       bbResult.Is1,
				Is2:       bbResult.Is2,
				Is3:       bbResult.Is3,
				IBest:     bbResult.IBest,
				ValidSyms: bbResult.ValidSyms,
				RawSigma:  bbResult.RawSigma,
			}

			// Compute LLR stats for each pass.
			llrSets := [3]*[CodedBits]float32{
				&bbResult.LLRa,
				&bbResult.LLRb,
				&bbResult.LLRc,
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
			// Mark unused 4th pass as uncomputed.
			diag.PassIdx[3] = -1

			if bbResult.Nsync <= minSyncForDecode {
				allDiags = append(allDiags, diag)
				continue
			}

			// Try each LLR set.
			for p, llr := range llrSets {
				var msg77 [10]byte
				var ok bool
				if p == 0 {
					msg77, ok = codec.DecodeMessage(*llr, maxIter)
				} else {
					msg77, ok = codec.DecodeMessageShallow(*llr, maxIter)
				}
				if ok && codec.ValidateMsg77(msg77) {
					// Hard error check (WSJT-X ft8b.f90 line 422).
					if countHardErrors(msg77, llr) > maxHardErrors {
						continue
					}
					msg77[9] &= 0xF8
					diag.PassIdx[p] = p
					// Post-decode SNR from s8 array (WSJT-X ft8b.f90 lines 438–452).
					itone := msg77ToTones(msg77)
					snr := computePostDecodeSNR(&bbResult.S8, &itone)
					// SNR+sync sanity check (WSJT-X ft8b.f90 line 456).
					if bbResult.Nsync <= 10 && snr < -24.0 {
						continue
					}
					if _, dup := seen[msg77]; !dup {
						seen[msg77] = struct{}{}
						diag.Decoded = true
						diag.SNR = snr
						dm := DecodedMessage{
							Msg77:   msg77,
							Freq:    cand.Freq,
							TimeOff: cand.TimeOff,
							SNR:     snr,
						}
						allDecoded = append(allDecoded, dm)
						passDecoded = append(passDecoded, dm)
					}
					break
				}
			}

			// If regular passes failed, try AP passes.
			if !diag.Decoded && ap != nil {
				msg77, apDecoded := tryAPPasses(&bbResult.LLRa, maxIter, ap)
				if apDecoded && codec.ValidateMsg77(msg77) {
					if countHardErrors(msg77, &bbResult.LLRa) > maxHardErrors {
						// filtered by hard error check
					} else {
						msg77[9] &= 0xF8
						itone := msg77ToTones(msg77)
						snr := computePostDecodeSNR(&bbResult.S8, &itone)
						// SNR+sync sanity check.
						if bbResult.Nsync <= 10 && snr < -24.0 {
							// filtered
						} else if _, dup := seen[msg77]; !dup {
							seen[msg77] = struct{}{}
							diag.Decoded = true
							diag.APType = -1 // indicates AP decoded (detailed type tracking TBD)
							diag.SNR = snr
							dm := DecodedMessage{
								Msg77:   msg77,
								Freq:    cand.Freq,
								TimeOff: cand.TimeOff,
								SNR:     snr,
							}
							allDecoded = append(allDecoded, dm)
							passDecoded = append(passDecoded, dm)
						}
					}
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

// tryAPPasses attempts AP decode passes on a candidate using the nsym=1 LLR
// array (llra) as the base. It iterates through the AP types defined by the
// APContext's QSO progress state.
//
// For AP type 1 (CQ), OSD depth is reduced to order-0 to lower the false
// alarm rate. CQ constrains only 32 of 77 message bits — full OSD order-2
// with this few constraints produces ~91× more false alarms. For AP types
// that constrain more bits (types 2–6), full OSD order-2 is used.
//
// Returns the decoded 77-bit message and true if any AP pass succeeds.
func tryAPPasses(baseLLR *[CodedBits]float32, maxIter int, ap *APContext) (msg77 [10]byte, ok bool) {
	qp := ap.QSOProgress
	if qp < 0 || qp > 5 {
		qp = 0
	}

	numPasses := nappasses[qp]
	for passIdx := range numPasses {
		apType := naptypes[qp][passIdx]
		if apType == APTypeNone {
			continue
		}

		// Guard: AP types requiring mycall.
		if apType >= APTypeMyCall && !ap.hasMyCall {
			continue
		}
		// Guard: AP types requiring dxcall.
		if apType >= APTypeMyDx && !ap.hasDxCall {
			continue
		}

		llrz, apmask, apOK := applyAPPass(baseLLR, apType, ap)
		if !apOK {
			continue
		}

		// Reduce OSD depth for AP type 1 (CQ) to lower false alarm rate.
		// CQ constrains only 32 of 77 message bits — full OSD order-2
		// inflates false positives. Types 2+ constrain more bits and
		// benefit from deeper OSD.
		ndeep := 2
		if apType == APTypeCQ {
			ndeep = 0 // order-0 only (no flip search)
		}

		m, decOK := codec.DecodeMessageAPWithDepth(llrz, apmask, maxIter, ndeep)
		if decOK {
			return m, true
		}
	}

	return msg77, false
}

// countHardErrors computes the number of coded bits where the decoded
// codeword disagrees with the LLR hard decisions. This matches WSJT-X's
// nharderrors = count((2*cw-1)*llr .lt. 0.0) from decode174_91.f90.
//
// For our LLR convention (positive = bit likely 0):
//   - Hard decision: bit = 0 if llr >= 0, bit = 1 if llr < 0
//   - A "hard error" is where decoded_bit != hard_decision
//
// The msg77 is re-encoded to get the full 174-bit codeword, then compared.
func countHardErrors(msg77 [10]byte, llr *[CodedBits]float32) int {
	// Re-encode to get the 174-bit codeword (packed in 22 bytes, MSB-first).
	cw := codec.EncodeMessage(msg77)

	nharderrors := 0
	for i := range CodedBits {
		// Extract coded bit i from the packed codeword.
		cwBit := (cw[i/8] >> uint(7-i%8)) & 1

		// Hard decision from LLR: bit = 0 if llr >= 0, bit = 1 if llr < 0.
		var hdec uint8
		if llr[i] < 0 {
			hdec = 1
		}

		if cwBit != hdec {
			nharderrors++
		}
	}
	return nharderrors
}
