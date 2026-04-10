// pipeline_stage_test.go — stage-by-stage integration tests comparing the Go
// FT8 DSP pipeline against WSJT-X expected behaviour.
//
// These tests load a real-world WAV file captured from live audio and run it
// through each pipeline stage independently, logging intermediate outputs for
// comparison with WSJT-X. The goal is to identify exactly WHERE and HOW MUCH
// the Go pipeline diverges from WSJT-X at each stage.
//
// The tests are ordered to match the pipeline flow:
//
//	Stage 1: Spectrogram construction
//	Stage 2: Candidate detection (sync scoring)
//	Stage 3: Candidate refinement
//	Stage 4: Demodulation (soft symbol extraction)
//	Stage 5: LDPC decode + CRC
//	Stage 6: Full pipeline (end-to-end)
//
// WSJT-X reference data is embedded as constants (from the user's WSJT-X
// output at the same UTC time) for cross-referencing.

package dsp

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// wsjtxDecode represents a single WSJT-X decode for cross-referencing.
type wsjtxDecode struct {
	SNR  int
	DT   float64 // time offset (seconds)
	Freq int     // audio frequency (Hz)
	Msg  string  // decoded message text
}

// wsjtxDecodes_141630 is the WSJT-X output from 2026-04-09 14:16:30 UTC on
// 10m FT8 (28.074 MHz). This is the time window that corresponds to the
// live_capture_20260409_161430.wav test file (which was captured at ~16:14 local
// = 14:14 UTC, one window later).
//
// These are used for cross-referencing which signals our pipeline should find.
var wsjtxDecodes_141630 = []wsjtxDecode{
	{1, 0.3, 300, "CQ EA2EED IN72"},
	{1, 0.2, 475, "CQ TN8GD JI75"},
	{4, 0.3, 1397, "PY3CC I0WBX R-15"},
	{-7, 0.3, 1708, "9V1YC YC9GBQ OI81"},
	{-1, 0.2, 807, "PS7LN SV4LGX KM09"},
	{-5, 0.4, 1581, "CQ EB5URB IM99"},
	{-5, 0.3, 191, "A71MM 9A2KS R-14"},
	{-9, -0.1, 2490, "CE3BT 9H4JX -04"},
	{-11, 0.2, 1954, "PZ5RA YB1LED OI33"},
	{0, 0.2, 2208, "PU1VLC IV3IRO -20"},
	{-9, 0.3, 2279, "9V1YC EA1CQ -24"},
	{-9, 0.3, 1027, "CQ EA5QS IM98"},
	{-1, 0.2, 1842, "PZ5RA EA1R IN73"},
	{-7, 0.3, 871, "ZS2M EA4AA R-09"},
	{-8, -0.8, 689, "XQ7IR DJ9HX R+08"},
	{-7, 0.3, 515, "9G1SD RQ7O KN98"},
	{-11, 0.3, 660, "CE4WJK R7LD LN07"},
	{-14, -0.2, 1045, "3B8GL F4EAR JN39"},
	{-6, 0.5, 1819, "3B9FR F4CNH -09"},
	{-17, 0.4, 1551, "CQ PU2BRU GH64"},
	{-13, 0.2, 1750, "YO5OSF CN8NS R+07"},
	{-18, 0.2, 1784, "3B9FR LA6UL JP22"},
	{-17, 0.2, 1028, "CE4WJK Z33B KN01"},
	{-11, 0.8, 1240, "CE4WJK F4KMA 73"},
	{-14, 0.2, 1863, "ZS2M UR5FFC KN56"},
	{-19, 1.3, 802, "3B9FR IW5BPV -06"},
	{-24, 0.4, 934, "LZ1EX PY5TD GG54"},
	{-13, 0.2, 1411, "M7XOM/P EA8ACW IL28"},
	{-13, 0.8, 2079, "XQ7IR IZ2EID JN34"},
	{-16, 0.3, 1254, "CQ PP5GP GG52"},
	{-18, 0.2, 1740, "R6DWD PU5MRL -17"},
	{-21, 0.2, 2095, "PZ5RA OM2ASH -13"},
}

