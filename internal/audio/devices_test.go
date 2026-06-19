package audio

import (
	"strings"
	"testing"
)

// TestMatchDeviceName covers the review 2026-06-19 M1 contract: a unique name
// resolves to its index, a missing name errors, and a DUPLICATE name errors
// ("ambiguous") rather than silently resolving to the first match — the
// wrong-codec guard for multi-rig stations with same-named USB codecs.
func TestMatchDeviceName(t *testing.T) {
	names := []string{"PCM2901 Codec", "PCM2903C Codec", "PCM2901 Codec"} // index 0 and 2 collide

	t.Run("unique name resolves", func(t *testing.T) {
		idx, err := MatchDeviceName(names, "PCM2903C Codec", "capture")
		if err != nil || idx != 1 {
			t.Fatalf("idx=%d err=%v, want idx=1 err=nil", idx, err)
		}
	})

	t.Run("missing name errors", func(t *testing.T) {
		if _, err := MatchDeviceName(names, "Nonexistent", "playback"); err == nil ||
			!strings.Contains(err.Error(), "not found") {
			t.Fatalf("err=%v, want a not-found error", err)
		}
	})

	t.Run("duplicate name is ambiguous, not first-match", func(t *testing.T) {
		idx, err := MatchDeviceName(names, "PCM2901 Codec", "capture")
		if err == nil {
			t.Fatalf("got idx=%d nil err, want an ambiguity error (must NOT first-match index 0)", idx)
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("err = %q, want it to say ambiguous", err.Error())
		}
	})
}
