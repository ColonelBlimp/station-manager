package dsp

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// injectCostas places the three Costas array patterns (start,
// middle, end of slot) into a synthetic spectrogram at a known
// centre frequency bin and time offset. Used to verify the sync
// detector finds the patterns it's supposed to find.
//
// centreBin: spectrogram-bin index for the candidate's centre
// (each bin = 3.125 Hz). The 7 Costas tones land at centreBin +
// SpectrogramOversampleFreq*Icos7[k].
//
// dtSteps: time offset in spectrogram steps from the nominal TX
// start (0 = perfectly aligned).
//
// powerPerCell: spectrogram power deposited at each in-pattern
// (time, freq) cell. Real FT8 signals have per-cell power in the
// 5-100× noise-floor range depending on SNR.
func injectCostas(spec [][]float64, centreBin, dtSteps int, powerPerCell float64) {
	const tstep = float64(NSTEP) / Fs
	nominalStartStep := int(math.Floor(nominalTXStartSeconds / tstep))

	for block := 0; block < NumCostasBlocks; block++ {
		blockStartSym := block * CostasBlockStrideSymbols
		for symInBlock := 0; symInBlock < CostasTonesPerBlock; symInBlock++ {
			channelSym := blockStartSym + symInBlock
			tStart := dtSteps + nominalStartStep + channelSym*SpectrogramStepsPerSymbol
			tone := int(Icos7[symInBlock])
			toneBin := centreBin + SpectrogramOversampleFreq*tone

			for step := 0; step < SpectrogramStepsPerSymbol; step++ {
				t := tStart + step
				if t < 0 || t >= NHSYM || toneBin < 0 || toneBin >= NH1 {
					continue
				}
				spec[t][toneBin] += powerPerCell
			}
		}
	}
}

// addNoiseFloor adds a uniform low-level noise floor to every
// (time, freq) cell — required so the score's noiseMean denominator
// is non-zero and the ratio score has signal-vs-noise meaning.
func addNoiseFloor(spec [][]float64, level float64) {
	for t := range spec {
		for f := range spec[t] {
			spec[t][f] += level
		}
	}
}

// makeSyntheticSpec builds an NHSYM × NH1 spectrogram with a
// uniform noise floor of 1.0 — the canonical baseline for the
// injection-based sync tests below.
func makeSyntheticSpec() [][]float64 {
	backing := make([]float64, NHSYM*NH1)
	spec := make([][]float64, NHSYM)
	for t := range spec {
		spec[t] = backing[t*NH1 : (t+1)*NH1]
	}
	addNoiseFloor(spec, 1.0)
	return spec
}

// TestSync_DetectsInjectedCostasAt1500Hz pins the headline path: a
// single Costas pattern injected at bin 480 (= 1500 Hz, the FT8
// default centre) and time offset 0 should be detected with the
// candidate at the expected frequency + time.
func TestSync_DetectsInjectedCostasAt1500Hz(t *testing.T) {
	const (
		centreBin    = 480 // 1500 Hz
		dtSteps      = 0
		powerPerCell = 50.0 // 50× noise floor — strong signal
		df           = Fs / float64(NFFT1)
	)

	spec := makeSyntheticSpec()
	injectCostas(spec, centreBin, dtSteps, powerPerCell)

	cands := Sync(spec, SyncOptions{})
	if len(cands) == 0 {
		t.Fatal("Sync returned no candidates for injected signal")
	}

	// First candidate (descending sync sort) should be at the
	// injection point.
	got := cands[0]
	wantFreq := float64(centreBin) * df
	if math.Abs(got.Freq-wantFreq) > df {
		t.Errorf("Freq = %g Hz, want %g Hz (±%g bin)", got.Freq, wantFreq, df)
	}
	if math.Abs(got.DT) > 0.05 {
		t.Errorf("DT = %g s, want ~0 (±50 ms)", got.DT)
	}
	// With 50× noise-floor signal across all 21 Costas tones, the
	// in-pattern mean should comfortably exceed the noise mean.
	if got.SyncPower < 5.0 {
		t.Errorf("SyncPower = %g, want >= 5.0 (50× signal vs 1× noise should ratio out clean)", got.SyncPower)
	}
}

