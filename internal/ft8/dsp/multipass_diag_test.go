// multipass_diag_test.go — diagnostic tests for the ProcessWindowMultiPass pipeline.
//
// These tests compare the new multi-pass pipeline against the working
// ProcessWindow pipeline to isolate the 0-decode regression.
//
// Debugging steps from context-handoff.md:
//   Step 1: Verify ProcessWindowMultiPass on WAV files
//   Step 2: Instrument syncScoreNeighbor threshold
//   Step 3: Compare candidate counts between pipelines
//   Step 4: Test with freqOSR=1 fallback

package dsp

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// loadTestWAV loads a WAV file from testdata/ and returns the samples.
func loadTestWAV(t *testing.T, name string) []float32 {
	t.Helper()
	path := filepath.Join(testdataDir(t), name)
	wav, err := audio.ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV(%s): %v", name, err)
	}
	if wav.SampleRate != SampleRate {
		t.Skipf("%s: sample rate %d Hz, want %d Hz", name, wav.SampleRate, SampleRate)
	}
	if wav.Channels != 1 {
		t.Skipf("%s: %d channels, want 1 (mono)", name, wav.Channels)
	}
	return wav.Samples
}

// --- Step 1: Compare ProcessWindowMultiPass vs ProcessWindow on WAV files ---

func TestStep1_ProcessWindowMultiPassVsProcessWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WAV test (slow)")
	}
	wavFiles := findWAVFiles(t)
	if len(wavFiles) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavFiles {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			samples := loadTestWAV(t, name)

			// Run the original working pipeline.
			msgsOrig := ProcessWindow(samples, 120, 40)
			t.Logf("ProcessWindow:          %d message(s)", len(msgsOrig))
			for i, dm := range msgsOrig {
				if msg, err := message.Unpack(dm.Msg77); err == nil {
					t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — %s",
						i, dm.Freq, dm.TimeOff, dm.SNR, formatMessage(msg))
				}
			}

			// Run the new multi-pass pipeline.
			msgsNew := ProcessWindowMultiPass(samples, 120, 40)
			t.Logf("ProcessWindowMultiPass: %d message(s)", len(msgsNew))
			for i, dm := range msgsNew {
				if msg, err := message.Unpack(dm.Msg77); err == nil {
					t.Logf("  [%d] freq=%.1f Hz time=%.3f s snr=%.1f dB — %s",
						i, dm.Freq, dm.TimeOff, dm.SNR, formatMessage(msg))
				}
			}

			// Report the comparison.
			if len(msgsNew) == 0 && len(msgsOrig) > 0 {
				t.Errorf("BUG CONFIRMED: ProcessWindow decoded %d, ProcessWindowMultiPass decoded 0",
					len(msgsOrig))
			} else if len(msgsNew) < len(msgsOrig) {
				t.Logf("WARNING: ProcessWindowMultiPass decoded %d vs %d for ProcessWindow (%.0f%%)",
					len(msgsNew), len(msgsOrig), 100*float64(len(msgsNew))/float64(len(msgsOrig)))
			}
		})
	}
}

// --- Step 2: Instrument syncScoreNeighbor score distribution ---

