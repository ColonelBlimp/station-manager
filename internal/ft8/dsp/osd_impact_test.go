package dsp

import (
	"fmt"
	"os"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// TestOSDImpactOnTestWAV measures the actual decode-rate improvement from
// OSD on the reference 13-message test WAV. It runs the baseband pipeline
// and compares BP-only decoding vs BP+OSD (the current DecodeMessage path).
//
// WSJT-X reference: 13 messages from this capture.
// Before OSD: 4/13 (baseband mode).
func TestOSDImpactOnTestWAV(t *testing.T) {
	wavPath := "testdata/ft8test_capture_20260410.wav"
	if _, err := os.Stat(wavPath); os.IsNotExist(err) {
		t.Skipf("test WAV not found: %s", wavPath)
	}

	samples, err := loadTestWAVSimple(wavPath)
	if err != nil {
		t.Fatalf("load WAV: %v", err)
	}

	const maxCandidates = 120
	const maxIter = 40

	// Build spectrogram and find candidates.
	sg := SpectrogramFT8HiRes(samples, FreqOSR)
	if sg == nil {
		t.Fatal("spectrogram returned nil")
	}
	const stepsPerSymbol = 4
	candidates := FindCandidatesHiRes(sg, maxCandidates, stepsPerSymbol)
	if len(candidates) == 0 {
		t.Fatal("no candidates found")
	}

	hann := HannCoefficients(SamplesPerSymbol)
	longFFT := LongFFT(samples)

	type decResult struct {
		freq    float32
		timeOff float32
		text    string
		bpOnly  bool // decoded by BP alone
		osdOnly bool // decoded by OSD fallback (BP failed)
	}

	seenBP := make(map[[10]byte]struct{})
	seenOSD := make(map[[10]byte]struct{})
	var bpDecoded, osdDecoded int
	var results []decResult

	for i := range candidates {
		cand := &candidates[i]
		refined := RefineCandidateAudioFast(samples, hann, *cand)

		bbResult := DemodulateBaseband(longFFT,
			float64(refined.Freq),
			float64(refined.TimeOff))

		if bbResult.Nsync <= minSyncForDecode {
			continue
		}

		llrSets := [4]*[CodedBits]float32{
			&bbResult.LLRa,
			&bbResult.LLRb,
			&bbResult.LLRc,
			&bbResult.LLRd,
		}

		// Try BP-only on all 4 LLR sets.
		var bpMsg [10]byte
		var bpOK bool
		for _, llr := range llrSets {
			info, ok := codec.Decode(*llr, maxIter)
			if !ok {
				continue
			}
			// Check CRC manually (reproducing DecodeMessage logic).
			var m77 [10]byte
			copy(m77[:], info[:10])
			m77[9] &= 0xF8
			wantCRC := message.CRC14(m77[:])
			gotCRC := uint16(info[9]&0x07)<<11 | uint16(info[10])<<3 | uint16(info[11]>>5)&0x07
			if gotCRC == wantCRC {
				bpMsg = m77
				bpOK = true
				break
			}
		}

		// Try BP+OSD (full DecodeMessage) on all 4 LLR sets.
		var osdMsg [10]byte
		var osdOK bool
		for _, llr := range llrSets {
			m, ok := codec.DecodeMessage(*llr, maxIter)
			if ok {
				m[9] &= 0xF8
				osdMsg = m
				osdOK = true
				break
			}
		}

		if bpOK {
			bpMsg[9] &= 0xF8
			if _, dup := seenBP[bpMsg]; !dup {
				seenBP[bpMsg] = struct{}{}
				bpDecoded++
			}
		}

		if osdOK {
			osdMsg[9] &= 0xF8
			if _, dup := seenOSD[osdMsg]; !dup {
				seenOSD[osdMsg] = struct{}{}
				osdDecoded++

				msg, unpackErr := message.Unpack(osdMsg)
				var text string
				if unpackErr != nil {
					text = fmt.Sprintf("(unpack error: %v)", unpackErr)
				} else {
					text = msg.String()
				}

				_, bpHad := seenBP[osdMsg]
				results = append(results, decResult{
					freq:    refined.Freq,
					timeOff: refined.TimeOff,
					text:    text,
					bpOnly:  bpHad,
					osdOnly: !bpHad,
				})
			}
		}
	}

	// Print results.
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════════════")
	t.Log("  OSD Impact: BP-only vs BP+OSD on test WAV (13 WSJT-X msgs)")
	t.Log("═══════════════════════════════════════════════════════════════")
	t.Log("")
	t.Logf("  %-9s %8s  %-8s  %s", "TIME (s)", "FREQ", "SOURCE", "MESSAGE")
	t.Logf("  %-9s %8s  %-8s  %s", "─────────", "────────", "────────", "────────────────────────")
	for _, r := range results {
		source := "BP"
		if r.osdOnly {
			source = "OSD ★"
		}
		t.Logf("  %+8.3f  %7.1f  %-8s  %s",
			r.timeOff, r.freq, source, r.text)
	}
	t.Log("")
	t.Logf("  BP-only decoded : %d/13", bpDecoded)
	t.Logf("  BP+OSD decoded  : %d/13", osdDecoded)
	t.Logf("  OSD added       : %d new message(s)", osdDecoded-bpDecoded)
	t.Log("")

	if osdDecoded > bpDecoded {
		t.Logf("  ✓ OSD improved decode rate: %d → %d (+%d)",
			bpDecoded, osdDecoded, osdDecoded-bpDecoded)
	} else if osdDecoded == bpDecoded {
		t.Log("  ⚠ OSD did not add new decodes on this capture")
		t.Log("    (may still help on other captures with weaker signals)")
	}
}