// TestSync_DetectsMultipleSignals confirms the detector pulls out
// multiple distinct candidates from a synthetic busy spectrogram.
// Three Costas patterns at different freqs + DTs → all detected.
func TestSync_DetectsMultipleSignals(t *testing.T) {
	const df = Fs / float64(NFFT1)
	signals := []struct {
		centreBin int
		dtSteps   int
	}{
		{200, 0},  // 625 Hz, aligned
		{400, 3},  // 1250 Hz, +120 ms
		{700, -2}, // 2187.5 Hz, -80 ms
	}

	spec := makeSyntheticSpec()
	for _, s := range signals {
		injectCostas(spec, s.centreBin, s.dtSteps, 50.0)
	}

	cands := Sync(spec, SyncOptions{})
	if len(cands) < len(signals) {
		t.Fatalf("got %d candidates, want >= %d", len(cands), len(signals))
	}

	const tstep = float64(NSTEP) / Fs
	for _, s := range signals {
		wantFreq := float64(s.centreBin) * df
		wantDT := float64(s.dtSteps) * tstep
		found := false
		for _, c := range cands {
			if math.Abs(c.Freq-wantFreq) <= df &&
				math.Abs(c.DT-wantDT) <= 2*tstep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no candidate within ±1 bin / ±80 ms of (%g Hz, %g s)", wantFreq, wantDT)
		}
	}
}

// TestSync_NoSignalReturnsNoCandidates pins that an all-noise
// spectrogram produces no false positives at the default threshold.
func TestSync_NoSignalReturnsNoCandidates(t *testing.T) {
	spec := makeSyntheticSpec() // noise floor 1.0, no injection
	cands := Sync(spec, SyncOptions{MinScore: 2.0})
	if len(cands) > 0 {
		t.Errorf("got %d candidates from noise-only spec; want 0", len(cands))
		for i, c := range cands {
			t.Logf("spurious [%d]: freq=%g dt=%g power=%g", i, c.Freq, c.DT, c.SyncPower)
		}
	}
}

// TestSync_NearDuplicateSuppression injects a single Costas pattern
// and verifies the detector doesn't emit a cluster of near-duplicate
// candidates from adjacent freq bins / time-steps picking up the
// same actual transmission.
func TestSync_NearDuplicateSuppression(t *testing.T) {
	spec := makeSyntheticSpec()
	injectCostas(spec, 480, 0, 50.0)

	cands := Sync(spec, SyncOptions{})
	if len(cands) == 0 {
		t.Fatal("Sync returned no candidates")
	}

	// Count candidates within the dedup window of the injection
	// point. Should be exactly 1 (the actual signal); anything more
	// means the dedup pass is leaking near-dupes.
	const (
		df    = Fs / float64(NFFT1)
		tstep = float64(NSTEP) / Fs
	)
	wantFreq := 480 * df
	dupes := 0
	for _, c := range cands {
		if math.Abs(c.Freq-wantFreq) <= df &&
			math.Abs(c.DT) <= 2*tstep {
			dupes++
		}
	}
	if dupes != 1 {
		t.Errorf("dedup let %d near-dupe candidates through at the injection point; want 1", dupes)
	}
}

