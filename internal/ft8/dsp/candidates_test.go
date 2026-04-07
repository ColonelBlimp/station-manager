// candidates_test.go — tests for FT8 Costas sync candidate detection.

package dsp

import (
	"math"
	"testing"
)

// --- Test helpers ---

// makeSpectrogram creates an nFrames × nBins spectrogram filled with fillVal.
func makeSpectrogram(nFrames, nBins int, fillVal float32) [][]float32 {
	sg := make([][]float32, nFrames)
	for i := range sg {
		sg[i] = make([]float32, nBins)
		if fillVal != 0 {
			for k := range sg[i] {
				sg[i][k] = fillVal
			}
		}
	}
	return sg
}

// placeCostasSignal sets the Costas sync tone positions in the spectrogram
// at the given time offset and base bin to the specified power level. This
// simulates a clean FT8 sync signal with no data symbol energy.
func placeCostasSignal(sg [][]float32, timeOff, baseBin int, power float32) {
	for _, start := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			sg[timeOff+start+j][baseBin+int(CostasSync[j])] = power
		}
	}
}

// placeFullSignal sets both the Costas sync tones and data symbol tones in
// the spectrogram. Data symbols are placed at a fixed tone (tone 0) with
// lower power to simulate a realistic signal where sync tones stand out.
func placeFullSignal(sg [][]float32, timeOff, baseBin int, syncPower, dataPower float32) {
	placeCostasSignal(sg, timeOff, baseBin, syncPower)

	// Place data symbols at tone 0 (arbitrary choice).
	// Data positions: 7–35 and 43–71.
	for s := Sync1Start + SyncLen; s < Sync2Start; s++ {
		sg[timeOff+s][baseBin] = dataPower
	}
	for s := Sync2Start + SyncLen; s < Sync3Start; s++ {
		sg[timeOff+s][baseBin] = dataPower
	}
}

// spectrogramBinWidth returns the bin width in Hz for an nBins-wide spectrogram.
func spectrogramBinWidth(nBins int) float32 {
	fftSize := 2 * (nBins - 1)
	return float32(SampleRate) / float32(fftSize)
}

// --- Nil / edge-case tests ---

func TestFindCandidatesNil(t *testing.T) {
	if c := FindCandidates(nil, 10); c != nil {
		t.Error("FindCandidates(nil) != nil")
	}
	if c := FindCandidates([][]float32{}, 10); c != nil {
		t.Error("FindCandidates([]) != nil")
	}
}

func TestFindCandidatesMaxZero(t *testing.T) {
	sg := makeSpectrogram(93, 1025, 0)
	if c := FindCandidates(sg, 0); c != nil {
		t.Error("maxCandidates=0 should return nil")
	}
	if c := FindCandidates(sg, -1); c != nil {
		t.Error("maxCandidates<0 should return nil")
	}
}

func TestFindCandidatesTooFewFrames(t *testing.T) {
	// Fewer than 79 frames → cannot contain a full FT8 message.
	sg := makeSpectrogram(NumSymbols-1, 1025, 0)
	if c := FindCandidates(sg, 10); c != nil {
		t.Error("too few frames should return nil")
	}
}

func TestFindCandidatesTooFewBins(t *testing.T) {
	// Fewer than 8 bins → cannot hold the 8-tone grid.
	sg := makeSpectrogram(93, NumTones-1, 0)
	if c := FindCandidates(sg, 10); c != nil {
		t.Error("too few bins should return nil")
	}
}

// --- Silence test ---

func TestFindCandidatesSilence(t *testing.T) {
	sg := makeSpectrogram(93, 1025, 0)
	c := FindCandidates(sg, 100)
	if len(c) != 0 {
		t.Errorf("silence: got %d candidates, want 0", len(c))
	}
}

// --- Single signal detection ---

