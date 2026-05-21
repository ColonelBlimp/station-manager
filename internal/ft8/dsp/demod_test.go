package dsp

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// synthSymbolTone places a single 8-FSK tone in the baseband at
// the given channel symbol position. tone in [0, 8) selects which
// FSK tone is "on"; the 32 samples for that symbol carry a unit-
// amplitude complex exponential at bin = tone.
//
// baseband must already be allocated to a length covering the
// nominal-start + 79 channel symbol slots.
func synthSymbolTone(baseband []complex128, chanSym int, tone uint8) {
	const nominalStartSamples = int(nominalTXStartSeconds * Fs2)
	start := nominalStartSamples + chanSym*SymbolFFTSize
	// Complex exponential at bin `tone` in the 32-point FFT — i.e.
	// frequency tone × (Fs2 / SymbolFFTSize) = tone × 6.25 Hz.
	for k := 0; k < SymbolFFTSize; k++ {
		phase := 2 * math.Pi * float64(tone) * float64(k) / float64(SymbolFFTSize)
		baseband[start+k] = complex(math.Cos(phase), math.Sin(phase))
	}
}

// makeSyntheticBaseband produces an NFFT2-sample complex baseband
// containing the FT8 signal for the given 174 codeword bits. The
// sync symbols (Costas blocks at indices 0..6, 36..42, 72..78) get
// zero-amplitude tones — Demodulate ignores those positions so
// their content doesn't matter for these tests.
func makeSyntheticBaseband(t *testing.T, codeword []byte) []complex128 {
	t.Helper()
	if len(codeword) != DemodLLRCount {
		t.Fatalf("codeword length %d, want %d", len(codeword), DemodLLRCount)
	}

	baseband := make([]complex128, NFFT2)
	llrIdx := 0
	for _, span := range [2][2]int{
		{SymbolDataStart1, SymbolDataEnd1},
		{SymbolDataStart2, SymbolDataEnd2},
	} {
		for chanSym := span[0]; chanSym <= span[1]; chanSym++ {
			// Pack 3 codeword bits into a binary index (MSB first).
			var bits uint8
			for j := 0; j < DemodBitsPerSymbol; j++ {
				bits = (bits << 1) | codeword[llrIdx]
				llrIdx++
			}
			// Map binary → tone via Gray code (QEX paper §4 Table 3).
			tone := GrayMap[bits]
			synthSymbolTone(baseband, chanSym, tone)
		}
	}
	return baseband
}

// TestDemodulate_OutputLength pins the output-length contract.
func TestDemodulate_OutputLength(t *testing.T) {
	// All-zero baseband: demodulator runs but outputs zeros.
	bb := make([]complex128, NFFT2)
	llrs := Demodulate(bb, 0, DefaultLLRScale)
	if len(llrs) != DemodLLRCount {
		t.Errorf("len(llrs) = %d, want %d", len(llrs), DemodLLRCount)
	}
}

// TestDemodulate_CleanSignalProducesSignedLLRs is the headline
// correctness check. Synthesize a baseband from a known 174-bit
// codeword, demodulate, verify every LLR has the correct sign:
//
//	bit=0 in codeword ⟹ LLR > 0 (LDPC-literature convention)
//	bit=1 in codeword ⟹ LLR < 0
//
// Tests several deterministic codewords (all-zero, all-one,
// alternating) plus a few random ones to catch any per-bit-
// position sign confusion.
func TestDemodulate_CleanSignalProducesSignedLLRs(t *testing.T) {
	cases := []struct {
		name     string
		codeword []byte
	}{
		{"all_zero", make([]byte, DemodLLRCount)},
		{"all_one", func() []byte {
			b := make([]byte, DemodLLRCount)
			for i := range b {
				b[i] = 1
			}
			return b
		}()},
		{"alternating", func() []byte {
			b := make([]byte, DemodLLRCount)
			for i := range b {
				b[i] = byte(i & 1)
			}
			return b
		}()},
		{"random_seed_42", randomCodeword(42, 43)},
		{"random_seed_99", randomCodeword(99, 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseband := makeSyntheticBaseband(t, tc.codeword)
			llrs := Demodulate(baseband, 0, DefaultLLRScale)
			if len(llrs) != DemodLLRCount {
				t.Fatalf("len(llrs) = %d, want %d", len(llrs), DemodLLRCount)
			}
			for i, b := range tc.codeword {
				if b == 0 && llrs[i] <= 0 {
					t.Errorf("LLR[%d] = %g, want >0 (bit=0 in codeword)", i, llrs[i])
				}
				if b == 1 && llrs[i] >= 0 {
					t.Errorf("LLR[%d] = %g, want <0 (bit=1 in codeword)", i, llrs[i])
				}
			}
		})
	}
}

// TestDemodulate_RejectsShortBaseband pins the input-length guard.
// Baseband too short for all 58 data symbols → nil.
func TestDemodulate_RejectsShortBaseband(t *testing.T) {
	// 100 samples — way too short to cover any meaningful symbol
	// extraction.
	bb := make([]complex128, 100)
	llrs := Demodulate(bb, 0, DefaultLLRScale)
	if llrs != nil {
		t.Errorf("Demodulate returned non-nil from short baseband; want nil")
	}
}

