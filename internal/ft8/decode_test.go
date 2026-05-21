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
// pipeline on each of the three vendored WSJT-X captures. Each
// capture has a known WSJT-X-2.7.0 main-loop decode count (cap1=11,
// cap2=14, cap3=23 per testdata/README.md); we record SM's count
// alongside as a regression baseline. Drops below baseline fail
// the test; sensitivity improvements that lift the count above
// baseline are bumped in via this test's floor constants.
//
// Skipped per-capture when the fixture is missing.
func TestDecode_RealCapture_SmokeTest(t *testing.T) {
	cases := []struct {
		// name is the WAV file vendored under testdata/.
		name string
		// wsjtxDecodes is WSJT-X 2.7.0 main-loop's decode count on
		// this capture, from testdata/README.md. Reference only —
		// we don't expect parity with a clean-room implementation.
		wsjtxDecodes int
		// minDecodes is SM's regression-floor baseline. Captured
		// post-Phase-1-optimization (Session 80, commit 7f0cab02):
		// the FFT-caching change doesn't affect decode count, only
		// runtime. Bump when sensitivity improvements (OSD, fine-
		// freq, fine-timing, K-scale) land.
		minDecodes int
	}{
		{"ft8_cap1.wav", 11, 1},
		{"ft8_cap2.wav", 14, 4},
		{"ft8_cap3.wav", 23, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wavPath := resolveCapturePath(t, tc.name)
			if wavPath == "" {
				t.Skipf("fixture %s not available; set FT8_TEST_CORPUS or vendor testdata/", tc.name)
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
			t.Logf("%s: decoded %d messages (WSJT-X 2.7.0 finds %d)", tc.name, len(results), tc.wsjtxDecodes)
			for i, d := range results {
				t.Logf("  [%d] %7.2f Hz  %+5.2f s  sync=%5.2f  %q", i, d.Freq, d.DT, d.SyncPower, d.Text)
			}
			if len(results) < tc.minDecodes {
				t.Errorf("%s decoded %d messages; baseline floor is %d. A drop below baseline indicates a regression in the audio→messages pipeline.",
					tc.name, len(results), tc.minDecodes)
			}
		})
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
// each of the three vendored FT8 captures. The hot path is the
// budget question for live decoding: every 15-s FT8 slot, we get
// exactly 15 s to run this whole chain.
//
// The three captures have progressively more traffic per WSJT-X's
// own decode counts (cap1=11 signals, cap2=14, cap3=23) — running
// all three confirms the per-candidate cost model holds across
// different busy-band conditions, not just the single-fixture
// happy path.
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
	captures := []string{"ft8_cap1.wav", "ft8_cap2.wav", "ft8_cap3.wav"}
	for _, name := range captures {
		b.Run(name, func(b *testing.B) {
			wavPath := resolveCapturePath(b, name)
			if wavPath == "" {
				b.Skipf("fixture %s not available", name)
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
		})
	}
}
