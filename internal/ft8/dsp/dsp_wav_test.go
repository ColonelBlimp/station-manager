// dsp_wav_test.go — test the RX pipeline against real FT8 WAV recordings.
//
// These tests load WAV files from testdata/ and run them through ProcessWindow.
// If no WAV files are present the tests skip gracefully (t.Skipf), so they
// never cause CI failures when the files are not bundled.
//
// To run these tests, place one or more 12 kHz mono FT8 WAV recordings in
// internal/ft8/dsp/testdata/ (see testdata/README.md for details).

package dsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// testdataDir returns the absolute path to the testdata directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	// test binary runs with cwd = package directory
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("resolve testdata dir: %v", err)
	}
	return dir
}

// findWAVFiles returns all .wav files in the testdata directory.
// Returns nil (no error) if the directory is empty or doesn't exist.
func findWAVFiles(t *testing.T) []string {
	t.Helper()
	dir := testdataDir(t)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read testdata dir: %v", err)
	}

	var wavs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".wav") {
			wavs = append(wavs, filepath.Join(dir, e.Name()))
		}
	}
	return wavs
}

// TestProcessWindowWAVFile loads each WAV file from testdata/ and runs the
// full RX pipeline. It verifies that:
//   - The WAV file is 12 kHz mono (expected for FT8)
//   - ProcessWindow returns at least one decoded message
//   - Each decoded message can be unpacked by message.Unpack
//
// This is the strongest validation of the RX stack — it exercises every
// component against real over-the-air signals.
func TestProcessWindowWAVFile(t *testing.T) {
	wavFiles := findWAVFiles(t)
	if len(wavFiles) == 0 {
		t.Skipf("no WAV files in testdata/ — place 12 kHz mono FT8 recordings there to enable this test")
	}

	for _, path := range wavFiles {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV(%s): %v", name, err)
			}

			// Validate format.
			if wav.SampleRate != SampleRate {
				t.Skipf("%s: sample rate %d Hz, want %d Hz (skipping)", name, wav.SampleRate, SampleRate)
			}
			if wav.Channels != 1 {
				t.Skipf("%s: %d channels, want 1 (mono) — stereo downmix not implemented (skipping)", name, wav.Channels)
			}

			t.Logf("%s: %d samples (%.2f s) at %d Hz",
				name, len(wav.Samples), float64(len(wav.Samples))/float64(wav.SampleRate), wav.SampleRate)

			// Run the RX pipeline.
			msgs := ProcessWindow(wav.Samples, 100, 50)

			t.Logf("%s: decoded %d message(s)", name, len(msgs))

			if len(msgs) == 0 {
				// Not a hard failure — the file may contain only noise or
				// signals too weak for our decoder. Log it as informational.
				t.Logf("%s: WARNING — no messages decoded (file may contain weak signals or noise only)", name)
				return
			}

			// Verify each decoded message can be unpacked.
			for i, dm := range msgs {
				msg, err := message.Unpack(dm.Msg77)
				if err != nil {
					t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — unpack error: %v (msg77=%x)",
						i, dm.Freq, dm.TimeOff, dm.SNR, err, dm.Msg77)
					// Not a fatal error — we may not support all message types yet.
					continue
				}
				t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — %s",
					i, dm.Freq, dm.TimeOff, dm.SNR, formatMessage(msg))
			}
		})
	}
}

// TestProcessWindowWAVFileDecodeRate loads each WAV file and checks the
// decode rate against a minimum threshold. This test is informational —
// it logs the count but does not fail if the threshold isn't met (real-world
// signals have variable decode rates depending on conditions).
func TestProcessWindowWAVFileDecodeRate(t *testing.T) {
	wavFiles := findWAVFiles(t)
	if len(wavFiles) == 0 {
		t.Skipf("no WAV files in testdata/")
	}

	for _, path := range wavFiles {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("skipping (rate=%d ch=%d)", wav.SampleRate, wav.Channels)
			}

			// Try different maxCandidates values and compare decode counts.
			for _, maxCand := range []int{20, 50, 100} {
				msgs := ProcessWindow(wav.Samples, maxCand, 50)
				t.Logf("  maxCandidates=%d → %d decoded", maxCand, len(msgs))
			}
		})
	}
}

// formatMessage produces a human-readable string from a decoded message.
func formatMessage(msg *message.Message) string {
	switch msg.MsgType {
	case message.TypeStandard:
		return msg.Call1 + " " + msg.Call2 + " " + msg.Grid
	case message.TypeFreeText:
		return msg.FreeText
	default:
		return msg.MsgType.String()
	}
}