func TestFindCandidatesSingleSignal(t *testing.T) {
	const nBins = 1025
	sg := makeSpectrogram(93, nBins, 0)

	// Place a strong Costas sync signal at a known position.
	// Choose baseBin 100 → frequency = 100 * binWidth Hz.
	const baseBin = 100
	const timeOff = 5
	const syncPower = float32(50.0)
	placeCostasSignal(sg, timeOff, baseBin, syncPower)

	candidates := FindCandidates(sg, 10)

	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate, got 0")
	}

	// The best candidate should be at the placed position.
	best := candidates[0]
	binWidth := spectrogramBinWidth(nBins)
	wantFreq := float32(baseBin) * binWidth
	wantTime := float32(timeOff) * SymbolPeriod

	if !approxEq(best.Freq, wantFreq, binWidth) {
		t.Errorf("best.Freq = %g Hz, want %g Hz (±%g)", best.Freq, wantFreq, binWidth)
	}
	if !approxEq(best.TimeOff, wantTime, SymbolPeriod) {
		t.Errorf("best.TimeOff = %g s, want %g s (±%g)", best.TimeOff, wantTime, SymbolPeriod)
	}
	if best.Score <= 0 {
		t.Errorf("best.Score = %g, want > 0", best.Score)
	}
}

// --- Full signal (sync + data) detection ---

func TestFindCandidatesFullSignal(t *testing.T) {
	const nBins = 1025
	sg := makeSpectrogram(93, nBins, 0)

	const baseBin = 200
	const timeOff = 3
	placeFullSignal(sg, timeOff, baseBin, 100.0, 20.0)

	candidates := FindCandidates(sg, 10)

	if len(candidates) == 0 {
		t.Fatal("expected at least 1 candidate, got 0")
	}

	best := candidates[0]
	binWidth := spectrogramBinWidth(nBins)
	wantFreq := float32(baseBin) * binWidth

	if !approxEq(best.Freq, wantFreq, binWidth) {
		t.Errorf("best.Freq = %g Hz, want %g Hz", best.Freq, wantFreq)
	}
}

// --- Multiple signals ---

func TestFindCandidatesMultipleSignals(t *testing.T) {
	const nBins = 1025
	sg := makeSpectrogram(93, nBins, 0)

	// Two signals at different frequencies, same time offset.
	const baseBin1 = 100
	const baseBin2 = 300
	const timeOff = 0
	placeCostasSignal(sg, timeOff, baseBin1, 50.0)
	placeCostasSignal(sg, timeOff, baseBin2, 80.0)

	candidates := FindCandidates(sg, 10)

	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}

	binWidth := spectrogramBinWidth(nBins)
	wantFreq1 := float32(baseBin1) * binWidth
	wantFreq2 := float32(baseBin2) * binWidth

	// The stronger signal (baseBin2) should be first (higher score).
	found1, found2 := false, false
	for _, c := range candidates {
		if approxEq(c.Freq, wantFreq1, binWidth*2) {
			found1 = true
		}
		if approxEq(c.Freq, wantFreq2, binWidth*2) {
			found2 = true
		}
	}

	if !found1 {
		t.Errorf("signal at bin %d (%.1f Hz) not found", baseBin1, wantFreq1)
	}
	if !found2 {
		t.Errorf("signal at bin %d (%.1f Hz) not found", baseBin2, wantFreq2)
	}

	// The stronger signal should rank higher.
	if !approxEq(candidates[0].Freq, wantFreq2, binWidth) {
		t.Errorf("expected stronger signal (~%.1f Hz) first, got %.1f Hz",
			wantFreq2, candidates[0].Freq)
	}
}

// --- Sorting ---

func TestFindCandidatesSorted(t *testing.T) {
	const nBins = 1025
	sg := makeSpectrogram(93, nBins, 0)

	// Three signals at different power levels.
	placeCostasSignal(sg, 0, 100, 30.0)
	placeCostasSignal(sg, 0, 200, 90.0)
	placeCostasSignal(sg, 0, 300, 60.0)

	candidates := FindCandidates(sg, 100)

	for i := 1; i < len(candidates); i++ {
		if candidates[i].Score > candidates[i-1].Score {
			t.Errorf("candidates not sorted: [%d].Score=%g > [%d].Score=%g",
				i, candidates[i].Score, i-1, candidates[i-1].Score)
		}
	}
}

// --- MaxCandidates truncation ---

func TestFindCandidatesMaxCandidates(t *testing.T) {
	const nBins = 1025
	sg := makeSpectrogram(93, nBins, 0)

	// Place several signals.
	placeCostasSignal(sg, 0, 100, 50.0)
	placeCostasSignal(sg, 0, 200, 60.0)
	placeCostasSignal(sg, 0, 300, 70.0)

	candidates := FindCandidates(sg, 2)

	if len(candidates) > 2 {
		t.Errorf("got %d candidates, want ≤ 2", len(candidates))
	}
}

