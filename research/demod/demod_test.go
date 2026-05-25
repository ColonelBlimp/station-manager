package demod

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
)

// TestIsCostas pins the channel-symbol → is-Costas mapping. The 21
// Costas positions are 0..6, 36..42, 72..78; every other 0..78 is a
// data symbol. The total accounting (21 Costas + 58 data = 79) is
// load-bearing for Demod's output shape and worth a direct test.
func TestIsCostas(t *testing.T) {
	expectedCostas := map[int]bool{}
	for _, s := range []int{0, 1, 2, 3, 4, 5, 6, 36, 37, 38, 39, 40, 41, 42, 72, 73, 74, 75, 76, 77, 78} {
		expectedCostas[s] = true
	}
	dataCount, costasCount := 0, 0
	for s := 0; s < nn; s++ {
		got := isCostas(s)
		want := expectedCostas[s]
		if got != want {
			t.Errorf("isCostas(%d) = %v, want %v", s, got, want)
		}
		if got {
			costasCount++
		} else {
			dataCount++
		}
	}
	if costasCount != 21 {
		t.Errorf("Costas count = %d, want 21", costasCount)
	}
	if dataCount != dataSymbolCount {
		t.Errorf("data count = %d, want %d", dataCount, dataSymbolCount)
	}
}

// TestDemod_CleanSignalsHaveDominantTonePerSymbol verifies on a clean
// synthetic fixture that every data symbol produces a clear tone
// winner — the dominant tone's energy is ≥ 2× the second-strongest
// tone's energy. This is a decoder-agnostic sanity check: it doesn't
// know the encoded bit sequence, but at clean SNR the 8-FSK symbol
// power should land overwhelmingly in one of the 8 bins, and a
// failure here points at gross misalignment (wrong txStart frame,
// wrong tone-spacing constant, wrong window length) rather than a
// subtle SNR margin.
//
// The "few weak symbols allowed" budget covers two real effects we
// will see even on a perfectly clean fixture:
//   - inter-symbol crosstalk at the symbol boundary (the rectangular
//     Goertzel window doesn't separate adjacent symbols perfectly)
//   - the candidate's (freq, dt) coming off the refinement grid may
//     sit a few Hz / ms off the true TX position, slightly diluting
//     the per-symbol contrast
//
// A budget of 5/58 (~9%) is a calibrated ceiling — empirically the
// clean fixture lands well under this. Tighten if it ever does.
func TestDemod_CleanSignalsHaveDominantTonePerSymbol(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}
	cands := candidates.Find(data.Samples)
	if len(cands) == 0 {
		t.Fatal("no candidates found in clean fixture")
	}

	// Pick the strongest by stage-1 score — gives us the highest-SNR
	// signal in the slot, which is the right yardstick for "should
	// have a clean dominant tone everywhere".
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	top := cands[0]

	energies := Demod(data.Samples, top.Freq, top.DT)

	weakSymbols := 0
	for i := 0; i < dataSymbolCount; i++ {
		first, second := 0.0, 0.0
		for j := 0; j < ft8ToneCount; j++ {
			e := energies[i][j]
			if e > first {
				second = first
				first = e
			} else if e > second {
				second = e
			}
		}
		if first == 0 {
			t.Fatalf("data symbol %d: all-zero energies (alignment bug?)", i)
		}
		if second > 0 && first < 2*second {
			weakSymbols++
		}
	}
	const weakBudget = 5
	if weakSymbols > weakBudget {
		t.Errorf("weak-winner data symbols: %d > budget %d (top candidate freq=%.2f dt=%.3f)",
			weakSymbols, weakBudget, top.Freq, top.DT)
	}
	t.Logf("clean fixture, top candidate freq=%.2f dt=%.3f: %d/%d weak symbols (budget %d)",
		top.Freq, top.DT, weakSymbols, dataSymbolCount, weakBudget)
}

// TestDemod_RespectsBufferBounds confirms the slot-edge handling
// contract: out-of-bounds data symbols leave their row at zero, and
// in-bounds rows are populated. A dtSec far outside the slot makes
// every data symbol fall outside the buffer; Demod should return the
// all-zero matrix without panicking.
func TestDemod_RespectsBufferBounds(t *testing.T) {
	samples := make([]float32, 180000)
	out := Demod(samples, 1500.0, 1000.0) // dtSec 1000 s → every symStart way past EOF
	for i := 0; i < dataSymbolCount; i++ {
		for j := 0; j < ft8ToneCount; j++ {
			if out[i][j] != 0 {
				t.Fatalf("expected zero energies for far-future dtSec, got out[%d][%d] = %v", i, j, out[i][j])
			}
		}
	}
}
