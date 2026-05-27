package candidates

import (
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// TestRefinementGate_GateAwarePreservesInvariant pins the invariant
// the Session-96 RefinementGateAware mode is supposed to maintain:
// EVERY returned candidate's Verify must still pass the gate that
// admitted it pre-refinement. With the operator's "rollback if no
// gate-passing neighbour improves" semantics, gate failures must
// be zero across the corpus regardless of how the corpus is shaped.
func TestRefinementGate_GateAwarePreservesInvariant(t *testing.T) {
	var captures []string
	for _, pattern := range []string{
		"../../captures/*.wav",
		"../../captures/synthetic/*.wav",
	} {
		matches, _ := filepath.Glob(pattern)
		captures = append(captures, matches...)
	}
	if len(captures) == 0 {
		t.Skip("no capture .wav files")
	}

	gates := DefaultGates
	gates.RefinementMode = RefinementGateAware

	totalGateFailures := 0
	for _, wavPath := range captures {
		data, err := audio.ReadWAV(wavPath)
		if err != nil || data.SampleRate != 12000 || data.Channels != 1 {
			continue
		}
		cands := FindWithGates(data.Samples, gates)
		for _, c := range cands {
			if c.Verify == nil {
				continue
			}
			if !passesCostasGate(c.Verify, gates) {
				totalGateFailures++
				t.Errorf("%s: refined candidate at (%.2f Hz, %+.3f s) fails gate "+
					"with WinsTotal=%d, blocks=%v — RefinementGateAware should never return this",
					filepath.Base(wavPath), c.Freq, c.DT, c.Verify.WinsTotal, c.Verify.WinsBlock)
			}
		}
	}
	if totalGateFailures == 0 {
		t.Logf("RefinementGateAware: invariant holds across %d captures (0 gate failures)", len(captures))
	}
}

// TestRefinementGate_DoesNotInvalidate is the documentary measurement
// for the 2026-05-26 code-review finding: "candidate refinement can
// invalidate the stage-2 gate." The Find pipeline applies
// `gates.SanityWinsTotal` and `gates.SanityWinsPerBlock` BEFORE
// calling refineCandidate; refineCandidate optimises GeoContrast,
// which can in principle drift to a position where the (now refined)
// CostasVerify no longer satisfies those gates.
//
// In practice GeoContrast and the win-count maxima usually co-locate
// at the signal, so the gap may never materialise on real data. This
// test measures empirically: count how often a returned Candidate's
// refined Verify would fail the same gates Find applied before
// refinement.
//
// Always passes (`t.Logf` only) — purpose is to surface the count, not
// to gate regressions. If the count is consistently 0 across captures,
// the finding is theoretical and no code fix is required. If > 0, the
// fix is to either re-apply the gate post-refinement or roll back to
// the pre-refinement position when the refined version fails.
//
// Run with `go test ./research/candidates/ -run TestRefinementGate -v`
// to see the per-fixture and aggregate counts.
func TestRefinementGate_DoesNotInvalidate(t *testing.T) {
	// Gather captures across both real and synthetic directories.
	// Real captures stress real-world refinement; synthetic stress
	// the on-grid corner case (refinement should be a no-op there).
	var captures []string
	for _, pattern := range []string{
		"../../captures/*.wav",
		"../../captures/synthetic/*.wav",
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		captures = append(captures, matches...)
	}
	if len(captures) == 0 {
		t.Skip("no capture .wav files found under ../../captures/")
	}

	var (
		totalCandidates  int
		totalGateFailure int
		totalWinsTotLow  int
		totalPerBlockLow int
	)

	for _, wavPath := range captures {
		data, err := audio.ReadWAV(wavPath)
		if err != nil {
			t.Logf("skip %s: %v", wavPath, err)
			continue
		}
		if data.SampleRate != 12000 || data.Channels != 1 {
			t.Logf("skip %s: rate=%d channels=%d", wavPath, data.SampleRate, data.Channels)
			continue
		}
		cands := Find(data.Samples)
		fileCandidates := len(cands)
		fileGateFailure := 0
		fileWinsTotLow := 0
		filePerBlockLow := 0
		for _, c := range cands {
			v := c.Verify
			if v == nil {
				continue
			}
			winsLow := v.WinsTotal < sanityWinsTotal
			blockLow := false
			for b := 0; b < numCostasBlocks; b++ {
				// Per the gate logic in Find: per-block floor applies
				// only to blocks where at least one anchor was accessible.
				if v.AccessibleBlock[b] == 0 {
					continue
				}
				if v.WinsBlock[b] < sanityWinsPerBlock {
					blockLow = true
					break
				}
			}
			if winsLow {
				fileWinsTotLow++
			}
			if blockLow {
				filePerBlockLow++
			}
			if winsLow || blockLow {
				fileGateFailure++
			}
		}
		totalCandidates += fileCandidates
		totalWinsTotLow += fileWinsTotLow
		totalPerBlockLow += filePerBlockLow
		totalGateFailure += fileGateFailure
		t.Logf("%-50s candidates=%d  gate-fail=%d  (winsTot<%d=%d  perBlock<%d=%d)",
			filepath.Base(wavPath), fileCandidates, fileGateFailure,
			sanityWinsTotal, fileWinsTotLow, sanityWinsPerBlock, filePerBlockLow)
	}

	t.Logf("")
	t.Logf("=== CORPUS TOTALS ===")
	t.Logf("  capture files: %d", len(captures))
	t.Logf("  total returned candidates: %d", totalCandidates)
	t.Logf("  refined-position fails gate (any): %d", totalGateFailure)
	t.Logf("  refined-position fails WinsTotal >= %d: %d", sanityWinsTotal, totalWinsTotLow)
	t.Logf("  refined-position fails WinsPerBlock >= %d: %d", sanityWinsPerBlock, totalPerBlockLow)
	if totalGateFailure == 0 {
		t.Logf("VERDICT: refinement never invalidates the gate on this corpus — finding #2 is theoretical.")
	} else {
		t.Logf("VERDICT: refinement invalidates the gate in %d/%d cases (raw measurement).",
			totalGateFailure, totalCandidates)
		t.Logf("Session 96 measurement on this corpus: gate-failing refined positions are empirically benign")
		t.Logf("(they don't produce CRC-passing decodes). RefinementGateAware was prototyped but kept off by")
		t.Logf("default — trade-off was +1 matched/+3 extras at production config vs -1 matched/-1 extras at")
		t.Logf("baseline. Available via gates.RefinementMode = RefinementGateAware for A/B re-measurement on")
		t.Logf("future corpus data.")
	}
}
