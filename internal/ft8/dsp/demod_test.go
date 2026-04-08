// demod_test.go — tests for FT8 soft demodulation.

package dsp

import (
	"encoding/hex"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// --- max4 tests ---

func TestMax4EqualValues(t *testing.T) {
	got := max4(5.0, 5.0, 5.0, 5.0)
	if got != 5.0 {
		t.Errorf("max4(5,5,5,5) = %g, want 5", got)
	}
}

func TestMax4OneDominant(t *testing.T) {
	got := max4(100, -30, -30, -30)
	if got != 100 {
		t.Errorf("max4(100,-30,-30,-30) = %g, want 100", got)
	}
}

func TestMax4KnownValue(t *testing.T) {
	got := max4(-10, -5, -20, -1)
	if got != -1.0 {
		t.Errorf("max4(-10,-5,-20,-1) = %g, want -1", got)
	}
}

func TestMax4NegativeValues(t *testing.T) {
	got := max4(-10.0, -20.0, -15.0, -12.0)
	if got != -10.0 {
		t.Errorf("max4(-10,-20,-15,-12) = %g, want -10", got)
	}
}

// --- Bit group consistency ---

// TestBitGroupsMatchGrayDecode verifies that the precomputed bit0Tones
// and bit1Tones tables are consistent with the grayDecode table.
func TestBitGroupsMatchGrayDecode(t *testing.T) {
	for b := range BitsPerSymbol {
		for _, k := range bit0Tones[b] {
			binary := grayDecode[k]
			bit := (binary >> uint(2-b)) & 1
			if bit != 0 {
				t.Errorf("bit0Tones[%d] contains tone %d, but grayDecode[%d]=%d has bit %d = %d",
					b, k, k, binary, b, bit)
			}
		}
		for _, k := range bit1Tones[b] {
			binary := grayDecode[k]
			bit := (binary >> uint(2-b)) & 1
			if bit != 1 {
				t.Errorf("bit1Tones[%d] contains tone %d, but grayDecode[%d]=%d has bit %d = %d",
					b, k, k, binary, b, bit)
			}
		}
	}

	// Each bit position must partition all 8 tones into exactly 4 + 4.
	for b := range BitsPerSymbol {
		seen := make(map[int]bool)
		for _, k := range bit0Tones[b] {
			seen[k] = true
		}
		for _, k := range bit1Tones[b] {
			if seen[k] {
				t.Errorf("tone %d appears in both bit0Tones[%d] and bit1Tones[%d]", k, b, b)
			}
			seen[k] = true
		}
		if len(seen) != NumTones {
			t.Errorf("bit %d: only %d of %d tones covered", b, len(seen), NumTones)
		}
	}
}

// --- Demodulate edge cases ---

func TestDemodulateNil(t *testing.T) {
	cand := Candidate{Freq: 1000, TimeOff: 0}
	llr := Demodulate(nil, cand)
	for i, v := range llr {
		if v != 0 {
			t.Errorf("nil spectrogram: llr[%d] = %g, want 0", i, v)
			break
		}
	}
}

// TestDemodulateRejectsLogDomainSpectrogram verifies that Demodulate detects
// a log-domain spectrogram (negative values, as produced by SpectrogramFT8)
// and returns zero LLRs rather than silently computing log(log2(power)).
func TestDemodulateRejectsLogDomainSpectrogram(t *testing.T) {
	// Build a spectrogram filled with negative values typical of
	// Log2PowerSpectrum output (log2(power) is negative for power < 1).
	sg := makeSpectrogram(93, 1025, -10.0)
	binWidth := spectrogramBinWidth(1025)
	cand := Candidate{Freq: 100 * binWidth, TimeOff: 0}

	llr := Demodulate(sg, cand)
	for i, v := range llr {
		if v != 0 {
			t.Errorf("log-domain spectrogram: llr[%d] = %g, want 0", i, v)
			break
		}
	}
}

func TestDemodulateOutOfBounds(t *testing.T) {
	sg := makeSpectrogram(93, 1025, 0)

	// Candidate with frequency beyond the spectrogram.
	cand := Candidate{Freq: 99999, TimeOff: 0}
	llr := Demodulate(sg, cand)
	for i, v := range llr {
		if v != 0 {
			t.Errorf("out-of-bounds freq: llr[%d] = %g, want 0", i, v)
			break
		}
	}

	// Candidate with time offset beyond the spectrogram.
	cand2 := Candidate{Freq: 1000, TimeOff: 999}
	llr2 := Demodulate(sg, cand2)
	for i, v := range llr2 {
		if v != 0 {
			t.Errorf("out-of-bounds time: llr[%d] = %g, want 0", i, v)
			break
		}
	}
}

func TestDemodulateZeroPower(t *testing.T) {
	// All-zero spectrogram → all LLRs should be zero (equal log-powers
	// in both groups cancel).
	sg := makeSpectrogram(93, 1025, 0)
	cand := Candidate{Freq: 1000, TimeOff: 0}
	llr := Demodulate(sg, cand)

	for i, v := range llr {
		if !approxEq(v, 0, 0.01) {
			t.Errorf("zero power: llr[%d] = %g, want ~0", i, v)
			break
		}
	}
}

// --- Single-tone LLR sign ---

// TestDemodulateSingleToneSigns verifies that when all power is at a single
// tone, the LLR signs correctly indicate the Gray-decoded bit values.
func TestDemodulateSingleToneSigns(t *testing.T) {
	const nBins = 1025
	binWidth := spectrogramBinWidth(nBins)
	const baseBin = 100
	const power = float32(100.0)

	for tone := range uint8(NumTones) {
		t.Run(toneLabel(tone), func(t *testing.T) {
			sg := makeSpectrogram(93, nBins, 0)

			// Place power at this tone for all data symbol positions.
			placeDataTone(sg, 0, baseBin, tone, power)

			cand := Candidate{
				Freq:    float32(baseBin) * binWidth,
				TimeOff: 0,
			}
			llr := Demodulate(sg, cand)

			// Expected bits from Gray decoding.
			binary := grayDecode[tone]

			// Check the first symbol's 3 LLRs (all symbols are identical).
			for b := range BitsPerSymbol {
				bit := (binary >> uint(2-b)) & 1
				llrVal := llr[b]
				if bit == 0 && llrVal <= 0 {
					t.Errorf("tone %d, bit %d: expected positive LLR (bit=0), got %g",
						tone, b, llrVal)
				}
				if bit == 1 && llrVal >= 0 {
					t.Errorf("tone %d, bit %d: expected negative LLR (bit=1), got %g",
						tone, b, llrVal)
				}
			}
		})
	}
}

// --- Known codeword LLR signs ---

// TestDemodulateLLRSignsMatchCodeword encodes a known codeword, places the
// correct tone for each data symbol, and verifies that every LLR sign
// matches the corresponding coded bit.
func TestDemodulateLLRSignsMatchCodeword(t *testing.T) {
	cwHex := "deadbeef123456789abcdee9f4dfae4ab374d7b4c33c"
	cwBytes, err := hex.DecodeString(cwHex)
	if err != nil {
		t.Fatal(err)
	}
	var cw [CodewordBytes]byte
	copy(cw[:], cwBytes)

	// Map to symbols.
	syms := BitsToSymbols(cw)

	// Build spectrogram with power at the correct tone per data symbol.
	const nBins = 1025
	const baseBin = 150
	const power = float32(200.0)
	binWidth := spectrogramBinWidth(nBins)

	sg := makeSpectrogram(93, nBins, 0)
	placeDataSymbols(sg, 0, baseBin, syms, power)

	cand := Candidate{
		Freq:    float32(baseBin) * binWidth,
		TimeOff: 0,
	}
	llr := Demodulate(sg, cand)

	// Verify each LLR sign matches the corresponding codeword bit.
	for i := range CodedBits {
		bit := (cw[i/8] >> uint(7-i%8)) & 1
		if bit == 0 && llr[i] <= 0 {
			t.Errorf("bit %d: coded=0, LLR=%g (expected positive)", i, llr[i])
		}
		if bit == 1 && llr[i] >= 0 {
			t.Errorf("bit %d: coded=1, LLR=%g (expected negative)", i, llr[i])
		}
	}
}

// --- Full round-trip: EncodeMessage → symbols → demod → DecodeMessage ---

// TestDemodulateDecodeRoundTrip verifies the complete chain from a packed
// 77-bit message through LDPC encoding, symbol mapping, spectrogram
// synthesis, demodulation, and LDPC decoding back to the original message.
func TestDemodulateDecodeRoundTrip(t *testing.T) {
	// A known 77-bit message (the "deadbeef" test vector).
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}

	// TX chain: pack → CRC → LDPC encode → symbol map.
	cw := codec.EncodeMessage(msg77)
	var cwDSP [CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	syms := BitsToSymbols(cwDSP)

	// Build spectrogram with power at the correct tone per data symbol.
	const nBins = 1025
	const baseBin = 200
	const power = float32(500.0)
	binWidth := spectrogramBinWidth(nBins)

	sg := makeSpectrogram(93, nBins, 0)
	placeDataSymbols(sg, 0, baseBin, syms, power)

	// RX chain: demodulate → decode.
	cand := Candidate{
		Freq:    float32(baseBin) * binWidth,
		TimeOff: 0,
	}
	llr := Demodulate(sg, cand)

	decoded, ok := codec.DecodeMessage(llr, 50)
	if !ok {
		t.Fatal("DecodeMessage failed")
	}

	// Compare the 77-bit message (mask trailing bits).
	want := msg77
	want[9] &= 0xF8
	got := decoded
	got[9] &= 0xF8
	if got != want {
		t.Errorf("round-trip mismatch:\n  want %x\n  got  %x", want, got)
	}
}

// --- LLR magnitude ---

// TestDemodulateLLRMagnitude verifies that stronger signals produce larger
// LLR magnitudes. A background noise level is set so that the LLR
// differences stay within the [LLRClampMax] range, allowing the test to
// distinguish weak from strong signals without hitting the clamp ceiling.
func TestDemodulateLLRMagnitude(t *testing.T) {
	const nBins = 1025
	const baseBin = 100
	const noise = float32(1.0) // background noise level
	binWidth := spectrogramBinWidth(nBins)
	cand := Candidate{Freq: float32(baseBin) * binWidth, TimeOff: 0}

	// Weak signal (power=5 on tone 3, noise=1 everywhere else).
	sg1 := makeSpectrogram(93, nBins, noise)
	placeDataTone(sg1, 0, baseBin, 3, 5.0)
	llr1 := Demodulate(sg1, cand)

	// Strong signal (power=100 on tone 3, noise=1 everywhere else).
	sg2 := makeSpectrogram(93, nBins, noise)
	placeDataTone(sg2, 0, baseBin, 3, 100.0)
	llr2 := Demodulate(sg2, cand)

	// The strong signal should produce larger magnitude LLRs.
	mag1 := llrMagnitudeSum(llr1)
	mag2 := llrMagnitudeSum(llr2)
	if mag2 <= mag1 {
		t.Errorf("strong signal magnitude %g ≤ weak signal %g", mag2, mag1)
	}
}

// --- Uniform power → zero LLR ---

// TestDemodulateUniformPower verifies that when all 8 tones have equal
// power, every LLR is zero (no information about which tone was sent).
func TestDemodulateUniformPower(t *testing.T) {
	const nBins = 1025
	const baseBin = 100
	binWidth := spectrogramBinWidth(nBins)

	sg := makeSpectrogram(93, nBins, 0)
	// Place equal power at all 8 tones for every data symbol.
	for tone := range uint8(NumTones) {
		placeDataTone(sg, 0, baseBin, tone, 50.0)
	}

	cand := Candidate{Freq: float32(baseBin) * binWidth, TimeOff: 0}
	llr := Demodulate(sg, cand)

	for i, v := range llr {
		if !approxEq(v, 0, 0.01) {
			t.Errorf("uniform power: llr[%d] = %g, want ~0", i, v)
			break
		}
	}
}

// --- Test helpers ---

// placeDataTone sets a single tone at the given power for all 58 data
// symbol positions (7–35 and 43–71) in the spectrogram.
func placeDataTone(sg [][]float32, timeOff, baseBin int, tone uint8, power float32) {
	for pos := Sync1Start + SyncLen; pos < Sync2Start; pos++ {
		sg[timeOff+pos][baseBin+int(tone)] = power
	}
	for pos := Sync2Start + SyncLen; pos < Sync3Start; pos++ {
		sg[timeOff+pos][baseBin+int(tone)] = power
	}
}

// placeDataSymbols places the correct tone for each data symbol (from
// BitsToSymbols output) in the spectrogram at the given position.
func placeDataSymbols(sg [][]float32, timeOff, baseBin int, syms [NumDataSyms]uint8, power float32) {
	symIdx := 0
	for pos := Sync1Start + SyncLen; pos < Sync2Start; pos++ {
		sg[timeOff+pos][baseBin+int(syms[symIdx])] = power
		symIdx++
	}
	for pos := Sync2Start + SyncLen; pos < Sync3Start; pos++ {
		sg[timeOff+pos][baseBin+int(syms[symIdx])] = power
		symIdx++
	}
}

// toneLabel returns a label for a tone index (for sub-test names).
func toneLabel(tone uint8) string {
	return string(rune('0' + tone))
}

// llrMagnitudeSum returns the sum of absolute values of all LLRs.
func llrMagnitudeSum(llr [CodedBits]float32) float64 {
	var sum float64
	for _, v := range llr {
		if v < 0 {
			sum -= float64(v)
		} else {
			sum += float64(v)
		}
	}
	return sum
}
