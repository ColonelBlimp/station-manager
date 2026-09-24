package stationevents

import "testing"

// The pair table is the single source the schema and the recorder are written
// from, so it must be well-formed: every kind under exactly one category, and
// the sets exactly the ones ruled (four notification kinds, six alarm kinds).
func TestKindsByCategory_IsAClosedWellFormedPairTable(t *testing.T) {
	pairs := KindsByCategory()
	if len(pairs) != 2 {
		t.Fatalf("categories = %d, want 2 (notification, alarm)", len(pairs))
	}
	if n := len(pairs[CategoryNotification]); n != 4 {
		t.Errorf("notification kinds = %d, want 4 (two ADR 0076 kinds + the two archive outcomes)", n)
	}
	if n := len(pairs[CategoryAlarm]); n != 6 {
		t.Errorf("alarm kinds = %d, want 6", n)
	}
	seen := map[string]string{}
	for cat, kinds := range pairs {
		for _, k := range kinds {
			if prev, dup := seen[k]; dup {
				t.Errorf("kind %q listed under both %q and %q", k, prev, cat)
			}
			seen[k] = cat
		}
	}
	// The table is returned fresh: a caller cannot edit the vocabulary.
	pairs[CategoryAlarm] = nil
	if len(KindsByCategory()[CategoryAlarm]) != 6 {
		t.Error("KindsByCategory returned a shared, mutable table")
	}
}