// TestDemodulate_FullPipelineRoundTrip exercises message → CRC14
// → LDPCEncode → synthesised baseband → Demodulate → LDPCDecode →
// message. The end-to-end "bits ↔ baseband ↔ bits" round-trip,
// minus the WAV / spectrogram / sync front-end.
//
// This is the strongest single regression test: it verifies the
// LLR sign convention, the Gray demap, the symbol positioning,
// and the LDPC + CRC chain all line up.
func TestDemodulate_FullPipelineRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		msg  []byte
	}{
		{"all_zero_msg", make([]byte, codec.MessageBits)},
		{"random_seed_1", randomMessage(1, 2)},
		{"random_seed_42", randomMessage(42, 43)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Message → CRC14 → 91-bit info word.
			info := make([]byte, codec.InfoBits)
			copy(info[:codec.MessageBits], tc.msg)
			crc := codec.CRC14(tc.msg)
			for i := 0; i < codec.CRCBits; i++ {
				info[codec.MessageBits+i] = byte((crc >> (codec.CRCBits - 1 - i)) & 1)
			}
			// LDPCEncode → 174-bit codeword.
			codeword := codec.LDPCEncode(info)

			// Synthesised baseband: tones per Gray-mapped 3-bit groups.
			baseband := makeSyntheticBaseband(t, codeword)

			// Demodulate → LLRs.
			llrs := Demodulate(baseband, 0, DefaultLLRScale)
			if len(llrs) != codec.CodewordBits {
				t.Fatalf("len(llrs) = %d, want %d", len(llrs), codec.CodewordBits)
			}

			// LDPCDecode → recovered 77-bit message.
			recovered, ok := codec.LDPCDecode(llrs, codec.LDPCMaxIterationsDefault)
			if !ok {
				t.Fatal("LDPCDecode returned ok=false on clean synthetic baseband")
			}

			// Compare to the original message.
			for i := 0; i < codec.MessageBits; i++ {
				if recovered[i] != tc.msg[i] {
					t.Errorf("recovered msg bit %d = %d, want %d", i, recovered[i], tc.msg[i])
					break
				}
			}
		})
	}
}

// TestDemodulate_HandlesDTOffset verifies the candidate-time-offset
// (dt) parameter shifts the symbol extraction window correctly. A
// non-zero dt should still demodulate a synthetic signal that's
// positioned at the corresponding offset.
func TestDemodulate_HandlesDTOffset(t *testing.T) {
	const dt = 0.04 // one spectrogram step late (40 ms)

	codeword := randomCodeword(13, 14)

	// Build baseband as if TX arrived at dt seconds late: synthesise
	// at chanSym + dtBasebandSamples.
	dtSamples := int(math.Round(dt * Fs2))
	const nominalStartSamples = int(nominalTXStartSeconds * Fs2)

	baseband := make([]complex128, NFFT2)
	llrIdx := 0
	for _, span := range [2][2]int{
		{SymbolDataStart1, SymbolDataEnd1},
		{SymbolDataStart2, SymbolDataEnd2},
	} {
		for chanSym := span[0]; chanSym <= span[1]; chanSym++ {
			var bits uint8
			for j := 0; j < DemodBitsPerSymbol; j++ {
				bits = (bits << 1) | codeword[llrIdx]
				llrIdx++
			}
			tone := GrayMap[bits]
			start := nominalStartSamples + dtSamples + chanSym*SymbolFFTSize
			if start+SymbolFFTSize > len(baseband) {
				continue
			}
			for k := 0; k < SymbolFFTSize; k++ {
				phase := 2 * math.Pi * float64(tone) * float64(k) / float64(SymbolFFTSize)
				baseband[start+k] = complex(math.Cos(phase), math.Sin(phase))
			}
		}
	}

	llrs := Demodulate(baseband, dt, DefaultLLRScale)
	if len(llrs) != DemodLLRCount {
		t.Fatalf("len(llrs) = %d, want %d", len(llrs), DemodLLRCount)
	}
	// Sign check — same as the clean-signal test, but with dt
	// applied to both synthesis and demodulation.
	for i, b := range codeword {
		if b == 0 && llrs[i] <= 0 {
			t.Errorf("LLR[%d] = %g, want >0 with dt=%g", i, llrs[i], dt)
		}
		if b == 1 && llrs[i] >= 0 {
			t.Errorf("LLR[%d] = %g, want <0 with dt=%g", i, llrs[i], dt)
		}
	}
}

// BenchmarkDemodulate measures one candidate's worth of demod work.
// 58 symbols × one 32-point FFT each. Should be quick — 32-point
// FFTs are sub-microsecond on the operator's hardware.
func BenchmarkDemodulate(b *testing.B) {
	codeword := randomCodeword(101, 202)
	baseband := makeSyntheticBaseband(&testing.T{}, codeword)
	b.ResetTimer()
	for range b.N {
		_ = Demodulate(baseband, 0, DefaultLLRScale)
	}
}

// randomCodeword generates a deterministic 174-bit codeword for
// tests that don't need the LDPC-encoded shape (sign-convention
// tests etc.). PCG seeded with two integers for reproducibility.
func randomCodeword(seed1, seed2 uint64) []byte {
	r := rand.New(rand.NewPCG(seed1, seed2))
	out := make([]byte, DemodLLRCount)
	for i := range out {
		out[i] = byte(r.UintN(2))
	}
	return out
}

// randomMessage generates a deterministic 77-bit message for the
// full-pipeline tests.
func randomMessage(seed1, seed2 uint64) []byte {
	r := rand.New(rand.NewPCG(seed1, seed2))
	out := make([]byte, codec.MessageBits)
	for i := range out {
		out[i] = byte(r.UintN(2))
	}
	return out
}

// Ensure the import is used even when not all tests run.
var _ = audio.FFT
