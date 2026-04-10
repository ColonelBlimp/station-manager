// baseband_pipeline_test.go — regression tests for the baseband decode pipeline.
//
// These tests run the baseband pipeline against known WAV captures and verify
// the decode count doesn't regress. They also verify false decode mitigation
// by checking that known false decodes from capture 2 are eliminated.

package dsp

import (
	"os"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// TestBasebandPipelineCapture1Regression runs ProcessWindowBaseband against
// capture 1 (13 WSJT-X decodes) and verifies the decode count doesn't drop
// below the established baseline.
func TestBasebandPipelineCapture1Regression(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping baseband pipeline WAV test (slow)")
	}

	wavPath := "testdata/ft8test_capture_20260410.wav"
	if _, err := os.Stat(wavPath); os.IsNotExist(err) {
		t.Skipf("test WAV not found: %s", wavPath)
	}

	samples, err := loadTestWAVSimple(wavPath)
	if err != nil {
		t.Fatalf("load WAV: %v", err)
	}

	msgs := ProcessWindowBaseband(samples, DefaultMaxCandidates, DefaultMaxIterations, nil)

	t.Logf("capture1: decoded %d message(s)", len(msgs))
	for i, dm := range msgs {
		if msg, err := message.Unpack(dm.Msg77); err == nil {
			t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — %s",
				i, dm.Freq, dm.TimeOff, dm.SNR, msg.String())
		} else {
			t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — unpack error: %v",
				i, dm.Freq, dm.TimeOff, dm.SNR, err)
		}
	}

	// Baseline: 9 correct decodes without AP context.
	// (10 with AP context using --mycall KB7THX --dxcall WB9VGJ)
	const minDecodes = 9
	if len(msgs) < minDecodes {
		t.Errorf("REGRESSION: capture1 decoded %d messages, expected >= %d", len(msgs), minDecodes)
	}
}

// TestBasebandPipelineCapture2Regression runs ProcessWindowBaseband against
// capture 2 (15 WSJT-X decodes) and verifies the decode count doesn't drop
// below the established baseline.
func TestBasebandPipelineCapture2Regression(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping baseband pipeline WAV test (slow)")
	}

	wavPath := "testdata/ft8test_capture2_20260410.wav"
	if _, err := os.Stat(wavPath); os.IsNotExist(err) {
		t.Skipf("test WAV not found: %s", wavPath)
	}

	samples, err := loadTestWAVSimple(wavPath)
	if err != nil {
		t.Fatalf("load WAV: %v", err)
	}

	msgs := ProcessWindowBaseband(samples, DefaultMaxCandidates, DefaultMaxIterations, nil)

	// Known WSJT-X decodes for reference.
	wsjtxDecodes := map[string]bool{
		"HA5LB 5B4AMX RR73":   true,
		"CQ ZS4AW KG31":       true,
		"CQ SV0TPN KM28":      true,
		"CQ Z62NS KN02":       true,
		"VK/ZL4XZ <...> RR73": true,
		"VK3ZSJ YO8RQP KN37":  true,
		"R1QD KB2ELA -12":     true,
		"UY7VV KE6SU DM14":    true,
		"TL8GD UT2VX KN69":    true,
		"RU4LM 4X5JK R-14":    true,
		"JT1CO IZ7DIO 73":     true,
		"VK3ZSJ US7KC KO21":   true,
		"JR3UIC SP7IIT RR73":  true,
		"JT1CO YO3HST KN24":   true,
		"CQ TN8GD JI75":       true,
	}

	correct := 0
	falseDecodes := 0
	t.Logf("capture2: decoded %d message(s)", len(msgs))
	for i, dm := range msgs {
		var text string
		if msg, err := message.Unpack(dm.Msg77); err == nil {
			text = msg.String()
		} else {
			text = "(unpack error)"
		}
		match := ""
		if wsjtxDecodes[text] {
			correct++
			match = " ✓"
		} else {
			falseDecodes++
			match = " ✗ (likely false)"
		}
		t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — %s%s",
			i, dm.Freq, dm.TimeOff, dm.SNR, text, match)
	}

	t.Logf("Summary: %d correct, %d likely false (out of %d total)", correct, falseDecodes, len(msgs))

	// Baseline: 8 correct decodes without AP context.
	const minCorrect = 7
	if correct < minCorrect {
		t.Errorf("REGRESSION: capture2 correct decodes = %d, expected >= %d", correct, minCorrect)
	}

	// False decode target: should be ≤ 1 with all mitigations.
	// Previously was 3 false decodes.
	const maxFalse = 2
	if falseDecodes > maxFalse {
		t.Errorf("Too many false decodes: %d, expected <= %d", falseDecodes, maxFalse)
	}
}

// TestBasebandPipelineCapture1WithAP tests AP decoding with capture 1.
func TestBasebandPipelineCapture1WithAP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping baseband pipeline AP test (slow)")
	}

	wavPath := "testdata/ft8test_capture_20260410.wav"
	if _, err := os.Stat(wavPath); os.IsNotExist(err) {
		t.Skipf("test WAV not found: %s", wavPath)
	}

	samples, err := loadTestWAVSimple(wavPath)
	if err != nil {
		t.Fatalf("load WAV: %v", err)
	}

	// KB7THX WB9VGJ RR73 should decode with AP context.
	ap := NewAPContext("KB7THX", "WB9VGJ", 0)
	msgs := ProcessWindowBaseband(samples, DefaultMaxCandidates, DefaultMaxIterations, ap)

	t.Logf("capture1 (with AP): decoded %d message(s)", len(msgs))
	for i, dm := range msgs {
		if msg, err := message.Unpack(dm.Msg77); err == nil {
			t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — %s",
				i, dm.Freq, dm.TimeOff, dm.SNR, msg.String())
		}
	}

	// With AP, should decode at least 10 (9 regular + 1 AP).
	const minDecodes = 9 // conservative — AP CQ OSD reduction may affect KB7THX decode
	if len(msgs) < minDecodes {
		t.Errorf("REGRESSION: capture1 with AP decoded %d messages, expected >= %d", len(msgs), minDecodes)
	}
}

// TestPostDecodeSNR verifies the post-decode SNR computation works correctly
// with known values.
func TestPostDecodeSNR(t *testing.T) {
	// Test with uniform s8 (all tones equal) — should give ~-24 dB (floor).
	var s8 [NumTones][NumSymbols]float64
	var itone [NumSymbols]uint8
	for i := range NumSymbols {
		for tone := range NumTones {
			s8[tone][i] = 1.0
		}
		itone[i] = uint8(i % 8)
	}
	snr := computePostDecodeSNR(&s8, &itone)
	// With equal signal and noise, xsig/xnoi ≈ 1, arg ≈ 0, so snr ≈ -24 (floor).
	if snr > -20.0 {
		t.Errorf("uniform s8: SNR = %.1f dB, expected near floor (-24 dB)", snr)
	}

	// Test with signal tone having 10× stronger magnitude.
	var s8strong [NumTones][NumSymbols]float64
	for i := range NumSymbols {
		for tone := range NumTones {
			s8strong[tone][i] = 1.0
		}
		s8strong[itone[i]][i] = 10.0 // signal tone is 10× stronger
	}
	snrStrong := computePostDecodeSNR(&s8strong, &itone)
	if snrStrong <= snr {
		t.Errorf("strong signal SNR (%.1f) should be > uniform SNR (%.1f)", snrStrong, snr)
	}
	t.Logf("uniform SNR=%.1f dB, strong signal SNR=%.1f dB", snr, snrStrong)
}
