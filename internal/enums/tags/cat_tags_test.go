package tags

import "testing"

func TestCatStateTagString(t *testing.T) {
	if got := Identity.String(); got != "IDENTITY" {
		t.Fatalf("Identity.String() = %q, want %q", got, "IDENTITY")
	}
}

func TestAllCatStateTags(t *testing.T) {
	want := []CatStateTag{
		Identity,
		VfoAFreq,
		VfoBFreq,
		Split,
		Select,
		MainMode,
		SubMode,
		TxPwr,
	}

	if len(AllCatStateTags) != len(want) {
		t.Fatalf("len(AllCatStateTags) = %d, want %d", len(AllCatStateTags), len(want))
	}

	for i, entry := range AllCatStateTags {
		if entry.Value != want[i] {
			t.Fatalf("AllCatStateTags[%d].Value = %q, want %q", i, entry.Value, want[i])
		}
		if entry.TSName != want[i].String() {
			t.Fatalf("AllCatStateTags[%d].TSName = %q, want %q", i, entry.TSName, want[i].String())
		}
	}
}