// TestSync_DedupAtExactBoundary pins finding #4: the dedup window
// uses integer coordinate (bin index, time-step index) comparisons,
// not float Hz/seconds derived from them. A float comparison at
// exactly the dedup threshold can let near-duplicates survive due
// to rounding — e.g. before the fix, ft8_cap1.wav produced two
// candidates at 1862.50 Hz exactly two time-steps apart (DT=-0.60
// and DT=-0.68), even though the documented dedup window is 2
// time-steps.
//
// Synthesise two Costas patterns at the same bin, exactly
// syncDedupTimeSteps apart in time. Confirm only one candidate
// survives the dedup pass.
func TestSync_DedupAtExactBoundary(t *testing.T) {
	spec := makeSyntheticSpec()
	// Strong injection at dtSteps=0; weaker at dtSteps=syncDedupTimeSteps.
	injectCostas(spec, 480, 0, 50.0)
	injectCostas(spec, 480, syncDedupTimeSteps, 30.0)

	cands := Sync(spec, SyncOptions{})
	if len(cands) == 0 {
		t.Fatal("Sync returned no candidates")
	}

	// Count candidates within the dedup window of the injection
	// point (1 bin × syncDedupTimeSteps). Should be exactly 1 —
	// the stronger one wins; the boundary-distant one is suppressed.
	const (
		df    = Fs / float64(NFFT1)
		tstep = float64(NSTEP) / Fs
	)
	wantFreq := 480 * df
	near := 0
	for _, c := range cands {
		if math.Abs(c.Freq-wantFreq) <= df &&
			math.Abs(c.DT) <= float64(syncDedupTimeSteps)*tstep {
			near++
		}
	}
	if near != 1 {
		t.Errorf("got %d candidates within dedup window of boundary-spaced injections; want exactly 1 (stronger wins, boundary-distant suppressed)", near)
		for i, c := range cands {
			t.Logf("  [%d] freq=%g dt=%g power=%g", i, c.Freq, c.DT, c.SyncPower)
		}
	}
}

// TestSync_RejectsInvalidSpec verifies the validSpec guard.
func TestSync_RejectsInvalidSpec(t *testing.T) {
	cases := [][][]float64{
		nil,
		{},
		make([][]float64, NHSYM-1), // wrong row count
		{make([]float64, NH1-1)},   // wrong col count + wrong row count
	}
	for i, spec := range cases {
		if got := Sync(spec, SyncOptions{}); got != nil {
			t.Errorf("case %d: Sync returned %d candidates on invalid spec; want nil", i, len(got))
		}
	}
}

// TestSync_RealCapture_SmokeTest exercises the full audio →
// spectrogram → sync pipeline on a real WSJT-X capture if the
// fixture is present. Skipped gracefully when missing.
//
// The capture is operator-owned (recorded with their FT8 station);
// not bundled in the SM repo. Test resolves the path via
// $FT8_TEST_CORPUS (preferred — per the milestone-4 L4 test
// architecture) or a fallback relative path to the operator's
// sibling go-ft8 research repo.
func TestSync_RealCapture_SmokeTest(t *testing.T) {
	wavPath := ""
	if env := os.Getenv("FT8_TEST_CORPUS"); env != "" {
		p := filepath.Join(env, "ft8_cap1.wav")
		if _, err := os.Stat(p); err == nil {
			wavPath = p
		}
	}
	if wavPath == "" {
		// Vendored fixture is one level up at internal/ft8/testdata/.
		// See internal/ft8/testdata/README.md for provenance.
		vendored := filepath.Join("..", "testdata", "ft8_cap1.wav")
		if _, err := os.Stat(vendored); err == nil {
			wavPath = vendored
		}
	}
	if wavPath == "" {
		t.Skip("no FT8 capture fixture available; set FT8_TEST_CORPUS or vendor internal/ft8/testdata/")
	}

	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("ReadWAV(%q): %v", wavPath, err)
	}
	if data.SampleRate != Fs {
		t.Fatalf("sample rate = %d, want %g (FT8 canonical)", data.SampleRate, Fs)
	}
	if data.Channels != 1 {
		t.Fatalf("channels = %d, want 1 (mono)", data.Channels)
	}

	spec := Spectrogram(data.Samples)
	cands := Sync(spec, SyncOptions{})

	// We don't pin a specific candidate count — the detector's
	// sensitivity is intentionally simpler than WSJT-X's, so the
	// expected count will be lower. But any well-populated FT8
	// slot recording should produce some candidates.
	if len(cands) == 0 {
		t.Errorf("Sync found 0 candidates in real capture %q; expected at least a few", wavPath)
	}
	t.Logf("%s: detected %d candidates", filepath.Base(wavPath), len(cands))
	maxLog := 10
	if len(cands) < maxLog {
		maxLog = len(cands)
	}
	for i := 0; i < maxLog; i++ {
		t.Logf("  [%d] freq=%7.2f Hz  dt=%+5.2f s  power=%6.2f", i, cands[i].Freq, cands[i].DT, cands[i].SyncPower)
	}
}