func TestStep2_SyncScoreNeighborDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WAV test (slow)")
	}
	wavFiles := findWAVFiles(t)
	if len(wavFiles) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	// Use the first WAV file.
	name := filepath.Base(wavFiles[0])
	samples := loadTestWAV(t, name)

	// Build the hi-res spectrogram.
	sg := SpectrogramFT8HiRes(samples, FreqOSR)
	if sg == nil {
		t.Fatal("SpectrogramFT8HiRes returned nil")
	}

	const stepsPerSymbol = 2
	nFrames := len(sg)
	nBins := len(sg[0])
	fftSize := 2 * (nBins - 1)
	binWidth := float64(SampleRate) / float64(fftSize)
	binsPerTone := ToneSpacing / binWidth

	t.Logf("Hi-res spectrogram: %d frames × %d bins", nFrames, nBins)
	t.Logf("FFT size: %d, bin width: %.4f Hz, bins per tone: %.4f", fftSize, binWidth, binsPerTone)

	maxToneOffset := int(math.Round(float64(NumTones-1) * binsPerTone))
	minFrames := (NumSymbols-1)*stepsPerSymbol + 1
	maxTimeOff := nFrames - minFrames

	minBin := int(minSearchFreqHz / binWidth)
	maxBin := int(maxSearchFreqHz/binWidth) + 1
	if maxBin > nBins-maxToneOffset-1 {
		maxBin = nBins - maxToneOffset - 1
	}

	// Scan ALL positions and collect score statistics.
	var allScores []float32
	var maxScore float32 = -1e30
	var aboveThreshold int
	threshold := float32(minSyncScoreLog2) // 1.5

	for tOff := 0; tOff <= maxTimeOff; tOff++ {
		for b := minBin; b <= maxBin; b++ {
			score := syncScoreNeighbor(sg, tOff, b, stepsPerSymbol, binsPerTone)
			allScores = append(allScores, score)
			if score > maxScore {
				maxScore = score
			}
			if score > threshold {
				aboveThreshold++
			}
		}
	}

	// Compute statistics.
	var sum, sum2 float64
	for _, s := range allScores {
		sum += float64(s)
		sum2 += float64(s) * float64(s)
	}
	n := float64(len(allScores))
	mean := sum / n
	variance := (sum2 / n) - (mean * mean)
	stddev := math.Sqrt(variance)

	// Find percentiles.
	sorted := make([]float32, len(allScores))
	copy(sorted, allScores)
	sortFloat32(sorted)

	p50 := sorted[len(sorted)/2]
	p90 := sorted[int(0.90*float64(len(sorted)))]
	p95 := sorted[int(0.95*float64(len(sorted)))]
	p99 := sorted[int(0.99*float64(len(sorted)))]
	p999 := sorted[int(0.999*float64(len(sorted)))]

	t.Logf("syncScoreNeighbor statistics over %d positions:", len(allScores))
	t.Logf("  min=%.4f max=%.4f mean=%.4f stddev=%.4f", sorted[0], maxScore, mean, stddev)
	t.Logf("  p50=%.4f p90=%.4f p95=%.4f p99=%.4f p99.9=%.4f", p50, p90, p95, p99, p999)
	t.Logf("  above threshold (%.2f): %d (%.4f%%)", threshold, aboveThreshold,
		100*float64(aboveThreshold)/n)

	// Try various thresholds.
	for _, th := range []float32{0.1, 0.2, 0.3, 0.5, 0.7, 1.0, 1.5, 2.0, 3.0} {
		count := 0
		for _, s := range allScores {
			if s > th {
				count++
			}
		}
		t.Logf("  threshold=%.1f → %d candidates (%.4f%%)", th, count, 100*float64(count)/n)
	}

	// Now compare with syncScoreSteps on the standard spectrogram.
	sgStd := SpectrogramFT8(samples)
	if sgStd != nil {
		candsStd := FindCandidates(sgStd, 200, stepsPerSymbol)
		t.Logf("\nFindCandidates (standard): %d candidates", len(candsStd))
		if len(candsStd) > 0 {
			t.Logf("  top scores: %.4f %.4f %.4f (first 3)",
				candsStd[0].Score,
				safeScore(candsStd, 1),
				safeScore(candsStd, 2))
		}
	}
}

// --- Step 3: Compare candidate counts ---

func TestStep3_CompareCandidateCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WAV test (slow)")
	}
	wavFiles := findWAVFiles(t)
	if len(wavFiles) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavFiles {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			samples := loadTestWAV(t, name)

			const stepsPerSymbol = 2

			// Standard spectrogram + FindCandidates.
			sgStd := SpectrogramFT8(samples)
			candsStd := FindCandidates(sgStd, 200, stepsPerSymbol)

			// Hi-res spectrogram + FindCandidatesHiRes.
			sgHiRes := SpectrogramFT8HiRes(samples, FreqOSR)
			candsHiRes := FindCandidatesHiRes(sgHiRes, 200, stepsPerSymbol)

			t.Logf("Standard: %d frames × %d bins → %d candidates",
				len(sgStd), len(sgStd[0]), len(candsStd))
			t.Logf("Hi-res:   %d frames × %d bins → %d candidates",
				len(sgHiRes), len(sgHiRes[0]), len(candsHiRes))

			if len(candsStd) > 0 {
				t.Logf("Standard top 5 scores: %s", topNScores(candsStd, 5))
			}
			if len(candsHiRes) > 0 {
				t.Logf("Hi-res top 5 scores:   %s", topNScores(candsHiRes, 5))
			}

			if len(candsHiRes) == 0 && len(candsStd) > 0 {
				t.Errorf("Hi-res found 0 candidates, standard found %d — threshold or scoring bug", len(candsStd))
			}
		})
	}
}