// --- Score properties ---

// TestSyncScorePositiveForSyncOnly verifies that a signal with energy only
// at sync positions produces a positive score.
func TestSyncScorePositiveForSyncOnly(t *testing.T) {
	const nBins = 1025
	sg := makeSpectrogram(93, nBins, 0)
	placeCostasSignal(sg, 0, 100, 10.0)

	score := syncScore(sg, 0, 100)
	if score <= 0 {
		t.Errorf("sync-only signal: score = %g, want > 0", score)
	}
}

// TestSyncScoreZeroForUniform verifies that a uniform-power spectrogram
// produces a score of zero (no excess sync power).
func TestSyncScoreZeroForUniform(t *testing.T) {
	const nBins = 1025
	sg := makeSpectrogram(93, nBins, 5.0) // uniform power everywhere

	score := syncScore(sg, 0, 100)
	if !approxEq(score, 0, 0.01) {
		t.Errorf("uniform spectrogram: score = %g, want ~0", score)
	}
}

// TestSyncScoreHigherForStrongerSignal verifies that a stronger signal
// produces a higher score.
func TestSyncScoreHigherForStrongerSignal(t *testing.T) {
	const nBins = 1025

	sg1 := makeSpectrogram(93, nBins, 0)
	placeCostasSignal(sg1, 0, 100, 10.0)
	score1 := syncScore(sg1, 0, 100)

	sg2 := makeSpectrogram(93, nBins, 0)
	placeCostasSignal(sg2, 0, 100, 50.0)
	score2 := syncScore(sg2, 0, 100)

	if score2 <= score1 {
		t.Errorf("stronger signal: score %g ≤ weaker %g", score2, score1)
	}
}

// --- Integration: spectrogram from synthesised tones ---

// TestFindCandidatesFromSynthesisedTones generates actual audio samples
// with Costas sync tones, builds a spectrogram, and verifies that
// FindCandidates detects the signal at the correct frequency.
func TestFindCandidatesFromSynthesisedTones(t *testing.T) {
	const nFrames = 93
	const step = SamplesPerSymbol
	nSamples := nFrames * step

	// Base frequency: 1000 Hz. Tones at 1000, 1006.25, ..., 1043.75 Hz.
	const baseFreqHz = 1000.0

	// Generate audio: silence everywhere, then add Costas sync tones.
	samples := make([]float32, nSamples)

	// Place Costas tones starting at time offset 0.
	for _, start := range [3]int{Sync1Start, Sync2Start, Sync3Start} {
		for j := range SyncLen {
			symIdx := start + j
			toneFreq := baseFreqHz + float64(CostasSync[j])*ToneSpacing
			sampleStart := symIdx * step
			for n := range step {
				samples[sampleStart+n] += float32(
					math.Cos(2 * math.Pi * toneFreq * float64(sampleStart+n) / float64(SampleRate)))
			}
		}
	}

	// Build spectrogram.
	sg := Spectrogram(samples, step, step)
	if len(sg) == 0 {
		t.Fatal("empty spectrogram")
	}

	candidates := FindCandidates(sg, 10)
	if len(candidates) == 0 {
		t.Fatal("no candidates found from synthesised signal")
	}

	// The best candidate's frequency should be near 1000 Hz.
	best := candidates[0]
	binWidth := spectrogramBinWidth(len(sg[0]))

	if !approxEq(best.Freq, baseFreqHz, binWidth*3) {
		t.Errorf("best.Freq = %g Hz, want ~%g Hz (±%g)",
			best.Freq, baseFreqHz, binWidth*3)
	}

	// Time offset should be near 0 seconds.
	if best.TimeOff > SymbolPeriod*2 {
		t.Errorf("best.TimeOff = %g s, want near 0", best.TimeOff)
	}
}

// --- Exact minimum spectrogram size ---

func TestFindCandidatesMinimumSize(t *testing.T) {
	// Exactly 79 frames and 8 bins — the absolute minimum.
	// No candidates expected (too few bins for the frequency search range),
	// but it should not panic.
	sg := makeSpectrogram(NumSymbols, NumTones, 0)
	c := FindCandidates(sg, 10)
	// Just verifying no panic; result may or may not be nil depending
	// on whether minBin..maxBin maps into the narrow bin range.
	_ = c
}
