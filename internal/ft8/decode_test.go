package ft8

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// synthesizeFT8Audio produces a 15-second FT8 audio waveform that
// encodes the given message at centre frequency f0 (Hz). Used by
// the end-to-end synthetic test below to verify the entire decode
// pipeline (spectrogram → sync → downsample → demod → LDPC → parse)
// recovers a known signal.
//
// The synthesis is a simple "tone-bursts" approximation of FT8:
// each channel symbol gets dsp.NSPS audio samples at the symbol's
// FSK tone frequency. Real FT8 uses GFSK (continuous-phase, Gaussian-
// shaped frequency transitions); our test synth uses absolute-time
// sinusoids which is technically discontinuous-phase. The spectral
// difference is small enough that the demod's per-symbol FFT
// recovers the tones either way.
//
// The 7-tone Icos7 Costas array is placed at channel symbol
// positions 0..6, 36..42, 72..78 (per QEX paper §4) so the sync
// detector finds the signal.
func synthesizeFT8Audio(t *testing.T, codeword []byte, f0 float64) []float32 {
	t.Helper()
	if len(codeword) != codec.CodewordBits {
		t.Fatalf("codeword length %d, want %d", len(codeword), codec.CodewordBits)
	}

	out := make([]float32, dsp.NMAX)
	nominalStart := int(0.5 * dsp.Fs) // 6000 samples = 0.5 s

	codewordIdx := 0
	for chanSym := 0; chanSym < dsp.NN; chanSym++ {
		var tone uint8
		switch {
		case chanSym < 7:
			tone = dsp.Icos7[chanSym]
		case chanSym >= 36 && chanSym < 43:
			tone = dsp.Icos7[chanSym-36]
		case chanSym >= 72:
			tone = dsp.Icos7[chanSym-72]
		default:
			// Data symbol — 3 codeword bits via Gray map.
			var bits uint8
			for j := 0; j < dsp.DemodBitsPerSymbol; j++ {
				bits = (bits << 1) | codeword[codewordIdx]
				codewordIdx++
			}
			tone = dsp.GrayMap[bits]
		}

		freq := f0 + float64(tone)*dsp.Baud
		symStart := nominalStart + chanSym*dsp.NSPS
		for k := 0; k < dsp.NSPS; k++ {
			sampleT := float64(symStart+k) / dsp.Fs
			out[symStart+k] = float32(math.Sin(2 * math.Pi * freq * sampleT))
		}
	}
	return out
}

// codewordFromMessage builds the 174-bit FT8 codeword for an
// arbitrary 77-bit message. The standard pipeline: msg → CRC14 →
// 91-bit info → LDPCEncode → 174-bit codeword.
func codewordFromMessage(msg []byte) []byte {
	info := make([]byte, codec.InfoBits)
	copy(info[:codec.MessageBits], msg)
	crc := codec.CRC14(msg)
	for i := 0; i < codec.CRCBits; i++ {
		info[codec.MessageBits+i] = byte((crc >> (codec.CRCBits - 1 - i)) & 1)
	}
	return codec.LDPCEncode(info)
}

// TestDecode_NilAudioReturnsNil pins finding #6: Decode's doc
// claims nil-on-malformed-audio behaviour, and the implementation
// must not panic on nil or empty input. Before the fix, Decode
// passed nil straight to Spectrogram which panics on nil.
func TestDecode_NilAudioReturnsNil(t *testing.T) {
	for name, sample := range map[string][]float32{
		"nil_slice":   nil,
		"empty_slice": {},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Decode(%s) panicked: %v — want nil return", name, r)
				}
			}()
			if got := Decode(sample, DecodeOptions{}); got != nil {
				t.Errorf("Decode(%s) = %v, want nil", name, got)
			}
		})
	}
}

// TestDecode_SyntheticRoundTrip is the headline integration test:
// encode a known message bit-pattern to FT8 audio, feed it through
// the full Decode pipeline, verify the recovered message bits
// match. Validates every stage of the chain end-to-end.
//
// Caveats:
//   - "Synthetic" here means tone-burst audio (not the GFSK
//     waveform real FT8 transmitters produce). For sensitivity
//     this is an easier signal to decode than real FT8 — but the
//     demod's per-symbol FFT doesn't care about the inter-symbol
//     phase trajectory, so the pipeline either works or it doesn't.
//   - We only verify the message bits, not the higher-level
//     codec.Message struct fields — bit-correct decode implies the
//     parse layer's correctness (separately tested in
//     internal/ft8/codec).
func TestDecode_SyntheticRoundTrip(t *testing.T) {
	// A simple Type 1 (Std Msg) message: K1JT calls G4ABC, grid IO91.
	in := codec.Message{
		Type:  codec.MessageTypeStd,
		Call1: "K1JT",
		Call2: "G4ABC",
		Grid:  "IO91",
	}
	msgBits, err := codec.EncodeMessage(in)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	// EncodeMessage returns the 77-bit message body. Build a
	// codeword from it and synthesize audio at 1500 Hz (default
	// FT8 working centre).
	codeword := codewordFromMessage(msgBits)
	samples := synthesizeFT8Audio(t, codeword, 1500.0)

	results := Decode(samples, DecodeOptions{})
	if len(results) == 0 {
		t.Fatal("Decode returned no messages for synthetic signal")
	}

	// At least one of the decoded messages should match the input.
	want, err := codec.FormatMessage(in)
	if err != nil {
		t.Fatalf("FormatMessage(in): %v", err)
	}
	found := false
	for _, d := range results {
		if d.Text == want {
			found = true
			t.Logf("matched at freq=%.2f Hz, dt=%+.2f s, sync=%.2f", d.Freq, d.DT, d.SyncPower)
			break
		}
	}
	if !found {
		t.Errorf("decoded %d messages but none matched %q", len(results), want)
		for i, d := range results {
			t.Logf("  [%d] freq=%.2f dt=%+.2f sync=%.2f → %q", i, d.Freq, d.DT, d.SyncPower, d.Text)
		}
	}
}