// --- Step 4: Test FindCandidatesHiRes with freqOSR=1 fallback ---

func TestStep4_FindCandidatesHiResWithFreqOSR1(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WAV test (slow)")
	}
	wavFiles := findWAVFiles(t)
	if len(wavFiles) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	name := filepath.Base(wavFiles[0])
	samples := loadTestWAV(t, name)

	// With freqOSR=1, SpectrogramFT8HiRes delegates to SpectrogramFT8.
	sgOSR1 := SpectrogramFT8HiRes(samples, 1)
	const stepsPerSymbol = 2
	candsOSR1 := FindCandidatesHiRes(sgOSR1, 120, stepsPerSymbol)

	sgOSR2 := SpectrogramFT8HiRes(samples, FreqOSR)
	candsOSR2 := FindCandidatesHiRes(sgOSR2, 120, stepsPerSymbol)

	t.Logf("freqOSR=1: %d frames × %d bins → %d candidates", len(sgOSR1), len(sgOSR1[0]), len(candsOSR1))
	t.Logf("freqOSR=2: %d frames × %d bins → %d candidates", len(sgOSR2), len(sgOSR2[0]), len(candsOSR2))

	if len(candsOSR1) > 0 && len(candsOSR2) == 0 {
		t.Errorf("freqOSR=1 found %d candidates, freqOSR=2 found 0 — hi-res spectrogram or scoring bug", len(candsOSR1))
	}
	if len(candsOSR1) > 0 {
		t.Logf("freqOSR=1 top 5 scores: %s", topNScores(candsOSR1, 5))
	}
	if len(candsOSR2) > 0 {
		t.Logf("freqOSR=2 top 5 scores: %s", topNScores(candsOSR2, 5))
	}
}

// --- Step 5: Test RefineCandidateAudioFast vs RefineCandidateAudio ---

func TestStep5_RefineFastVsOriginal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WAV test (slow)")
	}
	wavFiles := findWAVFiles(t)
	if len(wavFiles) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	name := filepath.Base(wavFiles[0])
	samples := loadTestWAV(t, name)

	// Use standard spectrogram + standard FindCandidates.
	sgStd := SpectrogramFT8(samples)
	const stepsPerSymbol = 2
	candidates := FindCandidates(sgStd, 120, stepsPerSymbol)

	hann := HannCoefficients(SamplesPerSymbol)

	// Test with RefineCandidateAudio (original).
	decodedOrig := 0
	seen := make(map[[10]byte]struct{})
	for i := range candidates {
		refined := RefineCandidateAudio(samples, hann, candidates[i])
		llr := DemodulateAudio(samples, hann, refined)
		NormalizeLLR(&llr)
		msg77, ok := codec.DecodeMessage(llr, 40)
		if !ok {
			continue
		}
		msg77[9] &= 0xF8
		if _, dup := seen[msg77]; !dup {
			seen[msg77] = struct{}{}
			decodedOrig++
		}
	}

	// Test with RefineCandidateAudioFast (new).
	decodedFast := 0
	seen2 := make(map[[10]byte]struct{})
	for i := range candidates {
		refined := RefineCandidateAudioFast(samples, hann, candidates[i])
		llr := DemodulateAudio(samples, hann, refined)
		NormalizeLLR(&llr)
		msg77, ok := codec.DecodeMessage(llr, 40)
		if !ok {
			continue
		}
		msg77[9] &= 0xF8
		if _, dup := seen2[msg77]; !dup {
			seen2[msg77] = struct{}{}
			decodedFast++
		}
	}

	t.Logf("Standard candidates (%d) + RefineCandidateAudio:     %d decoded", len(candidates), decodedOrig)
	t.Logf("Standard candidates (%d) + RefineCandidateAudioFast: %d decoded", len(candidates), decodedFast)

	if decodedFast == 0 && decodedOrig > 0 {
		t.Errorf("RefineCandidateAudioFast breaks decoding: original=%d, fast=0", decodedOrig)
	}
}

// --- Helpers ---

func sortFloat32(s []float32) {
	// Simple insertion sort for float32 slice.
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

func safeScore(cands []Candidate, i int) float32 {
	if i < len(cands) {
		return cands[i].Score
	}
	return 0
}

func topNScores(cands []Candidate, n int) string {
	if len(cands) < n {
		n = len(cands)
	}
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%.4f", cands[i].Score)
	}
	return s
}

// Keep these variables referenced so the imports are used.
var _ = audio.ReadWAV
var _ = message.Unpack
