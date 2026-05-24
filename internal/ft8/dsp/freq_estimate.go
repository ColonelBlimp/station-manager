package dsp

import "math"

// EstimateFreqOffsetCostas estimates the residual frequency offset
// (in Hz) of an FT8 signal whose nominal carrier is at candFreq and
// whose nominal slot-relative TX start is at dt seconds. The estimate
// is derived from the 21 Costas anchor symbols — known tones at known
// positions — and is independent of the data payload.
//
// **Why this exists.** The sync detector quantises candidate
// frequencies to the spectrogram's 3.125 Hz bin grid (Fs/NFFT1); a
// real signal sits at candFreq ± up to half the bin spacing
// (~1.56 Hz residual). For matched-filter signal SUBTRACTION
// (dsp.SubtractSignal) that residual offset accumulates ~6 complete
// phase rotations of mismatch across the 12.64-second TX window,
// breaking the constant-phase assumption the matched filter relies
// on and collapsing the amplitude estimate to near zero. Refining
// the candidate's frequency before synthesising the template fixes
// that.
//
// **Algorithm (clean-room, derived from FT8's signal model in QEX
// paper §4):** For each Costas anchor symbol at position p with
// known tone Icos7[p%7], compute a one-bin DTFT at the anchor's
// expected frequency, referenced to the TX start (NOT absolute
// sample 0 — the synth's intrinsic phase trajectory anchors at TX
// start, so the demod kernel must too):
//
//	y_p = Σ_{n=0}^{NSPS-1} audio[start_p + n] · exp(-i·2π·f_expected·(start_p+n−txStart)/Fs)
//
// If the actual signal sits at candFreq + Δf relative to TX start,
// each anchor's complex amplitude advances in phase by 2π·Δf·NSPS/Fs
// between consecutive anchors within a Costas block, with the
// tone-change contribution `−2π·Δtone·(p+1)` between anchor p and
// p+1 collapsing to an integer multiple of 2π (since Δtone is
// integer and Baud·NSPS/Fs = 1). Modulo 2π it vanishes, leaving Δf
// alone in the adjacent-pair angle.
//
// Within each of the three Costas blocks, summing the adjacent-pair
// products y_{p+1}·conj(y_p) yields:
//
//	Σ |y|² · exp(i·2π·Δf·NSPS/Fs)
//
// — eighteen such pairs across the three blocks combine coherently.
// The angle of the total sum gives the per-symbol phase advance, and
// Δf = angle · Fs / (2π·NSPS).
//
// **Unambiguous range:** the atan2 result lives in (−π, π], so
// |Δf| < Fs/(2·NSPS) = 3.125 Hz. The sync grid leaves at most
// ~1.56 Hz residual, comfortably inside the unambiguous range.
//
// Pairs are intra-block only (gap = NSPS samples). Cross-block gaps
// (5.76 s = 36 symbols) would wrap several times for any Δf > 0.087
// Hz — useless for estimation. Eighteen intra-block pairs is enough
// for a robust estimate at SNRs where the candidate decodes at all.
//
// Returns 0 if the audio is too short to cover the TX window, if
// txStart would land before sample 0, or if the anchors carry no
// measurable energy (all-zero audio, off-band candidate, etc.).
func EstimateFreqOffsetCostas(audio []float32, candFreq, dt float64) float64 {
	txStart := int(math.Round((SynthSlotStartSeconds + dt) * Fs))
	if txStart < 0 {
		return 0
	}
	// Need full 79-symbol window covered by audio.
	if txStart+NN*NSPS > len(audio) {
		return 0
	}

	const twoPiOverFs = 2.0 * math.Pi / Fs

	// Demodulate each Costas anchor to a single complex amplitude
	// at its expected frequency. Layout: 3 blocks × 7 anchors.
	type cpx struct{ re, im float64 }
	var anchors [NumCostasBlocks * CostasTonesPerBlock]cpx

	for block := 0; block < NumCostasBlocks; block++ {
		blockStartSym := block * CostasBlockStrideSymbols
		for symInBlock := 0; symInBlock < CostasTonesPerBlock; symInBlock++ {
			channelSym := blockStartSym + symInBlock
			symStartRel := channelSym * NSPS // TX-relative
			expectedTone := float64(Icos7[symInBlock])
			fExpected := candFreq + expectedTone*Baud

			var re, im float64
			for n := 0; n < NSPS; n++ {
				// rel is the TX-relative sample index. The synth's
				// phase trajectory is integrated from TX start, so
				// the demod kernel must reference the same origin;
				// otherwise a tone-change phase term `−2π·Δtone·
				// (txStart mod NSPS)/NSPS` per pair appears that
				// doesn't vanish mod 2π and biases the estimate.
				rel := symStartRel + n
				x := float64(audio[txStart+rel])
				phase := twoPiOverFs * fExpected * float64(rel)
				s, c := math.Sincos(phase)
				// y += x · exp(-i·phase) = x·(cos − i·sin)
				re += x * c
				im -= x * s
			}
			anchors[block*CostasTonesPerBlock+symInBlock] = cpx{re, im}
		}
	}

	// Sum intra-block adjacent-pair products y_{k+1}·conj(y_k).
	// For complex a = ar + i·ai, b = br + i·bi:
	//   b·conj(a) = (br·ar + bi·ai) + i·(bi·ar − br·ai)
	var sumRe, sumIm float64
	for block := 0; block < NumCostasBlocks; block++ {
		base := block * CostasTonesPerBlock
		for k := 0; k < CostasTonesPerBlock-1; k++ {
			a := anchors[base+k]
			b := anchors[base+k+1]
			sumRe += b.re*a.re + b.im*a.im
			sumIm += b.im*a.re - b.re*a.im
		}
	}

	// No coherent energy → can't estimate (all-zero audio, way
	// off-band, etc.). Tolerance well below any realistic anchor
	// magnitude squared (~ (NSPS·A/2)² ≈ 10⁶ for A=1, NSPS=1920).
	if sumRe*sumRe+sumIm*sumIm < 1e-12 {
		return 0
	}

	phaseAdvance := math.Atan2(sumIm, sumRe)
	return phaseAdvance * Fs / (2.0 * math.Pi * float64(NSPS))
}
