package unpack_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
	"github.com/ColonelBlimp/station-manager/research/ldpc"
	"github.com/ColonelBlimp/station-manager/research/truth"
	"github.com/ColonelBlimp/station-manager/research/unpack"
)

// TestUnpack_CleanFixtureMatchesTruth is the structural integration
// check: run the full research pipeline (candidates → demod → LLRs
// → ldpc.Decode → unpack) on the clean synthetic fixture, and
// verify every CRC-passing decode produces the text recorded in
// the truth manifest. A mismatch here means bit-alignment, field
// ordering, or one of the c28/g15 decoders is wrong.
//
// Every decode must match — clean fixture is end-to-end deterministic.
func TestUnpack_CleanFixtureMatchesTruth(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav: %v", err)
	}
	manifest, err := truth.Read(truth.PathFor(wavPath))
	if err != nil {
		t.Fatalf("read truth: %v", err)
	}
	if manifest == nil {
		t.Fatal("no truth manifest beside clean fixture")
	}

	// Build a set of expected texts.
	expected := make(map[string]bool, len(manifest.Signals))
	for _, s := range manifest.Signals {
		expected[s.Text] = true
	}

	cands := candidates.Find(data.Samples)
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })

	matched := 0
	mismatches := 0
	unsupported := 0
	for _, c := range cands {
		energies := demod.Demod(data.Samples, c.Freq, c.DT)
		llrs := demod.LLRs(energies)
		var input [174]float64
		for k := 0; k < 174; k++ {
			input[k] = llrs[k]
		}
		_, stats := ldpc.Decode(input)
		if !stats.ConvergedCRC {
			continue
		}
		// Need the actual decoded info word. Re-decode to get the Result.
		result, _ := ldpc.Decode(input)
		got, err := unpack.Unpack(result.Info)
		if err != nil {
			t.Logf("unsupported type at freq=%.2f dt=%.3f: %v", c.Freq, c.DT, err)
			unsupported++
			continue
		}
		if expected[got.Text] {
			matched++
			delete(expected, got.Text) // each truth claimed at most once
			t.Logf("OK: freq=%.2f dt=%.3f → %q", c.Freq, c.DT, got.Text)
		} else {
			mismatches++
			t.Errorf("UNEXPECTED TEXT: freq=%.2f dt=%.3f → %q", c.Freq, c.DT, got.Text)
		}
	}

	if matched < 10 {
		t.Errorf("matched only %d of 10 expected clean-fixture decodes", matched)
		if len(expected) > 0 {
			t.Logf("unmatched truth entries:")
			for k := range expected {
				t.Logf("  - %q", k)
			}
		}
	}
	if mismatches > 0 {
		t.Errorf("%d CRC-passing decode(s) produced text not in truth manifest", mismatches)
	}
	if unsupported > 0 {
		t.Logf("%d CRC-passing decode(s) had unsupported message types (i3 != 1)", unsupported)
	}
}