// liveWAVFiles returns WAV files suitable for stage-by-stage testing.
// Prefers live capture files, falls back to any testdata WAV.
func liveWAVFiles(t *testing.T) []string {
	t.Helper()
	dir := testdataDir(t)
	wavs := findWAVFiles(t)
	if len(wavs) == 0 {
		return nil
	}
	// Prefer live captures.
	var live []string
	for _, w := range wavs {
		if strings.Contains(filepath.Base(w), "live_capture") {
			live = append(live, w)
		}
	}
	if len(live) > 0 {
		return live
	}
	_ = dir
	return wavs
}

// --------------------------------------------------------------------------
// Stage 1: Spectrogram construction
// --------------------------------------------------------------------------

// TestStage1_Spectrogram characterises the spectrogram output for each WAV
// file. It compares Go's standard and hi-res spectrograms against WSJT-X's
// expected dimensions (3840-point FFT, ¼-symbol step, 372 frames, 1920 bins).
func TestStage1_Spectrogram(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pipeline stage test")
	}
	wavs := liveWAVFiles(t)
	if len(wavs) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavs {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("wrong format: rate=%d ch=%d", wav.SampleRate, wav.Channels)
			}
			samples := wav.Samples

			t.Logf("Input: %d samples (%.2f s) at %d Hz",
				len(samples), float64(len(samples))/float64(wav.SampleRate), wav.SampleRate)

			// --- WSJT-X reference dimensions ---
			// NFFT1=3840, NH1=1920, NSTEP=480, NHSYM=372
			t.Logf("")
			t.Logf("=== WSJT-X reference spectrogram ===")
			t.Logf("  FFT size:    3840 (2×NSPS)")
			t.Logf("  Freq bins:   1920 (NFFT1/2)")
			t.Logf("  Bin width:   3.125 Hz (12000/3840)")
			t.Logf("  Time step:   480 samples (NSPS/4 = quarter-symbol)")
			t.Logf("  Time frames: 372 (NMAX/NSTEP - 3)")
			t.Logf("  Bins/tone:   2 (EXACT integer — NFFT1/NSPS)")
			t.Logf("  Power:       linear (re²+im²)")

			// --- Go standard spectrogram ---
			sg := SpectrogramFT8(samples)
			t.Logf("")
			t.Logf("=== Go SpectrogramFT8 (standard) ===")
			if sg != nil {
				t.Logf("  FFT size:    2048 (NextPow2(1920))")
				t.Logf("  Freq bins:   %d", len(sg[0]))
				t.Logf("  Bin width:   %.4f Hz (12000/2048)", float64(SampleRate)/2048.0)
				t.Logf("  Time step:   960 samples (half-symbol)")
				t.Logf("  Time frames: %d", len(sg))
				t.Logf("  Bins/tone:   %.3f (6.25 / %.4f) — FRACTIONAL!",
					ToneSpacing/(float64(SampleRate)/2048.0),
					float64(SampleRate)/2048.0)
				t.Logf("  Power:       log2(re²+im²)")

				// Spot-check: what bin indices map to 1222 Hz (the signal we decoded)?
				binWidth := float64(SampleRate) / 2048.0
				bin1222 := 1222.0 / binWidth
				t.Logf("  1222 Hz → bin %.2f (fractional!)", bin1222)
			}

			// --- Go hi-res spectrogram ---
			sgHR := SpectrogramFT8HiRes(samples, FreqOSR)
			t.Logf("")
			t.Logf("=== Go SpectrogramFT8HiRes (FreqOSR=2) ===")
			if sgHR != nil {
				fftSizeHR := 2 * (len(sgHR[0]) - 1) // 4096
				binWidthHR := float64(SampleRate) / float64(fftSizeHR)
				t.Logf("  FFT size:    %d (NextPow2(3840))", fftSizeHR)
				t.Logf("  Freq bins:   %d", len(sgHR[0]))
				t.Logf("  Bin width:   %.6f Hz (12000/%d)", binWidthHR, fftSizeHR)
				t.Logf("  Time step:   960 samples (half-symbol)")
				t.Logf("  Time frames: %d", len(sgHR))
				t.Logf("  Bins/tone:   %.3f (6.25 / %.6f) — FRACTIONAL!",
					ToneSpacing/binWidthHR, binWidthHR)
				t.Logf("  Power:       log2(re²+im²)")

				bin1222HR := 1222.0 / binWidthHR
				t.Logf("  1222 Hz → bin %.2f (fractional!)", bin1222HR)
			}

			// --- What WSJT-X would see ---
			t.Logf("")
			t.Logf("=== WSJT-X bin alignment at 1222 Hz ===")
			wsjtxBinWidth := 12000.0 / 3840.0 // 3.125 Hz
			wsjtxBin := 1222.0 / wsjtxBinWidth
			t.Logf("  1222 Hz → bin %.2f (nfos=2, so tone 0 at bin %d, tone 7 at bin %d)",
				wsjtxBin, int(math.Round(wsjtxBin)), int(math.Round(wsjtxBin))+7*2)
		})
	}
}

