package dsp

import (
	"fmt"
	"os"
	"testing"
)

// TestBasebandLLRComparison loads the test WAV and prints comparative
// LLR diagnostics for the first strong candidate across all 4 passes.
// This is a diagnostic test, not a correctness assertion.
func TestBasebandLLRComparison(t *testing.T) {
	wavPath := "testdata/ft8test_capture_20260410.wav"
	if _, err := os.Stat(wavPath); os.IsNotExist(err) {
		t.Skipf("test WAV not found: %s", wavPath)
	}

	samples, err := loadTestWAVSimple(wavPath)
	if err != nil {
		t.Fatalf("load WAV: %v", err)
	}

	// Build spectrogram and find candidates.
	sg := SpectrogramFT8HiRes(samples, FreqOSR)
	if sg == nil {
		t.Fatal("spectrogram returned nil")
	}
	const stepsPerSymbol = 4
	candidates := FindCandidatesHiRes(sg, 120, stepsPerSymbol)
	if len(candidates) == 0 {
		t.Fatal("no candidates found")
	}

	// Refine the first (strongest) candidate.
	hann := HannCoefficients(SamplesPerSymbol)
	refined := RefineCandidateAudioFast(samples, hann, candidates[0])

	// Compute long FFT.
	longFFT := LongFFT(samples)

	// Run baseband demodulation.
	bbResult := DemodulateBaseband(longFFT, float64(refined.Freq), float64(refined.TimeOff))

	t.Logf("Candidate: %.1f Hz, t=%.3f s, nsync=%d, Δf=%.1f Hz",
		refined.Freq, refined.TimeOff, bbResult.Nsync, bbResult.FreqAdj)

	// Print first 30 LLR values from each pass.
	passNames := [4]string{"bmeta(nsym=1)", "bmetb(nsym=2)", "bmetc(nsym=3)", "bmetd(bit-norm)"}
	passes := [4]*[CodedBits]float32{&bbResult.LLRa, &bbResult.LLRb, &bbResult.LLRc, &bbResult.LLRd}

	for p, llr := range passes {
		t.Logf("\n%s:", passNames[p])
		s := ""
		for i := 0; i < 30; i++ {
			s += fmt.Sprintf(" %+.2f", llr[i])
		}
		t.Logf("  LLR[0:30]:%s", s)

		// Count sign agreements with pass 0.
		if p > 0 {
			agree := 0
			disagree := 0
			for i := range CodedBits {
				if (passes[0][i] > 0 && llr[i] > 0) || (passes[0][i] < 0 && llr[i] < 0) || (passes[0][i] == 0 && llr[i] == 0) {
					agree++
				} else {
					disagree++
				}
			}
			t.Logf("  Sign agreement with pass 0: %d/%d (disagree: %d)", agree, CodedBits, disagree)
		}
	}
}

// loadTestWAVSimple is a simple WAV loader for testing.
func loadTestWAVSimple(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Skip WAV header (44 bytes) and read 16-bit PCM samples.
	if len(data) < 44 {
		return nil, fmt.Errorf("WAV file too short")
	}

	pcm := data[44:]
	nSamples := len(pcm) / 2
	samples := make([]float32, nSamples)
	for i := 0; i < nSamples; i++ {
		lo := pcm[2*i]
		hi := pcm[2*i+1]
		s := int16(lo) | (int16(hi) << 8)
		samples[i] = float32(s) / 32768.0
	}
	return samples, nil
}