// TestDecode_RealCapture_SmokeTest exercises the full Decode
// pipeline on a real WSJT-X-recorded capture (ft8_cap1.wav from
// the operator's go-ft8 testdata) when the fixture is present.
// Logs whatever Decode returns — this is the diagnostic moment
// that tells us whether our clean-room spec implementation finds
// real FT8 signals, and how the count compares to WSJT-X's 11
// decodes from the same audio.
//
// Skipped gracefully when the fixture is missing.
func TestDecode_RealCapture_SmokeTest(t *testing.T) {
	wavPath := resolveCapturePath(t, "ft8_cap1.wav")
	if wavPath == "" {
		t.Skip("no FT8 capture fixture available; set FT8_TEST_CORPUS or vendor testdata/")
	}

	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("ReadWAV(%q): %v", wavPath, err)
	}
	if data.SampleRate != dsp.Fs {
		t.Fatalf("sample rate = %d, want %g", data.SampleRate, dsp.Fs)
	}
	if data.Channels != 1 {
		t.Fatalf("channels = %d, want 1 (mono)", data.Channels)
	}

	results := Decode(data.Samples, DecodeOptions{})
	t.Logf("ft8_cap1.wav: decoded %d messages (WSJT-X 2.7.0 finds 11)", len(results))
	for i, d := range results {
		t.Logf("  [%d] %7.2f Hz  %+5.2f s  sync=%5.2f  %q", i, d.Freq, d.DT, d.SyncPower, d.Text)
	}
	// **Regression floor** — finding #3 from the post-Session-78
	// review: the previous version of this test only t.Logf'd the
	// count, so a regression to zero would still pass. Pin the
	// current decode count as a minimum threshold; bump when
	// sensitivity improvements land.
	//
	// The 1 baseline = the post-finding-#4 result on ft8_cap1.wav:
	// `SV2SIH ES2AJ -16` at 1862.5 Hz. Before finding #4 fixed the
	// float-leaky sync dedup, there was a spurious second decode
	// (`VE1WT K4GBI 73` at 1309.4 Hz DT=-0.32 with weak sync power
	// 3.58) that survived only because float-arithmetic rounding
	// let it past the dedup window. Its stronger near-twin
	// (DT=-0.40, sync 8.00) is what's left after integer dedup,
	// and the coarse-DT misalignment prevents it from decoding —
	// fine-timing alignment is the next-session work that should
	// recover it.
	//
	// WSJT-X 2.7.0 finds 11 in this file; our 1/11 baseline is the
	// floor that sensitivity work will lift.
	const ft8Cap1MinDecodes = 1
	if len(results) < ft8Cap1MinDecodes {
		t.Errorf("ft8_cap1.wav decoded %d messages; baseline floor is %d. A drop below baseline indicates a regression in the audio→messages pipeline.",
			len(results), ft8Cap1MinDecodes)
	}
}

// resolveCapturePath returns the path to a vendored or operator-
// supplied FT8 capture fixture, or "" if none is reachable.
//
// Resolution order:
//  1. $FT8_TEST_CORPUS/<name> — operator override for pointing tests
//     at a larger corpus outside the repo.
//  2. Vendored fixture in this package's testdata/ — the default
//     CI-friendly path. See internal/ft8/testdata/README.md for the
//     three fixtures' provenance.
//
// Returns "" when neither resolves; callers should Skip on that.
// Accepts testing.TB so both tests and benchmarks can call it.
func resolveCapturePath(t testing.TB, name string) string {
	t.Helper()
	if env := os.Getenv("FT8_TEST_CORPUS"); env != "" {
		p := filepath.Join(env, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	vendored := filepath.Join("testdata", name)
	if _, err := os.Stat(vendored); err == nil {
		return vendored
	}
	return ""
}

// BenchmarkDecode_RealCapture profiles the full Decode pipeline on
// a real WSJT-X capture. The hot path here is the budget question
// for live decoding: every 15-s FT8 slot, we get exactly 15 s to
// run this whole chain. The decoded-result count is incidental; the
// per-op time is what matters.
//
// Run with:
//
//	go test -bench BenchmarkDecode_RealCapture -benchtime 5x \
//	  -cpuprofile cpu.prof -memprofile mem.prof ./internal/ft8/
//	go tool pprof -top -cum cpu.prof
//	go tool pprof -top -alloc_space mem.prof
//
// Skipped when no fixture is reachable.
func BenchmarkDecode_RealCapture(b *testing.B) {
	wavPath := resolveCapturePath(b, "ft8_cap1.wav")
	if wavPath == "" {
		b.Skip("no FT8 capture fixture available")
	}
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		b.Fatalf("ReadWAV: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = Decode(data.Samples, DecodeOptions{})
	}
}
