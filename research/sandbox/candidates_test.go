package sandbox

import "testing"

// TestSuppressOverlapsK2_TightDedup verifies a strict near-duplicate
// of an already-kept candidate is dropped.
func TestSuppressOverlapsK2_TightDedup(t *testing.T) {
	cands := []Candidate{
		{FreqHz: 1000, DtSec: 0.5, Sync: 10},
		{FreqHz: 1000.5, DtSec: 0.51, Sync: 5}, // within tight box
	}
	kept := SuppressOverlapsK2(cands, 3.125, 0.04, 6.25, 2, 100)
	if len(kept) != 1 {
		t.Fatalf("expected 1 kept (tight near-dupe dropped), got %d", len(kept))
	}
	if kept[0].Sync != 10 {
		t.Errorf("expected higher-score candidate kept, got Sync=%g", kept[0].Sync)
	}
}

// TestSuppressOverlapsK2_SameFreqDiffDt verifies two candidates at the
// same frequency but well-separated in dt both survive when K=2.
func TestSuppressOverlapsK2_SameFreqDiffDt(t *testing.T) {
	cands := []Candidate{
		{FreqHz: 1000, DtSec: 0.0, Sync: 10},
		{FreqHz: 1000, DtSec: 0.5, Sync: 7}, // same freq, well-separated dt
	}
	kept := SuppressOverlapsK2(cands, 3.125, 0.04, 6.25, 2, 100)
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept (same-freq pair admitted at K=2), got %d", len(kept))
	}
}

// TestSuppressOverlapsK2_GroupCap verifies the per-group K cap fires
// when more than K candidates land in the same freq group with
// sufficient dt separation to escape the tight box.
func TestSuppressOverlapsK2_GroupCap(t *testing.T) {
	cands := []Candidate{
		{FreqHz: 1000, DtSec: 0.0, Sync: 10},
		{FreqHz: 1002, DtSec: 0.3, Sync: 8}, // within group, escapes tight (dt diff)
		{FreqHz: 1001, DtSec: 0.7, Sync: 6}, // within group, escapes tight (dt diff) — should be capped
	}
	kept := SuppressOverlapsK2(cands, 3.125, 0.04, 6.25, 2, 100)
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept (third candidate exceeds K=2 group cap), got %d", len(kept))
	}
	if kept[0].Sync != 10 || kept[1].Sync != 8 {
		t.Errorf("expected highest-scoring 2 kept, got Sync=%g and %g", kept[0].Sync, kept[1].Sync)
	}
}

// TestSuppressOverlapsK2_DifferentFreqs verifies candidates in
// different freq groups are unaffected by the group cap.
func TestSuppressOverlapsK2_DifferentFreqs(t *testing.T) {
	cands := []Candidate{
		{FreqHz: 1000, DtSec: 0.0, Sync: 10},
		{FreqHz: 1050, DtSec: 0.0, Sync: 8}, // way outside group
		{FreqHz: 1100, DtSec: 0.0, Sync: 6}, // way outside group
	}
	kept := SuppressOverlapsK2(cands, 3.125, 0.04, 6.25, 2, 100)
	if len(kept) != 3 {
		t.Fatalf("expected 3 kept (distinct freq groups, K-cap doesn't apply), got %d", len(kept))
	}
}
