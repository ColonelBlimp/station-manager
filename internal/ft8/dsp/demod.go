// demod.go — soft demodulation of FT8 8-FSK symbols to LLR values.
//
// This is the critical bridge between the DSP front-end (spectrogram,
// candidate detection) and the LDPC decoder ([codec.Decode]). For each
// detected [Candidate], the demodulator extracts the 8-tone power values
// at each of the 58 data symbol positions and converts them to 174
// log-likelihood ratios (3 bits × 58 symbols) using Gray-code-aware
// soft demapping.
//
// LLR sign convention (matches [codec.Decode]):
//   - Positive LLR → bit more likely 0
//   - Negative LLR → bit more likely 1
//   - Magnitude → confidence
//
// The demodulation uses the log-sum-exp trick for numerical stability when
// computing the LLR from log-domain tone powers.

package dsp

import "math"

// logFloor is the minimum value used for log(power) when power is zero or
// negative. This prevents -Inf from propagating into the LLR computation,
// which would produce NaN when both groups have -Inf log-power.
//
// The value -30 corresponds to power ≈ 9.4×10⁻¹⁴, effectively zero in the
// context of audio spectral power.
const logFloor = -30.0

// Precomputed tone groups for LLR computation. For each bit position
// (0 = MSB, 2 = LSB), the tone indices whose Gray-decoded binary value
// has that bit equal to 0 or 1.
//
// Derived from grayDecode = {0, 1, 3, 2, 7, 6, 4, 5}:
//
//	Tone 0 → 000  Tone 1 → 001  Tone 2 → 011  Tone 3 → 010
//	Tone 4 → 111  Tone 5 → 110  Tone 6 → 100  Tone 7 → 101
var (
	bit0Tones = [BitsPerSymbol][4]int{
		{0, 1, 2, 3}, // bit 0 (MSB) = 0
		{0, 1, 6, 7}, // bit 1 = 0
		{0, 3, 5, 6}, // bit 2 (LSB) = 0
	}
	bit1Tones = [BitsPerSymbol][4]int{
		{4, 5, 6, 7}, // bit 0 (MSB) = 1
		{2, 3, 4, 5}, // bit 1 = 1
		{1, 2, 4, 7}, // bit 2 (LSB) = 1
	}
)

// Demodulate extracts 174 soft log-likelihood ratio (LLR) values from the
// spectrogram at the given candidate position. The returned LLRs are
// suitable for direct input to codec.Decode or codec.DecodeMessage.
//
// The candidate's Freq and TimeOff are converted back to spectrogram bin
// and frame indices. The spectrogram must be a uniform matrix (all rows
// same length) and large enough to contain the candidate's full 79-symbol
// span; otherwise a zero-filled LLR array is returned.
func Demodulate(spectrogram [][]float32, cand Candidate) [CodedBits]float32 {
	var llr [CodedBits]float32

	if len(spectrogram) == 0 || len(spectrogram[0]) == 0 {
		return llr
	}

	nBins := len(spectrogram[0])
	fftSize := 2 * (nBins - 1)
	binWidth := float32(SampleRate) / float32(fftSize)

	// Convert candidate position back to spectrogram indices.
	baseBin := int(math.Round(float64(cand.Freq) / float64(binWidth)))
	timeFrame := int(math.Round(float64(cand.TimeOff) / float64(SymbolPeriod)))

	// Bounds check.
	if baseBin < 0 || baseBin+NumTones > nBins {
		return llr
	}
	if timeFrame < 0 || timeFrame+NumSymbols > len(spectrogram) {
		return llr
	}

	llrIdx := 0

	// Iterate over data symbol positions (7–35 and 43–71).
	for pos := Sync1Start + SyncLen; pos < Sync2Start; pos++ {
		demodSymbol(spectrogram[timeFrame+pos], baseBin, llr[:], &llrIdx)
	}
	for pos := Sync2Start + SyncLen; pos < Sync3Start; pos++ {
		demodSymbol(spectrogram[timeFrame+pos], baseBin, llr[:], &llrIdx)
	}

	return llr
}

// demodSymbol extracts 3 LLR values from a single spectrogram row at the
// given base bin, appending them to llr starting at *idx.
//
// For each of the 3 bit positions (MSB to LSB), the 8 tones are partitioned
// by the Gray-decoded bit value. The LLR is:
//
//	LLR = logSumExp(s[k] for k where bit=0) − logSumExp(s[k] for k where bit=1)
//
// where s[k] = log(power[k]).
func demodSymbol(row []float32, baseBin int, llr []float32, idx *int) {
	// Extract log-powers for the 8 tones.
	var s [NumTones]float64
	for k := range NumTones {
		p := float64(row[baseBin+k])
		if p > 0 {
			s[k] = math.Log(p)
		} else {
			s[k] = logFloor
		}
	}

	// Compute LLR for each of the 3 bits (MSB to LSB).
	for b := range BitsPerSymbol {
		g0 := bit0Tones[b]
		g1 := bit1Tones[b]
		lse0 := logSumExp4(s[g0[0]], s[g0[1]], s[g0[2]], s[g0[3]])
		lse1 := logSumExp4(s[g1[0]], s[g1[1]], s[g1[2]], s[g1[3]])
		llr[*idx] = float32(lse0 - lse1)
		*idx++
	}
}

// logSumExp4 computes log(exp(a) + exp(b) + exp(c) + exp(d)) using the
// log-sum-exp trick to avoid numerical overflow/underflow.
//
//	result = max(a,b,c,d) + log(exp(a−max) + exp(b−max) + exp(c−max) + exp(d−max))
func logSumExp4(a, b, c, d float64) float64 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	if d > m {
		m = d
	}
	return m + math.Log(
		math.Exp(a-m)+math.Exp(b-m)+math.Exp(c-m)+math.Exp(d-m))
}
