package types

import "testing"

func TestResolveFt8Display(t *testing.T) {
	t.Run("nil yields all defaults", func(t *testing.T) {
		d := ResolveFt8Display(nil)
		if d.HistoryMax != DefaultFt8HistoryMax || d.FeedMode != DefaultFt8FeedMode ||
			d.HighlightUnworked != DefaultFt8HighlightUnworked || d.HighlightWorked != DefaultFt8HighlightWorked {
			t.Fatalf("nil override = %+v, want defaults", d)
		}
	})

	t.Run("zero/empty fields fall back per-field", func(t *testing.T) {
		d := ResolveFt8Display(&Ft8DisplayConfig{FeedMode: "single"})
		if d.FeedMode != "single" {
			t.Errorf("FeedMode = %q, want single", d.FeedMode)
		}
		if d.HistoryMax != DefaultFt8HistoryMax {
			t.Errorf("HistoryMax = %d, want default %d (unset)", d.HistoryMax, DefaultFt8HistoryMax)
		}
		if d.HighlightUnworked != DefaultFt8HighlightUnworked {
			t.Errorf("HighlightUnworked = %q, want default (unset)", d.HighlightUnworked)
		}
	})

	t.Run("history max clamps but unset keeps default", func(t *testing.T) {
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 0}); d.HistoryMax != DefaultFt8HistoryMax {
			t.Errorf("HistoryMax 0 (unset) = %d, want default %d", d.HistoryMax, DefaultFt8HistoryMax)
		}
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 5}); d.HistoryMax != 10 {
			t.Errorf("HistoryMax 5 = %d, want clamp to 10", d.HistoryMax)
		}
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 99999}); d.HistoryMax != 2000 {
			t.Errorf("HistoryMax 99999 = %d, want clamp to 2000", d.HistoryMax)
		}
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 250}); d.HistoryMax != 250 {
			t.Errorf("HistoryMax 250 = %d, want 250", d.HistoryMax)
		}
	})

	t.Run("invalid feed mode falls back to default", func(t *testing.T) {
		if d := ResolveFt8Display(&Ft8DisplayConfig{FeedMode: "bogus"}); d.FeedMode != DefaultFt8FeedMode {
			t.Errorf("FeedMode bogus = %q, want default %q", d.FeedMode, DefaultFt8FeedMode)
		}
	})

	t.Run("colours pass through when set", func(t *testing.T) {
		d := ResolveFt8Display(&Ft8DisplayConfig{HighlightUnworked: "#abcdef", HighlightWorked: "#123456"})
		if d.HighlightUnworked != "#abcdef" || d.HighlightWorked != "#123456" {
			t.Fatalf("colours = (%q, %q), want passthrough", d.HighlightUnworked, d.HighlightWorked)
		}
	})
}

func TestFt8FeedModeValid(t *testing.T) {
	for _, ok := range []string{"accumulate", "single"} {
		if !Ft8FeedModeValid(ok) {
			t.Errorf("Ft8FeedModeValid(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Accumulate", "rolling", "off"} {
		if Ft8FeedModeValid(bad) {
			t.Errorf("Ft8FeedModeValid(%q) = true, want false", bad)
		}
	}
}
