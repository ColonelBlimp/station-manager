package sandbox_test

import (
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/sandbox"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

// TestCorpusBaseline pins the sandbox decoder's matched/extras totals
// against the 6-capture real-world FT8 corpus under the canonical
// configuration:
//
//   - DefaultMultiPassOptions / DefaultSearchOptions (K=2 medium NMS +
//     symmetric Extract; Session 101 default)
//   - truth.NormalizeText on both sides of text equality (Session 102 —
//     absorbs jt9-oracle manifest formatting variance: trailing "a1"
//     annotations, "R-09" vs "R -09" report convention)
//   - freq tolerance 5 Hz, dt tolerance 0.5 s (matches sandbox-asym-ab
//     and sandbox-multipass scoring)
//
// Expected: matched == 111, extras == 23 across 144 truth signals
// (6 captures × ~20-30 truths each).
//
// Baseline history:
//   - 94/29   pre-NormalizeText (Session 102 raw text equality)
//   - 107/16  post-NormalizeText, N=1+N=2 cascade (Session 102)
//   - 108/17  post-NormalizeText, N=1+N=2+N=3 cascade (Session 103)
//   - 111/23  + N1Norm bit-normalized cascade (Session 103, +3 matched
//     / +6 extras — biggest decoder-side lift so far)
//
// Why this test exists: any decoder change (LLR generation, BP+OSD
// gates, NMS tuning, channelizer geometry) can silently shift these
// numbers. Pin them here so future PRs surface regressions cleanly.
// Specifically: the upcoming bit-normalized / best-of-N cascade
// additions are expected to move matched UP — that's a deliberate
// change, and the test should be updated in the same commit. A drop
// is the bug pattern this test catches.
//
// Gated behind testing.Short() because the full corpus run takes ~45
// seconds end-to-end (per feedback_no_heavy_tests_under_race memory
// — full FT8 decode tests skip under -short so CI's -race -short
// pass stays fast).
//
// Path resolution: locates captures/ via runtime.Caller relative to
// this test file (research/sandbox/corpus_baseline_test.go → ../../
// → repo root → captures/). Test skips with t.Skip if the directory
// is absent (e.g. on a checkout without the corpus).
func TestCorpusBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus baseline: heavy decode; skipped under -short")
	}

	capturesDir := locateCapturesDir(t)
	matches, err := filepath.Glob(filepath.Join(capturesDir, "*.wav"))
	if err != nil {
		t.Fatalf("glob captures: %v", err)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		t.Skipf("no WAV files under %q — corpus not present in this checkout", capturesDir)
	}

	const (
		expectedMatched = 111
		expectedExtras  = 23
		freqTol         = 5.0
		dtTol           = 0.5
	)

	totalMatched := 0
	totalExtras := 0
	totalTruths := 0
	captureCount := 0

	for _, w := range matches {
		manifestPath := truth.PathFor(w)
		m, err := truth.Read(manifestPath)
		if err != nil {
			t.Fatalf("read manifest %q: %v", manifestPath, err)
		}
		if m == nil {
			continue // no manifest — skip (matches sandbox-asym-ab behaviour)
		}

		data, err := audio.ReadWAV(w)
		if err != nil {
			t.Fatalf("read WAV %q: %v", w, err)
		}
		if data.SampleRate != 12000 || data.Channels != 1 {
			t.Fatalf("WAV %q: expected 12 kHz mono, got %d Hz / %d channels", w, data.SampleRate, data.Channels)
		}

		opts := sandbox.DefaultMultiPassOptions()
		ht := sandbox.NewCallsignHashTable()
		decodes := sandbox.MultiPassDecodeWithHashes(data.Samples, opts, ht)

		matchedThis := 0
		matchedTruth := make([]bool, len(m.Signals))
		for _, d := range decodes {
			dText := truth.NormalizeText(d.Text)
			didMatch := false
			for i, sig := range m.Signals {
				if matchedTruth[i] {
					continue
				}
				if math.Abs(d.FreqHz-sig.FreqHz) <= freqTol &&
					math.Abs(d.DtSec-sig.DTSec) <= dtTol &&
					dText == truth.NormalizeText(sig.Text) {
					matchedTruth[i] = true
					matchedThis++
					didMatch = true
					break
				}
			}
			if !didMatch {
				totalExtras++
			}
		}
		totalMatched += matchedThis
		totalTruths += len(m.Signals)
		captureCount++
	}

	if captureCount == 0 {
		t.Skip("no manifests beside WAVs — corpus not present in this checkout")
	}

	if totalMatched != expectedMatched || totalExtras != expectedExtras {
		t.Errorf("corpus baseline drifted:\n  matched = %d (want %d)\n  extras  = %d (want %d)\n  truths  = %d across %d captures",
			totalMatched, expectedMatched, totalExtras, expectedExtras, totalTruths, captureCount)
	}
}

// locateCapturesDir resolves the captures/ directory via runtime.Caller
// of this test file. The test runs from the package directory; we walk
// up two levels (research/sandbox → research → repo root) and join with
// "captures". Returns "" if Caller fails — caller skips the test in
// that case.
func locateCapturesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller(0) failed — can't locate captures dir")
	}
	pkgDir := filepath.Dir(file)
	repoRoot := filepath.Dir(filepath.Dir(pkgDir))
	return filepath.Join(repoRoot, "captures")
}