// --------------------------------------------------------------------------
// Stage 2: Candidate detection
// --------------------------------------------------------------------------

// TestStage2_Candidates runs candidate detection on each WAV file and
// cross-references the detected frequencies against WSJT-X's decoded messages.
func TestStage2_Candidates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pipeline stage test")
	}
	wavs := liveWAVFiles(t)
	if len(wavs) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavs {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("wrong format")
			}
			samples := wav.Samples

			const stepsPerSymbol = 4

			// Standard path.
			sg := SpectrogramFT8(samples)
			candidates := FindCandidates(sg, DefaultMaxCandidates, stepsPerSymbol)

			t.Logf("=== Standard candidate detection ===")
			t.Logf("Candidates found: %d (max=%d)", len(candidates), DefaultMaxCandidates)

			// Hi-res path.
			sgHR := SpectrogramFT8HiRes(samples, FreqOSR)
			candidatesHR := FindCandidatesHiRes(sgHR, DefaultMaxCandidates, stepsPerSymbol)

			t.Logf("")
			t.Logf("=== Hi-res candidate detection ===")
			t.Logf("Candidates found: %d (max=%d)", len(candidatesHR), DefaultMaxCandidates)

			// Cross-reference against WSJT-X known decodes.
			t.Logf("")
			t.Logf("=== Cross-reference: which WSJT-X signals did we find? ===")
			t.Logf("(Match tolerance: ±15 Hz frequency)")

			for _, wd := range wsjtxDecodes_141630 {
				freqTarget := float64(wd.Freq)

				// Search standard candidates.
				bestStd := findClosestCandidate(candidates, freqTarget)
				bestHR := findClosestCandidate(candidatesHR, freqTarget)

				stdStr := "NOT FOUND"
				if bestStd != nil && math.Abs(float64(bestStd.Freq)-freqTarget) <= 15 {
					stdStr = fmt.Sprintf("freq=%.1f score=%.3f dt=%.2f (Δf=%.1f)",
						bestStd.Freq, bestStd.Score, bestStd.TimeOff,
						float64(bestStd.Freq)-freqTarget)
				}

				hrStr := "NOT FOUND"
				if bestHR != nil && math.Abs(float64(bestHR.Freq)-freqTarget) <= 15 {
					hrStr = fmt.Sprintf("freq=%.1f score=%.3f dt=%.2f (Δf=%.1f)",
						bestHR.Freq, bestHR.Score, bestHR.TimeOff,
						float64(bestHR.Freq)-freqTarget)
				}

				t.Logf("  WSJT-X: %4d Hz SNR=%+3d %-30s | Std: %s | HiRes: %s",
					wd.Freq, wd.SNR, wd.Msg, stdStr, hrStr)
			}

			// Score histogram for standard candidates.
			t.Logf("")
			t.Logf("=== Standard candidate score distribution ===")
			logScoreHistogram(t, candidates)

			t.Logf("")
			t.Logf("=== Hi-res candidate score distribution ===")
			logScoreHistogram(t, candidatesHR)
		})
	}
}

// --------------------------------------------------------------------------
// Stage 3: Candidate refinement
// --------------------------------------------------------------------------

// TestStage3_Refinement tests the Goertzel-based refinement for candidates
// at known WSJT-X signal frequencies. This validates that refinement moves
// candidates closer to the true signal position.
func TestStage3_Refinement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pipeline stage test")
	}
	wavs := liveWAVFiles(t)
	if len(wavs) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavs {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("wrong format")
			}
			samples := wav.Samples

			const stepsPerSymbol = 4
			hann := HannCoefficients(SamplesPerSymbol)

			// Get candidates from standard path.
			sg := SpectrogramFT8(samples)
			candidates := FindCandidates(sg, DefaultMaxCandidates, stepsPerSymbol)

			t.Logf("=== Refinement results for WSJT-X known-signal frequencies ===")

			for _, wd := range wsjtxDecodes_141630 {
				if wd.SNR < -15 {
					continue // skip very weak signals for clarity
				}

				freqTarget := float64(wd.Freq)
				cand := findClosestCandidate(candidates, freqTarget)
				if cand == nil || math.Abs(float64(cand.Freq)-freqTarget) > 15 {
					t.Logf("  %4d Hz %-25s — no candidate found within ±15 Hz", wd.Freq, wd.Msg)
					continue
				}

				// Run standard refinement.
				refined := RefineCandidateAudio(samples, hann, *cand)

				// Run fast (coarse-fine) refinement.
				refinedFast := RefineCandidateAudioFast(samples, hann, *cand)

				t.Logf("  %4d Hz %-25s (SNR=%+d)", wd.Freq, wd.Msg, wd.SNR)
				t.Logf("    Coarse:       freq=%.1f dt=%.3f score=%.3f",
					cand.Freq, cand.TimeOff, cand.Score)
				t.Logf("    Refined:      freq=%.1f dt=%.3f score=%.3f (Δf=%.1f from WSJT-X)",
					refined.Freq, refined.TimeOff, refined.Score,
					float64(refined.Freq)-freqTarget)
				t.Logf("    RefinedFast:  freq=%.1f dt=%.3f score=%.3f (Δf=%.1f from WSJT-X)",
					refinedFast.Freq, refinedFast.TimeOff, refinedFast.Score,
					float64(refinedFast.Freq)-freqTarget)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Stage 4: Demodulation (soft symbol extraction)
// --------------------------------------------------------------------------

// TestStage4_Demodulation tests soft symbol extraction for candidates at
// known WSJT-X frequencies. It logs LLR statistics to assess quality.
func TestStage4_Demodulation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pipeline stage test")
	}
	wavs := liveWAVFiles(t)
	if len(wavs) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavs {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("wrong format")
			}
			samples := wav.Samples

			const stepsPerSymbol = 4
			hann := HannCoefficients(SamplesPerSymbol)

			sg := SpectrogramFT8(samples)
			candidates := FindCandidates(sg, DefaultMaxCandidates, stepsPerSymbol)

			t.Logf("=== Demodulation results for WSJT-X known-signal frequencies ===")

			for _, wd := range wsjtxDecodes_141630 {
				if wd.SNR < -15 {
					continue
				}

				freqTarget := float64(wd.Freq)
				cand := findClosestCandidate(candidates, freqTarget)
				if cand == nil || math.Abs(float64(cand.Freq)-freqTarget) > 15 {
					continue
				}

				// Refine candidate.
				refined := RefineCandidateAudio(samples, hann, *cand)

				// Demodulate.
				llr := DemodulateAudio(samples, hann, refined)
				NormalizeLLR(&llr)

				// LLR statistics.
				var absSum, maxAbs float64
				var positiveCount, negativeCount int
				for _, v := range llr {
					abs := math.Abs(float64(v))
					absSum += abs
					if abs > maxAbs {
						maxAbs = abs
					}
					if v > 0 {
						positiveCount++
					} else if v < 0 {
						negativeCount++
					}
				}
				meanAbs := absSum / float64(CodedBits)

				// Try LDPC decode.
				msg77, ok := codec.DecodeMessage(llr, DefaultMaxIterations)
				decodeResult := "FAILED"
				if ok {
					msg77[9] &= 0xF8
					if msg, err := message.Unpack(msg77); err == nil {
						decodeResult = fmt.Sprintf("OK → %s", formatMessage(msg))
					} else {
						decodeResult = fmt.Sprintf("OK (unpack failed: %v)", err)
					}
				}

				t.Logf("  %4d Hz %-25s (SNR=%+d)", wd.Freq, wd.Msg, wd.SNR)
				t.Logf("    Refined: freq=%.1f dt=%.3f", refined.Freq, refined.TimeOff)
				t.Logf("    LLR stats: mean|LLR|=%.2f max|LLR|=%.2f pos=%d neg=%d",
					meanAbs, maxAbs, positiveCount, negativeCount)
				t.Logf("    LDPC decode: %s", decodeResult)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Stage 5: Full pipeline comparison (standard vs multi-pass)
// --------------------------------------------------------------------------

// TestStage5_FullPipeline runs both pipeline variants and compares the
// decoded messages against WSJT-X's output.
func TestStage5_FullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pipeline stage test")
	}
	wavs := liveWAVFiles(t)
	if len(wavs) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavs {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("wrong format")
			}
			samples := wav.Samples

			// Standard pipeline.
			stdMsgs := ProcessWindow(samples, DefaultMaxCandidates, DefaultMaxIterations)
			t.Logf("=== Standard pipeline: %d decoded ===", len(stdMsgs))
			for i, dm := range stdMsgs {
				if msg, err := message.Unpack(dm.Msg77); err == nil {
					t.Logf("  [%2d] freq=%7.1f dt=%.3f snr=%+6.1f — %s",
						i, dm.Freq, dm.TimeOff, dm.SNR, formatMessage(msg))
				}
			}

			// Multi-pass pipeline.
			mpMsgs := ProcessWindowMultiPass(samples, DefaultMaxCandidates, DefaultMaxIterations)
			t.Logf("")
			t.Logf("=== Multi-pass pipeline: %d decoded ===", len(mpMsgs))
			for i, dm := range mpMsgs {
				if msg, err := message.Unpack(dm.Msg77); err == nil {
					t.Logf("  [%2d] freq=%7.1f dt=%.3f snr=%+6.1f — %s",
						i, dm.Freq, dm.TimeOff, dm.SNR, formatMessage(msg))
				}
			}

			// Cross-reference: which WSJT-X decodes did we match?
			t.Logf("")
			t.Logf("=== WSJT-X cross-reference: %d reference decodes ===", len(wsjtxDecodes_141630))

			stdMatched := 0
			mpMatched := 0
			for _, wd := range wsjtxDecodes_141630 {
				stdMatch := findDecodedMessage(stdMsgs, wd)
				mpMatch := findDecodedMessage(mpMsgs, wd)

				stdStr := "MISS"
				if stdMatch != "" {
					stdStr = "HIT"
					stdMatched++
				}
				mpStr := "MISS"
				if mpMatch != "" {
					mpStr = "HIT"
					mpMatched++
				}

				t.Logf("  %4d Hz SNR=%+3d %-30s | Std: %s | MultiPass: %s",
					wd.Freq, wd.SNR, wd.Msg, stdStr, mpStr)
			}

			t.Logf("")
			t.Logf("=== Summary ===")
			t.Logf("WSJT-X decoded:    %d messages", len(wsjtxDecodes_141630))
			t.Logf("Standard pipeline: %d decoded, %d/%d WSJT-X matches",
				len(stdMsgs), stdMatched, len(wsjtxDecodes_141630))
			t.Logf("Multi-pass:        %d decoded, %d/%d WSJT-X matches",
				len(mpMsgs), mpMatched, len(wsjtxDecodes_141630))
		})
	}
}

// --------------------------------------------------------------------------
// Stage 6: Spectrogram architecture comparison
// --------------------------------------------------------------------------

// TestStage6_SpectrogramArchitecture directly measures the impact of the FFT
// size mismatch by computing sync scores using Goertzel (which has NO bin
// alignment issues) vs the spectrogram-based scoring (which does).
//
// For each WSJT-X-confirmed signal, we compute:
//   - Spectrogram-based sync score (subject to bin mismatch)
//   - Goertzel-based sync score (exact frequency, no bin mismatch)
//
// A large gap between the two indicates the bin mismatch is degrading
// candidate detection.
func TestStage6_SpectrogramArchitecture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pipeline stage test")
	}
	wavs := liveWAVFiles(t)
	if len(wavs) == 0 {
		t.Skip("no WAV files in testdata/")
	}

	for _, path := range wavs {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("wrong format")
			}
			samples := wav.Samples
			hann := HannCoefficients(SamplesPerSymbol)

			t.Logf("=== Goertzel vs spectrogram sync score comparison ===")
			t.Logf("(Goertzel computes power at exact FT8 tone frequencies — no bin mismatch)")
			t.Logf("")

			for _, wd := range wsjtxDecodes_141630 {
				if wd.SNR < -15 {
					continue
				}

				// Compute Goertzel-based sync score at the WSJT-X known position.
				// Use the WSJT-X time offset to compute start sample.
				startSample := int(math.Round(wd.DT * float64(SampleRate)))
				baseFreq := float64(wd.Freq)

				// Check bounds.
				endSample := startSample + NumSymbols*SamplesPerSymbol
				if startSample < 0 || endSample > len(samples) {
					t.Logf("  %4d Hz %-25s — out of bounds (dt=%.1f)", wd.Freq, wd.Msg, wd.DT)
					continue
				}

				// Goertzel-based sync score (exact frequencies).
				goertzelScore := syncScoreAudio(samples, hann, startSample, baseFreq)

				// Also refine from this position.
				cand := Candidate{
					Freq:    float32(baseFreq),
					TimeOff: float32(startSample) / float32(SampleRate),
				}
				refined := RefineCandidateAudio(samples, hann, cand)

				t.Logf("  %4d Hz %-25s (SNR=%+d): Goertzel=%.3f  Refined: freq=%.1f dt=%.3f score=%.3f",
					wd.Freq, wd.Msg, wd.SNR,
					goertzelScore,
					refined.Freq, refined.TimeOff, refined.Score)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// findClosestCandidate returns the candidate nearest to targetFreq, or nil.
func findClosestCandidate(candidates []Candidate, targetFreq float64) *Candidate {
	if len(candidates) == 0 {
		return nil
	}
	best := &candidates[0]
	bestDist := math.Abs(float64(best.Freq) - targetFreq)
	for i := 1; i < len(candidates); i++ {
		d := math.Abs(float64(candidates[i].Freq) - targetFreq)
		if d < bestDist {
			best = &candidates[i]
			bestDist = d
		}
	}
	return best
}

// findDecodedMessage checks if any decoded message matches the WSJT-X
// reference by frequency (±20 Hz). Returns the decoded text or "".
func findDecodedMessage(msgs []DecodedMessage, wd wsjtxDecode) string {
	for _, dm := range msgs {
		if math.Abs(float64(dm.Freq)-float64(wd.Freq)) <= 20 {
			if msg, err := message.Unpack(dm.Msg77); err == nil {
				return formatMessage(msg)
			}
		}
	}
	return ""
}

// logScoreHistogram logs a score histogram for the candidate list.
func logScoreHistogram(t *testing.T, candidates []Candidate) {
	t.Helper()
	if len(candidates) == 0 {
		t.Logf("  (no candidates)")
		return
	}

	// Score buckets.
	buckets := []struct {
		label string
		lo    float32
		hi    float32
		count int
	}{
		{"< 1.5", 0, 1.5, 0},
		{"1.5–2.0", 1.5, 2.0, 0},
		{"2.0–2.5", 2.0, 2.5, 0},
		{"2.5–3.0", 2.5, 3.0, 0},
		{"3.0–4.0", 3.0, 4.0, 0},
		{"4.0–5.0", 4.0, 5.0, 0},
		{"> 5.0", 5.0, 999, 0},
	}

	for _, c := range candidates {
		for i := range buckets {
			if c.Score >= buckets[i].lo && c.Score < buckets[i].hi {
				buckets[i].count++
				break
			}
		}
	}

	for _, b := range buckets {
		bar := strings.Repeat("█", b.count)
		t.Logf("  %8s: %3d %s", b.label, b.count, bar)
	}

	// Top/bottom scores.
	sorted := make([]float32, len(candidates))
	for i, c := range candidates {
		sorted[i] = c.Score
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })

	top := 5
	if len(sorted) < top {
		top = len(sorted)
	}
	t.Logf("  Top %d scores: %v", top, sorted[:top])
}
